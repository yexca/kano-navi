# Workflows and Synchronization

## Fetch Windows and Historical Backfill

The console's Sources page offers `recent`, `before`, and `range` fetches for
X and YouTube, and `count` fetches for X. The Posts (X) and Videos (YouTube)
pages repeat the common ones as shortcuts: one-click `recent` windows of 1, 3,
7 or 30 days, a `range` form, and for X one-click `count` windows of 20, 50,
100 or 200 posts. `POST /api/admin/runs` with `{ "kind": "fetch", "source": "x" }`
plus `"window": { "mode": "recent", "days": 30 }`,
`"window": { "mode": "before", "days": 30 }`,
`"window": { "mode": "range", "startDate": "2026-09-01", "endDate": "2026-09-30" }`,
or `"window": { "mode": "count", "count": 50 }` starts one.
Days must be integers from 1 to 365; date ranges cover at most 365 days; a
count is an integer from 1 to 200 per account and is accepted for X only. Both
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

Saved workflows have `sourceLookbackDays: { x: 7, youtube: 14 }`, persisted in
the additive `workflows.source_lookback_json` column. Existing workflows receive
these defaults without clearing settings, snapshots, or timers. Manual and
scheduled workflow runs both use rolling recent windows; the workflow editor
saves each source's days independently of the timer interval. A source API
serves that window only with its credential; without `X_API_BEARER_TOKEN` a
saved workflow's X step stays incremental and never pages the timeline window
path, which serves operator fetches only. CLI/MCP full sync
without fetch windows retains the existing incremental behavior described above.

## Modular Steps and Timers

Synchronization is split into modular steps declared in
`backend-go/internal/jobs` (`StepOrder`), executed in this order: `x`,
`youtube`, `bilibili`, `media`, `schedule`, `translate`, `reconcile`. `kano-go sync` (`make sync`) runs every step
and records the ones disabled by `SKIP_*` as skipped.
A saved workflow chooses a subset; its run records `sync_runs.source =
workflow` and exposes per-step progress (`pending`, `running`, `completed`,
`partial`, `failed`, `skipped`) through `/api/admin/runs`. When a selected
step is disabled by `SKIP_*`, its result records `{ skipped: true, reason:
"environment" }` instead of silently disappearing.

`bilibili` paces itself: it reads the account's upload list at most once per
`BILIBILI_MIN_INTERVAL_MINUTES` and never during a captcha backoff, so it is
safe in a frequent workflow (see [Bilibili](sources.md#bilibili)). New
installations include it in both starting workflows; existing workflows keep
their step lists, so an operator adds it on the Runs & workflows page.

`reconcile` applies the deterministic duplicate-merge rules in
[Duplicate merging](data-model.md#duplicate-merging) to the Japan dates from
30 days ago to 60 days ahead, then the
[video merging](data-model.md#video-merging) rule to every Bilibili upload.
Every run that includes `x`, `youtube`, `bilibili` or `schedule` (sync, scan,
fetch windows, WebSub pushes, MCP and timed workflows) appends it as its last
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

A workflow timer is 15 minutes to 7 days. Enabling a timer (or changing its
interval) plans the next run one interval later; it never fires immediately.
The server process checks due workflows every 30 seconds and starts one
through the same single-flight queue as manual and MCP jobs; a due workflow
that finds the queue busy is retried on the next tick. Two workflows are seeded
once (`full-refresh` "Full refresh", which also translates on a new
installation, and `sources-only` "Sources only") without timers; deleting them does not recreate them, and seeding never renames an
existing workflow. Set `WORKFLOW_SCHEDULER_ENABLED=0` to disable timers.

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

The probe may run more often than a saved workflow (15 minutes) because one
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
single-flight queue and the timer that starts due saved workflows through it.
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
