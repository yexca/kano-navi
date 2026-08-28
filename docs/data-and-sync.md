# Data and Synchronization

## SQLite Snapshot

The database file is `data/kano.sqlite`. On startup the server creates the
directory, schema, and missing seed records. The current tables are:

| Table | Purpose |
| --- | --- |
| `profiles` | Name, bio, avatar, banner, and official entry points |
| `posts` | X posts, publication time, engagement counts, and media |
| `events` | X schedules and platform reservations |
| `videos` | Published YouTube videos and scheduled streams |
| `focus` | The page's “Latest focus” item |
| `timeline` | Person and activity timeline |
| `resources` | X, YouTube, Wikipedia, and other resource links |
| `assets` | Schedule images and their sources |
| `media_assets` | Remote media identities, cache metadata, and fetch status |
| `media_links` | Links from cached media to posts, videos, profiles, and focus items |
| `sync_runs` | Status, counts, and error summaries for each synchronization |

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

## Current Sources

### X

The script reads the public profile named by `X_HANDLE` (default `kano_2525`),
extracts status IDs, and calls a public status JSON service to normalize text,
timestamps, engagement counts, and media URLs. The page displays posts from the
most recent `days` window, three days by default; older records may remain in
the database.

Each discovered image URL is registered as a pending media candidate and linked
to its owner. Registration is metadata-only in the current phase.

### YouTube

The script reads the RSS feed for `YOUTUBE_CHANNEL_ID` (the current main channel
by default) and stores recent videos. It then inspects the channel streams page
and video pages for `scheduledStartTime`, writing future streams to both
`videos` and `events`. When no schedule time is available, the script does not
invent one.

Video thumbnails are registered as pending media candidates and linked to their
video rows. A later cache worker can download them without changing the source
adapter.

## Failure and Freshness

- Each run starts a `sync_runs` record and finishes with `success`, `partial`, or `failed`.
- A source failure does not clear another source or any existing snapshot; errors are available for maintainer diagnosis.
- The page shows the latest completion time, but that is not the same as real-time data. Users should follow the original platform link for confirmation.
- CI never performs live synchronization, so builds do not depend on platform networks, login walls, or rate limits.

## Environment Variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `X_HANDLE` | `kano_2525` | Public X account name |
| `YOUTUBE_CHANNEL_ID` | Current main channel ID | YouTube channel identifier |
| `SYNC_TIMEOUT_MS` | `7000` | Timeout for an individual network request |
| `SKIP_X` | Unset | Set to `1` to skip X |
| `SKIP_YOUTUBE` | Unset | Set to `1` to skip YouTube |
| `PORT` | `8787` | Express listening port |

These are public-source or local runtime parameters, not credentials. If an
authenticated API is added later, reassess its trust boundary and storage
strategy instead of reusing the snapshot fields for tokens.

## Manual Maintenance

When correcting an event or focus item, prefer updating traceable seed/source
data and recording its source URL. Do not make unreproducible edits directly in
a production database. If a manual override is necessary, add an explicit
override field or file and document its precedence, expiry, and rollback rules.
