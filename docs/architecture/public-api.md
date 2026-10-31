# Public API

Public reads, feeds, media and MCP read a prepared SQLite snapshot. None of
them fetches X, YouTube or a model provider. The service that hosts them is
described in [Backend](backend.md).

## Public Snapshot Contract

`GET /api/health` returns service status, the current ISO timestamp, and a
whitelisted synchronization summary; it does not return a database path or
label. `GET /api/dashboard?days=3` clamps days to 1 through 30 and returns
profile, summary, posts, events, videos, focus, timeline, resources, schedule
assets, media status, and metadata. Posts are the newest 100 published within
the window plus, however old, the eight newest of each account; MCP
`dashboard_read` and `posts_list` read the same list. `counts.posts` counts
the window only.
The summary contains `nextEvent`, `nextStream`, `latestVideo`, `latestPost`,
and counts; `latestPost` survives an empty requested window. `latestVideo` is
the stage's fallback: the newest non-upcoming video that is not a Short,
unless the operator featured that Short; a video of unknown format stays
eligible. Metadata includes configured X accounts and the Featured video ID.

`meta.sourceFetchedAt` contains only `x`, `youtube` and `bilibili` ISO
timestamps or null, derived from successful incremental source reads, the head
pages of manual timeline fetches and the Bilibili account's last successful
read. Other manual fetches do not refresh it; a stored recent-window
checkpoint counts up to its window's end time. Timeline `count` and `all`
fetches retain their successful head-page read time, so resuming old pages
does not refresh the source; a timeline checkpoint without that time leaves
the source time unknown. Media-only runs,
cursor cleanup and API reads do not advance these source timestamps. Internal
account IDs, checkpoints and fetch errors remain protected.

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

`/api/dashboard`, `/api/calendar`, `/api/posts` and `/api/videos` send
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
`/media/profile/banner`, and the LINE BLOG archive's files use
[`/media/lineblog/<name>`](media-cache.md#line-blog-archive-media).

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
`dashboard_read` shares the bounded snapshot. Events merged into another event
are excluded from every one of these reads, including totals, day counts,
adjacent dates and summary candidates (see
[Duplicate merging](data-model.md#duplicate-merging)).

Dashboard SQL selects up to 100 recent post previews plus the latest-post
fallback, up to 100 current-week event previews plus 30 upcoming and 30 live
focus candidates and at most one linked event per returned video, and bounded
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
handle. An invalid range returns 400 `invalid_range` and an invalid handle
400 `invalid_account`. Reads revalidate through their
[ETags](#public-snapshot-contract), negotiate gzip like the other public JSON
reads and never fetch platforms. The board's month dialogs use them with the
calendar.

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
an hour otherwise, since RFC 5545 would make it zero-length. A UID is the
public event ID with `@kano-navi`. `LAST-MODIFIED` is the event's stored
`updated_at`, and `SEQUENCE` is that time in whole seconds since 2020-01-01
UTC, so it grows whenever the event is written and clients replace a changed
entry; an event without a parseable update time has neither. `STATUS` is
`CANCELLED` only for a cancelled status or an operator's confirmation
(`manual_confirmed`), `TENTATIVE` for a suspected cancellation or a
`tentative` status, and `CONFIRMED` otherwise. The description carries the
event's detail, its provenance and its public link; only absolute HTTP(S)
links are written. Text is escaped and lines are folded at 75 octets without
splitting a character.

`GET /api/feed.xml` is an Atom 1.0 feed of at most 50 YouTube videos and
reservations dated 30 days before to 11 days after today, newest first. An
entry is dated by its publication (a future time is dated at the start of
today's Japan day until it passes, so the entry does not move between polls),
says when a reservation is scheduled, and links the video and any
merged platform copy. Entry and feed IDs are `urn:kano-navi:` URNs. The
server never builds links from the request's `Host`: with `PUBLIC_ORIGIN` the
feed links the site absolutely and adds a `rel="self"` link to
`/api/feed.xml`; without it the site link stays `/` and there is no self link.

Both take an optional `lang` query, `en`, `ja` or `zh-CN`; a missing or
unknown value is the English base, so the bare paths stay English. Titles
resolve [translations](data-model.md#translations) the way the board does:
Japanese readers get the source wording, and English and Chinese readers get
the locale's translation when one exists (an event only through its linked
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
`posts_list`, and `health_read` are public read-only tools and do not require a
credential. Control tools are only registered for requests carrying
`Authorization: Bearer <MCP_CONTROL_TOKEN>`: they can advance a dashboard
revision, start or inspect an asynchronous sync, or run the automatic schedule
scan. The token is independent of `ADMIN_PASSWORD` and browser sessions.

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
