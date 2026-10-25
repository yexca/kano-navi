# Sources and Schedule Extraction

## Current Sources

### X

The synchronizer discovers status IDs for each account named by `X_HANDLES`
(default `kano_2525,_Kanotic`) from the public FxTwitter timeline
`api.fxtwitter.com/2/profile/<handle>/statuses`, which lists the account's
statuses newest first in pages of 20 with a continuation cursor. Discovery
pages back until a page's oldest own status is already stored, is not newer
than the account's stored cursor, or predates the lookback window, so posts
published between runs are found even when several arrive at once. Reposts
carry another author and are skipped. `X_DISCOVERY_LIMIT` caps the discovered
IDs and therefore the pages (at most 10) per account and run; a failed later
page keeps the newer pages already read.

When the first timeline page fails, is not a timeline, or yields none of the
account's own statuses, discovery falls back to the logged-out profile page
`x.com/<handle>`, which lists only the newest few statuses. The run records the
source used in `sync_state.metadata_json.discovery` (`timeline` or `profile`)
and counts fallback accounts as `profileFallbacks` in the step result. A
fallback alone does not fail the account or mark the run partial.

Discovered IDs keep the existing detail path: their Snowflake timestamps are
decoded and IDs older than `X_BOOTSTRAP_DAYS` (seven by default) or the
workflow's X lookback are skipped before `api.vxtwitter.com` detail requests.
Each account has its own cursor in `sync_state`. Later runs prioritize unknown
IDs and may refresh only a small `X_REFRESH_KNOWN` budget.
`X_MAX_STATUS_REQUESTS` is the total detail-request budget shared by all
configured accounts. Successful posts are stored with `account_handle` and
returned as one time-ordered feed. Image-only posts are retained as candidates
as long as they have a publication time and a media URL. `X_HANDLE` remains a
single-account compatibility fallback.

