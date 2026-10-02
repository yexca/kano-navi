# Data and Synchronization

## SQLite Snapshot

The database file is `data/database/kano.sqlite`. On startup the server creates the
directory, schema, and missing seed records. The current tables are:

| Table                    | Purpose                                                                                                   |
| ------------------------ | --------------------------------------------------------------------------------------------------------- |
| `profiles`               | Name, bio, avatar, banner, and official entry points                                                      |
| `posts`                  | X posts, account source, publication time, engagement counts, and media                                   |
| `events`                 | Unified schedules, provenance, confidence, manual locks, and tombstones                                   |
| `event_sources`          | Source post/reservation identities attached to each event                                                 |
| `videos`                 | Published YouTube videos and scheduled streams                                                            |
| `focus`                  | The page's latest focus item and stable YouTube video ID                                                  |
| `timeline`               | Person and activity timeline                                                                              |
| `resources`              | X, YouTube, Wikipedia, and other resource links                                                           |
| `assets`                 | Schedule images and their sources                                                                         |
| `media_assets`           | Remote media identities, cache metadata, and fetch status                                                 |
| `media_links`            | Links from cached media to posts, videos, profiles, and focus items                                       |
| `sync_runs`              | Status, counts, error summaries, trigger, and asynchronous job identity                                   |
| `sync_state`             | Durable per-account X and YouTube cursors                                                                 |
| `app_settings`           | Non-secret model, schedule-extractor, X-account, and Featured settings                                    |
| `schedule_extractions`   | Versioned LLM inputs, outcomes, and structured result metadata                                            |
| `post_llm_states`        | Per-post LLM status, last attempt, error, and reprocess queue state                                       |
| `schedule_asset_reviews` | Per-schedule-image LLM verdict, skip reason, and optional manual label with its reason                    |
| `llm_providers`          | OpenAI-compatible endpoints, Text/Image capabilities, health, and encrypted keys                          |
| `llm_models`             | Per-provider model catalog with tags (`text`/`image` routing; `reasoning`/`tools`/`embedding` labels)     |
| `llm_route_targets`      | Ordered provider + model failover targets for `schedule_board`, `schedule_message`, and `schedule_vision` |
| `llm_route_providers`    | Legacy provider-only routes, copied once into `llm_route_targets` and no longer written                   |
| `workflows`              | Saved step selections, timer settings, and last/next run metadata                                         |

The API maps snake_case columns to camelCase and removes `raw_json`. The
frontend must not depend on database fields that are not declared in the API
response.

Remote media URL columns are retained as migration-era source fields. The
dashboard maps them through `media_assets`: a ready file becomes
`/media/<opaque-id>`, a tracked static file remains `/assets/...`, and an
unready remote file becomes `null` with an explicit status.

See [Media cache](media-cache.md) for the file layout, lifecycle, and downloader
boundary.

## Seed versus Synchronization

`server/seed-data.js` is the reviewable initial public snapshot used for a new
environment and offline display. `npm run seed` only fills missing IDs by
default. `npm run sync` reads public sources and upserts updated records. Neither
command should silently delete historical posts or events.

Legacy `events` and `focus` tables are migrated in place. Existing X schedule
rows are treated as manually curated and locked; existing YouTube reservations
remain automatic. The migration does not clear snapshots.

## Current Sources

### X

The script reads each public profile named by `X_HANDLES` (default
`kano_2525,_Kanotic`), extracts status IDs, and decodes their Snowflake
timestamps before making detail requests. Each account has its own cursor in
`sync_state`; with no cursor, IDs older than `X_BOOTSTRAP_DAYS` (seven by
default) are skipped. Later runs prioritize unknown IDs and may refresh only a
small `X_REFRESH_KNOWN` budget. Discovery and detail-request limits cap the
work per run; `X_MAX_STATUS_REQUESTS` is the total detail-request budget shared
by all configured accounts. Successful posts are stored with `account_handle`
and returned as one time-ordered feed. Image-only posts are retained as
candidates as long as they have a publication time and a media URL.
`X_HANDLE` remains a single-account compatibility fallback.

Each discovered image URL is registered and linked to its post. A bounded media
stage later in the same command downloads pending image files from X's
`*.twimg.com` media CDN subdomains.
The X adapter also accepts media and schedule keywords carried by a quoted
tweet (`qrt`) in the detail response.
When a schedule asset already has a source status URL but no image URL, the
synchronizer adds up to `X_SCHEDULE_REFRESH_LIMIT` recent source IDs per
account to the detail-request queue. On a cursorless bootstrap with no asset
source for an account, a bounded set of keyword-matching snapshot posts can
serve the same purpose. These requests supplement the profile HTML without
bypassing the shared `X_MAX_STATUS_REQUESTS` budget and can recover a schedule
post that has fallen out of the profile page's visible status list.

