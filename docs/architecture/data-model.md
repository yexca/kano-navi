# Data Model

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
| `workflows`              | Saved step selections, rolling source windows, timer settings, and last/next run metadata                 |

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

`server/seed-data.ts` initializes the static profile, milestone timeline, and
resource directory. New environments have no X posts, events, YouTube videos,
Featured item, schedule assets, or LLM providers/models/routes. `npm run seed`
only fills missing static IDs by default; it does not recreate deleted providers.
`npm run sync` reads public sources and upserts updated records. Neither command
silently deletes existing snapshots or operator-configured providers.

Legacy `events` and `focus` tables are migrated in place. Existing X schedule
rows are treated as manually curated and locked; existing YouTube reservations
remain automatic. The migration does not clear snapshots.

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

## Schema Changes

server/database.ts owns the schema and additive upgrade behavior. Update this
contract, seed behavior, and relevant tests together. Never silently drop
columns, rebuild an existing database from seed data, clear snapshots, or
recreate deleted providers. See [Database operations](../operations/database.md).

## Implementation Ownership

### Persistence: `server/database.ts`

This module creates the SQLite schema, provides seed/upsert/query functions, and
keeps the database at `data/database/kano.sqlite`. Raw source data may be retained in
internal `raw_json`, but API mapping removes it. Writes should use the existing
transaction and upsert patterns so a partial synchronization cannot erase known
records.

Automatic event writes have lower precedence than `manual_locked` records.
Manual edits and confirmations lock the row; manual deletion keeps a hidden
tombstone so a later extraction cannot recreate the same event. LLM cancellation
is a separate evidence overlay: it may mark an existing automatic or manual
event as `llm_suspected`, but it never deletes the row or changes its core
schedule fields. A human can clear that overlay or set `manual_confirmed` with
a reason.

LLM providers store an explicit `text`/`image` capability array in
`capabilities_json`; `vision_capable` remains a derived migration field for old
clients. The provider routes `schedule_board`, `schedule_message`, and legacy
`schedule_vision` each have their own priority order.

`server/media-cache.ts` owns cache-root path validation, source URL identities,
content hashes, and atomic file writes. `server/media-downloader.ts` performs
bounded downloads from the X and YouTube image hosts and promotes verified
files to ready cache rows.

## Related Docs

- [Sources and extraction](sources.md)
- [Workflow source windows](workflows.md#fetch-windows-and-historical-backfill)
- [Media cache](media-cache.md)
