# Data Model

## SQLite Snapshot

The database file is `data/database/kano.sqlite`. On startup the server creates the
directory, schema, and missing seed records. The current tables are:

The development Go service can now do fresh initialization with `-initialize`
using its embedded canonical SQL and public static seed. It retains existing
current-schema records when reseeded, validates unknown schemas before writes,
and keeps external snapshots and LLM catalogs empty. Full legacy upgrades still
belong to the TypeScript initializer. See
[Go source workflows and media downloads](go-source-workflows.md).

| Table                    | Purpose                                                                                                |
| ------------------------ | ------------------------------------------------------------------------------------------------------ |
| `profiles`               | Name, bio, avatar, banner, and official entry points                                                   |
| `posts`                  | X posts, account source, publication time, engagement counts, and media                                |
| `events`                 | Unified schedules, provenance, confidence, manual locks, tombstones, and duplicate merges              |
| `event_sources`          | Source post/reservation identities attached to each event                                              |
| `videos`                 | YouTube videos and streams, Bilibili uploads and their merges, corrections, and refresh requests       |
| `video_revisions`        | Titles and thumbnails a platform replaced, one row per change                                          |
| `focus`                  | The page's latest focus item and stable YouTube video ID                                               |
| `timeline`               | Person and activity timeline                                                                           |
| `resources`              | X, YouTube, Wikipedia, and other resource links                                                        |
| `assets`                 | Schedule images and their sources                                                                      |
| `media_assets`           | Remote media identities, cache metadata, and fetch status                                              |
| `media_links`            | Links from cached media to posts, videos, profiles, and focus items                                    |
| `media_versions`         | Earlier cached files of an asset whose bytes a revalidation replaced                                   |
| `sync_runs`              | Status, counts, error summaries, trigger, and asynchronous job identity                                |
| `sync_state`             | Durable per-account X, YouTube and Bilibili cursors and read bookkeeping                               |
| `app_settings`           | Model, schedule-extractor, X-account, Featured, and YouTube push settings (push secret sealed)         |
| `schedule_extractions`   | Versioned LLM inputs, outcomes, and structured result metadata                                         |
| `post_llm_states`        | Per-post LLM status, last attempt, error, and reprocess queue state                                    |
| `schedule_asset_reviews` | Per-schedule-image LLM verdict, skip reason (`no_image` for a URL-less row), and optional manual label |
| `llm_providers`          | OpenAI-compatible or Anthropic endpoints, Text/Image capabilities, health, and encrypted keys          |
| `llm_models`             | Per-provider model catalog with tags (`text`/`image` routing; `reasoning`/`tools`/`embedding` labels)  |
| `llm_route_targets`      | The ordered provider + model failover queue (`route = 'schedule'`) shared by every extraction stage    |
| `llm_route_providers`    | Legacy provider-only routes, copied once into `llm_route_targets` and no longer written                |
| `workflows`              | Saved step selections, rolling source windows, timer settings, and last/next run metadata              |
| `translations`           | Manual, platform, and machine translations of post text and video titles per target locale             |
| `translation_states`     | Per-post/video machine translation state: source hash and language, last attempt, operator request     |

The API maps snake_case columns to camelCase and removes `raw_json`. The
frontend must not depend on database fields that are not declared in the API
response.

The development Go executor adds `workflow_migrations`, `workflow_runs`,
`workflow_run_steps`, `workflow_step_attempts` and `workflow_leases` through
atomic additive upgrades. Schema version 2 freezes a run's source windows in
`source_lookback_json` and records real adapters in `adapter_capability` while
preserving the v1 capability column and all prior attempts. These tables are
optional for ordinary TypeScript snapshots and Go public reads. See
[Go source workflows and media downloads](go-source-workflows.md) for ownership,
lease fencing, compatibility, identity repair and verification.

Remote media URL columns are retained as migration-era source fields. The
dashboard maps them through `media_assets`: a ready file becomes
`/media/<opaque-id>`, a tracked static file remains `/assets/...`, and an
unready remote file becomes `null` with an explicit status.

See [Media cache](media-cache.md) for the file layout, lifecycle, and downloader
boundary.

## Seed versus Synchronization

`backend-go/internal/store/sqlite/static-seed.json` initializes the static
profile, milestone timeline, and resource directory. New environments have no X posts, events, YouTube videos,
Featured item, schedule assets, or LLM providers/models/routes. `kano-go seed` (`npm run seed`)
only fills missing static IDs by default; it does not recreate deleted providers.
`kano-go sync` (`npm run sync`) reads public sources and upserts updated records. Neither command
silently deletes existing snapshots or operator-configured providers.

