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
  "workflows": {"total", "scheduled", "nextRunAt"},
  "sources": {
    "x":       {"skipped", "lastSuccessAt", "lastStep"},
    "youtube": {"skipped", "lastSuccessAt", "lastStep", "push": {"enabled", "verified"}}
  },
  "review": {
    "pendingImages",          // unlabelled images the model has not settled (pending, uncertain, failed)
    "suspectedCancellations", // events with an LLM cancellation overlay
    "failedPosts",            // posts whose last extraction failed
    "reprocessRequested"      // posts queued for re-extraction
  },
  "counts": {"upcomingEvents", "events", "posts", "videos"}
}
```

`lastSuccessAt` is the newest successful incremental read of the source
(`sync_state.last_success_at`). `lastStep` is `{runId, status, finishedAt}` of
the source's step in the newest finished run among the last 20 that included
it, or `null`.

## Runs

A run is one single-flight unit of work on the durable workflow queue. Its
shape is the `jobs.Job` projection shared with MCP control tools:
`{id, kind, status, createdAt, startedAt, finishedAt, triggeredBy, workflowId,
workflowName, steps, progress, fetchWindows, result, error, run}` where
`kind` is `sync`, `scan` or `workflow`, `status` is `queued`, `running`,
`completed` or `failed`, and `progress` maps each step to `pending`,
`running`, `completed`, `partial`, `failed`, `skipped` or `cancelled`.

| Method and path          | Body / query              | Result                                 |
| ------------------------ | ------------------------- | -------------------------------------- |
| `GET /runs`              | `page`, `pageSize` (≤ 50) | list of runs, newest first             |
| `GET /runs/{id}`         |                           | `{run}`; `404 run_not_found`           |
| `POST /runs`             | see below                 | `202 {run}`; `409 {error:"busy", run}` |
| `POST /runs/{id}/cancel` |                           | `202 {run}`                            |
| `POST /runs/{id}/retry`  |                           | `202 {run}`; `409 busy`                |

`POST /runs` starts exactly one of:

- `{"kind": "sync"}`: every source, media and schedule step.
- `{"kind": "scan"}`: the automatic schedule extraction only.
- `{"kind": "workflow", "workflowId": "..."}`: a saved workflow;
  `404 workflow_not_found`.
- `{"kind": "fetch", "source": "x" | "youtube", "window": Window}`: one
  source plus media for a [history window](sources.md). Past windows need the
  source's official API credential (`400 invalid_input`).

A busy queue answers `409` with the active run.

## Workflows

| Method and path          | Body                                                                  | Result                                 |
| ------------------------ | --------------------------------------------------------------------- | -------------------------------------- |
| `GET /workflows`         |                                                                       | `{items, steps, limits, skippedSteps}` |
| `POST /workflows`        | `{name, steps, scheduleEnabled, intervalMinutes, sourceLookbackDays}` | `201 {workflow}`                       |
| `PUT /workflows/{id}`    | any subset of the above                                               | `{workflow}`                           |
| `DELETE /workflows/{id}` |                                                                       | `204`; `409 busy` while it runs        |

`steps` is the catalog `[{id, group}]` (`x`, `youtube` are `source`; `media`,
`schedule` are `process`), `limits` is `{minIntervalMinutes,
maxIntervalMinutes}` and `skippedSteps` lists steps disabled through `SKIP_*`.
Each workflow carries `lastRunAt`, `lastStatus` (durable run status) and
`nextRunAt`.

## Sources

| Method and path                            | Body                     | Result                    |
| ------------------------------------------ | ------------------------ | ------------------------- |
| `GET /sources`                             |                          | per-source status (below) |
| `GET /sources/youtube/push`                |                          | push status               |
| `PUT /sources/youtube/push`                | `{enabled, callbackUrl}` | push status               |
| `POST /sources/youtube/push/rotate-secret` |                          | push status               |

```json
{
  "x":       {"skipped", "historyAvailable", "accounts": [{"account", "oldest"}]},
  "youtube": {"skipped", "historyAvailable", "oldest", "push": PushStatus}
}
```

`PushStatus` is `{available, enabled, callbackUrl, callbackPath,
secretConfigured, verified, leaseExpiresAt, nextRenewalAt, pending,
lastNotificationAt, lastTriggeredAt, lastError}`. The signing secret is never
returned.

## Events

| Method and path             | Body / query                                                                                                   | Result            |
| --------------------------- | -------------------------------------------------------------------------------------------------------------- | ----------------- |
| `GET /events`               | `page`, `pageSize` (≤ 100), `search`, `provenance`, `source`, `cancellation`, `from`, `to`, `order`, `deleted` | list of events    |
| `POST /events`              | event fields                                                                                                   | `201 {event}`     |
| `PUT /events/{id}`          | event fields                                                                                                   | `{event}`         |
| `POST /events/{id}/confirm` | optional overrides                                                                                             | `{event}`         |
| `DELETE /events/{id}`       |                                                                                                                | `204` (tombstone) |

`provenance` is `automatic` or `manual`; `cancellation` is `none`,
`llm_suspected` or `manual_confirmed`; `order` is `asc` or `desc` (default);
`deleted=1` includes tombstones. Event fields are `title`, `detail`,
`startsOn`, `startsAt`, `endsAt`, `timePrecision`, `eventType`, `status`,
`url`, `cancellationStatus` and `cancellationReason` (required for
`manual_confirmed`). Every write is manual: it locks the event against
automatic extraction, and a delete is a durable tombstone. Confirming keeps
the stored values unless the body overrides them.

## Schedule images

| Method and path                     | Body / query           | Result                                    |
| ----------------------------------- | ---------------------- | ----------------------------------------- |
| `GET /schedule-images`              | `limit` (≤ 200)        | `{items}`                                 |
| `POST /schedule-images/{id}/review` | `{status, reason}`     | `{image}`; `404 schedule_image_not_found` |
| `PUT /schedule-images/{id}/period`  | `{start, end, reason}` | `{image}`                                 |

`status` is `schedule`, `not_schedule` or `unreviewed` (which clears the
label; the others require `reason`). A `schedule` label is the manual gate
that lets an image reach the image model.

## Posts

| Method and path              | Body / query                                                                                         | Result        |
| ---------------------------- | ---------------------------------------------------------------------------------------------------- | ------------- |
| `GET /posts`                 | `page`, `pageSize` (≤ 200), `source`, `account`, `from`, `to`, `search`, `media`, `llm`, `reprocess` | list of posts |
| `POST /posts/{id}/reprocess` | `{route?}`                                                                                           | `{state}`     |

`media` is `present` or `missing`; `llm` is `never`, `pending`, `processed`,
`failed`, `skipped` or `uncertain`; `reprocess=1` keeps only queued posts.

## Videos

| Method and path        | Body / query                                              | Result                      |
| ---------------------- | --------------------------------------------------------- | --------------------------- |
| `GET /videos`          | `page`, `pageSize` (≤ 200), `status`, `kind`, `thumbnail` | list plus `featuredVideoId` |
| `PUT /videos/featured` | `{videoId}` (`null` restores automatic selection)         | `{featuredVideoId}`         |

Changing the featured video advances the dashboard revision.

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

## Extraction and models

| Method and path                          | Body                                                                                       | Result                                            |
| ---------------------------------------- | ------------------------------------------------------------------------------------------ | ------------------------------------------------- |
| `GET /extraction`                        |                                                                                            | extraction settings                               |
| `PUT /extraction`                        | any subset of `{keywordEnabled, visionEnabled, messageEnabled, messageAllPosts, keywords}` | extraction settings                               |
| `GET /llm`                               |                                                                                            | `{providers, models, routes, modelTags}`          |
| `POST /llm/providers`                    | provider fields                                                                            | `201 {provider}`                                  |
| `PUT /llm/providers/{id}`                | provider fields                                                                            | `{provider}`                                      |
| `DELETE /llm/providers/{id}`             |                                                                                            | `204`                                             |
| `POST /llm/providers/{id}/test`          | `{model?}`                                                                                 | `{ok, method, model, remoteModelCount, provider}` |
| `POST /llm/providers/{id}/discover`      |                                                                                            | `{provider, models}` remote catalog               |
| `POST /llm/providers/{id}/models`        | `{models, origin?}`                                                                        | `201 {provider, models}`                          |
| `PUT /llm/providers/{id}/models`         | one model `{id, ...}`                                                                      | `{provider, model, models}`                       |
| `POST /llm/providers/{id}/models/remove` | `{ids}`                                                                                    | `{provider, models, removed}`                     |
| `PUT /llm/routes/{route}`                | `{targets: [{providerId, modelId}]}`                                                       | `{routes}`                                        |

Extraction settings are `{enabled, keywordEnabled, visionEnabled,
messageEnabled, messageAllPosts, keywords}`; `enabled` is derived: the message
stage alone, or keyword candidates plus boards. `messageAllPosts` makes the
message stage select every post instead of only heuristic matches. `routes`
maps each route (`schedule_vision`, `schedule_message`,
`schedule_board`) to `{targets, configured}`:
the effective ordered targets and the operator's saved list. Model IDs may
contain `/`, so model writes name the model in the body. Provider responses
report `apiKeyConfigured` only; keys are write-only.

## Related Docs

- [Backend](backend.md)
- [Frontend](frontend.md)
- [Workflows](workflows.md)
- [Secure development](../development/security.md)
