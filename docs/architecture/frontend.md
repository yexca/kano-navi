# Frontend

## Presentation: `src/`

`src/main.tsx` routes between the public board, the static `/history`
page in `src/history/`, the static `/about` page in `src/about/`, and `/admin`. History copy and image metadata are
static; verified fixed images ship in `public/assets/history/` and `dist/`.
`scripts/package-history-media.ts` updates these files from a local reference
archive without network requests.
See [History and visual archive](../product/history.md). The three public
pages share `src/site/`: `site-shell.tsx` (header with page navigation and
Japan clock, footer, skip link, section title, empty and loading states),
`media-dialog.tsx` (the native modal image viewer), `links.tsx` (external
links and platform marks), and `site.css` (shell, controls, tone classes and
the dialog). The board lives in `src/dashboard/`: `use-dashboard.ts` owns the
snapshot request and revision polling, `format.ts` owns Japan-time and
event-status helpers, `content.ts` holds static public links, and
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
shows a platform link for the video and each of its `alternates`, and the
length from `durationSeconds` when known. Its styles are in
`src/dashboard/board.css` on top of `src/site/site.css` and the shared tokens
in `src/index.css`. The hidden `/admin` console
lives in `src/admin/` and is organized around operator tasks: `index.tsx`
registers the views (Overview, Review queue, Schedule, Posts, Videos, Profile
images, Runs & workflows, Sources, Models & detection) and renders the shell
with hash routing (`#view/tab`; older `#content/<tab>` links open the matching
view); `use-admin-data.ts` owns every
[Admin API](admin-api.md) call, the session and the overview poll that tracks
the active run; `api.ts` is the transport and error mapping; `types.ts` holds
the wire shapes; `ui.tsx` the console primitives (buttons, panels, native
modal dialog, toasts); `views/` one file per section; and `admin.css` styles
them with `adm-` prefixed selectors. `src/components/ui/` contains
basic UI primitives, while the CSS files contain layout and design tokens. The
presentation layer must not open SQLite or call X, YouTube, OpenAI,
or a third-party proxy directly.

## Request and UI Ownership

Keep app composition in src/main.tsx, public snapshot requests in
src/dashboard/use-dashboard.ts, and admin requests and polling in
src/admin/use-admin-data.ts. Reuse the existing state and request patterns.
Shared primitives must remain independent of dashboard, history, and admin
features. Extract cohesive helpers when needed; avoid a repository-wide move
for a small change.

`/admin`, `/history`, and `/about` use React lazy imports with independent page
CSS and translation chunks. The shared settings provider owns persistent theme
and language; page message providers extend its translator without duplicating
preferences. Shared navigation labels remain in the base dictionary. Suspense
shows a localized loading status, and a page error boundary offers a full-page
reload when a chunk fails (including after a deployment). Direct visits and
trailing-slash routes work through the existing SPA fallback. History/about
remain static and never mount the dashboard hook or request its API.

