# Frontend

## Presentation: `src/`

`src/main.tsx` routes between the public board, the static `/history`
page in `src/history/`, the static `/lineblog` archive in `src/lineblog/`, the
static `/about` page in `src/about/`, and `/admin`.
Links between the four public pages open in place: the next page's code
loads while the current page stays on screen (hovering or focusing a link
starts that load), then the pages cross-fade under a fixed header with the
View Transition API. Back and forward restore each entry's scroll position,
including after a reload and for the entries the browser adds for fragment
links within a page; a remembered position wins over the address's fragment.
`/admin`, other paths, modified clicks and links
with a target load normally; back and forward within `/admin` or another path
stay with that page, which keeps the browser's own scroll restoration and
handles its fragment moves (the console's views), and `index.html` applies the saved theme and
language before the first paint so no load flashes the other theme. History copy and image metadata are
static; verified fixed images ship in `public/assets/history/` and `dist/`.
`scripts/package-history-media.ts` updates these files from a local reference
archive without network requests.
See [History archive maintenance](../development/history-archive.md). The
`/lineblog` article index and per-article files are static JSON written by
`scripts/package-lineblog.ts`, and each article's archived comments are
static JSON written by `scripts/package-lineblog-comments.ts`; the page picks
an article, a month or a page from its query (`?article=`, `?month=`,
`?page=`) and moves between them in place, pushing an entry whose scroll the
shared navigation restores. Each article file and comments file is its own
build chunk: the page's code waits for the articles its address shows (and a
single article's comments), a move keeps the current view on screen until
the next view's files have loaded (pointing at or focusing a link starts
that load), and a failed article load offers a reload. Windows up to 600
pixels wide render the blog's smartphone template and wider ones its PC
layout, switching on a media query; see
[Smartphone Template](../development/lineblog-archive.md#smartphone-template). Its
media load from `/media/lineblog/<name>`, including the header and profile
art of the design of the newest article shown; see
[LINE BLOG archive maintenance](../development/lineblog-archive.md). The four
public pages share `src/site/`: `site-shell.tsx` (header with page navigation and
Japan clock, footer, skip link, section title, empty and loading states;
narrow screens show the site name and fold the page links into a menu behind
the `kano-seal.tsx` deer seal),
`media-dialog.tsx` (the native modal image viewer: its close button stays in
the screen's top-right corner, and a click anywhere outside the image, its
arrows and its details closes it; a set takes the viewer's full height and
centres each image in it, so its arrows stay in place across image sizes,
and shows the details panel for every image once any image has details; an
image's small-print note closes the panel, or the viewer when there is none;
every control that opens it carries `media-opener`, which shows the zoom
cursor), `links.tsx` (external
links and platform marks), and `site.css` (shell, controls, tone classes and
the dialog). A `pill-button is-collapsible` shows only its icon at 560px and
below, so it keeps its label as `aria-label`. The board lives in
`src/dashboard/`: `use-dashboard.ts` owns the snapshot request and the
revision stream, `format.ts` owns Japan-time and event-status helpers, `content.ts` holds static public links, and
`components/` renders the next-up stage and profile card, the week guide,
the post wall, the video shelf, and the link directory, plus
`archive-dialog.tsx`, the native modal sheet that browses one month of the
schedule, posts or videos. `video-groups.ts` sorts the shelf's videos from
the local snapshot: broadcasts (`LIVE ARCHIVE`, `UPCOMING LIVE`, `GAMEPLAY`,
`TALK`) are streams and every other kind a work; a video's phase follows the
stage's live rule (a fresh confirmed state first, then the stream's
three-hour grace or an explicit end), so a reservation past its start is a
started ticket rather than an archive and an operator-confirmed cancellation
leaves the shelf once its start passes. A past video whose `isShort` is
`true` is a Short before its kind is read, so a Short titled like a stream is
neither an archive nor a work; `null` (unknown) is an ordinary upload. The
lead is the featured video, or else the newest `MUSIC` work or premiere, or
else the newest work, so a Short leads only when featured; each video appears
once. The filter offers All plus Streams, Works and Shorts for each kind
present, and only when two are. Shorts render as 9:16 cards cropped from the
centre of the 1280×720 cover (the vertical frame between YouTube's blurred
pillarbox): one row of up to six in the All view, fewer as columns narrow and
a swipeable rail on phones, and up to twelve in the Shorts view. The shelf
shows the length from `durationSeconds` when known. `videoLinks` in
`src/dashboard/video-groups.ts` orders a video's platform links (the video,
then each of its `alternates`); the first is the link its card, ticket or row
follows. For `zh-CN` a past video with a bilibili alternate puts bilibili
first; reservations, started and live videos keep their own platform. With
two or more links, the shelf cards, rows, tickets and the stage's latest
video show `PlatformLinks` in that order and an `aria-hidden`
`PlatformBadge` on the cover (`src/dashboard/components/platform-links.tsx`). Its styles are in
`src/dashboard/board.css` on top of `src/site/site.css` and the shared tokens
in `src/index.css`. The hidden `/admin` console
lives in `src/admin/` and is organized around operator tasks: `index.tsx`
registers the views (Overview, Review queue, Schedule, Posts, Videos, Profile
images, Runs, Sources, Detection, LLM) and renders the shell
with hash routing (`#view/tab`; a `#content/<tab>` link opens the matching
view and a `#models` link opens LLM); `use-admin-data.ts` owns every
[Admin API](admin-api.md) call, the session and the overview poll that tracks
the active run while the console is visible; `api.ts` is the transport and error mapping; `types.ts` holds
the wire shapes; `ui.tsx` the console primitives (buttons, panels, native
modal dialog, toasts); `views/` one file per section; and `admin.css` styles
them with `adm-` prefixed selectors. `src/components/ui/` contains
basic UI primitives, while the CSS files contain layout and design tokens. The
presentation layer must not open SQLite or call X, YouTube, OpenAI,
or a third-party proxy directly. The root app checks the same-origin
`/api/visitor-region` response once per browser session; when the operator has
enabled the setting and it reports mainland China, a dismissible Toast offers
the optional mainland China site.