### YouTube

The script reads the RSS feed for `YOUTUBE_CHANNEL_ID` (the current main channel
by default). With no cursor, it stores the latest `YOUTUBE_BOOTSTRAP_VIDEOS`
entries (six by default). Later runs store only entries before the prior cursor,
or entries newer than its timestamp when that cursor has fallen out of the RSS
window. It separately inspects the streams page and keeps rechecking active or
recent reservations for `scheduledStartTime`, writing them to both `videos` and
`events`. When no schedule time is available, the script does not invent one.

Video thumbnails are registered and linked to their rows, then the bounded media
stage downloads pending files from YouTube's `*.ytimg.com` thumbnail CDN
subdomains (including `i.ytimg.com` and its numbered hosts).

### Schedule Extraction Pipeline

Schedule extraction has two independent candidate paths:

1. The schedule-board path uses the admin-configurable keyword list to select
   posts that look like a schedule image or board. It uses the
   `schedule_board` provider route.
2. The single-message path applies a cheap local date/time and scheduling-verb
   heuristic to ordinary X messages, including messages without a configured
   keyword. Suspected messages are sent to the `schedule_message` provider
   route, which first classifies the post as `schedule`, `not_schedule`, or
   `uncertain` before extracting events.

The two candidate lists are merged, de-duplicated by X post ID, sorted by
publication time, and limited by one shared scan limit. A post selected by the
board path is not sent a second time through the message path in the same scan.
The legacy `schedule_vision` route remains available for older API clients and
is used as the compatibility fallback when a new route has no explicit order.

Each route is an ordered list of targets: a provider plus one of its catalog
models. An empty model ID means "the provider's default model" (`llm_providers.model`)
and uses the provider-level capabilities; this is how legacy provider-only
routes were migrated and how the old `PUT /api/admin/providers/order` endpoint
still writes. The default model's catalog tags and the provider capability
columns are kept identical whichever side is edited. Target selection follows
the original post modality. Text-only input requires `text`; image-only input
requires `image`; mixed input requires a model with both tags. Only ready cached images are sent to the model. A post that
declares an image but whose cache is not ready is recorded as `media_pending`
and is not silently reduced to text-only input. Each provider uses the OpenAI
Responses or Chat Completions shape, receives at most three total calls (one
initial request plus two retries), and then yields to the next enabled, keyed,
capability-compatible provider in that route's priority order. The persisted
`max_retries` field is retained for old clients and migrated to this fixed
policy; it is not a cost-control override.

### LLM Provider Catalog

`/admin` manages providers centrally. A provider stores the API host, format
(Responses or Chat Completions), timeout, and encrypted key. On an operator
action the server requests `GET <baseUrl>/models` with the stored key, a
timeout, `redirect: "error"`, and a 4 MB response limit; it accepts OpenAI-style
`{ data: [{ id }] }`, `{ models: [...] }`, or bare arrays. Upstream bodies and
errors are reduced to fixed messages. Tags for newly added models are suggested
from the model ID and stay editable. Removing a model removes its route
targets; removing the default model clears `llm_providers.model`. The private
and loopback address guard for provider URLs applies to model discovery too.

A local validator rejects invalid calendar dates, time formats, enumerations,
or confidence values before any event write. `schedule` results update automatic
events. A model never deletes or definitively cancels an event: `action=cancel`
matches the clearest existing event and writes an `llm_suspected` cancellation
overlay containing the reason, source evidence, confidence, and post ID. The
overlay applies to manually locked events too, so a manually entered event can
remain visible with the model's cancellation evidence. `not_schedule` and
`uncertain` results only record the extraction outcome and leave existing events
untouched. Input fingerprints and the extractor version make these outcomes
idempotent. Date-only events store `starts_on`, a null `starts_at`, and
`time_precision = unknown`; the model must not invent a specific time.

Every stored post has an LLM processing state. Failed, skipped, and uncertain
attempts remain visible to operators, and `/api/admin/posts/:id/llm/reprocess`
queues a forced pass that bypasses the successful-result cache. The automatic
scan consumes queued posts even when they no longer match the current keyword
heuristic; a missing provider or pending image keeps the queue request pending,
while a provider failure consumes it. Queued posts run before keyword
candidates within the shared scan limit. They use the route the operator
requested; without one, a post that a keyword path also selected keeps that
route, image posts use `schedule_board`, and text posts use `schedule_message`.

