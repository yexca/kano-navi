# Runs and Queue Preview

Starting and reading runs, and the queue preview.

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
  [copies run](../media-cache/resized-copies.md#copies-run). A process without the service's
  data directory and built site answers `400 invalid_input`.
- `{"kind": "workflow", "steps": ["x", "translate"]}`: a one-off step run.
  Known catalog steps run once in catalog order; an empty list or another
  value is `400 invalid_input`. Source steps read as their
  [source schedule](sources.md#sources) does.
- `{"kind": "fetch", "source": "x" | "x_related" | "youtube" | "bilibili" | "bilibili_dynamics" | "shop", "method": Method, "window":
Selection, "options"?: {"extract"?, "translate"?, "refresh"?}}`: one
  source plus media for a
  [manual fetch](../workflows/manual-fetches.md)
  through the one method it names. `Method` is `timeline` or `api` for X,
  `timeline` for a related account, `api`, `rss` or `ytdlp` for YouTube,
  `space` for the Bilibili uploads and dynamics, and `catalog` for the shop, whose
  fetch is the `shop` step alone, with `{mode: "all"}` and no option. `Selection` is `{mode: "count", count}`
  (1 to the method's `maxCount`), `{mode: "all"}`, `{mode: "range",
startDate, endDate}` or `{mode: "before", days}` (1–365), as the method's
  `modes` allow. An X selection may add `account`, one of `X_HANDLES`, to
  read that account alone. A related account's selection is
  `{mode: "incremental", account}`, where `account` is one of the
  [related accounts](sources.md#sources). Either handle may carry a leading `@` and
  any letter case, and the run freezes its configured spelling; another
  account answers `400 invalid_input`, as does `account` on a YouTube,
  Bilibili or shop fetch. `extract` (X, related-account and Bilibili
  dynamics methods) adds the `schedule` step,
  `translate` the `translate` step, and `refresh` (YouTube methods)
  rewrites stored videos the listing names again; each is a boolean and
  defaults to false. An unknown method, a mode or count the method does not
  serve, an option it does not take, `extract` on a `range` that ends before
  the extraction lookback, or a method that cannot run now (its credential
  is missing) answers `400 invalid_input`; the fetch never falls back to
  another method.

A busy queue answers `409` with the active run. There are no saved workflows
([source schedules](../workflows/source-schedules.md#source-schedules)): a
`workflowId` in the body answers `400 invalid_input`, and there are no
`/workflows` routes (`404 not_found`).

`GET /runs/steps` is the step catalog `[{id, group}]` in execution order
(`x`, `x_related`, `youtube`, `bilibili`, `bilibili_dynamics`, `shop` are `source`; `media`,
`schedule`,
`translate`, `reconcile` are `process`), and `skippedSteps` lists the
steps disabled through `SKIP_*`.

A failed run with a retryable failed step can be retried;
[Run execution](../workflows/run-execution.md) names the steps that run again.
The run keeps its steps, sync settings and windows, so no saved definition is
needed. A manual
fetch's `fetchWindows` entry is its frozen selection with its `method`; a
stored run frozen without a method still reads as stored, and running it
again fails its fetch step.

`GET /runs/retention` answers `{succeededDays, failedDays, maxDays, pruner}`:
the [retention windows](../workflows/run-execution.md) in days (0 keeps those
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
the snapshot alone: they write nothing and contact no source or model. A
preview holds as of its read: posts synced, edited or requested, and model
queue changes, before a run starts change what that run takes. Each
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
judgement ([Schedule extraction pipeline](../sources/extraction.md)).
It reads the post LLM states, post images, their media assets and the stored
extractions once and judges every candidate from them; the run reads them
for a batch of queued posts at a time, since it writes between posts. Its size
does not grow with the number of posts: `{generatedAt, disabled,
boardEnabled, messageEnabled, allPosts, limit, lookbackDays, postsRead,
candidates, requested, candidateMs, elapsedMs, run, later, cached, backoff,
retryStopped}`, `lookbackDays` being the extraction lookback the candidates
were selected with (0 for no limit). `run` lists, in order, the at most `limit` posts that take a
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