## Request and UI Ownership

Keep app composition in src/main.tsx, public snapshot requests in
src/dashboard/use-dashboard.ts, and admin requests and polling in
src/admin/use-admin-data.ts. Reuse the existing state and request patterns.
Shared primitives must remain independent of dashboard, history, and admin
features. Extract cohesive helpers when needed; avoid a repository-wide move
for a small change.

Every page, the board included, loads through a dynamic import with its own
CSS and translation chunks, so a direct visit downloads the shared runtime
(React, the settings provider and the base dictionary) and that page's code
only. The first page's import starts as soon as `src/main.tsx` runs. Because
the server answers every page with the same `index.html`, the board's chunks
cannot be listed there statically: the build
(`scripts/vite-page-preload.ts`) writes a small script ahead of the entry
that adds `modulepreload` and style `preload` links for them only when the
address is the board, so they download alongside the entry. The shared
settings provider owns persistent theme, interface language (`kano-locale`)
and content language (`kano-content-locale`, a locale or `interface` to
follow the interface); page message providers
extend its translator without duplicating preferences. The base dictionary
holds the copy shared by pages (header, footer, navigation, page status and
the translation toggle); the board's own copy is in
`src/dashboard/dashboard-i18n.ts`. While a page's code loads, the current
page stays on screen, or on a direct visit a localized loading status
appears after a short delay; a page error boundary offers a full-page
reload when a chunk fails (including after a deployment). Direct visits and
trailing-slash routes work through the existing SPA fallback, and
`/index.html`, which the server answers as a file, is the board
(`src/lib/page-path.ts`). Any other path
shows a localized not-found view with a link back to the board instead of the
board itself; the server answers it with status 404, so `src/main.tsx` and
`spaPages` in `backend/internal/httpapi/static.go` list the same pages.
History, the LINE BLOG archive and About
remain static and never mount the dashboard hook or request its API.

