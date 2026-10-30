# Sources and Schedule Extraction

## Current Sources

### X

The synchronizer discovers status IDs for each account named by `X_HANDLES`
(default `kano_2525,_Kanotic`) from the public FxTwitter timeline
`api.fxtwitter.com/2/profile/<handle>/statuses`, which lists the account's
statuses newest first in pages of 20 with a continuation cursor. Discovery
pages back until a page's oldest own status is already stored or is not newer
than the account's stored cursor, so posts published between runs are found
even when several arrive at once. Before an account has a cursor, statuses
older than `X_BOOTSTRAP_DAYS` (seven by default) also end the first run.
Reposts carry another author and are skipped. The cap, `X_DISCOVERY_LIMIT` or
the X schedule's incremental `max` (see
[source schedules](workflows.md#source-schedules)), counts the
statuses that are not stored yet and sets the pages of 20 (at most 10) per
account and run. A failed later page keeps the newer pages already read.

A run that stops short of the stored cursor, because of the cap, the page
budget, a failed later page or a profile fallback, records the unread stretch
in `sync_state.metadata_json.timelineGap` as `{cursor, floor, older}`: the FxTwitter
continuation (empty for the newest page) and the cursor the run started from.
Later runs read the newest pages first and, when those reach a stored post,
continue the gap with a page budget of their own until a status at or below
`floor` appears with no status above `floor` after it on the page, so an old
pinned post leading the newest page does not close the gap. A second gap is
read first and keeps the earlier one, with its own continuation, as `older`;
once the newer gap reaches its floor, reading goes on from where the older one
stopped. An account keeps at most four gaps; past that the oldest merge into
the last kept one, which then reads on to the oldest floor. A failed
continuation restarts at the newest page. A timeline that ends
before the floor closes the gap and every older one and reports
`x_timeline_gap`, so an operator
can fill it with a timeline `all` fetch or an X API date range. The step result counts
accounts with an open gap as `timelineGaps`
([ADR-0002](../decisions/ADR-0002-count-based-source-sync.md)).

The timeline is the first method of the X schedule's default
[method chain](workflows.md#method-chains). When its first page fails, is
not a timeline, or yields none of the account's own statuses, the chain goes
on to the logged-out profile page `x.com/<handle>`, which lists only the
newest few statuses. Only links whose path is `/<handle>/status/<id>` (handle
case-insensitive, plain or URL-encoded) count; quoted statuses, reposts and
other handles' links are left out, as the timeline leaves them out. The
chain may also enable the [X API](#x-api-in-the-chain). The run records the
method that served in `sync_state.metadata_json.discovery` (`timeline`,
`profile` or `api`) and counts accounts the profile page served as
`profileFallbacks` in the step result. A fallback alone does not fail the
account or mark the run partial. When a method other than the timeline
serves, every gap keeps its floor and drops its FxTwitter continuation, so
the timeline later reads it again from the newest page.

Before an account has a cursor, the Snowflake timestamps of discovered IDs are
decoded and IDs older than `X_BOOTSTRAP_DAYS` are skipped. Each account has
its own cursor in `sync_state`. Later runs prioritize unknown IDs and may
refresh only a small `X_REFRESH_KNOWN` budget.

The timeline pages a run reads already carry each status, so a new or pending
status that a page lists with usable content is stored from the page, through
the same mapping as an operator's manual fetch (below), without a detail
request; posts the [X API](#x-api-in-the-chain) returns are stored the same
way. An `api.vxtwitter.com` detail request remains the fallback for a
status the timeline lists without content, for a pending status the pages no
longer list, for the `X_REFRESH_KNOWN` refreshes, and for every status found
on the logged-out profile page, which lists IDs only.
`X_MAX_STATUS_REQUESTS` is the total detail-request budget shared by all
configured accounts; posts read from the timeline do not use it, and the step
result counts them as `timelinePosts`. A stored post that a page lists again
keeps the text, media and schedule candidate it has, as in a manual fetch.
Successful posts are stored with `account_handle` and returned as one
time-ordered feed. Image-only posts are retained as candidates as long as they
have a publication time and a media URL.

Both paths store the same values for one status, so a post first stored from
the timeline and refreshed through a detail request later keeps its
extraction fingerprint. Text, publication time, URL and counters match as
they are. FxTwitter appends `?name=orig` to a photo's URL and the detail
answer does not, so the timeline mapping drops that size. The detail answer
lists a video's or GIF's file in `mediaURLs`; the adapter reads its
`media_extended` instead and keeps the thumbnail, as the timeline does, and
rewrites `raw_json.mediaURLs` to match. A timeline post's `raw_json` lists its
own images (not a quoted status's) as `mediaURLs` too, because schedule
extraction declares a post's images from that field. A stored video or GIF
post that still names its file takes the thumbnail on its next refresh. A
quoted status is read into the same
[quote projection](#quoted-posts) on both paths.

The operator's timeline fetches read the same timeline differently (see
[Manual fetches](workflows.md#manual-fetches-and-historical-backfill)): they
request pages of 100 and store each own status from the page itself.
The adapter maps FxTwitter's `created_timestamp`, `text`, counters and
`media` (a photo's URL without the `?name=orig` size, or a video's or GIF's
thumbnail, in post order; a quoted status's images belong to its quote) onto
the same post fields the detail path fills, and keeps only the author's name
and handle of the repeated profile, and of a quoted status's, in `raw_json`. Such fetches leave stored posts, the
cursor and the pending queue unchanged.

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
after a process restart. Pending work remains retryable when it falls out
of the profile or ages beyond the bootstrap window, and a pending status that
a timeline page lists with content is stored from the page whatever its
failures. Successful posts, pending-work removal, the failure counts and the
latest successful cursor commit atomically. Budget exhaustion retains
unfinished IDs; an all-failed run does not record a successful cursor or
timestamp. Manual fetches neither consume this queue nor update normal
incremental state.

Failed detail requests are counted per ID in
`sync_state.metadata_json.pendingRetries` (`{failures, gone, retryAt}` by
ID, kept beside the `pendingStatusIds` array; rows without it start with no
failures). A 404 or 410 answer counts as gone, and the second consecutive
gone answer drops the ID with `x_status_gone`. An answer that the status is
another account's (an ID an older profile fallback queued from a quote, say)
is no failure: it drops the ID at once with `x_status_foreign`, without an
error, retry state or partial run, though it still counts as requested. Any
other failure (timeout, 429, 5xx, invalid JSON or response) waits 1 hour after the
first consecutive failure, 6 hours after the second and 24 hours after the
third, as the extraction failure backoff does, and the fourth drops the ID
with `x_retry_limit`; a gone answer waits the same way before its second
request. A waiting ID makes no request, uses none of the budget and keeps its
place in the queue, and a cancelled run counts no failure. A dropped ID leaves the queue for good; a manual fetch can
still store it. The step result counts waiting IDs as `pendingHeld` and lists
dropped ones as `pendingDropped` (`<account>: <id>: <code>`); both keys
appear only when used.

Each discovered image URL is registered and linked to its post. A bounded media
stage later in the same command downloads pending image files from X's
`*.twimg.com` media CDN subdomains.
When a schedule asset already has a source status URL but no image URL, the
synchronizer adds up to `X_SCHEDULE_REFRESH_LIMIT` recent source IDs per
account to the detail-request queue. On a cursorless bootstrap with no asset
source for an account, a bounded set of keyword-matching snapshot posts can
serve the same purpose. These requests supplement discovery without
bypassing the shared `X_MAX_STATUS_REQUESTS` budget and can recover a schedule
post that has fallen out of the discovered status list.

#### X API in the chain

The X schedule's [method chain](workflows.md#method-chains) can enable the
official API (`api`, off by default), which needs `X_API_BEARER_TOKEN`
([ADR-0004](../decisions/ADR-0004-method-fallback-chains.md)). It reads
`https://api.x.com/2/users/<id>/tweets` without reposts and replies, newest
first, with the account's cursor as `since_id`, up to the run's cap of new
posts in pages of 5 to 100 and at most 10 pages; before an account has a
cursor it reads from `X_BOOTSTRAP_DAYS` ago instead. The numeric user ID
comes from `/2/users/by/username/<handle>` once per process. Requests share
the manual X API method's client (see
[X API method](workflows.md#x-api-method)): the token only in the
`Authorization` header, the `SYNC_TIMEOUT_MS` timeout, the 4 MiB limit,
rejected redirects and `source_api_*` codes.

A read that stops short of the end (its cap, the daily cap, the page bound
or a failed later page) opens a [timeline gap](#x) at the cursor. Once the
newest posts are read, each gap is read between its floor (`since_id`) and
the oldest stored status above it (`until_id`), so it closes exactly and
needs no continuation; a read cut short resumes below the oldest status it
stored.

The answer carries what the timeline and detail paths store, and each post
is stored exactly as they store it, so a later timeline read or detail
refresh keeps its extraction fingerprint: a long post's whole text
(`note_tweet`), its `t.co` links replaced by their targets and its own
media links removed, HTML entities decoded; its own images in attachment
order (a photo's `url`, or a video's or GIF's `preview_image_url`); the
like, repost and reply counts; `raw_json.mediaURLs`; and the
[quote projection](#quoted-posts) of the status it quotes, read from the
answer's expansions. Another author's post fails the answer with
`source_api_invalid_records`. Posts read this way need no detail request
and count as `apiPosts` in the step result.

**Gate.** The chain reads the API only after the account's timeline failed
`apiGate` consecutive runs (3 by default, 0–100; 0 opens it at once). The
current run's failure counts, so the third failing run already reads through
the API. A run the timeline serves starts the count over, and a cancelled run
changes nothing. A chain without the timeline has nothing to count and never
waits. A gated method records `x_api_gated` in `tried`, and the chain goes
on without making the run partial.

**Daily cap.** `apiDailyCap` (100 by default, 1–10,000) bounds the posts
X API answers return per Japan day, the unit X bills. The accounts post a few
times a day and a `since_id` read that finds nothing new returns nothing, so
an ordinary day of fallback reads returns a few dozen posts; 100 leaves room
to fill a gap after an outage while a month stays near 3,000 posts at worst.
A request never asks for more posts than are left. With fewer than five left
(the endpoint's smallest page), the chain skips the API with
`x_api_daily_cap` until midnight in Japan, and the run is partial. Manual
X API fetches ignore the gate and the cap, but the posts they read count
toward the day.

Each account's failure streak and the day's usage are kept in `sync_state`
(source `x-api`; see [Sync state](data-model.md#sync-state)). The Sources
page shows today's usage beside the cap and each account's streak, and the
step result adds `apiPosts` and `apiRead`, the posts the answers returned,
when the API was used.

### YouTube

The `youtube` step lists the uploads of `YOUTUBE_CHANNEL_ID` (the current
main channel by default) through the YouTube schedule's
[method chain](workflows.md#method-chains): by default the Data API's uploads
playlist when `YOUTUBE_API_KEY` is set (see [Data API](#data-api)), then the
RSS feed, then the uploads tab. It takes the newest listed uploads and stores the ones
not stored yet; see [source schedules](workflows.md#source-schedules). An
incremental run takes at most the YouTube schedule's `max` (50 by default and
for full syncs; the feed lists 15) uploads that are not stored yet, plus the
stored ones among the newest `max` as detail candidates. A `count` run takes
the newest `count` entries, stored or not. Without a key, a `count` above the
feed's 15 is partial and reports `youtube_count_limited`. The operator's
manual fetches list uploads through their own method instead (see
[Manual fetches](workflows.md#youtube-methods)).
The cursor is the newest listed upload. An incremental run that leaves a new
upload above the stored cursor unread, because of its cap or a failed
playlist page, keeps the cursor, so later runs take the rest while the
listing still reaches it; without a cursor the first run is bounded on
purpose. It separately inspects the streams page and keeps rechecking active or
recent reservations for `scheduledStartTime`, writing them to both `videos` and
`events`. When no schedule time is available, the step does not invent one. RSS insertion
does not count as a reservation detail check, but RSS lists waiting rooms and
premieres like ordinary uploads, so the uploads a run takes are detail
candidates as well, after stream-page discoveries. A watch
page whose player reports `isLiveContent: false` with a start time is a
premiere: its event is a `release` and its video keeps the title-derived kind.
`isLiveContent: true` is a stream verdict only on a page that reports the
broadcast over, since a premiere can read as live content before it ends.
The verdict is stored as `videos.is_premiere` (1 for a premiere, 0 for live
content), and a check that cannot tell, such as a page without
`isLiveContent` or a waiting room or broadcast on air that reads as live
content, keeps the stored one for the video and its event. Until a verdict
exists the video and its event are a stream.
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
attempts. Manual fetches
do not write this incremental metadata.

Feed failures record codes with their own `youtube_feed_` prefix
(`youtube_feed_upstream_5xx`, `youtube_feed_unavailable`,
`youtube_feed_http_error`, `youtube_feed_timeout`,
`youtube_feed_rate_limited`, and so on), so the run view tells them apart
from the streams page, the uploads tab and watch pages, which keep the
`youtube_` prefix. The public RSS feed returns intermittent HTTP 5xx
responses that clear within seconds, so the feed request alone is repeated up
to two more times after an upstream 5xx (`youtube_feed_upstream_5xx`) or a
connection failure (`youtube_feed_unavailable`), waiting 0.5 and then 1.5
seconds. Each try keeps the 10-second request timeout, and cancellation stops
the wait. Rate limiting, other 4xx responses (`youtube_feed_http_error`),
timeouts, redirects, and oversized bodies are never repeated. Streams and
watch pages are requested once; their 5xx responses record
`youtube_upstream_5xx`.

The feed can also answer 404 for hours at a time, so a failed listing does not
stop the step. It records the code and still works through stored detail
work: refresh requests, the rotating queue, active reservations and due
follow-ups. The chain's next method, by default the channel's uploads tab
(`/channel/<id>/videos`), is read once in its place, and its newest 15 IDs
are detail candidates, after the streams page's. The tab has no publication times, so
those IDs add no unchecked rows and leave the cursor alone; a watch page
supplies a new video's time from `publishDate` (a stored time always wins).
The step result reports where the uploads came from as `listing`: `api`,
`rss`, `videos_page`, or empty when nothing answered, `tried` lists every
listing method tried with its code, and a run that recorded
failures lists their controlled codes as `errors`, each once in order of
first appearance with `×n` when it occurred n times (for example
`youtube_watch_details_incomplete ×12`). The run view in `/admin` shows such
lists of plain values joined by commas. Only a run with no
listing (the uploads tab counts as one) and no successful detail check
fails, as retryable `dependency_unavailable` (or `timeout`/`rate_limited`)
after the feed's code. It writes no video, event, cursor or incremental
metadata, so existing snapshots remain; it stores only the rotated detail
queue, so IDs whose checks keep failing do not starve the rest.

#### Data API

With `YOUTUBE_API_KEY` set and the chain's `api` method on, the recent step
asks the YouTube Data API:

- `playlistItems.list` on the channel's uploads playlist (`UU` and the channel
  ID after `UC`) is the listing, read like feed entries: the video's
  publication time (or the entry's), the title and the largest `*.ytimg.com`
  thumbnail. Private entries are skipped. Pages of 50 (one quota unit each,
  at most four) follow `nextPageToken` until the run has its uploads (new
  ones, for an incremental run). An incremental run also stops at a page that
  reaches the stored cursor (an entry at or below its time), or without a
  cursor at a page that holds a stored video, so uploads an earlier capped
  run left below newer stored ones are still read. A failed
  later page keeps the earlier ones. When the first page fails or lists no
  upload, the chain's next method is tried.
- One `videos.list` call (`snippet`, `contentDetails`, `liveStreamingDetails`,
  `localizations`) covers the first 50 IDs of the detail queue, and any room
  left in it goes to the newest listed uploads of the run, stored or new. A
  described queued video counts as a successful detail check without a
  watch-page request: its title, length (ISO 8601 `duration`), and for a
  broadcast the actual start (or the reservation), the actual end and
  `liveBroadcastContent` as `upcoming`/`live`/`ended`. The watch-page budget
  (`YOUTUBE_MAX_DETAIL_REQUESTS`) then goes to the queued IDs the API left out.
  An answer for a listed upload outside the queue only refreshes its
  [localized titles](#localized-titles).
- The API has no `isLiveContent`. An upcoming or live broadcast with a length
  is a premiere, since a stream reports `P0D`; one without a length gets no
  verdict, since a premiere can report `P0D` before it ends too.
- An ended broadcast is classified by the channel's per-format playlists.
  When a queued video is an ended broadcast that has no stored premiere
  verdict, the run reads the newest page of the live playlist (`UULV` and the
  channel ID after `UC`) and of the long-form playlist (`UULF` and the
  channel ID after `UC`), one `playlistItems.list` call and one quota unit
  each, counted in `apiRequests`, at most once per `videos.list` call. YouTube lists
  streams on the live playlist and premieres with ordinary uploads on the
  long-form one, so an ended broadcast on the live playlist is a stream and one
  on the long-form playlist is a premiere. The playlists are undocumented,
  like the [Shorts playlist](#shorts), and are read only after the
  end, so a listing before or during a broadcast decides nothing. A video on
  neither page (members-only streams, Shorts, older broadcasts) or on both
  gets no verdict from them, and a failed read records its `youtube_api_` code
  and decides nothing. A stored premiere verdict is final and is never looked
  up, while a stored stream verdict is looked up again and replaced when the
  long-form playlist names the video.
- Without such a verdict, an ended broadcast is classified from its stored
  row only when an earlier detail check gave it a start time and a stored
  `is_premiere` verdict; the kind cannot tell, since a premiere keeps its
  title kind, which may be a live one. Otherwise, as for a row without a
  stored verdict, an untitled video or an upcoming one without a start, its
  watch page decides.
- When that watch page fails for an ended broadcast the API gave a start
  (`ytsource.Video.Unclassified`), the API answer is used anyway rather than
  leaving the video unchecked, since some hosts never get a readable page. It
  writes the title, thumbnail, actual start and end, length and `ended`
  status, sets `live_checked_at` and answers a refresh request, while the
  stored kind (a stored `UPCOMING LIVE` becomes `LIVE ARCHIVE`), the
  `is_premiere` verdict and the reservation event's type and detail stay. Such
  an answer updates an existing reservation event but creates none, and the
  failed page's code is still recorded. The `reconcile` step adds the event of
  an ended broadcast that has none (see
  [Duplicate merging](data-model.md#duplicate-merging)). A later readable page
  settles the verdict as usual.
- A thumbnail that is the same image as the stored one keeps the stored URL,
  so the watch page's `?v=` version does not read as a replacement; the media
  step still revalidates the file. See [Video corrections](data-model.md#video-corrections-and-history)
  for what counts as the same image.

A run that finds a stored video on its first playlist page spends two quota
units (of the default 10,000 a day), whatever the queue length; each further
page adds one, and the per-format playlists two more when a queued ended
broadcast needs them. The key is sent only in the `X-Goog-Api-Key` header to
`https://www.googleapis.com/youtube/v3/`, with the client's 10-second timeout,
4 MiB limit and rejected redirects. Failures record controlled codes
(`youtube_api_forbidden` for a refused key or quota, `youtube_api_rate_limited`,
`youtube_api_http_error`, `youtube_api_invalid_response`, and so on) and fall
back to the public pages; `apiRequests` in the step result counts the calls.
The [live-status probe](#live-status-probe) asks the same `videos.list`
first, one call and one quota unit per probe run, under the same
classification rules. An operator's manual API fetch checks the videos it
lists under these rules too, without watch pages; a whole-playlist fetch
reads the per-format playlists to their end rather than their newest pages
(see [YouTube methods](workflows.md#youtube-methods)).

A YouTube chain with `api` off makes no Data API call for its runs, key or
not: no listing, `videos.list`, per-format or Shorts playlist, and the
[live-status probe](#live-status-probe) reads watch pages only. Watch pages
take over as they do without a key.

#### Localized Titles

Titles the creator localized on YouTube are stored as
[platform translations](data-model.md#translations) with platform `youtube`.
Only a `videos.list` answer carries them, from its `localizations` part, which
costs no extra quota unit; both the recent step's call and the live-status
probe's call write them. Language keys map to the target locales: `en` and
any `en-*` variant to `en`; `zh`, `zh-CN`, `zh-Hans` and `zh-Hans-*` to
`zh-CN`. Traditional Chinese (`zh-TW`, `zh-Hant`, …) and other languages are
ignored. When several keys fill one locale, the key naming the locale wins,
then the first in sort order. A blank title, one equal to the video's
original title, or one over `MaxTranslationRunes` is no translation.

For every stored YouTube video an answer described, the writer upserts the
locale's `youtube` row in the source's transaction and deletes it when the
answer no longer fills that locale. A video the API did not answer, or a run
whose call failed, keeps its rows. Manual and machine rows are never touched,
and the dashboard revision moves only when readers see the change: a manual
row of the locale hides the platform one. A video whose locales are all
covered is not sent to the [translate step](#translation)'s model.

Without `YOUTUBE_API_KEY` no localized title is read. A watch page requested
with `hl=en` or `hl=zh-CN` shows the localized title in
`videoPrimaryInfoRenderer`, but reading it would add two page requests per
video to the bounded watch-page budget and the public pages' rate limits, and
the page localizes by more than the query, so localized titles need the key.

Video thumbnails are registered and linked to their rows, then the bounded media
stage downloads pending files from YouTube's `*.ytimg.com` thumbnail CDN
subdomains (including `i.ytimg.com` and its numbered hosts). A size YouTube
never generated falls back to the next smaller one, and the thumbnail of an
upcoming or recent video is revalidated because its URL stays the same when the
creator replaces the image; see [Media cache](media-cache.md).

#### Shorts

The feed is the signal that tells a Short apart. Each entry's
`<link rel="alternate">` points a Short to `https://www.youtube.com/shorts/<id>`
and every other upload, stream or premiere to its `watch?v=<id>` page
(`ytsource.Video.Short`). Only an HTTPS link on `www.youtube.com` or
`youtube.com` naming the entry's own ID counts; no link, another host or a
link for a different ID leaves the format unknown. Every run records the
verdict of each listed entry as `videos.is_short` (1 or 0) on the stored row,
new or old, after the video upsert; a verdict for a video the snapshot does
not hold adds no row, and a changed value advances the dashboard revision.
The stored `url` stays the canonical watch page. Watch pages, the live probe,
manual fetches and Bilibili never write `is_short`, so a later detail check
cannot clear it. Rows neither the feed nor the Shorts playlist below named
stay `NULL` (unknown); the length is never used to guess. The step result's
`shorts` counts the uploads marked as Shorts in that run, so a feed that
stops naming the format shows as zero.

When the Data API supplies the listing, the feed is still read for these
verdicts alone, a fixed helper read whether or not the chain enables `rss`. If it fails, its code is recorded and one `playlistItems.list`
page (one quota unit, counted in `apiRequests`) of the channel's Shorts
playlist (`UUSH` and the channel ID after `UC`) stands in: every upload it
names that the run listed or the snapshot holds is marked a Short (1). The
playlist is undocumented, and an upload missing from it is not proof of a
regular video, so it never writes 0; only the feed does. It goes to the same
Data API destination under the same rules as the uploads playlist. A failure,
such as the 404 of a channel without Shorts, records its `youtube_api_` code
after the feed's, and that run marks no Shorts. Without a key, a failed feed
marks none. An operator correction (`manual_is_short`) lies over the stored
verdict; see [Video corrections](data-model.md#video-corrections-and-history).

#### Follow-up checks and refresh requests

Creators often replace a premiere's placeholder thumbnail, or retitle an
upload, after it goes out. A finished video therefore has its watch page read
again 1 hour, 1 day, 3 days and 7 days after its anchor
(`ytsource.FollowUpOffsets`): the broadcast end the linked event records, or
otherwise its start or publication. A video is due when the newest passed
offset has no successful check (`live_checked_at`) at or after it; anchors
older than eight days are left alone, so older videos are not reread. The YouTube step adds at most ten due videos per run behind the
active reservations, so they never starve new discoveries, and uses the same
detail budget, completeness checks and rotating queue.

An operator can also ask for one video from `/admin` (see
[Admin API](admin-api.md#video-corrections-and-history)). The request is
stored as `videos.refresh_requested_at`; the next YouTube step reads that page
before any other detail work, and a successful check clears it. The page is
read even when the [Data API](#data-api) describes the video, so a refresh
can settle a premiere verdict that the API answer of an ended broadcast only
repeats; when the page fails, the API answer is used. A request
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
start time. A separate probe step (`live`) asks only about the videos it
checks, through the Data API when `YOUTUBE_API_KEY` is set and the YouTube
chain enables `api`, and otherwise their watch pages, with no feed, playlist or streams page, for at most five
(`ytsource.MaxLiveProbes`) stored videos per run:

- reservations whose scheduled start is within the next 15 minutes or has
  passed;
- broadcasts that started without a known end.

Both are limited to starts in the last 24 hours. Videos the page already
reported as over, events whose `ends_at` has passed, and manually confirmed
cancellations are left out. Each active video is due by its phase: once before
its start, every 3 minutes from the start until the page reports it live (for
up to 45 minutes), then every operator interval (see
[Workflows](workflows.md#live-status-probe)). A run requests only due videos,
the earliest due first and, among equals, never-checked videos before the
least recently checked, so failures cannot starve the rest. With nothing due,
the probe makes no request and queues no run.

`ParseWatch` keeps its completeness checks (matching ID, title, start time for
an upcoming page) and then reads `videoDetails.isLive` / `isUpcoming` and
`microformat.playerMicroformatRenderer.liveBroadcastDetails` (`isLiveNow`,
`startTimestamp`, `endTimestamp`). It exposes `LiveNow` (live and not yet
ended) and `LiveStatus`: `upcoming`, `live`, `ended` (an end time, or
`isLiveNow: false` on a broadcast that is not upcoming), or empty when the page
names none. A live or ended broadcast's `startTimestamp` is its actual start
and replaces the reservation time.

With a key and the chain's `api` method on, one `videos.list` call
(`snippet`, `contentDetails`,
`liveStreamingDetails`, `localizations`) asks about all of a run's due videos
before any watch page, and is read as in the [Data API](#data-api) detail
check, including the [localized titles](#localized-titles).
`liveBroadcastContent` `upcoming` with a `scheduledStartTime` is `upcoming`;
`live` without an `actualEndTime` is `live` and sets `LiveNow`; an
`actualEndTime` is `ended` and writes the event's `ends_at`. The
`actualStartTime`, when present, is the actual start and replaces the
reservation time, as `startTimestamp` does. A length marks an upcoming or
live premiere, and an ended broadcast takes the verdict of the
per-format playlists, read as in the recent step, or else the stored
`is_premiere` verdict, which the probe loads with its targets. A video's
watch page is still read when the call fails (its code is recorded and
every video falls back), when the answer leaves the video out, when it
names no broadcast state, or when it cannot classify the video, such as an
ended broadcast with neither verdict or an upcoming one without a
start. If that page fails too, an ended broadcast with an API start is
written unclassified, as in the recent step. `apiRequests` in the run
result counts the calls; `requested` counts watch pages. Each run costs one
quota unit whatever the number of due videos, two more when an ended
broadcast needs the per-format playlists, and nothing while nothing is
due; the probe's worst case of 30 runs an hour is 720 units a day, and
together with a regular YouTube step every 15 minutes (two units per run)
stays under a tenth of the default 10,000. Without either the probe reads
watch pages only.

The store writes `videos.live_status` and `live_checked_at`, the actual start,
`is_upcoming`, `ends_at` once reported and the status text through the same
upsert as the regular step. It never adds a video, never moves the feed cursor
or detail queue, keeps manual locks and tombstones, and increments
`dashboard_revision` only when a public video or event field changed. A Data
API answer goes through the same upsert. In the same transaction it records
when each video was asked about, by the API or its page, in the
`youtube-live` `sync_state` row. A video neither answered keeps its
snapshot and is asked again when its phase next makes it due. Each request
uses the client's existing 10-second timeout, 4 MiB body limit and redirect
rejection; the key travels only in the `X-Goog-Api-Key` header.

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
  YouTube schedule's chain (`youtube`, `media`, `translate`, then
  `reconcile`), read with that schedule's sync setting, through the operator
  job path. The run is labelled
  `websub` and shares the single-flight queue; a busy queue keeps it pending and
  retries every 30 seconds.

The notification never writes snapshots; the RSS and stream-page reads above
do. Live start/end and reservation-time changes are not reliably pushed, so
reservation rechecks still depend on the YouTube schedule, and live start/end
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
least `BILIBILI_MIN_INTERVAL_MINUTES` (180 by default) apart, which is also
the shortest Bilibili schedule interval. A skipped read records `{ skipped: true, reason: "interval" |
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
list only YouTube videos, so one cover is never listed twice. The `reconcile`
step merges an upload into the YouTube video of the same work, which then
lists it in `alternates` and shows its title as the video's Chinese platform
translation; operators confirm, add or undo merges in `/admin` (see
[Video merging](data-model.md#video-merging)). Operators can
add an upload by BV ID or `bilibili.com` video link and read one again at
once from `/admin` (see [Admin API](admin-api.md#bilibili-uploads)); those
reads respect the same backoff and accept only uploads of the configured
account. They run outside the workflow queue, so the state write keeps a
block that a concurrent read stored meanwhile (one still active and later
than the writer's), with its failure count and code: a captcha backoff is
never lost. A read that changes a merged upload's title rewrites the
translation in the same transaction.

### Source Request Limits

Every source client requests fixed destinations with a 10-second timeout
(the X API, manual or in the chain, uses `SYNC_TIMEOUT_MS`; see
[X API method](workflows.md#x-api-method))
that stays active through body consumption, rejects redirects, and records
only controlled error codes. Response bodies are limited to 4 MiB for X and
YouTube and 2 MiB for Bilibili; a timed-out or oversized stream is cancelled
and releases its reader so the single-flight queue can run a subsequent job.

The YouTube client builds every public URL itself on `https://www.youtube.com`:
`/feeds/videos.xml?channel_id=<id>`, `/channel/<id>/streams`,
`/channel/<id>/videos` and `/watch?v=<id>`, after checking that the channel
ID matches `UC` plus 22 characters and the video ID 11 characters. The Data
API is reached only at `https://www.googleapis.com/youtube/v3/` (see
[Data API](#data-api)). A redirect answer is reported as
`youtube_redirect_rejected` and an oversized body as
`youtube_response_oversized`, with `youtube_feed_` in place of `youtube_` for
the feed.

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
cache hits and posts held back by the failure backoff below do not consume it.
Failures and blocked candidates move behind older
unattempted work on later scans. Manual reprocess requests still run first. A post selected by the
board path is not sent a second time through the message path in the same scan.

#### Extraction Failure Backoff

A post fails when every compatible queue target exhausted its calls. The
scan then records a consecutive failure count for that input (its fingerprint
plus the ordered targets that were tried) and leaves the post alone for 1 hour
after the first failure, 6 hours after the second and 24 hours after the
third. The fourth consecutive failure stops automatic retries. A post held
back this way makes no provider call, keeps its `failed` state and last error,
and is counted as `backoff` (waiting) or `retryStopped` in the step result;
these keys appear only when a post was held back, and held posts are not
failures of that run. X timers run as often as every 15 minutes and each
failure already costs up to three calls per target, so the first wait covers a
short provider outage, the 24-hour wait a daily quota reset, and a post that
still fails after about 31 hours most likely fails on its content.

The backoff starts over when the input changes (edited text, publication
time, URL or cached images), when the queue's targets or models change, and
when an operator requests a reprocess, which also clears it immediately. A
queued reprocess request always runs, whatever the backoff, and a forced pass
that fails counts as the first failure. A success or `uncertain` result,
including a cache hit, clears the backoff. A cancelled run is not a failure.
`skipped` outcomes that make no call (`media_pending`, `no_content`, no
configured or compatible target) are not failures, keep any earlier count,
and stay eligible for later scans. The console shows the next retry time or
that retries stopped next to the post's state. A failed post without a
recorded failure count has no backoff and is tried on the next scan.

Both paths, and the schedule-image check below, draw from one model priority
queue: an ordered list of targets, each a provider plus one of its catalog
models, and a target's capabilities are its model's `text`/`image` tags.
Providers have no default model, and nothing reads the
`llm_providers.model` column. A one-time startup upgrade, recorded as
`llm_default_model_retired`, rewrites each queue target without a model
(including legacy provider-only routes) to name its provider's stored model.
Where that pair is already queued, the earlier position stays, and a target
whose provider has no stored model is dropped because it could never be
called. Target selection follows
the original post modality. Text-only input requires `text`; image-only input
requires `image`; mixed input requires a model with both tags. Only ready cached images are sent to the model. A post that
declares an image but whose cache is not ready is recorded as `media_pending`
and is not silently reduced to text-only input. Each provider uses OpenAI
Responses, Chat Completions, or Anthropic Messages, receives at most three total calls (one
initial request plus two retries), and then yields to the next enabled, keyed,
capability-compatible target in queue order; targets whose capabilities do
not fit the input are skipped without a call. `llm_providers.max_retries`
always stores 2 (startup sets every row to it), and the admin API validates
but does not store a different `maxRetries`; it is not a cost-control
override.

### LLM Provider Catalog

`/admin` manages providers centrally. A provider stores the API host, format
(Responses, Chat Completions, or Anthropic Messages), timeout, and encrypted key. On an operator
action the server requests `GET <baseUrl>/models` with the stored key, a
timeout, rejected redirects, and a 4 MiB response limit; it accepts OpenAI-style
`{ data: [{ id }] }`, `{ models: [...] }`, or bare arrays. Upstream bodies and
errors are reduced to fixed messages. Tags for newly added models are suggested
from the model ID and stay editable. Removing a model removes its queue
targets. The private
and loopback address guard for provider URLs applies to model discovery too.

API hosts can omit `/v1`: bare origins use `/v1`, while OpenRouter and Gemini
use their preset API roots. Explicit versioned roots and custom paths are
preserved, and a pasted inference endpoint is reduced to its API root.
`backend-go/internal/llmadmin/catalog.go` applies this rule to server calls,
and `src/lib/llm-endpoints.ts` applies the same rule to UI previews.
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
shared IDs; the later `reconcile` step gives an event for a linked YouTube
video the video's start and merges date-only and X-notice duplicates into the
more precise event (see
[Duplicate merging](data-model.md#duplicate-merging)). Re-extraction rewrites
the source row but never its merge columns.

Every stored post has an LLM processing state. Failed, skipped, and uncertain
attempts remain visible to operators, and `POST /api/admin/posts/:id/reprocess`
queues a forced pass that bypasses the successful-result cache. The automatic
scan consumes queued posts even when they no longer match the current keyword
heuristic; a missing provider or queue target or a pending image keeps the reprocess request
pending,
while a provider failure consumes it and starts the failure backoff over. Queued posts run before keyword
candidates within the shared scan limit. They use the detection route the
operator requested; without one, a post that a keyword path also selected
keeps that route, image posts use `schedule_board`, and text posts use
`schedule_message`. The route chooses the prompt path, not the models.

Providers and the model queue are configured explicitly in `/admin`; initialization
does not create or enable an OpenAI provider. Provider API keys are encrypted with
`LLM_SECRETS_KEY`; ciphertext is never returned by an API response or written
to extraction `raw_json`. A legacy `OPENAI_API_KEY` can be imported once with
`kano-go migrate-llm` (`npm run migrate:llm`) after creating an `openai-default` provider in `/admin`;
synchronization never reads that environment variable.
Candidates from either X account use the same idempotent fingerprint and
manual-lock rules. The stage flags are stored as `schedule_keyword_enabled`,
`schedule_vision_enabled`, and `schedule_message_enabled` and can be changed
independently in `/admin`. The legacy `schedule_extraction_enabled` setting
(or `SCHEDULE_EXTRACTION_ENABLED`) is only the default for a stage flag that
is not set; it is not a runtime master switch, so an enabled stage runs even
when it contains `0`. `schedule_message_all_posts` is off unless set
explicitly and does not follow the legacy setting.

The `featured_video_id` setting is maintained through `/admin`. It points to an
existing local `videos` row (or an empty value for no Featured item), and source
synchronization never changes it. The dashboard includes the selected video
even when it falls outside the most recent 30 rows.

Image extraction requires every declared image to have a linked ready cache
file. Missing links, partially ready images, absent files or unsafe paths keep
`media_pending`, preserve a forced reprocess request, and cannot replace an
existing complete extraction or retire its events. Text-only overrides do not
bypass that readiness gate.

### Quoted Posts

A status a post quotes (`qrt` in the detail answer, `quote` on a timeline
page) is never a post of its own: it does not enter `posts`, the timeline
cursor, the pending queue or extraction. The adapter
(`backend-go/internal/source/x/quote.go`) reads it into a sanitized
projection stored beside the quoting post in `posts.quote_json`: the quoted
status ID, its author's handle and display name, its wording (as written,
untranslated), its publication time, and an x.com URL built from the
validated ID and handle rather than copied from the payload. Its images (a
photo, or a video's or GIF's thumbnail, at most four) are linked to the
quoting post with the `quote-image` role and cached like the post's own
(see [Media cache](media-cache.md)); `quote_json` holds no image address.

A quote the payload names but does not carry is stored as unavailable with
only its ID and URL: vxtwitter's `qrtURL` without `qrt`, an FxTwitter quote
typed as anything other than a status (a tombstone), or a quoted status
without an author or content. A payload that names no usable status ID
stores no quote. X API answers carry the quoted status as an expansion; one
X does not return is stored as unavailable in the same way.

An operator can hide a post's quote from the public pages
(`posts.quote_hidden_at`); synchronization, the X API fetch method and the
startup quote backfill (which fills a missing `quote_json` from the post's
stored `raw_json`) keep updating the stored quote underneath and never write
that decision.

#### Refresh from the Source

An operator's refresh (`POST /api/admin/posts/{id}/refresh`) only stores
`posts.refresh_requested_at` and removes the status's pending failure
backoff. The next X recent synchronization reads up to 20 such posts per
account, oldest request first, and puts them ahead of the queue. Under a
`count` sync setting, whose timeline read leaves the incremental state as it
is, the step makes these detail requests after that read and alone: no
discovery, pending queue or change to the incremental cursor, pending IDs or
timeline gap. Each takes
a detail request even when a timeline page lists the status (which would
otherwise store it from the page and leave the stored post unchanged) and
even while a failure backoff holds it. A usable answer rewrites the post,
its quote and its `quote-image` links like any detail answer; the hide
decision stays. The request is cleared once its detail request was made,
whatever it answered; a failed request is then pending like any other. A
refresh shares the `X_MAX_STATUS_REQUESTS` budget, so with a budget of 0 it
waits. The step result counts made refreshes as `refreshed`, only when
there were any.

**Quoted images are never schedule candidates.** A post's `MediaURLs`, its
`raw_json.mediaURLs` and its `post-image` links are its own images only, so
a post that has only a quoted image becomes a text post for keyword
candidates and extraction, whatever its wording says.

### Schedule Images

Recent X synchronization and manual X fetches (timeline and API pages) share
one candidate rule: a post with an image of its own whose text contains one
of the fixed source keywords (`schedule`, `スケジュール`, `配信予定`,
`週間予定`, `今週の予定`, case-insensitive; the configurable extraction
keywords play no part) registers its first image as the schedule asset
`schedule-x-<post ID>`, with its source URL and account. The asset, its
`schedule-image` media link and a `pending` / `unreviewed` review commit
with the post, and with the page's continuation checkpoint for a manual fetch.
Registration does not run extraction or approve an image, and it writes no
period.

A schedule asset's period is `period_start`/`period_end` with its
`period_basis`; the schema upgrade that adds these columns fills them from a
stored `week_start` as a seven-day period. An asset without a period is unknown and labelled so
in the console. Operators set or correct a period with a required reason
(`manual_period_start`, `manual_period_end`, `manual_period_reason`)
independently of image approval, and the manual period wins. The dashboard
and calendar list an approved asset only while its period overlaps the
requested dates.

A schedule image (`assets.kind = schedule`) is public only after two gates:

1. The board-notice gate (`scheduleBoardPattern`,
   `backend-go/internal/schedule/store.go`) checks that the source post's
   own wording (its stored `raw_json.search_text` when present, otherwise
   its text) reads as a weekly or multi-day board notice. The stored post `label` is not part of that input, because the
   looser post classifier labels any `配信予定` post as `SCHEDULE / 日程`.
2. The automatic scan sends each unreviewed candidate's ready cached image,
   without the post text, to the first image-capable targets of the model
   queue and records `schedule`, `not_schedule`, or `uncertain` in
   `schedule_asset_reviews`. A candidate whose source post does not pass
   gate 1 is recorded as `skipped` / `source_not_board` without a model
   call. A pending cache, or no keyed Image provider, is also `skipped` and
   retried. An asset without an image URL, such as a `weekly-schedule`
   alias row, has nothing to download: it is recorded once as `skipped` /
   `no_image` and left out of later scans, forced ones included, until a
   writer gives it a URL. It is never public and never counts as needing
   review; the console shows it as having no image, and an operator may
   still label it `not_schedule`.

`/api/dashboard` returns a schedule asset only when an operator labelled it
`schedule`, or when it has no manual label, still passes gate 1, and the model
returned `schedule`. A manual `not_schedule` always hides it. Labels require a
reason and are set in the `/admin` schedule-images view. A model verdict
belongs to the image URL it judged: when a post's asset receives a different
URL, its `asset_version` increases and its model verdict returns to
`pending`, while a manual label stays.

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
the `text` tag. Each target gets at most three calls before moving to the
next configured target, in queue order.
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
The translation fields hold the complete displayed text. Preserving an
untranslated song title or name means including that wording in the result,
even if the whole title stays unchanged; it never justifies an empty field.

Translation uses native JSON Schema output for all three provider protocols;
Anthropic sends `output_config.format` and reads text blocks rather than
asking the model to call a result tool. Schedule extraction keeps its tool
protocol. Haiku 5.5 translation requests explicitly disable thinking and set
`effort` to `low`; these model-specific options are not sent to other models.
The initial output budget is `max(1024, 400 + 3 * source characters)`, capped
at 4000 tokens. A truncated response is never saved, even if its JSON parses:
the next call doubles the budget up to 4000, without a backoff wait. Invalid
JSON, missing fields or invalid translation values get one corrective prompt;
a repeated validation failure moves to the next target. Connection failures,
timeouts, HTTP 408/429 and 5xx retry with bounded backoff. Other HTTP errors
and model refusals move directly to the next target. No target is added to
the operator's queue automatically.

The protected run's translation counts include `diagnostics`: at most 600
model-attempt entries with owner type/ID, configured provider/model, attempt,
output budget, allowlisted stop reason, bounded input/output token usage
when reported, and a fixed error category. Additional entries increment
`diagnosticsOmitted`. No response text, reasoning, refusal wording, headers
or credentials are stored in these diagnostics. Public projections omit them.

Platform translations belong to source synchronization, not to this step,
which only reads them. Writers store them through
`UpsertPlatformTranslationTx` and remove them through
`DeletePlatformTranslationTx` (see [Translations](data-model.md#translations)).
Two writers store them: the YouTube source, for the
[titles a creator localized](#localized-titles) as a Data API answer reports
them (platform `youtube`, `en` and `zh-CN`), and the
[video merge](data-model.md#video-merging),
for the title of a Bilibili upload merged into a YouTube video (platform
`bilibili`, `zh-CN`).

### Budget Fairness

Incremental X rotates the first account across runs using independent
`x-budget` state, so a one-request budget also serves later accounts. Failed
requests consume their turn and budget. Each account retains its own durable
pending-ID queue and incremental cursor. YouTube detail work uses an independent
`youtube-details` durable queue: new discoveries join the tail, attempted work
rotates before its request, failed work remains retryable, and completed ordinary
videos leave the queue. Reservations continue to rotate for refresh. Reopening
SQLite preserves those turns. Manual fetches use separate rotation/queue
identities and never rewrites incremental cursors or their check metadata.

## Related Docs

- [Workflows and manual fetches](workflows.md)
- [Event precedence](data-model.md#event-precedence)
- [Configuration](../operations/configuration.md)
- [Secure development](../development/security.md)
