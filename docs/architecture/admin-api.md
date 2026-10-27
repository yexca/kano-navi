# Admin API

The operator console at `/admin` is the only client of `/api/admin`. The API
is a resource-oriented JSON surface served by `backend-go/internal/adminapi`,
which owns one route table (Go `http.ServeMux` method patterns), the session
and origin guards, and the shared response conventions. Feature packages
(`llmadmin`, `contentadmin`, YouTube `websub`) contribute route handlers;
they do not route paths themselves.

MCP (`/mcp`) is a separate boundary with its own projections and never reuses
these routes. See [Backend](backend.md#mcp-boundary-mcp).

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
  "activeRun": Run | null,
  "recentRuns": [Run],            // newest 6
  "scheduler": {"enabled", "running", "tickSeconds"},
  "timers": {"scheduled", "nextRunAt"},
  "sources": {
    "x":       {"skipped", "lastSuccessAt", "lastStep"},
    "youtube": {"skipped", "lastSuccessAt", "lastStep", "push": {"enabled", "verified"}}
  },
  "review": {
    "pendingImages",          // unlabelled images with a URL the model has not settled (pending, uncertain, failed)
    "suspectedCancellations", // events with an LLM cancellation overlay
    "failedPosts",            // posts whose last extraction failed
    "reprocessRequested"      // posts queued for re-extraction
  },
  "counts": {"upcomingEvents", "events", "posts", "videos"}
}
```

`timers` counts the enabled [source schedules](#sources) this process runs
(not those disabled by `SKIP_*`) and the earliest planned run among them.
Retired saved workflows are not counted.

`lastSuccessAt` is the newest successful incremental read of the source
(`sync_state.last_success_at`). `lastStep` is `{runId, status, finishedAt}` of
the source's step in the newest finished run among the last 20 that included
it, or `null`.

## Runs

A run is one single-flight unit of work on the durable workflow queue. Its
shape is the `jobs.Job` projection shared with MCP control tools:
`{id, kind, status, createdAt, startedAt, finishedAt, triggeredBy, workflowId,
sourceSchedule, steps, progress, fetchWindows, result, error, run}` where
`kind` is `sync`, `scan`, `translate`, `workflow` or `live` (the YouTube
live-status probe, started only by its timer with `triggeredBy:
"live-probe"` and the one step `live`), `status` is `queued`, `running`,
`completed` or `failed`, and `progress` maps each step to `pending`,
`running`, `completed`, `partial`, `failed`, `skipped` or `cancelled`.
`sourceSchedule` names the source whose chain the run is: its timer
(`triggeredBy: "source-schedule"`) or the operator's Run now
(`triggeredBy: "admin"`); it is null for other runs. `workflowId` is null
for every new run. An older run of a retired saved workflow keeps that
workflow's ID there; its name is not read.

| Method and path          | Body / query              | Result                                 |
| ------------------------ | ------------------------- | -------------------------------------- |
| `GET /runs`              | `page`, `pageSize` (≤ 50) | list of runs, newest first             |
| `GET /runs/steps`        |                           | `{steps, skippedSteps}`                |
| `GET /runs/{id}`         |                           | `{run}`; `404 run_not_found`           |
| `POST /runs`             | see below                 | `202 {run}`; `409 {error:"busy", run}` |
| `POST /runs/{id}/cancel` |                           | `202 {run}`                            |
| `POST /runs/{id}/retry`  |                           | `202 {run}`; `409 busy`                |

`POST /runs` starts exactly one of:

- `{"kind": "sync"}`: every source, media, schedule and translate step.
- `{"kind": "scan"}`: the automatic schedule extraction only.
- `{"kind": "translate"}`: the translate step only.
- `{"kind": "workflow", "steps": ["x", "translate"]}`: a one-off step run.
  Known catalog steps run once in catalog order; an empty list or another
  value is `400 invalid_input`. Source steps read as their
  [source schedule](#sources) does.
- `{"kind": "fetch", "source": "x" | "youtube", "window": Window}`: one
  source plus media for a [history window](workflows.md#fetch-windows-and-historical-backfill).
  `Window` is `{mode: "recent" | "before", days}`, `{mode: "range",
startDate, endDate}` or `{mode: "count", count}` (1–200 per X account or
  for the YouTube channel). A mode outside the source's `windows` is
  rejected with `400 invalid_input`: past YouTube windows and X backfills
  need the official API credential, while X `range` and `count` windows
  page the public timeline and a YouTube `count` window reads the uploads
  listing.

A busy queue answers `409` with the active run. Saved workflows are removed
([ADR-0003](../decisions/ADR-0003-per-source-sync-schedules.md)): a
`workflowId` in the body answers `400 invalid_input`, and the
`/workflows` endpoints answer `404 not_found`.

`GET /runs/steps` is the step catalog `[{id, group}]` in execution order
(`x`, `youtube`, `bilibili` are `source`; `media`, `schedule`,
`translate`, `reconcile` are `process`), and `skippedSteps` lists the
steps disabled through `SKIP_*`.

A failed run with a retryable failed step can be retried. The run keeps its
steps, sync settings and windows, so no saved definition is needed.

## Sources

| Method and path                            | Body                               | Result                    |
| ------------------------------------------ | ---------------------------------- | ------------------------- |
| `GET /sources`                             |                                    | per-source status (below) |
| `PUT /sources/{source}/schedule`           | `{enabled, intervalMinutes, sync}` | `SourceSchedule`          |
| `POST /sources/{source}/run`               |                                    | `202 {run}`; `409 busy`   |
| `GET /sources/youtube/push`                |                                    | push status               |
| `PUT /sources/youtube/push`                | `{enabled, callbackUrl}`           | push status               |
| `POST /sources/youtube/push/rotate-secret` |                                    | push status               |
| `GET /sources/youtube/live-probe`          |                                    | live probe status         |
| `PUT /sources/youtube/live-probe`          | `{enabled, intervalMinutes}`       | live probe status         |

```json
{
  "schedules": [SourceSchedule],
  "x":       {"skipped", "historyAvailable", "windows", "accounts": [{"account", "oldest"}]},
  "youtube": {"skipped", "historyAvailable", "windows", "oldest", "push": PushStatus, "liveProbe": LiveProbeStatus},
  "bilibili": {"available", "skipped", "account", "videos", "signedIn", "minIntervalMinutes", "lastAttemptAt", "lastSuccessAt", "blockedUntil", "lastError"}
}
```

`historyAvailable` reports the official API credential; `windows` lists the
fetch window modes the source can run now, such as `["recent", "range",
"count"]` for X without `X_API_BEARER_TOKEN`.

`schedules` lists the [source schedules](workflows.md#source-schedules) of
`x`, `youtube` and `bilibili`, in that order. `SourceSchedule` is `{source,
enabled, intervalMinutes, minIntervalMinutes, maxIntervalMinutes, sync,
nextRunAt, lastRun, skipped}`, plus `reason: "environment"` while `SKIP_*`
disables the source. `sync` is `{mode: "incremental", max}` (default, `max` 50) or `{mode: "count", count}`, each 1–200, and null for Bilibili.
`lastRun` is `{id, status, at}` of the newest run of the source's chain, from
its timer or Run now
(durable run status; `at` is when it finished, started or was queued), or
null. The Bilibili `minIntervalMinutes` is `BILIBILI_MIN_INTERVAL_MINUTES`.
`PUT /sources/{source}/schedule` takes any subset of `{enabled,
intervalMinutes, sync}` and keeps omitted fields. It answers `404 not_found`
for another source and `400 invalid_input` for other fields, an interval
outside the bounds, an invalid `sync` or a `sync` for Bilibili. Enabling a
schedule or changing its interval plans the next run one interval later,
with the timer's [random offset](workflows.md#source-schedules); a sync
change alone keeps the plan.

`POST /sources/{source}/run` is **Run now**: it submits the source's fixed
chain once, triggered by the operator and read with the schedule's sync
setting, and answers `202 {run}` or `409 {error: "busy", run}` while the
queue is occupied. It does not move the schedule's `nextRunAt`. It answers
`404 not_found` for another source and `400 invalid_input` while `SKIP_*`
disables the source.

`PushStatus` is `{available, enabled, callbackUrl, callbackPath,
secretConfigured, verified, leaseExpiresAt, nextRenewalAt, pending,
lastNotificationAt, lastTriggeredAt, lastError}`. The signing secret is never
returned.

`LiveProbeStatus` is `{enabled, intervalMinutes, minIntervalMinutes,
maxIntervalMinutes, available, lastRunAt, nextRunAt, activeVideos,
maxVideos}`. `available` is false while timers or YouTube are disabled for
the process; `activeVideos` counts the videos the probe is following, never
their IDs, and one run checks at most `maxVideos` of them. `nextRunAt` is when
the next run is planned (the earliest due video, at least two minutes after
`lastRunAt`, and no earlier than now), or null while nothing is active or the
probe is off or unavailable. `PUT` accepts either field, keeps an omitted
one, and rejects other fields and intervals outside 5–360 minutes with
`400`.

## Events

| Method and path             | Body / query                                                                                                            | Result            |
| --------------------------- | ----------------------------------------------------------------------------------------------------------------------- | ----------------- |
| `GET /events`               | `page`, `pageSize` (≤ 100), `search`, `provenance`, `source`, `cancellation`, `merge`, `from`, `to`, `order`, `deleted` | list of events    |
| `POST /events`              | event fields                                                                                                            | `201 {event}`     |
| `PUT /events/{id}`          | event fields                                                                                                            | `{event}`         |
| `POST /events/{id}/confirm` | optional overrides                                                                                                      | `{event}`         |
| `POST /events/{id}/unmerge` |                                                                                                                         | `{event}`         |
| `DELETE /events/{id}`       |                                                                                                                         | `204` (tombstone) |

`provenance` is `automatic` or `manual`; `cancellation` is `none`,
`llm_suspected` or `manual_confirmed`; `order` is `asc` or `desc` (default);
`deleted=1` includes tombstones. Event fields are `title`, `detail`,
`startsOn`, `startsAt`, `endsAt`, `timePrecision`, `eventType`, `status`,
`url`, `cancellationStatus` and `cancellationReason` (required for
`manual_confirmed`). Every write is manual: it locks the event against
automatic extraction, and a delete is a durable tombstone. Confirming keeps
the stored values unless the body overrides them.

The list includes merged duplicates by default; `merge=merged` lists only
them and `merge=visible` excludes them, as every public reader does. Each
event carries `mergedInto`, `mergedIntoTitle`, `mergeSource` (`rule` or
`manual`), `mergeReason` and `mergedAt`. `unmerge` shows a merged event again
as a durable operator decision (`mergeSource = manual` without a target) that
the reconciler never overrides; an event that is not merged answers `400`.
Editing or confirming a rule-merged event also shows it again, because a
manual event is never merged away. Unmerging and edits advance the dashboard
revision.

## Schedule images

| Method and path                     | Body / query           | Result                                    |
| ----------------------------------- | ---------------------- | ----------------------------------------- |
| `GET /schedule-images`              | `limit` (≤ 200)        | `{items}`                                 |
| `POST /schedule-images/{id}/review` | `{status, reason}`     | `{image}`; `404 schedule_image_not_found` |
| `PUT /schedule-images/{id}/period`  | `{start, end, reason}` | `{image}`                                 |

`status` is `schedule`, `not_schedule` or `unreviewed` (which clears the
label; the others require `reason`). A `schedule` label is the manual gate
that lets an image reach the image model. Each item's `mediaStatus` is
`ready`, `pending`, `failed`, `missing` (no usable cache file yet) or
`none` (the asset has no image URL, such as the legacy `weekly-schedule`
alias); `none` never needs review and keeps `previewUrl` null.

## Posts

| Method and path                | Body / query                                                                                         | Result        |
| ------------------------------ | ---------------------------------------------------------------------------------------------------- | ------------- |
| `GET /posts`                   | `page`, `pageSize` (≤ 200), `source`, `account`, `from`, `to`, `search`, `media`, `llm`, `reprocess` | list of posts |
| `POST /posts/{id}/reprocess`   | `{route?}`                                                                                           | `{state}`     |
| `PUT /posts/{id}/quote/hidden` | `{hidden}`                                                                                           | `{state}`     |
| `POST /posts/{id}/refresh`     | none                                                                                                 | `{state}`     |

`account` is an X handle, matched without case or a leading `@`; `media` is
`present` or `missing`; `llm` is `never`, `pending`, `processed`, `failed`,
`skipped` or `uncertain`; `reprocess=1` keeps only queued posts;
`translation` is `incomplete`, `complete` or `requested`.

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
Its `media` has the `{url, status, alt}` shape but lists only ready cached
quote images, never the post's own. `quoteHiddenAt` is when an operator hid
the quote from the public pages, and `refreshRequestedAt` is a queued
refresh from the source; both are `null` otherwise. The console never edits
the quoted wording, author or images.

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
[Refresh from the source](sources.md#refresh-from-the-source). Both routes
answer `{state: {postId, quoteHiddenAt, refreshRequestedAt}}`; MCP control
has neither.

## Videos

| Method and path             | Body / query                                              | Result                                       |
| --------------------------- | --------------------------------------------------------- | -------------------------------------------- |
| `GET /videos`               | `page`, `pageSize` (≤ 200), `status`, `kind`, `thumbnail` | list plus `featuredVideoId`, `featuredVideo` |
| `PUT /videos/featured`      | `{videoId}` (`null` restores automatic selection)         | `{featuredVideoId}`                          |
| `GET /videos/{id}/history`  |                                                           | `{video, revisions, thumbnails}`             |
| `PUT /videos/{id}`          | `{title?, kind?, thumbnail?, isShort?}`                   | `{video}`                                    |
| `POST /videos/{id}/refresh` |                                                           | `202 {video, started, run}`                  |
| `POST /videos/bilibili`     | `{input}` (BV ID or bilibili.com video link)              | `{video, started, run}`                      |
| `GET /videos/merges`        |                                                           | `{items}` merge suggestions                  |
| `GET /videos/{id}/merges`   |                                                           | `{items}` for one Bilibili upload            |
| `POST /videos/{id}/merge`   | `{into}` (YouTube video ID or watch link)                 | `{video}`                                    |
| `POST /videos/{id}/unmerge` |                                                           | `{video}`                                    |

Each video's `thumbnailUrl` is the local `/media/<id>?v=<sha256>` URL of its
cached thumbnail, or `null` until that file is ready; the remote CDN address is
never returned. `thumbnailStatus` is `ready`, `pending`, `failed` or `missing`.

`featuredVideo` is the pinned video in the same shape, whatever page or filter
is listed, or `null` while the board chooses automatically.

Changing the featured video advances the dashboard revision.

`GET /videos` also accepts `source` (`youtube` or `bilibili`).

### Bilibili uploads

`POST /videos/bilibili` reads one upload of the configured Bilibili account
now and stores it; `POST /videos/{id}/refresh` on a Bilibili video reads it
again the same way and answers `200`. Both start the `media` step for the
cover (`started` is false while another run holds the queue). An input that
is neither a BV ID nor a `bilibili.com` video link, or an upload of another
account, answers `400`; a video Bilibili does not list answers `404`. While
Bilibili asks for a captcha, both answer `503 {"error":"bilibili_blocked",
blockedUntil, message}` without sending a request until that time; other
upstream failures answer `502` with their `bilibili_*` code. `bilibili` in
`GET /sources` reports the account, the stored uploads, whether a login cookie
is configured (`signedIn`, never the cookie), the read interval and the
latest attempt, success, backoff and error.

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
transaction.

`GET /videos/merges` lists, newest first and at most 50, the unmerged
Bilibili uploads the rule would merge (`status: "rule"`), or leaves to the
operator (`ambiguous`, or `unverified` when no candidate satisfies every
rule), without writing. Uploads an operator kept separate are left out.
Each item has `bilibili` (the upload as listed) and up to five `candidates`:
`{youtube, complete, titleMatch, durationDelta, gapMinutes}`, where
`durationDelta` is null when a length is unknown. `GET /videos/{id}/merges`
describes one upload whatever its state, adding `merged`, `separate` and
`none`.

### Video corrections and history

An operator correction replaces a platform value on the board without
changing it: `title`, `kind`, `isShort` and `thumbnailUrl` show the
correction, while `sourceTitle`, `sourceKind` and `sourceIsShort` keep what
the platform reported and `manualTitle`, `manualKind`, `manualIsShort` and
`manualThumbnail` (`{mediaId, sha256}`) are the corrections themselves.
`PUT /videos/{id}` sets a field from a string, for `isShort` from a boolean
(`true` for a Short, `false` for any other upload), or for `thumbnail` from
one `{mediaId, sha256}` file listed in the history; `null` clears it and an
omitted field keeps it. A title is 1–300 characters after trimming; a kind is
one of `VIDEO`, `MUSIC`, `LIVE ARCHIVE`, `GAMEPLAY` or `TALK`. Other fields,
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

| Method and path                     | Body                | Result                      |
| ----------------------------------- | ------------------- | --------------------------- |
| `GET /profile-media`                |                     | `{items, active}`           |
| `POST /profile-media`               | `{slot, sourceUrl}` | `201 {item, items, active}` |
| `POST /profile-media/discover`      | `{slot?, source?}`  | discovery result            |
| `POST /profile-media/upload?slot=`  | raw image body      | `201 {item, items, active}` |
| `GET /profile-media/{id}/preview`   |                     | image bytes (ready only)    |
| `POST /profile-media/{id}/download` |                     | `{item, items, active}`     |
| `POST /profile-media/{id}/select`   |                     | `{item, items, active}`     |

## Translations

| Method and path                              | Body                                                | Result                    |
| -------------------------------------------- | --------------------------------------------------- | ------------------------- |
| `GET /translations`                          |                                                     | `{counts, locales}`       |
| `POST /translations/request`                 | `{missing: true, ownerType?}` or `{ownerType, ids}` | `202 {queued, run, busy}` |
| `PUT /translations/{owner}/{id}/{locale}`    | `{text}`                                            | `{translation}`           |
| `DELETE /translations/{owner}/{id}/{locale}` |                                                     | `{translation}`           |

`owner` and `ownerType` are `post` or `video`; `locale` is `en` or `zh-CN`.
`counts` maps each owner type to `{total, incomplete, requested}` over the
posts and videos the translate step covers. A translation summary is
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
(at most 200), or with `missing: true` for every post or video (of
`ownerType`, or both) lacking a usable translation, whatever its age. When it
queued any and the run queue is free it starts a `translate` run; `busy: true`
means the requests wait for the next translate step.

## Extraction and models

| Method and path                          | Body                                                                                       | Result                                                    |
| ---------------------------------------- | ------------------------------------------------------------------------------------------ | --------------------------------------------------------- |
| `GET /extraction`                        |                                                                                            | extraction settings                                       |
| `PUT /extraction`                        | any subset of `{keywordEnabled, visionEnabled, messageEnabled, messageAllPosts, keywords}` | extraction settings                                       |
| `GET /llm`                               |                                                                                            | `{providers, models, queue, translationQueue, modelTags}` |
| `POST /llm/providers`                    | provider fields                                                                            | `201 {provider}`                                          |
| `PUT /llm/providers/{id}`                | provider fields                                                                            | `{provider}`                                              |
| `DELETE /llm/providers/{id}`             |                                                                                            | `204`                                                     |
| `POST /llm/providers/{id}/test`          | `{model?}`                                                                                 | `{ok, method, model, remoteModelCount, provider}`         |
| `POST /llm/providers/{id}/discover`      |                                                                                            | `{provider, models}` remote catalog                       |
| `POST /llm/providers/{id}/models`        | `{models, origin?}`                                                                        | `201 {provider, models}`                                  |
| `PUT /llm/providers/{id}/models`         | one model `{id, ...}`                                                                      | `{provider, model, models}`                               |
| `POST /llm/providers/{id}/models/remove` | `{ids}`                                                                                    | `{provider, models, removed}`                             |
| `PUT /llm/queue`                         | `{route?, targets: [{providerId, modelId}]}`                                               | `{route, queue}`                                          |

Extraction settings are `{enabled, keywordEnabled, visionEnabled,
messageEnabled, messageAllPosts, keywords}`; `enabled` is derived: the message
stage alone, or keyword candidates plus boards. `messageAllPosts` makes the
message stage select every post instead of only heuristic matches. `queue` is
the single ordered `[{providerId, modelId}]` list every extraction stage draws
from; every target names a catalog model (`modelId` is required), and a save
replaces the whole list. `route` selects the queue a save replaces: `schedule`
(the default) or `translate`, the separate `translationQueue` of the
translate step, whose targets must carry the `text` tag. Removing a model
removes its targets from both. Providers have no default model: a provider response
carries no `model` field, a `model` field in a provider write is ignored, and a
provider test without `{model}` only lists the provider's models. Creating a
provider or adding catalog models never queues anything. Model IDs may
contain `/`, so model writes name the model in the body. Provider responses
report `apiKeyConfigured` only; keys are write-only.

## Related Docs

- [Backend](backend.md)
- [Frontend](frontend.md)
- [Workflows](workflows.md)
- [Secure development](../development/security.md)
