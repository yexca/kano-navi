# Data and Synchronization

## SQLite Snapshot

The database file is `data/database/kano.sqlite`. On startup the server creates the
directory, schema, and missing seed records. The current tables are:

| Table                  | Purpose                                                                   |
| ---------------------- | ------------------------------------------------------------------------- |
| `profiles`             | Name, bio, avatar, banner, and official entry points                      |
| `posts`                | X posts, account source, publication time, engagement counts, and media   |
| `events`               | Unified schedules, provenance, confidence, manual locks, and tombstones   |
| `event_sources`        | Source post/reservation identities attached to each event                 |
| `videos`               | Published YouTube videos and scheduled streams                            |
| `focus`                | The page's latest focus item and stable YouTube video ID                  |
| `timeline`             | Person and activity timeline                                              |
| `resources`            | X, YouTube, Wikipedia, and other resource links                           |
| `assets`               | Schedule images and their sources                                         |
| `media_assets`         | Remote media identities, cache metadata, and fetch status                 |
| `media_links`          | Links from cached media to posts, videos, profiles, and focus items       |
| `sync_runs`            | Status, counts, error summaries, trigger, and asynchronous job identity   |
| `sync_state`           | Durable per-account X and YouTube cursors                                 |
| `app_settings`         | Non-secret model, schedule-extractor, X-account, and Featured settings    |
| `schedule_extractions` | Versioned OpenAI inputs, outcomes, and structured result metadata         |
| `llm_providers`        | OpenAI-compatible endpoints, capability flags, health, and encrypted keys |
| `llm_route_providers`  | Ordered provider failover routes (including `schedule_vision`)            |

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
and returned as one time-ordered feed. `X_HANDLE` remains a single-account
compatibility fallback.

Each discovered image URL is registered and linked to its post. A bounded media
stage later in the same command downloads pending files from `pbs.twimg.com`.
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
stage downloads pending files from `i.ytimg.com` or its official numbered CDN
hosts (`i1.ytimg.com` through `i4.ytimg.com`).

### Schedule Extraction Pipeline

Schedule extraction is independent of the public feed's old `notice/daily`
classification. Stage one uses the admin-configurable keyword list to select X
post candidates. Stage two sends the public post text and any ready cached
images to the ordered `schedule_vision` provider route. Each provider can use
the OpenAI Responses or Chat Completions shape, and a failed request is retried
according to its bounded setting before the next provider is attempted. A
second local validator rejects invalid calendar dates, time formats,
enumerations, or confidence values before any event write.

Input fingerprints and the extractor version make successful results
idempotent. Model failures and invalid results are recorded in
`schedule_extractions` and preserve the prior event snapshot. Date-only events
store `starts_on`, a null `starts_at`, and `time_precision = unknown`; the model
must not invent a specific time.

The legacy model comes from `app_settings.llm_model`, falling back to
`OPENAI_MODEL` and then `gpt-4o-mini`. The legacy environment key remains a
compatibility fallback for the seeded `openai-default` provider. Keys entered
for additional providers are encrypted with `LLM_SECRETS_KEY`; ciphertext is
never returned by an API response or written to extraction `raw_json`.
Candidates from either X account use the same idempotent fingerprint and
manual-lock rules. The two stage flags are stored as
`schedule_keyword_enabled` and `schedule_vision_enabled` and can be changed
independently in `/admin`.

The `featured_video_id` setting is maintained through `/admin`. It points to an
existing local `videos` row (or an empty value for no Featured item), and source
synchronization never changes it. The dashboard includes the selected video
even when it falls outside the most recent 30 rows.

## Event Precedence

Automatic X extraction and YouTube reservations use `provenance = automatic`.
Creating, editing, or confirming an event through `/admin` changes it to
`provenance = manual` and sets `manual_locked = 1`. Automatic upserts include a
database-level lock condition and cannot replace those rows. Manual deletion is
a soft-delete tombstone with the same lock, so synchronization cannot resurrect
it. The dashboard hides tombstones and labels visible automatic/manual events.

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
| `OPENAI_API_KEY`              | Empty                | Optional Responses API credential, environment only          |
| `OPENAI_MODEL`                | `gpt-4o-mini`        | Initial/fallback schedule extraction model                   |
| `OPENAI_TIMEOUT_MS`           | `30000`              | Timeout for one Responses API request                        |
| `LLM_SECRETS_KEY`             | Empty                | Environment-only master key for encrypted provider API keys  |
| `PORT`                        | `8787`               | Express listening port                                       |
| `SCHEDULE_EXTRACTION_ENABLED` | `1`                  | Enable the automatic schedule stage                          |
| `SCHEDULE_KEYWORD_ENABLED`    | `1`                  | Enable keyword candidate selection                           |
| `SCHEDULE_VISION_ENABLED`     | `1`                  | Enable visual LLM extraction                                 |
| `SCHEDULE_KEYWORDS`           | `schedule,...`       | Comma-separated schedule candidate keywords                  |
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
`OPENAI_API_KEY`, `LLM_SECRETS_KEY`, and `MCP_CONTROL_TOKEN` are credentials;
never place real values in source code, SQLite, URLs, API responses, or logs.

## Manual Maintenance

Use `/admin` instead of direct SQL for event maintenance. Creating, editing,
confirming, or deleting through that API records manual precedence and preserves
source links. Direct database edits can omit the lock/tombstone rules and should
be reserved for reviewed recovery work.

The `/mcp` endpoint is intentionally narrower than `/admin`. Its public tools
read the prepared snapshot and sanitized schedule/status views. Bearer-protected
tools may request a revision, start a full synchronization, or run the automatic
keyword/vision scan; all of these return an asynchronous job result. MCP has no
manual confirmation, edit, delete, profile-media selection/upload, SQL, file, or
arbitrary URL-proxy operation. A page refresh or revision request never performs
an external fetch.