Operator fetch windows read the same timeline differently (see
[Fetch windows](workflows.md#fetch-windows-and-historical-backfill)): without
`X_API_BEARER_TOKEN`, `recent` and `range` windows, and `count` windows
always, request pages of 100 and store each own status from the page itself.
The adapter maps FxTwitter's `created_timestamp`, `text`, counters and
`media` (a photo's URL, or a video's or GIF's thumbnail, in post order, also
inside a quoted status) onto the same post fields the detail path fills, and
keeps only the author's name and handle of the repeated profile in
`raw_json`. Such windows leave stored posts, the cursor and the pending queue
unchanged.

Both discovery hosts and the detail host share one client: a fixed timeout and
User-Agent, a 4 MiB response limit, rejected redirects, typed error codes, and
no credentials or personal data in URLs. A failed source keeps stored posts and
cursors.

Normal incremental runs persist admitted but unfinished status IDs in the
account's `sync_state.metadata_json.pendingStatusIds` before detail requests.
The array's order is the per-account retry order. Newly admitted IDs join after
existing pending work; IDs selected within the account's request allocation move
behind unattempted work, with that rotation persisted before detail requests.
Thus a repeatedly failing ID cannot keep blocking other admitted work, even
after a process restart. Retries use each run's existing budget without time
backoff or discarding failures. Pending work remains retryable when it falls out
of the profile or ages beyond the bootstrap window. Successful posts,
pending-work removal, and the latest successful cursor commit atomically.
Budget exhaustion and failed/invalid detail responses retain unfinished IDs;
an all-failed run does not record a successful cursor or timestamp. Date-window
fetches neither consume this queue nor update normal incremental state.

Each discovered image URL is registered and linked to its post. A bounded media
stage later in the same command downloads pending image files from X's
`*.twimg.com` media CDN subdomains.
The X adapter also accepts media and schedule keywords carried by a quoted
tweet (`qrt`) in the detail response.
When a schedule asset already has a source status URL but no image URL, the
synchronizer adds up to `X_SCHEDULE_REFRESH_LIMIT` recent source IDs per
account to the detail-request queue. On a cursorless bootstrap with no asset
source for an account, a bounded set of keyword-matching snapshot posts can
serve the same purpose. These requests supplement discovery without
bypassing the shared `X_MAX_STATUS_REQUESTS` budget and can recover a schedule
post that has fallen out of the discovered status list.

### YouTube

The script reads the RSS feed for `YOUTUBE_CHANNEL_ID` (the current main channel
by default). With no cursor, it stores the latest `YOUTUBE_BOOTSTRAP_VIDEOS`
entries (six by default). Later runs store only entries before the prior cursor,
or entries newer than its timestamp when that cursor has fallen out of the RSS
window. It separately inspects the streams page and keeps rechecking active or
recent reservations for `scheduledStartTime`, writing them to both `videos` and
`events`. When no schedule time is available, the script does not invent one. RSS insertion
does not count as a reservation detail check, but RSS lists waiting rooms and
premieres like ordinary uploads, so RSS entries published within the lookback
window are detail candidates as well, after stream-page discoveries. A watch
page whose player reports `isLiveContent: false` with a start time is a
premiere: its event is a `release` and its video keeps the title-derived kind.
A finished stream or premiere writes the `endTimestamp` from its
`liveBroadcastDetails` as the event's `ends_at`; a later check without one keeps
the stored end. Every successful watch-page check also records the broadcast
state the page reported (see [Live-status probe](#live-status-probe)). The
reservation event stores the status code `scheduled`, `live` or `ended`, and a
broadcast reported live is `live`, never `ended`; its detail is `YouTube Live`
or `YouTube Premiere`, and a members-only title sets the `member` type (see
[Stored event text](data-model.md#stored-event-text)). Successful detail checks are recorded in incremental
`sync_state.metadata.streamDetailsCheckedIds` for currently discovered, recent
RSS and active videos. Eligible discoveries and reservation
refreshes share the durable rotating detail queue; failed checks remain eligible,
and failed refreshes retain existing video/event snapshots. HTTP 200 alone is
not success: a reservation needs a parsed timestamp and page title; an ordinary
video without a reservation needs a complete, playable player response with a
matching video ID and title. Missing/truncated details, invalid timestamps, and
upcoming pages without a start time remain retryable within the existing request
budget. They record the protected `youtube_watch_details_incomplete` diagnostic
without updating video/event details or claiming a successful check. Incremental
metadata distinguishes successful `inspected` checks from `detailRequested`
attempts. Historical windows
do not write this incremental metadata.

The public RSS feed returns intermittent HTTP 5xx responses that clear within
seconds, so the feed request alone is repeated up to two more times after an
upstream 5xx (`youtube_upstream_5xx`) or a connection failure
(`youtube_unavailable`), waiting 0.5 and then 1.5 seconds. Each try keeps the
10-second request timeout, and cancellation stops the wait. Rate limiting
(`youtube_rate_limited`), other 4xx responses (`youtube_http_error`), timeouts,
redirects, and oversized bodies are never repeated. Streams and watch pages are
requested once; their 5xx responses record `youtube_upstream_5xx`. When the feed
still fails, the step ends as retryable `dependency_unavailable` (or
`timeout`/`rate_limited`) without writing, so existing video and event snapshots
remain.

Video thumbnails are registered and linked to their rows, then the bounded media
stage downloads pending files from YouTube's `*.ytimg.com` thumbnail CDN
subdomains (including `i.ytimg.com` and its numbered hosts). A size YouTube
never generated falls back to the next smaller one, and the thumbnail of an
upcoming or recent video is revalidated because its URL stays the same when the
creator replaces the image; see [Media cache](media-cache.md).

#### Follow-up checks and refresh requests

Creators often replace a premiere's placeholder thumbnail, or retitle an
upload, after it goes out. A finished video therefore has its watch page read
again 1 hour, 1 day, 3 days and 7 days after its anchor
(`ytsource.FollowUpOffsets`): the broadcast end the linked event records, or
otherwise its start or publication. A video is due when the newest passed
offset has no successful check (`live_checked_at`) at or after it; anchors
older than eight days are left alone, so an upgrade does not reread the
history. The YouTube step adds at most ten due videos per run behind the
active reservations, so they never starve new discoveries, and uses the same
detail budget, completeness checks and rotating queue.

An operator can also ask for one video from `/admin` (see
[Admin API](admin-api.md#video-corrections-and-history)). The request is
stored as `videos.refresh_requested_at`; the next YouTube step reads that page
before any other detail work, and a successful check clears it. A request
that no successful check answers within 24 hours stops going first.

Every detail check stores the player's `lengthSeconds` as
`duration_seconds` once a video is neither upcoming nor live. When a check
reports a new title or thumbnail URL, the replaced platform state is kept as a
`video_revisions` row (`origin = sync`); a YouTube API history page does the
same for a new title only, because the API names thumbnails differently from
the watch page. After any successful page read, the media step revalidates the
video's thumbnail file whatever its age, and new bytes at the same URL keep
the replaced file as a version (`origin = media`). Operator corrections of the
title, kind or thumbnail are separate columns that synchronization never
writes; see [Video corrections](data-model.md#video-corrections-and-history).

#### Live-status probe

Whether a stream is on air is read from YouTube rather than guessed from its
start time. A separate probe step (`live`) requests **only watch pages**, no
feed and no streams page, for at most five (`ytsource.MaxLiveProbes`) stored
videos per run:

- reservations whose scheduled start is within the next 15 minutes or has
  passed;
- broadcasts that started without a known end.

Both are limited to starts in the last 24 hours. Videos the page already
reported as over, events whose `ends_at` has passed, and manually confirmed
cancellations are left out. Never-checked videos go first, then the least
recently checked, so failures cannot starve the rest. With no active video,
the probe makes no request and queues no run.

`ParseWatch` keeps its completeness checks (matching ID, title, start time for
an upcoming page) and then reads `videoDetails.isLive` / `isUpcoming` and
`microformat.playerMicroformatRenderer.liveBroadcastDetails` (`isLiveNow`,
`startTimestamp`, `endTimestamp`). It exposes `LiveNow` (live and not yet
ended) and `LiveStatus`: `upcoming`, `live`, `ended` (an end time, or
`isLiveNow: false` on a broadcast that is not upcoming), or empty when the page
names none. A live or ended broadcast's `startTimestamp` is its actual start
and replaces the reservation time.

The store writes `videos.live_status` and `live_checked_at`, the actual start,
`is_upcoming`, `ends_at` once reported and the status text through the same
upsert as the regular step. It never adds a video, never moves the feed cursor
or detail queue, keeps manual locks and tombstones, and increments
`dashboard_revision` only when a public video or event field changed. A failed
or incomplete page keeps its snapshot and is asked again on the next run. Each
request uses the client's existing 10-second timeout, 4 MiB body limit and
redirect rejection.

The public dashboard projects `liveStatus` (`live`, `upcoming`, `ended` or
null) on videos and on the events linked to them. `ended` is final; `live` and
`upcoming` count only while the check is younger than two probe intervals, so
stale results fall back to the start-time grace. A fresh `live` video stays
listed beyond that grace. Run timing is described in
[Workflows](workflows.md#live-status-probe).

#### WebSub Push

Polling remains the baseline. The operator turns push on in `/admin` (Content
& media → YouTube push) by entering a public HTTPS callback URL that forwards to
`/api/websub/youtube`. The standalone process then subscribes the channel's
feed topic at Google's hub (`https://pubsubhubbub.appspot.com/subscribe`)
within seconds, without a restart, and again at every startup.
`backend-go/internal/source/youtube/websub` owns the protocol and its admin
module:

- `GET /api/admin/sources/youtube/push` returns the settings and runtime status:
  enabled, callback URL and required path, whether a secret exists,
  verification, lease expiry, next renewal, pending notification, last
  notification/trigger, the last fixed error code, and `available` (false
  while `SKIP_YOUTUBE=1`). `GET /api/admin/sources` carries the same object
  as `youtube.push`. Neither ever includes the secret.
- `PUT /api/admin/sources/youtube/push` accepts `{ "enabled": boolean, "callbackUrl":
string | null }`; omitted fields keep their value. The callback must be an
  absolute HTTPS URL of at most 2048 characters without credentials or a
  fragment. Enabling generates a 256-bit signing secret when none exists.
  `POST /api/admin/sources/youtube/push/rotate-secret` replaces it and resubscribes.
  Settings live in `app_settings` (`youtube_websub_enabled`,
  `youtube_websub_callback_url`, `youtube_websub_secret`); the secret is sealed
  with `LLM_SECRETS_KEY` like provider keys. A secret that no longer decrypts
  after a key change is replaced at startup.
- Disabling push or moving the callback sends an `unsubscribe` for the previous
  callback (at most three attempts; its lease expires anyway). While disabled,
  the callback route answers like an unknown API path, except to confirm that
  withdrawal once within an hour.
- The subscribe request asks for asynchronous verification, a five-day lease,
  and the HMAC secret. It has a 10-second timeout, rejects redirects, discards
  the response body after 64 KiB, and records only fixed codes
  (`websub_hub_timeout`, `websub_hub_http_error`, `websub_hub_denied`, and so on).
  A failed request is retried after 15 minutes.
- `GET /api/websub/youtube` echoes `hub.challenge` (at most 1 KiB) only for this
  channel's topic, and only for a subscription while enabled or the withdrawal
  this process just requested. Only the verification of a request sent within
  the last hour updates the lease, so renewal happens at half the granted lease
  but no more than hourly. When no verification arrives within an hour, the
  status records `websub_verification_missing` and the subscription is
  requested again.
- `POST /api/websub/youtube` reads at most 256 KiB and verifies
  `X-Hub-Signature` (`sha1`, `sha256`, `sha384` or `sha512`) in constant time.
  Unsigned, mis-signed, malformed, deleted-entry, or other-channel bodies are
  acknowledged with `202` and ignored, as WebSub requires.
- A verified notification for a valid video ID only marks a sync as pending.
  After one quiet minute (which also lets the public RSS list the entry), and at
  least two minutes after the previous push-triggered run, it starts the
  `youtube` and `media` steps through the operator job path. The run is labelled
  `websub` and shares the single-flight queue; a busy queue keeps it pending and
  retries every 30 seconds.

The notification never writes snapshots; the RSS and stream-page reads above
do. Live start/end and reservation-time changes are not reliably pushed, so
reservation rechecks still depend on saved workflow timers, and live start/end
on the [live-status probe](#live-status-probe). Runtime state is in
memory: a restart subscribes again, and a missed push is caught by the next
poll.

### Bilibili

The `bilibili` step reads the uploads of `BILIBILI_UID` (Kano's account by
default) from Bilibili's public web API, `api.bilibili.com`, using only
endpoints that need no request signature:

- `/x/frontend/finger/spi` returns the anonymous device identity
  (`buvid3`/`buvid4`) Bilibili's own pages send. It is reused for seven days
  and kept in `sync_state` (source `bilibili`).
- `/x/series/recArchivesByKeywords` lists the account's newest 20 uploads
  with title, cover, publication time, length and description. Entries of
  another uploader are skipped.
- `/x/web-interface/view` reads one upload for an operator.

Bilibili answers a few anonymous requests and then puts the server's address
behind a captcha for hours: HTTP 412, an HTML page, or the JSON codes `-352`
and `-412`. The client never retries and reports these as
`bilibili_risk_control` (`-799`/`-509`/429 as `bilibili_rate_limited`). The
adapter then waits 6 hours, doubled for each consecutive block up to a day,
and the step makes no request until then; independently, list reads are at
least `BILIBILI_MIN_INTERVAL_MINUTES` (180 by default) apart whatever the
workflow timer. A skipped read records `{ skipped: true, reason: "interval" |
"blocked" }`; a failed one records its code as a failed step without a retry,
keeps every stored upload, and lets the rest of the run continue.

`BILIBILI_SESSDATA` optionally adds an operator's login cookie to the API
requests (never to the device-identity request). Signed-in reads meet the
captcha far less often. The cookie stays in memory: it is not part of the
persisted state, not logged and not returned; see
[Configuration](../operations/configuration.md).

Uploads are `videos` rows with `source = 'bilibili'` and the BV ID as their
ID, linked to `https://www.bilibili.com/video/<BV>`. The kind comes from the
title (`翻唱`/cover → `MUSIC`, `直播`/`录播` → `LIVE ARCHIVE`, and so on);
the description and `aid` stay in `raw_json`. Every list read replaces the
platform fields and keeps a replaced title or cover in `video_revisions`, as
YouTube does; operator corrections survive. Covers come only from
`*.hdslb.com`, upgraded to HTTPS, and are cached under `data/bilibili/` by
the media step.

Bilibili uploads stay in the console: the board, the video archive and MCP
list only YouTube videos until an upload can be matched with its YouTube copy,
so one cover is never listed twice. Operators can add an upload by BV ID or
`bilibili.com` video link and read one again at once from `/admin` (see
[Admin API](admin-api.md#bilibili-uploads)); those reads respect the same
backoff and accept only uploads of the configured account.

### Schedule Extraction Pipeline

Schedule extraction has two independent candidate paths:

1. The schedule-board path uses the admin-configurable keyword list to select
   posts that look like a schedule image or board; its posts are recorded with
   the `schedule_board` detection route.
2. The single-message path applies a cheap local date/time and scheduling-verb
   heuristic to ordinary X messages, including messages without a configured
   keyword. Suspected messages are recorded with the `schedule_message`
   detection route, and the model first classifies the post as `schedule`,
   `not_schedule`, or `uncertain` before extracting events. With `schedule_message_all_posts`
   enabled, the path skips the heuristic and selects every X post with text or
   images; each new post then costs one model call, still within the shared
   scan limit.

The model text starts with the stored publication timestamp followed by its
Asia/Tokyo date, weekday, and time, so relative dates such as 今日, 明日, or a
bare weekday resolve without a UTC conversion. Fingerprints use the stored
timestamp alone.

The two candidate lists are merged, de-duplicated by X post ID, sorted by
least recent attempt (unattempted posts first), with publication time as a tie
breaker. The shared limit counts candidates requiring work; unchanged fingerprint
cache hits do not consume it. Failures and blocked candidates move behind older
unattempted work on later scans. Manual reprocess requests still run first. A post selected by the
board path is not sent a second time through the message path in the same scan.

Both paths, and the schedule-image check below, draw from one model priority
queue: an ordered list of targets, each a provider plus one of its catalog
models, and a target's capabilities are its model's `text`/`image` tags.
Providers have no default model. The retired `llm_providers.model` column is
kept but no longer read. A one-time upgrade (`llm_default_model_retired`)
rewrites each queue target that followed it, including legacy provider-only
routes, to name that model. Where the pair was already queued, the earlier
position stays, and a target whose provider had no default is dropped
because it could never be called. Target selection follows
the original post modality. Text-only input requires `text`; image-only input
requires `image`; mixed input requires a model with both tags. Only ready cached images are sent to the model. A post that
declares an image but whose cache is not ready is recorded as `media_pending`
and is not silently reduced to text-only input. Each provider uses OpenAI
Responses, Chat Completions, or Anthropic Messages, receives at most three total calls (one
initial request plus two retries), and then yields to the next enabled, keyed,
capability-compatible target in queue order; targets whose capabilities do
not fit the input are skipped without a call. The persisted
`max_retries` field is retained for old clients and migrated to this fixed
policy; it is not a cost-control override.

### LLM Provider Catalog

`/admin` manages providers centrally. A provider stores the API host, format
(Responses, Chat Completions, or Anthropic Messages), timeout, and encrypted key. On an operator
action the server requests `GET <baseUrl>/models` with the stored key, a
timeout, `redirect: "error"`, and a 4 MB response limit; it accepts OpenAI-style
`{ data: [{ id }] }`, `{ models: [...] }`, or bare arrays. Upstream bodies and
errors are reduced to fixed messages. Tags for newly added models are suggested
from the model ID and stay editable. Removing a model removes its queue
targets. The private
and loopback address guard for provider URLs applies to model discovery too.

API hosts can omit `/v1`: bare origins use `/v1`, while OpenRouter and Gemini
use their preset API roots. Explicit versioned roots and custom paths are
preserved, and a pasted inference endpoint is reduced to its API root.
`src/lib/llm-endpoints.ts` owns this rule for both UI previews and server calls.
Anthropic requests use `x-api-key` and `anthropic-version: 2023-06-01`.
Model discovery follows `has_more`/`last_id` with 100 models per page and a
20-page cap under one timeout. Extraction offers one tool with the existing
JSON schema under `tool_choice: auto` (newer Claude models reject a forced
tool choice) and a system instruction to call it, then validates its
`tool_use.input` locally; a reply without that tool call fails the attempt; cached images are sent
as base64 image blocks. Model and inference responses are bounded to 4 MiB of
streamed bytes, and redirects are rejected. Connection tests and inference share
the protocol-specific output limit: `max_output_tokens` for Responses,
`max_completion_tokens` for Chat Completions, and `max_tokens` for Anthropic.

The instruction keeps titles and cited evidence in the post's wording and asks
for English detail and reason and a short lowercase English status word; an
entry without a status, or with a cancellation-like one, stores `pending`.
A local validator rejects invalid calendar dates, time formats, enumerations,
or confidence values before any event write. `schedule` results update automatic
events. A model never deletes or definitively cancels an event: `action=cancel`
matches the clearest existing event and writes an `llm_suspected` cancellation
overlay containing the reason, source evidence, confidence, and post ID. The
overlay also handles cancellation-like event status strings on add/update
results. Such entries never create a definitively cancelled event or retire
existing source evidence; mixed results still update their scheduled entries.
Cancellation-only entries without an unambiguous existing target remain in
extraction metadata. The overlay applies to manually locked events too, so a manually entered event can
remain visible with the model's cancellation evidence. `not_schedule` and
`uncertain` results only record the extraction outcome and leave existing events
untouched. Input fingerprints (including text, publication time, URL, and cached image
hashes), provider endpoint/model identity, and the extractor version make these
outcomes idempotent. Successful/uncertain posts stay eligible for cache checks
and can run again when their input or provider configuration changes. Date-only events store `starts_on`, a null `starts_at`, and
`time_precision = unknown`; the model must not invent a specific time.
The extractor never deduplicates against other posts or reservations beyond
shared IDs; the later `reconcile` step merges date-only and X-notice
duplicates into the more precise event (see
[Duplicate merging](data-model.md#duplicate-merging)). Re-extraction rewrites
the source row but never its merge columns.

Every stored post has an LLM processing state. Failed, skipped, and uncertain
attempts remain visible to operators, and `POST /api/admin/posts/:id/reprocess`
queues a forced pass that bypasses the successful-result cache. The automatic
scan consumes queued posts even when they no longer match the current keyword
heuristic; a missing provider or queue target or a pending image keeps the reprocess request
pending,
while a provider failure consumes it. Queued posts run before keyword
candidates within the shared scan limit. They use the detection route the
operator requested; without one, a post that a keyword path also selected
keeps that route, image posts use `schedule_board`, and text posts use
`schedule_message`. The route chooses the prompt path, not the models.

Providers and the model queue are configured explicitly in `/admin`; initialization
does not create or enable an OpenAI provider. The legacy model preference comes
from `app_settings.llm_model`. Provider API keys are encrypted with
`LLM_SECRETS_KEY`; ciphertext is never returned by an API response or written
to extraction `raw_json`. A legacy `OPENAI_API_KEY` can be imported once with
`kano-go migrate-llm` (`npm run migrate:llm`) after creating an `openai-default` provider in `/admin`;
synchronization never reads that environment variable.
Candidates from either X account use the same idempotent fingerprint and
manual-lock rules. The stage flags are stored as `schedule_keyword_enabled`,
`schedule_vision_enabled`, and `schedule_message_enabled` and can be changed
independently in `/admin`. `schedule_message_all_posts` is off unless set
explicitly and does not follow the legacy switch. The legacy `schedule_extraction_enabled` setting is
retained for old clients and for seeding the stage defaults; it is no longer a
runtime master switch, so a newly enabled stage can run even when an older
database still contains `0` there. The old admin API field updates all three
stage flags together for compatibility.

The `featured_video_id` setting is maintained through `/admin`. It points to an
existing local `videos` row (or an empty value for no Featured item), and source
synchronization never changes it. The dashboard includes the selected video
even when it falls outside the most recent 30 rows.

Image extraction requires every declared image to have a linked ready cache
file. Missing links, partially ready images, absent files or unsafe paths keep
`media_pending`, preserve a forced reprocess request, and cannot replace an
existing complete extraction or retire its events. Text-only overrides do not
bypass that readiness gate.

### Schedule Images

Public-profile synchronization and official X date-window pages share the same
candidate builder. Each page registers its newest matching image per applicable
Japan Monday week, together with its source account. Explicit source dates win
over relative wording. An omitted year uses the nearest valid year to publication;
December-to-January ranges roll forward across the year. `今週` / `本周` /
`this week` and `来週` / `下周` / `next week` anchor to the Japan publication
week, with next week starting seven days later, including Sunday publication.
Multi-day ranges retain both inclusive ends, even across multiple weeks.
An explicit connected range is selected independently of other date mentions;
multiple ranges or disconnected date lists are ambiguous. Ambiguous, invalid or
absent dates produce a null period, never a publication-week
guess. Operators can correct a period with a required reason independently of
image approval; unknown and legacy periods are labelled in the console. Candidates, post/media records,
and historical continuation checkpoints commit atomically. Older pages or
accounts cannot replace a newer image for that week or the `weekly-schedule`
compatibility alias. Registration does not run extraction or approve an image.

A schedule image (`assets.kind = schedule`) is public only after two gates:

1. Sync promotes a post's image only when the author's own wording, including
   quoted text, explicitly reads as a weekly or multi-day board notice
   (`backend-go/internal/schedule/assets.go`). The stored post `label` is not part of that
   input, because the looser post classifier labels any `配信予定` post as
   `SCHEDULE / 日程`. The configurable extraction keywords do not promote images.
2. The automatic scan sends each unreviewed candidate's ready cached image,
   without the post text, to the first image-capable targets of the model
   queue and records
   `schedule`, `not_schedule`, or `uncertain` in `schedule_asset_reviews`. A
   candidate whose source post no longer passes gate 1 (written by an older
   sync) is recorded as `skipped` / `source_not_board` without a model call. A
   pending cache, or no keyed Image provider, is also `skipped` and retried.
   An asset without an image URL (the legacy `weekly-schedule` alias written
   by the old seed/importer) has nothing to download: it is recorded once as
   `skipped` / `no_image` and left out of later scans, forced ones included,
   until a writer gives it a URL. It is never public and never counts as
   needing review; the console shows it as having no image, and an operator
   may still label it `not_schedule`.

`/api/dashboard` returns a schedule asset only when an operator labelled it
`schedule`, or when it has no manual label, still passes gate 1, and the model
returned `schedule`. A manual `not_schedule` always hides it. Labels require a
reason and are set in the `/admin` schedule-images view. Sync reuses the
`schedule-<week>` IDs and the `weekly-schedule` alias, so a review belongs to
the image URL and version it judged: when an asset ID receives a different URL, its model
verdict and manual label reset to `pending` / `unreviewed`.

Source HTML/XML/JSON reads have a 4 MiB response-byte limit, reject redirects,
and keep the configured timeout active through body consumption. A timed-out or
oversized stream is cancelled and releases its reader so the single-flight queue
can run a subsequent job.

Image review budgets apply after cache checks and manual-label filtering across
all stored candidates, rather than only the newest page. Retry order follows
the least recent check/review attempt. Reviews persist an input fingerprint
including image hash, source gate input, and ordered provider/model targets;
changed content or configuration can invalidate a successful verdict.

### Translation

The `translate` step machine-translates X post text and YouTube video titles
into English and Simplified Chinese (see
[Translations](data-model.md#translations) for storage and precedence).
Bilibili metadata is already Chinese and is never sent to a model, and the
step never requests a source. Its models come only from the separate
translation queue (`llm_route_targets.route = 'translate'`), configured in
`/admin` → Models; it never falls back to the schedule queue. Targets need
the `text` tag, and each follows the same three-call failover as extraction.
Without a usable target the step records `disabled` with the reason and
keeps operator requests.

Candidates are posts and YouTube videos whose current wording lacks a usable
translation of a needed locale: operator requests first (forced passes that
replace machine rows, whatever their age), then never-attempted wording, then
the least recently attempted, newest first. Unrequested candidates are
limited to the last `TRANSLATION_LOOKBACK_DAYS` (30) by publication, and at
most `TRANSLATION_SCAN_LIMIT` (20) are sent per run. A locale already covered
by a manual or platform row is not a reason to call a model.

Wording with only links, hashtags, mentions or emoji is recorded as
`skipped` with language `none` without a call; wording over 4000 characters
is skipped without a call. The model receives the kind (X post or YouTube video
title) and the text, returns `sourceLanguage` plus English and Simplified
Chinese, and is told to keep URLs, tags, mentions, emoji and bracketed
channel tags. A result is accepted only with a known language and a
non-empty, bounded translation for every locale that differs from it. The
result is stored only if the wording still has the hash that was sent.

Platform translations belong to source synchronization, not to this step,
which only reads them. Writers store them through
`UpsertPlatformTranslationTx` and remove them through
`DeletePlatformTranslationTx` (see [Translations](data-model.md#translations)).
No source writes them yet. The planned writers are YouTube titles a creator
localized, read at the follow-up checks (the watch page's
`videoPrimaryInfoRenderer` title under `hl=en` or `hl=zh-CN` differs from the
original, while `videoDetails.title` stays the original), and the Chinese title
of a Bilibili upload merged into a YouTube video.

## Related Docs

- [Workflow and historical fetch windows](workflows.md)
- [Event precedence](data-model.md#event-precedence)
- [Configuration](../operations/configuration.md)
- [Secure development](../development/security.md)

## Budget Fairness

Incremental X rotates the first account across runs using independent
`x-budget` state, so a one-request budget also serves later accounts. Failed
requests consume their turn and budget. Each account retains its own durable
pending-ID queue and incremental cursor. YouTube detail work uses an independent
`youtube-details` durable queue: new discoveries join the tail, attempted work
rotates before its request, failed work remains retryable, and completed ordinary
videos leave the queue. Reservations continue to rotate for refresh. Reopening
SQLite preserves those turns. Date-window work uses separate rotation/queue
identities and never rewrites incremental cursors or their check metadata.