English is the base locale. Every message exists in English, a key missing
from Japanese or Simplified Chinese falls back to it, and a browser language
that is none of the three selects it. Stored records are not interface copy:
event status codes and the generated YouTube details become labels through
`src/lib/event-text.ts`, which also reads the Chinese text earlier releases
stored, while other details and operator notes show as stored (see
[Stored event text](data-model.md#stored-event-text)).

Post text and video titles come with server-resolved `translations` (see
[Translations](data-model.md#translations)). `src/components/translated-text.tsx`
chooses what a reader sees: English and Chinese readers get their locale's
translation by default and Japanese readers always get the source. Block
text such as a post uses `TranslatedText`, which labels the origin (AI,
platform or editor) and offers a Show original button; a title inside a
link, where a button cannot go, uses `TranslationMark`, whose tooltip holds
the source. Video and schedule titles go through
`src/dashboard/components/translated-title.tsx`: `TranslatedTitle` for a
title inside a link (spotlight tickets, shelf tickets, cards and rows,
archive rows, week-guide entries) and `TranslatedHeading`, with the button,
for the shelf's lead card. A clamped shelf title clamps only its wording, so
the mark stays visible.
Month-grid chips are too small for a mark and show the reader's wording
unmarked; the week guide marks it and holds the source. An operator's
Featured title from the focus record shows as written. The wording shown
carries its `lang` attribute. The console's
completeness chips and translation editor are in
`src/admin/views/translation-parts.tsx`, shared by posts and videos.

SQLite stores one text per seeded timeline and link row. The board translates
that text through src/dashboard/seed-copy.ts, keyed by the exact seed wording
in static-seed.json, so a row whose text was changed shows as stored. Update
both files together; the frontend tests require English and Japanese copy for
every seed row.

The dashboard hook coordinates one current request. Snapshot refreshes share an
in-flight promise; a manual refresh cancels an older revision read, and stale
completions cannot install state. A 15-second deadline covers headers and JSON
consumption. Polling uses a completion-scheduled timer, normally every 8 seconds,
with failed requests backing off through 8/16/32/60 seconds. Failed first loads
and failed reloads retry snapshots, preserving a previously loaded snapshot.
Hidden pages pause polling; visibility/focus events coalesce into a prompt
revision check while respecting failure backoff and a one-second minimum gap.
Unmount aborts requests and removes deadlines, polling and wake listeners.
The separate 30-second local time derivation remains unchanged.

## Time-dependent Snapshot State

The profile card shows separate X and YouTube successful-read times from
`meta.sourceFetchedAt`. A source becomes stale after 24 hours; the local clock
updates that warning without a request. Missing or future timestamps remain
waiting, one unread source makes a known snapshot partial, and API or failed-run
warnings take priority. Older payloads without per-source metadata fall back
to `meta.fetchedAt`. This is a display threshold, not a synchronization timer.

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
(`featuredVideoIdOf`: `meta.featuredVideoId`, or the focus video of an older
snapshot). An unknown format stays eligible, and when every recent upload is a
Short the stage shows no latest video. The play mark appears only on
YouTube or Twitch links; X links read as an announcement.
Matching needs no protected source item IDs or additional public API fields.
At the end instant the video yields to later focus candidates. This derived video
`endsAt` belongs to local snapshot state. Without an explicit end, a stream's
three-hour grace ends exactly three hours after the start. Confirmed cancellation flags prevent local
rederivation from restoring a cancelled reservation. Selected-media startup
repairs follow the [durable revision contract](data-model.md#event-precedence),
so existing revision polling reloads a changed avatar URL without increasing
full-snapshot request frequency. This requires no source
synchronization or external request.

## Provider Company Icons

The provider presets are OpenAI, Anthropic, DeepSeek, OpenRouter, Gemini, and
Custom. Company marks are bundled local SVGs from Lobe Icons 1.90.0 (MIT);
they never require a browser CDN request. The license ships in
`public/assets/providers/LICENSE.txt`. Configured providers use the API host,
name, or stable ID to identify a mark; unknown custom providers retain their
initial. Removing a preset does not delete saved providers.

The provider dialog lists presets beside the connection form. Model discovery
there is still an explicit operator action: it saves the connection first, so a
new provider exists before its key is used, and later saves in the same dialog
update that row. There is no default model: on save, ticked models join the
catalog and models marked for the queue are appended to its end. The models
dialog queues, dequeues and tests one catalog model at a time.

## Related Docs

- [Design and interaction](../development/design.md)
- [Testing](../development/testing.md)
- [Product overview](../overview.md)
- [History and visual archive](../product/history.md)

## Polling and Calendar Recovery

Calendar and admin requests use the shared lifecycle/deadline helper with a
12-second deadline including body consumption. The public snapshot coordinator
uses the 15-second deadline and refresh deduplication described above. All requests
are cancelled on unmount and reject superseded responses. Automatic polls wait for an active request rather
than starting overlapping work. Their timer is scheduled after settlement,
including failure, with exponential backoff capped at 60 seconds. Public polls
continue to pause while hidden. A failed initial snapshot retries even before
any revision has been recorded; manual retry remains available. Admin 401 stops
polling and requires login. Failed initial admin loads also retry.

The board's calendar shows one week; its selected week lives in
`use-dashboard.ts`. It reads
one bounded page, shows the complete range/day counts, and offers more-event
pagination. Changing the range cancels old work and begins at page one; errors keep a retry control.
Same-range refreshes with an unchanged revision retain every already loaded page.
A revision change rereads the loaded page range and replaces it atomically, stopping
at the new last page if the range shrank. A change while reading that range restarts
from page one, with at most three attempts under the shared request deadline.
Failed or repeatedly changing reads retain the complete known generation for retry;
they never append pages from different revisions. Automatic snapshot refresh waits
for active calendar work, and a pending calendar revision is checked on later polls.
Adjacent-date hints support jumping to stored activity in either direction.

The month dialogs read through `useArchive` in `use-dashboard.ts`, only
while one is open: the schedule reads the Monday-first weeks covering the month
from `/api/calendar`, and past posts and videos read the month's own days from
[`/api/posts` and `/api/videos`](backend.md#bounded-public-archives). They
follow the same rules as the calendar: pages of one revision are appended, a
later page from another revision rereads the loaded pages atomically (up to
three attempts under the 12-second deadline), and a board revision change
rereads them while the dialog stays open. Changing the month or the post
account, or closing the dialog, cancels the read and discards late pages.
Picking a day in the month grid lists its entries beside the grid (below it on
phones) and can show that week on the board. An image viewer opened from the
post archive stacks above it; Escape closes the topmost dialog and focus
returns to its opener.
Images match their applicable inclusive period, without an updated-at fallback.
The bounded focus snapshot refreshes at least once per minute while visible to
advance its shortlist as time passes, even when the durable revision is unchanged.
