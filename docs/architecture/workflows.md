# Workflows and Synchronization

## Fetch Windows and Historical Backfill

The console's Sources page offers `recent`, `before`, `range` and `count`
fetches for X and YouTube. The Posts (X) and Videos (YouTube)
pages repeat the common ones as shortcuts: one-click `recent` windows of 1, 3,
7 or 30 days, a `range` form, and one-click `count` windows of 20, 50,
100 or 200 posts or videos. `POST /api/admin/runs` with `{ "kind": "fetch", "source": "x" }`
plus `"window": { "mode": "recent", "days": 30 }`,
`"window": { "mode": "before", "days": 30 }`,
`"window": { "mode": "range", "startDate": "2026-09-01", "endDate": "2026-09-30" }`,
or `"window": { "mode": "count", "count": 50 }` starts one.
Days must be integers from 1 to 365; date ranges cover at most 365 days; a
count is an integer from 1 to 200 per X account or for the YouTube channel.
A YouTube `count` window reads the uploads listing like the recent step (see
[YouTube](sources.md#youtube)); without `YOUTUBE_API_KEY` the feed lists 15. Both
selected dates are inclusive in `Asia/Tokyo`; internal timestamp bounds are
start-inclusive and end-exclusive. `before` uses the oldest persisted post
per X account, or oldest published YouTube video, rather than a UI page's
limited list. With no records, fetch a date range first. X accounts without
records are skipped and reported while other accounts can backfill.
`GET /api/admin/sources` lists the modes each source can run now as
`windows`.

These requests run the chosen source and media stages through the existing
single-flight queue; opening a page does not fetch platforms. They do not run
schedule extraction on historical posts or overwrite the regular incremental
source cursor. `GET /api/admin/sources` reports oldest timestamps and
credential-availability flags only, never keys.

Without `X_API_BEARER_TOKEN`, X `recent` and `range` windows page the public
FxTwitter timeline (`api.fxtwitter.com/2/profile/<handle>/statuses`, 100
statuses per page) newest first, and `count` windows always do, with or
without the token. Each status is stored from the timeline page itself,
without vxtwitter detail requests or the incremental pending queue. Paging
never reads or moves the incremental cursor: a `recent` or `range` window
continues until a page's oldest own status predates the window, and a `count`
window stops after the account's newest `count` own statuses, whatever their
age. Reposts are skipped, replies are not listed, and statuses outside a date
window are ignored. Posts that are already stored keep the text, media and
schedule candidate the detail path wrote, so a window only fills gaps and
does not change extraction fingerprints. A status the timeline lists without
usable content is reported as `<id>: x_invalid_response`; a timeline that
ends before the window's start (or before `count` statuses) reports
`x_timeline_end`. Timeline pages use the X source client's protections: a
10-second timeout, a 4 MiB response limit, rejected redirects and controlled
`x_*` error codes. Recent YouTube fetches without `YOUTUBE_API_KEY` filter the
RSS entries; a chosen date window cannot enlarge that finite coverage, so the
run is marked partial and reports the limitation. `before` windows require
environment-only `X_API_BEARER_TOKEN` (with full-archive access) or
`YOUTUBE_API_KEY`, as YouTube `range` windows do. These credentials are sent
in HTTP headers, never URLs or SQLite. The adapter in
`backend-go/internal/source/history` uses
[X full-archive search](https://docs.x.com/x-api/posts/search/quickstart/full-archive-search)
and [YouTube date-window search](https://developers.google.com/youtube/v3/docs/search/list).
Source access, quota, and search coverage still depend on the platform. YouTube
channel search is capped at 500 results and may omit results with date sorting;
use narrower date ranges for dense archives. This feature does not promise a
complete export of the platform's archive.

`SOURCE_HISTORY_MAX_PAGES` defaults to four pages per source/account per run
(1–20), for API and timeline pages alike. X also shares
`X_MAX_STATUS_REQUESTS` across accounts as a page budget, including failed
requests. Each successful page and its continuation checkpoint are stored
atomically in `sync_state` (`x-window`/`youtube-window`, and
`x-timeline-window` for timeline cursors, which are not API next tokens).
Repeating an interrupted window resumes its next page; completed ranges can be
refreshed. When a resumed timeline cursor fails, the window starts again from
the newest page, in the same run if the budget allows. Recent runs
finish a still-relevant pending interval before opening the next rolling
interval. `before` runs finish pending pages before moving farther back, using
`x-backfill`/`youtube-backfill` search boundaries. A completed empty interval
also moves this boundary backward, preventing an endless retry over the same
empty month. Failures retain all records and the last successful checkpoint;
request errors expose only controlled codes, not upstream bodies or credentials.
Each official API response has a fixed 4 MiB byte limit. The adapter checks
`Content-Length` and counts streamed bytes, cancelling an oversized body before
parsing or writing its page. Timeouts and redirect blocking apply throughout.

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
source step), labelled `source-schedule` in the run history. The follow-up
steps always run, even when the source wrote nothing: each works from its
own queue and makes no provider call while that queue is empty, so failed
extractions, pending image reviews, download retries and operator
translation requests keep moving while a source is quiet. YouTube pushes
submit the `youtube` chain as well (labelled `websub`). The operator's
single-video refresh keeps its own steps, and the
[live-status probe](#live-status-probe) is unchanged. X has no push, so its
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
  [timeline gap](sources.md#x) that later runs read.
- `{ "mode": "count", "count": N }`: read the newest `N` (1–200) every run,
  stored or not. X reads them as a `count` window from the timeline, which
  leaves the incremental cursor and pending queue alone. YouTube reads the
  uploads listing.

YouTube pushes and manual runs of saved workflows read X and YouTube with
these settings too. Credentials never turn a scheduled read into a date
search: X always reads the public timeline (the token serves operator
`before` and `range` windows), and YouTube with a key pages the uploads
playlist at one quota unit per 50 uploads. Full syncs (console, MCP,
`kano-go sync`) carry no setting and run incrementally with
`X_DISCOVERY_LIMIT` for X and 50 uploads for YouTube.

Enabling a schedule or changing its interval plans the next run one interval
later, never immediately; a sync change alone keeps the plan. Claiming a
schedule's run plans the next one an interval after the claim.
`WORKFLOW_SCHEDULER_ENABLED=0` stops source schedules as well, and `SKIP_X`,
`SKIP_YOUTUBE` and `SKIP_BILIBILI` disable that source's schedule, which the
console reports as `{ skipped: true, reason: "environment" }`.

#### Upgrade from workflow timers

Before this, saved workflows were the only timers. On the first start of an
upgraded database, `kano-go serve` runs one migration, guarded by the
`app_settings` key `source_schedules_migrated`:

- For each timed workflow that contains a source step, that source's
  schedule is enabled with the smallest interval among such workflows,
  raised to the Bilibili floor where it applies, and the stored `sourceSync`
  of that workflow (or the default).
- That workflow's timer is then disabled. Its steps and history stay.
- Timed workflows without a source step keep their timers.

Every start also raises a stored Bilibili interval below a raised
`BILIBILI_MIN_INTERVAL_MINUTES` to the new floor. `workflows.source_sync_json`
stays in the schema and is no longer read; the retired `sourceLookbackDays`
stays in `source_lookback_json` and is no longer read for saved workflows.

## Modular Steps and Timers

Synchronization is split into modular steps declared in
`backend-go/internal/jobs` (`StepOrder`), executed in this order: `x`,
`youtube`, `bilibili`, `media`, `schedule`, `translate`, `reconcile`. `kano-go sync` (`make sync`) runs every step
and records the ones disabled by `SKIP_*` as skipped.
A saved workflow chooses a subset for manual runs; its run records
`sync_runs.source = workflow` and exposes per-step progress (`pending`, `running`, `completed`,
`partial`, `failed`, `skipped`) through `/api/admin/runs`. When a selected
step is disabled by `SKIP_*`, its result records `{ skipped: true, reason:
"environment" }` instead of silently disappearing.

`bilibili` paces itself: it reads the account's upload list at most once per
`BILIBILI_MIN_INTERVAL_MINUTES` and never during a captcha backoff, so it is
safe in a manual workflow run soon after its timer (see
[Bilibili](sources.md#bilibili)). New installations include it in both
starting workflows; existing workflows keep their step lists.

`reconcile` applies the deterministic duplicate-merge rules in
[Duplicate merging](data-model.md#duplicate-merging) to the Japan dates from
30 days ago to 60 days ahead, then the
[video merging](data-model.md#video-merging) rule to every Bilibili upload.
Every run that includes `x`, `youtube`, `bilibili` or `schedule` (sync, scan,
fetch windows, source schedules, WebSub pushes, MCP and saved workflows) appends it as its last
step, so saved workflows need no edit; it can also be selected alone. Its
result counts `considered`, `merged`, `retargeted`, `released`, `kept` and
`ambiguous` events, and `videosConsidered`, `videosMerged`, `videosReleased`,
`videosKept` and `videosAmbiguous` uploads. It advances the dashboard
revision only when a merge decision or a merged upload's translation
changed.

`translate` machine-translates X post text and YouTube titles that lack a
usable translation (see [Translation](sources.md#translation)). Its final
failure does not abort a run, so `reconcile` still runs after it. It reads
only the translation queue and records a disabled result while that queue
is empty. `POST /api/admin/translations/request` starts it alone as a
`translate` run after queueing operator requests.

Saved workflows are presets for manual runs and custom combinations, such
as a translate-only sweep. A workflow timer is 15 minutes to 7 days and is
available only to workflows without a source step: the API refuses a timer
on a workflow that reads a source and names the source schedule to use
instead. Enabling a timer (or changing its interval) plans the next run one
interval later; it never fires immediately. The server process checks due
workflows and due source schedules together every 30 seconds and submits
the one with the earliest planned run, so a frequent source cannot starve
another, through the same single-flight queue as manual and MCP jobs; an
item that finds the queue busy stays due for the next tick. Two workflows
are seeded once (`full-refresh` "Full refresh", which also translates on a
new installation, and `sources-only` "Sources only") without timers;
deleting them does not recreate them, and seeding never renames an existing
workflow. Set `WORKFLOW_SCHEDULER_ENABLED=0` to disable all timers.

### Live-status probe

The YouTube [live-status probe](sources.md#live-status-probe) has its own
timer and its own operator interval, stored in `app_settings`
(`youtube_live_probe_enabled`, default on; `youtube_live_probe_interval_minutes`,
default 15, 5–360). It is edited in `/admin` (Sources → Live-status probe).
Each active video is due by its phase against its reserved start (its actual
start once on air), counted from its last successful watch-page check or
probe request, whichever is newer:

| Phase      | When                                          | Next check                           |
| ---------- | --------------------------------------------- | ------------------------------------ |
| `prestart` | within 15 minutes before the start            | once in that lead, then at the start |
| `starting` | from the start for 45 minutes, not yet live   | every 3 minutes                      |
| `live`     | the page reported the broadcast live          | every operator interval              |
| `overdue`  | 45 minutes after the start and still not live | every operator interval              |

A check by the regular YouTube step counts as well, so a reservation it just
read is not asked again. The same server process checks every 30 seconds;
when a video is due and at least two minutes have passed since the previous
probe run was queued, it queues a run of the single `live` step
(`kind: "live"`, ad hoc workflow `@live`, labelled `live-probe`) through the
same single-flight queue. The run requests only the due videos, earliest
first. A busy queue or nothing due waits for the next tick. Requests are
recorded per video in `sync_state` (source `youtube-live`), answered or not,
so a page that keeps failing is asked again on its phase's schedule rather
than on every tick; entries older than two days are dropped. Runs are recorded
in `sync_runs` and `/api/admin/runs` with `active`, `candidates` (the due
videos), `requested`, `inspected`, `live`, `changed` and fixed error codes; a
run whose pages all fail is `failed`, a mixed one `partial`.

The probe may run more often than a source schedule (15 minutes) because one
run requests at most five watch pages and runs only while something is due.
One stream that starts on time costs a check before the start, a few in the
starting window and four an hour on air at the default interval; a waiting
room that never opens costs at most 15 in its starting window and then one
per interval until 24 hours after its start. At most five pages per run and
runs two minutes apart bound the worst case, several reservations starting
together, at 150 pages an hour; a regular YouTube step every 15 minutes (feed,
streams page and up to 12 detail pages) is about 56 requests an hour. Longer
than six hours would let a stale result outlive most streams. `live` is not
a catalog step: saved workflows and full syncs never select it.
`WORKFLOW_SCHEDULER_ENABLED=0` and `SKIP_YOUTUBE=1` stop the probe as well.
Page loads, refreshes and revision requests never start it.

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
single-flight queue, and the timer that starts due saved workflows and source
schedules (`SourceChains`) through it.
A new module needs one catalog entry and one step adapter; the console reads
the catalog rather than maintaining another step list. See
[Go backend](go-standalone.md#jobs-and-workflows).

Adapters, candidate selection, model routes, modality, retries, and per-post
reprocess behavior are defined in [Sources and extraction](sources.md).
Register media candidates before bounded downloads; only successfully fetched
records are upserted. A failed step records its outcome and retains known state.

## Related Docs

- [Sources and extraction](sources.md)
- [Failure and freshness](../operations/reliability.md)
- [Configuration](../operations/configuration.md)
- [Admin operations](../operations/database.md#manual-maintenance)
