# Sources and Schedule Extraction

## Current Sources

### X

The synchronizer discovers status IDs for each account named by `X_HANDLES`
(default `kano_2525,_Kanotic`) from the public FxTwitter timeline
`api.fxtwitter.com/2/profile/<handle>/statuses`, which lists the account's
statuses newest first in pages of 20 with a continuation cursor; the page
size is fixed whatever `count` a request names. Discovery
pages back until a page's oldest own status is already stored or is not newer
than the account's stored cursor, so posts published between runs are found
even when several arrive at once. Before an account has a cursor, statuses
older than the first-run age limit (seven days by default; see
[sync settings](../operations/configuration.md#sync-settings)) also end the first run. A repost
lists the original status's ID, time and author, so it is never a post: the
run records it as a repost link (see [Reposts](#reposts)). The cap, the full-sync post cap or
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
([source schedules](../architecture/workflows.md#source-schedules)).

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
decoded and IDs older than the first-run age limit are skipped. Each account has
its own cursor in `sync_state`. Later runs prioritize unknown IDs and may
refresh only a small budget of known posts.

The timeline pages a run reads already carry each status, so a new or pending
status that a page lists with usable content is stored from the page, through
the same mapping as an operator's manual fetch (below), without a detail
request; posts the [X API](#x-api-in-the-chain) returns are stored the same
way. An `api.vxtwitter.com` detail request remains the fallback for a
status the timeline lists without content, for a pending status the pages no
longer list, for the known-post refreshes, and for every status found
on the logged-out profile page, which lists IDs only.
The status requests per run setting is the total detail-request budget shared by all
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
read the same pages of 20 and store each own status from the page itself.
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

#### Related-account rationale

Related accounts stay in a private status pool because their timelines contain announcements for many people. Keyword and operator promotion put only relevant statuses on the board, while quoted and reposted statuses can supply context and translation without becoming public cards. Separate cursors and a separate timer keep this filtering independent from Kano's own accounts.

#### Related accounts

A related account (such as the agency, `@Mil_Pro_`) is a second account
role beside the own accounts of `X_HANDLES`, configured on the console's
Sources page beside its timer, with its keyword rules in the app setting
`x_related_accounts` (see [Configuration](../operations/configuration.md#related-x-accounts)
and [Related accounts](../architecture/sources.md#related-accounts)).
A handle is in one role at a time, and the setting starts empty.

The related accounts have their own timer and step, `x_related`
([Source schedules](workflows.md#source-schedules), every 180 minutes by
default), apart from the own accounts' `x`. Each run reads each related
account's own timeline into the X status pool (`x_statuses`, see
[Quoted posts](#quoted-posts)):

- It reads the FxTwitter timeline alone, at most the schedule's incremental
  `max` (1–200, default 50) new statuses per account, else the X discovery
  limit, and on a first read back the run's lookback days, else the X
  first-run age. It has no `count` setting, no profile-page fallback and no
  X API method, and it makes no detail request, since a page carries each
  status; a status without usable content is left out. Its reposts and other
  authors' statuses are left out too.
- The incremental cursor and gap model follows the own-account rules, with its
  own `sync_state` row of source `x-related` (the lower-case handle as account): the cursor, the timeline
  gap and the known statuses come from the pool's statuses that the
  account's timeline listed (`listed_at`), never from `posts`, so a role
  change never mixes cursors.
- A failed read keeps the cursor and the pool and records its code in the
  row's `lastError`. While FxTwitter is down, related accounts are not read;
  a timeline gap covers the stretch.

The step result is `{source: "x_related", accounts, listed, promoted,
demoted}`. Without a related account the step records `{skipped: true,
reason: "no_accounts"}` and makes no request. An account whose read failed,
and a read's notes such as a timeline gap, leave the step `partial` with
`errors` as `@<handle>: <code>`; when every account failed the step fails
with `x_related_failed`. An operator's
[manual fetch](workflows.md#manual-fetches-and-historical-backfill) of one
related account runs the same read for that account alone and fails once
with its code. The own accounts' read and the related accounts'
read never decide each other's outcome, and quotes and reposts of the own
accounts write the pool in the `x` step whether or not `x_related` runs.

Every pooled status stores the reasons that apply to it:

| Reason     | When                                                                    | Effect              |
| ---------- | ----------------------------------------------------------------------- | ------------------- |
| `keyword`  | a related account's status whose text contains one of its keyword rules | promoted            |
| `manual`   | an operator promoted a related account's status                         | promoted            |
| `quoted`   | an own account quoted it, whatever its author                           | translated, no card |
| `reposted` | an own account reposted it, and its author is a related account         | translated, no card |
| `none`     | none of the above                                                       | stored only         |

Keyword rules match anywhere in the text, ignoring letter case. Reasons are
evaluated when a related timeline lists a status, when an own account
reposts it, when an operator decides on it, and for every pooled status when
the related accounts or their rules are saved.

**Promotion** writes the status from its pooled `raw_json` to `posts` under
its author's handle, with `posts.promoted_at` set. It registers the status's
own images and quoted images as any post does, but never a schedule-image
candidate, and gives the status it quotes no `quoted` reason. From then on
the post takes the paths every post takes: the wall, the archive,
translation, extraction and media. **Demotion** sets `posts.hidden_at` and
keeps the row, its media and its events, whose automatic ones then leave the
public pages (see [Events of hidden posts](data-model.md#events-of-hidden-posts)).
An automatic demotion records `x_statuses.demoted_at`, so an automatic
promotion shows again only a post the rules hid; an operator's hide in Posts
stays. Showing a promoted post in Posts records a manual promote, and hiding
it withdraws that promote. An operator's promote or demote (`manual_decision`, with its reason
and time) is a durable lock that the rules never override; a manual promote
shows a hidden promoted post, and returning a status to the rules lets them
decide again. Only a related account's status can be promoted by hand, and a
pooled status that is an own post is never promoted or demoted.

#### X API in the chain

The X schedule's [method chain](workflows.md#method-chains) can enable the
official API (`api`, off by default), which needs `X_API_BEARER_TOKEN`
([method chains](../architecture/workflows.md#method-chains)). It reads
`https://api.x.com/2/users/<id>/tweets` without reposts and replies, newest
first, with the account's cursor as `since_id`, up to the run's cap of new
posts in pages of 5 to 100 and at most 10 pages; before an account has a
cursor it reads from the first-run age limit ago instead. The numeric user ID
comes from `/2/users/by/username/<handle>` once per process. Requests share
the manual X API method's client (see
[X API method](workflows.md#x-api-method)): the token only in the
`Authorization` header, the request timeout setting, the 4 MiB limit,
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
RSS feed, configured yt-dlp when its method is enabled, and the uploads tab. It takes the newest listed uploads and stores the ones
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
premiere: its event is a `release` and its video's kind is `premiere`.
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
without updating video/event details or claiming a successful check. A page
that shows YouTube's bot check instead records `youtube_watch_login_required`:
a `LOGIN_REQUIRED` player whose reason names the check ("confirm you're not a
bot") or gives none that is recognized, or the check's page without a player.
YouTube puts a host behind this check for every watch page at once, so the
run requests no further watch page, and the code appears once. A
`LOGIN_REQUIRED` reason that concerns one video is not the check, and the run
goes on: a private video reads as `youtube_watch_private` and an age gate
records `youtube_watch_details_incomplete`. Reasons are recognized in English and
Japanese, in the player's reason, messages and error screen. The queued IDs it did not reach
keep their places ahead of the rotated one, and an unclassified Data API
answer stands in for each of them as for a failed page. A members-only page
answers `UNPLAYABLE`: one that still names its start, as a stream archive
does, passes as members-only, and one that does not fails as
`youtube_watch_members`. A page that tells who can watch the video (see
[Video availability](#video-availability)) is no error and does not stop the
run's page requests. Incremental
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
`rss`, `ytdlp`, `videos_page`, or empty when nothing answered, `tried` lists every
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
  later page keeps the earlier ones. When the first page fails, the feed and
  then the uploads tab are tried as above.
- `videos.list` calls (`snippet`, `contentDetails`, `liveStreamingDetails`,
  `localizations`) cover the detail queue in order, 50 IDs per call and at
  most five calls (`ytsource.MaxAPIDetailCalls`), so a 200-video `count`
  window fits in one run. Any room left in the last call goes to the newest
  listed uploads of the run, stored or new. A failed call records its
  `youtube_api_` code and ends the lookup, since a refused key, quota or rate
  limit holds for the later calls too; the IDs it and later calls would have
  covered fall back to watch pages. A described queued video counts as a
  successful detail check without a watch-page request: its title, length
  (ISO 8601 `duration`), and for a
  broadcast the actual start (or the reservation), the actual end and
  `liveBroadcastContent` as `upcoming`/`live`/`ended`. The watch-page budget
  (the video pages per run setting) then goes to the queued IDs the API left out.
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
  each, counted in `apiRequests`, at most once per run. YouTube lists
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
  `is_premiere` verdict. Otherwise, as for a row without a
  stored verdict, an untitled video or an upcoming one without a start, its
  watch page decides.
- When that watch page fails for an ended broadcast the API gave a start
  (`ytsource.Video.Unclassified`), the API answer is used anyway rather than
  leaving the video unchecked, since some hosts never get a readable page. It
  writes the title, thumbnail, actual start and end, length and `ended`
  status, sets `live_checked_at` and answers a refresh request, while a
  stored `stream` or `premiere` kind (otherwise the kind is `unknown`), the
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

A run that finds a stored video on its first playlist page spends three
quota units (of the default 10,000 a day), the members-only playlist page
included (see [Video availability](#video-availability)), while its detail
queue holds at most 50 IDs; each further 50 queued IDs add one, up to five `videos.list` calls,
each further playlist page adds one, and the per-format playlists two more
when a queued ended broadcast needs them. The key is sent only in the `X-Goog-Api-Key` header to
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
not: no listing, `videos.list`, per-format, Shorts or members-only playlist, and the
[live-status probe](#live-status-probe) reads watch pages only. Watch pages
and the members-only feed take over as they do without a key.

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

#### Video availability

Every check that can tell records who can watch a YouTube video in
`videos.availability`:

- A `videos.list` answer (the request names the `status` part, at no extra
  quota) gives `public` or `unlisted` from `status.privacyStatus`. The API
  cannot tell a members-only video from a public one, so it keeps a stored
  `members`.
- Every recent step run ends with the channel's members-only playlist
  (`UUMO` and the channel ID after `UC`): every stored video it names is
  `members`, whatever its age, and a video checked in the same run carries
  the verdict, so its entry is a `member` entry from that run on. The
  playlist is undocumented, and absence from it proves nothing, so it records
  no other value. With a key the run reads its newest page (50 entries), one
  `playlistItems.list` call and one quota unit counted in `apiRequests`, at
  the same Data API destination under the same rules as the uploads
  playlist, unless an earlier Data API call of the run failed, since a
  refused key, quota or rate limit holds for it too. Without a key, or when
  that call is skipped or fails, the playlist's public feed
  (`https://www.youtube.com/feeds/videos.xml?playlist_id=UUMO…`, its newest
  15 entries) stands in, a fixed helper read whether or not the chain enables
  `rss`, under the channel feed's codes, retries and limits. A run whose
  channel feed read failed skips it, since both are the same feed endpoint.
  Each failure, such as the 404 of a channel without members-only uploads,
  records its code and costs only the verdicts. The step result reports
  `members`, how many videos the playlist named, and `membersFrom`, the read
  that answered (`api` or `rss`, empty when none did). An operator's YouTube
  [manual fetch](workflows.md#manual-fetches-and-historical-backfill) ends
  with the same read and reports it the same way: through the Data API with
  the `api` method unless a Data API call of the fetch failed, otherwise the
  feed.
- A watch page that passes the completeness checks, a playable page or a
  `LIVE_STREAM_OFFLINE` waiting room, is `public`, or `unlisted` when its
  microformat says so; an `UNPLAYABLE` or waiting-room page that names a
  members-only reason and still names its start is `members`.
- A failed page tells only when YouTube answered it: `youtube_watch_private`
  is `private` and `youtube_watch_members` is `members`. For a video that a
  `videos.list` call which succeeded left out, as the API does private and
  removed videos, a sign-in page that does not name the bot check is
  `private` and any other page without the details is `removed`. A page that
  was not answered (a timeout, a 5xx) and the bot check tell nothing.

A failed page that told, and the members-only playlist, write only the
availability of the stored video, and a failed page that told counts as no
run error. A check that cannot tell keeps the stored value, and
nothing writes `NULL` back. A `members` video is a `member` entry whatever its
title says. Bilibili uploads keep `NULL`. An operator's correction
(`manual_availability`, see
[Video corrections](data-model.md#video-corrections-and-history)) lies over
the recorded value on the board, which checks keep writing beneath it. The
board treats `NULL` as watchable; the stage's latest video skips `private` and
`removed`, and their cards open no YouTube page (see
[Public API](public-api.md)).

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

An operator also adds or re-reads one video by ID or link from `/admin` (see
[Admin API](admin-api.md#youtube-videos)). That read runs at once, outside
the queue: the Data API describes the video under the recent step's rules
when a key is configured, and the watch page does without one or when the
answer cannot classify an ended broadcast. Only a video whose channel
(`channelId` in the API snippet or the page's video details) is
`YOUTUBE_CHANNEL_ID` is accepted (`youtube_other_channel` otherwise). It is
written like a manual fetch's listed and checked video, so manual locks and
deletion tombstones hold. Neither the incremental cursor nor the detail
queue moves.

Every detail check stores the player's `lengthSeconds` as
`duration_seconds` once a video is neither upcoming nor live. When a check
reports a new title or thumbnail URL, the replaced platform state is kept as a
`video_revisions` row (`origin = sync`); a YouTube API history page does the
same for a new title only, because the API names thumbnails differently from
the watch page. After any successful page read, the media step revalidates the
video's thumbnail file whatever its age, and new bytes at the same URL keep
the replaced file as a version (`origin = media`). Operator corrections of the
title, kind, tag or thumbnail are separate columns that synchronization never
writes; see [Video corrections](data-model.md#video-corrections-and-history).
The kind is the video's form, decided from these platform facts alone, and
what a video is about is its tag; see
[Video form and tags](data-model.md#video-form-and-tags).

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
reported as over, events whose `ends_at` has passed, manually confirmed
cancellations and [stopped videos](#stopped-videos) are left out. Each active video is due by its phase: once before
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
written unclassified, as in the recent step. A page that asks to sign in
(`youtube_watch_login_required`) of a video the answer did not leave out ends
the run's page requests the same way: the remaining videos are left as if
their pages failed. `apiRequests` in the run
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

##### Stopped videos

A video the probe can learn nothing more about is stopped rather than asked
again until its 24 hours run out. It is unavailable when its failed page
tells it `private` or `removed` (see [Video availability](#video-availability)),
which also records that availability. A members-only page records `members`
without stopping the video, and a page that was not answered at all (a
timeout, a 5xx) or the bot check stops no video. An unavailable
video is not a run error: the run result counts it in `unavailable`, and a run
whose only due videos were unavailable succeeds. A later regular or probe
check that records the video `public`, `unlisted` or `members` lifts an
`unavailable` stop, so a video made watchable again is asked about again. An
operator can also stop any active video, or let a stopped one be asked again,
from `/admin`; only the operator lifts an `operator` stop.

Stops are kept in the same `youtube-live` `sync_state` row as the requests,
with when and why (`unavailable` or `operator`), and are forgotten with them
after two days. A stop leaves the video's stored broadcast state alone: a
`live` or `upcoming` result simply ages out of the board's freshness window,
and the video shows its start-time fallback. Letting a video be asked again
makes it a target again while its start is within the probe's window.

The public dashboard projects `liveStatus` (`live`, `upcoming`, `ended` or
null) on videos and on the events linked to them. `ended` is final; `live` and
`upcoming` count only while the check is younger than two probe intervals, so
stale results fall back to the start-time grace. A fresh `live` video stays
listed beyond that grace. Run timing is described in
[Workflows](workflows.md#live-status-probe).

#### WebSub Push

Polling remains the baseline. The operator turns push on in `/admin`
(Sources → YouTube → YouTube push) by entering the deployment's public HTTPS
address; the console adds `/api/websub/youtube` and saves the result as the
callback URL. The standalone process then subscribes the channel's
feed topic at Google's hub (`https://pubsubhubbub.appspot.com/subscribe`)
within seconds, without a restart, and again at every startup.
`backend/internal/source/youtube/websub` owns the protocol and its admin
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
  channel's topic, and only for the subscription or the withdrawal this
  process requested within the last hour, while enabled for a subscription.
  Any other verification answers like an unknown path, so a stranger's
  subscribe request with this callback cannot replace the signing secret. The
  confirmed verification updates the lease, so renewal happens at half the
  granted lease but no more than hourly. A verification that arrives after a
  restart for the previous process's request is refused too, and the missing
  verification below requests the subscription again. When no verification arrives within an hour, the
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

The `bilibili` step reads the uploads and the `bilibili_dynamics` step the
dynamics (动态) of `BILIBILI_UID` (Kano's account by default), each on its
own [timer](workflows.md#source-schedules); the [live-status
probe](workflows.md#live-status-probe) checks its live room. Every request
goes to one of four fixed hosts: `api.bilibili.com`, `www.bilibili.com`
(the opus page of one dynamic only), `api.live.bilibili.com` and
`app.bilibili.com` (the uploads' last fallback, below). The upload list
uses only endpoints that need no request signature; its fallbacks are
signed (the dynamics listing with WBI, the official client's listing with
its app key):

- `/x/frontend/finger/spi` returns the anonymous device identity
  (`buvid3`/`buvid4`) Bilibili's own pages send; its `b_nut` is when it was
  issued. It is reused for seven days and kept in `sync_state` (source
  `bilibili`).
- `POST /bapis/bilibili.api.ticket.v1.Ticket/GenWebTicket` returns the
  `bili_ticket` cookie (a JWT valid about three days), signed as Bilibili's
  web script signs it (`key_id` `ec02`, `hexsign` an HMAC-SHA256 of
  `ts<unix seconds>`, `context[ts]`, an empty `csrf`). It is kept with the
  device identity and renewed six hours before it expires, and its answer
  also names the current WBI keys. A failed ticket request is not a
  failure: the read goes on without it.
- `/x/series/recArchivesByKeywords` lists the account's uploads newest
  first, 20 a page, with title, cover, publication time, length and
  description; the timed read takes the first page and a
  [manual fetch](workflows.md#bilibili-methods) pages on. Entries of
  another uploader are skipped.
- `/x/web-interface/view` reads one upload for an operator, and the cover
  address of an upload on the Works page.

Every request reads like Bilibili's own pages: one desktop browser user
agent (`BrowserUserAgent` in `backend/internal/source/bilibili/client.go`,
updated deliberately together with this page), `Accept-Language:
zh-CN,zh;q=0.9`, the page it comes from as `Referer` (the space for the
upload list and the listing, the video page for one upload, the home page
for the device identity, ticket and `nav`, `https://live.bilibili.com/` for
the live host) and, on the API hosts, that page's `Origin`. The device
cookies (`buvid3`, `buvid4`, `b_nut`, `bili_ticket` and
`bili_ticket_expires`) go to `api.bilibili.com` and the opus page, never to
the live host; the device-identity request sends none.

Bilibili still puts an address behind a captcha for hours at times. The
client never retries and names how Bilibili answered:
`bilibili_risk_control_http` (HTTP 412), `bilibili_risk_control_page` (an
HTML captcha page, or an opus page without its state),
`bilibili_risk_control_352` and `bilibili_risk_control_412` (the JSON codes)
and `bilibili_risk_control_voucher` (a `v_voucher` instead of the listing);
`-799`/`-509`/429 are `bilibili_rate_limited`. Each starts the same backoff:
the adapter waits 6 hours, doubled for each consecutive block up to a day,
and neither step makes an `api.bilibili.com` or `www.bilibili.com` request
until then (the upload read may still ask `app.bilibili.com`, below), and
only an `api.bilibili.com` success clears it; independently, upload reads are at least
`BILIBILI_MIN_INTERVAL_MINUTES` (180 by default) apart, which is also the
shortest interval of either Bilibili timer. A skipped read records
`{ skipped: true, reason: "interval" | "blocked" }`; a failed one records its
code as a failed step without a retry, keeps every stored upload and post,
and lets the rest of the run continue.

`BILIBILI_COOKIE` (the signed-in browser's cookies) or `BILIBILI_SESSDATA`
(its `SESSDATA` alone) optionally adds an operator's login to the
`api.bilibili.com` requests (never to the device-identity or ticket request,
the opus page or the live host); see
[Configuration](../operations/configuration.md) for the cookies kept. A
signed-in request sends `SESSDATA` with the browser's `bili_jct`,
`DedeUserID`, `DedeUserID__ckMd5` and `sid`, and the browser's device
cookies (`buvid3`, `buvid4`, `b_nut`, `_uuid`, `buvid_fp`) in place of the
anonymous identity where the login carries them, so the account and its
device match. Signed-in reads meet the captcha far less often, and the
dynamics listing needs one. The login is verified at most once
a day through `nav`'s `isLogin`, when the listing runs; while the latest check
found it expired, no request sends it and the listing waits for the next
check (`{ skipped: true, reason: "login_expired" }` on the
`bilibili_dynamics` step, error
`bilibili_login_expired`). The console reports `signed_in`, `expired`,
`unchecked` or `not_configured`. The login cookies stay in memory: they are
not part of the persisted state, not logged and not returned.

When the upload list fails (a captcha included), or the device-identity
request before it does, the same read falls back at once, in order, to:

1. the first page of the signed-in [dynamics listing](#dynamics), when a
   login is configured that the latest check did not find expired and the
   device identity was read, storing
   the uploads its video dynamics show: one listing request, plus `nav` when
   the login check or the WBI keys are due. It leaves the dynamics read's own
   clock and the dynamics themselves to the `bilibili_dynamics` step;
2. the official client's space listing,
   `app.bilibili.com/x/v2/space/archive/cursor?vmid=<mid>&ps=20&order=pubdate`,
   when an app key pair is configured (`BILIBILI_APP_KEY` and
   `BILIBILI_APP_SECRET`, see [Configuration](../operations/configuration.md);
   without one it is not tried): one request without cookies or Referer,
   signed as Bilibili's apps sign (`appkey` and `ts` added, `sign` the MD5 of
   the key-sorted query followed by the app secret). It answers a different
   service from the web API that shows the captcha. Its ordinary
   uploads (`goto` `av`) with a BV ID, title and publication time (`ctime`)
   are stored with their cover and length; it carries no description, so a
   stored one is kept. The listing names no uploader, so a co-created upload
   (`is_cooperation`), which may be another account's, is left to the other
   listings.

The first success leaves the step `partial` with `fallback` (`dynamics` or
`app`), the list's code as `listError` and any earlier fallback's failure in
`fallbackErrors`. A dynamics success is an `api.bilibili.com` answer and
clears the backoff the list's failure started; an app success keeps it.
While a backoff lasts, an upload read due by its interval asks only the
official client's listing when a key pair is configured (`listError`
`bilibili_blocked`) and is skipped as `blocked` otherwise; that read never
extends the backoff. When every listing fails, the step keeps the list's
code and adds each fallback's as `<fallback>: <code>` to its errors; the
backoff starts once, at the first captcha among them.

The dynamics and the official client's listing date an upload by their own
clocks and the latter omits its `aid`, so an upload either of them reads is
partial: it keeps a stored upload's `published_at` and `aid` (the upload
list still corrects them), and a missing `aid` never replaces a known one.

Signing with an official client's key makes those requests read as that
client rather than as this service. That is a trade-off each deployment
accepts by configuring the pair, so the repository carries none: the pair
grants no account access, and when Bilibili changes it the fallback fails
with a controlled code until the operator configures the new one.

An operator's single read (one upload, one dynamic) answers
`bilibili_blocked` during a backoff without a request, unless the operator
forces it once from the console (`force`, see
[Admin API](admin-api.md#bilibili-uploads)). A forced read that succeeds
lifts the backoff; one that fails extends it as usual. Timer reads and
[manual fetches](workflows.md#bilibili-methods) are never forced.

Uploads are `videos` rows with `source = 'bilibili'` and the BV ID as their
ID, linked to `https://www.bilibili.com/video/<BV>`. The list and view
endpoints carry no live-replay flag, so a new upload is stored as an
unsettled `upload` and its form (`upload` or `stream`) is settled later by
rule, model or operator (see [Bilibili form](data-model.md#bilibili-form));
Bilibili's categories say nothing about the content, which the
[video tag](data-model.md#video-form-and-tags) carries. The description and
`aid` stay in `raw_json`. Every list read replaces the other platform fields
and keeps a replaced title or cover in `video_revisions`, as YouTube does;
the form and operator corrections survive. Covers and dynamic pictures come
only from `*.hdslb.com`, upgraded to HTTPS, and are cached under
`data/bilibili/` by the media step.

An upload whose form is settled is a public video like a YouTube one, on the
board, the video archive, MCP and the Atom feed, until it is merged. The
`reconcile` step merges an upload into the YouTube video of the same work
(a live replay into the stream archive of the same broadcast), which then
lists it in `alternates` and shows its title as the video's Chinese platform
translation, so one work is never listed twice; operators confirm, add or
undo merges in `/admin` (see
[Video merging](data-model.md#video-merging)). Bilibili answers no
availability for a listed upload; an operator marks a deleted or hidden one
`removed` or `private` through `manual_availability`. Operators can
add an upload by BV ID or `bilibili.com` video link and read one again at
once from `/admin` (see [Admin API](admin-api.md#bilibili-uploads)); those
reads respect the same backoff and accept only uploads of the configured
account. They run outside the workflow queue, so the state write keeps a
block that a concurrent read stored meanwhile (one still active and later
than the writer's), with its failure count and code: a captcha backoff is
never lost. A read that changes a merged upload's title rewrites the
translation in the same transaction.

#### Bilibili login boundary

The dynamics listing requires a signed-in cookie because anonymous requests depend on browser fingerprint parameters and encounter captcha responses. The server therefore skips the listing without a login and keeps the cookie in memory for the Bilibili API only. An operator can still add one dynamic through its public opus page, which does not require the listing credential.

#### Dynamics

The dynamics listing,
`/x/polymer/web-dynamic/v1/feed/space?host_mid=<mid>`, answers reliably only
to a signed-in caller, so it runs only with a login (`BILIBILI_COOKIE` or
`BILIBILI_SESSDATA`,
[Bilibili dynamics](../architecture/sources.md#dynamics)).
Without the cookie the `bilibili_dynamics` step records `{ skipped: true,
reason: "login_required" }` and makes no anonymous attempt. The listing is signed
with Bilibili's WBI scheme (`wts` and `w_rid` from the rotating keys that
the ticket or `/x/web-interface/nav` names); the keys are cached for 12
hours and dropped after a `v_voucher` or `-352` answer, which may mean they
rotated. A signed-out `nav` answer, or `-101` from the listing, is
`bilibili_login_expired`. The listing shares the upload read's captcha
backoff and device identity, so a block either one met holds both back, and
runs on its own clock at least `BILIBILI_MIN_INTERVAL_MINUTES` apart. The
single-flight queue never runs the two steps at once, so their writes of the
shared state never race. One timed read requests the first page only: the
listing itself, plus `nav` for the daily login check or stale keys, and the
device identity and ticket when they are due. An operator's
[manual fetch](workflows.md#bilibili-methods) follows the offset each page
answers to older pages.

A listing keeps the account's own `DYNAMIC_TYPE_WORD` and `DYNAMIC_TYPE_DRAW`
items (the opus and picture cards), `DYNAMIC_TYPE_ARTICLE` items only in
their `MAJOR_TYPE_OPUS` form, whose text comes from the opus summary, and
forwards (`DYNAMIC_TYPE_FORWARD`, see [Forwards](#forwards)); a live-start
notice and a collection update are not posts. A supporters-only dynamic
(`basic.is_only_fans`, 充电专属) is never a post, even when a supporter's
login carries it in full. An upload's dynamic
(`DYNAMIC_TYPE_AV` with a `MAJOR_TYPE_ARCHIVE` card) of the account is not a
post either: the read stores it as the upload it publishes (BV ID, title,
cover, description, length from `duration_text`, published at the dynamic's
time), written as the upload list writes one, so a new upload reaches the
store even while the list meets the captcha. The upload read's newest
cursor stays its own. The stored text joins the nodes' text (words, topics, mentions,
links) and leaves out emoji nodes (`RICH_TEXT_NODE_TYPE_EMOJI`); an opus
title leads it, and a picture post's `desc` nodes give it beside its
pictures. The nodes, emoji included, stay in `raw_json`. An
emoji-only dynamic therefore has empty text and is translated as `none`
without a model call. Up to nine pictures are kept as the post's images.

Dynamics are `posts` rows with `source = 'bilibili'`, the ID
`bilibili-<id_str>` (dynamic and X status IDs are both numbers of about 19
digits and share `posts.id`), `account_handle` the mid, `url`
`https://www.bilibili.com/opus/<id_str>` and `published_at` the publication
time in UTC. They have no engagement counts; a forward has a quote. A read
replaces a dynamic's text, time, pictures and quote, keeps an operator's
hide, and advances the dashboard revision only when the text, time, link,
pictures, quote, quoted pictures or hide changed. Its pictures are
`post-image` links, so they feed
[schedule extraction](#schedule-extraction-pipeline) once cached; a
schedule board on a Bilibili picture is not registered as a
[schedule image](#schedule-images).

An operator adds or re-reads one dynamic by its `bilibili.com/opus/<id>` or
`t.bilibili.com/<id>` link from `/admin` (see
[Admin API](admin-api.md#bilibili-uploads)). That read takes the dynamic from
the server-rendered state (`window.__INITIAL_STATE__`) of its
`www.bilibili.com/opus/<id>` page, which needs no login; it respects the
captcha backoff and accepts only dynamics of the configured account whose
comment area is a post's (`basic.comment_type` 11 pictures, 12 article, 17
text) and that the page shows without a fallback; any other is
`bilibili_unsupported_dynamic`. The read takes the dynamic as its page
shows it, with no quote: a forward and its quote come from the listing, and
a single read never rewrites a stored forward (`raw_json.type`
`DYNAMIC_TYPE_FORWARD`), whose row stays as the listing stored it.

#### Forwards

A forward is the account's own post that quotes the dynamic it forwards, the
way an X post [quotes a status](#quoted-posts): it has its own dynamic ID,
publication time and wording, and the listing carries the forwarded
dynamic in full in its `orig` object, so a forward needs no request of its
own. Its text is its `desc` nodes without emoji, and it has no pictures of
its own. The listing keeps every forward of the account. The store leaves
out a new forward that has no wording of its own and an unavailable quote,
which would be an empty card; a stored forward whose dynamic becomes
unavailable is rewritten with the unavailable quote, loses its
`quote-image` links and advances the dashboard revision, with or without
wording of its own.

`backend/internal/source/bilibili/dynamic.go` reads `orig` into the same
sanitized projection an X quote has, stored in `posts.quote_json`:

- `id` is `bilibili-<orig id_str>`, the post ID that dynamic has (or would
  have) as a stored post, so a forward of an own dynamic resolves to that
  post like an X self-quote;
- `authorHandle` is the author's mid and `authorName` the display name,
  HTML-unescaped, trimmed and at most 50 characters;
- `text` is built like a listed dynamic's (opus title and summary, or `desc`
  nodes, emoji left out); a forwarded forward shows its own wording only and
  is not followed further;
- `publishedAt` is its publication time in UTC, and `url` its
  `https://www.bilibili.com/opus/<id>` page, or for a forwarded upload
  (`DYNAMIC_TYPE_AV`) the `https://www.bilibili.com/video/<BV ID>` page, with
  the video title as text and its cover as the one image.

Its pictures (HTTPS on `*.hdslb.com`, at most nine) are `quote-image` links
of the forward and never schedule candidates; `quote_json` holds no image
address. A deleted dynamic (`DYNAMIC_TYPE_NONE`, `MAJOR_TYPE_NONE`), a
missing `orig`, a dynamic of any other kind (a live room, a collection,
goods, a vote, an article in its old form), a supporters-only dynamic or one
with neither wording nor pictures is unavailable, with only its ID and its opus page as URL (an
upload too); when even its ID is unknown, `id` is empty and `url` is the
forward's own opus page. An `orig` of an unexpected shape is unavailable
as well, keeping its ID when `id_str` still reads, and never fails the page.
An unavailable quote leaves the dynamic's pool row as it was.

Every available forwarded dynamic also enters the Bilibili status pool,
`bilibili_statuses`, in the forward's transaction, whatever its author. The
pool row and `quote_json` come from the same projection. The row keeps the
post ID, the author's mid and name, the wording, publication time, URL, a
sanitized `raw_json` with the pictures' addresses, and `forwarded_at`, the
publication time of the newest own forward of it. No public reader reads or
joins the pool. The [translate step](#translation) translates a forwarded
dynamic that is not a stored post by its post ID (`owner_type`
`bilibili_status`), so it is translated once however often it is forwarded,
and [schedule extraction](#schedule-extraction-pipeline) reads its wording
as context of the forward. A forward's events are Bilibili-sourced events
like any dynamic's.

#### Live room

The account's live room is not configured: each live check learns it.
The [live-status probe](workflows.md#live-status-probe) asks
`api.live.bilibili.com/room/v1/Room/get_status_info_by_uids?uids[]=<mid>`,
which needs no cookie, about the account's room in one request, and keeps
the answer's `room_id` with the rest of the state. Only `live_status` 1 is a
broadcast (0 is off air and 2 a rotation of recordings, which is not live);
`live_time` is its actual start. A live answer without `live_time` is live
with an unknown start: the broadcast already seen, or one that started by
that check; it never ends a broadcast. The live host has its own captcha backoff,
with the same 6-hour doubling. The state (room, title, status, start,
checked and attempted times, backoff) is kept in `sync_state` (source
`bilibili-live`, the mid as account), together with the start of the last
broadcast seen live, the last check that saw it live and the first later
check that saw the room off air, and the start and observed end of the
broadcast before it when a check saw that one end.

Entries are tied to the room by their [platforms](data-model.md#event-platforms),
never by a link: the room's link is the same for every broadcast, while
readers treat an entry's link as the identity of one broadcast. Public reads
add the room as `liveUrl` and as the bilibili link to every `stream` or
`member` entry shown on Bilibili once a check learned the room, so entries
extracted before the first check get it too and a changed room needs no
rewrite. A broadcast starting at L is matched against the active, timed
stream and members-only entries that are not merged away or a confirmed
cancellation, all of them rather than one read's, so every page agrees:

1. the entry shown on Bilibili (its platforms, with those its merged
   entries lend it, include `bilibili`) starting at S when
   S−2h ≤ L ≤ S+3h and not ended before L (its `ends_at`), the nearest of
   several;
2. otherwise the YouTube event whose video a check saw `live` or `ended`,
   whose broadcast started within 30 minutes of L and had not ended before
   L, the nearest of several: the broadcast is its simulcast, and its shown
   platforms gain an observed `bilibili`. A reservation that has not started,
   or that no check has seen, never takes it.

One broadcast matches one entry at most. The entry the previous broadcast
matched takes no broadcast that starts more than 30 minutes after a check
saw that one end: it is another broadcast, not a restart. That entry is `live` while the
check is younger than two Bilibili check intervals, and `ended` once a
later check saw the room off air, unless its YouTube broadcast already
reports a state; a live room always makes it `live`. The last check that saw the
broadcast live is its approximate end, kept in the state and shown in the
console; it is never written as the entry's `ends_at`. Bilibili has no
reservation state, so no entry is `upcoming` from the room; an entry's own
start drives that. A live broadcast that matches no stored entry creates no
entry: the dashboard reports it as `liveNow` (see
[Public API](public-api.md#public-snapshot-contract)). Raw answers, counters
and errors stay in the console.

### Official shop

The `shop` step reads the sale periods of goods Kano takes part in from the
official shop, `https://shop.milpr.com`, a Shopify storefront, so a sale
reaches the board even when no post announces it. It needs no model: the
storefront's public product JSON states everything a period needs. The step
has its own [timer](workflows.md#source-schedules) and runs only in
server-side workflows; page loads never read the shop.

`backend/internal/source/shop` builds three fixed URLs on that one host and
nothing else; no address comes from data:

- `/collections/new-arrivals/products.json?limit=50`, the newest 50
  products, read by every timed run, Run now and full sync;
- `/products.json?limit=250&page=<n>`, the whole catalog, read only by the
  operator's [read-all fetch](workflows.md#manual-fetches-and-historical-backfill):
  at most six pages, three seconds apart, until a page lists fewer than 250
  products, counting those left out as unusable;
- `/collections.json?limit=250`, the collection list, read at most once per
  run, three seconds after the listing, and only when a listed product is in
  no stored sale of its period.

Every request names the client honestly as `kano-navi-go/shop`. The shop
answers clients that pose as a browser with `429 local_rate_limited`, so the
client never sends a browser's user agent. A 429 ends the run without a
retry: its `Retry-After` (seconds or a date; a minute when missing, at most
a day) is stored, and no read, timed or manual, requests the shop before it
passes (`{ skipped: true, reason: "rate_limited" }`). The other failures are
`shop_timeout`, `shop_unavailable`, `shop_redirect_rejected`,
`shop_response_oversized`, `shop_http_<status>` and `shop_invalid_json`. Any
failed request fails the read: it records its code and changes no entry.

A product counts only when it carries the tag `鹿乃まほろ`, a constant of the
code, since the board is Kano's, and when its description states a dated
period: a label (受注期間, 販売期間, 受付期間 or 抽選受付期間), a dated
opening, and on the same line `〜`, `～` or `~` and a dated close, such as
`＜受注期間＞ 2026年10月24日 〜 2026年11月24日`. Full-width digits read as
digits, every Unicode space (such as the no-break space of `&nbsp;`) reads
as a plain space, a weekday or time may follow the opening, and a close
without a year falls on or after the opening: in the opening's year, or the
next one when its month and day come earlier, and only then is the date
checked, so `2027年12月1日 〜 2月29日` closes on 2028-02-29. A permanent item (`販売期間 2026年10月1日～`
with no close), a product without a period and a period with impossible
dates are skipped and counted by reason (`open_ended`, `no_period`,
`invalid_period`, `untagged`). Stock (`variants[].available`) is not the
period: a sold-out product still has its sale. Prices, stock and images are
never decoded.

The products of one period form one group, with one entry, not one entry
per product. The shop writes some titles with decomposed kana (`ホ` followed
by the combining voiced sound mark U+3099 for `ボ`) and others with the
precomposed kana, so every product and collection title is stored with each
kana and a following U+3099 or U+309A composed, as Unicode NFC composes
them; nothing a reader sees changes. A group's key is its title without
whitespace:

1. A product that a stored group of the same period holds stays in it, so a
   group keeps its identity however the newest-50 window cuts it.
2. A new product joins the collection whose title, ignoring whitespace, its
   own title starts with, the longest such of at least four characters
   (`鹿乃まほろ 加入記念グッズ` for `鹿乃まほろ加入記念グッズ アクリルスタンド`).
   The group takes the collection's title and links
   `https://shop.milpr.com/collections/<handle>`.
3. Any other product joins the group of its period whose key its title
   starts with, the longest one.
4. Any other starts a group titled by its title without the last word,
   linking its first product's `https://shop.milpr.com/products/<handle>`.
   Shorter titles are grouped first, so `… 2026 ランダム缶バッジ 全8種`
   joins `… 2026`.

Each group is an automatic `sale` event (see
[Sale periods](data-model.md#sale-periods)) with `source` `shop`,
`source_item_id` the group key and `source_key` the period
(`<opens>/<closes>`), and an `event_sources` row of the same identity:

| Field                     | Value                                                                                                                                      |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `title`                   | the group title as the shop writes it                                                                                                      |
| `starts_on` / `starts_at` | the opening date; `starts_at` is the earliest `published_at` of the group's products that falls on that Japan date, to the minute, or null |
| `ends_on` / `ends_at`     | the close date; `ends_at` stays null, since the JSON gives no close time                                                                   |
| `time_precision`          | `exact` with `starts_at`, otherwise `unknown`                                                                                              |
| `url`                     | the collection page, or the first product's page                                                                                           |
| `detail`                  | null                                                                                                                                       |
| `platform` / `platforms`  | `other` / `["other"]`, stated (`platforms_inferred` 0)                                                                                     |

`raw_json`, on the event and its source row, holds only the group's title,
link and collection handle, the matched period line, and each product's ID,
handle, title and publication time (at most 100 products). The source row
follows every read, so a locked or deleted entry still keeps its products and
its identity. A read rewrites an entry only when a shown field changed, and
then advances the dashboard revision. Nothing is retired: a product that
leaves the listing, or the shop, keeps its entry, and a changed period is a
new group whose old entry stays. Manual edits, locks and tombstones win as
for every automatic entry; an operator who adds the close time saves a
locked entry the shop never rewrites. [Rule C](data-model.md#duplicate-merging)
merges a post's entry of the same sale into the shop's, which states an
opening time and a close date. The read's bookkeeping is the `sync_state`
row of source `shop` and account `shop.milpr.com`; its last success is the
shop's [freshness](public-api.md#public-snapshot-contract).

### Works Cover Lookups

The media step finds covers for the Works page's uploads that no stored
video covers (see
[Works upload covers](media-cache.md#works-upload-covers)). YouTube needs no
lookup. The others are bounded per step: at most 30 niconico and 5 Bilibili
lookups, none started after two minutes, newest upload first.

- niconico: `https://ext.nicovideo.jp/api/getthumbinfo/<sm|nm|so ID>`, one
  request per video through the media
  [outbound proxy](outbound-proxy.md) area, with a 10-second timeout, a
  256 KiB body limit and no redirects. Only the answer's video ID and a
  thumbnail URL on `https://nicovideo.cdn.nimg.jp/thumbnails/<n>/<n>[.<hash>]`,
  where both `<n>` are the requested video's number, are read; `DELETED`,
  `NOT_FOUND` and `COMMUNITY` mean the video is gone. A deadline before or
  during the body is `niconico_timeout`. The first 429
  (`niconico_rate_limited`) is recorded as a failed lookup and ends the
  step's niconico lookups.
- Bilibili: the view endpoint above, for any uploader, with the account's
  device identity and backoff. A blocked source makes no request, and the
  first captcha or rate-limit answer starts the shared backoff and ends the
  step's lookups. A lookup never changes the list read's interval, last
  success or last error, and the step stops looking up while
  `SKIP_BILIBILI` is set.

The step's result adds `works`: `queued` YouTube images, `lookups`,
`resolved`, `missing` and `failed` lookups, the `covered`, `waiting` and
`uncovered` uploads, and `bilibili: "blocked"` or `"skipped"` when that source
was not asked. A failed lookup makes the step `partial`.

### Source Request Limits

Every source client requests fixed destinations with a 10-second timeout
(the X API, manual or in the chain, uses the request timeout setting; see
[X API method](workflows.md#x-api-method))
that stays active through body consumption, rejects redirects, and records
only controlled error codes. The Bilibili client builds its URLs itself on
its three fixed hosts and checks the mid and the dynamic ID before a
request; the shop client builds its three fixed paths on `shop.milpr.com`
(see [Official shop](#official-shop)). Response bodies are limited to 4 MiB
for X, YouTube and the shop and 2 MiB for Bilibili; a timed-out or oversized stream is cancelled
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
   heuristic to ordinary X posts and Bilibili dynamics, including messages
   without a configured keyword. A Bilibili dynamic also matches the Chinese
   words 今天, 明天, 后天, `<n>点`, 北京时间, 直播, 开播, 播, 开始, 预约,
   上传 and 投稿. Suspected messages are recorded with the `schedule_message`
   detection route, and the model first classifies the post as `schedule`,
   `not_schedule`, or `uncertain` before extracting events. With `schedule_message_all_posts`
   enabled, the path skips the heuristic and selects every post with text or
   images; each new post then costs one model call, still within the shared
   scan limit.

The model text starts with the stored publication timestamp followed by its
date, weekday, and time in the source's zone (Asia/Tokyo for X, Asia/Shanghai
for Bilibili), so relative dates such as 今日, 明天, or a bare weekday resolve
without a UTC conversion. Fingerprints use the stored timestamp alone.

Every returned entry must pass the event check: a non-blank title, a real
`YYYY-MM-DD` date, `HH:MM` times (an end time only with a start time, or for
a sale with a close date), for a sale a close date (`endDate`) that is a real
date not before its opening date, known `timePrecision` and `eventType` codes
and a confidence from 0 to 1. One
entry that fails it fails the whole extraction with `schedule extraction
contained an invalid event`. The prompt therefore states these formats and
asks the model to leave out entries that give only a month, a season or a
vague period, and to classify a post with no remaining entry as
`not_schedule`. The console's
[recent failures](admin-api.md#extraction-and-models) show the newest
stored failures with their messages.

The prompt defines each `eventType` in one sentence; a `sale` is a period to
buy, pre-order, order or apply for goods, media or tickets, and a `release`
the date a work becomes available. It asks for one entry per sale period with
its opening as `date` and `time` and its close as `endDate` and `endTime`;
本日から means the post date. A post that names only the deadline gives the
deadline as both dates and no start time. The close is stored as the
[sale period](data-model.md#sale-periods) for a sale and ignored for every
other type, and a shipping or delivery date is not an entry. A sale's
`endTime` without `endDate` closes it on the Japan date that time falls on.

Neither path, nor an operator's reprocess request, selects a post with
`posts.hidden_at` set; a request for a hidden post waits until the post is
shown again, and showing it makes it a candidate again. The events such a
post alone supports leave the public pages while it is hidden (see
[Events of hidden posts](data-model.md#events-of-hidden-posts)).

A post that quotes another status gives the model the quoted wording after
its own text, marked as quoted context (`[Quoted post by @handle. …]`),
because a quoted status reaches extraction no other way: posts often say
little more than 「これ！」 over a quoted announcement. A Bilibili
[forward](#forwards) marks the forwarded dynamic's wording the same way but
names its author by display name (`[Forwarded dynamic by <name>. …]`) and
says that times written in it without a zone are Beijing time, as in the
forward's own. The quoted wording joins the post's extraction fingerprint,
the single-message heuristic (the Chinese one for a forward too) and the
reading of the entry's platform; a post without a quote has the
fingerprint of its own wording alone. An unavailable quote, or one an
operator hid from the public pages (`posts.quote_hidden_at`), adds nothing,
so hiding or showing a quote changes the input and the post is extracted
again. Quoted images never join the input (see
[Quoted posts](#quoted-posts)).

The two candidate lists are merged, de-duplicated by post ID, sorted by
least recent attempt (unattempted posts first), with publication time as a tie
breaker. The shared limit counts candidates requiring work; unchanged fingerprint
cache hits and posts held back by the failure backoff below do not consume it.
Failures and blocked candidates move behind older
unattempted work on later scans. Manual reprocess requests still run first. A post selected by the
board path is not sent a second time through the message path in the same scan.

Each post is judged before anything is written: no content or images still
downloading is a skip, a stored success or `uncertain` result for the same
input is a cache hit, a post in its failure backoff is held back, and any
other post gets a model call. The scan and the console's
[queue preview](admin-api.md#queue-preview) share that judgement and the
order above, so the preview lists what a run started at that moment
processes. A cache hit whose post state already records that result writes
nothing.

A settled post is a cache hit decided without loading it. Each detection
stage reads every shown post with its input stamp for that stage, computed
in SQLite from everything the extraction identity and the image check read
(see [Post LLM states](data-model.md#post-llm-states)). A post whose state
records a cache hit on that stage's route for exactly that stamp, with no
reprocess request, error or failure backoff, keeps its place among the
candidates but counts as a cache hit without its images, cached files or
stored extractions being read. Its stage still selects it, so a board post
that stays on the board path is not sent through the message path, and the
order, the limit and the outcome of every other post are those of judging it.

The step result records the candidate phase as `candidateMs`
(listing requests and candidates, with their stamps, and ordering them),
`postsRead` (rows the detection stages read, each stage reading every shown
X and Bilibili post) and `candidatePosts` (the posts queued), then `judged`
(posts the scan loaded and judged), `settled` (settled posts it counted as
cache hits without loading them) and `judgeMs` (time spent loading and
judging, without model calls or writes). `cached` counts both kinds of cache
hit.

#### Time-zone rationale

The extraction model returns the date and time as written by the source, and code performs the fixed conversion to Asia/Tokyo. This keeps day rollovers deterministic and preserves the wording used for translation, while all stored schedule instants retain one display zone for readers, merging, and feeds.

#### Source time zones

A Bilibili dynamic gets its own prompt: a Chinese dynamic written for
Chinese viewers, with times in Beijing time unless it says Japan time. The
model gives each entry's date and times as written and names their zone in
`timeZone` (`Asia/Tokyo` or `Asia/Shanghai`, or an alias such as `CST`,
`UTC+8`, `北京时间`, `JST` or `UTC+9`); code converts them to
Asia/Tokyo, including the day rollover (Beijing 23:30 is 00:30 the next day
in Japan), so the model does no time arithmetic
([source time zones](../architecture/sources.md#source-time-zones)). An entry
without a known `timeZone` is in the source's zone, never an invalid
entry. X posts keep the Japan prompt and schema, and their entries are always
Asia/Tokyo whatever `timeZone` a model adds.

Events of a Bilibili dynamic have IDs `bilibili-schedule-<date>-<hash>`,
`events.source` and `event_sources.source` `bilibili`, and the dynamic's opus
page as the `event_sources` link. Extraction results and post states of a
dynamic are stored with source `bilibili` under its post ID.

Every entry, of either source, has its [announced
platforms](data-model.md#event-platforms) from `youtube`, `bilibili` and
`other` (another platform, or someone else's channel or room such as a
collaboration), in announced order. An entry's **wording** is its title,
detail and cited evidence, and the post's text when the post has this one
entry. **Bilibili-only wording** calls it Bilibili-only (bilibili限定,
B站限定, B限 as its own word rather than the end of WEB限定, ビリビリ限定)
without also naming YouTube.

The Bilibili schema asks for a `platforms` array of the platforms the post
names for the entry, both for a simulcast, and none for a plain 直播; a
value outside the enum is dropped, never an invalid entry. Code decides
whether Bilibili alone is stated: `["bilibili"]` from the model or a room
link is stated only for Bilibili-only wording, and so is a stream or
release that names none with such wording; otherwise a dynamic's
`["bilibili"]`, and a stream or members-only stream naming none, is
inferred, the source's default.

The X schema keeps its single `platform` (`unknown` when the post does not
say, and for an invalid value), and code derives the set, always stated: a
stream or release of a post with Bilibili-only wording is `bilibili` when
the model named none, and else the model's platform holds. A stream or
release whose wording announces a simulcast (同時配信, 同时直播, 同步直播,
simulcast, or YouTube and Bilibili both named together with 同時) adds the
other of `youtube` and `bilibili` after the model's platform (`youtube`
first when the model named none), so a notice whose model named Bilibili
still meets the dynamic's entry by Rule D; naming both sites without 同時
is no simulcast, and an `other` entry, a collaboration elsewhere, stays
`other`. On either source a YouTube link adds `youtube`. The model gives a link only when the post
does: an entry keeps the post's link, or the model's explicit one. A
live-room link (`live.bilibili.com`) never stays as an entry's link, since
reads add the room (see [Live room](#live-room)): it makes the entry
`bilibili` (on X in place of its single platform) and the entry keeps the
post's link, unless the entry is `other`, a collaboration in someone else's
room, which keeps that link and gets no live state.

Extraction identities name neither prompt nor schema, so a schema change
of one source extracts no stored post again: an entry keeps its platforms
until its post's content changes or an operator asks for its extraction.

Reconciliation links a dynamic's release to the upload it announced and
merges an X notice of a Bilibili stream into the dynamic's entry (see
[Duplicate merging](data-model.md#duplicate-merging)).

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
time, URL or cached images), when the text or image extraction queue's targets
or models change, and when an operator requests a reprocess, which also clears it immediately. A
queued reprocess request always runs, whatever the backoff, and a forced pass
that fails counts as the first failure. A success or `uncertain` result,
including a cache hit, clears the backoff. A cancelled run is not a failure.
`skipped` outcomes that make no call (`media_pending`, `no_content`, no
configured or compatible target) are not failures, keep any earlier count,
and stay eligible for later scans. The console shows the next retry time or
that retries stopped next to the post's state. A failed post without a
recorded failure count has no backoff and is tried on the next scan.

An operator can dismiss a failed post. It becomes `dismissed`, leaves the
console's failed list, and is held back like a post whose retries stopped
until its input or targets change or an operator requests a reprocess.

Text-only posts draw from the schedule extraction queue and posts with images
draw from the image extraction queue. Each queue is an ordered list of targets, a
provider plus one of its catalog models, and a target's capabilities are its
model's `text`/`image` tags.
Providers have no default model, and nothing reads the
`llm_providers.model` column. A one-time startup upgrade, recorded as
`llm_default_model_retired`, rewrites each queue target without a model
(including legacy provider-only routes) to name its provider's stored model.
Where that pair is already queued, the earlier position stays, and a target
whose provider has no stored model is dropped because it could never be
called. Target selection follows
the original post modality. Text-only input requires `text`; image-only input
requires `image`; mixed input uses the image extraction queue and requires an
image-capable model. Only ready cached images are sent to the model. A post that
declares an image but whose cache is not ready is recorded as `media_pending`
and is not silently reduced to text-only input. Each provider uses OpenAI
Responses, Chat Completions, or Anthropic Messages, receives at most the
[attempts per model](../operations/configuration.md#sync-settings) in total
calls (three by default), and then yields to the next enabled, keyed,
capability-compatible target in queue order; targets whose capabilities do
not fit the input are skipped without a call. `llm_providers.max_retries`
always stores 2 (startup sets every row to it), and the admin API validates
but does not store a different `maxRetries`; calls follow the attempts
setting, not this column.

### LLM Provider Catalog

`/admin` manages providers centrally. A provider stores the API host, format
(Responses, Chat Completions, or Anthropic Messages), timeout, and encrypted key. On an operator
action the server requests `GET <baseUrl>/models` with the stored key, a
timeout, rejected redirects, and a 4 MiB response limit; it accepts OpenAI-style
`{ data: [{ id }] }`, `{ models: [...] }`, or bare arrays. Upstream bodies and
errors are reduced to fixed messages. Tags for newly added models are suggested
from the model ID and stay editable. Removing a model removes its queue
targets. Model discovery uses only a `baseUrl` that passed the
[provider address rules](admin-api.md#extraction-and-models).

API hosts can omit `/v1`: bare origins use `/v1`, while OpenRouter and Gemini
use their preset API roots. Explicit versioned roots and custom paths are
preserved, and a pasted inference endpoint is reduced to its API root.
`backend/internal/llmadmin/catalog.go` applies this rule to server calls,
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
or confidence values before any event write. Times run up to `29:59`, as
Japanese schedules count late-night hours on from the evening's date: a start
of `25:30` on one date is stored as `01:30` on the next, with that date in its
event ID, so it matches the same entry written as `01:30`. An end is read from
the same evening, and one before the start falls on the following day. An
empty time means no time. A date more than 180 days before the post's
publication (Asia/Tokyo) moves to the following year when that lands between
a week before and three months after the post, so a December notice's 1/5 is
the coming January; any other date stays as the model gave it. It also rejects a result that is
not an object, a non-array `events` field, a result with neither a
classification nor an events list, and a `schedule` result without entries
unless its action is `cancel`; a rejected result is a failed attempt and
leaves existing events untouched. So is a reply the provider reports as cut
off at the token limit (`max_tokens`, `max_output_tokens`, `length`), refused,
or incomplete, even when its tool call parses; image extraction and
schedule-image reviews follow
the same rule, and a refusal stores the fixed `LLM refused the request`. `schedule` results update automatic
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
hashes), the detection path, input mode and the extractor version make these
outcomes idempotent. Each stored extraction also names the queue target that
produced it, but the cache ignores that part: when the newest attempt of the
current input is `success` or `uncertain`, any queue target's result stands,
so adding, removing, reordering or replacing queue targets and models sends no
post again, and an empty queue keeps the cached state. A post runs again when
its input changes, when its newest attempt failed (under the backoff below),
and on an operator's reprocess request. Date-only events store `starts_on`, a null `starts_at`, and
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

Providers and the model queues are configured explicitly in `/admin`; initialization
does not create or enable an OpenAI provider. Provider API keys are encrypted with
`LLM_SECRETS_KEY`; ciphertext is never returned by an API response or written
to extraction `raw_json`. A legacy `OPENAI_API_KEY` can be imported once with
`kano-go migrate-llm` (`npm run migrate:llm`) after creating an `openai-default` provider in `/admin`;
synchronization never reads that environment variable.
Candidates from either X account use the same idempotent fingerprint and
manual-lock rules. The stage flags are stored as `schedule_keyword_enabled`,
`schedule_vision_enabled`, and `schedule_message_enabled`. The Detection page
saves them, with `schedule_message_all_posts`, as one of two modes: keyword
match (the three stages, not all posts) or everything to the LLM (the message
stage on all posts); `PUT /api/admin/extraction` accepts any combination.
Once (marker `schedule_detection_mode_settled`), the first standalone start
stores running flags that match neither mode as everything to the LLM when
the message stage runs on all posts and as keyword match otherwise. Flags that
already match a mode, from storage or the environment, and flags with no stage
running are not written. The legacy `schedule_extraction_enabled` setting
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
cursor or the pending queue, and extraction reads it only as context of the
quoting post. The adapter
(`backend/internal/source/x/quote.go`) reads it into a sanitized
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

Every available quote also enters the X status pool, `x_statuses`, in the
quoting post's transaction, whatever its author (`backend/internal/store/sqlite/x_pool.go`).
The pool row and `quote_json` come from the same projection, so the pool
text is the text the quote card shows. The row keeps the status ID, author,
wording, publication time, x.com URL and a sanitized `raw_json` with the
quoted images' addresses, and `quoted_at`, the publication time of the
newest own post that quotes it. A quote of a stored post (a self-quote)
enters the pool too, but its translations stay the stored post's. No public
reader reads or joins the pool, and nothing of a pooled status gets a card,
a media link or a schedule candidate of its own. Startup fills the pool from
the quotes stored posts' `raw_json` already hold, with no request.

The [translate step](#translation) translates quoted statuses by status ID,
so a status quoted several times is translated once, and the quote card
resolves those translations against its own wording (see
[Translations](data-model.md#translations)). Schedule extraction reads the
quoted wording as context of the quoting post (see
[Schedule extraction pipeline](#schedule-extraction-pipeline)).

An operator can hide a post's quote from the public pages
(`posts.quote_hidden_at`); synchronization, the X API fetch method and the
startup quote backfill (which fills a missing `quote_json` from the post's
stored `raw_json`) keep updating the stored quote underneath and never write
that decision. Hiding the whole post (`posts.hidden_at`) works the same way:
synchronization keeps rewriting the stored post and never clears it, while
extraction skips it (see
[Schedule extraction pipeline](#schedule-extraction-pipeline)).

#### Reposts

Timeline pages list an own account's repost under the original status's ID,
time and author, with `reposted_by` naming the account; X API answers list
it as a post that references the status as `retweeted`. Neither is ever a
post or a cursor, pending or gap entry. Recent synchronization and manual
fetches record each one in `x_reposts`: the reposted status ID, the
reposting account and, when known, the status's author, with the time it
was first seen, which a later listing keeps. The board shows no reposts. The
timed X API method asks for the account's posts without retweets, so it
records none.

A reposted status of a [related account](#related-accounts) also enters the
pool with `reposted_at`, from the repost item when the account's own
timeline has not stored it yet, and its reasons are evaluated again: it is
translated for the `reposted` reason and promoted only by a keyword or by
hand. A repost of any other author records the link alone.

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
refresh shares the X status-request budget, so with a budget of 0 it
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

An operator can also make any of an X post's own images its schedule asset
from the console's post dialog, with a required reason and optional period.
The pick labels the asset `schedule` and sets `manual_image_at`; a different
image than the asset showed returns its model verdict to `pending`. Later
synchronization keeps a picked asset's image, review and period.

A schedule asset's period is `period_start`/`period_end` with its
`period_basis`; the schema upgrade that adds these columns fills them from a
stored `week_start` as a seven-day period. The image check does not read
dates, so an asset without a stored period takes the whole Monday-to-Sunday
weeks around the live (not deleted) events extracted from its source post,
with basis `events` (`SchedulePeriodSQL`,
`backend/internal/store/sqlite/schedule_period.go`). Readers compute it on
every request, so it follows later extractions and event edits. An asset
whose post has no events is unknown and labelled so in the console. Operators set or correct a period with a required reason
(`manual_period_start`, `manual_period_end`, `manual_period_reason`)
independently of image approval, and the manual period wins. The dashboard
and calendar list an approved asset only while its period overlaps the
requested dates.

A schedule image (`assets.kind = schedule`) is public only after two gates:

1. The board-notice gate (`scheduleBoardPattern`,
   `backend/internal/schedule/store.go`) checks that the source post's
   own wording (its stored `raw_json.search_text` when present, otherwise
   its text) reads as a weekly or multi-day board notice. The stored post `label` is not part of that input, because the
   looser post classifier labels any `配信予定` post as `SCHEDULE / 日程`.
2. The automatic scan sends each unreviewed candidate's ready cached image,
   without the post text, to the image extraction queue
   (`llm_route_targets.route = 'verify'`), whose targets need the `image` tag
   and which never falls back to the text extraction queue. The model records
   `schedule`, `not_schedule`, or `uncertain` plus the visible schedule text in
   `schedule_asset_reviews`. A candidate whose source post does not pass
   gate 1 is recorded as `skipped` / `source_not_board` without a model
   call. A pending cache, or no queued, keyed Image model, is also `skipped`
   and retried. An asset without an image URL, such as a `weekly-schedule`
   alias row, has nothing to download: it is recorded once as `skipped` /
   `no_image` and left out of later scans, forced ones included, until a
   writer gives it a URL. It is never public and never counts as needing
   review; the console shows it as having no image, and an operator may
   still label it `not_schedule`.

`/api/dashboard` returns a schedule asset only when an operator labelled it
`schedule`, or when it has no manual label, still passes gate 1, and the model
returned `schedule`. A manual `not_schedule` always hides it. Labels require a
reason and are set on the `/admin` schedule calendar, in its list of
images or in the Review queue. A model verdict
belongs to the image URL it judged: when a post's asset receives a different
URL, its `asset_version` increases and its model verdict returns to
`pending`, while a manual label stays.

Image review budgets apply after cache checks and manual-label filtering across
all stored candidates, rather than only the newest page. Retry order follows
the least recent check/review attempt. Reviews persist an input fingerprint
of the extractor version, image hash and source post URL. A `schedule`,
`not_schedule` or `uncertain` verdict for the current fingerprint stands
whichever check-queue target gave it, so adding, removing, reordering or
replacing targets and models sends no image again, and an empty queue keeps
the verdict. An image is checked again when its image or source changes, when its last
check failed, and in a forced pass.

### Translation

The `translate` step machine-translates X post and Bilibili dynamic text,
the titles of the unmerged videos public readers list (YouTube videos and
Bilibili uploads whose form is settled), the X statuses own posts quote or
repost and the
dynamics own forwards forward into
English, Japanese and Simplified Chinese, each
locale only when it is not the detected source language (see
[Translations](data-model.md#translations) for storage and precedence).
A merged Bilibili upload's title is its video's platform translation and is
never sent to a model, and the step never requests a source. Before any
model call it settles [Bilibili forms](data-model.md#bilibili-form) by rule,
whether or not a queue is configured, and the same queue then asks the
unsettled uploads for form and tag. Its models come only from the separate
translation queue (`llm_route_targets.route = 'translate'`), configured in
`/admin` → Models; it never falls back to the schedule extraction queue. Targets need
the `text` tag. Each target gets at most three calls before moving to the
next configured target, in queue order.
Without a usable target the step records `disabled` with the reason and
keeps operator requests.

Candidates are posts, YouTube videos, pooled quoted or reposted X
statuses that are not stored posts (`owner_type` `x_status`, see
[Quoted posts](#quoted-posts)) and pooled forwarded dynamics that are not
stored posts (`owner_type` `bilibili_status`, see [Forwards](#forwards))
whose current wording lacks a usable translation of a needed locale:
operator requests first (forced passes that replace machine rows, whatever
their age), then posts and videos, then pooled X statuses, then pooled
dynamics; within each, never-attempted wording, then the least recently
attempted, newest first. Unrequested candidates are limited to the
translation lookback (30 days by default), by publication, for a pooled
status by when an own post last quoted it, and for a pooled dynamic by when
an own forward last forwarded it, and at most the translation items per run
(20 by default) are sent per run, pooled statuses included (see
[sync settings](../operations/configuration.md#sync-settings)). A locale already covered
by a manual or platform row is not a reason to call a model.

Wording with only links, hashtags, mentions or emoji is recorded as
`skipped` with language `none` without a call; wording over 4000 characters
is skipped without a call. The model receives the kind (X post, which a
pooled status also is, Bilibili dynamic, which a pooled dynamic also is, or
YouTube video title) and the text, returns `sourceLanguage` plus English, Japanese and
Simplified Chinese, and is told to keep URLs, tags, mentions, emoji,
bracketed emoji codes and bracketed channel tags, and to keep times as
written with their zone (北京时间17点 becomes 17:00 Beijing time; converting
a time belongs to extraction). A result is accepted only with a known language and a
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
