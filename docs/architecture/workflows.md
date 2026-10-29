# Workflows and Synchronization

## Fetch Windows and Historical Backfill

The console's Sources page offers `recent`, `before`, `range` and `count`
fetches for X and YouTube; it is the one place to start them. The Posts and
Videos pages open on their lists and link to it from their header.
`POST /api/admin/runs` with `{ "kind": "fetch", "source": "x" }` plus
`"window": { "mode": "recent", "days": 30 }`,
`"window": { "mode": "before", "days": 30 }`,
`"window": { "mode": "range", "startDate": "2026-09-01", "endDate": "2026-09-30" }`,
or `"window": { "mode": "count", "count": 50 }` starts one.
Days must be integers from 1 to 365; date ranges cover at most 365 days; a
count is an integer from 1 to 200 per X account or for the YouTube channel.
Both selected dates are inclusive in `Asia/Tokyo`; internal timestamp bounds
are start-inclusive and end-exclusive. `before` ends at the oldest persisted
post per X account, or oldest published YouTube video, rather than a UI
page's limited list. With no records, fetch a date range first. X accounts
without records are skipped and reported while other accounts can backfill.

The run freezes the validated selection (mode with days, dates or count) in
`workflow_runs.fetch_windows_json`, so a recovered or retried run never
changes it. The step resolves the bounds when it runs and stores them with
its checkpoint, so a resumed window keeps the bounds it started with; X
search ends at least ten seconds before the present.

`GET /api/admin/sources` reports per source `historyAvailable` (whether the
official-API credential is set), `windows` (the modes it can run now) and
the oldest stored timestamps, never keys; a mode outside `windows` is
rejected with a validation message. Each window and credential is served as
follows:

| Window   | X without `X_API_BEARER_TOKEN` | X with the token    | YouTube without `YOUTUBE_API_KEY` | YouTube with the key          |
| -------- | ------------------------------ | ------------------- | --------------------------------- | ----------------------------- |
| `recent` | public timeline                | full-archive search | listed uploads of its days        | date search, then recent step |
| `before` | unavailable                    | full-archive search | unavailable                       | date search                   |
| `range`  | public timeline                | full-archive search | unavailable                       | date search                   |
| `count`  | public timeline                | public timeline     | uploads listing (15 at most)      | uploads listing               |

