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
the video shelf, the post wall, and the link directory. Its styles are in
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
event status codes become labels through `src/lib/event-text.ts`, which also
reads the Chinese text earlier releases stored, while titles, details and
operator notes show as stored (see
[Stored event text](data-model.md#stored-event-text)).

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
compact ticket unless it is the same link. The play mark appears only on
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

The calendar's view (week or month), selected week and selected month live in
`use-dashboard.ts`; a month reads the Monday-first weeks that cover it. It reads
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
Images match their applicable inclusive period, without an updated-at fallback.
The bounded focus snapshot refreshes at least once per minute while visible to
advance its shortlist as time passes, even when the durable revision is unchanged.