The default model comes from the database (`app_settings.llm_model` and the
seeded `openai-default` provider). Provider API keys are encrypted with
`LLM_SECRETS_KEY`; ciphertext is never returned by an API response or written
to extraction `raw_json`. A legacy `OPENAI_API_KEY` can be imported once with
`npm run migrate:llm`; synchronization never reads that environment variable.
Candidates from either X account use the same idempotent fingerprint and
manual-lock rules. The stage flags are stored as `schedule_keyword_enabled`,
`schedule_vision_enabled`, and `schedule_message_enabled` and can be changed
independently in `/admin`. The legacy `schedule_extraction_enabled` setting is
retained for old clients and for seeding the stage defaults; it is no longer a
runtime master switch, so a newly enabled stage can run even when an older
database still contains `0` there. The old admin API field updates all three
stage flags together for compatibility.

The `featured_video_id` setting is maintained through `/admin`. It points to an
existing local `videos` row (or an empty value for no Featured item), and source
synchronization never changes it. The dashboard includes the selected video
even when it falls outside the most recent 30 rows.

### Schedule Images

A schedule image (`assets.kind = schedule`) is public only after two gates:

1. Sync promotes a post's image only when the author's own wording, including
   quoted text, explicitly reads as a weekly or multi-day board notice
   (`server/schedule-asset.js`). The stored post `label` is not part of that
   input, because the looser post classifier labels any `配信予定` post as
   `SCHEDULE / 日程`. The configurable extraction keywords do not promote images.
2. The automatic scan sends each unreviewed candidate's ready cached image,
   without the post text, to the `schedule_board` route and records
   `schedule`, `not_schedule`, or `uncertain` in `schedule_asset_reviews`. A
   candidate whose source post no longer passes gate 1 (written by an older
   sync) is recorded as `skipped` / `source_not_board` without a model call. A
   pending cache, or no keyed Image provider, is also `skipped` and retried.

`/api/dashboard` returns a schedule asset only when an operator labelled it
`schedule`, or when it has no manual label, still passes gate 1, and the model
returned `schedule`. A manual `not_schedule` always hides it. Labels require a
reason and are set in the `/admin` schedule-images view. Sync reuses the
`schedule-<week>` IDs and the `weekly-schedule` alias, so a review belongs to
the image URL it judged: when an asset ID receives a different URL, its model
verdict and manual label reset to `pending` / `unreviewed`.

## Workflows and Scheduling

Synchronization is split into modular steps declared in
`server/workflow-catalog.js`, executed in this order: `x`, `youtube`, `media`,
`schedule`. `npm run sync` still runs every step not disabled by `SKIP_*`.
A saved workflow chooses a subset; its run records `sync_runs.source =
workflow` and exposes per-step progress (`pending`, `running`, `completed`,
`partial`, `failed`, `skipped`) through `/api/admin/sync/jobs`. When a selected
step is disabled by `SKIP_*`, its result records `{ skipped: true, reason:
"environment" }` instead of silently disappearing.

A workflow timer is 15 minutes to 7 days. Enabling a timer (or changing its
interval) plans the next run one interval later; it never fires immediately.
The server process checks due workflows every 30 seconds and starts one
through the same single-flight queue as manual and MCP jobs; a due workflow
that finds the queue busy is retried on the next tick. Two workflows are seeded
once (`full-refresh` and `sources-only`) without timers; deleting them does not
recreate them. Set `WORKFLOW_SCHEDULER_ENABLED=0` to disable timers.

## Event Precedence

Automatic X extraction and YouTube reservations use `provenance = automatic`.
Creating, editing, or confirming an event through `/admin` changes it to
`provenance = manual` and sets `manual_locked = 1`. Automatic upserts include a
database-level lock condition and cannot replace those rows. Manual deletion is
a soft-delete tombstone with the same lock, so synchronization cannot resurrect
it. Manual operators can mark an event `manual_confirmed` with a required reason;
that decision is separate from the LLM overlay and is what the public dashboard
uses as a definitive cancellation. Editing or confirming an event that carries
an `llm_suspected` overlay keeps the overlay and its evidence unless the
operator explicitly dismisses it (`none`) or replaces it with a manual decision.
LLM matching compares start instants, so `Z` and `+09:00` offsets refer to the
same event. The dashboard hides tombstones, keeps cancelled rows visible with
their reason/evidence, and labels visible automatic/manual events.

## Failure and Freshness

- Each run starts a `sync_runs` record and finishes with `success`, `partial`, or `failed`.
- A source failure does not clear another source or any existing snapshot; errors are available for maintainer diagnosis.
- A media failure never downgrades an existing ready file. An invalid LLM result
  never replaces existing events.
