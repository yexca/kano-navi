# Seeds, Schema Changes and Persistence

How seeds relate to synchronization, how the schema changes, and what the persistence package owns.

## Seed versus Synchronization

`backend/internal/store/sqlite/static-seed.json` holds the static profile,
milestone timeline, and resource directory. Every open inserts the static IDs
that are missing, plus the `dashboard_revision`, the default `x_accounts`
and the `llm_route_targets_migrated`, `llm_verify_queue_seeded` and
`llm_image_queue_seeded` markers when absent. `x_related_accounts` starts empty, and seeding leaves it as it is. New environments have no X posts, events, YouTube videos, Featured item, schedule assets, or
LLM providers/models/routes.
`kano-go seed` (`npm run seed`) creates or upgrades the database and fills
missing static IDs; `-overwrite` restores the shipped profile, timeline and
resource wording and increments the dashboard revision. Seeding never touches
source snapshots, events, media, or operator configuration and does not
recreate deleted providers. `kano-go sync` (`npm run sync`) reads public
sources and upserts updated records. Neither command silently deletes
existing snapshots or operator-configured providers.

Startup also updates a seeded timeline or resource row whose wording or order
still exactly matches an outdated shipped value; operator edits and ordering
are preserved. The 2010 entry moves after the first-album milestone only when
its original title, detail, and sort order still match.

Media assets have `retry_attempts` and `next_retry_at`. Failed downloads
preserve cached bytes and retry state on source rediscovery. Successful
downloads and 304 revalidations clear that state. See
[Media cache](../media-cache/lifecycle.md) for retry eligibility and bounds.

Timeline-window checkpoints keep `headFetchedAt` in `sync_state.metadata_json`
after a successful newest-page read. Resumed pages retain it, and cursor-only
cleanup preserves `last_success_at`. A checkpoint without a head time cannot
establish public source freshness.

An `events` table without `starts_on` (one source per event and no calendar
date) is rebuilt into `events` and `event_sources`: every row keeps its ID and
becomes its own source evidence, X schedule rows become manual and locked, and
YouTube reservations stay automatic. When startup adds `focus.video_id`,
each row receives it from the YouTube identity of its URL; a post without `account_handle`
receives the handle in its URL. Automatic, unlocked YouTube events with a
start time and `unknown` precision become `exact`. A video whose `kind` or
`manual_kind` is not one of the [video forms](video-form.md#video-form-and-tags) is
rewritten once, as that section describes. A post label stored in two
languages (`DAILY / 近况`, `SCHEDULE / 日程`) becomes its English text
(`DAILY`, `SCHEDULE`), and the dashboard revision advances. None of these
steps clears a snapshot.

## Schema Changes

Startup additively creates `posts_url_published_at_idx` on
`posts(url, published_at DESC)` for the correlated schedule-image source-text
lookup. Existing rows, reviews, manual labels, locks/tombstones, provider routes,
and settings remain in place. The lookup chooses the latest stored source
wording (including `raw_json.search_text`) and applies the existing text/image
and manual-review gates. The index is non-unique because multiple stored records
can share a source URL.

Startup also additively creates two partial post indexes for the board
snapshot: `posts_account_instant_idx` on
`posts(lower(COALESCE(account_handle, '')), julianday(published_at) DESC, id)`
over the public posts that are not promoted, which lists the accounts and each
account's newest posts, and `posts_bilibili_account_idx` on
`posts(account_handle)` over the Bilibili posts, which lists the Bilibili
accounts. Neither changes a stored row.

backend/internal/store/sqlite owns the schema (`bootstrap.sql`), the
additive upgrade behavior (`schema_upgrade.go`) and the workflow tables
(`workflow_migrations.go`). Update this
contract, seed behavior, and relevant tests together. Never silently drop
columns, rebuild an existing database from seed data, clear snapshots, or
recreate deleted providers. See [Database operations](../../operations/database.md).

## Implementation Ownership

### Persistence: `backend/internal/store/sqlite`

This package creates the SQLite schema, provides seed/upsert/query functions, and
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
`capabilities_json` and a protocol (`openai-responses`,
`openai-chat-completions`, or `anthropic-messages`). No schema rebuild is needed
to store Anthropic providers. The same capability and route rules apply to all
protocols.
`vision_capable` (API `visionCapable`) is derived from the `image`
capability; a provider write without `capabilities` may send `visionCapable`
instead. `llm_providers.model` is written empty and never read: providers have
no default model. Text schedule extraction reads `schedule`; image schedule
extraction and schedule-image review read `verify`, and translation/light tasks
read `translate`. Startup folds any per-stage `schedule_message`,
`schedule_board`, and `schedule_vision` targets into the text schedule queue in
that order, keeping the first position of a repeated provider + model pair,
and once (marker `llm_default_model_retired`) turns a target that followed a
provider's default model into a target naming that model. Once under each of
the markers `llm_verify_queue_seeded` and `llm_image_queue_seeded`, which new
databases start with, it copies the `schedule` queue's image-tagged targets in
their order into an empty `verify` queue, so an install whose image posts
read the text queue gets an image extraction queue; a `verify` queue with
targets is kept as it is.

`internal/httpapi/media.go` and `internal/store/sqlite/media.go` own cache-root
path validation and serving; `internal/store/sqlite/source_write.go` owns
source URL identities (see [Media cache](../media-cache/layout-and-model.md#database-model)). `internal/mediadownload` performs bounded downloads
from the X and YouTube image hosts and promotes verified files to ready cache
rows.