A YouTube `recent` window without a key keeps the listed uploads published
within its days, so it cannot reach past what the feed lists. With a key the
date search is followed by the regular recent read, which refreshes
reservations the search API does not describe; if that read fails, the run
is `partial` with `youtube_recent_unavailable`. A YouTube `count` window reads
the uploads listing like the recent step (see [YouTube](sources.md#youtube)).

These requests run the chosen source and media stages through the
single-flight queue; opening a page does not fetch platforms. They do not
run schedule extraction on historical posts or overwrite the regular
incremental source cursor. Schedule-image candidates on fetched X pages are
registered with each page (see [Schedule images](sources.md#schedule-images)).
Source schedules, Run now, YouTube pushes and one-off step runs carry no
window: they read with the schedule's frozen sync setting (see
[Source schedules](#source-schedules)), whatever the credentials. A full
sync or scan carries neither and stays incremental.

### Public X timeline windows

X timeline windows page the public FxTwitter timeline
(`api.fxtwitter.com/2/profile/<handle>/statuses`, 100 statuses per page)
newest first. Each status is stored from the timeline page itself, without
vxtwitter detail requests or the incremental pending queue. Paging never
reads or moves the incremental cursor: a `recent` or `range` window
continues until a page's oldest own status predates the window, and a
`count` window stops after the account's newest `count` own statuses,
whatever their age. Reposts are skipped, replies are not listed, and
statuses outside a date window are ignored. Posts that are already stored
keep the text, media and schedule candidate the detail path wrote, so a
window only fills gaps and does not change extraction fingerprints. A
status the timeline lists without usable content is reported as
`<id>: x_invalid_response`; a timeline that ends before the window's start
(or before `count` statuses) reports `x_timeline_end`. Timeline pages use
the X source client's protections: a 10-second timeout, a 4 MiB response
limit, rejected redirects and controlled `x_*` error codes.

### Official API windows

`backend-go/internal/source/history` calls only
`https://api.x.com/2/tweets/search/all`
([X full-archive search](https://docs.x.com/x-api/posts/search/quickstart/full-archive-search),
which needs full-archive access) and
`https://www.googleapis.com/youtube/v3/search`
([YouTube date-window search](https://developers.google.com/youtube/v3/docs/search/list)).
The environment-only credentials travel in request headers
(`Authorization: Bearer` for X, `X-Goog-Api-Key` for YouTube), never in URLs,
SQLite or results. Each request has the `SYNC_TIMEOUT_MS` timeout (7 seconds
by default, at most 60), pages wait `SYNC_REQUEST_DELAY_MS` apart, and
redirects are rejected so a credential header never follows one. Each
response has a fixed 4 MiB byte limit: the adapter checks `Content-Length`
and counts streamed bytes, cancelling an oversized body before parsing or
writing its page. Failures reach run history only as controlled codes:
`source_api_http_<status>`, `source_api_request_failed`,
`source_api_response_too_large`, `source_api_invalid_response`,
`source_api_invalid_records` and `source_api_repeated_page`, never upstream
bodies or credentials. A window that stops at its page budget reports
`history_page_budget_reached; repeat this fetch to continue`.

Source access, quota, and search coverage depend on the platform. YouTube
channel search is capped at 500 results and may omit results with date
sorting; use narrower date ranges for dense archives. This feature does not
promise a complete export of the platform's archive.

### Budgets and checkpoints

`SOURCE_HISTORY_MAX_PAGES` bounds pages per source/account per run (4 by
default, 1–20), for API and timeline pages alike. X windows share
`X_MAX_STATUS_REQUESTS` across accounts as a page budget, including failed
requests: each account gets an even share of what is left, and the first
account rotates across runs per mode (`sync_state` source `x-budget`, key
`window-<mode>`), so a small budget still reaches every account. An account
left without budget reports `X request budget exhausted`.

Each successful page and its continuation checkpoint commit in one
transaction with the page's posts or videos and schedule candidates. The
checkpoint is a `sync_state` row keyed by account and a hash of the
selection: source `x-window` or `youtube-window` for API next tokens, and
`x-timeline-window` for timeline cursors, which are not API next tokens.
Repeating an interrupted window resumes its next page; completed ranges can
be refreshed. A `recent` window resumes only while its saved interval still
overlaps the new one. When a resumed timeline cursor fails, the window
starts again from the newest page, in the same run if the budget allows.
`before` runs finish a pending interval (`pendingWindow`) before moving
farther back, using `x-backfill`/`youtube-backfill` search boundaries. A
completed empty interval also moves this boundary backward, preventing an
endless retry over the same empty month. Failures retain all records and the
last successful checkpoint.

### Source schedules

Each source has its own timer
([ADR-0003](../decisions/ADR-0003-per-source-sync-schedules.md)), stored in
the additive `source_schedules` table and edited on the console's Sources
page. A fresh installation keeps all three disabled.

| Source     | Interval (default, bounds)                        | Fixed chain                           |
| ---------- | ------------------------------------------------- | ------------------------------------- |
| `x`        | 30 min, 15 min – 7 days                           | `x`, `media`, `schedule`, `translate` |
| `youtube`  | 60 min, 15 min – 7 days                           | `youtube`, `media`, `translate`       |
| `bilibili` | 180 min, `BILIBILI_MIN_INTERVAL_MINUTES` – 7 days | `bilibili`, `media`, `translate`      |

When a schedule is due, the server process submits its chain as the ad hoc
run `@source-<source>` (`reconcile` is appended as for every run with a
source step), labelled `source-schedule` in the run history. **Run now** on a
source's section (`POST /api/admin/sources/{source}/run`) submits the same
chain once, triggered by the operator, and leaves the schedule's next run
where it was; the history shows it as "X · run now". The follow-up
steps always run, even when the source wrote nothing: each works from its
own queue and makes no provider call while that queue is empty, so failed
extractions, pending image reviews, download retries and operator
translation requests keep moving while a source is quiet. YouTube pushes
submit the `youtube` chain as well (labelled `websub`). The operator's
single-video refresh keeps its own steps, and the
[live-status probe](#live-status-probe) has its own timer. X has no push, so its
timer is how new posts arrive; YouTube push and the probe catch most
YouTube changes and its timer is a fallback; the Bilibili interval cannot go
below the environment floor, and the adapter's own interval and backoff
checks stay as the safety net.

X and YouTube schedules carry a sync setting that says how much a run reads
([ADR-0002](../decisions/ADR-0002-count-based-source-sync.md)). Each run
freezes it in `workflow_runs.source_sync_json`:

- `{ "mode": "incremental", "max": 50 }`, the default: read newest first until
  a stored record, at most `max` (1–200) records that are not stored yet, per
  X account or for the YouTube channel. An X run that stops short leaves a
  [timeline gap](sources.md#x) that later runs read; a YouTube run that
  leaves new uploads above its cursor keeps the cursor, and later runs take
  them ([YouTube](sources.md#youtube)).
- `{ "mode": "count", "count": N }`: read the newest `N` (1–200) every run,
  stored or not. X reads them as a `count` window from the timeline, which
  leaves the incremental cursor and pending queue alone. YouTube reads the
  uploads listing.

Every other run with an X or YouTube step reads that source with these
settings too, frozen on the run: a YouTube push, Run now, a
[one-off step run](#modular-steps-and-timers) and the operator's single-video refresh.
A step with a fetch window reads its window instead. Credentials never turn
a scheduled read into a date search: X always reads the public timeline (the token serves operator
`before` and `range` windows), and YouTube with a key pages the uploads
playlist at one quota unit per 50 uploads. Full syncs (console, MCP,
`kano-go sync`) carry no setting and run incrementally with
`X_DISCOVERY_LIMIT` for X and 50 uploads for YouTube.

Enabling a schedule or changing its interval plans the next run one interval
later, never immediately; a sync change alone keeps the plan. Claiming a run
the timer submitted plans the next one an interval after the claim. Only the
first claim plans: claiming it again after a retry or after recovery from an
expired lease (both counted in `recovery_count`) keeps the plan, and Run now
does not move it either.

Each plan adds a random offset of whole seconds, uniform in ±10% of the
interval (a 30-minute X timer runs 27–33 minutes apart), so timed reads do
not reach FxTwitter, YouTube or `api.bilibili.com` on a fixed cadence. An
offset that would leave the interval bounds is reflected back, so a plan is
never shorter than 15 minutes nor longer than 7 days. Bilibili's offset is
never negative (0 to +10%): its interval is never below
`BILIBILI_MIN_INTERVAL_MINUTES`, so a plan is never sooner than the floor and
the adapter's interval check does not skip a timed run. Run now, YouTube
pushes and the live-status probe have no offset. The draw is the store's
injectable `SetScheduleOffset`, which tests replace for deterministic plans.
`WORKFLOW_SCHEDULER_ENABLED=0` stops source schedules as well, and `SKIP_X`,
`SKIP_YOUTUBE` and `SKIP_BILIBILI` disable that source's schedule, which the
console reports as `{ skipped: true, reason: "environment" }`.

#### The `workflows` table

Per-source timers are the only timers
([ADR-0003](../decisions/ADR-0003-per-source-sync-schedules.md)). The
`workflows` table, with its `source_sync_json` and `source_lookback_json`
columns, stays in the schema and a fresh installation creates it empty;
nothing reads or writes it, and no timer is derived from it. A run whose
`workflow_id` is not a reserved `@` ID (see
[Backend](backend.md#jobs-and-workflows)) keeps that ID, and the history
labels it as a retired workflow. Every start of `kano-go serve` raises a
stored Bilibili interval below `BILIBILI_MIN_INTERVAL_MINUTES` to that floor.

## Modular Steps and Timers

Synchronization is split into modular steps declared in
`backend-go/internal/jobs` (`StepOrder`), executed in this order: `x`,
`youtube`, `bilibili`, `media`, `schedule`, `translate`, `reconcile`. `kano-go sync` (`make sync`) runs every step
and records the ones disabled by `SKIP_*` as skipped.
A one-off step run (the Runs page, `POST /api/admin/runs` with
`{ "kind": "workflow", "steps": [...] }`) picks any subset from the catalog
(`GET /api/admin/runs/steps`) and runs it once in catalog order; nothing is
saved. Its run records `sync_runs.source = workflow` and exposes per-step progress (`pending`, `running`, `completed`,
`partial`, `failed`, `skipped`) through `/api/admin/runs`. When a selected
step is disabled by `SKIP_*`, its result records `{ skipped: true, reason:
"environment" }` instead of silently disappearing. Every run keeps its own
steps, sync settings and windows, so a failed run with a retryable failed
step can be retried without any saved definition.

`bilibili` paces itself: it reads the account's upload list at most once per
`BILIBILI_MIN_INTERVAL_MINUTES` and never during a captcha backoff, so it is
safe in a manual run soon after its timer (see
[Bilibili](sources.md#bilibili)).

`reconcile` derives the YouTube events of stored videos and applies the
deterministic duplicate-merge rules in
[Duplicate merging](data-model.md#duplicate-merging) to the Japan dates from
30 days ago to 60 days ahead, then the
[video merging](data-model.md#video-merging) rule to every Bilibili upload.
Every run that includes `x`, `youtube`, `bilibili` or `schedule` (sync, scan,
fetch windows, source schedules, Run now, step runs, WebSub pushes and MCP) appends it as its last
step; it can also be selected alone. Its
result counts `considered`, `merged`, `retargeted`, `released`, `kept` and
`ambiguous` events, `eventsTimed` and `eventsCreated` YouTube events, and
`videosConsidered`, `videosMerged`, `videosReleased`, `videosKept` and
`videosAmbiguous` uploads. It advances the dashboard revision only when a
merge decision, a derived YouTube event or a merged upload's translation
changed.

`translate` machine-translates X post text and YouTube titles that lack a
usable translation (see [Translation](sources.md#translation)). Its final
failure does not abort a run, so `reconcile` still runs after it. It reads
only the translation queue and records a disabled result while that queue
is empty. `POST /api/admin/translations/request` starts it alone as a
`translate` run after queueing operator requests.

The server process checks due source schedules every 30 seconds and submits
the one with the earliest planned run, so a frequent source cannot starve
another, through the same single-flight queue as manual and MCP jobs; a
schedule that finds the queue busy stays due for the next tick. Set
`WORKFLOW_SCHEDULER_ENABLED=0` to stop the source schedules and the
live-status probe.

### Live-status probe

The YouTube [live-status probe](sources.md#live-status-probe) has its own
timer and its own operator interval, stored in `app_settings`
(`youtube_live_probe_enabled`, default on; `youtube_live_probe_interval_minutes`,
default 15, 5–360). It is edited in `/admin` (Sources → Live-status probe).
Each active video is due by its phase against its reserved start (its actual
start once on air), counted from its last successful check (a watch page or
a Data API answer) or probe request, whichever is newer:

| Phase      | When                                          | Next check                           |
| ---------- | --------------------------------------------- | ------------------------------------ |
| `prestart` | within 15 minutes before the start            | once in that lead, then at the start |
| `starting` | from the start for 45 minutes, not yet live   | every 3 minutes                      |
| `live`     | the check reported the broadcast live         | every operator interval              |
| `overdue`  | 45 minutes after the start and still not live | every operator interval              |

A check by the regular YouTube step counts as well, so a reservation it just
read is not asked again. The same server process checks every 30 seconds;
when a video is due and at least two minutes have passed since the previous
probe run was queued, it queues a run of the single `live` step
(`kind: "live"`, ad hoc workflow `@live`, labelled `live-probe`) through the
same single-flight queue. The run asks only about the due videos, earliest
first: with `YOUTUBE_API_KEY`, in one Data API call, then the watch pages of
those the API could not settle (see
[Live-status probe](sources.md#live-status-probe)). A busy queue or nothing
due waits for the next tick. Requests are recorded per video in `sync_state`
(source `youtube-live`), answered or not, so a video that keeps failing is
asked again on its phase's schedule rather than on every tick; entries older
than two days are dropped. Runs are recorded in `sync_runs` and
`/api/admin/runs` with `active`, `candidates` (the due videos), `requested`
(watch pages), `apiRequests` (Data API calls), `inspected`, `live`,
`changed` and fixed error codes; a run that answered none of its videos is
`failed`, one with any recorded failure (an API call that failed but whose
pages answered, for example) `partial`.

The probe may run more often than a source schedule (15 minutes) because one
run asks about at most five videos and runs only while something is due.
With a key it spends one quota unit per run, at most 720 a day.
One stream that starts on time costs a check before the start, a few in the
starting window and four an hour on air at the default interval; a waiting
room that never opens costs at most 15 in its starting window and then one
per interval until 24 hours after its start. At most five pages per run and
runs two minutes apart bound the worst case, several reservations starting
together, at 150 pages an hour; a regular YouTube step every 15 minutes (feed,
streams page and up to 12 detail pages) is about 56 requests an hour. Longer
than six hours would let a stale result outlive most streams. `live` is not
a catalog step: step runs and full syncs never select it.
`WORKFLOW_SCHEDULER_ENABLED=0` and `SKIP_YOUTUBE=1` stop the probe as well.
Page loads, refreshes and revision requests never start it.

## Run Execution

Every run is a durable `workflow_runs` row with its ordered
`workflow_run_steps` and one `workflow_step_attempts` row per try; the
console's job list and the `sync_runs` history are projections of them (see
[Backend](backend.md#jobs-and-workflows) for the reserved run IDs). A run
freezes what its steps read: `fetch_windows_json` for operator windows,
`source_sync_json` for schedule sync settings and `source_lookback_json` for
per-step days. Retry and recovery therefore repeat the same work without a
saved definition.

Statuses are fixed enums. Runs use `queued`, `running`, `succeeded`,
`partial`, `failed`, `cancelled` and `blocked`; steps use the same set with
`pending` in place of `queued`; attempts use `running` and the terminal
values. `trigger_source` and `execution_source` are `operator`, `scheduler`
or `recovery`. Idempotency keys are stored as SHA-256 hashes. Admin
projections omit raw payloads, stack traces, paths, credentials and provider
text.

Each step records the adapter that serves it in `adapter_capability`
(`x_recent`, `youtube_recent`, `bilibili_recent`, `youtube_live`,
`media_download`, `schedule_extract`, `text_translate` or
`event_reconcile`), which projections read before the constrained
`capability` column (`synthetic`, `x_recent` or `capability_not_migrated`).
A step without an adapter finishes `blocked` with `capability_not_migrated`
and never counts as successful work.

**Lease.** Claiming a queued run takes the SQLite write lock, sets it
`running` and writes the single `workflow_leases` row (scope `global`) with
an owner, a generation and an expiry two minutes ahead; a heartbeat renews
it every 40 seconds. Every step, attempt and run write, and every source
transaction, checks owner, generation and expiry in the same transaction,
so a worker that lost its lease cannot commit, and a failed renewal cancels
its outbound requests. A partial unique index allows at most one `queued` or
`running` run, so a second start answers `409` with `busy` and the active
run. A dispatched run has at most 20 minutes.

**Retry.** A step error carries a category. `timeout`, `rate_limited` and
`dependency_unavailable` are retryable and get one more attempt in the same
run; `permanent`, `schema_failure`, `validation_failure`, `cancelled` and
`blocked` are not. The final failure of `x`, `youtube`, `bilibili` or
`translate` is recorded with its code and the run continues, finishing
`partial`, or `failed` when no step completed. Any other final failure ends
the run and marks its pending steps `blocked`.
`POST /api/admin/runs/{id}/retry` requeues a `failed`, `partial` or
`blocked` run while no other run is active, when it has a retryable failed
step and no permanent failure: retryable failed and blocked steps return to
`pending`, and completed steps and earlier attempts stay.

**Recovery.** At startup and every 30 seconds the server recovers `running`
runs whose lease is missing or expired: their running attempts become
`failed` with `lease_expired` (retryable), their running steps return to
`pending`, and the run returns to `queued` with `execution_source =
recovery` and `recovery_count` incremented. A queued run is then dispatched.
`POST /api/admin/runs/{id}/cancel` records the request; a queued run is
cancelled at once, and a running one stops its network requests and finishes
`cancelled` with its pending steps. Finishing a run updates its `sync_runs`
row and the dashboard revision and releases the lease.

Schedule-step outcomes combine post extraction and image verification. An
image-only failure cannot be reported as success: a scan with failures and no
successful/cached/uncertain work is `failed`; mixed outcomes are `partial`.
Workflow progress, persisted `sync_runs`, and standalone scans use the same
aggregation. Completed sync/workflow jobs expose their persisted run, including
its failure status, and always release the single-flight queue after settlement.

## Implementation Ownership

backend-go/internal/jobs owns the ordered step catalog and presents durable
workflow runs as operator jobs. internal/workflow maps steps to their adapters
(`SourceSteps`), executes runs with leases, retries and recovery, and owns the
single-flight queue, and the timer that starts due source schedules
(`SourceChains`) through it. `jobs.QueueManager` submits Run now and step
runs and freezes each source step's schedule sync on them.
A new module needs one catalog entry and one step adapter; the console reads
the catalog rather than maintaining another step list. See
[Backend](backend.md#jobs-and-workflows).

Adapters, candidate selection, model routes, modality, retries, and per-post
reprocess behavior are defined in [Sources and extraction](sources.md).
Register media candidates before bounded downloads; only successfully fetched
records are upserted. A failed step records its outcome and retains known state.

## Related Docs

- [Sources and extraction](sources.md)
- [Failure and freshness](../operations/reliability.md)
- [Configuration](../operations/configuration.md)
- [Admin operations](../operations/database.md#manual-maintenance)
