# Data and Synchronization

## SQLite Snapshot

The database file is `data/kano.sqlite`. On startup the server creates the
directory, schema, and missing seed records. The current tables are:

| Table                  | Purpose                                                                 |
| ---------------------- | ----------------------------------------------------------------------- |
| `profiles`             | Name, bio, avatar, banner, and official entry points                    |
| `posts`                | X posts, publication time, engagement counts, and media                 |
| `events`               | Unified schedules, provenance, confidence, manual locks, and tombstones |
| `event_sources`        | Source post/reservation identities attached to each event               |
| `videos`               | Published YouTube videos and scheduled streams                          |
| `focus`                | The page's latest focus item and stable YouTube video ID                |
| `timeline`             | Person and activity timeline                                            |
| `resources`            | X, YouTube, Wikipedia, and other resource links                         |
| `assets`               | Schedule images and their sources                                       |
| `media_assets`         | Remote media identities, cache metadata, and fetch status               |
| `media_links`          | Links from cached media to posts, videos, profiles, and focus items     |
| `sync_runs`            | Status, counts, and error summaries for each synchronization            |
| `sync_state`           | Durable per-account X and YouTube cursors                               |
| `app_settings`         | Non-secret application settings such as the selected model name         |
| `schedule_extractions` | Versioned OpenAI inputs, outcomes, and structured result metadata       |

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

The script reads the public profile named by `X_HANDLE` (default `kano_2525`),
extracts status IDs, and decodes their Snowflake timestamps before making detail
requests. With no cursor, IDs older than `X_BOOTSTRAP_DAYS` (seven by default)
are skipped. Later runs prioritize unknown IDs and may refresh only a small
`X_REFRESH_KNOWN` budget. Discovery and detail-request limits cap the work per
run. The page displays posts from the most recent `days` window, three days by
default; older records may remain in SQLite.

Each discovered image URL is registered and linked to its post. A bounded media
stage later in the same command downloads pending files from `pbs.twimg.com`.

### YouTube

The script reads the RSS feed for `YOUTUBE_CHANNEL_ID` (the current main channel
by default). With no cursor, it stores the latest `YOUTUBE_BOOTSTRAP_VIDEOS`
entries (six by default). Later runs store only entries before the prior cursor,
or entries newer than its timestamp when that cursor has fallen out of the RSS
window. It separately inspects the streams page and keeps rechecking active or
recent reservations for `scheduledStartTime`, writing them to both `videos` and
`events`. When no schedule time is available, the script does not invent one.

Video thumbnails are registered and linked to their rows, then the bounded media
stage downloads pending files from `i.ytimg.com`.

### OpenAI Schedule Extraction

X posts whose normalized label starts with `SCHEDULE` are extraction candidates.
If `OPENAI_API_KEY` is absent, this stage skips without changing events. If it is
present, `server/schedule-extractor.js` sends the public post text and any ready
cached post images to the OpenAI Responses API. The request disables storage and
uses a strict JSON schema. A second local validator rejects invalid calendar
dates, time formats, enumerations, or confidence values before any event write.

Input fingerprints and the extractor version make successful results
idempotent. Model failures and invalid results are recorded in
`schedule_extractions` and preserve the prior event snapshot. Date-only events
store `starts_on`, a null `starts_at`, and `time_precision = unknown`; the model
must not invent a specific time.

The effective model comes from `app_settings.llm_model`, falling back to
`OPENAI_MODEL` and then `gpt-4o-mini`. Only the model name is stored in SQLite;
the API key remains in the process environment.

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

| Variable                      | Default       | Purpose                                                      |
| ----------------------------- | ------------- | ------------------------------------------------------------ |
| `APP_MODE`                    | Development   | `development` bypasses admin login; `production` requires it |
| `ADMIN_PASSWORD`              | Empty         | Production admin password, minimum 12 characters             |
| `OPENAI_API_KEY`              | Empty         | Optional Responses API credential, environment only          |
| `OPENAI_MODEL`                | `gpt-4o-mini` | Initial/fallback schedule extraction model                   |
| `OPENAI_TIMEOUT_MS`           | `30000`       | Timeout for one Responses API request                        |
| `PORT`                        | `8787`        | Express listening port                                       |
| `X_HANDLE`                    | `kano_2525`   | Public X account name                                        |
| `X_BOOTSTRAP_DAYS`            | `7`           | First-run X lookback in days                                 |
| `X_DISCOVERY_LIMIT`           | `50`          | Maximum discovered status IDs considered                     |
| `X_MAX_STATUS_REQUESTS`       | `12`          | Maximum X detail requests per run                            |
| `X_REFRESH_KNOWN`             | `1`           | Known X posts refreshed per run                              |
| `YOUTUBE_CHANNEL_ID`          | Main channel  | YouTube channel identifier                                   |
| `YOUTUBE_BOOTSTRAP_VIDEOS`    | `6`           | First-run RSS entries stored                                 |
| `YOUTUBE_MAX_DETAIL_REQUESTS` | `12`          | Reservation pages inspected per run                          |
| `SYNC_TIMEOUT_MS`             | `7000`        | Timeout for a source network request                         |
| `SYNC_REQUEST_DELAY_MS`       | `150`         | Delay between source detail requests                         |
| `MEDIA_DOWNLOAD_LIMIT`        | `20`          | Pending images attempted per run                             |
| `MEDIA_DOWNLOAD_TIMEOUT_MS`   | `10000`       | Timeout for an image request                                 |
| `MEDIA_MAX_BYTES`             | `10485760`    | Maximum bytes accepted for one image                         |
| `SKIP_X`, `SKIP_YOUTUBE`      | `0`           | Set an individual source flag to `1` to skip it              |
| `SKIP_MEDIA`, `SKIP_LLM`      | `0`           | Set a post-processing stage flag to `1` to skip it           |

`.env.example` is the complete non-secret inventory. `ADMIN_PASSWORD` and
`OPENAI_API_KEY` are credentials; never place real values in source code,
SQLite, URLs, API responses, or logs.

## Manual Maintenance

Use `/admin` instead of direct SQL for event maintenance. Creating, editing,
confirming, or deleting through that API records manual precedence and preserves
source links. Direct database edits can omit the lock/tombstone rules and should
be reserved for reviewed recovery work.
