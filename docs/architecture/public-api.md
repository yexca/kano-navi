# Public API

Public reads, feeds, media and MCP read a prepared SQLite snapshot. None of
them fetches X, YouTube or a model provider. The service that hosts them is
described in [Backend](backend.md).

## Public Snapshot Contract

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
the window plus, however old, the eight newest of each own account; MCP
`dashboard_read` and `posts_list` read the same list. A post with
`promoted: true` is a related account's status promoted onto the board (see
[Related accounts](sources.md#related-accounts)): it is never `latestPost`
or one of an account's eight, and the list and the post archive leave it out
while a shown own post shows it as a shown quote. `counts.posts` counts
the window only. `counts.videos` counts public videos whose publication time is
within the most recent seven days; it is independent of the bounded homepage
video list. Public videos, on the board, the archive, the single-video read,
MCP and the Atom feed, are every YouTube video and each Bilibili upload that
is not merged into another video and whose form is settled (see
[Video merging](data-model.md#video-merging)); a video's `source` is
`youtube` or `bilibili` and its `url` its own platform's page, and a YouTube
video lists its merged Bilibili uploads in `alternates` as `{source, url}`.
A Bilibili upload is never upcoming or live; its `kind` is `upload` or
`stream` (a live replay, shown as a stream archive).
The summary contains `nextEvent`, `nextStream`, `latestVideo`, `latestPost`,
and counts; `latestPost` survives an empty requested window. `nextEvent` is
never a sale period. `latestVideo` is
the stage's fallback: the newest non-upcoming video that is not a Short,
unless the operator featured that Short, and that is neither private nor
removed; a video of unknown format or unknown availability stays
eligible. Metadata includes configured X accounts and the Featured video ID.

The dashboard also carries a locale-neutral `projection` for shared board
decisions. `projection.version` identifies the contract (`1` is the current
version), `nextTransitionAt` is the next parseable ISO instant that can change
a time-dependent decision, `focus.eventIds` and `focus.videoIds` are the
ordered event and video candidates, and `sales` is the ordered list of sale
event IDs shown in the sale strip. `spotlight` identifies the next or latest
ticket, its live state, the members chip decision, and an optional companion
video. `shelf` contains the lead ID, whether it is Featured, and the ordered
reservation, work, archive and Short IDs. The browser uses these IDs to join
the returned records; it does not infer their order from localized text.

Each event has a `view` with the canonical `state`, `typeCode`,
`activityCode`, `activityContext`, `isWork`, and sale-expanded
`calendarEntries`. `state` has exactly seven values: `upcoming` means the
entry has not started, `live` means a linked broadcast is on air, `open` means
a sale period is open, `done` means a non-pending entry or sale has ended,
`pending` means the time or status still needs confirmation, `cancelled` means
the entry is cancelled, and `cancellation_review` means cancellation evidence
is awaiting confirmation. `typeCode` is one of `stream`, `member`, `event`,
`release`, `video` or `sale`, or null when the stored type is outside the
public set. `activityCode` is one of `live`, `game`, `talk`, `song` or `video`,
or null. `activityContext` is `stream` or `work` when an activity code is
present, otherwise the empty string. `isWork` is true only for `release` and
`video` types. `calendarEntries` keeps the stored `timePrecision` code
(`exact`, `approximate` or `unknown`); `saleMark` is null for ordinary events,
`opens` for a sale opening and `closes` for a sale closing. A sale deadline
has only its closing entry.

Each video has a `view` with its `phase`, `formCode`, `shelfGroup`,
`latestEligible`, `focusEligible`, and `tagContext`. `phase` is one of
`upcoming`, `live`, `started`, `past` or `cancelled`. `formCode` is one of
`short`, `upcoming`, `stream`, `archive`, `premiere` or `video`, or null when
no public form applies. `shelfGroup` is one of `reservation`, `work`,
`archive` or `short`, or the empty string for a video omitted from the shelf.
`tagContext` is `stream` or `work`; the two eligibility fields are booleans.
These values are codes and booleans, never localized copy. Calendar, archive
and MCP `schedule_list` reads carry the same event and video `view` objects.
Clients keep an additive fallback for responses without `projection` or
`view` so a retained snapshot remains readable while a deployment serves
mixed versions.

The remaining projection fields use these fixed values. `spotlight.mode` is
`next` when a current or upcoming candidate exists and `latest` when the
latest eligible video is the fallback; `spotlight` is null when neither
exists. `shelf.leadId` is null when there is no work lead, and
`leadIsFeatured` distinguishes the operator featured video from the derived
lead. `nextTransitionAt` is null when no scheduled transition is known.

A post's `source` is `x` or `bilibili`. A Bilibili dynamic has the ID
`bilibili-<dynamic ID>`, its opus page as `url`, the account's mid as
`accountHandle` and its space as `accountUrl` and zero engagement counts; its
text may be empty when the dynamic held only emoji. A forward's `quote` is
the dynamic it forwards (see [Quoted posts](data-model.md#quoted-posts)),
with that dynamic's post ID as `id` (`null` when the listing did not name
it), the author's mid as `authorHandle`, and its Bilibili page as `url`; a
forward's own text may be empty.

Every public event reports its `platform` (`youtube`, `bilibili`, `other`,
`unknown`, or null for an entry stored before platforms were recorded) and
`platforms`, the platforms it is shown on with its own first (an empty array
when none is known; see
[Event platforms](data-model.md#event-platforms)). Full event reads (the
board, the calendar) add `links`, one `{platform, url, liveStatus}` per
shown platform in that order, where `url` is a page on that platform or
null (an announcement post stays the event's `url`) and `liveStatus` is
that platform's broadcast state, and `liveUrl`: the
account's Bilibili live room for an entry of type `stream` or `member` shown
on Bilibili once a live check learned the room, null otherwise. `url` stays
the announcement's link, except that a stored live-room link reads as null.
An entry's `liveStatus` follows its linked video, a YouTube event an
operator merged into it, or the room when the room's broadcast matched it
(see [Live room](sources.md#live-room)), and is `live` while any of them
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
`/api/videos`, `/api/videos/months` and `/api/works/uploads` send
`Cache-Control: no-cache` and a weak ETag, a SHA-256 prefix of the
uncompressed JSON body. The dashboard's tag leaves out `meta.generatedAt`,
which changes on every read; the other time-derived fields, such as
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
closes. The [reverse-proxy settings](../operations/docker.md#reverse-proxy)
the stream needs are with the deployment guide.

Media responses follow the [media HTTP contract](media-cache.md#http-contract).
Selected profile media uses `/media/profile/avatar` and
`/media/profile/banner`, the LINE BLOG archive's files use
[`/media/lineblog/<name>`](media-cache.md#line-blog-archive-media), and the
discography covers use [`/media/works/<name>`](media-cache.md#discography-covers).

## Bounded Public Calendar

`GET /api/calendar?from=2026-10-05&to=2026-10-11&page=1&pageSize=100`
reads an inclusive range of at most 42 Japan dates (six Monday-first weeks,
enough for a month grid), with 1–100 events per page in stable chronological
order. It returns sanitized `items` / `events`, `total`, `totalPages`,
`hasNext`, `dayCounts`, `revision`, matching approved schedule images and
`adjacent.previous` / `adjacent.next` event dates. Invalid ranges return 400.
Every read uses the same public event projection as the board, including the
trusted `liveStatus` of an event's linked video; it does not fetch platforms
or models. MCP `schedule_list` is the bounded, paginated history reader;
`dashboard_read` shares the bounded snapshot. Events merged into another event,
and automatic events whose every source post is hidden, are excluded from
every one of these reads, including totals, day counts, adjacent dates and
summary candidates (see [Duplicate merging](data-model.md#duplicate-merging)
and [Events of hidden posts](data-model.md#events-of-hidden-posts)).

Every public event carries `endsOn`, the Japan close date of a
[sale period](data-model.md#sale-periods) or null. A range lists a sale when
it contains the sale's opening or close date, and `dayCounts` and the adjacent
dates count the close date too.

Every public event carries `tag`: `live`, `game`, `talk`, `song` or `video`,
or null when the entry is unlabelled. Every read derives it the same way from
the event and its YouTube videos, so the board, `/api/calendar`, MCP
`dashboard_read` and `schedule_list` report the same value, and an entry
without a video link can carry one. Readers translate the code for display
and fall back to the title when it is null; see
[Shown label](data-model.md#shown-label) for the order.

Dashboard SQL selects up to 100 recent post previews plus the latest-post
fallback, up to 100 current-week event previews plus 30 upcoming and 30 live
focus candidates, up to 10 [sale periods](data-model.md#sale-periods) on sale
or opening within seven days (closing soonest first), and at most one linked
event per returned video, and bounded
video candidates plus Featured selection and the latest completed video.
Live event eligibility is applied before the 30-row limit: cancelled and
suspected cancellations, tombstones, non-stream activities and expired streams
do not consume the shortlist. Eligible live events use ascending start instants
and stable ID ties, matching the frontend focus ordering, including streams
spanning earlier weeks. Known end times remove finished streams before limiting
focus candidates and retain streams with explicit longer durations; an absent
end gives streams and memberships the strictly less-than-three-hour grace
period, while other YouTube activities such as premieres need an explicit
future end. Video/event association uses indexed primary, source-evidence and
normalized URL identities, in one query for all returned videos rather than
scanning events for each video. Media is resolved only for returned owners:
their media rows and post links are read in batched queries, and each
directory of the media cache is resolved once per read. Event and media growth therefore does
not enlarge a homepage response. Full event totals and upcoming counts are
separate SQL aggregates; calendar pagination preserves access to every
activity instead of dropping later rows.

JSON parser failures return `invalid_json` (400), excessive bodies return
`request_too_large` (413), and unsupported encodings return 415. The global
handler logs only a fixed event/code/status object, including unexpected
errors; it never serializes error messages, stacks, raw bodies, headers or
keys.

## Bounded Public Archives

`GET /api/posts?from=2026-10-01&to=2026-10-31&page=1&pageSize=30&account=kano_2525`
and `GET /api/videos?from=2026-10-01&to=2026-10-31&page=1&pageSize=30` read
the stored posts or videos of an inclusive range of at most 42 Japan dates,
newest first, with 1–50 items per page (30 by default) and stable ID ties.
A post's time is its publication; a video's is its scheduled start, or its
publication without one. Undated videos are not listed. Both return sanitized
`items` in the dashboard's post and video projections (resolved ready media,
no raw JSON), `page`, `pageSize`, `total`, `totalPages`, `hasNext`,
`revision` and `adjacent.previous` / `adjacent.next`, the nearest Japan
dates outside the range that hold an item. The optional post `account`
filter is one X handle, compared without case, and also narrows the totals and
adjacent dates; `accountCounts` always counts the range's posts per lowercase
handle. The optional video `category` filter is `game`, `talk`, `song` or
`other`, narrows the totals and adjacent dates the same way, and
`categoryCounts` always counts the range's videos per category. A video's
category is its shown [tag](data-model.md#video-form-and-tags) (`game`,
`talk` or `song`: the operator's, else its schedule entry's label, else the
title inference's), or `other` without one; each read derives it, and every
video projection carries the tag as `tag`, `null` without one, and who can
watch it as `availability` (`public`, `unlisted`, `members`, `private`,
`removed`, or `null` until a check could tell; see
[Video availability](sources.md#video-availability)). An invalid range returns 400
`invalid_range`, an invalid handle 400 `invalid_account` and an unknown
category 400 `invalid_category`. Reads revalidate through their
[ETags](#public-snapshot-contract), negotiate gzip like the other public JSON
reads and never fetch platforms. The board's schedule and post month dialogs
use the ranges with the calendar; its video dialog uses the timeline.

Without `from` and `to`,
`GET /api/videos?until=2026-09-30&page=1&pageSize=30&category=game` is the
video timeline: the stored videos dated on or before the inclusive Japan date
`until`, or every dated video without it, newest first, paged, projected and
filtered like a range. Its `categoryCounts` count every video it pages
through, `adjacent.previous` is always `null`, and `adjacent.next` is the
nearest later Japan date holding a video, so a timeline that starts at an
earlier month knows that newer videos exist. An invalid `until`, a lone `from`
or `to`, or `until` together with a range returns 400 `invalid_range`.

`GET /api/posts/months` and `GET /api/videos/months` are the month indexes of
the post archive and the video timeline: `months` lists every Japan month
(`YYYY-MM`) that holds a shown post or a listed video, oldest first, each with
its `total` and its `accountCounts` per lowercase handle or `categoryCounts`
per category, plus the `revision`. Hidden posts and records without a
parseable time are not counted. They take no parameters, revalidate through
their ETags and negotiate gzip like the archive pages.

## Works Uploads

`GET /api/works/uploads` joins the Works page's packaged upload list
([Upload covers](../development/works-archive.md#upload-covers)) with stored
data by video ID, on each read and without writing. It returns `updatedAt`,
the list's date, `revision`, and `uploads`, keyed by upload ID, for each
upload with a ready cover or a stored video: `cover` (`{url, width, height}`,
a local `/media/<id>?v=<sha256>` URL with smaller copies, or `null`) and
`videos`, the stored YouTube and Bilibili videos of the upload in list order
as `{source, id, title, url, publishedAt, scheduledAt, durationSeconds,
availability, translations, sourceLanguage}`. Uploads with neither are left
out. A stored video's title is its shown one (an operator's correction
first); raw JSON and thumbnails' source URLs never appear.

`recent` lists, newest first, at most 48 of the stored videos dated after
`updatedAt` that the page shows above the list, derived on each read under
the rules in
[Uploads after the collection date](../development/works-archive.md#uploads-after-the-collection-date);
`recentTotal` counts them all.
Each is `{id, source, title, url, publishedAt, cover, origin, translations,
sourceLanguage, alternates}`: `source` is `youtube` or `bilibili`,
`publishedAt` is a premiere's scheduled time or the publish time, `cover`
is its ready cached thumbnail in the shape above or `null`, `origin` is
`auto` (listed by its tag, form and format) or `operator` (an operator's
decision includes it), and `alternates` names, for a YouTube video, its
merged Bilibili uploads as `{source, url}`. No kind, artist, credits or
albums are inferred, and the
operator's decision itself, availability and counters stay out. Other videos
outside the list never appear. The read revalidates through its
[ETag](#public-snapshot-contract) and never fetches platforms.

## Public Feeds

`GET /api/calendar.ics` is an iCalendar (RFC 5545) subscription of the events
dated 30 days before to 90 days after today's Japan date, at most 500 events,
each once: events from today on fill the cap first and the most recent past
events take what is left. They are read through the same bounded calendar
pages and event projection as `/api/calendar`. The calendar names no time
zone, so it reads the same in every viewer's zone: times are UTC, and an event
without a time, or of `unknown` precision, is a floating all-day entry
(`DTSTART;VALUE=DATE` with an exclusive `DTEND` the next day) on its Japan
date. All-day entries also carry `X-MICROSOFT-CDO-ALLDAYEVENT:TRUE`, which
keeps Outlook from drawing them across two days; timed entries do not. A
timed event without a stored end after its start lasts an hour when it is a
stream (the `stream` and `member` types the board treats as streams) and half
an hour otherwise, since RFC 5545 would make it zero-length. A sale period
is written at its ends, as the board shows it: its opening (titled "Sales
open:" in the feed's language) and, when it closes on a later date, its close
("Sales close:"), or the close alone for a deadline (a close on the opening
date without a stored opening time; a stored time of `unknown` precision
makes an all-day opening, as on the board). A closing entry with a
time ends at that time and starts half an hour earlier. A UID is the
public event ID with `@kano-navi`; the closing entry of a period adds
`-close` to the ID. `LAST-MODIFIED` is the event's stored
`updated_at`, and `SEQUENCE` is that time in whole seconds since 2020-01-01
UTC, so it grows whenever the event is written and clients replace a changed
entry; an event without a parseable update time has neither. `STATUS` is
`CANCELLED` only for a cancelled status or an operator's confirmation
(`manual_confirmed`), `TENTATIVE` for a suspected cancellation or a
`tentative` status, and `CONFIRMED` otherwise. The description carries the
event's detail, its provenance and its public link; only absolute HTTP(S)
links are written. Text is escaped and lines are folded at 75 octets without
splitting a character.

`GET /api/feed.xml` is an Atom 1.0 feed of at most 50 public videos (YouTube
videos and reservations and unmerged Bilibili uploads, as the video archive
lists them) dated 30 days before to 11 days after today, newest first. An
entry is dated by its publication (a future time is dated at the start of
today's Japan day until it passes, so the entry does not move between polls),
says when a reservation is scheduled, and links the video and any
merged platform copy. Its categories name the form (`stream`, `premiere`,
`upload`, `short` for a Short, or `video` while the form is unknown) and
then the video's tag, when it has one. Entry and feed IDs are `urn:kano-navi:` URNs. The
server never builds links from the request's `Host`: with `PUBLIC_ORIGIN` the
feed links the site absolutely and adds a `rel="self"` link to
`/api/feed.xml`; without it the site link stays `/` and there is no self link.

Both take an optional `lang` query, `en`, `ja` or `zh-CN`; a missing or
unknown value is the English base, so the bare paths stay English. Titles
resolve [translations](data-model.md#translations) the way the board does:
each reader gets the locale's translation when one exists, except that
Japanese readers keep wording detected as Japanese (an event only through its linked
video's title). The fixed wording the server writes — the calendar and feed
names (the board's `site.name`), the event descriptions' provenance and
unofficial notes, the generated YouTube details (`YouTube Live`,
`YouTube Premiere` and the older stored forms it still recognizes) and the
Atom subtitle and summaries — comes from a small table in
`backend/internal/feeds/locale.go`. Stored data is unchanged: only
rendering picks the language. A non-English Atom feed declares `xml:lang`, has
the feed ID `urn:kano-navi:feed:<lang>` and its self link keeps the `lang`
query.

Both answer `GET` and `HEAD`, never fetch platforms or models, negotiate gzip
like the [public JSON reads](backend.md#text-transfer-and-static-caching), and
send `Cache-Control: public, max-age=600` with a weak ETag that validates both
encodings. The ETag names the language, the build version (so an upgrade's
rewording is not hidden by a cached copy) and a hash of the rendered body.
Every calendar `DTSTAMP` is the start of today's Japan date, so the
calendar changes only with the snapshot, the date and the build; its ETag
also names the dashboard revision. The Atom feed also follows the clock,
since a reservation turns into an aired video when its time passes. A
matching `If-None-Match` answers 304. A database failure answers 503 without
an ETag.
The routes sit under `/api/`, so the development proxy and the
[read-only mode](backend.md#read-only-mode) serve them unchanged.

## MCP Boundary: `/mcp`

`/mcp` uses stateless Streamable HTTP. `dashboard_read`, `schedule_list`,
`posts_list`, `health_read` and `sync_status` are public read-only tools and
do not require a credential. Public `sync_status` lists the recent jobs' kind,
status and times only: it carries no job ID and takes no `jobId`. Control
tools are only registered for requests carrying
`Authorization: Bearer <MCP_CONTROL_TOKEN>`: they can advance a dashboard
revision, start an asynchronous sync, or run the automatic schedule scan. For
those requests `sync_status` also reports each job's `id` and looks a job up
by `jobId`, so a started job can be followed. The token is independent of
`ADMIN_PASSWORD` and browser sessions.

A JSON-RPC batch holds at most 100 messages and runs at most 10 tool calls,
since each can read a whole snapshot. Later `tools/call` requests in the same
batch are answered with the tool error `Too many tool calls in one batch (at
most 10)` without running; other requests in the batch are answered as usual.

The endpoint deliberately has no tool for manual event confirmation, editing,
deletion, bulk approval, profile-media selection/upload, SQL, file paths, DOM
commands, or arbitrary URL proxying. Human confirmation remains a human-only
workflow at `/admin`.

## YouTube WebSub Callback: `/api/websub/youtube`

The route is active only while YouTube push is enabled in `/admin`
(`/api/admin/sources/youtube/push`); otherwise it answers like an unknown API
path. It answers Google's hub verification for this channel's topic and
accepts signed Atom notifications, which only wake a queued YouTube sync. See
[Sources](sources.md#websub-push) for the settings, verification, signature,
debounce and queue rules.

## Related Docs

- [Backend](backend.md)
- [Admin API](admin-api.md)
- [Data model](data-model.md)
- [Media cache](media-cache.md)
- [Deployment security](../operations/security.md)
