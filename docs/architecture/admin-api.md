# Admin API

The operator console at `/admin` is the only client of `/api/admin`. The API
is a resource-oriented JSON surface served by `backend/internal/adminapi`,
which owns one route table (Go `http.ServeMux` method patterns), the session
and origin guards, and the shared response conventions. Feature packages
(`llmadmin`, `contentadmin`, `outboundproxy`, YouTube `websub`) contribute route handlers;
they do not route paths themselves.

MCP (`/mcp`) is a separate boundary with its own projections and never reuses
these routes. See [Public API](public-api.md#mcp-boundary-mcp).

## Conventions

- Every response carries `Cache-Control: no-store, no-transform` and is
  never compressed.
- All routes except `GET /session`, `POST /session` and `DELETE /session`
  require the admin session (production) and every unsafe method passes the
  Fetch Metadata / origin guard, including login.
- Unsafe methods answer `503 {"error":"read_only"}` when the process opened
  SQLite read-only. A missing runtime dependency (no job queue) answers
  `503 {"error":"unavailable"}`.
- Errors are `{"error": "<code>", "message"?: "<safe text>"}`. Validation is
  `400 invalid_input` with an operator-facing message; malformed JSON is
  `400 invalid_json`; oversized bodies are `413 request_too_large`; unknown
  resources are `404 <resource>_not_found`; unknown routes are `404 not_found`
  and wrong methods are `405 method_not_allowed` with `Allow`.
- Lists are `{"items": [...], "page", "pageSize", "total", "totalPages"}`.
  `page` is 1-based and clamped to the last page; each list documents its
  maximum `pageSize`.
- Single-resource writes answer with the updated resource under its own name
  (`{"event": ...}`); deletes answer `204`.
- Timestamps are ISO 8601 strings; dates are Japan dates (`YYYY-MM-DD`).
- No response contains secrets, provider ciphertext, `raw_json`, absolute
  paths or raw upstream error bodies.

## Session

| Method and path   | Body / query | Result                                            |
| ----------------- | ------------ | ------------------------------------------------- |
| `GET /session`    |              | `{authenticated, mode, requiresPassword}`         |
| `POST /session`   | `{password}` | session payload; `401 invalid_credentials`, `429` |
| `DELETE /session` |              | `204`; the HttpOnly cookie is cleared             |

## Overview

`GET /overview` is the console home. It is read-only, bounded and does no
external work:

```json
{
  "visitorRegionEnabled": false,
  "activeRun": Run | null,
  "recentRuns": [Run],            // newest 6
  "scheduler": {"enabled", "running", "tickSeconds"},
  "timers": {"scheduled", "nextRunAt"},
  "queue": [{"kind", "source"?, "nextRunAt", "due"}],
  "backlog": {
    "extraction",       // posts queued for re-extraction
    "postRefresh",      // X posts with a refresh from the source queued
    "postTranslation",  // posts with a machine translation requested
    "videoRefresh",     // YouTube videos with an open refresh request
    "videoTranslation"  // video titles with a machine translation requested
  },
  "sources": {
    "x":       {"skipped", "lastSuccessAt", "lastStep"},
    "x_related": {"skipped", "lastSuccessAt", "lastStep"}, // lastSuccessAt: the newest related account's read
    "youtube": {"skipped", "lastSuccessAt", "lastStep", "push": {"enabled", "verified"}},
    "bilibili": {"skipped", "lastSuccessAt", "lastStep"},
    "bilibili_dynamics": {"skipped", "lastSuccessAt", "lastStep"}, // lastSuccessAt: the listing's
    "shop": {"skipped", "lastSuccessAt", "lastStep"}
  },
  "review": {
    "pendingImages",          // unlabelled images with a URL the model has not settled (pending, uncertain, failed)
    "suspectedCancellations", // live events whose review is `suspected`
    "uncertainEvents",        // live events whose review is `uncertain`
    "failedPosts",            // posts with a failed extraction or translation
    "failedExtractions",      // posts whose last extraction failed
    "failedTranslations",     // X and Bilibili posts whose current wording failed machine translation and still lacks a translation
    "videoMerges",            // unmerged Bilibili uploads the merge rule leaves to the operator
    "bilibiliForms"           // unmerged Bilibili uploads waiting for the operator's form decision
  },
  "counts": {"upcomingEvents", "events", "posts", "videos"}
}
```

`timers` counts the enabled [source schedules](#sources) this process runs
(not those disabled by `SKIP_*`) and the earliest planned run among them.
`queue` lists the timed work in the order it reaches the single-flight
queue: each of those schedules (`kind: "source"` with its `source`) and the
[live-status probe](workflows.md#live-status-probe) when it has a next run
(`kind: "live-probe"`), by planned run and then by source, as the scheduler
picks due schedules. `due` is true once `nextRunAt` has passed; a due entry
waits for the current run and the entries due before it.

`backlog` counts the operator requests that wait for the step that does
the work: schedule extraction takes a re-extraction, the X step a post
refresh, the YouTube step a video refresh and the translate step a machine
translation. A video refresh request counts while it is open, for a day
after it was made. Each count matches the `queued` filter of
[`GET /posts`](#posts) or [`GET /videos`](#videos).

`lastSuccessAt` is the newest successful incremental read of the source
(`sync_state.last_success_at`). `lastStep` is `{runId, status, finishedAt}` of
the source's step in the newest finished run among the last 20 that included
it, or `null`.

The overview's `visitorRegionEnabled` flag controls the public pages' optional
mainland China site suggestion. It defaults to `false`.

## Site settings

| Method and path                | Body                     | Result                   |
| ------------------------------ | ------------------------ | ------------------------ |
| `GET /settings/visitor-region` |                          | `{ "enabled": boolean }` |
| `PUT /settings/visitor-region` | `{ "enabled": boolean }` | `{ "enabled": boolean }` |

The setting is stored in `app_settings` as `visitor_region_enabled`,
`visitor_region_provider_name`, `visitor_region_provider_url` and
`visitor_region_mirror_url`. Enabling it requires a provider name and absolute
HTTP(S) URLs for the provider website and mirror site. It publishes the site
and the same-origin country-code check described in the [public API](public-api.md#public-snapshot-contract); it never sends an IP to a geolocation service.

### Image copies

| Method and path       | Query          | Result                |
| --------------------- | -------------- | --------------------- |
| `GET /media/variants` | `refresh` (=1) | `{groups, checkedAt}` |

`groups` is the [status](media-cache.md#copy-groups) of the `lineblog`,
`works`, `history` and `media` groups in that order, each `{group, originals, ready,
unhashed, widths: [{width, copies, kept, missing}]}`, and `checkedAt` is when
it was listed. The listing is answered again until another run finishes, or
made anew with `refresh=1` ([Copies Run](media-cache.md#copies-run)). A
process without the service's data directory and built site answers
`503 unavailable`. The copies themselves are made by
`POST /runs {"kind": "variants"}`.

### Works upload covers

| Method and path           | Query | Result                                                                   |
| ------------------------- | ----- | ------------------------------------------------------------------------ |
| `GET /media/works-covers` | none  | `{total, covered, due, waiting, uncovered, uploads, skipped, checkedAt}` |

The counts sort the packaged upload list's uploads by the
[cover rules](media-cache.md#works-upload-covers): `covered` have a ready
cover, `due` are those the next media step looks up or queues, `waiting`
have an image or lookup on its way or waiting to retry, and `uncovered` have
every video gone. `uploads` lists every upload without a cover, newest first,
as `{id, date, state, videos}` with `state` `due`, `waiting` or `uncovered`;
each video, in cover order, is `{platform, id, members, state, code,
retryAt}` with `state` `due`, `waiting` or `gone`, the error code of its
last failed lookup or download and when that failure is tried again.
`skipped` is true while `SKIP_MEDIA` disables the media step, and `checkedAt`
is when the snapshot was read. The route reads the snapshot only and makes
no lookup; a process without the packaged list answers `503 unavailable`.
The covers are filled by `POST /runs {"kind": "workflow", "steps":
["media"]}`.

## Runs

A run is one single-flight unit of work on the durable workflow queue. Its
shape is the `jobs.Job` projection shared with MCP control tools:
`{id, kind, status, createdAt, startedAt, finishedAt, triggeredBy, workflowId,
sourceSchedule, steps, progress, fetchWindows, result, error, run}` where
`kind` is `sync`, `scan`, `translate`, `variants`, `workflow` or `live` (the YouTube
live-status probe, started only by its timer with `triggeredBy:
"live-probe"` and the one step `live`), `status` is `queued`, `running`,
`completed` or `failed`, and `progress` maps each step to `pending`,
`running`, `completed`, `partial`, `failed`, `skipped` or `cancelled`.
`sourceSchedule` names the source whose chain the run is: its timer
(`triggeredBy: "source-schedule"`) or the operator's Run now
(`triggeredBy: "admin"`); it is null for other runs. `workflowId` is null
for ad hoc runs, which every submission creates; a stored run whose ID names
a saved workflow reports that ID there, and its name is not read.

| Method and path          | Body / query              | Result                                 |
| ------------------------ | ------------------------- | -------------------------------------- |
| `GET /runs`              | `page`, `pageSize` (≤ 50) | list of runs, newest first             |
| `GET /runs/steps`        |                           | `{steps, skippedSteps}`                |
| `GET /runs/{id}`         |                           | `{run}`; `404 run_not_found`           |
| `POST /runs`             | see below                 | `202 {run}`; `409 {error:"busy", run}` |
| `POST /runs/{id}/cancel` |                           | `202 {run}`                            |
| `POST /runs/{id}/retry`  |                           | `202 {run}`; `409 busy`                |
| `GET /runs/retention`    |                           | retention settings and cleanup status  |
| `PUT /runs/retention`    | see below                 | retention settings and cleanup status  |

`POST /runs` starts exactly one of:

- `{"kind": "sync"}`: every source, media, schedule and translate step.
- `{"kind": "scan"}`: the automatic schedule extraction only.
- `{"kind": "translate"}`: the translate step only.
- `{"kind": "variants"}`: the `variants` step only, the
  [copies run](media-cache.md#copies-run). A process without the service's
  data directory and built site answers `400 invalid_input`.
- `{"kind": "workflow", "steps": ["x", "translate"]}`: a one-off step run.
  Known catalog steps run once in catalog order; an empty list or another
  value is `400 invalid_input`. Source steps read as their
  [source schedule](#sources) does.
- `{"kind": "fetch", "source": "x" | "x_related" | "youtube" | "bilibili" | "bilibili_dynamics" | "shop", "method": Method, "window":
Selection, "options"?: {"extract"?, "translate"?, "refresh"?}}`: one
  source plus media for a
  [manual fetch](workflows.md#manual-fetches-and-historical-backfill)
  through the one method it names. `Method` is `timeline` or `api` for X,
  `timeline` for a related account, `api`, `rss` or `ytdlp` for YouTube,
  `space` for the Bilibili uploads and dynamics, and `catalog` for the shop, whose
  fetch is the `shop` step alone, with `{mode: "all"}` and no option. `Selection` is `{mode: "count", count}`
  (1 to the method's `maxCount`), `{mode: "all"}`, `{mode: "range",
startDate, endDate}` or `{mode: "before", days}` (1–365), as the method's
  `modes` allow. An X selection may add `account`, one of `X_HANDLES`, to
  read that account alone. A related account's selection is
  `{mode: "incremental", account}`, where `account` is one of the
  [related accounts](#sources). Either handle may carry a leading `@` and
  any letter case, and the run freezes its configured spelling; another
  account answers `400 invalid_input`, as does `account` on a YouTube,
  Bilibili or shop fetch. `extract` (X, related-account and Bilibili
  dynamics methods) adds the `schedule` step,
  `translate` the `translate` step, and `refresh` (YouTube methods)
  rewrites stored videos the listing names again; each is a boolean and
  defaults to false. An unknown method, a mode or count the method does not
  serve, an option it does not take, or a method that cannot run now (its
  credential is missing) answers `400 invalid_input`; the fetch never falls
  back to another method.

A busy queue answers `409` with the active run. There are no saved workflows
([source schedules](../architecture/workflows.md#source-schedules)): a
`workflowId` in the body answers `400 invalid_input`, and there are no
`/workflows` routes (`404 not_found`).

`GET /runs/steps` is the step catalog `[{id, group}]` in execution order
(`x`, `x_related`, `youtube`, `bilibili`, `bilibili_dynamics`, `shop` are `source`; `media`,
`schedule`,
`translate`, `reconcile` are `process`), and `skippedSteps` lists the
steps disabled through `SKIP_*`.

A failed run with a retryable failed step can be retried. The run keeps its
steps, sync settings and windows, so no saved definition is needed. A manual
fetch's `fetchWindows` entry is its frozen selection with its `method`; a
stored run frozen without a method still reads as stored, and running it
again fails its fetch step.

`GET /runs/retention` answers `{succeededDays, failedDays, maxDays, pruner}`:
the [retention windows](workflows.md#run-execution) in days (0 keeps those
runs forever), their bound (3650) and the cleanup of this process.
`pruner` is `{available: false}` in a process that does not clean the
history, otherwise `{available, running, intervalHours, lastPrunedAt,
lastDeleted, lastFailed}` for its newest pass since start (`lastPrunedAt`
null before the first). `PUT /runs/retention` accepts `{succeededDays?,
failedDays?}`, each an integer from 0 to 3650; omitted fields keep their
value, and another field or value answers `400 invalid_input`.

## Queue preview

Every step works out its queue when a run starts; nothing stores it. These
read-only routes work a queue out the same way when the console asks, from
the snapshot alone: they write nothing and contact no source or model. Each
answers `{queue}`, and a route whose step the process does not run answers
`503 unavailable`.

| Method and path        | Result                             |
| ---------------------- | ---------------------------------- |
| `GET /queue/schedule`  | `{queue, skipped}`: extraction     |
| `GET /queue/youtube`   | `{queue}`: YouTube detail checks   |
| `GET /queue/translate` | `{queue, skipped}`: translate step |
| `GET /queue/x-refresh` | `{queue}`: X post refresh requests |

`skipped` (in `queue` for YouTube and X) is true while `SKIP_*` turns the
step off for the process.

`GET /queue/schedule` judges every candidate with the scan's own order and
judgement ([Schedule extraction pipeline](sources.md#schedule-extraction-pipeline)).
It reads the post LLM states, post images, their media assets and the stored
extractions once and judges every candidate from them; the run reads them
per post, since it writes between posts. Its size
does not grow with the number of posts: `{generatedAt, disabled,
boardEnabled, messageEnabled, allPosts, limit, postsRead, candidates,
requested, candidateMs, elapsedMs, run, later, cached, backoff,
retryStopped}`. `run` lists, in order, the at most `limit` posts that take a
place in a run started now, each `{postId, source, route, outcome, reason,
inputMode?, publishedAt?, lastAttemptAt?, url?, excerpt}`: `outcome` is
`call` with `reason` `requested`, `new` (never attempted), `changed`
(attempted, with no stored result for its current input) or `backoffDue`,
or `skip` with the skip code (`no_content`, `media_pending`,
`no_route_target`, `no_compatible_provider`, `missing_api_key`). `later`
counts the posts after them that would take a place in later runs; `cached`
counts cache hits, settled posts included, and `backoff` and `retryStopped` are `{count, nextAt?}`,
`nextAt` being the earliest retry. `disabled` is true when both detection
stages are off and no reprocess is requested. The queue targets are read
once per input mode.

`GET /queue/youtube` answers `{generatedAt, skipped, api, apiVideos,
watchPages, limit, counts, entries}`. `entries` is the YouTube step's detail
queue in its order, each ID once (at most `limit`, 1000): operator refresh
requests of the last day, the stored rotating queue (`sync_state`
`youtube-details`), up to 30 active reservations, then due follow-ups, each
`{id, title, url, group, publishedAt?, scheduledAt?, upcoming}` with `group`
`refresh`, `queue`, `active` or `followUp`; `counts` gives each group's size
before de-duplication. A run also queues the videos its listing turns up,
after the rotating queue, which no preview can list. `watchPages` is the
per-run watch-page budget and `apiVideos` the videos the Data API can answer
per run (0 without `YOUTUBE_API_KEY`, `api` false).

`GET /queue/translate` answers `{generatedAt, disabled, reason?, limit,
lookbackDays, candidates}`: the candidates the translate step reads (twice
`limit`, in its order), each `{ownerType, ownerId, source, requested,
excerpt}`. `disabled` with `reason` (`no_route_target`,
`no_compatible_provider`, `missing_api_key`) reports a translation queue
with no usable target.

`GET /queue/x-refresh` answers `{generatedAt, skipped, handles, perAccount,
detailBudget, total, limit, entries}`: up to `limit` (500) posts with an open
refresh request, oldest request first, each `{postId, source, accountHandle,
text, url, publishedAt, requestedAt, status}`. `status` is `next` for the
oldest requests of each account in `handles` (`X_HANDLES`) that the next run
reads, `later` for that account's others, and `notRead` for an account the X
step does not read. `detailBudget` is the per-run X detail request budget the
refreshes lead: the run takes the accounts from its rotation's start, gives
each an even share of the budget left, and reads at most `perAccount` of an
account's refreshes within its share, so an account's `next` requests are
the smallest of its requests, `perAccount` and its share, with the share an
account's refreshes leave passing to the next account.

## Sources

| Method and path                               | Body                                                                               | Result                         |
| --------------------------------------------- | ---------------------------------------------------------------------------------- | ------------------------------ |
| `GET /sources`                                |                                                                                    | per-source status (below)      |
| `PUT /sources/{source}/schedule`              | `{enabled, intervalMinutes, sync, chain}`                                          | `SourceSchedule`               |
| `POST /sources/{source}/schedule/start`       |                                                                                    | `SourceSchedule`               |
| `POST /sources/{source}/run`                  |                                                                                    | `202 {run}`; `409 busy`        |
| `GET /sources/youtube/push`                   |                                                                                    | push status                    |
| `PUT /sources/youtube/push`                   | `{enabled, callbackUrl}`                                                           | push status                    |
| `POST /sources/youtube/push/rotate-secret`    |                                                                                    | push status                    |
| `GET /sources/youtube/live-probe`             |                                                                                    | live probe status              |
| `PUT /sources/youtube/live-probe`             | `{enabled, intervalMinutes, bilibiliEnabled, bilibiliIntervalMinutes}`, any subset | live probe status              |
| `PUT /sources/youtube/live-probe/videos/{id}` | `{stopped}`                                                                        | live probe status              |
| `GET /sources/youtube/ytdlp`                  |                                                                                    | `{available, version}`         |
| `POST /sources/youtube/ytdlp/update`          |                                                                                    | updated `{available, version}` |
| `GET /sync-settings`                          |                                                                                    | `{settings, fields}`           |
| `PUT /sync-settings`                          | any subset of the setting fields                                                   | `{settings, fields}`           |
| `GET /sources/x/related`                      |                                                                                    | related accounts (below)       |
| `PUT /sources/x/related`                      | `{accounts: [{handle, keywords}]}`                                                 | related accounts               |

```json
{
  "schedules": [SourceSchedule],
  "x":       {"skipped", "methods": [FetchMethod], "accounts": [{"account", "oldest"}]},
  "youtube": {"skipped", "methods": [FetchMethod], "oldest", "push": PushStatus, "liveProbe": LiveProbeStatus},
  "bilibili": {"available", "skipped", "methods": [FetchMethod], "oldest", "account", "videos", "signedIn", "login", "loginCheckedAt", "minIntervalMinutes", "lastAttemptAt", "lastSuccessAt", "blockedUntil", "lastError",
               "dynamics": {"posts", "methods": [FetchMethod], "oldest", "lastAttemptAt", "lastSuccessAt", "lastError", "skipped"},
               "live": {"roomId", "liveStatus", "title", "liveTime", "checkedAt", "lastLiveAt", "blockedUntil", "lastError"}},
  "shop": {"skipped", "methods": [FetchMethod], "status": {"sales", "lastAttemptAt", "lastSuccessAt", "blockedUntil", "lastError"}}
}
```

`shop.status` counts the shop's visible sale entries and reports its read
bookkeeping: `blockedUntil` is when a 429 answer's `Retry-After` passes.

The related accounts answer is `{accounts, defaultKeywords, status}`:
`accounts` lists `{handle, keywords}` (see
[Related accounts](sources.md#related-accounts)), `defaultKeywords` the rules
the console offers a new account, and `status` each account's
`{handle, lastSuccessAt, lastError, statuses, promoted}`, from its
`x-related` row and the pool. `PUT` replaces the list: at most 10 accounts,
each a valid handle (a leading `@` is dropped) listed once and not one of
`X_HANDLES`, with up to 30 distinct trimmed keywords of at most 100
characters; anything else answers `400 invalid_input` naming the problem.
Saving evaluates every pooled status against the new rules in the same
transaction, promoting and demoting, and moves the dashboard revision when a
post changed. Neither route contacts X; the next `x_related` run, or a
manual fetch of one account, reads the accounts.

`FetchMethod` describes one manual fetch method in the order the console
offers them: `{id, modes, maxCount, pageSize, requires, refresh, extract,
available}`. `requires` names the environment variable the method needs
(`X_API_BEARER_TOKEN`, `YOUTUBE_API_KEY`, `YTDLP_BINARY` or, for a Bilibili login, `BILIBILI_COOKIE or BILIBILI_SESSDATA`), never its value, and is absent
for public methods; `available` is false while
it, or the method's adapter, is missing. `refresh` and `extract` say which
options the method takes.

`schedules` lists the [source schedules](workflows.md#source-schedules) of
`x`, `x_related`, `youtube`, `bilibili`, `bilibili_dynamics` and `shop`, in
that order.
`SourceSchedule` is `{source,
enabled, intervalMinutes, minIntervalMinutes, maxIntervalMinutes, sync,
chain, nextRunAt, lastRun, skipped}`, plus `reason: "environment"` while `SKIP_*`
disables the source. `sync` is `{mode: "incremental", max}` (default, `max` 50) or `{mode: "count", count}`, each 1–200; `x_related` takes the incremental form only, and `sync` is null for both Bilibili timers and the shop.
`chain` is the source's [method chain](workflows.md#method-chains):
`{methods, fixed}`, where `methods` lists `{id, enabled, available,
requires}` in the order reads try them. `available` is false while the
method's credential is missing, and `requires` names its variable
(`X_API_BEARER_TOKEN`, `YOUTUBE_API_KEY` or `YTDLP_BINARY`) for methods that need one. `fixed`
is true for the related-accounts, Bilibili and shop timers, whose one
method (`timeline` for the related accounts, `list` for uploads, `dynamics`
for the dynamics listing, `new_arrivals` for the shop) cannot change. The X chain adds
`apiGate`, `apiDailyCap`, `apiUsage` (`{day, posts}`, the posts X API
answers returned on the current Japan day) and `timelineFailures`, each
account's current streak (see
[X API in the chain](sources.md#x-api-in-the-chain)).
`lastRun` is `{id, status, at}` of the newest run of the source's chain, from
its timer or Run now
(durable run status; `at` is when it finished, started or was queued), or
null. The `minIntervalMinutes` of both Bilibili timers is
`BILIBILI_MIN_INTERVAL_MINUTES`, and the shop's 180.
`PUT /sources/{source}/schedule` takes any subset of `{enabled,
intervalMinutes, sync, chain}` and keeps omitted fields. `chain` takes any
subset of `{methods, apiGate, apiDailyCap}`: `methods` is `[{id, enabled}]`
in the new order and must list every method of the source once with at
least one enabled; `apiGate` (0–100) and `apiDailyCap` (1–10,000) apply to X
only. It answers `404 not_found` for another source and `400 invalid_input`
for other fields, an interval outside the bounds, an invalid `sync`, a
`count` sync for `x_related` or a `sync` for either Bilibili timer or the
shop, and for a chain with an unknown or repeated method, a missing method,
no enabled method, a gate or cap out of range or for YouTube, or any chain
for the related-accounts, either Bilibili or the shop timer. A method may be enabled while its
credential is missing; reads skip it until it is set. Enabling a
schedule or changing its interval plans the next run one interval later,
with the timer's [random offset](workflows.md#source-schedules); a sync
change alone keeps the plan.

`POST /sources/{source}/schedule/start` starts a timer, as **Start now** on
the overview queue and **Run now** on the Sources page while the timer runs
do: it makes an enabled schedule due at once (a schedule already due keeps
its place) and asks the scheduler to submit the earliest due schedule
immediately. While a run holds the queue the schedule waits as **Run next**
and starts after the current run and the schedules due before it. The
submitted run is the timer's own, so its claim plans the following run (see
[Source schedules](workflows.md#source-schedules)). It answers
`404 not_found` for another source and `400 invalid_input` while `SKIP_*`
disables the source, while timers do not run in this process or while the
schedule is off.

`POST /sources/{source}/run` is **Run now** while the timer is off or
timers do not run in the process: it submits the source's fixed chain once, triggered by the operator and read with the schedule's sync
setting and method chain, and answers `202 {run}` or `409 {error: "busy", run}` while the
queue is occupied. It does not move the schedule's `nextRunAt`. It answers
`404 not_found` for another source and `400 invalid_input` while `SKIP_*`
disables the source.

`PushStatus` is `{available, enabled, callbackUrl, callbackPath,
secretConfigured, verified, leaseExpiresAt, nextRenewalAt, pending,
lastNotificationAt, lastTriggeredAt, lastError}`. The signing secret is never
returned.

`LiveProbeStatus` is `{enabled, intervalMinutes, minIntervalMinutes,
maxIntervalMinutes, available, lastRunAt, nextRunAt, activeVideos,
maxVideos, videos, stopped}`. `available` is false while timers or YouTube
are disabled for the process; `activeVideos` counts the videos the probe is
following, and one run checks at most `maxVideos` of them. `videos` lists
them earliest due first as `{id, title, url, scheduledAt, phase, dueAt}`, and
`stopped` lists the [stopped videos](sources.md#stopped-videos) still stored,
newest stop first, as `{id, title, url, scheduledAt, stoppedAt, reason}` with
`reason` `unavailable` or `operator`. `nextRunAt` is when the next run is
planned (the earliest due video, at least two minutes after `lastRunAt`, and
no earlier than now), or null while nothing is active or the probe is off or
unavailable. `PUT` accepts either field, keeps an omitted one, and rejects
other fields and intervals outside 5–360 minutes with `400`.

`PUT /sources/youtube/live-probe/videos/{id}` takes `{stopped}` (required,
boolean) for one stored YouTube video: `true` stops the probe asking about it,
`false` lets it ask again. It answers the new `LiveProbeStatus`, `404
not_found` for a video that is not stored and `400` for an ID that is not a
YouTube video ID or another body.

`/sync-settings` holds the [sync settings](../operations/configuration.md#sync-settings).
`settings` maps each field (`xMaxStatusRequests`, `xRefreshKnown`,
`xBootstrapDays`, `xDiscoveryLimit`, `youtubeMaxDetailRequests`,
`fetchMaxPages`, `fetchTimeoutMs`, `fetchRequestDelayMs`,
`translationScanLimit`, `translationLookbackDays`, `llmModelAttempts`) to its
integer value, and
`fields` lists `{field, default, min, max}` for each. `PUT` keeps omitted
fields and answers `400 invalid_input` for an unknown field or a value that is
not an integer within its range; the next run reads the saved values.

## Events

| Method and path             | Body / query                                                                                                            | Result            |
| --------------------------- | ----------------------------------------------------------------------------------------------------------------------- | ----------------- |
| `GET /events`               | `page`, `pageSize` (≤ 100), `search`, `provenance`, `source`, `cancellation`, `merge`, `from`, `to`, `order`, `deleted` | list of events    |
| `GET /events/{id}/evidence` |                                                                                                                         | evidence          |
| `POST /events`              | event fields                                                                                                            | `201 {event}`     |
| `PUT /events/{id}`          | event fields                                                                                                            | `{event}`         |
| `POST /events/{id}/confirm` | optional overrides                                                                                                      | `{event}`         |
| `POST /events/{id}/merge`   | `{into}` (event ID)                                                                                                     | `{event}`         |
| `POST /events/{id}/unmerge` |                                                                                                                         | `{event}`         |
| `DELETE /events/{id}`       |                                                                                                                         | `204` (tombstone) |

Each event carries `review`, why it needs an operator's look, or `null`:
`{kind: "suspected", reasons: []}` for a live suspected cancellation, or
`{kind: "uncertain", reasons}` for an uncertain addition, an unconfirmed
automatic entry starting on or after today in Japan whose model confidence
is below 0.7 (`lowConfidence`) or whose status is `tentative`
(`tentative`). Deleted and merged entries need no look. The overview's
review counts and `GET /schedule-attention` follow the same rule.

`provenance` is `automatic`, `manual` or `unconfirmed` (automatic and not
locked by an operator); `cancellation` is `none`,
`llm_suspected` or `manual_confirmed`; `order` is `asc` or `desc` (default);
`deleted=1` includes tombstones and `deleted=only` lists only them. Event fields are `title`, `detail`,
`startsOn`, `startsAt`, `endsAt`, `endsOn`, `timePrecision`, `eventType`, `tag`, `status`,
`url`, `platforms`, `cancellationStatus` and `cancellationReason` (required for
`manual_confirmed`). `eventType` is `event` (the default), `stream`,
`member`, `release`, `appearance` or `sale`. `endsOn` is a sale's close date
and is rejected for any other type; it is not before `startsOn`, `endsAt` then
falls on it in Japan, and a deadline (`endsOn` equal to `startsOn`) may have
`endsAt` without `startsAt`. A sale's `endsAt` requires `endsOn`. Saving
another type clears it. Every write is manual: it locks the event against
automatic extraction, and a delete is a durable tombstone. Confirming keeps
the stored values unless the body overrides them.

`platforms` are the entry's [announced platforms](data-model.md#event-platforms):
an array of `youtube`, `bilibili` and `other`, each at most once, both
`youtube` and `bilibili` for a simulcast; null names none, an update
without the field keeps the stored ones, and any other value answers
`400 invalid_input`. A save writes them as the operator's (not inferred)
under the manual lock. A live-room link (`live.bilibili.com`) as `url`
answers `400 event_live_room_url` unless the platforms include `other`
(someone else's room); the schedule editor turns a pasted room link into
the bilibili platform instead. Confirming an entry whose stored `url` is a
room link without `other` drops that link, as readers already do. Each
event carries `platforms`, its announced platforms as readers see them (a
stored room link without `other` adds `bilibili`), `platformsInferred` as
stored, and `shownPlatforms`, the platforms the public reads show (with
those its merged entries lend it and a simulcast the live room observed);
the list marks the ones it does not announce. The order of `platforms` is
kept: the first is the entry's own platform.

`tag` is the calendar label: `live`, `game`, `talk`, `song` or `video`, an
empty value clears it, and `auto` hands it back to the automatic layers. A
saved label that differs from the stored one, a cleared one included, is an
operator's decision (`tagSource = manual`) and outranks the automatic ones; a
cleared entry shows no label. `auto` stores no label, no `tagSource` and no
attempt, so the entry reads on to its videos' tags and they are asked about
again; on a new entry it is the same as an empty value. The schedule editor
offers it as Automatic and selects it for an entry without an operator's
label;
a save that sends the stored label back unchanged, such as a cancellation
decision, keeps its `tagSource`. The whole row being locked already keeps a
later automatic write from replacing it. The list
reports `tagSource` as `manual`, `llm` (extraction prompt or merged X notice),
`title` (title inference, also set with a null `tag` when the title gave no
label) or null; see [Event labels](data-model.md#event-labels) for their
precedence. `tagSource` is admin-only. An unknown value answers
`400 invalid_input`. Every
reader, including the board, `/api/calendar`, MCP
`dashboard_read` and `schedule_list`, reports the same label, derived from the
stored one and the entry's videos (see
[Shown label](data-model.md#shown-label)); this list shows the stored label.

The list includes merged duplicates by default; `merge=merged` lists only
them and `merge=visible` excludes them, as every public reader does. Each
event carries `mergedInto`, `mergedIntoTitle`, `mergeSource` (`rule` or
`manual`), `mergeReason` and `mergedAt`. `hiddenByPosts` is true for an
automatic event that every public reader leaves out because each of its
source posts is hidden (see
[Events of hidden posts](data-model.md#events-of-hidden-posts)); the console
list keeps it under every filter.

`merge` hides the event behind the event `into` names, on any date, as a
durable operator decision (`mergeSource = manual`, `mergeReason = manual`)
that the reconciler never changes. The target must be another visible event:
itself, a merged or a deleted target, or a missing `into`, answers `400`; a
missing or deleted `{id}` answers `404`. Rule merges into the merged event are
released with it, and an operator's or another owner's merge into it answers
`409 {"error":"event_merge_conflict"}`, so no merge points at a merged event.
Repeating the same merge changes nothing. Deleting the target undoes the
operator merges into it, as `unmerge` would. `unmerge` shows a merged event again
as a durable operator decision (`mergeSource = manual` without a target) that
the reconciler never overrides; an event that is not merged answers `400`.
Editing or confirming a rule-merged event also shows it again, because a
manual event is never merged away; an operator merge stays. Merging,
unmerging and edits advance the dashboard revision.

`evidence` answers what an operator needs to judge an entry, deleted and
merged ones included, or `404 event_not_found`. `event` is the entry as the
list reports it. `posts` are the stored posts it was read from, oldest first,
each a post as `GET /posts` reports it with `roles`: `source` (one of its own
event sources), `merged` (a source of an entry merged into it) and
`cancellation` (the post that suggested its cancellation). `links` are the
`{source, url, role}` of event sources that are not stored posts, such as a
YouTube reservation. `nearby` are up to 20 other posts, nearest first and
listed oldest first, by the same source accounts from a day before the first
evidence post to a day after the last; an entry without a stored evidence
post reads the own X and Bilibili accounts' posts in the 72 hours before it
starts. Stored source JSON never appears.

## Schedule images

| Method and path                     | Body / query                  | Result                                    |
| ----------------------------------- | ----------------------------- | ----------------------------------------- |
| `GET /schedule-images`              | `limit` (≤ 200), `from`, `to` | `{items}`                                 |
| `GET /schedule-attention`           |                               | schedule work by week                     |
| `POST /schedule-images/{id}/review` | `{status, reason}`            | `{image}`; `404 schedule_image_not_found` |
| `PUT /schedule-images/{id}/period`  | `{start, end, reason}`        | `{image}`                                 |

`from` and `to`, both dates, keep the images whose period overlaps them; the
list is ordered by period start, newest first. `periodBasis` is `events` for
a period derived from the source post's events, and `inferredPeriodStart` and
`inferredPeriodEnd` give the period without the manual correction.
`sourcePost` is the stored source post's `text` and `publishedAt`, or `null`;
the board gate's stored search text stays on the server.
Each item also includes `llmText`, the model's visible schedule transcription,
or `null` when the image model has not returned one.

`GET /schedule-attention` is the Review queue's schedule work:
`{today, weeks: [{weekStart, images, events}], undated, truncated}`.
`weeks`, oldest first, hold from Monday `weekStart` the images that wait for
a decision (unlabelled, with a URL, and not settled by the model) whose
period starts in the week, and the events whose `review` is set that start
in it, as `GET /events` reports them; `undated` holds the waiting images
without a period. Each kind reads at most 100 items; `truncated` says one
had more.

`status` is `schedule`, `not_schedule` or `unreviewed` (which clears the
label; the others require `reason`). A `schedule` label is the manual gate
that lets an image reach the image model. Each item's `mediaStatus` is
`ready`, `pending`, `failed`, `missing` (no usable cache file yet) or
`none` (the asset has no image URL, such as the legacy `weekly-schedule`
alias); `none` never needs review and keeps `previewUrl` null.

## Posts

| Method and path                  | Body / query                                                                                            | Result                           |
| -------------------------------- | ------------------------------------------------------------------------------------------------------- | -------------------------------- |
| `GET /posts`                     | `page`, `pageSize` (≤ 200), `source`, `account`, `from`, `to`, `search`, `llm`, `queued`, `translation` | list of posts                    |
| `PUT /posts/{id}/hidden`         | `{hidden}`                                                                                              | `{state}`                        |
| `POST /posts/{id}/reprocess`     | `{route?}`                                                                                              | `{state}`                        |
| `POST /posts/{id}/dismiss`       | none                                                                                                    | `{state}`; `409 post_not_failed` |
| `POST /posts/reprocess`          | `{ids}`                                                                                                 | `{queued}`                       |
| `POST /posts/dismiss`            | `{ids}`                                                                                                 | `{dismissed}`                    |
| `PUT /posts/{id}/schedule-image` | `{index, reason, start?, end?}`                                                                         | `{image}`                        |
| `PUT /posts/{id}/quote/hidden`   | `{hidden}`                                                                                              | `{state}`                        |
| `POST /posts/{id}/refresh`       | none                                                                                                    | `{state}`                        |

Each post reports `promotedAt`, when a related account's status was promoted
into posts, or null for an own post; External posts holds its decisions.
`account` is an X handle, matched without case or a leading `@`; `llm` is `never`, `pending`, `processed`, `failed`,
`dismissed`, `skipped` or `uncertain`; `queued` keeps the posts with an open
operator request: `extraction` (queued for re-extraction), `refresh` (a
refresh from the source), `translation` (a machine translation) or `any` of
them;
`translation` is `incomplete`, `complete`, `requested` or `failed` (the
machine pass of the current wording failed and a locale still lacks a usable
translation).

`from` and `to` are inclusive Asia/Tokyo publication dates. Search matches
text, URL, image alt text and the stored post ID before pagination, so older
matching posts remain reachable. The console preserves these filters while
paging and resets to page one when a filter changes.

A post whose extraction keeps failing carries `llmRetryAt`, the earliest time
the automatic scan tries it again, or `llmRetryStopped: true` once the scan
no longer retries its current input (see
[Failure backoff](sources.md#extraction-failure-backoff)). Both stay
`null`/`false` for other posts and for a post with a queued reprocess request,
which runs regardless. `POST /posts/{id}/reprocess` clears the backoff.
`POST /posts/{id}/dismiss` turns a `failed` extraction into `dismissed` and
stops its automatic retries (see
[Failure backoff](sources.md#extraction-failure-backoff)); any other state
answers `409 post_not_failed`.

`POST /posts/reprocess` and `POST /posts/dismiss` apply the same changes to
1 to 200 listed posts in one transaction. They skip unknown IDs and, for a
dismissal, posts whose extraction has not failed, and answer how many posts
they queued or dismissed; a reprocess keeps each stored route.

`scheduleImage` is the post's schedule image record, or `null`: its asset
`id`, the `index` of the post's own image it shows (`null` for none of them),
`llmStatus`, `manualStatus` and `picked`. `PUT /posts/{id}/schedule-image`
makes the X post's image at `index` (in `media` order) its schedule image,
labelled `schedule` with the required `reason` (see
[Schedule images](sources.md#schedule-images)). `start` and `end` set its
manual period and come together; without them the stored manual period stays.
An unknown or non-X post answers `404 post_not_found`, and an index without
an image `400`.

An X post carries `translation` (see [Translations](#translations)); other
posts carry `null`. `GET /videos` does the same for YouTube videos.

Each post's `media` lists its images in order (linked post images, or else
the stored media URL) as `{url, status, alt}`. `url` is the local
`/media/<id>?v=<sha256>` URL of the cached file, or `null` until that file is
ready; the remote address stays in `mediaUrl` and is never shown. `status` is
`ready`, `pending`, `failed` or `missing`. `mediaStatus` repeats the first
image's status, or is `none` for a text-only post.

Each post also carries `quote`, the stored quoted status read-only (see
[Quoted posts](data-model.md#quoted-posts)), or `null`:
`{id, authorHandle, authorName, text, publishedAt, url, unavailable, media}`.
A Bilibili forward's quote is the dynamic it forwards, with its author's mid
as `authorHandle` and an empty `id` when the listing did not name it.
Its `media` has the `{url, status, alt}` shape but lists only ready cached
quote images, never the post's own. `quoteHiddenAt` is when an operator hid
the quote from the public pages, `hiddenAt` when an operator hid the whole
post, and `refreshRequestedAt` is a queued refresh from the source; each is
`null` otherwise. The console never edits
the quoted wording, author or images.

`PUT /posts/{id}/hidden` with `{"hidden": true}` hides the whole post from the
dashboard, the post archive and MCP; `false` shows it again. A hidden post is
never selected for extraction, and an operator's reprocess request for it
waits until it is shown again (the extraction backlog does not count it). The
automatic events it alone supports leave the public pages while it is hidden
(see [Events of hidden posts](data-model.md#events-of-hidden-posts)). It
follows the quote rules below, except that any stored post can be hidden.

`PUT /posts/{id}/quote/hidden` with `{"hidden": true}` hides the post's
quote from the dashboard, the post archive and MCP; `false` shows it again.
`hidden` must be a JSON boolean. Hiding again keeps the first decision's
time. A post without a stored quote answers `404 quote_not_found`.
Synchronization never writes the decision, so it outlives every rewrite of
the stored quote. A change increments the dashboard revision.

`POST /posts/{id}/refresh` queues a stored X post for a detail request in
the next X synchronization and removes its pending failure backoff; it
contacts no source itself. A repeated request keeps the first request's
place. Posts that are not X statuses with a known account answer
`400 invalid_input`. See
[Refresh from the source](sources.md#refresh-from-the-source). The three
routes answer `{state: {postId, hiddenAt, quoteHiddenAt, refreshRequestedAt}}`;
MCP control has none of them.

## External posts

| Method and path                 | Body / query                                                      | Result           |
| ------------------------------- | ----------------------------------------------------------------- | ---------------- |
| `GET /x-statuses`               | `page`, `pageSize` (≤ 100), `author`, `reason`, `state`, `search` | list of statuses |
| `PUT /x-statuses/{id}/decision` | `{decision, reason}`                                              | `{status}`       |
| `GET /bilibili-statuses`        | `page`, `pageSize` (≤ 100), `author`, `search`, `kind`            | list of dynamics |

The list pages the X status pool, newest status first (see
[Related accounts](sources.md#related-accounts)), without the statuses of
the own accounts (`X_HANDLES`) or any own post the pool holds as a quote;
those belong to Posts. `author` is a handle,
matched without case or a leading `@`; `reason` is `keyword`, `manual`,
`quoted`, `reposted` or `none`; `state` is `promoted` (a shown promoted
post), `demoted` (a hidden one) or `stored` (pool only); `search` matches
the text or the status ID. Each
status is `{id, authorHandle, authorName, text, publishedAt, url, images,
reasons, state, related, decision, decisionReason, decidedAt, quotedAt,
repostedAt, listedAt, firstSeenAt, quotedBy, repostedBy, translation}`:
`images` counts its pooled image addresses, `related` says its author is a
related account, `quotedBy` lists up to five own posts quoting it, newest
first, `repostedBy` the own accounts that reposted it, and `translation` is
its `x_status` translation summary (see [Translations](#translations)), or
null for a status whose post owns the translations.

`decision` is `promote` or `demote`, which need a `reason` of at most 500
characters and become a durable lock, or `automatic`, which removes the lock
so the keyword rules decide again. A promote needs a related account's
status, writes or shows its post and answers the status; a demote hides the
promoted post. An own post answers `400 invalid_input`, a missing status
`404 not_found`. A change moves the dashboard revision. Translations of a
pooled status use the [translation routes](#translations) with owner
`x_status`.

`GET /bilibili-statuses` pages the Bilibili status pool (see
[Forwards](sources.md#forwards)), newest dynamic first, without a dynamic
that is a stored post (a forward of an own dynamic), whose translations
belong to Posts. `author` is an exact mid or part of the author's name,
matched without case; `search` matches the text or the dynamic or post ID;
`kind` is `dynamic` or `video` (an upload, whose URL is its video page), and
any other value answers `400 invalid_input`. Each dynamic is `{id,
authorMid, authorName, text, publishedAt, url, kind, images, reasons,
forwardedAt, firstSeenAt, forwardedBy, shown, translation}`: `id` is its
post ID, `images` counts its pooled image addresses, `reasons` is
`["forwarded"]`, `forwardedBy` lists up to five own posts forwarding it,
newest first, as `{postId, hidden, quoteHidden, quoteAvailable}`, and
`translation` is its `bilibili_status` translation summary. `shown` is
`public` when a shown forward shows it with its quote shown, `hidden` when
every forward that shows it available is hidden or hides its quote, and
`gone` when no forward shows it available. The pool has no decision route:
a quoted status appears inside its quoting post and never as a post of its
own ([related accounts](../architecture/sources.md#related-accounts)),
and the forward's quote already shows the dynamic. Its translations use the
translation routes with owner `bilibili_status`.

## Videos

| Method and path              | Body / query                                                                                                    | Result                                       |
| ---------------------------- | --------------------------------------------------------------------------------------------------------------- | -------------------------------------------- |
| `GET /videos`                | `page`, `pageSize` (≤ 200), `status`, `kind`, `thumbnail`, `source`, `search`, `merge`, `translation`, `queued` | list plus `featuredVideoId`, `featuredVideo` |
| `PUT /videos/featured`       | `{videoId}` (`null` restores automatic selection)                                                               | `{featuredVideoId}`                          |
| `GET /videos/{id}/history`   |                                                                                                                 | `{video, revisions, thumbnails}`             |
| `PUT /videos/{id}`           | `{title?, kind?, tag?, availability?, thumbnail?, isShort?, work?}`                                             | `{video}`                                    |
| `POST /videos/{id}/refresh`  |                                                                                                                 | `202 {video, started, run}`                  |
| `POST /videos/bilibili`      | `{input}` (BV ID or bilibili.com video link)                                                                    | `{video, started, run}`                      |
| `POST /videos/youtube`       | `{input}` (YouTube video ID or link)                                                                            | `{video, started, run}`                      |
| `GET /videos/merges`         |                                                                                                                 | `{items}` merge suggestions                  |
| `GET /videos/forms`          |                                                                                                                 | `{items}` Bilibili form decisions            |
| `GET /videos/works-recent`   |                                                                                                                 | `{updatedAt, items}`                         |
| `GET /videos/{id}/merges`    |                                                                                                                 | `{items}` for one Bilibili upload            |
| `POST /videos/{id}/merge`    | `{into}` (YouTube video ID or watch link)                                                                       | `{video}`                                    |
| `POST /videos/{id}/unmerge`  |                                                                                                                 | `{video}`                                    |
| `POST /videos/{id}/separate` |                                                                                                                 | `{video}`                                    |

Each video's `thumbnailUrl` is the local `/media/<id>?v=<sha256>` URL of its
cached thumbnail, or `null` until that file is ready; the remote CDN address is
never returned. `thumbnailStatus` is `ready`, `pending`, `failed` or `missing`.

`featuredVideo` is the pinned video in the same shape, whatever page or filter
is listed, or `null` while the board chooses automatically.

Changing the featured video advances the dashboard revision.

`status` is `published`, `upcoming` or `live`; `kind` is the shown form,
`stream`, `premiere`, `upload` or `unknown`; `thumbnail` is `ready`,
`present` or `missing`; `source` is `youtube` or `bilibili`. `search` matches
the shown title, the platform title or the video ID. `merge=visible` excludes
uploads merged into another video and `merge=merged` lists only them;
`translation` is `incomplete`, `complete` or `requested`, as for posts, over
the videos the translate step covers. `queued` keeps the videos with an open
operator request: `refresh` (a YouTube refresh request made within the last
day, which the YouTube step still takes first), `translation` (a machine
translation) or `any` of them.

### YouTube videos

`POST /videos/youtube` reads one video of the configured channel now and
stores it as a manual fetch stores a listed video and its details (see
[YouTube methods](workflows.md#youtube-methods)), so manual locks and
deletion tombstones hold; a stored video is read again and keeps a replaced
title as history. The input is a video ID or a `youtube.com` watch,
`/shorts/`, `/live/` or `/embed/` link or a `youtu.be` link. With
`YOUTUBE_API_KEY` the Data API describes it under the recent step's rules
(one `videos.list` call, plus the newest page of each format playlist for an
ended broadcast that needs a verdict); without one, or when the answer
cannot classify an ended broadcast, its watch page does. It then starts the
`media` step for the thumbnail (`started` is false while another run holds
the queue). Another input or field, a video of another channel, or a
process that skips YouTube answers `400 invalid_input`; a video YouTube does
not show publicly answers `404 {"error":"video_not_found", code}` with the
watch page's `youtube_watch_*` code, and other upstream failures `502` with
their `youtube_*` code.

### Bilibili uploads

`POST /videos/bilibili` reads one upload of the configured Bilibili account
now and stores it; `POST /videos/{id}/refresh` on a Bilibili video reads it
again the same way and answers `200`. Both start the `media` step for the
cover (`started` is false while another run holds the queue). An input that
is neither a BV ID nor a `bilibili.com` video link, or an upload of another
account, answers `400`; a video Bilibili does not list answers `404`. A
captcha answer, and any later read while the backoff lasts, answers `503
{"error":"bilibili_blocked", code, blockedUntil, force: true, message}`,
where `code` names the captcha answer or `bilibili_blocked` for the waiting
backoff; nothing is sent while it waits. Both reads accept `{force: true}`
in the body, which sends that one read anyway: a success lifts the backoff
and a failure extends it. Other upstream failures answer `502` with their
`bilibili_*` code. `bilibili` in
`GET /sources` reports the uploads' manual fetch `methods` and the
`oldest` stored upload's publication time, the account, the stored uploads, whether a login cookie
is configured (`signedIn`, never the cookie), `login` (`signed_in`,
`expired`, `unchecked` or `not_configured`) with `loginCheckedAt`, the read
interval and the
latest attempt, success, backoff and error. Its `dynamics` reports the stored
dynamics, their manual fetch `methods` and `oldest` stored dynamic, and the listing's latest attempt, success and error
(`bilibili_login_expired` when the cookie no longer signs in), and `skipped`:
`login_required` without a cookie, `login_expired` while the latest login
check found it expired, `blocked` during the backoff, otherwise null. Its `live` is the room as the last check saw it (`liveStatus` 0 off
air, 1 live, 2 rotation; `roomId` null until the first check) and
`lastLiveAt`, the last check that saw it live: the approximate end of the
last broadcast once the room is off air.

`POST /posts/bilibili` with `{input}`, a `bilibili.com/opus/<id>` or
`t.bilibili.com/<id>` link, reads that dynamic of the configured account from
its opus page now, stores it and starts the `media` step for its pictures,
answering `{post, started, run}` with the post in the list shape. Another
input or a dynamic of another account answers `400 invalid_input`, a
video or another non-post dynamic `400 {"error":"bilibili_unsupported_dynamic"}`,
one Bilibili does not show `404 {"error":"post_not_found"}`, and a captcha,
`force` and other upstream failures work as for uploads. The read takes the
dynamic as its page shows it, with no quote: forwards and their quotes come
from the listing, and the read never rewrites a stored forward (see
[Sources: Bilibili dynamics](sources.md#dynamics)).

The live probe status adds `bilibili`: `{enabled, intervalMinutes,
minIntervalMinutes, maxIntervalMinutes, available, nextCheckAt, targets}`,
each target `{eventId, title, startsAt, phase, dueAt}` with `phase` one of
the YouTube phases or `baseline`.

### Video merging

A Bilibili upload merged into the YouTube video of the same work (see
[Video merging](data-model.md#video-merging)) reports `mergedInto`,
`mergedIntoTitle`, `mergeSource` (`rule` or `manual`), `mergeReason` and
`mergedAt`; `mergeSource: "manual"` with a null `mergedInto` is an undone
merge. Every video lists the uploads merged into it in `alternates`
(`{id, source, title, url, mergeSource, mergeReason, mergedAt}`).

`POST /videos/{id}/merge` merges a Bilibili upload into the YouTube video
`into` names, by ID or `youtube.com`/`youtu.be` link, as an operator decision
the rule never changes. A rule merge of another upload into that video is
released; another operator merge into it answers
`409 {"error":"merge_conflict"}`. An `{id}` that is not a Bilibili upload,
or an `into` that is not a stored YouTube video, answers `400`; an unknown
`{id}` answers `404`. `POST /videos/{id}/unmerge` undoes a merge, rule or
manual, and keeps the upload separate (`mergeReason: "unmerged_from:<id>"`);
an upload that is not merged answers `400`. Both advance the dashboard
revision and write or remove the target's Bilibili translation in the same
transaction. `POST /videos/{id}/separate` keeps an unmerged Bilibili upload
apart (`mergeSource: "manual"`, `mergeReason: "kept_separate"`), so the rule
never merges it; the board is unchanged, so the revision stays. A merged
upload answers `409 {"error":"merge_conflict"}` (undo it instead), an
`{id}` that is not a Bilibili upload `400` and an unknown one `404`.

`GET /videos/merges` lists, newest first and at most 50, the unmerged
Bilibili uploads the rule would merge (`status: "rule"`), or leaves to the
operator (`ambiguous`, or `unverified` when no candidate satisfies every
rule, or when only a video published more than six hours apart matches the
song title and length), without writing; the overview's `review.videoMerges` counts the
`ambiguous` and `unverified` uploads without the limit. Uploads an operator
kept separate are left out.
Each item has `bilibili` (the upload as listed) and up to five `candidates`:
`{youtube, complete, titleMatch, broadcast, durationDelta, gapMinutes}`,
where `durationDelta` is null when a length is unknown and `broadcast` marks
the stream archive of the broadcast a live replay replays (the rule's only
candidate for it). `GET /videos/{id}/merges`
describes one upload whatever its state, adding `merged`, `separate` and
`none`.

`GET /videos/works-conflicts` lists the stored Bilibili uploads whose merge
disagrees with the packaged Works list: the list pairs the upload with other
YouTube videos than the one it is merged into, or with stored ones while it
is unmerged. Each item is `{upload, bilibili, mergedInto, worksYoutube}`. The
list only reports; the Review queue's merge decisions show the items.

`GET /videos/works-recent` lists, newest first, at most 200 of the videos
dated after the packaged Works list's `updatedAt` (also returned) that the
Works page shows, and those the operator's Works decision keeps out, with
`total` counting them all, under the rules in
[Uploads after the collection date](../development/works-archive.md#uploads-after-the-collection-date).
Each item is `{id, source, title, url, publishedAt, scheduledAt, kind, tag,
isShort, availability, work, origin, thumbnailUrl}`: `work` is the decision
(`true`, `false` or `null`), `origin` is `auto` or `operator` for a listed
video and `null` for an excluded one, and `thumbnailUrl` is the local URL of
the ready cached thumbnail or `null`. It reads the snapshot only. The Site
page lists them, excludes an automatic one and returns a decided one to the
rules; the video editor's Works page field sets the decision on any video.

### Bilibili form

Each video reports `formSource`: `manual` when `manualKind` is set, `rule` or
`llm` for a Bilibili upload whose form a rule or the model settled, and
`null` for an unsettled upload or a YouTube video's platform facts (see
[Bilibili form](data-model.md#bilibili-form)). `formReview` is `uncertain`
or `failed` while an unsettled upload waits for the operator, and `public`
says whether public readers list the video. `PUT /videos/{id}` with
`{kind}` answers the form; a Bilibili upload accepts only `upload` or
`stream`, and anything else answers `400`.

`GET /videos/forms` lists, newest first and at most 100, the unmerged
Bilibili uploads waiting for the operator's form decision that nobody marked
private or removed. Each item is `{video, review, broadcast}`: `review` is
`uncertain` or `failed`, and `broadcast` is the broadcast the replay rule
read, `{source, eventId, title, startsAt, endsAt}` with `source` `entry` or
`room`, or `null`. The overview's `review.bilibiliForms` counts them without
the limit.

### Video corrections and history

An operator correction replaces a platform value on the board without
changing it: `title`, `kind`, `isShort`, `availability` and `thumbnailUrl`
show the correction, while `sourceTitle`, `sourceKind`, `sourceIsShort` and
`sourceAvailability` keep what the platform reported and `manualTitle`,
`manualKind`, `manualIsShort`, `manualAvailability` and `manualThumbnail`
(`{mediaId, sha256}`) are the corrections themselves. `manualWork` is the
operator's Works decision (`true` includes the video among the Works page's
uploads after the collection date, `false` excludes it, `null` leaves it to
the rules).
`tag` is the shown content tag: `manualTag`, else `scheduleTag` (the label of
the video's schedule entry), else `inferredTag` (the title inference's), with
`inferredTagSource` set once the inference judged the video, with or without
a tag (see [Video form and tags](data-model.md#video-form-and-tags)).
`PUT /videos/{id}` sets a field from a string, for `isShort` from a boolean
(`true` for a Short, `false` for any other upload), for `work` from a
boolean (`true` includes, `false` excludes), or for `thumbnail` from
one `{mediaId, sha256}` file listed in the history; `null` clears it and an
omitted field keeps it. A title is 1–300 characters after trimming; a kind is
`stream`, `premiere` or `upload`, a tag `game`, `talk` or `song`, and an
availability `public`, `unlisted`, `members`, `private` or `removed`, compared
without case. Other fields,
unknown files and invalid values answer `400`, an unknown video `404`. A
change advances the dashboard revision, and machine translations follow the
corrected title. The feed names the format of its newest uploads only (see
[Sources](sources.md#shorts)), so `sourceIsShort` is `null` for older ones and
the console's Format field is how an operator moves one into the board's
Shorts row.

`GET /videos/{id}/history` lists, newest first, the titles and thumbnails the
platform replaced (`revisions`, each with `origin` `sync` for a new title or
thumbnail URL and `media` for new bytes at the same URL) and the cached
`thumbnails` an operator may pin: the platform's current file (`current`),
earlier files of the same URL and files of earlier URLs. `selected` marks the
pinned one. Every URL is a local `/media/<id>?v=<sha256>` address.

`POST /videos/{id}/refresh` records `refreshRequestedAt` on a YouTube video
and starts the `youtube` and `media` steps; the YouTube step reads requested
pages before any other detail work. `started` is false while another run
holds the queue, and the request then waits up to 24 hours for the next run
that reads YouTube. A successful page read clears it. Each video also reports
`durationSeconds`, `checkedAt` (its last successful page read) and its number
of `revisions`.

## Profile media

| Method and path                     | Body                         | Result                      |
| ----------------------------------- | ---------------------------- | --------------------------- |
| `GET /profile-media`                |                              | `{items, active}`           |
| `POST /profile-media`               | `{source, sourceUrl, slot?}` | `201 {item, items, active}` |
| `POST /profile-media/discover`      | `{source, slot?}`            | discovery result            |
| `POST /profile-media/upload?slot=`  | raw image body (`slot?`)     | `201 {item, items, active}` |
| `GET /profile-media/{id}/preview`   |                              | image bytes (ready only)    |
| `POST /profile-media/{id}/download` |                              | `{item, items, active}`     |
| `POST /profile-media/{id}/select`   | `{slot?}`                    | `{item, items, active}`     |
| `DELETE /profile-media/{id}`        |                              | `{items, active}`           |

`source` is `x` or `youtube`, and `slot` is `avatar` or `banner`. A candidate's
`slot` is the slot it is active for, or otherwise the one that found it; an
added or uploaded candidate without one starts as an avatar. A source URL is
one candidate whichever slot finds it. Discovery without a slot records the
avatar and the banner the profile page names, and its result then has no
`slot`. Selection uses the given slot, or the candidate's own, and moves the
candidate there; the slot it leaves falls back to the built-in image.
Deleting the active candidate falls back to the built-in image, an unknown ID
answers `404 profile_media_not_found`, and a later discovery may record a
deleted URL again. Files follow
[Selected Profile Media](media-cache.md#selected-profile-media).

## Translations

| Method and path                              | Body                                                         | Result                    |
| -------------------------------------------- | ------------------------------------------------------------ | ------------------------- |
| `GET /translations`                          |                                                              | `{counts, locales}`       |
| `POST /translations/request`                 | `{missing: true, ownerType?, source?}` or `{ownerType, ids}` | `202 {queued, run, busy}` |
| `PUT /translations/{owner}/{id}/{locale}`    | `{text}`                                                     | `{translation}`           |
| `DELETE /translations/{owner}/{id}/{locale}` |                                                              | `{translation}`           |

`owner` and `ownerType` are `post`, `video`, `x_status`, a pooled X status
by its status ID, or `bilibili_status`, a pooled forwarded dynamic by its
post ID (see [Translations](data-model.md#translations)); `locale`
is `en`, `ja` or `zh-CN`. `counts` maps `post` and `video` to `{total, incomplete,
requested, sources}` over the posts and videos the translate step covers;
`sources` holds the same totals per source (`x` and `bilibili` for posts,
`youtube` and `bilibili` for videos), which the console's platform tabs show. A translation summary is
`{status, sourceLanguage, machineStatus, lastAttemptAt, lastError, requested,
locales}` with `status` `complete` or `incomplete`, `machineStatus` `never`,
`success`, `failed`, `skipped` or `outdated` (the wording changed after the
last attempt), and one entry per locale: `{locale, status, origin, platform,
text, manual, manualStale, machine, machineAt, model}`. A locale's `status` is
`translated`, `missing`, `stale` (only an outdated machine row), `source`
(already in that language) or `not_needed`; `text` and `origin` are what
readers see.

Translation `lastError` distinguishes `output_truncated`,
`no_structured_output`, `missing_translation_en`,
`missing_translation_zh_cn`, `invalid_translation` and `model_refused`,
alongside the generic request categories (for example `timeout` or
`authentication`). It never contains upstream response wording. A run's
`run.counts.translate.diagnostics` records bounded, sanitized per-attempt
metadata; see [Translation](sources.md#translation).

`PUT` stores a manual translation (trimmed, at most 8000 characters) that
outranks platform and machine rows; `DELETE` removes it. Both answer the
owner's new summary and `404 not_found` for an unknown owner. `POST
/translations/request` queues forced machine passes for the listed owners
(at most 200), or with `missing: true` for every owner of `ownerType`, or
every post and video without one, lacking a usable translation, whatever
its age; `source` keeps such a request to one of the owner type's sources
and needs `ownerType` (any other pairing answers `400`). Owners outside the translate step's scope, such as a pooled status
that is a stored post, are not queued. When it
queued any and the run queue is free it starts a `translate` run; `busy: true`
means the requests wait for the next translate step.

## Extraction and models

| Method and path                          | Body                                                                                       | Result                                                                       |
| ---------------------------------------- | ------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------- |
| `GET /extraction`                        |                                                                                            | extraction settings                                                          |
| `PUT /extraction`                        | any subset of `{keywordEnabled, visionEnabled, messageEnabled, messageAllPosts, keywords}` | extraction settings                                                          |
| `GET /llm`                               |                                                                                            | `{providers, models, queue, verificationQueue, translationQueue, modelTags}` |
| `GET /llm/failures`                      |                                                                                            | `{items}` newest 30 failures                                                 |
| `POST /llm/providers`                    | provider fields                                                                            | `201 {provider}`                                                             |
| `PUT /llm/providers/{id}`                | provider fields                                                                            | `{provider}`                                                                 |
| `DELETE /llm/providers/{id}`             |                                                                                            | `204`                                                                        |
| `POST /llm/providers/{id}/test`          | `{model?}`                                                                                 | `{ok, method, model, remoteModelCount, provider}`                            |
| `POST /llm/providers/{id}/discover`      |                                                                                            | `{provider, models}` remote catalog                                          |
| `POST /llm/providers/{id}/models`        | `{models, origin?}`                                                                        | `201 {provider, models}`                                                     |
| `PUT /llm/providers/{id}/models`         | one model `{id, ...}`                                                                      | `{provider, model, models}`                                                  |
| `POST /llm/providers/{id}/models/remove` | `{ids}`                                                                                    | `{provider, models, removed}`                                                |
| `PUT /llm/queue`                         | `{route?, targets: [{providerId, modelId}]}`                                               | `{route, queue}`                                                             |

Extraction settings are `{enabled, keywordEnabled, visionEnabled,
messageEnabled, messageAllPosts, keywords}`; `enabled` is derived: the message
stage alone, or keyword candidates plus boards. `messageAllPosts` makes the
message stage select every post instead of only heuristic matches. `queue` is
the ordered `[{providerId, modelId}]` text schedule extraction list; every
target names a catalog model (`modelId` is required), and a save replaces the
whole list. `route` selects the queue a save replaces: `schedule` (the
default) for text extraction, `verify` for image schedule extraction and
schedule-image review (targets must carry the `image` tag), or `translate` for
translation and other short text tasks (targets must carry the `text` tag).
Removing a model removes its targets from every queue. Providers have no default model: a provider response
carries no `model` field, a `model` field in a provider write is ignored, and a
provider test without `{model}` only lists the provider's models. Creating a
provider or adding catalog models never queues anything. Model IDs may
contain `/`, so model writes name the model in the body. Provider responses
report `apiKeyConfigured` only; keys are write-only. A provider write that
changes `baseUrl` while a key is stored must carry `apiKey`, otherwise it is
`400 invalid_input`, so a stored key is only sent to the address it was
entered for.

A provider `baseUrl` may name a model server on the local network: a private
or shared (carrier-grade NAT) IPv4 address, a unique-local IPv6 address, or a
name ending in `.local`, `.lan`, `.internal` or `.home.arpa`, such as
`host.docker.internal`. Production accepts plain HTTP only for these
local-network addresses; every other address must use HTTPS. This machine
(`localhost`, `.localhost` and loopback addresses), link-local addresses
(including cloud metadata endpoints), multicast and the other
special-purpose ranges are refused. Names are not resolved, so a public name
is accepted wherever it points; only the operator sets provider addresses.

`GET /llm/failures` lists the 30 newest stored model failures, newest first:
failed schedule extractions (one per input and queue target), failed image
extractions and schedule-image checks, and owners whose last machine translation failed. A
stopped run (`LLM request cancelled`) is not listed. Each item is `{route,
subject, subjectId, url, providerId, model, message, at}`: `route` is
`schedule`, `verify` or `translate`; `subject` is `post`, `image` (a schedule
image, with its post's `url`) or `video`; `providerId` is known for
extractions and `model` for extractions and image checks; `message` is the
stored fixed message, never upstream wording; `at` is when the call failed.

## Outbound proxies

| Method and path         | Body                                                              | Result                                                |
| ----------------------- | ----------------------------------------------------------------- | ----------------------------------------------------- |
| `GET /proxy`            |                                                                   | `{proxies, routes, directFallback, areas, testAreas}` |
| `PUT /proxy`            | any subset of `{proxies, routes, directFallback}`                 | the same settings                                     |
| `POST /proxy/{id}/test` | `{area?}` (`x`, `youtube` (default), `bilibili`, `shop`, `media`) | `{ok, status?, error?, latencyMs, proxy}`             |

`proxies` is the ordered queue; each proxy is `{id, name, scheme, host, port,
username, hasPassword, passwordUnreadable, enabled, health}`, where `health`
is `{lastSuccessAt, lastFailureAt, coolingUntil}` from this process. A save
replaces each field it carries: `proxies` replaces the whole queue (a proxy
without `id` is new), `routes` maps every area (`x`, `youtube`,
`bilibili`, `shop`, `media`, `llm`) to `{enabled, proxyIds}`. In a proxy write,
an omitted `password` keeps the stored password of the proxy with the same
`id` while its scheme, host and port stay the same (a changed one is
`400 invalid_input` until a password is sent), `""` clears it, and a
username-less proxy has none. Passwords are
write-only. Selections drop unknown ids; an unknown area, a route turned on
without any proxy, or an invalid protocol, address or port is
`400 invalid_input`. Unparseable stored settings answer reads with
`503 proxy_settings_unavailable`; a save replaces them. Storing a password
without `LLM_SECRETS_KEY` is `503 secrets_key_unavailable`. A test reports a
failed attempt as `200 {ok: false, error: "timeout" | "failed"}`; an unknown
proxy is `404 proxy_not_found`. See [Outbound proxies](outbound-proxy.md).

## Related Docs

- [Backend](backend.md)
- [Frontend](frontend.md)
- [Workflows](workflows.md)
- [Secure development](../development/security.md)