English is the base locale. Every message exists in English, a key missing
from Japanese or Simplified Chinese falls back to it, and a browser language
that is none of the three selects it. Stored records are not interface copy:
event status codes and the generated YouTube details become labels through
`src/lib/event-text.ts`, which also reads the stored legacy Chinese values,
while other details and operator notes show as stored (see
[Stored event text](data-model.md#stored-event-text)).

A post that quotes another status (`post.quote`, see
[Quoted posts](data-model.md#quoted-posts)) shows it nested in the post note,
below the post's own wording and images and above the engagement counts: an
indented card on the quieter sunken surface with "display name @handle ·
time", the quoted wording clamped to four lines and up to three thumbnails
("+N" counts the rest). The card follows the quoted post's x.com link from
anywhere except its thumbnails, which open the image viewer with the quote's
images. Read more also appears when the clamp hides quoted wording; the full
post dialog and the archive show the whole quote with larger thumbnails. An
unavailable quote shows a localized "Quoted post unavailable" placeholder
linking to x.com. Quoted wording is shown as written, without translations.
A quote an operator hid is absent from the snapshot, so the board shows the
post without it.

The console's schedule (`src/admin/views/schedule.tsx`) has a list and a
calendar view; the browser remembers the last one in local storage. Both
share a search field and status tabs for the review tasks: All, Not
confirmed, Suspected (cancellation) and Deleted. The list groups entries
under one heading per Japan date, with the day's count and a button that
opens a new entry on that date; Upcoming, Past and All dates tabs set the
date range beside it. Each row (`schedule-parts.tsx`) shows the time in
Japan (or All day), the title, which opens the entry dialog, and its badges;
an unconfirmed entry has a labelled Confirm button before the icon actions.
The calendar (`schedule-calendar.tsx`) shows a Monday-first month grid
without merged duplicates. Each entry is a chip with its time and title,
styled by review state as the legend below names; a chip opens the entry,
and a day opens its rows below the grid with the same actions and its new
entry button. On a narrow grid the chips become dots. The entry dialog is
either timed or all day. A timed entry needs a start time; its length
defaults to the [calendar feed's](public-api.md#public-feeds) length for the
type without saving an end, so the board keeps treating a stream as
open-ended, while presets and a custom end save `endsAt`. A preview line
renders the entry as calendar subscribers will see it.

The console's video list (`src/admin/views/videos.tsx`) puts the featured
video and the Bilibili merge suggestions above a list with a search field
and task tabs: All, Upcoming, Bilibili and Missing translation. Uploads
merged into another video appear on that video's row, so only the Bilibili
tab lists them on their own. Each row shows the
thumbnail, the title, which opens the correction dialog, the platform, time,
kind, length and format, its badges and merge line, the translation chips,
and the feature, correct and open actions. The correction dialog
(`video-editor.tsx`) previews the chosen thumbnail beside the facts and
fields and keeps refresh, cancel and save in its footer.

The console's Sources page (`src/admin/views/sources.tsx`) has a row of
tabs for X, YouTube and Bilibili, each naming whether its timer runs here,
and three parts for the selected source. On the left are its timer switch
and its [method chain](workflows.md#method-chains): every method's switch and
up/down moves, which save at once, a method the process cannot run greyed
with the variable it needs, and Bilibili's one fixed method read-only. A
source that `SKIP_*` turns off is locked. Its settings sit beside the chain:
the timer panel (next and last run, Run now, the interval and the sync
setting), for X the X API panel (gate, daily cap, today's usage and each
account's timeline failure streak), and for YouTube the progressive
confirmation (push, the live-status probe and the read-only
[follow-up checks](sources.md#follow-up-checks-and-refresh-requests)). The
manual fetch sits below the settings: method, mode, count or dates, options,
then the newest fetch's summary; Bilibili shows its single-video form
there. On a narrower screen the chain moves above the settings, and on a
phone the tabs share the width and show only the source names.

The console's post list (`src/admin/views/posts.tsx`) shows a post's quote
read-only in its own cell under the wording, indented on the sunken surface
so its thumbnails never read as the post's own images: author, time, the
wording clamped to three lines, ready quote thumbnails that open the image
viewer, or the unavailable state. Badges mark a quote hidden from the board
and a queued refresh. Its actions hide the quote from the board or show it
again, queue a refresh from the source (disabled while one is queued), and
open the quoted post; nothing edits the quote itself. On narrow containers
the quote cell spans the row.

Post text and video titles come with server-resolved `translations` (see
[Translations](data-model.md#translations)). `src/components/translated-text.tsx`
chooses what a reader sees from the content language, not the interface
locale: English and Chinese content gets that locale's translation by default
and Japanese content always gets the source. The header's language menu
(`src/components/preference-controls.tsx`) sets both; the content language
defaults to "Same as interface", and the operator console shows only the
interface choice. The LINE BLOG paragraph translations and the schedule
month grid follow the same content locale, while feed links, history copy
and the bilibili-first rule follow the interface locale. Block
text such as a post uses `TranslatedText`, which labels the origin (AI,
platform or editor) and offers a Show original button. A card title uses
`TranslationToggle`, a compact switch that reads "AI" (or "Translated" for
a platform's or editor's wording) and swaps in the source; its tooltip and
accessible name spell out the origin, and the tooltip holds the source. Video and schedule titles
go through `src/dashboard/components/translated-title.tsx`: `TranslatedTitle`
for a card title (spotlight tickets, shelf tickets, cards and rows, archive
rows, week-guide entries) and `TranslatedHeading`, with the full note, for
the shelf's lead card. A button cannot go inside a link, so these cards are
not links themselves: the title is the link (`.card-link`), and
`followCardLink` in `src/site/links.tsx` opens it from anywhere else on the
card. A clamped title clamps only its wording, so the switch stays visible.
Month-grid chips are too small for a switch and show the reader's wording
unmarked. An operator's
Featured title from the focus record shows as written. The wording shown
carries its `lang` attribute. The console's
completeness chips and translation editor are in
`src/admin/views/translation-parts.tsx`, shared by posts and videos.

Board images from the media cache, LINE BLOG archive images and the fixed
history images load through `srcset`: `src/lib/media.ts` lists the server's
[resized copies](media-cache.md#resized-copies) below an image's known width,
and each card or thumbnail passes a `sizes` hint for its layout. The
full-image viewers, profile media and other bundled assets keep their single
URL.
The schedule header's Subscribe menu (`calendar-subscribe.tsx`) offers the
[calendar feed](public-api.md#public-feeds) as a `webcal:` link and a copyable
`https` address built by `src/lib/feeds.ts` from the page's origin. Both carry
the reader's language as `?lang=ja` or `?lang=zh-CN`; English keeps the bare
path. The settings provider points `index.html`'s Atom and calendar discovery
links at the same language whenever the locale changes.

SQLite stores one text per seeded timeline and link row. The board translates
that text through src/dashboard/seed-copy.ts, keyed by the exact seed wording
in static-seed.json, so a row whose text was changed shows as stored. Update
both files together; the frontend tests require English and Japanese copy for
every seed row.

The dashboard hook coordinates one current request. Snapshot refreshes share an
in-flight promise; a manual refresh cancels an older revision read, and stale
completions cannot install state. A 15-second deadline covers headers and JSON
consumption.

While the page is visible the hook keeps an `EventSource` on the
[revision stream](public-api.md#revision-stream). A pushed revision that
differs from the snapshot's reads the snapshot after a random delay of up to
three seconds, so the boards that hear one change do not all read at once; an
unchanged one refreshes a stale calendar. A change pushed while a snapshot
read is in flight is compared once that read ends. Between changes a
completion-scheduled timer only revalidates the snapshot once a minute, since
time-derived fields such as `liveStatus` expire without a revision change.
EventSource reconnects by itself after a drop, and the reconnected stream
resends the current revision. A refused stream, or three errors without a
message between them, hands over to revision polling every 30 seconds; the
stream is tried again after five minutes of polling. Failed requests back off
through 8/16/32/60 seconds. Failed first loads and failed reloads retry
snapshots, preserving a previously loaded snapshot. A hidden page closes the
stream and pauses its timer; becoming visible reopens the stream, whose first
message is the current revision, and visibility/focus events coalesce into a
prompt check while respecting failure backoff and a one-second minimum gap.
Unmount closes the stream, aborts requests and removes deadlines, timers and
wake listeners. The 30-second local time derivation below runs separately.

## Time-dependent Snapshot State

The overall state, shown by the header dot and the profile card, is the
latest completed run in `meta.lastSync`: its outcome and its finish time (its
start time when it has none). An API failure takes priority, then a run that
is neither `success` nor `partial` (needs review); a `partial` run is partial
and a run older than 24 hours is out of date. Below it, the profile card lists
the last successful X, YouTube and bilibili reads from `meta.sourceFetchedAt`,
each marked out of date after 24 hours; these source times do not change the
overall state. The local clock updates both warnings without a request.
Missing timestamps, and ones more than five minutes ahead of the local clock
(a visitor clock slightly behind the server still shows a fresh read), remain
waiting. This is a display threshold, not a synchronization timer.

The board rederives event/video availability, counts, and focus candidates from
its local snapshot every 30 seconds and when the page regains visibility/focus,
even when `dashboard_revision` is unchanged. Expired activities yield to later
events; date-only entries use the current Asia/Tokyo date and never acquire a
synthetic start time. A fresh probe-confirmed `liveStatus` decides first:
`live` stays live until a known end, while `ended` and `upcoming` are never
on air, and the On air badge and the schedule's On air status use the same
rule, so the week guide never shows a stream as ended while the stage shows
it on air; a confirmed `upcoming` waiting room past its reserved time stays
upcoming there. Without one, streams and
memberships retain a three-hour live grace period, or their explicit end time; other activities, including YouTube
premieres, count as live only before an explicit end. A started candidate
without an explicit end or a confirmed live state is only presumed live: in `nextEvent`, `nextStream` and
`pickSpotlight` it yields to a later timed candidate that has started or starts
within 30 minutes, while a candidate with an explicit end keeps its place until
that end. Events and videos for the same YouTube video, matched by the identity
below, form one spotlight candidate. A video linked to an event inherits its explicit end time for
both `nextStream` and `pickSpotlight`. YouTube watch, youtu.be, live, and shorts
links match by the video identity parsed locally from their public URL, ignoring
share/query parameters and supported YouTube subdomain differences. Other links
retain exact URL matching; different videos and lookalike hosts do not match.
`pickSpotlight` gives a candidate with artwork the full ticket and one without
it the compact layout; `pickCompanion` then adds the latest video beneath a
compact ticket unless it is the same link. Both read `summary.latestVideo`,
which `deriveDashboardAt` recomputes as the newest non-upcoming video that is
not a Short (`isShort === true`), unless it is the featured video
(`featuredVideoIdOf`: `meta.featuredVideoId`, or the focus video of a
snapshot without that field). An unknown format stays eligible, and when every recent upload is a
Short the stage shows no latest video. The play mark appears only on
YouTube or Twitch links; X links read as an announcement.
Matching needs no protected source item IDs or additional public API fields.
At the end instant the video yields to later focus candidates. This derived video
`endsAt` belongs to local snapshot state. Without an explicit end, a stream's
three-hour grace ends exactly three hours after the start. Confirmed cancellation flags prevent local
rederivation from restoring a cancelled reservation. Selected-media startup
repairs follow the [durable revision contract](data-model.md#event-precedence),
so the board's revision follow-up reloads a changed avatar URL without
increasing full-snapshot request frequency. This requires no source
synchronization or external request.

## Installable App

`index.html` links `public/manifest.webmanifest` (name "What’s Kano Up To?",
short name "Kano Navi", `start_url` and `scope` `/`, `display: standalone`,
background and theme colour `#fdf9f6`, the light `--background` token) and
the icons in `public/assets/icons/`: a 32 px favicon, 192 and 512 px icons, a
512 px maskable icon with the avatar inside the 80% safe zone on the
avatar's own pink, and a 180 px `apple-touch-icon`. They are resized copies of
the bundled `public/assets/kano-avatar.jpg` and follow its provenance; the
theme-color metas switch between the light and dark backgrounds.
There is no service worker or offline mode: an installed app loads the same
pages, which read the live local API. The Go static server serves
`.webmanifest` as `application/manifest+json` and, like other unhashed public
files, revalidates the manifest and icons on each use, so a replaced icon
takes effect without renaming it.

## Provider Company Icons

The provider presets are OpenAI, Anthropic, DeepSeek, OpenRouter, Gemini, and
Custom. Company marks are bundled local SVGs from Lobe Icons 1.90.0 (MIT);
they never require a browser CDN request. The license ships in
`public/assets/providers/LICENSE.txt`. Configured providers use the API host,
name, or stable ID to identify a mark; unknown custom providers retain their
initial. Removing a preset does not delete saved providers.

The LLM view shows the schedule and translation queues side by side, then the
providers as a list beside the selected provider's detail. A queue change
(add from the model picker, drag or move, remove) shows at once and is saved
in order: each queue has at most one save in flight, and edits made meanwhile
collapse into the latest order, sent next. A refused save returns the queue to
its last stored order and drops the edits that followed it. Each queue marks
the model each stage calls first. The provider dialog holds only the connection, with the presets above
it; there is no default model. Saving a new provider with a key selects it and
lists its models, an explicit operator action. The detail adds ticked models
to the catalog, edits their tags, turns them on or off, puts them in either
queue and tests them one at a time.

List filters in the console are one row of pills, one per filter, with a
date range pill and a clear button that appears while a filter narrows the
list. Run details explain stored error codes in words and keep the code
beside them.

## Related Docs

- [Design and interaction](../development/design.md)
- [Testing](../development/testing.md)
- [User guide](../user/index.md)
- [History archive maintenance](../development/history-archive.md)
- [LINE BLOG archive maintenance](../development/lineblog-archive.md)

## Polling and Calendar Recovery

Calendar and admin requests use the shared lifecycle/deadline helper with a
12-second deadline including body consumption. The public snapshot coordinator
uses the 15-second deadline and refresh deduplication described above. All requests
are cancelled on unmount and reject superseded responses. Automatic polls wait for an active request rather
than starting overlapping work. Their timer is scheduled after settlement,
including failure, with exponential backoff capped at 60 seconds. Public polls
pause while hidden, and so does the admin overview poll, which reads at once
when the console becomes visible again. A failed initial snapshot retries even
before any revision has been recorded; manual retry is available. Admin 401
stops polling and requires login. Failed initial admin loads also retry.

The board's calendar shows one week; its selected week lives in
`use-dashboard.ts`. It reads
one bounded page, shows the complete range/day counts, and offers more-event
pagination. Changing the range cancels old work and begins at page one; errors keep a retry control.
Every same-range refresh rereads the loaded page range and replaces it atomically,
even at the same revision: time-based public fields such as a probe's trusted
`liveStatus` can expire without a stored write. A refresh stops
at the new last page if the range shrank. A change while reading that range restarts
from page one, with at most three attempts under the shared request deadline.
Failed or repeatedly changing reads retain the complete known generation for retry;
they never append pages from different revisions. Every successful full board
snapshot, including its visible minute refresh and manual reload, refreshes the
calendar. Active calendar work finishes first, and later revision checks,
pushed or polled, consume any pending time refresh or calendar revision.
The calendar snapshot and its errors are tagged with their range. Only the
first read shows the loading placeholder. A week change keeps the last loaded
week rendered with its own dates, dimmed and inert after a short delay, and
swaps the whole body (days, count and schedule image) once the selected week
arrives; a failed read reports its error only while its week is selected. A
refresh of the shown week keeps its days or empty-week note in place and swaps
the data when it arrives, so a quiet week does not flicker on each refresh. On
wider screens the schedule-image thumbnail keeps its slot when a week has no
image.
Adjacent-date hints support jumping to stored activity in either direction.

Snapshot, calendar and archive reads use `cache: "no-cache"`: the browser
revalidates its stored copy with the response's
[ETag](public-api.md#public-snapshot-contract) and receives a bodiless 304 when
nothing changed, so the minute refresh, and an open dialog's reread after it,
cost little when the board is quiet.

The month dialogs read through `useArchive` in `use-dashboard.ts`, only
while one is open: the schedule reads the Monday-first weeks covering the month
from `/api/calendar`, and past posts and videos read the month's own days from
[`/api/posts` and `/api/videos`](public-api.md#bounded-public-archives). They
follow the same rules as the calendar: pages of one revision are appended, a
later page from another revision rereads the loaded pages atomically (up to
three attempts under the 12-second deadline), and a board revision change
rereads them while the dialog stays open. Schedule and video archives also reread
their loaded pages after each successful full board snapshot, even at the same
revision, so expired live states disappear there too. A snapshot arriving during
an archive read or pagination waits for that work to settle before refreshing;
a failed refresh keeps the complete known prefix and retries on the next board
snapshot. Post archives follow stored revisions. These refreshes
share the board's visibility pause and snapshot backoff rather than starting a
separate archive timer. Changing the month or the post
account, or closing the dialog, cancels the read and discards late pages.
A click anywhere in a month-grid day (outside an entry link) picks it, listing
its entries beside the grid (below it on phones) until the hide button clears
the selection and the grid takes the full width; the panel can show that week
on the board. On phones entries are dots that ignore taps, so a tap always
picks the day. The post archive lays out full
posts either as a masonry wall or as a single column, a choice kept in
`localStorage` (`kano-post-archive-layout`). The masonry measures each post
and places it, in order, under the column that ends highest
(`masonryLayout` in `src/dashboard/post-rows.ts`), so it reads row by row
while the cells stay in post order for keyboard and screen readers; it
re-measures when the wall or a post resizes. A container query on the dialog
head hides the layout switch while the wall is narrower than two masonry
columns, where both layouts are one column. An image viewer opened from the
post archive stacks above it; Escape closes the topmost dialog and focus
returns to its opener.
Images match their applicable inclusive period, without an updated-at fallback.
The bounded focus snapshot refreshes at least once per minute while visible to
advance its shortlist as time passes, even when the durable revision is unchanged.
