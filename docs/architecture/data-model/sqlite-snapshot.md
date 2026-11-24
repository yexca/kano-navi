# SQLite Snapshot

The snapshot tables, workflow tables and sync state.

The database file is `data/database/kano.sqlite`. `kano-go serve`, `seed` and
`sync` open it through `OpenStandalone`
(`backend/internal/store/sqlite/schema_upgrade.go`), which creates the
directory and then, in one transaction:

- gives a file without tables the canonical schema embedded from
  `bootstrap.sql`;
- upgrades an existing snapshot to that schema. A snapshot must contain
  `profiles`, `posts`, `events`, `videos`, `app_settings` and `sync_runs`;
  anything else is rejected before any write. Missing tables, columns (with
  their declared defaults) and indexes are added, and the backfills described
  in this document run. A required column without a default fails the open
  instead of being guessed. The only rebuild is described in
  [Seed versus Synchronization](seeds-and-schema.md#seed-versus-synchronization); no column is
  dropped and no snapshot is cleared;
- verifies the required columns and inserts missing static seed records.

The [workflow tables](#workflow-tables) are then created or verified in their
own transaction, and the selected profile media is reconciled with its files
(see [Event Precedence](event-precedence.md)). The tables are:

| Table                       | Purpose                                                                                                |
| --------------------------- | ------------------------------------------------------------------------------------------------------ |
| `profiles`                  | Name, bio, avatar, banner, and official entry points                                                   |
| `profile_media`             | Avatar and banner candidates, their downloads, and the operator's selected file                        |
| `account_avatars`           | Per-account active avatar state and display mode (`auto`, `manual`, `hidden`)                          |
| `account_avatar_candidates` | Cached and uploaded avatar history per tracked X account                                               |
| `posts`                     | X posts and Bilibili dynamics, account, publication time, engagement counts, media, and the quote      |
| `events`                    | Unified schedules, platform, provenance, confidence, manual locks, tombstones, and duplicate merges    |
| `event_sources`             | Source post/reservation identities attached to each event                                              |
| `videos`                    | YouTube videos and streams, Bilibili uploads and their merges, corrections, and refresh requests       |
| `video_revisions`           | Titles and thumbnails a platform replaced, one row per change                                          |
| `focus`                     | The page's latest focus item and stable YouTube video ID                                               |
| `timeline`                  | Person and activity timeline                                                                           |
| `resources`                 | X, YouTube, Wikipedia, and other resource links                                                        |
| `assets`                    | Schedule images and their sources                                                                      |
| `media_assets`              | Remote media identities, cache metadata, and fetch status                                              |
| `media_links`               | Links from cached media to posts, videos, profiles, focus items and Works upload videos                |
| `media_versions`            | Earlier cached files of an asset whose bytes a revalidation replaced                                   |
| `sync_runs`                 | Status, counts, error summaries, trigger, and asynchronous job identity                                |
| `sync_state`                | Durable per-account X, YouTube, Bilibili and shop cursors, read bookkeeping and X API chain state      |
| `app_settings`              | Model, extractor, X-account, Featured, YouTube push and proxy settings (secrets sealed)                |
| `schedule_extractions`      | Versioned LLM inputs, outcomes, and structured result metadata                                         |
| `post_llm_states`           | Per-post LLM status, last attempt, error, reprocess queue state, failure backoff, settled input stamp  |
| `schedule_asset_reviews`    | Per-schedule-image LLM verdict, skip reason (`no_image` for a URL-less row), and optional manual label |
| `llm_providers`             | OpenAI-compatible or Anthropic endpoints, Text/Image capabilities, health, and encrypted keys          |
| `llm_models`                | Per-provider model catalog with tags (`text`/`image` routing; `reasoning`/`tools`/`embedding` labels)  |
| `llm_route_targets`         | The ordered provider + model failover queues: `schedule`, `verify` and `translate`                     |
| `llm_route_providers`       | Provider-only routes that startup copies once into `llm_route_targets`; nothing else writes them       |
| `source_schedules`          | Each source's timer, sync setting and method chain                                                     |
| `workflows`                 | Saved workflow definitions: kept in the schema, created empty, never read or written                   |
| `work_cover_lookups`        | Each niconico or Bilibili cover lookup of a Works upload video: outcome, image URL and retry time      |
| `translations`              | Manual, platform, and machine translations of posts, videos and pooled statuses per target locale      |
| `translation_states`        | Per-owner machine translation state: source hash and language, last attempt, operator request          |
| `x_statuses`                | The X status pool: related accounts' statuses and statuses own posts quote or repost; never public     |
| `x_reposts`                 | The own X accounts' reposts: reposted status, account, author, first sighting and first known own ID   |
| `x_repost_ids`              | Every known own status ID of a repost, with its original status, account and first sighting            |
| `bilibili_statuses`         | The Bilibili status pool: dynamics own forwards forward; never public                                  |
| `alert_incidents`           | [Operator alert](../alerts.md#incidents) incidents: rule, subject, severity, detail, status, ack time  |
| `alert_deliveries`          | The [alert outbox](../alerts.md#delivery-outbox): channel, kind, status, attempts and error code       |

The API maps snake_case columns to camelCase and removes `raw_json`. The
frontend must not depend on database fields that are not declared in the API
response.

Remote media URL columns (`posts.media_url`, `videos.thumbnail_url`,
`focus.image_url` and the like) keep the addresses the source reported; they
are never browser URLs. The dashboard maps them through `media_assets`: a
ready file becomes `/media/<opaque-id>`, a tracked static file remains
`/assets/...`, and an unready remote file becomes `null` with an explicit
status. See [Media cache](../media-cache.md) for the file layout, lifecycle, and
downloader boundary.

## Workflow Tables

`backend/internal/store/sqlite/workflow_migrations.go` creates the durable
run queue with `CREATE TABLE IF NOT EXISTS` and adds missing columns, then
checks that every table has the columns its recorded version requires:

| Table                      | Purpose                                                                                                |
| -------------------------- | ------------------------------------------------------------------------------------------------------ |
| `workflow_migrations`      | Applied workflow schema versions (current: 3); a newer recorded version fails the open                 |
| `workflow_runs`            | One row per run: `workflow_id`, status, trigger/execution source, idempotency key, times, cancellation |
| `workflow_run_steps`       | Ordered steps of a run: status, error code, retryability, `capability` and `adapter_capability`        |
| `workflow_step_attempts`   | Every attempt of a step, kept across retries                                                           |
| `source_response_captures` | Bounded, compressed source HTTP response bodies and request/run metadata                               |
| `workflow_leases`          | The single `global` lease (owner, expiry, generation) that fences source and media writes              |

`source_response_captures` records the textual response body presented to a
source parser, after HTTP content decoding and before parsing. It stores a
gzip-compressed body, its SHA-256, status, content type, normalized endpoint,
and the workflow run, step and attempt when available. Request query strings
and headers are not stored. A capture has no cascading run foreign key, so
removing workflow history does not remove its response. Successful HTTP
statuses are kept 30 days, error statuses 90 days, and the oldest captures are
removed when compressed bodies exceed 512 MiB. Bodies are limited to 4 MiB;
truncated captures are marked and their hash covers only the stored prefix.
The local `kano-go responses` command lists capture metadata and exports a
verified body; the HTTP API does not return capture bodies.

A run freezes how its source steps read: `source_sync_json` holds the
per-source sync ([source schedules](../workflows/source-schedules.md#source-schedules);
`{}` reads as each step's incremental default), `fetch_windows_json` an
operator's [manual fetch](../workflows/manual-fetches.md)
(its method and selection), `source_chain_json` the per-source
[method chain](../workflows/source-schedules.md#method-chains) (`{}` reads as each source's
default chain) and `source_lookback_json` the days a stored recent window
froze; new runs write `{}` there. A partial unique
index admits one queued or running run at a time. `capability` keeps its
constrained values (`synthetic`, `x_recent`, `capability_not_migrated`);
`adapter_capability` names the adapter that runs the step (`x_recent`,
`x_related`, `youtube_recent`, `bilibili_recent`, `bilibili_dynamics`, `shop_sales`,
`youtube_live`, `media_download`,
`schedule_extract`, `text_translate`, `media_variants` or `event_reconcile`), and a step with
neither finishes `blocked`. `recovery_count` counts how often a run was
queued again by a retry or by recovery from an expired lease. Finished runs
and their steps, attempts and `sync_runs` rows are deleted after their
[retention window](../workflows/run-execution.md).

`source_schedules` holds each source's timer
([source schedules](../workflows/source-schedules.md#source-schedules)): `source`
(`x`, `x_related`, `youtube`, `bilibili`, `bilibili_dynamics` or `shop`,
the primary key), `enabled`, `interval_minutes`, `sync_json`
(`{"x": SourceSync}`, `{"x_related": SourceSync}` or
`{"youtube": SourceSync}`; `{}` for the Bilibili and shop timers and as the
default, and a `count` setting of `x_related` reads as the default),
`chain_json` (X, YouTube and Bilibili uploads: `{methods: [{id, enabled}], apiGate,
apiDailyCap}`, the gate and cap for X only; `{}` for the related-accounts,
Bilibili dynamics and shop timers,
and a chain that is unreadable or does not
list every method of its source once reads as the default;
[method chains](../workflows/source-schedules.md#method-chains)), `next_run_at`
and `updated_at`. A missing row reads as that source's disabled default (see
[Source schedules](../workflows/source-schedules.md#source-schedules)). The `workflows` table and
its columns, including `source_sync_json` and `source_lookback_json`, stay in
the schema; nothing reads or writes them. Every new run has an ad hoc
`workflow_id` starting with `@`; a stored run whose `workflow_id` names a
saved workflow shows in the history with that ID, without joining
`workflows`.

## Sync State

`sync_state` rows are keyed by `source` and `account_id` and hold a cursor
(`cursor_id`, `cursor_time`), `last_success_at` and `metadata_json`. Besides
each source's incremental cursors and the fetch checkpoints (see
[Budgets and checkpoints](../workflows/manual-fetches.md#budgets-and-checkpoints)), source
`x-api` keeps the [X API chain](../sources/x.md#x-api-in-the-chain) state: one row
per X account with `{timelineFailures}`, its current streak of runs whose
timeline failed, and the row `@usage` with `{day, posts}`, the posts X API
answers returned on that Japan day. A count for an earlier day reads as
zero, and the first read of a new day starts it over. Source `x-related`
keeps one row per [related account](../sources/x-accounts.md#related-accounts), keyed by
the lower-case handle and written by the `x_related` step: the cursor of its
own timeline and
`{handle, discovered, listed, discovery, timelineGap, lastError}`.

The `bilibili` row of the account (the mid) keeps, besides the upload
read's device identity, clock and captcha backoff, the dynamics listing's
`dynamics` `{lastAttemptAt, lastSuccessAt, lastError}` and the cached WBI
signature keys `wbi` `{img, sub, at}`; the cursor names the newest upload.
The `bilibili-live` row of the same account keeps the
[live room](../sources/bilibili.md#live-room) state `{roomId, title, liveStatus,
liveTime, checkedAt, lastAttemptAt, blockedUntil, failures, lastError,
lastBroadcast, lastLiveAt, endedAt}`; the room ID is learned from the checks.
The `bilibili` row's metadata also keeps the device's `bNut` and
`bili_ticket` (with when it was issued and its lifetime) inside `session`,
`login` `{checkedAt, signedIn}` from the latest login check, and
`blockedAt`/`clearedAt`, when the current block was recorded and when the
latest successful read started, so a forced read's success lifts a block
while a block a concurrent read stored is kept. Neither is returned by a public read.

The `shop` row of account `shop.milpr.com` keeps the
[official shop](../sources/shop.md#official-shop)'s last successful read in
`last_success_at` and `{lastAttemptAt, blockedUntil, lastError}`, where
`blockedUntil` is when a 429 answer's `Retry-After` passes. The shop's
period groups are not kept here: their evidence is their `event_sources`
rows.
