# Data Model

## SQLite Snapshot

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
| `posts`                  | X posts and Bilibili dynamics, account, publication time, engagement counts, media, and the quote      |
| `events`                 | Unified schedules, platform, provenance, confidence, manual locks, tombstones, and duplicate merges    |
| `event_sources`          | Source post/reservation identities attached to each event                                              |
| `videos`                 | YouTube videos and streams, Bilibili uploads and their merges, corrections, and refresh requests       |
| `video_revisions`        | Titles and thumbnails a platform replaced, one row per change                                          |
| `focus`                  | The page's latest focus item and stable YouTube video ID                                               |
| `timeline`               | Person and activity timeline                                                                           |
| `resources`              | X, YouTube, Wikipedia, and other resource links                                                        |
| `assets`                 | Schedule images and their sources                                                                      |
| `media_assets`           | Remote media identities, cache metadata, and fetch status                                              |
| `media_links`            | Links from cached media to posts, videos, profiles, focus items and Works upload videos                |
| `media_versions`         | Earlier cached files of an asset whose bytes a revalidation replaced                                   |
| `sync_runs`              | Status, counts, error summaries, trigger, and asynchronous job identity                                |
| `sync_state`             | Durable per-account X, YouTube, Bilibili and shop cursors, read bookkeeping and X API chain state      |
| `app_settings`           | Model, extractor, X-account, Featured, YouTube push and proxy settings (secrets sealed)                |
| `schedule_extractions`   | Versioned LLM inputs, outcomes, and structured result metadata                                         |
| `post_llm_states`        | Per-post LLM status, last attempt, error, reprocess queue state, failure backoff, settled input stamp  |
| `schedule_asset_reviews` | Per-schedule-image LLM verdict, skip reason (`no_image` for a URL-less row), and optional manual label |
| `llm_providers`          | OpenAI-compatible or Anthropic endpoints, Text/Image capabilities, health, and encrypted keys          |
| `llm_models`             | Per-provider model catalog with tags (`text`/`image` routing; `reasoning`/`tools`/`embedding` labels)  |
| `llm_route_targets`      | The ordered provider + model failover queues: `schedule`, `verify` and `translate`                     |
| `llm_route_providers`    | Provider-only routes that startup copies once into `llm_route_targets`; nothing else writes them       |
| `source_schedules`       | Each source's timer, sync setting and method chain                                                     |
| `workflows`              | Saved workflow definitions: kept in the schema, created empty, never read or written                   |
| `work_cover_lookups`     | Each niconico or Bilibili cover lookup of a Works upload video: outcome, image URL and retry time      |
| `translations`           | Manual, platform, and machine translations of posts, videos and pooled statuses per target locale      |
| `translation_states`     | Per-owner machine translation state: source hash and language, last attempt, operator request          |
| `x_statuses`             | The X status pool: related accounts' statuses and statuses own posts quote or repost; never public     |
| `x_reposts`              | The own X accounts' reposts: reposted status, account, author and first sighting                       |
| `bilibili_statuses`      | The Bilibili status pool: dynamics own forwards forward; never public                                  |

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

