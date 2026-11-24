# Presentation

Page routing and the presentation of the public pages.

## Presentation: `src/`

`src/main.tsx` routes between the public board, the `/posts` archive in
`src/posts/`, the static `/journey`
page in `src/history/` (an address of `/history` is replaced with `/journey`
before the first page loads), the static `/works` discography in `src/works/`, the
static `/lineblog` archive in `src/lineblog/`, the static `/about` page in
`src/about/`, and `/admin`.
Links between the six public pages open in place: the next page's code
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
See [History archive maintenance](../../development/history-archive.md). The
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
[Smartphone Template](../../development/lineblog-archive/comments-and-template.md#smartphone-template). Its
media load from `/media/lineblog/<name>`, including the header and profile
art of the design of the newest article shown; see
[LINE BLOG archive maintenance](../../development/lineblog-archive.md). The
`/works` tables are static JSON written by `scripts/package-works.ts`. The
page keeps its section in the `?view=` query, replacing the current entry
when a visitor switches sections, and `?release=<id>` opens that release's
sheet once and is then dropped from the address; the personal works build with the page,
and the participation and upload sections are their own chunks that load
the first time they show, with a retry on failure. The side the trilogy
scene shows is kept in `localStorage` (`kano-works-trilogy-side`). A release opens in a
native modal sheet (`works-sheet.tsx`), and covers open in the shared image
viewer above it. Covers load from `/media/works/<name>`; see
[Works collection maintenance](../../development/works-archive.md). The upload
section reads [`/api/works/uploads`](../public-api/works.md#works-uploads) once per
page load (`use-works-uploads.ts`) for the uploads' covers, lengths,
availability, and for the uploads after the collection
date, which `recent-uploads.tsx` shows as a block above the toolbar; until
it answers the art stays blank, and a failed read keeps the packaged list
with printed sleeves and no newer block. The five
public pages share `src/site/`: `site-shell.tsx` (header with page navigation and
Japan clock, footer, skip link, section title, empty and loading states;
narrow screens show the site name and fold the page links into a menu behind
the `kano-seal.tsx` deer seal; the footer is the fan-made and unaffiliated
line with Notes and Back to top as icons),
`site-notice.tsx` (the site's one notice, the same on every page, which
Notes opens in a native modal dialog and About shows in place),
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
below, so it keeps its label as `aria-label`. There the post wall's and the
video shelf's filters share one row with that icon, and a filter too long for
the row scrolls sideways without a visible scrollbar. The board lives in
`src/dashboard/`: `use-dashboard.ts` owns the snapshot request and the
revision stream, `format.ts` owns Japan-time and event-status helpers, `content.ts` holds static public links, and
`components/` renders the next-up stage and profile card, the on-sale list
(`sale-strip.tsx`: the IDs from `projection.sales`, rendered in API order,
open or soon by each event's `view.state`; the stage passes its
placement, `side` under the profile card beside a full ticket or `main` under
a compact one, which sets how many show in place, and the rest open in a
`sheet-dialog.tsx` sheet), the week guide,
the post wall, the video shelf, and the link directory, plus
`archive-dialog.tsx`, the native modal sheet that browses one month of the
schedule or the video timeline, with `ArchiveMonthBar`, the month controls the
`/posts` page shares (at 640px and below its head is the month row beside
the close button, then any filter: the title stays only for
assistive technology, the month's count is hidden and the month label takes
the short month name), and `sheet-dialog.tsx`, the smaller modal sheet
that shows one long post or one schedule entry in full and can open above
the month sheet. `video-groups.ts` consumes the dashboard's
`projection.shelf` and each video's `view`: the API supplies the lead, group,
phase and stable order, while the browser applies the active filter and
renders the cards. A video the shelf lists leave out, such as the lead in a
filtered view, joins its `view.shelfGroup`, and one without a group stays off
the shelf. In the All view, `sideSlots` in
`video-groups.ts` cuts the side group beside the lead (or the full-width
ticket row without one) at every width: up to four earlier works when there
are no reservations; otherwise up to four tickets and earlier works up to
five items in all, so at least one earlier work stays beside them. Tickets
past the fourth fold behind a `+N` toggle at the trailing edge of the Coming
up subhead row: a sibling `button` of the `h3` with `aria-expanded`,
`aria-controls` on the ticket list and a translated name, which expands the
list in place to every reservation without changing the earlier works. Its
state is local and starts collapsed; the Streams and Works views list every
matching reservation. A video carries two chips. The form
chip (`videoFormLabel`) translates `view.formCode`, and `VideoTagChip`
translates the stored tag with the context in `view.tagContext`; neither
helper reconstructs the phase or form. The
tag chip (`VideoTagChip` in
`src/dashboard/components/event-tag.tsx`) adds the tag with the schedule's
icon, in the schedule's words for a stream (gameplay, talk, singing) and as
the work's content otherwise (gameplay, talk, music, where a note replaces
the singing microphone, as on the archive's Songs button); it appears on every
card, row, ticket and the lead. The shelf's group or filter names a past
video's form, so its grid cards and earlier-work rows show no form chip, and
the lead shows one only for a featured stream archive or Short; reservation
tickets and the video dialog's cards keep it. The Next Up ticket names the video's tag, else the entry's
activity label, before the form; `spotlight.membersChip` adds the
members-only chip (`MembersChip`) beside it, and `spotlight.isLive` marks the
ticket On air; an `ongoing` event's ticket names it under way, without a
countdown. Schedule entries consume the event
`view`: `eventKinds`, `scheduleEntries` and `eventStatus` translate its type,
activity, calendar entries and state. The sale period's opening and closing
dates come from `view.calendarEntries`, and the status codes are
`upcoming`, `open` and `done`. The canonical enum contract is in the [Public
API](../public-api/snapshot.md): `event.view.state` has eight
values (`upcoming`, `live`, `ongoing`, `open`, `done`, `pending`,
`cancelled`, `cancellation_review`); the week folds neither a `live` nor an
`ongoing` entry. `video.view.phase` has five (`upcoming`, `live`,
`started`, `past`, `cancelled`), and `formCode` has six non-null values
(`short`, `upcoming`, `stream`, `archive`, `premiere`, `video`). The
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
registers the views in four nav groups by what the operator is doing — Today
(Overview, Review queue), Content, what the public pages show (Schedule,
Posts, External posts, Videos, Account avatars, Profile images), Pipeline,
how data comes in (Sources, Detection, Runs, Alerts), and System, the rarely
used settings and maintenance (Proxies, Site, Works) — and renders the shell with hash routing
(`#view/tab`, where the tab is everything after the view and may hold
several parts; `resolveHash` maps older addresses: `#content/<tab>` opens
the matching view, `#models` and `#llm/<provider>` open Detection, which
holds the model setup beside the detection mode, and `#queue/<step>` opens
the Runs page's Up next tab); `use-admin-data.ts` owns every
[Admin API](../admin-api.md) call, the session and the overview poll that tracks
the active run while the console is visible. Until a session read succeeds
the login requirements are unknown, so a failed read shows its reason and a
retry rather than a password-only form, and a refused login reads the
session again in case the one-time code was turned on or off; `api.ts` is the transport and error mapping; `types.ts` holds
the wire shapes; `ui.tsx` the console primitives (buttons, panels, native
modal dialog, toasts); `views/` one file per section, with
`views/platform-tabs.tsx` the platform tab row that opens Posts, External
posts and Videos: one icon tab per platform, styled like the Sources tabs,
above a tab panel, the platform kept in the URL, and the arrow keys, Home and
End moving between the tabs; and `admin.css` styles
them with `adm-` prefixed selectors, importing one file per area from
`styles/` in cascade order. `src/components/ui/` holds the Radix wrappers
for dropdown menus, tabs and tooltips and the header's button; the shared
design tokens are in `src/index.css`, and each area's stylesheet holds its
layout and look (see
[Design and interaction](../../development/design.md)). The
presentation layer must not open SQLite or call X, YouTube, OpenAI,
or a third-party proxy directly. The optional mainland China site comes from
the same-origin `/api/visitor-region` response, read at most once per page
load through `src/lib/visitor-region.ts`. When the operator has enabled it,
the footer links to the mirror while the interface is Simplified Chinese;
when the response also reports mainland China, the root app shows a
dismissible Toast offering the site, once per browser session. About's credit
for the mainland China server site is static copy in `src/about/` and does
not read this response.
