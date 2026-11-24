# Public Snapshot Contract

The dashboard snapshot contract and the revision stream.

`GET /api/health` returns service status, the current ISO timestamp, and a
whitelisted synchronization summary; it does not return a database path or
label. `lastSync.message` is fixed English base text, not localized copy: it
is `Sync completed`, `Some data sources are unavailable or reported warnings;
the existing snapshot remains`, `Sync failed; the existing snapshot remains`,
or `Sync in progress` for the corresponding `success`, `partial`, `failed`,
and `running` statuses. It is `null` for any other status. `GET /api/dashboard?days=3`
clamps days to 1 through 30 and returns
profile, summary, posts, events, videos, focus, timeline, resources, schedule
assets, media status, and metadata. Posts are the newest 100 published within
the window plus, however old, the eight newest of each own account, without
the posts of archive-only accounts (`x_archive_accounts`); MCP
`dashboard_read` and `posts_list` read the same list. A post with
`promoted: true` is another account's status promoted onto the board, a
related account's or a reposted one (see
[Related accounts](../sources/x-accounts.md#related-accounts) and
[Reposts](../sources/quoted-posts.md#reposts)): it is never `latestPost`
or one of an account's eight. It appears in the list and in the post archive
like any other post, except while a listed own post quotes it with its quote
shown: the quote card already shows it, and it returns once that post or its
quote is hidden. Its `accountHandle` is its
author. `repostedBy` lists, as handles without `@` and first sighting first,
the own accounts that reposted a promoted post; it is `[]` for every other
post. `counts.posts` counts
the window only. `counts.videos` counts public videos whose publication time is
within the most recent seven days; it is independent of the bounded homepage
video list. Public videos, on the board, the archive, the single-video read,
MCP and the Atom feed, are every YouTube video and each Bilibili upload that
is not merged into another video and whose form is settled (see
[Video merging](../data-model/videos.md#video-merging)); a video's `source` is
`youtube` or `bilibili` and its `url` its own platform's page, and a YouTube
video lists its merged Bilibili uploads in `alternates` as `{source, url}`.
A Bilibili upload is never upcoming or live; its `kind` is `upload` or
`stream` (a live replay, shown as a stream archive).
The summary contains `nextEvent`, `nextStream`, `latestVideo`, `latestPost`,
and counts; `latestPost` survives an empty requested window. `nextEvent` is
never a sale period. `nextStream` is a video confirmed on air when there is
one, otherwise the first focus video by start; a waiting room without a
start is `nextStream` only when no scheduled video leads. `latestVideo` is
the stage's fallback: the newest non-upcoming video that is not a Short,
unless the operator featured that Short, and that is neither private nor
removed; a video of unknown format or unknown availability stays
eligible. Metadata includes configured X accounts and the Featured video ID.

A post's `media` and its quote's `media` list images as `{id, url, status,
sourceUrl, alt, position, width, height, kind}`. `kind` is `photo`, `video`
or `gif`: a video's or GIF's entry is its thumbnail, and the board links it
to the post or the quoted status on X. Every image that is not an X video or
GIF, a Bilibili one included, is a `photo`.

A post's `type` is `daily` or `notice`, and its `label` is English base text,
not localized copy: `SCHEDULE` or `ANNOUNCEMENT` for an X notice by its
wording, `DAILY` for any other X post and every Bilibili dynamic, or `null`
for a post stored without one.

A post's `source` is `x` or `bilibili`. A Bilibili dynamic has the ID
`bilibili-<dynamic ID>`, its opus page as `url`, the account's mid as
`accountHandle` and its space as `accountUrl` and zero engagement counts; its
text may be empty when the dynamic held only emoji. A forward's `quote` is
the dynamic it forwards (see [Quoted posts](../data-model/posts.md#quoted-posts)),
with that dynamic's post ID as `id` (`null` when the listing did not name
it), the author's mid as `authorHandle`, and its Bilibili page as `url`; a
forward's own text may be empty. A dynamic with stickers adds `rich`, and an
available quote with stickers `quote.rich`: the original wording as
`{text}` and `{emoji, url}` segments whose `text` joined is `text`, where
`url` is a `/media/` address of this site (on `CDN_PUBLIC_BASE` when set) or
`null` and never a third-party one (see
[Bilibili stickers](../data-model/bilibili-stickers.md)).

Every public event reports its `platform` (`youtube`, `bilibili`, `other`,
`unknown`, or null for an entry stored before platforms were recorded) and
`platforms`, the platforms it is shown on with its own first (an empty array
when none is known; see
[Event platforms](../data-model/events.md#event-platforms)). Full event reads (the
board, the calendar) add `links`, one `{platform, url, liveStatus}` per
shown platform in that order, where `url` is a page on that platform or
null (an announcement post stays the event's `url`) and `liveStatus` is
that platform's broadcast state, and `liveUrl`: the
account's Bilibili live room for an entry of type `stream` or `member` shown
on Bilibili once a live check learned the room, null otherwise. `url` stays
the announcement's link, except that a stored live-room link reads as null.
An entry's `liveStatus` follows its linked video, a YouTube event an
operator merged into it, or the room when the room's broadcast matched it
(see [Live room](../sources/bilibili.md#live-room)), and is `live` while any of them
is. The
dashboard adds `liveNow`, `{platform: "bilibili", url, title, startedAt}`,
while the room is live within two Bilibili check intervals and the
broadcast matches no stored entry, and null otherwise; it is never an event,
and readers match it against entries by platform and time, not by link. `meta.bilibiliAccounts` lists the accounts (mids) of the
stored Bilibili posts; the post archive's `account` filter and its
`accountCounts` take a mid like an X handle.

`meta.sourceFetchedAt` contains only `x`, `youtube`, `bilibili` and `shop`
ISO timestamps or null, derived from successful incremental source reads, the
head pages of manual timeline fetches, the Bilibili account's last successful
read and the shop's. Other manual fetches do not refresh it; a stored recent-window
checkpoint counts up to its window's end time. Timeline `count` and `all`
fetches retain their successful head-page read time, so resuming old pages
does not refresh the source; a timeline checkpoint without that time leaves
the source time unknown. Media-only runs,
cursor cleanup and API reads do not advance these source timestamps. Internal
account IDs, checkpoints and fetch errors remain protected.

`GET /api/visitor-region` returns `{ "enabled": false, "mainlandChina": false }`
by default. When the operator enables the setting with a provider name,
provider website and mirror site, it also returns `providerName`, `providerUrl`
and `mirrorUrl`. It returns `mainlandChina: true` when the configured edge or
reverse proxy supplies a `CN` country code, and `false` otherwise. It reads `CF-IPCountry` first, then `X-Country-Code`,
`X-Geo-Country` and `X-Visitor-Country`; the proxy must overwrite these
headers rather than pass visitor-supplied values through. The response is
`no-store` and contains no address or raw header. The public pages link the
site in the Simplified Chinese footer whenever `enabled` is true and use
`mainlandChina` only to decide whether to offer it in a notice. The operator console controls
`enabled` through its `visitor_region_enabled` setting, which defaults to off.

`backend/internal/publicview` owns shared public event, synchronization,
and selected profile-media projections for HTTP and MCP. A public sync summary
contains only id, source, start/finish times, status, and a fixed status
message. Public profile slots contain only id, slot, source, sourceUrl,
status, isActive, and publicUrl. Cache paths, source references, download
errors, and candidate metadata remain available through authenticated admin
reads. Per-source counters, raw fetch errors, job IDs, and internal source
item IDs stay in the guarded admin API. `GET /api/dashboard/revision` reports
a small revision hint without external work, and
[`/api/dashboard/events`](#revision-stream) pushes the same number as it
changes. Timestamps are parseable ISO 8601 strings; display and date-window
boundaries use Asia/Tokyo.

`/api/dashboard`, `/api/calendar`, `/api/posts`, `/api/posts/months`,
`/api/videos`, `/api/videos/months`, `/api/works/uploads` and the
[collection reads](journey.md) and the
[LINE BLOG archive](lineblog.md) reads send
`Cache-Control: no-cache` and a weak ETag, a SHA-256 prefix of the
uncompressed JSON body. The dashboard's tag leaves out `meta.generatedAt`,
the time the response was computed, which a
[prepared response](../backend.md#text-transfer-and-static-caching) keeps
until it expires; the other time-derived fields, such as
`isUpcoming` and `liveStatus` in every one of these reads, are included, so
the tag changes with them as well as with stored data. A matching
`If-None-Match`, weak or strong, answers 304 without a body, and the client
keeps its stored copy, including that copy's `generatedAt`. Because the tag is
weak it validates the gzip and identity encodings alike.
`/api/dashboard/revision` is `no-store`.

## Revision Stream

`GET /api/dashboard/events` is a `text/event-stream` of the dashboard
revision. On connect it sends `retry: 5000` and the current revision as one
`data:` line, then one `data:` event for each later change, and a comment
every 25 seconds so proxies keep a quiet stream open. An event carries only
the revision number, the value `/api/dashboard/revision` already reports;
snapshot data, job IDs, counters and errors never travel on it. A client that
reconnects receives the current revision again, so it cannot miss a change.

One watcher goroutine in the serving process reads the stored revision every
second while at least one stream is open and offers each change to every
stream; it stops with the last stream. Reading the stored value covers every
writer, including `kano-go sync` and other processes that write SQLite
directly. A stream answers `Cache-Control: no-store, no-transform` and
`X-Accel-Buffering: no` and is never compressed. The listener's read and
write timeouts do not end it; each write instead has its own 10-second
deadline, so a client that stops reading releases its stream. At most 1,000
streams are open at once: another one answers 503 `stream_unavailable` with
`Retry-After`, and a database failure before the stream starts answers 503
`database_unavailable`. Shutdown ends every stream before the listener
closes. The [reverse-proxy settings](../../operations/docker.md#reverse-proxy)
the stream needs are with the deployment guide.

Media responses follow the [media HTTP contract](../media-cache/layout-and-model.md#http-contract).
Selected profile media uses `/media/profile/avatar` and
`/media/profile/banner`, cached per-account X avatars use
[`/media/account-avatar/{id}`](../media-cache/lifecycle.md#account-avatars), the LINE BLOG archive's files use
[`/media/lineblog/<name>`](../media-cache/packaged-media.md#line-blog-archive-media), and the
discography covers use [`/media/works/<name>`](../media-cache/packaged-media.md#discography-covers).
`/api/dashboard` and `/api/posts` also include `accountAvatars`, mapping
`<source>:<account_key>` to its versioned `/media/account-avatar/...` path or
`null` when hidden; accounts with no cached avatar are omitted.
With `CDN_PUBLIC_BASE` set, the reads name these routes on the CDN, which pulls
them from the same paths here.