- The page shows the latest completion time, but that is not the same as real-time data. Users should follow the original platform link for confirmation.
- CI never performs live synchronization, so builds do not depend on platform networks, login walls, or rate limits.

## Environment Variables

| Variable                      | Default              | Purpose                                                      |
| ----------------------------- | -------------------- | ------------------------------------------------------------ |
| `APP_MODE`                    | Development          | `development` bypasses admin login; `production` requires it |
| `ADMIN_PASSWORD`              | Empty                | Production admin password, minimum 12 characters             |
| `LLM_SECRETS_KEY`             | Empty                | Environment-only master key for encrypted provider API keys  |
| `PORT`                        | `8787`               | Express listening port                                       |
| `SCHEDULE_EXTRACTION_ENABLED` | `1`                  | Enable the automatic schedule stage                          |
| `SCHEDULE_KEYWORD_ENABLED`    | `1`                  | Enable keyword candidate selection                           |
| `SCHEDULE_VISION_ENABLED`     | `1`                  | Enable schedule-board LLM extraction                         |
| `SCHEDULE_MESSAGE_ENABLED`    | `1`                  | Enable single-message schedule detection                     |
| `SCHEDULE_KEYWORDS`           | `schedule,...`       | Comma-separated schedule candidate keywords                  |
| `WORKFLOW_SCHEDULER_ENABLED`  | `1`                  | Start timed saved workflows inside the server process        |
| `MCP_ENABLED`                 | `1`                  | Expose the stateless `/mcp` endpoint                         |
| `MCP_CONTROL_TOKEN`           | Empty                | Bearer token for MCP mutation tools                          |
| `X_HANDLES`                   | `kano_2525,_Kanotic` | Comma-separated public X account names                       |
| `X_HANDLE`                    | Empty                | Single-account compatibility fallback                        |
| `X_BOOTSTRAP_DAYS`            | `7`                  | First-run X lookback in days                                 |
| `X_DISCOVERY_LIMIT`           | `50`                 | Maximum discovered status IDs considered                     |
| `X_MAX_STATUS_REQUESTS`       | `12`                 | Maximum X detail requests per run                            |
| `X_REFRESH_KNOWN`             | `1`                  | Known X posts refreshed per run                              |
| `X_SCHEDULE_REFRESH_LIMIT`    | `1`                  | Missing schedule source posts prioritized per account        |
| `YOUTUBE_CHANNEL_ID`          | Main channel         | YouTube channel identifier                                   |
| `YOUTUBE_BOOTSTRAP_VIDEOS`    | `6`                  | First-run RSS entries stored                                 |
| `YOUTUBE_MAX_DETAIL_REQUESTS` | `12`                 | Reservation pages inspected per run                          |
| `SYNC_TIMEOUT_MS`             | `7000`               | Timeout for a source network request                         |
| `SYNC_REQUEST_DELAY_MS`       | `150`                | Delay between source detail requests                         |
| `MEDIA_DOWNLOAD_LIMIT`        | `20`                 | Pending images attempted per run                             |
| `MEDIA_DOWNLOAD_TIMEOUT_MS`   | `10000`              | Timeout for an image request                                 |
| `MEDIA_MAX_BYTES`             | `10485760`           | Maximum bytes accepted for one image                         |
| `SKIP_X`, `SKIP_YOUTUBE`      | `0`                  | Set an individual source flag to `1` to skip it              |
| `SKIP_MEDIA`, `SKIP_LLM`      | `0`                  | Set a post-processing stage flag to `1` to skip it           |

`.env.example` is the complete non-secret inventory. `ADMIN_PASSWORD` and
`LLM_SECRETS_KEY`, `MCP_CONTROL_TOKEN`, and provider API keys are credentials;
never place real values in source code, SQLite plaintext, URLs, API responses,
or logs. `OPENAI_API_KEY` is accepted only by the one-time migration command.

## Manual Maintenance

Use `/admin` instead of direct SQL for event maintenance. Creating, editing,
confirming, or deleting through that API records manual precedence and preserves
source links. Direct database edits can omit the lock/tombstone rules and should
be reserved for reviewed recovery work.

The `/mcp` endpoint is intentionally narrower than `/admin`. Its public tools
read the prepared snapshot and sanitized schedule/status views. Bearer-protected
tools may request a revision, start a full synchronization, or run the automatic
keyword/board/message scan; all of these return an asynchronous job result. MCP has no
manual confirmation, edit, delete, profile-media selection/upload, SQL, file, or
arbitrary URL-proxy operation. A page refresh or revision request never performs
an external fetch.