Startup also fills missing static IDs, including the first-album milestone.
The 2010 seed moves after that milestone only when its original title, detail,
and sort order still match; operator edits and ordering are preserved.

Media assets add `retry_attempts` and `next_retry_at` through additive startup
upgrades. Failed downloads preserve cached bytes and retry state on source
rediscovery. Successful downloads and 304 revalidations clear that state.
See [Media cache](media-cache.md#lifecycle) for retry eligibility and bounds.

Timeline-window checkpoints keep `headFetchedAt` in `sync_state.metadata_json`
after a successful newest-page read. Resumed pages retain it, and cursor-only
cleanup preserves `last_success_at`. Legacy checkpoints without a head time
cannot establish public source freshness.

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

A duplicate of the same activity is hidden by a merge decision rather than
deleted, with this precision rank: manual > YouTube reservation > X exact time

> X approximate time > date-only. Manual or locked events are never merged
> away but can receive merges, so an operator edit always wins; editing or
> confirming a rule-merged event shows it again. See
> [Duplicate merging](#duplicate-merging).

X event IDs always include a hash of the date, time (or unknown), and normalized
title, independently of the number of events in a result. Matching evidence in
`event_sources` and the unique source tuple reuse existing IDs, including legacy
date IDs, manual locks, and tombstones. Identical activities across X posts share
an event; evidence attached by an old date-ID collision is reused only when it
matches the surviving row's source key, so re-extraction can separate those
activities. YouTube URLs use the video reservation identity. Replacing a source's
events and retiring its missing evidence happen in one transaction. An automatic
event retires only when no active source evidence remains.

Event and video ordering compares actual ISO instants rather than timestamp
strings. Date-only events follow timed events on the same Japan date without
inventing a start time; equal instants use event/video ID as the stable tie break.
The dashboard and paginated admin schedule apply this ordering. A linked YouTube
reservation with `manual_confirmed` cancellation is excluded from `nextStream`,
including after source refresh. An `llm_suspected` overlay remains review evidence
and does not definitively cancel its video.

Videos have additive `live_status` (`upcoming`, `live`, `ended` or null) and
`live_checked_at` columns, written by every successful watch-page check,
including the [live-status probe](sources.md#live-status-probe). The
dashboard projects only a sanitized `liveStatus`: `ended` always, `live` and
`upcoming` while younger than two probe intervals. Older snapshots receive
the columns empty on upgrade.

Selected profile-media rows have additive `active_sha256` and
`active_mime_type` columns describing the selected file independently of the
latest candidate download. `download_status` and `last_download_at` record the
latest attempt; `last_error` remains diagnostic. Every startup reconciles active
hash/MIME from the actual selected file, including databases previously upgraded
with candidate metadata. It preserves candidate metadata, the manual selection,
and unrelated snapshots/settings. Missing, unsafe, oversized, or unsupported
files leave active hash/MIME unavailable and cannot be served; a subsequent
startup can recover them after a valid selected file is restored. When active
hash/MIME changes, the repair increments the persistent `dashboard_revision`
once in the same transaction as all selected metadata updates. An unchanged
startup does not increment it. Existing revision polling therefore reloads the
repaired public profile snapshot, including its versioned avatar URL.
Schedule image reviews add `llm_input_fingerprint` for content/configuration
cache validation; legacy reviews are rechecked when next scanned. Automatic
review writes update LLM columns only. Image responses compare the captured
`assets.asset_version`, URL and media hash inside a transaction; replacement,
including replacing and restoring the same URL, invalidates a waiting response.
Manual verdicts, reasons and decision timestamps survive concurrent model work.

Schedule assets keep publication in `updated_at` separately from inclusive
`period_start` / `period_end`, with `period_basis` (`explicit`, `relative`,
`unknown`, or `legacy`). `week_start` is the Monday of the applicable start,
not the publication week. Additive migration copies existing week values to a
seven-day `legacy` period without asserting that those old dates were verified.
Manual correction stores separate `manual_period_start`, `manual_period_end`,
and `manual_period_reason`; it preserves the inferred values, source and review.
Registration of the same image retains that correction. A different image URL
resets its review and correction. Unknown periods stay null until corrected and
are not assigned to a calendar week.

Complete post `media_urls` snapshots reconcile `post-image` ownership in the
same transaction as the post write. An empty list removes all display links;
omitted media fields preserve the known declaration. Failed source reads never
call this writer. Cache rows/files may remain after a link is removed.

The dashboard reads a bounded current-week/focus selection, not all history.
Event totals and upcoming counts use SQL aggregates over all visible records;
post counts describe the requested date window, even if its preview exceeds
100 posts. Media cache totals aggregate recorded statuses; each displayed asset
is independently checked for file availability. Calendar pages contain at most
100 events in an inclusive range of at most 42 days (a Monday-first month
grid), total/day counts, the snapshot
revision and adjacent dates. Tombstones are excluded on every public path.

Additive indexes on `(source, source_item_id, id)` and locally normalized
`stream_identity(url)` support dashboard video/event lookups. The latter excludes
null URLs and uses the same deterministic YouTube/exact-URL identity rule as the
frontend. Source evidence uses the existing `event_sources` identity index.
Startup adds these indexes to existing snapshots without rewriting events,
manual locks, tombstones, cancellation evidence or operator settings.

## Stored Event Text

English is the base language of stored text. An event's `status` holds either a
lowercase English code the interface translates or an operator's note, shown as
written. YouTube synchronization writes `scheduled`, `live` or `ended`; an
extraction entry without a status, or with a cancellation-like one, stores
`pending`, and the model is asked for short English words such as `scheduled`,
`tentative` or `released`. Automatic `detail` text is English: `YouTube Live`
or `YouTube Premiere`, which the interface reads as codes and translates, or
the model's summary, shown as written. Titles keep their source wording.
The labels and the codes they cover live in `src/lib/event-text.ts`.

Earlier releases stored Chinese display text (`已预约`, `会员限定`, `直播中`,
`已完成`, `待确认` and `YouTube Live / 预约`-style details). Startup replaces it
with the codes and English wording on rows synchronization owns (automatic,
unlocked, not deleted) and increments the dashboard revision when a row
changes. Manual locks and tombstones keep their text; the frontend reads those
legacy values as the same codes and wording.

## Translations

Post text and video titles keep their source wording in `posts.text` and
`videos.title`. `translations` stores their translations for the target
locales `en` and `zh-CN` (Japanese readers see the source), keyed by
`(owner_type, owner_id, field, locale, origin, platform)`: `owner_type` is
`post` (field `text`) or `video` (field `title`), `origin` is `manual`,
`platform` or `machine`, and `platform` names the platform of a platform row
(`youtube`, `bilibili`) and is empty otherwise. One row per origin lets the
three coexist; readers resolve each locale by precedence: manual, then
platform (YouTube before Bilibili, then any other), then machine.

Every row stores `source_hash`, the SHA-256 of the trimmed source wording it
translates; SQL compares it through the deterministic `translation_hash()`
function. A machine row counts only while its hash matches the current
wording, so a changed post or title retires it until the translate step
replaces it. A manual row always counts; the console flags it when the
wording changed after it was written. Platform rows are owned by the source
writers that store them (`UpsertPlatformTranslationTx`,
`DeletePlatformTranslationTx` in `backend-go/internal/store/sqlite/translations.go`),
which keep them current and remove them when the platform no longer provides
one; readers do not compare their hash. Automatic writers never change a
manual row, and every write that changes what readers see increments the
dashboard revision.

`translation_states` holds one row per post or video the machine step has
seen: the hash and detected `source_language` (`ja`, `en`, `zh`, `ko`,
`other`, or `none` when there is nothing to translate) of the wording it last
attempted, its `status` (`success`, `failed`, `skipped`), the last attempt and
sanitized error, and `requested`, an operator's request for a forced pass. A
locale whose language matches the detected source language needs no
translation, and `none` needs none at all. A post or video is complete when
every locale it needs has a usable row. The public dashboard, archives and
video lists add `translations` (`{locale: {text, origin, platform}}`) and
`sourceLanguage` to posts and videos; providers, models and hashes stay in
the console. A Bilibili video, which the machine step never translates,
reports `sourceLanguage` `zh`. A video's translations follow its effective
title, `COALESCE(manual_title, title)`, so an operator's title correction
retires its machine translations.

Events have no translation rows of their own. A board or calendar event
whose URL names a YouTube video in the translate step's scope (by
`stream_identity`) carries that video's `translations` and `sourceLanguage`
while its trimmed title equals the video's effective title; an event with
an operator's or an extraction's own wording carries none. The projection is
computed at read time, so a video translation's revision already covers it.

Startup creates both tables in existing snapshots; no post, video or event
row is rewritten.

Translation model attempts also record bounded diagnostics in
`sync_runs.counts_json` under `translate.diagnostics`: configured model and
provider, owner reference, attempt and output budget, known stop reason,
numeric token usage and a fixed error category. These are protected run
metadata, not public translation fields. The payload contains no provider
response text or reasoning; see [Translation](sources.md#translation).

## Duplicate Merging

The additive `events` columns `merged_into` (target event ID), `merge_source`,
`merge_reason` and `merged_at` record that an event duplicates another. They
are a separate durable decision: the X upsert and the YouTube writer never
name them, so re-extraction, retirement and relisting of the source row, or a
reservation refresh, keep the merge. `merge_source` names the owner:

- `rule`: the deterministic `reconcile` workflow step
  (`backend-go/internal/store/sqlite/event_merge.go`). It recomputes its own
  decisions from the current rows on every run, so it is idempotent and
  releases a merge whose target was deleted, merged or moved to another
  date, or whose event became manual or locked. Deleting an event releases
  the rule merges into it in the same transaction, so a duplicate older than
  the reconciler's window is shown again; other owners' merges stay.
- `manual`: an operator decision. `merge_source = manual` with a null
  `merged_into` is an undone merge; the reconciler never merges that event
  again.
- Any other owner (reserved for a later model adjudication) is left untouched
  by the rules in the same way.

The rules consider active, non-deleted `stream`/`member` events from 30 days
before to 60 days after the current Japan date:

- Rule B: an automatic X event with an exact or approximate time merges into
  the only compatible YouTube event on the same date that starts within 30
  minutes. The stored time precision stays unchanged. A generic stream may
  merge into a members-only stream, not the reverse.
- Rule A (after Rule B): a date-only automatic event merges into the timed
  stream on the same date whose normalized title overlaps its own (one
  contains the other or they share a token of at least two characters after
  punctuation, brackets and medium words such as 配信 are removed). Without a
  title overlap it merges only when the date has exactly one date-only event
  and one timed stream.
- Several plausible targets leave the event unmerged and are counted as
  `ambiguous`. A cancellation overlay (`llm_suspected` or `manual_confirmed`)
  on either side freezes the current decision.

Every public reader excludes merged rows: the dashboard events, totals,
upcoming counts and summary/spotlight candidates, calendar items, totals,
pagination, day counts and adjacent dates, and MCP `schedule_list`. The
operator console lists them with their target and can undo a merge. Startup
adds the columns and the partial `events_merged_into_idx` index to existing
snapshots without rewriting events.

## Video Corrections and History

Synchronization owns a video's platform fields (`title`, `kind`,
`thumbnail_url` and its media link) and keeps rewriting them on every
successful page read. An operator correction lives beside them in
`manual_title`, `manual_kind`, `manual_is_short` and
`manual_thumbnail_media_id` / `manual_thumbnail_sha256` with
`manual_updated_at`; synchronization never
writes these columns, and clearing one shows the platform value again. Every
public reader (dashboard, video archive, MCP) and the console show
`COALESCE(manual, platform)`, and a pinned thumbnail is served while its file
is still cached, otherwise the platform thumbnail. Machine translations
follow the corrected title. A correction is not an event lock:
reservation events keep their own provenance and manual edits.

Nothing the platform replaces is discarded. A page read that reports a new
title or thumbnail image inserts the previous `title`, `thumbnail_url`, `kind`
and the then-current thumbnail file (`thumbnail_media_id`,
`thumbnail_sha256`) into `video_revisions` with `origin = sync` and
`replaced_at`. For YouTube, the sizes, formats and numbered hosts of one
video's `*.ytimg.com` thumbnail are the same image (`ytsource.SameImage`),
so the Data API's `hqdefault` and the watch page's `og:image` record no
revision; a live placeholder (`*_live`) or a different `?v=` version on both
URLs is a new image. History pages fill a missing thumbnail but never replace
a stored one. A thumbnail revalidation that downloads new bytes for the same
URL keeps the old file in `media_versions` and records a `media` revision for
every linked video. `duration_seconds` and `refresh_requested_at` are
described in [Sources](sources.md#follow-up-checks-and-refresh-requests).
Public readers (dashboard, video archive, MCP) add the length as
`durationSeconds`, null while it is unknown.

`is_short` is the YouTube feed's format verdict: 1 for a Short, 0 for any
other upload, `NULL` while unknown (see [Sources](sources.md#shorts)).
`manual_is_short` is the operator's correction with the same values and
`NULL` for none; synchronization never writes it. Public readers add
`isShort = COALESCE(manual_is_short, is_short)` as `true`, `false` or `null`,
and the board treats only `true` as a Short. Older rows stay unknown rather
than being guessed from their length.
`is_premiere` is the stored premiere verdict of a YouTube reservation: 1 for
a premiere (a `release` event), 0 for live content, `NULL` while no check
could tell, which includes every row stored before the column existed (see
[Sources](sources.md#youtube)). It is never derived from the kind.
Startup adds the columns, both tables and `video_revisions_video_idx` to
existing snapshots without rewriting videos or media rows.

## Video Merging

A Bilibili upload of the same work as a YouTube video is attached to it, so
readers list one video with both links. The additive `videos` columns
`merged_into` (the YouTube video's ID), `merge_source`, `merge_reason` and
`merged_at` hold the decision on the Bilibili row. No source writer names
them, so rereading either platform keeps the merge. `merge_source` names the
owner as for [event merges](#duplicate-merging):

- `rule`: the `reconcile` workflow step
  (`backend-go/internal/store/sqlite/video_merge.go`), which recomputes its
  decisions from the current rows on every run and releases a merge whose
  basis no longer holds. `merge_reason` is `rule:time_duration_title`.
- `manual`: an operator merge (`merge_reason` `manual`), which the rule never
  changes. `manual` with a null `merged_into` is an undone merge
  (`unmerged_from:<id>`); the rule never merges that upload again.

An upload merges by rule into a YouTube video only when all of these hold:

- the YouTube anchor (`scheduled_at`, else `published_at`) is within six
  hours of the upload's `published_at`;
- both `duration_seconds` are known and differ by at most three seconds;
- the song titles match. A Bilibili title is reduced to its `《…》` name (or,
  without one, the title less `【…】` labels) and a YouTube title to the part
  before `（`/`(`, `-`, `歌ってみた` or `【`, after leading `【…】` labels.
  Full-width ASCII is folded, punctuation and brackets split the rest, and
  parts that only name the performer or format (`鹿乃`, `翻唱`, `cover`,
  …) are dropped. The names match when they are equal, however short (`奏`),
  or share a part of at least two characters.

Effective titles (`COALESCE(manual_title, title)`) are compared. Several
complete matches, or one video claimed by several uploads, leave the upload
unmerged and are counted as `ambiguous`. A YouTube video receives at most
one upload: a video an operator merged an upload into is no rule target. A
video read before lengths were stored has no `duration_seconds`, so its
upload is left to the operator. A Short of the same song, published another
day or with another length, never matches.

Whenever a merge is written or a merged upload's effective title changes (a
Bilibili read, an operator correction or the reconcile step), the upload's
title is stored in the same transaction as the YouTube video's `zh-CN`
platform translation with platform `bilibili`; a release or undo removes it.
A change of either moves the dashboard revision. The machine translation
scope excludes merged rows.

Public readers keep `source = 'youtube'`: the dashboard and video archive add
`alternates` (`[{source, url}]`) to each YouTube video for its merged
uploads, and Bilibili rows themselves stay in the console. Startup adds the
columns and the partial `videos_merged_into_idx` index to existing snapshots
without rewriting videos.

## Schema Changes

Startup additively creates `posts_url_published_at_idx` on
`posts(url, published_at DESC)` for the correlated schedule-image source-text
lookup. Existing rows, reviews, manual labels, locks/tombstones, provider routes,
and settings remain in place. The lookup still chooses the latest stored source
wording (including `raw_json.search_text`) and applies the existing text/image
and manual-review gates. The index is non-unique because multiple stored records
can share a source URL.

backend-go/internal/store/sqlite owns the schema (`bootstrap.sql`) and the
additive upgrade behavior (`schema_upgrade.go`). Update this
contract, seed behavior, and relevant tests together. Never silently drop
columns, rebuild an existing database from seed data, clear snapshots, or
recreate deleted providers. See [Database operations](../operations/database.md).

## Implementation Ownership

### Persistence: `backend-go/internal/store/sqlite`

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
protocols. The
`vision_capable` field remains a derived migration field for old
clients. Every stage shares one priority queue; the startup upgrade folds the
older per-stage `schedule_message`, `schedule_board`, and `schedule_vision`
targets into it in that order, keeping the first position of a repeated
provider + model pair. `post_llm_states.route` still records which detection
path (`schedule_message` or `schedule_board`) handled a post.

`internal/httpapi/media.go` and `internal/store/sqlite/media.go` own cache-root
path validation and serving; `internal/store/sqlite/source_write.go` owns
source URL identities. `internal/mediadownload` performs bounded downloads
from the X and YouTube image hosts and promotes verified files to ready cache
rows.

## Related Docs

- [Sources and extraction](sources.md)
- [Workflow source windows](workflows.md#fetch-windows-and-historical-backfill)
- [Media cache](media-cache.md)
