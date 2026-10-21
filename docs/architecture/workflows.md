# Workflows and Synchronization

## Fetch Windows and Historical Backfill

The console's Sources page offers `recent`, `before`, and `range` fetches for
X and YouTube. `POST /api/admin/runs` with `{ "kind": "fetch", "source": "x" }`
plus `"window": { "mode": "recent", "days": 30 }`,
`"window": { "mode": "before", "days": 30 }`, or
`"window": { "mode": "range", "startDate": "2026-09-01", "endDate": "2026-09-30" }`
starts one.
Days must be integers from 1 to 365; date ranges cover at most 365 days. Both
selected dates are inclusive in `Asia/Tokyo`; internal timestamp bounds are
start-inclusive and end-exclusive. `before` uses the oldest persisted post
per X account, or oldest published YouTube video, rather than a UI page's
limited list. With no records, fetch a date range first. X accounts without
records are skipped and reported while other accounts can backfill.

These requests run the chosen source and media stages through the existing
single-flight queue; opening a page does not fetch platforms. They do not run
schedule extraction on historical posts or overwrite the regular incremental
source cursor. `GET /api/admin/sources` reports oldest timestamps and
credential-availability flags only, never keys.

Without official API credentials, recent X fetches filter IDs discoverable on
the public profile, and recent YouTube fetches filter the RSS entries. A chosen
date window cannot enlarge these public sources' finite coverage, so the run
is marked partial and reports the limitation. Historical controls require
environment-only `X_API_BEARER_TOKEN` (with full-archive access) or
`YOUTUBE_API_KEY`. These credentials are sent in HTTP headers, never URLs or
SQLite. The adapter in `backend-go/internal/source/history` uses
[X full-archive search](https://docs.x.com/x-api/posts/search/quickstart/full-archive-search)
and [YouTube date-window search](https://developers.google.com/youtube/v3/docs/search/list).
Source access, quota, and search coverage still depend on the platform. YouTube
channel search is capped at 500 results and may omit results with date sorting;
use narrower date ranges for dense archives. This feature does not promise a
complete export of the platform's archive.

`SOURCE_HISTORY_MAX_PAGES` defaults to four pages per source/account per run
(1–20). X also shares `X_MAX_STATUS_REQUESTS` across accounts, including failed
requests. Each successful page and its continuation checkpoint are stored
atomically in `sync_state` (`x-window`/`youtube-window`). Repeating an interrupted
range resumes its next page; completed ranges can be refreshed. Recent runs
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
saves each source's days independently of the timer interval. CLI/MCP full sync
without fetch windows retains the existing incremental behavior described above.

## Modular Steps and Timers

Synchronization is split into modular steps declared in
`backend-go/internal/jobs` (`StepOrder`), executed in this order: `x`,
`youtube`, `media`, `schedule`. `kano-go sync` (`make sync`) runs every step
and records the ones disabled by `SKIP_*` as skipped.
A saved workflow chooses a subset; its run records `sync_runs.source =
workflow` and exposes per-step progress (`pending`, `running`, `completed`,
`partial`, `failed`, `skipped`) through `/api/admin/runs`. When a selected
step is disabled by `SKIP_*`, its result records `{ skipped: true, reason:
"environment" }` instead of silently disappearing.

A workflow timer is 15 minutes to 7 days. Enabling a timer (or changing its
interval) plans the next run one interval later; it never fires immediately.
The server process checks due workflows every 30 seconds and starts one
through the same single-flight queue as manual and MCP jobs; a due workflow
that finds the queue busy is retried on the next tick. Two workflows are seeded
once (`full-refresh` and `sources-only`) without timers; deleting them does not
recreate them. Set `WORKFLOW_SCHEDULER_ENABLED=0` to disable timers.

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