`backend/internal/store/sqlite/workflow_migrations.go` creates the durable
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
per-source sync ([source schedules](../architecture/workflows.md#source-schedules);
`{}` reads as each step's incremental default), `fetch_windows_json` an
operator's [manual fetch](workflows.md#manual-fetches-and-historical-backfill)
(its method and selection), `source_chain_json` the per-source
[method chain](workflows.md#method-chains) (`{}` reads as each source's
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
[retention window](workflows.md#run-execution).

`source_schedules` holds each source's timer
([source schedules](../architecture/workflows.md#source-schedules)): `source`
(`x`, `x_related`, `youtube`, `bilibili`, `bilibili_dynamics` or `shop`,
the primary key), `enabled`, `interval_minutes`, `sync_json`
(`{"x": SourceSync}`, `{"x_related": SourceSync}` or
`{"youtube": SourceSync}`; `{}` for the Bilibili and shop timers and as the
default, and a `count` setting of `x_related` reads as the default),
`chain_json` (X and YouTube: `{methods: [{id, enabled}], apiGate,
apiDailyCap}`, the gate and cap for X only; `{}` for the related-accounts,
Bilibili and shop timers,
and a chain that is unreadable or does not
list every method of its source once reads as the default;
[method chains](../architecture/workflows.md#method-chains)), `next_run_at`
and `updated_at`. A missing row reads as that source's disabled default (see
[Source schedules](workflows.md#source-schedules)). The `workflows` table and
its columns, including `source_sync_json` and `source_lookback_json`, stay in
the schema; nothing reads or writes them. Every new run has an ad hoc
`workflow_id` starting with `@`; a stored run whose `workflow_id` names a
saved workflow shows in the history with that ID, without joining
`workflows`.

### Sync State

`sync_state` rows are keyed by `source` and `account_id` and hold a cursor
(`cursor_id`, `cursor_time`), `last_success_at` and `metadata_json`. Besides
each source's incremental cursors and the fetch checkpoints (see
[Budgets and checkpoints](workflows.md#budgets-and-checkpoints)), source
`x-api` keeps the [X API chain](sources.md#x-api-in-the-chain) state: one row
per X account with `{timelineFailures}`, its current streak of runs whose
timeline failed, and the row `@usage` with `{day, posts}`, the posts X API
answers returned on that Japan day. A count for an earlier day reads as
zero, and the first read of a new day starts it over. Source `x-related`
keeps one row per [related account](sources.md#related-accounts), keyed by
the lower-case handle and written by the `x_related` step: the cursor of its
own timeline and
`{handle, discovered, listed, discovery, timelineGap, lastError}`.

The `bilibili` row of the account (the mid) keeps, besides the upload
read's device identity, clock and captcha backoff, the dynamics listing's
`dynamics` `{lastAttemptAt, lastSuccessAt, lastError}` and the cached WBI
signature keys `wbi` `{img, sub, at}`; the cursor names the newest upload.
The `bilibili-live` row of the same account keeps the
[live room](sources.md#live-room) state `{roomId, title, liveStatus,
liveTime, checkedAt, lastAttemptAt, blockedUntil, failures, lastError,
lastBroadcast, lastLiveAt, endedAt}`; the room ID is learned from the checks.
The `bilibili` row's metadata also keeps the device's `bNut` and
`bili_ticket` (with when it was issued and its lifetime) inside `session`,
`login` `{checkedAt, signedIn}` from the latest login check, and
`blockedAt`/`clearedAt`, when the current block was recorded and when the
latest successful read started, so a forced read's success lifts a block
while a block a concurrent read stored is kept. Neither is returned by a public read.

The `shop` row of account `shop.milpr.com` keeps the
[official shop](sources.md#official-shop)'s last successful read in
`last_success_at` and `{lastAttemptAt, blockedUntil, lastError}`, where
`blockedUntil` is when a 429 answer's `Retry-After` passes. The shop's
period groups are not kept here: their evidence is their `event_sources`
rows.

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
start time and `unknown` precision become `exact`. A video whose `kind` or
`manual_kind` is not one of the [video forms](#video-form-and-tags) is
rewritten once, as that section describes. None of these steps clears a
snapshot.

## Event Labels

`events.tag` is the calendar label of one entry: `live`, `game`, `talk`, `song`
or `video`, or NULL. `game`, `talk` and `song` name the content and take
priority; `song` covers singing streams, covers, originals, music videos and
song premieres. `live` is a stream and `video` an upload or premiere that none
of them fits or whose content is not shown, and a mixed entry takes the
activity it leads with. The extraction prompt and the title inference share
this definition. `tag_source` records who decided it: `manual` for an
operator, `llm` for the extraction prompt or an X notice merged into the
entry, and `title` for the title inference. Precedence is manual > `llm` >
`title` > none, except that among automatic labels a content label is never
replaced by a form label:

- An operator's value belongs to the locked row, so nothing automatic replaces
  it. Clearing a stored label is the operator's decision too: it stores
  `tag_source = manual` without a tag, so the entry shows no label and its
  videos are not sent to the title inference. Saving the label as automatic
  (`auto`) clears `tag`, `tag_source` and `tag_attempted_at`: the row stays
  locked, so the extraction and the entry's own title inference leave it
  alone, but it reads on to its videos' tags and no longer holds them back
  from the title inference. A save that sends the
  stored label back unchanged keeps its `tag_source`, so an unrelated edit or
  a cancellation decision does not make an automatic label the operator's.
- The extraction prompt returns a label only when the post or image states the
  activity. An unknown value is dropped and the entry is kept without a label.
  A post that states no label leaves the row's label and source as they are;
  a stated label replaces a `title` label on the same row unless a form would
  replace content.
- YouTube reservations are created by the sync without the extraction prompt,
  so the `translate` queue also labels them from their title. The request
  names the entry's form (a `release` entry is a premiere, any other a
  stream), so a title that shows no content still gets the right form label.
  A judgement without a label, including an answer outside the five codes,
  is stored as `tag_source = title` with no tag and is not asked again. A
  title every model fails on stays unjudged and only receives
  `tag_attempted_at`; it is offered again only once that attempt is a day
  old, so it costs at most one attempt a day rather than one a run.
  Candidates never attempted come first, then the least recently attempted,
  so a failing title cannot hold the head of the queue and a provider outage
  judges nothing. An entry that a labelled notice is
  merged into is not sent.
- A creator's rename, written by the YouTube sync, clears a `title`
  judgement that found no content (no label or a form label), so the new
  title is asked about again; a content label the title gave stays.
- Every rule merge left in place, including one kept from an earlier run, lends
  the label of its notice to the surviving event when that event has no label
  or a `title` one and is neither locked nor manual, unless a form would
  replace content. When several labelled notices share a target, a content
  label lends before a form label, then the smallest notice ID wins. An
  inherited `llm` label is replaced by later runs only when it is a form and
  the notice gives content. The copied label stays when the merge is released.

Storing a label or replacing one with a different value advances the
dashboard revision; a judgement without a label, a failed attempt, or a change
of `tag_source` alone does not.

### Shown Label

Public readers do not show `events.tag` as stored. Each read derives the
label of an entry from the entry and its YouTube videos: the videos linked to
the entry or to an entry merged into it, by the entry ID, its source item or
its event sources. The shown label is the first of:

1. the entry's label when `tag_source = manual` (an operator's row without a
   label reads on);
2. an operator's tag on one of its videos (`videos.manual_tag`);
3. the entry's content label;
4. a video's inferred tag (`videos.tag`);
5. the entry's form label.

Video tags are content tags only, and with several videos the alphabetically
first tag is taken. A video's tag reads the entry in the other direction (see
[video form and tags](../architecture/data-model.md#video-form-and-tags)),
so the calendar and the video archive agree on content. Nothing is copied
between the rows. The derivation looks every entry and video up by ID, and a
store test checks the query plan for it. The admin event list shows the
stored label and its source.

## Sale-period rationale

A sale is stored as one period and shown at its opening and closing dates. This keeps long order windows out of every intervening calendar day while preserving both dates for calendar feeds and status calculation.

## Sale Periods

`events.event_type` is `stream`, `member` (a members-only stream),
`appearance`, `release` (the date a work becomes available), `sale` or
`event` (any other entry). A `sale` is a period to buy, pre-order, order or
apply for goods, media or tickets, and is the only type that stores a close:
the nullable `ends_on` holds its Japan close date and `ends_at` its close
instant when the time is known. A sale's `ends_at` always has its `ends_on`,
the Japan date it falls on; the store rejects a manual entry whose close
time falls on another date. Every other row has a null `ends_on`; the admin
API rejects one and the extraction ignores it. Sales come from post
extraction and from the [official shop](sources.md#official-shop), whose
entries are periods with the opening time when known and the close date
without a time. A sale row takes one of three
shapes:

| Shape        | `starts_on` / `starts_at`  | `ends_on` / `ends_at`                   |
| ------------ | -------------------------- | --------------------------------------- |
| Period       | opening date, opening time | a later close date, close time          |
| Deadline     | the close date, no time    | the same date, close time or null       |
| Opening only | opening date, opening time | null, or the same date and a close time |

Every reader shows a sale at its ends rather than across its days: a period
on its opening date and on its close date, a deadline on its date alone, and
any other sale on its opening date. Calendar pages therefore include a sale
whose `ends_on` falls in the range, count it in `dayCounts` on its close date
when that is later than its opening date, and take close dates into the
adjacent dates; the dashboard's current-week selection includes it the same
way. The dashboard also carries up to 10 sales that are on sale on the
current Japan date or open within seven days (a deadline counts as on sale
until its date), closing soonest first, so a period that opened weeks ago
reaches the board's on-sale list. A sale is never `nextEvent`. Reminders of one sale merge into its
period ([Rule C](#duplicate-merging)). The partial `events_ends_on_idx` index
supports the close-date reads, and startup adds the column empty and the
index to existing snapshots without rewriting events.

## Event Precedence

`events.source` names the writer of an entry: `x` or `bilibili` for post
extraction, `youtube` for reservations and broadcasts, `shop` for the
[official shop](sources.md#official-shop)'s sale periods and `manual` for an
entry an operator created. Automatic X extraction, YouTube reservations and
shop reads use `provenance = automatic`.
Creating, editing, or confirming an event through `/admin` changes it to
`provenance = manual` and sets `manual_locked = 1`. Automatic upserts include a
database-level lock condition and cannot replace those rows. Manual deletion is
a soft-delete tombstone with the same lock, so synchronization cannot resurrect
it. An automatic retirement stays unlocked: new X evidence, a YouTube
reservation, or the reconciler's event for an ended broadcast of the same
event restores it. Manual operators can mark an event `manual_confirmed` with a required reason;
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
Schedule image reviews add `llm_input_fingerprint` for input cache validation
(see [Schedule images](sources.md#schedule-images)); a review without
one is rechecked when next scanned. Automatic
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
resets its review and correction. `manual_image_at` records when an operator
picked one of the post's images as the schedule image; synchronization then
leaves the asset's image, review and correction alone. Readers derive the
period of an asset with no stored or corrected one from its source post's
events (basis `events`, see [Schedule images](sources.md#schedule-images));
nothing stores it. A period that is still unknown stays null until corrected
and is not assigned to a calendar week.

Complete post `media_urls` snapshots reconcile `post-image` ownership in the
same transaction as the post write. An empty list removes all display links;
omitted media fields preserve the known declaration. Failed source reads never
call this writer. Cache rows/files may remain after a link is removed.

## Quoted Posts

A post that quotes another status keeps it in `posts.quote_json`, a
sanitized projection (`id`, `authorHandle`, `authorName`, `text`,
`publishedAt`, `url`, and `unavailable` for a quote the source names but
does not carry). An X post quotes an X status; a Bilibili
[forward](sources.md#forwards) quotes the dynamic it forwards, with the
author's mid as `authorHandle` and the post ID `bilibili-<dynamic ID>` as
`id`. The quoted status's images are `media_links` with the role
`quote-image` and the quoting post as owner; `quote_json` holds no image
address, and a post's `post-image` links, `media_url` and
`raw_json.mediaURLs` are its own images only. A post without a quote stores
`NULL`. See [Quoted posts](sources.md#quoted-posts) for how the X adapter
reads it and why quoted images are never schedule candidates.

The public dashboard, the post archive and MCP add `quote` to each post:
`{id, authorHandle, authorName, text, publishedAt, url, unavailable, media,
translations, sourceLanguage}`, where empty fields are `null` and `media` has
the shape of the post's own `media` but lists only ready, contained opaque-ID
images ([Translations](#translations) describes the last two). The read path
accepts a projection only for its post's source. On an X post, it drops one
whose ID is not a status ID or whose URL is not that status's x.com address.
On a Bilibili post, it drops one whose ID is not `bilibili-<digits>` or
whose URL is not exactly that dynamic's `https://www.bilibili.com/opus/` page
or, for an available forwarded upload, a `https://www.bilibili.com/video/`
page. An unavailable quote has only `id` and `url`; an unavailable forward
of a dynamic whose ID the listing did not give has a `null` `id` and the
forward's own opus page as `url`. The console's post list keeps a post's own
images apart from its quote.

Every available X quote is also a row of the X status pool `x_statuses` (see
[Quoted posts](sources.md#quoted-posts)), which also holds every status of a
[related account](sources.md#related-accounts): `id`, `author_handle`,
`author_name`, `text`, `published_at`, `url`, a sanitized `raw_json` (a
quote's `{id, text, created_at, url, author, mediaURLs}`, or the timeline
status as an own post stores it), `quoted_at`, the publication time of the
newest own post that quotes it, `reposted_at`, when an own account's repost
of a related account's status was first seen, `listed_at`, when its related
author's timeline first listed it, `keyword` (1 while a keyword rule
matches), the operator's `manual_decision` (`promote` or `demote`) with
`manual_reason` and `manual_at`, `demoted_at`, set while an automatic
demotion hides its post, `first_seen_at` and `updated_at`. A quote's `text`
equals the `text` of the `quote_json` written with it, since both come from
one projection; a status its related author's timeline listed keeps that
listing's wording and payload. No public reader reads or joins the table.

Every available forwarded dynamic is likewise a row of the Bilibili status
pool `bilibili_statuses` (see [Forwards](sources.md#forwards)): `id` (the
post ID `bilibili-<dynamic ID>`), `author_mid`, `author_name`, `text`,
`published_at`, `url`, a sanitized `raw_json` (`{id, text, publishedAt, url,
author: {mid, name}, mediaURLs}`), `forwarded_at`, the publication time of
the newest own forward of it, `first_seen_at` and `updated_at`. Its `text`
equals the forward's `quote_json` `text`, and no public reader reads or joins
the table. A fresh database and an upgraded snapshot start with it empty.

`posts.promoted_at` marks a related account's status promoted into `posts`
(`NULL` for an own post), whose `account_handle` is its author. Readers that
are an own account's (the dashboard's latest post and per-account newest
posts) leave it out, so a promoted post appears only in the combined post
wall and the archive, which also leave out a promoted post that a shown own
post quotes with its quote shown: the quote card already shows it. That is a
display rule computed at read time; it sets nothing, and the post returns
when the quoting post or its quote is hidden. `x_reposts` holds one row per reposted
status and own account (`status_id`, `account_handle` in lower case,
`author_handle` or `NULL`, `first_seen_at`); see [Reposts](sources.md#reposts).

`posts.quote_hidden_at` records an operator's decision to keep a post's
quote off the public pages: the dashboard, the post archive and MCP then
omit `quote` entirely, while the console still shows it. No synchronization
path, refresh or backfill writes this column, so the decision survives every
rewrite of `quote_json` and the `quote-image` links, like a manual lock.
`posts.hidden_at` is the same decision for the whole post: the dashboard
(its post wall, latest post and post count), the post archive and MCP leave
the post out, extraction never selects it, and the automatic events it alone
supports leave the public pages (see
[Events of hidden posts](#events-of-hidden-posts)). The console still lists
the post, and no synchronization path writes the column. `posts.refresh_requested_at` is a
queued refresh from the source, cleared once the X synchronization made its
detail request (see
[Refresh from the source](sources.md#refresh-from-the-source)). All three are
additive columns, `NULL` by default.

Startup creates both tables in existing snapshots. On every start it adds
to the pool the quote of every stored X post whose available quote the pool
does not hold as quoted by that post yet, mapped from the post's `raw_json`
like a source write, without a request.

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
locales `en`, `ja` and `zh-CN`, keyed by
`(owner_type, owner_id, field, locale, origin, platform)`: `owner_type` is
`post` (field `text`), `video` (field `title`), `x_status` (field `text`,
a pooled X status keyed by its status ID) or `bilibili_status` (field
`text`, a pooled forwarded dynamic keyed by its post ID), `origin` is `manual`,
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
`DeletePlatformTranslationTx` in `backend/internal/store/sqlite/translations.go`),
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
translation, and `none` needs none at all: a Japanese X post or YouTube title
needs `en` and `zh-CN`, a Chinese Bilibili dynamic `en` and `ja`. Wording the
step has not seen in its current form needs `en` and `zh-CN`, and `ja` only
when it is a Bilibili dynamic, stored or pooled, or a Bilibili title, since X
posts and statuses and YouTube titles are presumed Japanese until detected;
the console shows such a `ja` as the source. A post
or video is complete when every locale it needs has a usable row. The
machine step translates posts of the sources `x` and `bilibili` and the
unmerged videos public readers list (YouTube videos and Bilibili uploads
whose form is settled, see [Video form and tags](#video-form-and-tags)). The
public dashboard, archives and
video lists add `translations` (`{locale: {text, origin, platform}}`) and
`sourceLanguage` to posts and videos; providers, models and hashes stay in
the console. A Bilibili video whose language the machine step has not
detected reports `sourceLanguage` `zh`. A video's translations follow its effective
title, `COALESCE(manual_title, title)`, so an operator's title correction
retires its machine translations.

Events have no translation rows of their own. A board or calendar event
whose URL names a YouTube video in the translate step's scope (by
`stream_identity`) carries that video's `translations` and `sourceLanguage`
while its trimmed title equals the video's effective title; an event with
an operator's or an extraction's own wording carries none. The projection is
computed at read time, so a video translation's revision already covers it.

A post's available `quote` carries `translations` and `sourceLanguage`,
computed at read time. A quote of a stored post (a self-quote, or a forward
of an own stored dynamic) carries that post's. Any other quote carries the
pool rows of its ID, `x_status` on an X post and `bilibili_status` on a
Bilibili forward, resolved against the quote's own `text` in `quote_json`: a
machine row and the detected language count only while their source hash
matches that text, and manual and platform rows always count. The read uses
the translation tables alone and never reads a pool table. The machine step
translates a pooled X status while an own post quotes or reposts it, and a
pooled dynamic while an own forward forwards it, when it is not a stored
post (see [Translation](sources.md#translation)).

Startup creates both translation tables in existing snapshots; no post,
video or event row is rewritten.

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
  (`backend/internal/store/sqlite/event_merge.go`). It recomputes its own
  decisions from the current rows on every run, so it is idempotent and
  releases a merge whose target was deleted, merged or moved to another
  date, or whose event became manual or locked or now holds another owner's
  merge. An event holding another owner's merge is never merged away, so no
  merge points at a merged event. Deleting an event, or retiring it
  automatically when its last source evidence goes, releases the rule merges
  into it in the same transaction, so a duplicate older than the
  reconciler's window is shown again.
- `manual`: an operator decision (`merge_reason` `manual`), which the rules
  never change. `merge_source = manual` with a null `merged_into` is an undone
  merge; the reconciler never merges that event again. Deleting the target
  undoes an operator merge into it (`merge_reason` `unmerged_from:<id>`), so
  no entry stays hidden behind a tombstone.
- Any other owner is left untouched by the rules in the same way, and its
  merges stay when their target is deleted.

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
  one date-only event and one timed stream, and not into a stream of platform
  `bilibili` unless the date-only entry is `bilibili` as well. An entry
  stated Bilibili-only (see [Event platforms](#event-platforms)) never
  merges into a stream whose own platforms lack `bilibili`. A YouTube event
  never merges into an X or Bilibili event.

- Rule B also moves an automatic entry of a Bilibili dynamic whose platform
  is `youtube` (such as 「今晚8点YouTube直播」), under the same constraints. A
  `stream` or `member` entry whose platforms include both `youtube` and
  `bilibili` is never a Rule B notice; Rule E moves it.
- Rule D (after Rule B): an automatic X `stream`, `member` or `release`
  whose platforms include `bilibili`, with an exact or approximate time,
  merges into the only
  compatible entry of a Bilibili dynamic (`source` `bilibili`) on the same
  date that starts within 30 minutes; a release only into a release. The
  dynamic is the platform's first-party post, as the YouTube event is for
  Rule B, and an X entry of platform `bilibili` is never a Rule B notice.
  An X notice of a simulcast whose model named Bilibili keeps `bilibili`
  first and merges here, then follows the dynamic's entry (below).
  Without a dynamics listing the X entry stands alone and still gets the
  room as `liveUrl`.

- Rule E (after Rule D): an automatic `stream` or `member` entry, of any
  source but YouTube, whose announced platforms are `bilibili` by a
  source's default (inferred) or include both `youtube` and `bilibili`
  merges into the only compatible YouTube `stream` or `member` event on the
  same date that starts within 30 minutes, as in Rule B
  (`merge_reason` `rule_e:simulcast`): it is the Bilibili side of a
  simulcast, and the YouTube event's shown platforms include `bilibili`
  (see [Event platforms](#event-platforms)). An entry stated Bilibili-only,
  such as 「B站限定」, and a release never merge by this rule.

After every rule, an entry whose target is itself merged away goes on to
the final target, whichever rules made the two merges (an X notice Rule D
merged into a dynamic's entry that Rule E moves ends on the YouTube event),
so no merge points at a merged event.

- Rule C: an automatic `sale` merges into another sale of the same shop
  whose [period](#sale-periods) covers its own, such as a deadline reminder
  into the announced period. Two sales are of the same shop when their links
  name the same page (host without `www`, path without a trailing slash, and
  query; a link to an X post names no shop), or, when either has no shop
  link, when their titles overlap as in Rule A once the words and counts
  every sale title uses (such as 受注, 受付, 締切, グッズ, 本日まで or 残り2日)
  and Kano's own name (鹿乃まほろ, its parts 鹿乃 and まほろ, and its
  romanization) are removed as well, so a title naming no goods or event
  matches no other sale. A period covers another that
  opens no earlier and closes no later, and a sale whose close is not known
  when its opening date falls inside; a deadline is covered only by a period
  that closes on its date, or by the same deadline. Of two sales covering
  each other the one that states more (a known opening, a close date, a start
  time, a close time) stays, then the smaller ID. Among several covering
  sales the one no other covers is the target; several leave the sale
  unmerged. A sale counts in the window by its close date as well as its
  opening date, and its `merge_reason` is `rule_c:sale_period`. Because the
  target covers the merged sale, the surviving entry shows the whole period.
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

Every public reader excludes merged rows, and the events
[hidden by their posts](#events-of-hidden-posts): the dashboard events,
totals, upcoming counts and summary/spotlight candidates, calendar items,
totals, pagination, day counts and adjacent dates, and MCP `schedule_list`. The
operator console lists them with their target and can undo a merge. Startup
adds the columns and the partial `events_merged_into_idx` index to existing
snapshots without rewriting events.

An entry of a Bilibili dynamic never merges into an X entry, by any rule; it
is otherwise an X entry for Rules A and C. A shop entry is an automatic sale
like any other for Rule C, so a post's entry of the same sale merges into it
or receives it, by link or title as the rule says.

### Bilibili releases

Before merging, `reconcile` recomputes the pairing of the automatic,
unlocked `release` entries of Bilibili dynamics in its window with the
account's Bilibili uploads. A release and an upload pair when each is the
other's only match within 30 minutes: the entry's link becomes the upload's
page and an active `bilibili-video` event source (`source_key` `upload`)
names it. A pairing that no longer holds (the release moved, or a second
release or upload made it ambiguous) is released first: its source row is
retired and the link goes back to the dynamic's. An upload another entry
still holds is not paired: a locked or manual entry keeps its upload row
whatever its state, and so does any other entry that is not deleted and
whose row is active, so a locked entry's row is never rewritten. An
unchanged run writes nothing; an
extraction that rewrites the link restores the dynamic's link until the next
run pairs it again. The upload source is evidence of the work, not an
announcement, so it keeps no entry alive that its dynamic no longer lists.

## Events of Hidden Posts

An automatic, unlocked event whose every active source is a hidden post is
hidden by its posts: the active `event_sources` rows of the event and of the
events merged into it all name a stored X or Bilibili post with
`posts.hidden_at` set, and there is at least one. It is a visibility rule
computed at read time, not a deletion or tombstone: the row, its evidence and
its merges stay, and showing a source post shows the event again. An event
with `manual_locked = 1` or `provenance = manual` is never hidden this way,
since the operator's decision outranks hiding; confirming an event keeps it
shown while its post stays hidden. An event with any other active source,
such as a YouTube reservation or a visible post, or with no evidence at all,
stays shown. Every public event reader leaves such an event out, as it leaves
out merged rows (see [Duplicate merging](#duplicate-merging)), and so does a
video's linked event. The console lists it with `hiddenByPosts`.

## Event Platforms

An entry happens on one or more platforms. `events.platforms` is the JSON
array of its **announced platforms**, each of `youtube`, `bilibili` and
`other` at most once, in announced order (the entry's own platform first),
and `events.platforms_inferred` is 1 when the array came from a source's
default rather than from the wording or an operator; a Bilibili-only entry
is one whose array is `["bilibili"]`, stated. `events.platform` is
the first announced platform, or `unknown`, for readers of the single
value. Startup adds the columns without rewriting the array: `platform` is
null on rows written before it existed, and the startup that adds it sets
`youtube` on the YouTube writers' events. A row whose array is null reads as
`["youtube"]`, stated, when its `platform` is `youtube`, and as no platform
otherwise.

Extraction writes the announced platforms with the other fields of an
automatic entry, under the same manual locks and tombstones (see
[Source time zones](sources.md#source-time-zones)); the YouTube writers
write `["youtube"]` and the shop step `["other"]`, both stated. An operator's entry carries the operator's platforms,
which are part of its manual lock.

`events.url` is the announcement's link (a post, an operator's link, a
paired upload, a watch link), never the live room, because readers use the
link as the identity of one broadcast. A stored row whose `url` is a
live-room link (`live.bilibili.com`) and whose platforms do not include
`other` reads as announced on `bilibili` with no `url`; the row is not
rewritten.

Readers compute an entry's **shown platforms**: its announced platforms,
those the automatic entries merged into it lend, and `bilibili` when the
live room's broadcast is the simulcast of its YouTube broadcast (observed;
see [Live room](sources.md#live-room)). Only merges that join platforms
lend: Rule D, Rule E and an operator's merge; Rules A, B and C join
duplicates of one entry. Nothing observed is written. Each shown platform
has a link only to a page on that platform: YouTube's is the watch link (the
entry's `url` when it names a YouTube video, or that of a YouTube event an
operator merged into it), Bilibili's the account's live room for a `stream`
or `member` entry once a live check learned the room (or the stored room
link of an older row) and a release's Bilibili upload page, and `other`'s
the entry's `url`. A platform without such a page has a null link: the
announcement (a post) stays the entry's own `url`, never labelled as a
platform. The entry's `liveStatus` is `live` while any of its platforms is:
its own video's state, then a merged YouTube event's, then the room's.
Public events report the single value as `platform` and the shown platforms
as `platforms`; full reads add `links`, and `liveUrl`, the room, to every
`stream` or `member` entry shown on Bilibili (see
[Public API](public-api.md#public-snapshot-contract)). The dashboard's
`liveNow` reports a live broadcast that matches no entry; no entry is
created for it.

## Video Corrections and History

Synchronization owns a video's platform fields (`title`, `kind`,
`thumbnail_url` and its media link) and keeps rewriting them on every
successful page read. An operator correction lives beside them in
`manual_title`, `manual_kind`, `manual_tag`, `manual_is_short`,
`manual_availability` and
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

`is_short` is the YouTube format verdict: 1 for a Short, 0 for any other
upload, `NULL` while unknown. The feed writes both values; the Shorts
playlist that stands in for a failed feed writes only 1 (see
[Sources](sources.md#shorts)).
`manual_is_short` is the operator's correction with the same values and
`NULL` for none; synchronization never writes it. Public readers add
`isShort = COALESCE(manual_is_short, is_short)` as `true`, `false` or `null`,
and the board treats only `true` as a Short. A row without a verdict stays
unknown rather than being guessed from its length.
`is_premiere` is the stored premiere verdict of a YouTube reservation: 1 for
a premiere (a `release` event), 0 for live content, `NULL` while no check
could tell, which is also what the upgrade gives existing rows (see
[Sources](sources.md#youtube)). It is never derived from the kind; the kind
follows it.
`availability` is who can watch a YouTube video: `public`, `unlisted`,
`members`, `private` or `removed`, `NULL` until a check could tell, which is
also what existing rows and Bilibili uploads keep (see
[Video availability](sources.md#video-availability)). A check that cannot
tell keeps the stored value. `manual_availability` is the operator's
correction with the same values and `NULL` for none; synchronization never
writes it. Public readers add
`availability = COALESCE(manual_availability, availability)`.
`manual_work` is the operator's Works decision: 1 lists the video among the
Works page's uploads after the collection date, 0 keeps it off, `NULL`
leaves it to the rules (see
[Uploads after the collection date](../development/works-archive.md#uploads-after-the-collection-date)).
Synchronization never writes it, a decision on a video outside the list's
date or scope stays stored without effect, and no public reader reports it.
Startup adds the columns, both tables and `video_revisions_video_idx` to
existing snapshots without rewriting videos or media rows.

## Modeling rationale

Events, video form, video content, and sale periods use separate fields because they answer different reader questions. A stream or premiere is a publication form; a game, talk, or song is content; a sale is a period with two endpoints. Keeping those dimensions independent lets calendar labels and archive filters agree without guessing from titles or overwriting operator corrections.

## Video Form and Tags

A video has three independent layers, each with one owner
([video form and tags](../architecture/data-model.md#video-form-and-tags)).

**Form.** `kind` is `stream`, `premiere`, `upload` or `unknown`, written by
synchronization from platform facts only:

- A YouTube detail check (watch page or Data API) that found a broadcast
  start writes `premiere` when the premiere verdict says so and `stream`
  otherwise; one that found no broadcast start writes `upload`. An ended
  broadcast whose answer cannot tell (an unclassified Data API answer) keeps
  a stored `stream` or `premiere` and is `unknown` without one.
- A listing (feed, uploads playlist, uploads tab, yt-dlp metadata, history
  window) inserts `unknown`, and a listing never replaces a stored row's
  kind; the detail check settles it.
- A Bilibili upload is `upload` or `stream` (a live replay), settled as
  [Bilibili form](#bilibili-form) describes. The list read inserts `upload`
  for a new row and never replaces a stored row's kind.

`manual_kind` corrects the form with `stream`, `premiere` or `upload`, and a
Bilibili upload's with `stream` or `upload`.
Readers show `COALESCE(manual_kind, kind)`.

**State.** Upcoming, live, ended and cancelled are not part of the kind:
`is_upcoming`, `live_status`, `scheduled_at`, the linked entry's `ends_at` and
its operator-confirmed cancellation say where a stream or premiere stands. A
stream archive is a `stream` whose broadcast ended.

**Content.** A video's tag is `game`, `talk` or `song`. Readers derive the
shown tag on each read as the first of:

1. `manual_tag`, the operator's video tag;
2. the [label](#event-labels) of the video's schedule entry (its
   `youtube-<id>` event, an event whose `source_item_id` it is, or one with a
   YouTube `event_sources` row for it; deleted entries are skipped and the
   label of the entry it is merged into comes first), when that label is
   `game`, `talk` or `song`;
3. `tag`, the title inference's tag of the video itself.

The entry label is never copied into the video row, so an edit, merge or
release on the entry shows at once. `live` and `video` labels name a form and
never reach a video. A video without a tag is `other` in the archive's
category filter.

`tag`, `tag_source` and `tag_attempted_at` follow the entry inference's rules
(see [Event labels](#event-labels)): the `translate` queue asks about the
videos public readers list (see [Video merging](#video-merging)), a Bilibili
upload only while nobody marked it private or removed, each not merged into
another and without a manual tag or judgement,
whose entries, if any, carry neither a content label, a title judgement nor
an operator's label, are not merged into such an entry, and are not
automatic reservations still within the entry inference window. An
operator's row that never had a label (`tag_source` empty) reads on to the
video's tag, so it does not hold the video back; a cleared label
(`tag_source = manual`) does. Only `game`, `talk` or `song` is stored; another
answer or none is stored as `tag_source = title` without a tag and is not
asked again until a creator's rename clears that judgement, whether the
recent synchronization or a history page sees the new title first (a stored
tag, or an operator's `manual_title`, keeps it); a failure only stamps
`tag_attempted_at` and waits a day, as an entry does. Videos
never attempted come first, then the least recently attempted, each newest
first. Storing a tag advances the dashboard revision; a judgement without
one or a failed attempt does not. A Bilibili upload whose form is unsettled
is asked for form and tag together instead (see [Bilibili form](#bilibili-form)),
after the entries and before the other videos, from the same per-run limit.

### Bilibili Form

Bilibili's unsigned endpoints carry no live-replay flag, so a Bilibili
upload's form is settled by the first of:

1. the operator's `manual_kind`;
2. a rule, applied by the `translate` step before any model call to every
   upload whose form neither the operator nor a rule settled. A rule that
   applies only later, once the broadcast it reads is known, replaces a form
   the model settled meanwhile:
   - a title naming a replay (`直播回放`, `录播`, `直播录像`, `回放`) is a
     `stream`;
   - an upload of at least thirty minutes published within 48 hours after a
     Bilibili broadcast ended, at least half that broadcast's length, is a
     `stream`. A broadcast is a `stream` entry shown on Bilibili (its
     platforms, or an entry merged into it) that is neither deleted, merged
     nor cancelled by an operator, ending at its `ends_at` or else its
     YouTube archive's start plus length; or one of the live room's last two
     broadcasts, the last one ending at the last check that saw it live;
   - an upload shorter than ten minutes is an `upload`;
3. the title inference: the `translate` queue asks about each unmerged
   upload nobody settled or marked private or removed, with its title,
   length and the first 200 characters of its description, for the form
   (`upload`, `stream` or `uncertain`) and the content tag together.

`kind_source` records who settled the stored `kind`: `rule` or `llm`, `NULL`
while unsettled; an operator's `manual_kind` shows as `manual`. An
`uncertain` answer sets `kind_review` to `uncertain`; a failed call counts
`kind_attempts` and stamps `kind_attempted_at`, waits a day and on the third
failure sets `kind_review` to `failed`. An upload with a `kind_review` and no
settled form waits in the Review queue for the operator and is not asked
again; a rule that later settles it clears the review. Settling the form of
an unmerged upload advances the dashboard revision. Startup adds the columns
to existing snapshots; existing Bilibili rows start unsettled and the next
`translate` run settles them.

A Bilibili upload has no premieres, Shorts or waiting room: `is_short`,
`is_upcoming`, `live_status` and `availability` stay empty, and readers treat
a `stream` as a stream archive.

Startup rewrites a row that still carries an older title kind, once:
`manual_kind` `GAMEPLAY`, `TALK` and `MUSIC` become `manual_tag` `game`,
`talk` and `song` and clear the form correction, `LIVE ARCHIVE` and
`UPCOMING LIVE` become `stream` and `VIDEO` becomes `upload`. `kind` is
derived from the stored facts: a start with a premiere verdict decides; a
start without one is a `premiere` for an old `MUSIC` or `VIDEO` kind and a
`stream` otherwise; a row without a start is an `upload` once
`live_checked_at` records a detail check and `unknown` otherwise; a Bilibili
row is an `upload`. A row already in the new shape is untouched.

## Video Merging

A Bilibili upload of the same work as a YouTube video is attached to it, so
readers list one video with both links. The additive `videos` columns
`merged_into` (the YouTube video's ID), `merge_source`, `merge_reason` and
`merged_at` hold the decision on the Bilibili row. No source writer names
them, so rereading either platform keeps the merge. `merge_source` names the
owner as for [event merges](#duplicate-merging):

- `rule`: the `reconcile` workflow step
  (`backend/internal/store/sqlite/video_merge.go`), which recomputes its
  decisions from the current rows on every run and releases a merge whose
  basis no longer holds. `merge_reason` is `rule:broadcast` for a live
  replay and `rule:time_duration_title` otherwise.
- `manual`: an operator merge (`merge_reason` `manual`), which the rule never
  changes. `manual` with a null `merged_into` is an undone merge
  (`unmerged_from:<id>`) or an upload an operator kept apart
  (`kept_separate`); the rule never merges that upload again.

A live replay (an upload whose settled form is `stream`) merges by rule into
the YouTube stream archive linked to the schedule entry of the broadcast it
replays. The replayed broadcast is the one among those
[Bilibili form](#bilibili-form) reads that ended no more than 48 hours
before the upload was published, the latest end first, with the upload at
least half its length when that length is known; a live room's record
stands for the schedule entry it overlaps. The replay merges only when that
entry links a YouTube video of form `stream` (its `youtube-<id>` event,
source item or event source). A replay whose broadcast links no such
archive, such as one of a Bilibili-only broadcast, never reaches an earlier
broadcast's archive: it is public on its own unless the rule below merges
it.

Any other upload merges by rule into a YouTube video only when all of these
hold:

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
day or with another length, never matches. An upload with no candidate
within six hours whose song title and known length (within three seconds)
match a video published further apart, such as a copy posted days later,
is never merged by rule; it is put to the operator as `unverified`.

Whenever a merge is written or a merged upload's effective title changes (a
Bilibili read, an operator correction or the reconcile step), the upload's
title is stored in the same transaction as the YouTube video's `zh-CN`
platform translation with platform `bilibili`; a release or undo removes it.
A change of either moves the dashboard revision. The machine translation
scope excludes merged rows.

Public readers (the board's shelf and summary, the video archive with its
month index and category counts, the single-video read, the Works page's
recent uploads, MCP and the Atom feed) list every YouTube video and each
Bilibili upload that is not merged into another video and whose form is
settled. They add `alternates` (`[{source, url}]`) to each YouTube video for
its merged uploads; a merged upload is listed only that way, so a work shows
once. Startup adds the columns and the partial `videos_merged_into_idx`
index to existing snapshots without rewriting videos.

## Schema Changes

Startup additively creates `posts_url_published_at_idx` on
`posts(url, published_at DESC)` for the correlated schedule-image source-text
lookup. Existing rows, reviews, manual labels, locks/tombstones, provider routes,
and settings remain in place. The lookup chooses the latest stored source
wording (including `raw_json.search_text`) and applies the existing text/image
and manual-review gates. The index is non-unique because multiple stored records
can share a source URL.

backend/internal/store/sqlite owns the schema (`bootstrap.sql`), the
additive upgrade behavior (`schema_upgrade.go`) and the workflow tables
(`workflow_migrations.go`). Update this
contract, seed behavior, and relevant tests together. Never silently drop
columns, rebuild an existing database from seed data, clear snapshots, or
recreate deleted providers. See [Database operations](../operations/database.md).

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
source URL identities (see [Media cache](media-cache.md#database-model)). `internal/mediadownload` performs bounded downloads
from the X and YouTube image hosts and promotes verified files to ready cache
rows.

### Post LLM states

`post_llm_states.route` records which detection path (`schedule_message` or
`schedule_board`) handled a post.

`post_llm_states.failure_count` counts consecutive failed extractions of the
input named by `failure_fingerprint` (a hash of the post's input fingerprint
and the ordered targets that were tried), and `next_attempt_at` is the ISO
time the automatic scan may try it again; a positive count with a null
`next_attempt_at` means automatic retries stopped. A success or uncertain
outcome, cached or not, and an operator reprocess request reset all three
(`0`, `NULL`, `NULL`); startup adds them to existing snapshots with those
values, so a stored failure is retried once more before backing off. An
operator's dismissal sets `status` to `dismissed` with a positive count and a
null `next_attempt_at`, keeping `failure_fingerprint`. See [Extraction failure backoff](sources.md#extraction-failure-backoff).

`post_llm_states.settled_stamp` names the input a post's cache hit recorded:
the SQLite function `schedule_input_stamp`
(`internal/store/sqlite/schedule_stamp.go`) hashes the detection stage, the
extractor version, the post's `source`, `text`, `published_at`, `url` and
`media_url`, the image lists `raw_json` declares (`mediaUrls`, `media_urls`,
`mediaURLs`, or the whole record when it is not a JSON object), the shown
quote's `authorHandle`, `authorName`, `text` and `unavailable` as the
extractor decodes them, and every `post-image` link in `position, id` order
with its media ID and the asset's `status`, `sha256`, `mime_type` and
`cache_path`. It covers every input of the extraction identity and the image
check, so a change to any of them changes the stamp; a stamp that changes
when the identity does not only has the post judged once more.

Only a cache hit writes the stamp, in the same statement as the state that
hit records. Every other state the extractor writes, including a fresh
model result, stores NULL, as do an operator reprocess request and a
dismissal. A post is settled for a detection stage when its `status` is
`success` or `uncertain`, its `route` is that stage's, `reprocess_requested`
is 0, `last_error`, `failure_fingerprint` and `next_attempt_at` are NULL,
`failure_count` is 0, and `settled_stamp` equals the stage's current stamp;
the scan then counts it as a cache hit without loading it (see
[Schedule extraction pipeline](sources.md#schedule-extraction-pipeline)).
Startup adds the column empty, so every post is judged until a cache hit
records its stamp.

A settled post's images are not read, so a cached file deleted from disk
after the post settled goes unnoticed: the post keeps its stored result
instead of waiting as `media_pending`, until its input changes or an
operator requests a reprocess.

## Related Docs

- [Sources and extraction](sources.md)
- [Manual source fetches](workflows.md#manual-fetches-and-historical-backfill)
- [Media cache](media-cache.md)
