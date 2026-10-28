# Data Model

## SQLite Snapshot

The database file is `data/database/kano.sqlite`. `kano-go serve`, `seed` and
`sync` open it through `OpenStandalone`
(`backend-go/internal/store/sqlite/schema_upgrade.go`), which creates the
directory and then, in one transaction:

- gives a file without tables the canonical schema embedded from
  `bootstrap.sql`;
- upgrades an existing snapshot to that schema. A snapshot must contain
  `profiles`, `posts`, `events`, `videos`, `app_settings` and `sync_runs`;
  anything else is rejected before any write. Missing tables, columns (with
  their declared defaults) and indexes are added, and the backfills described
  in this document run. A required column without a default fails the open
  instead of being guessed. The only rebuild is described in
  [Seed versus Synchronization](#seed-versus-synchronization); no column is
  dropped and no snapshot is cleared;
- verifies the required columns and inserts missing static seed records.

The [workflow tables](#workflow-tables) are then created or verified in their
own transaction, and the selected profile media is reconciled with its files
(see [Event Precedence](#event-precedence)). The tables are:

| Table                    | Purpose                                                                                                |
| ------------------------ | ------------------------------------------------------------------------------------------------------ |
| `profiles`               | Name, bio, avatar, banner, and official entry points                                                   |
| `profile_media`          | Avatar and banner candidates, their downloads, and the operator's selected file                        |
| `posts`                  | X posts, account source, publication time, engagement counts, media, and the quoted status             |
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
| `post_llm_states`        | Per-post LLM status, last attempt, error, reprocess queue state, and extraction failure backoff        |
| `schedule_asset_reviews` | Per-schedule-image LLM verdict, skip reason (`no_image` for a URL-less row), and optional manual label |
| `llm_providers`          | OpenAI-compatible or Anthropic endpoints, Text/Image capabilities, health, and encrypted keys          |
| `llm_models`             | Per-provider model catalog with tags (`text`/`image` routing; `reasoning`/`tools`/`embedding` labels)  |
| `llm_route_targets`      | The ordered provider + model failover queue (`route = 'schedule'`) shared by every extraction stage    |
| `llm_route_providers`    | Provider-only routes that startup copies once into `llm_route_targets`; nothing else writes them       |
| `source_schedules`       | Each source's timer and sync setting                                                                   |
| `workflows`              | Saved workflow definitions (ADR-0003): kept in the schema, created empty, never read or written        |
| `translations`           | Manual, platform, and machine translations of post text and video titles per target locale             |
| `translation_states`     | Per-post/video machine translation state: source hash and language, last attempt, operator request     |

The API maps snake_case columns to camelCase and removes `raw_json`. The
frontend must not depend on database fields that are not declared in the API
response.

Remote media URL columns (`posts.media_url`, `videos.thumbnail_url`,
`focus.image_url` and the like) keep the addresses the source reported; they
are never browser URLs. The dashboard maps them through `media_assets`: a
ready file becomes `/media/<opaque-id>`, a tracked static file remains
`/assets/...`, and an unready remote file becomes `null` with an explicit
status. See [Media cache](media-cache.md) for the file layout, lifecycle, and
downloader boundary.

### Workflow Tables

`backend-go/internal/store/sqlite/workflow_migrations.go` creates the durable
run queue with `CREATE TABLE IF NOT EXISTS` and adds missing columns, then
checks that every table has the columns its recorded version requires:

| Table                    | Purpose                                                                                                |
| ------------------------ | ------------------------------------------------------------------------------------------------------ |
| `workflow_migrations`    | Applied workflow schema versions (current: 3); a newer recorded version fails the open                 |
| `workflow_runs`          | One row per run: `workflow_id`, status, trigger/execution source, idempotency key, times, cancellation |
| `workflow_run_steps`     | Ordered steps of a run: status, error code, retryability, `capability` and `adapter_capability`        |
| `workflow_step_attempts` | Every attempt of a step, kept across retries                                                           |
| `workflow_leases`        | The single `global` lease (owner, expiry, generation) that fences source and media writes              |

A run freezes how its source steps read: `source_sync_json` holds the
per-source sync ([ADR-0002](../decisions/ADR-0002-count-based-source-sync.md);
`{}` reads as each step's incremental default), `fetch_windows_json` an
operator's [fetch windows](workflows.md#fetch-windows-and-historical-backfill)
and `source_lookback_json` the days of a recent window. A partial unique
index admits one queued or running run at a time. `capability` keeps its
constrained values (`synthetic`, `x_recent`, `capability_not_migrated`);
`adapter_capability` names the adapter that runs the step (`x_recent`,
`youtube_recent`, `bilibili_recent`, `youtube_live`, `media_download`,
`schedule_extract`, `text_translate` or `event_reconcile`), and a step with
neither finishes `blocked`. `recovery_count` counts how often a run was
queued again by a retry or by recovery from an expired lease.

`source_schedules` holds each source's timer
([ADR-0003](../decisions/ADR-0003-per-source-sync-schedules.md)): `source`
(`x`, `youtube` or `bilibili`, the primary key), `enabled`,
`interval_minutes`, `sync_json` (`{"x": SourceSync}` or `{"youtube":
SourceSync}`; `{}` for Bilibili and as the default), `next_run_at` and
`updated_at`. A missing row reads as that source's disabled default (see
[Source schedules](workflows.md#source-schedules)). The `workflows` table and
its columns, including `source_sync_json` and `source_lookback_json`, stay in
the schema; nothing reads or writes them. Every new run has an ad hoc
`workflow_id` starting with `@`; a stored run whose `workflow_id` names a
saved workflow shows in the history with that ID, without joining
`workflows`.

## Seed versus Synchronization

`backend-go/internal/store/sqlite/static-seed.json` holds the static profile,
milestone timeline, and resource directory. Every open inserts the static IDs
that are missing, plus the `dashboard_revision`, the default `x_accounts`
and the `llm_route_targets_migrated` marker when absent. New environments
have no X posts, events, YouTube videos, Featured item, schedule assets, or
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
[Media cache](media-cache.md#lifecycle) for retry eligibility and bounds.

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
start time and `unknown` precision become `exact`. None of these steps clears
a snapshot.

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
`upcoming` while younger than two probe intervals. Startup adds the columns
empty to existing snapshots.

Selected profile-media rows have additive `active_sha256` and
`active_mime_type` columns describing the selected file independently of the
latest candidate download. `download_status` and `last_download_at` record the
latest attempt; `last_error` remains diagnostic. Every startup reconciles active
hash/MIME from the actual selected file, whatever the stored values. It preserves candidate metadata, the manual selection,
and unrelated snapshots/settings. Missing, unsafe, oversized, or unsupported
files leave active hash/MIME unavailable and cannot be served; a subsequent
startup can recover them after a valid selected file is restored. When active
hash/MIME changes, the repair increments the persistent `dashboard_revision`
once in the same transaction as all selected metadata updates. An unchanged
startup does not increment it. Open boards therefore follow the revision and
reload the repaired public profile snapshot, including its versioned avatar
URL.
Schedule image reviews add `llm_input_fingerprint` for content/configuration
cache validation; a review without one is rechecked when next scanned. Automatic
review writes update LLM columns only. Image responses compare the captured
`assets.asset_version`, URL and media hash inside a transaction; replacement,
including replacing and restoring the same URL, invalidates a waiting response.
Manual verdicts, reasons and decision timestamps survive concurrent model work.

Schedule assets keep publication in `updated_at` separately from inclusive
`period_start` / `period_end`, with `period_basis` (`explicit`, `relative`,
`unknown`, or `legacy`). `week_start` is the Monday of the applicable start,
not the publication week. When startup adds the period columns, it copies
each schedule asset's `week_start` into a seven-day `legacy` period, which does
not assert that those dates were verified.
Manual correction stores separate `manual_period_start`, `manual_period_end`,
and `manual_period_reason`; it preserves the inferred values, source and review.
Registration of the same image retains that correction. A different image URL
resets its review and correction. Unknown periods stay null until corrected and
are not assigned to a calendar week.

Complete post `media_urls` snapshots reconcile `post-image` ownership in the
same transaction as the post write. An empty list removes all display links;
omitted media fields preserve the known declaration. Failed source reads never
call this writer. Cache rows/files may remain after a link is removed.

## Quoted Posts

A post that quotes another status keeps it in `posts.quote_json`, a
sanitized projection (`id`, `authorHandle`, `authorName`, `text`,
`publishedAt`, `url`, and `unavailable` for a quote the source names but
does not carry). The quoted status's images are `media_links` with the role
`quote-image` and the quoting post as owner; `quote_json` holds no image
address, and a post's `post-image` links, `media_url` and
`raw_json.mediaURLs` are its own images only. A post without a quote stores
`NULL`. See [Quoted posts](sources.md#quoted-posts) for how the X adapter
reads it and why quoted images are never schedule candidates.

The public dashboard, the post archive and MCP add `quote` to each post:
`{id, authorHandle, authorName, text, publishedAt, url, unavailable, media}`,
where empty fields are `null` and `media` has the shape of the post's own
`media` but lists only ready, contained opaque-ID images. The read path
drops a stored projection whose ID is not a status ID or whose URL is not
that status's x.com address. An unavailable quote has only `id` and `url`.
The console's post list keeps a post's own images apart from its quote.

`posts.quote_hidden_at` records an operator's decision to keep a post's
quote off the public pages: the dashboard, the post archive and MCP then
omit `quote` entirely, while the console still shows it. No synchronization
path, refresh or backfill writes this column, so the decision survives every
rewrite of `quote_json` and the `quote-image` links, like a manual lock.
`posts.refresh_requested_at` is a queued refresh from the source, cleared
once the X synchronization made its detail request (see
[Refresh from the source](sources.md#refresh-from-the-source)). Both are
additive columns, `NULL` by default.

Startup adds `quote_json` additively. It then fills it for every X post
whose `raw_json` names a quoted status (an object under `qrt`, `quote`,
`quotedTweet` or `quoted_tweet`, or a `qrtURL`) and still has no
projection, and moves quoted images stored as the post's own. Such a post
lists its own images first and the quoted status's after them, all as
`post-image`; a `post-image` link moves to
`quote-image` when its image is one of the quote's and not one the post's
`raw_json` declares as its own, so the remaining own links keep their
positions, and an image both declare stays the post's. `media_url` then
names the first own image or `NULL`. A post without own images took the
quote's first image as its schedule candidate, so a `schedule-x-<post id>`
asset whose image is one of the moved quoted images is removed with its
review and `schedule-image` link, unless an operator labelled it or set its
period. The step reads only rows without a
projection, so it is idempotent; it never clears a post or another link,
and it increments the dashboard revision when it changes a row. A timeline
page that lists a stored post again leaves it unchanged, so a stored post
without a projection gets its quote from this step; any later detail refresh
rewrites it from the source.

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

Startup replaces Chinese display text in stored events (`已预约`, `会员限定`,
`直播中`, `已完成`, `待确认` and `YouTube Live / 预约`-style details) with the
codes and English wording on rows synchronization owns (automatic, unlocked,
not deleted) and increments the dashboard revision when a row changes. Manual
locks and tombstones keep their text; the frontend reads those legacy values
as the same codes and wording.

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
one; readers do not compare their hash. YouTube synchronization writes the
`en` and `zh-CN` titles a creator localized (platform `youtube`, see
[Localized titles](sources.md#localized-titles)), and the
[video merge](#video-merging) writes a merged Bilibili upload's title
(platform `bilibili`). Automatic writers never change a
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
- `manual`: an operator decision (`merge_reason` `manual`), which the rules
  never change. `merge_source = manual` with a null `merged_into` is an undone
  merge; the reconciler never merges that event again.
- Any other owner is left untouched by the rules in the same way.

The rules consider active, non-deleted events from 30 days before to 60 days
after the current Japan date:

- Rule B: an automatic X `stream`, `member` or `release` with an exact or
  approximate time, or an X `event` (an entry that names no activity, such as
  「このあと20時からはじまります！」) with an exact time, merges into the only
  compatible YouTube event on the same date that starts within 30 minutes. A
  generic stream may merge into a members-only stream, not the reverse; a
  release only into a release; an `event` into a stream or members-only
  stream. The stored time precision stays unchanged.
- Rule A (after Rule B): a date-only automatic `stream`/`member` event merges
  into the timed stream on the same date whose normalized title overlaps its
  own (one contains the other or they share a token of at least two
  characters after punctuation, brackets and medium words such as 配信 are
  removed). Without a title overlap it merges only when the date has exactly
  one date-only event and one timed stream. A YouTube event never merges into
  an X event.
- Several plausible targets leave the event unmerged and are counted as
  `ambiguous`. A cancellation overlay (`llm_suspected` or `manual_confirmed`)
  on either side freezes the current decision.

Before the rules run, the same step derives YouTube events from the stored
videos whose start falls in that window, so the YouTube side of a match
exists and carries its time whichever source was read first:

- An automatic, unlocked YouTube event (`source = youtube`, the video's
  reservation identity) takes its start from its video with `exact`
  precision: the reservation's start, or the publication time of an upload or
  Short. An X post that links a video writes that event with only the post's
  date or announced time. An end before the new start is cleared.
- An ended broadcast with a start but without any event or evidence row for
  its video gets its reservation event, as the YouTube writer would write it
  (see [YouTube](sources.md#youtube)). A broadcast without a premiere verdict
  counts as a stream only when it lasted at least 30 minutes; a shorter one
  gets no event.

Manual locks and tombstones keep their rows, and no video gets a second
event identity.

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
and the board treats only `true` as a Short. A row without a verdict stays
unknown rather than being guessed from its length.
`is_premiere` is the stored premiere verdict of a YouTube reservation: 1 for
a premiere (a `release` event), 0 for live content, `NULL` while no check
could tell, which is also what the upgrade gives existing rows (see
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
video without `duration_seconds` leaves its upload to the operator. A Short of the same song, published another
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
and settings remain in place. The lookup chooses the latest stored source
wording (including `raw_json.search_text`) and applies the existing text/image
and manual-review gates. The index is non-unique because multiple stored records
can share a source URL.

backend-go/internal/store/sqlite owns the schema (`bootstrap.sql`), the
additive upgrade behavior (`schema_upgrade.go`) and the workflow tables
(`workflow_migrations.go`). Update this
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
protocols.
`vision_capable` (API `visionCapable`) is derived from the `image`
capability; a provider write without `capabilities` may send `visionCapable`
instead. `llm_providers.model` is written empty and never read: providers have
no default model. Every stage shares one priority queue. Startup folds any
per-stage `schedule_message`, `schedule_board`, and `schedule_vision` targets
into it in that order, keeping the first position of a repeated provider +
model pair, and once (marker `llm_default_model_retired`) turns a target
that followed a provider's default model into a target naming that model.
`post_llm_states.route` records which detection path (`schedule_message` or
`schedule_board`) handled a post.

`post_llm_states.failure_count` counts consecutive failed extractions of the
input named by `failure_fingerprint` (a hash of the post's input fingerprint
and the ordered targets that were tried), and `next_attempt_at` is the ISO
time the automatic scan may try it again; a positive count with a null
`next_attempt_at` means automatic retries stopped. A success or uncertain
outcome, cached or not, and an operator reprocess request reset all three
(`0`, `NULL`, `NULL`); startup adds them to existing snapshots with those
values, so a stored failure is retried once more before backing off. See [Extraction failure backoff](sources.md#extraction-failure-backoff).

`internal/httpapi/media.go` and `internal/store/sqlite/media.go` own cache-root
path validation and serving; `internal/store/sqlite/source_write.go` owns
source URL identities (see [Media cache](media-cache.md#database-model)). `internal/mediadownload` performs bounded downloads
from the X and YouTube image hosts and promotes verified files to ready cache
rows.

## Related Docs

- [Sources and extraction](sources.md)
- [Workflow source windows](workflows.md#fetch-windows-and-historical-backfill)
- [Media cache](media-cache.md)
