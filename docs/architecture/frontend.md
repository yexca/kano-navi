# Frontend

## Presentation: `src/`

`src/main.tsx` routes between the public board, the static `/journey`
page in `src/history/` (an address of `/history` is replaced with `/journey`
before the first page loads), the static `/works` discography in `src/works/`, the
static `/lineblog` archive in `src/lineblog/`, the static `/about` page in
`src/about/`, and `/admin`.
Links between the five public pages open in place: the next page's code
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
static; the images are served from `data/journey/` (`/media/journey/<name>`),
and release milestones show the `/works` covers of the release they name.
`scripts/package-history-media.ts` packages the images from a local reference
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
[LINE BLOG archive maintenance](../development/lineblog-archive.md). The
`/works` tables are static JSON written by `scripts/package-works.ts`. The
page keeps its section in the `?view=` query, replacing the current entry
when a visitor switches sections, and `?release=<id>` opens that release's
sheet once and is then dropped from the address; the personal works build with the page,
and the participation and upload sections are their own chunks that load
the first time they show, with a retry on failure. The side the trilogy
scene shows is kept in `localStorage` (`kano-works-trilogy-side`). A release opens in a
native modal sheet (`works-sheet.tsx`), and covers open in the shared image
viewer above it. Covers load from `/media/works/<name>`; see
[Works collection maintenance](../development/works-archive.md). The upload
section reads [`/api/works/uploads`](public-api.md#works-uploads) once per
page load (`use-works-uploads.ts`) for the uploads' covers, lengths,
availability, and for the uploads after the collection
date, which `recent-uploads.tsx` shows as a block above the toolbar; until
it answers the art stays blank, and a failed read keeps the packaged list
with printed sleeves and no newer block. The five
public pages share `src/site/`: `site-shell.tsx` (header with page navigation and
Japan clock, footer, skip link, section title, empty and loading states;
narrow screens show the site name and fold the page links into a menu behind
the `kano-seal.tsx` deer seal; the footer is the fan-made and unaffiliated
line, About and Back to top, with the page's disclaimer behind a switch),
`media-dialog.tsx` (the native modal image viewer: its close button stays in
the screen's top-right corner, and a click anywhere outside the image, its
arrows and its details closes it; a set without details takes the
viewer's full height and centres each image in it, so its arrows stay in
place across image sizes; a viewer with details keeps one size for every
image, at most 820px tall, and shows the details panel for every image once
any image has details, and with `scaleUp` (Works and Journey) a smaller
image grows to fill the stage; an
image's small-print note closes the panel, or the viewer when there is none;
every control that opens it carries `media-opener`, which shows the zoom
cursor), `links.tsx` (external
links and platform marks), and `site.css` (shell, controls, tone classes and
the dialog). A `pill-button is-collapsible` shows only its icon at 560px and
below, so it keeps its label as `aria-label`. The board lives in
`src/dashboard/`: `use-dashboard.ts` owns the snapshot request and the
revision stream, `format.ts` owns Japan-time and event-status helpers, `content.ts` holds static public links, and
`components/` renders the next-up stage and profile card, the on-sale strip
(`sale-strip.tsx`: up to three IDs from `projection.sales`, rendered in API
order; snapshots without that projection use `currentSales`), the week guide,
the post wall, the video shelf, and the link directory, plus
`archive-dialog.tsx`, the native modal sheet that browses one month of the
schedule, posts or videos, and `sheet-dialog.tsx`, the smaller modal sheet
that shows one long post or one schedule entry in full and can open above
the month sheet. `video-groups.ts` consumes the dashboard's
`projection.shelf` and each video's `view`: the API supplies the lead, group,
phase and stable order, while the browser applies the active filter and
renders the cards. A snapshot without the projection uses the legacy local
grouping until the next API response. A video carries two chips. The form
chip (`videoFormLabel`) translates `view.formCode`, and `VideoTagChip`
translates the stored tag with the context in `view.tagContext`; neither
helper reconstructs the phase or form when the projection is present. The
tag chip (`VideoTagChip` in
`src/dashboard/components/event-tag.tsx`) adds the tag with the schedule's
icon, in the schedule's words for a stream (gameplay, talk, singing) and as
the work's content otherwise (gameplay, talk, music, where a note replaces
the singing microphone, as on the archive's Songs button); it appears on every
card, row, ticket and the lead, including stream archives that hide the
form chip. The Next Up ticket names the video's tag, else the entry's
activity label, before the form; a `member` entry adds the members-only chip
(`MembersChip`) beside it unless the video's availability chip already
shows `members`, `private` or `removed`. Schedule entries consume the event
`view`: `eventKinds`, `scheduleEntries` and `eventStatus` translate its type,
activity, calendar entries and state. The sale period's opening and closing
dates come from `view.calendarEntries`, and the status codes are
`upcoming`, `open` and `done`. The canonical enum contract is in the [Public
API](public-api.md#public-snapshot-contract): `event.view.state` has seven
values (`upcoming`, `live`, `open`, `done`, `pending`, `cancelled`,
`cancellation_review`), `video.view.phase` has five (`upcoming`, `live`,
`started`, `past`, `cancelled`), and `formCode` has six non-null values
(`short`, `upcoming`, `stream`, `archive`, `premiere`, `video`). A response
without the view uses the stored fields as a compatibility fallback. The
filter offers All plus Streams, Works and Shorts for each kind
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
video show `PlatformLinks` in that order. Every video cover carries an
`aria-hidden` `PlatformBadge` (`src/dashboard/components/platform-links.tsx`)
of `videoPlatforms`: the links' platforms, led by the video's own platform
when it is private or removed. A `members` video, or a stage ticket's
`member` entry, adds YouTube's members star (`YouTubeMembersMark`, in
YouTube's members green as on the Works page) after the YouTube mark. Such a
cover with no link left fades (`is-closed`). `VideoAvailabilityChip` marks
`members` with the same star in the solid `--leaf` green, as `MembersChip`
does for the schedule's `member` entries, and `private` and `removed`
muted. Its styles are in
`src/dashboard/board.css`, which imports one file per area from
`src/dashboard/styles/` in cascade order, on top of `src/site/site.css` and the shared tokens
in `src/index.css`. The hidden `/admin` console
lives in `src/admin/` and is organized around operator tasks: `index.tsx`
registers the views (Overview, Review queue, Schedule, Posts, Videos, Profile
images, Runs, Queue, Sources, Detection, Site) and renders the shell
with hash routing (`#view/tab`, where the tab is everything after the view
and may hold several parts; a `#content/<tab>` link opens the matching
view, and `#models` and `#llm/<provider>` links open Detection); `use-admin-data.ts` owns every
[Admin API](admin-api.md) call, the session and the overview poll that tracks
the active run while the console is visible; `api.ts` is the transport and error mapping; `types.ts` holds
the wire shapes; `ui.tsx` the console primitives (buttons, panels, native
modal dialog, toasts); `views/` one file per section, with
`views/platform-tabs.tsx` the platform tab row that opens Posts, External
posts and Videos: one icon tab per platform, styled like the Sources tabs,
above a tab panel, the platform kept in the URL, and the arrow keys, Home and
End moving between the tabs; and `admin.css` styles
them with `adm-` prefixed selectors, importing one file per area from
`styles/` in cascade order. `src/components/ui/` contains
basic UI primitives, while the CSS files contain layout and design tokens. The
presentation layer must not open SQLite or call X, YouTube, OpenAI,
or a third-party proxy directly. The optional mainland China site comes from
the same-origin `/api/visitor-region` response, read at most once per page
load through `src/lib/visitor-region.ts`. When the operator has enabled it,
the footer links to the mirror while the interface is Simplified Chinese;
when the response also reports mainland China, the root app shows a
dismissible Toast offering the site, once per browser session. About's credit
for the mainland China server site is static copy in `src/about/` and does
not read this response.

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

A related account's promoted post (`post.promoted`) shows the account's
initial on the sunken surface in place of Kano's portrait, beside its
handle; it appears in the All tab and the archive, never under an own
account's tab.

A post that quotes another status (`post.quote`, see
[Quoted posts](data-model.md#quoted-posts)) shows it nested in the post note,
below the post's own wording and images and above the engagement counts: an
indented card on the quieter sunken surface with "display name @handle ·
time", the quoted wording clamped to four lines and up to three thumbnails
("+N" counts the rest). The card follows the quoted post's link (its x.com
page, or for a Bilibili forward the forwarded dynamic's or upload's Bilibili
page) from anywhere except its thumbnails, which open the image viewer with
the quote's images. A forward's card names the forwarded dynamic's author by
display name after the bilibili mark, never by `@` or mid. Read more also
appears when the clamp hides quoted wording; the full post dialog and the
archive show the whole quote with larger thumbnails. An unavailable quote
shows a localized "Quoted post unavailable" placeholder linking to the
quote's `url`. Quoted wording follows the content language like the
post's own: the quote's `translations` (its stored post's for a self-quote,
otherwise the quoted status's own) show with their own translation switch,
and a quote without one shows its wording as written.
A quote an operator hid is absent from the snapshot, so the board shows the
post without it.

A Bilibili dynamic (`source: "bilibili"`) shows the bilibili mark and name,
linked to the account's space, instead of an `@handle`, and its open link
goes to the opus page. It has no engagement counts. A forward shows the
dynamic it forwards as its quote card, and an empty text (an emoji-only
dynamic, or a forward without wording of its own) leaves only its images or
its quote. The
wall's top line adds bilibili while it holds a dynamic.

An entry follows its first platform link (`eventLink` in
`src/dashboard/format.ts`), or its `url`, or, in a snapshot without
`links`, a stream's `liveUrl ?? url`. `eventLinks` orders an entry's
`links` as `videoLinks` orders a video's: a platform whose `liveStatus` is
`live` first, marked live, then for zh-CN once the entry is done its
bilibili link, else its own platform first; a platform without a link is
left out. An entry shows them with `PlatformLinks` (`platform-links.tsx`,
the live one marked) when it has more than one, or one beside an
announcement of its own (`eventAnnouncement`: its `url` when no platform
link is that page): on its stage ticket, on its week and calendar card,
where the open arrow then opens the announcement, and in its sheet, where
the open button does; the announcement is never labelled as a platform. A
full stage ticket of an entry marks its `platforms` on the art with
`PlatformBadge`. When the stage shows the reserved video of an entry, the
ticket adds the entry's links on platforms the video has no copy on, such
as the bilibili room of a simulcast, marks the video's own link live while
the probe confirms it, and reads live, without a countdown, while any of
its links is. `PlatformLinks` names a link on another platform by its
host. The dashboard's `liveNow` becomes a live stage
ticket, "Live now on bilibili", unless an entry shown on that platform, of
type stream or member and confirmed `live`, starts at S with S−2h ≤
`startedAt` ≤ S+3h, as the server matches broadcasts; the match never
compares links. The
post wall's account filter adds a bilibili button for each of
`meta.bilibiliAccounts`, with the bilibili mark instead of the mid. A snapshot restored from the
browser drops `liveNow` until the first fresh read.

The console's schedule (`src/admin/views/schedule.tsx`) has a calendar and
a list. It opens on the calendar unless the browser remembers the list in
local storage. `#schedule/YYYY-MM-DD` opens the calendar on that day,
`#schedule/look-YYYY-MM-DD` on that day with only what needs a look, and
`#schedule/YYYY-MM` on the first of that month; `#schedule/images` opens
the list on the schedule images.

The calendar (`schedule-calendar.tsx`) works like a phone calendar around a
selected day, today at first. A compact Monday-first month grid sits on the
left, with previous, next and Today buttons above it: each day shows up to
four dots coloured by its entries' states (on the board, uncertain
addition, suspected cancellation, cancelled, hidden by its posts) as the
legend names, and each week row ends in an image mark when a schedule image
covers that week, honey while one waits for review. Choosing a day selects
it and its week, which is tinted; a double click starts a new entry on that
day. Paging months selects the first day, or today in the current month.
Under the grid, held in view, sit the selected week's schedule images
(`schedule-week-image.tsx`); those labelled Not a schedule fold away below
them, where This is a schedule takes them back. Each shows its state,
dates, source link and post, and offers Week earlier and Week later, which
shift its dates by seven days as a manual correction and select that week;
Fit to this week, when its dates are not exactly the week; and the label
decisions. The right side lists the selected week's
seven days, Monday to Sunday with their own previous and next buttons, each
with its rows and new entry button, empty days included, the selected day
marked. Merged duplicates sit under the entry they are merged into, each
with its undo. All and Needs a look (with the month's count) filter the
entries; while Needs a look is on, Next week to look at selects the next
week holding something that needs a look, round to the earliest after the
last, and Review queue (with the queue's count) returns to the queue. The
calendar has no search of its own: searching opens the list on all dates
with the matches. Under 1000px the grid, the images and the days stack.

The server decides which entries need a look and reports it as each
entry's `review` (see [Admin API](admin-api.md#events)): the model suspects
its cancellation, or it is an uncertain addition. Its row's edge takes the
colour, honey or lavender, and a note in the row names the reason; a
suspected cancellation's note shows the model's evidence and confidence with
Confirm cancelled (saving the evidence as the reason) and Not cancelled, and
either note opens the entry's source posts below it. An uncertain addition
is settled with the row's Confirm or delete.

The list shows the entries or the schedule images. Its entries are grouped
under one heading per Japan date, with the day's
count and a button that opens a new entry on that date; Upcoming, Past and
All dates tabs set the date range beside it, and the status tabs All, Not
confirmed, Suspected (cancellation) and Deleted with a search field filter
it. Each row (`schedule-parts.tsx`) shows the time in Japan (or All day), the
title, which opens the entry dialog, its badges and its review note; an
unconfirmed entry has a labelled Confirm button before the icon actions. An
automatic entry whose every source post is hidden carries a Hidden by its
posts badge and is dimmed, since the public pages leave it out.

The list's schedule images are the newest stored ones as decision cards
(`schedule-images.tsx`) under Active, Approved, Undated (with its count) and
Not a schedule tabs. Active leaves out images an operator labelled Not a
schedule, so a rejected card leaves the list; the Not a schedule tab keeps
them, where the label can be cleared. Undated holds those without covered
dates, which the calendar cannot show. A card shows its source post's time and wording, folded to four
lines. Correct dates opens a wide dialog with the image and the whole post
above the form, side by side or stacked on a phone. A card's covered dates
note when they come from the post's events, and a dated card offers Show
its week, which opens the calendar on the day its dates start.

The entry dialog (`event-dialog.tsx`) is either timed or all day. A timed entry needs a start time. Its length
starts at the type default, the [calendar feed's](public-api.md#public-feeds)
length for the type, and saves that end as `endsAt` like the presets and a
custom end. **No end time** saves none, so the board treats a stream as
open-ended; a saved entry without an end opens on it, so an unrelated edit
never adds one. A sale replaces the length with a close date and an optional
close time (`endsOn` and `endsAt`).
A preview line renders the entry as calendar subscribers will see it.
An existing entry's dialog puts its evidence (`event-evidence.tsx`) beside
the form, scrolling on its own and below the form on narrow screens: one
timeline of the posts it was read from, labelled Source, Merged entry or
Cancellation, between the same accounts' nearby posts, which are quieter
and can be folded away; links of other evidence sit above it.

The console's video list (`src/admin/views/videos.tsx`) has platform tabs
(All, YouTube, Bilibili) above the featured video and a list with task tabs
(All, Upcoming, Missing translation and Queued, an open refresh or
translation request), which any platform narrows, and a search field.
Both are in the URL, `#videos/<platform>/<task>`, and either part may be
left out: `#videos/bilibili` opens the Bilibili uploads and `#videos/queued`
the Queued tab on every platform. Uploads merged into another video appear
on that video's row, so only the Bilibili tab lists them on their own. Each row shows the
thumbnail, the title, which opens the correction dialog, the platform, time,
kind (for a Bilibili upload with who settled it, or a honey badge while it
is unsettled and not public), tag, length and format, its badges and merge
line, the translation chips, and the feature (public videos only), correct
and open actions. A merge badge opens the other side's correction dialog:
the YouTube row's badge the upload, the upload's badge its YouTube video.
The correction dialog
(`video-editor.tsx`) carries no explanatory text. Its left column shows the
board's row as the draft leaves it (the chosen thumbnail, title, source, kind,
tag, format and the number of corrections) and when the page was last read,
then below a hairline the corrections: title, kind, tag, format, who can
watch, and the thumbnail files when any are stored, as fixed-size tiles with
a check on the chosen one. Its right column is one soft panel as tall as the
left, holding the platform's record: the merge section, its field and button
on one row, and, below a hairline, what the platform replaced when it
replaced anything, each entry a two-line title with its date. Narrow screens
stack the panel below the corrections. A corrected control is tinted and
offers an icon reset to the platform value beside its label. The footer holds
the refresh as a quiet button, the platform link, cancel and save.

The Review queue (`src/admin/views/review.tsx`) lists only what waits for a
decision, in tabs for the schedule, video merges and posts; a decision
removes the item and the sidebar badge counts again. `#review/images` and
`#review/cancellations` open the schedule tab. The schedule tab
(`review-schedule.tsx`) reads the schedule work by week (see
[Admin API](admin-api.md#schedule-images)) and shows one group per week,
oldest first: its dates, marked when it is this week, its counts of images
to review, suspected cancellations and uncertain additions, and Open in
calendar, which opens `#schedule/look-<Monday>`. The uncertain additions
follow as quiet lines with their reasons, to settle on the calendar beside
the rest of the week; then the pending images' and suspected cancellations'
decision cards. Weeks before last week fold under Earlier weeks, and
undated images form their own group, whose cards set their dates. An image
or a suspected cancellation is a decision
card: its left edge takes the state colour (honey
while it needs a decision, leaf once the image reaches the board, neutral
when rejected), the image or the entry's date sits on the left, and the body
shows the badges and source link, the model's verdict with a confidence bar,
the extracted schedule text when present, and its reason or evidence, the facts,
and a bottom bar with the note field
and the decision buttons. A cancellation needs a reason before it can be
confirmed, and its Source posts button opens the same evidence timeline in
the card. The video merges tab (`merge-parts.tsx`) lists each Bilibili
upload the merge rule merges at its next run or leaves to the operator,
beside its candidates: Merge attaches it to one, and Keep separate keeps it
apart so the rule never merges it; a live replay's candidate is marked as
the archive of the same broadcast. The Bilibili form tab lists the uploads
the model was unsure about or failed on, each with its cover, title, length,
publication time and the broadcast the replay rule read, and Upload and Live
replay buttons that set its form. The posts tab has two labelled filters:
Step, Schedule extraction or Translation, each with the overview's failure
count, and State within the step: Failed or Uncertain for extraction;
translation has only failures and shows no State filter. A new step keeps
the state when the step has it and falls back to Failed otherwise; below
560px each filter takes its own full-width line. Posts queued for a run wait
on the run, not the operator, so the tab lists none of them: a note under
the filters counts the step's queued posts from the overview's `backlog` and
links to them in the post list. Each post is a card whose left edge takes
its state's colour, danger for a failure and honey for an uncertain
extraction: the account, time and post link above the wording, with up to
three of its images beside it (below it under 640px) and its quote under
it, then, past a hairline, a verdict line and the actions. The verdict
names the state (Schedule extraction failed, Extraction uncertain or
Translation failed) with its icon in the edge colour, the error, when the
step last tried, the automatic retry or its stop, and a Queued mark while a
run waits for the post. A failed extraction offers Dismiss beside Process
again; a translation card offers Translate again, which queues a machine
pass. The list pages 50 posts at a time.
Each row has a selection box on its left edge, and shift-click selects the range from the last toggled row. A selection
bar above the list, which sticks under the top bar, selects the whole page
and applies the state's actions to every selected post in one request:
Dismiss and Process again for failed extractions, Process again for
uncertain posts, Translate again for failed translations. A new filter or
page clears the selection.

Console dates are `YYYY-MM-DD` and times `YYYY-MM-DD HH:mm`, in Japan time
and the same in every console language (`src/admin/format.ts`); a stored
day adds its short weekday.

The console's Overview (`src/admin/views/overview.tsx`) opens with the
queue: the active run with its steps and Cancel, or Idle, then the timers in
the order they start, from the overview's `queue`. A due timer shows Due and
whether it waits for the current run; a source timer that is not due has
Start now, or Run next while a run holds the queue. The live-status probe is
listed without a button. While timers are off in the process the panel says
so instead of listing them. Under the timers, Waiting for a run lists the
overview's `backlog`: each kind of operator request that has any (posts to
extract again, X posts and videos to read again, posts and video titles to
translate), with its count and the run that takes it. A row opens the post
list or the video list on those requests; nothing shows while none waits.
Beside Source timers, What runs next opens the Queue page.

The Queue page (`src/admin/views/queue.tsx`) has one tab per step (`#queue/
schedule`, `youtube`, `translate`, `x-refresh`), each read from its
[queue preview](admin-api.md#queue-preview) when the tab opens, on Refresh
and when a run finishes. Schedule extraction lists the posts one run
processes in order, numbered, each with why (requested, new post, input
changed, backoff over) or the skip that takes its place, its route and
source, and Open post, which opens the post list searched for it; counts of
the posts left for later runs, cache hits, posts in backoff with the next
retry and stopped retries follow, and the candidate counts head the tab.
YouTube details lists the detail queue in group order with each run's
watch-page and Data API budget and notes that videos a run's listing turns
up join it unlisted. Translation lists the step's candidates, marking those
past the per-run calls that a run reaches only when earlier ones need no
call. X refreshes lists refresh requests oldest first, each marked for the
next run, a later run, or an account the X step does not read. A step turned
off for the service or with nothing to work with says so above its list.

The console's Sources page (`src/admin/views/sources.tsx`) has a row of
tabs for X, YouTube, Bilibili uploads, Bilibili dynamics and the official
shop, each with the platform's logo (a shopping bag for the shop), a dot (or
a lock) for whether its timer runs here and the state as its tooltip.
The selected source opens with a summary: what it reads, its timer state,
next run, last run and last successful read, the timer switch (which saves
at once) and Run now. While the timer runs, Run now starts it as the
overview queue's Start now does, and reads Run next while a run holds the
queue; while it is off, Run now runs the source's steps once and waits for a
free queue. A source that `SKIP_*` turns off is locked. The Bilibili
tabs' summaries add the login cookie's state and the shared captcha backoff
as chips, with a note on setting `BILIBILI_COOKIE` or `BILIBILI_SESSDATA` while no cookie is
configured or the configured one expired. Below the
summary the main column holds the timed reads
(`src/admin/views/source-timed.tsx`), one form with one Save and
Discard: the interval and, for X and YouTube, the sync setting; and the
[method chain](workflows.md#method-chains), with every method's switch and
up/down moves, a method the process cannot run greyed with the variable it
needs, and the one fixed method of each Bilibili timer and the shop read-only. Save sends only the
changed fields, and the timer switch keeps unsaved edits.

Settings below the timed reads are folded (`src/admin/views/source-parts.tsx`
holds the fold and the question-mark help): a header row with the title,
its state and any quick switch opens onto the settings and figures, and
each fold saves on its own. X adds its X API limits (gate and daily cap,
with the API's place in the chain and today's usage in the header, and
today's usage and each account's timeline failures shown as meters against
them). X related adds the related accounts panel
(`src/admin/views/related-accounts.tsx`): each account's handle and keyword
rules side by side (stacked on a phone), the rules entered as chips in the
shared tag input, where Enter or a comma adds one, with its last read,
stored and promoted counts and last error below, an Add account button that
starts with the default rules, and one Save; a link opens External posts.
The panel and the related manual fetch share the tab's one read of the
accounts, made only while the tab is open. YouTube adds its progressive
confirmation (`src/admin/views/source-live.tsx`, with the Bilibili live room
fold), where push and the
live-status probe are folds with their switch in the header (the probe's fold
also lists its active videos, each with Stop checking, and its stopped
videos, each with Check again), followed by
the read-only
[follow-up checks](sources.md#follow-up-checks-and-refresh-requests), and
yt-dlp, with its status in the header. Bilibili uploads adds the Bilibili
live room fold, the live-status probe's Bilibili part: the room's state and
the check's switch in the header, its interval, next check and the stream
entries it follows, then the room's last check.
The shop tab (`src/admin/views/shop-parts.tsx`) adds the shop read's state:
its sale entries, last read and error, whether it runs here and the wait a
429 answer set.
X and YouTube fold their timed-read
limits in the main column and the manual-fetch limits, which they share,
under the manual fetch, one field per row with its label beside the input.
Both limit folds edit the page's one read of the settings. Longer
explanations sit in a dialog behind a question mark beside a title or
label: the chain, the X API limits, YouTube's per-run limit, its
progressive confirmation and each of its steps, and yt-dlp.

The manual fetch (`src/admin/views/source-fetch.tsx`) sits beside the main
column: method, for X every own
account or one of them, mode, count or dates, options, then the newest
fetch's summary. The method and mode pickers show only when there is more
than one to pick, and only YouTube's methods and the date windows carry a
hint. The X related tab's fetch picks one related account,
shows its last read, and offers schedule extraction and translation; its
summary counts the statuses listed, promoted and demoted. The Bilibili tabs'
fetch form is disabled while the captcha backoff lasts, with its end. The
shop's is one button that reads the whole catalog, disabled while a 429's
wait lasts, then the newest read's listed products, dated products of Kano,
sale periods and pages. Below the fetch, YouTube adds one video by ID or
link and the Bilibili tabs one upload by BV ID or link or one dynamic by its
link, each disabled while the source does not run here. On a narrower screen the side column follows
the main one, and on a phone the tabs share the width and show only the
source names, and a fold's header controls move under its title.

External posts (`src/admin/views/external-posts.tsx`, in the Data group
after Posts) has a platform tab per status pool, X and Bilibili, with no
All tab since the pools take different actions (`#external/bilibili`). The
X tab lists the
X status pool, without Kano's own accounts' statuses, in the post list's
feed layout:
author, name and Japan time; the wording with its image count, links to the
own posts that quote it (opening Posts searched for the post), the accounts
that reposted it, a link to a promoted status's post in Posts and a locked
decision's reason; the translation summary of a status that has no post;
badges for its state (On the board, Demoted, Stored only), a lock
and its reasons; and icon actions to promote a related account's status,
demote a promoted one, return a locked one to the keyword rules and open it
on X. Promote and demote open a dialog that shows the wording and requires
a reason. Filters cover the text or ID, the author (the related accounts),
the reason and the state; a header link opens the related accounts on
Sources.

The Bilibili tab lists the Bilibili status pool, without forwards of
Kano's own dynamics, in the same layout and read-only: the Bilibili mark
with the author's name and mid, the Japan time, the wording with its image
count and a "Forwarded in a post" link to each forward in Posts (marked when
that forward is hidden, hides its quote or shows it unavailable), the
translation editor, badges for whether a forward shows it (Public, Not
public, Gone), Video for an upload and its reason, and a link to it on
Bilibili. It has no promote or demote, since the forward's quote shows the
dynamic. Filters cover the text or ID, the author's mid or name, and the
kind.

The console's post list (`src/admin/views/posts.tsx`) has platform tabs
(All, X, Bilibili; `#posts/x`, `#posts/bilibili`) above a feed of posts
separated by rules, one row of columns per post: the account over the Japan
time, the wording (three lines) and its quote, the post's own cached images
(up to three, "+N" counts the rest), the translation status, the extraction
state with a Hidden badge for a post hidden from the public pages, a
Promoted badge, which opens External posts, for a related account's
promoted status, and a
Schedule image badge for a post with a schedule image record, and the
edit, "process again with the LLM" and open actions. The columns after the
wording have fixed widths, so they line up from post to post. A text-only
post shows no image cell; a post whose images are not cached yet shows one
placeholder with the reason. A feed under 1000px puts the account, time,
state and actions on one line above the wording, images and translations,
and under 560px the state gets a line of its own. A hidden post's wording is
dimmed. Beside the account, extraction, translation and date filters,
Queued for keeps the posts with an open operator request: any, Extract
again, Read again from X or AI translation. The account filter lists the X
accounts and is absent on the Bilibili tab. A platform keeps the other
filters and starts the list again at its first page. A
`#posts/queued-<kind>` link opens the list on one kind, after the platform
when there is one (`#posts/x/queued-refresh`).

Edit opens the post in a dialog: the whole wording, every cached image, the
translations, the quote, the schedule image form and a switch that hides the
post from the public pages or shows it again, with the LLM and open actions
in its footer. An X post with images can be marked as a schedule image there:
the image (a choice when the post has several), a required reason and an
optional first and last day; a post with a record shows its badge and a link
to the Schedule list's images.

The quote is a read-only card on the sunken surface under the wording, so
its thumbnails never read as the post's own images. Its header holds the
author, handle and time, badges for a quote hidden from the board and a
queued refresh, and icon actions that hide the quote from the board or show
it again, queue a refresh from the source (disabled while one is queued) and
open the quoted post. Below come the wording, folded to three lines with a
"Show full quote" toggle that appears only when the clamp cuts it, the ready
quote thumbnails that open the image viewer, or the unavailable state. The
edit dialog shows the quote's whole wording. Nothing edits the quote itself.

Post text and video titles come with server-resolved `translations` (see
[Translations](data-model.md#translations)). `src/components/translated-text.tsx`
chooses what a reader sees from the content language, not the interface
locale: each content language gets that locale's translation by default,
except that Japanese content keeps wording detected as Japanese
(`pickTranslation`). The header's language menu
(`src/components/preference-controls.tsx`) sets both; the content language
defaults to "Same as interface", and the operator console shows only the
interface choice. The LINE BLOG paragraph translations and the schedule
month grid follow the same content locale, while feed links, history copy
and the bilibili-first rule follow the interface locale. Translated wording
is followed by `TranslationToggle`, a translate icon followed by the
origin's mark (`translationMarkKind`): "AI" for a machine translation, the
YouTube or bilibili logo for a platform translation, and nothing for a manual
translation or another platform's (`is-icon`). It is lit (`is-active`,
`aria-pressed`) while the translation shows; selecting it shows the source
and turns it off. Its
tooltip and accessible name spell out the origin (AI, platform or editor),
and on a title the tooltip holds the source. A post's switch follows its
last word; while the board's six-line clamp hides the end of the post, it
keeps its hidden place in the text and a copy shows beside Read more. The
LINE BLOG archive's marks use the same class and states. Video and schedule titles
go through `src/dashboard/components/translated-title.tsx`: `TranslatedTitle`
for a card title (spotlight tickets, shelf tickets, cards and rows, archive
rows, week-guide entries) and `TranslatedHeading` for the shelf's lead card.
A button cannot go inside a link, so these cards are
not links themselves: the title is the link (`.card-link`), and
`followCardLink` in `src/site/links.tsx` opens it from anywhere else on the
card. A week-guide entry's title is instead the button that opens the
entry's sheet, which holds its link, and `followCardLink` presses it the
same way. A clamped title clamps only its wording, so the switch stays visible.
Month-grid chips are too small for a switch and show the reader's wording
unmarked. An operator's
Featured title from the focus record shows as written. The wording shown
carries its `lang` attribute. The console's
completeness chips and translation editor are in
`src/admin/views/translation-parts.tsx`, shared by posts and videos.

Board images from the media cache, LINE BLOG archive images, Works covers
and Journey images load through `srcset`: `src/lib/media.ts` lists the server's
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

Each successful snapshot is also kept in `localStorage`
(`kano-dashboard-snapshot-v1`, with the time it was stored). The next visit
renders a stored snapshot under a week old at once and reads the API as usual;
the board reports a refresh rather than loading, and the first read replaces
it or, on failure, leaves it shown with the error. Additive projection fields
are filled with compatible defaults while a stored response does not contain
them. Without a usable stored snapshot the stage shows its placeholder, which
has a full ticket's shape (the 16:9 art band and a stub with a two-line
title, three lines at 640px and below), so the usual ticket fills it in place.

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
wake listeners. The local clock updates freshness copy; snapshot revalidation
keeps the time-dependent projection current without source synchronization.

## Time-dependent Snapshot State

The overall state, shown by the header dot and the profile card, is the
latest completed run in `meta.lastSync`: its outcome and its finish time (its
start time when it has none). An API failure takes priority, then a run that
is neither `success` nor `partial` (needs review); a `partial` run is partial
and a run older than 24 hours is out of date. Below it (on phones, before
its dot on one row, with the state's wording left to screen readers and the
dot's tooltip), the profile card lists
the last successful X, YouTube, bilibili and shop reads from `meta.sourceFetchedAt`,
each marked out of date after 24 hours; these source times do not change the
overall state. The local clock updates both warnings without a request.
Missing timestamps, and ones more than five minutes ahead of the local clock
(a visitor clock slightly behind the server still shows a fresh read), remain
waiting. This is a display threshold, not a synchronization timer.

The API supplies event and video availability, counts, focus order, spotlight
identity, sale IDs and shelf order in the dashboard projection. `view` fields
travel with calendar and archive records so the same codes render in every
reader. The minute revalidation obtains a fresh projection when time-dependent
states can expire without a durable revision; a visibility or focus wake also
uses the same request path. `deriveDashboardAt` returns a versioned projected
snapshot as received. A response without the projection follows the compatible
local derivation path, including the live grace, sale expansion, focus yield,
latest-video eligibility and cancellation rules needed by that response.

`pickSpotlight` joins the projected event and video IDs to the returned
summary, translates the projected live and members-chip flags, and keeps the
existing full or compact layout. `pickCompanion` follows the projected
companion ID. URL identity matching remains a link presentation concern: it
orders platform links and labels the destination without deciding focus or
availability. The public API exposes only the stable IDs, codes and booleans;
source item IDs and raw provider data remain on the server. Selected-media startup
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

The Detection view shows the detection mode and keywords, then the text
schedule extraction, image extraction and translation/light-task queues side
by side, then
the providers as a list beside the selected provider's detail
(`#detection/<provider>`), the model limits, and the recent failures. Recent failures
lists each stored failure's stage, relative time (the Japan time on hover),
message, model and a link to its post or video; it loads with the view and
on its reload button. A queue change
(add from the model picker, drag or move, remove) shows at once and is saved
in order: each queue has at most one save in flight, and edits made meanwhile
collapse into the latest order, sent next. A refused save returns the queue to
its last stored order and drops the edits that followed it. Each queue marks
the model each stage calls first. The provider dialog holds only the connection, with the presets above
it; there is no default model. Saving a new provider with a key selects it and
lists its models, an explicit operator action. The detail adds ticked models
to the catalog, edits their tags, turns them on or off, puts them in any queue
whose tag they carry and tests them one at a time. Removing a model takes a
second click on its danger-toned icon within a few seconds.

List filters in the console are one row of pills, one per filter, with a
date range pill and a clear button that appears while a filter narrows the
list. A choice filter opens its own list under the pill, which follows the
listbox keyboard pattern. Paged lists show the total, the first and last
page numbers with two either side of the current page, and a jump field
that goes to the nearest existing page. The Runs page puts the run history
beside the open run on a wide
screen (the newest run is open until another is chosen) and opens a run in
place on a narrower one. The open run is in the address (`#runs/<id>`): the
overview's recent runs link there, and a linked run scrolls into view, read
on its own and listed first when newer runs pushed it off the first page. A
run lists its steps one line each: the step's
outcome, its window and up to three non-zero counts, problems first. A step
opens to everything it counted, as label and value lines with zeros
receding; failed and partial steps open by themselves. Run details explain
stored error codes in words and keep the code beside them. A failed or
partial run copies its diagnostics for an agent or an issue: each step's
outcome and error code, then the stored run as JSON, with codes and English
labels so the text matches the source. A browser that refuses the clipboard
shows the text in a dialog to copy by hand.

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
are cancelled on unmount and reject superseded responses. An admin view's
`useResource` read supersedes only that view's earlier read, so two views
reading the same path load independently, and only the view's newest load
writes its state. Automatic polls wait for an active request rather
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
the data when it arrives, so a quiet week does not flicker on each refresh. The
schedule-image strip below the days appears only for a week with a schedule
image; a week without one has no strip or placeholder.
Adjacent-date hints support jumping to stored activity in either direction.

Snapshot, calendar and archive reads use `cache: "no-cache"`: the browser
revalidates its stored copy with the response's
[ETag](public-api.md#public-snapshot-contract) and receives a bodiless 304 when
nothing changed, so the minute refresh, and an open dialog's reread after it,
cost little when the board is quiet.

The archive dialogs read through `useArchive` in `use-dashboard.ts`, only
while one is open: the schedule reads the Monday-first weeks covering the month
from `/api/calendar`, past posts read the month's own days from
[`/api/posts`](public-api.md#bounded-public-archives), and past videos read
the [video timeline](public-api.md#bounded-public-archives) from `/api/videos`,
from the newest video or from the last day of a chosen start month. They
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
separate archive timer. Changing the month, the post account, the video
category or the timeline's start month, or closing the dialog, cancels the
read and discards late pages.
While the post archive or the video timeline is open, `useArchiveMonths` reads
its month index from
[`/api/posts/months` or `/api/videos/months`](public-api.md#bounded-public-archives)
and rereads it on each board revision. The month label opens a jump grid
(`MonthPicker`): a year stepper bounded by the oldest indexed month and the
latest month (this Japan month, or a later month holding a video
reservation), over twelve months whose counts follow the account or category
filter. Months without posts and months
after the latest are marked `aria-disabled`; arrow keys move between months,
Page Up and Page Down change the year, and Escape or a press outside closes the
grid before the dialog. A failed index read keeps a known index, and without
one every month up to the latest is offered.
A click anywhere in a month-grid day picks it, listing its entries beside the
grid (below it on phones) until the hide button clears the selection and the
grid takes the full width. A grid entry is a button: it picks its day, marks
the entry in that list and scrolls it into view, again on every pick, and the
list's entries open the details sheet. Only the day heading of the list is a
live region, so an opened sheet is not announced as an update. The panel can
show that week on the board. On phones entries are dots that ignore taps, so a tap always
picks the day. The post archive lays out full
posts either as a masonry wall or as a single column, a choice kept in
`localStorage` (`kano-post-archive-layout`). The masonry measures each post
and places it, in order, under the column that ends highest
(`masonryLayout` in `src/dashboard/post-rows.ts`), so it reads row by row
while the cells stay in post order for keyboard and screen readers; it
re-measures when the wall or a post resizes. A container query on the dialog
head hides the layout switch while the wall is narrower than two masonry
columns, where both layouts are one column. The video dialog is a timeline
(`VideoTimeline`): one section per Japan month of the loaded videos, in the
API's newest-first order, under a header that sticks to the top of the dialog
body and names the month with its indexed count. Each month is a grid of
cards, each the cover above the title, then the Japan date and time, the
phase or form chip, the tag chip and platform links: four columns, three or
two as the dialog narrows. The next page loads once the load button comes
within 600 px of the visible area (an `IntersectionObserver` on the dialog
body), and the button stays for keyboards; after a failed read only the button
loads again. A polite status line counts the loaded and total videos. Loaded
pages stay in memory until the dialog closes; months out of view skip layout
and paint (`content-visibility: auto`) and the cards are memoized, so a
change of the scrolled-to month renders no card again. In the
dialog's timeline mode (`ArchiveDialog`'s `timeline`), the month label follows
the scroll (the last month whose header has reached the top of the body) and
is not a live region, and the month arrows give way to a Latest button. A
month chosen in the jump grid scrolls into view when it is loaded; otherwise
the timeline restarts from that month, offering a button for the newer videos,
and Latest returns to the newest. The
category buttons (`VideoArchiveFilter`) carry the tag icons, pass the API's
`category` filter, show the index's per-category totals over every month
(`categoryCounts` of the loaded timeline while the index is unknown), and keep
the choice until the dialog closes. An image viewer opened from the
post archive stacks above it; Escape closes the topmost dialog and focus
returns to its opener.
Images match their applicable inclusive period, without an updated-at fallback.
The bounded focus snapshot refreshes at least once per minute while visible to
advance its shortlist as time passes, even when the durable revision is unchanged.
