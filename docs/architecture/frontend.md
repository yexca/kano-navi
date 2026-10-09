# Frontend

## Presentation: `src/`

`src/main.tsx` routes between the public board, the static `/history`
page in `src/history/`, the static `/about` page in `src/about/`, and `/admin`. History copy and image metadata are
static; verified fixed images ship in `public/assets/history/` and `dist/`.
`scripts/package-history-media.ts` updates these files from a local reference
archive without network requests; the runtime importer remains available for
legacy `/media` consumers.
See [History and visual archive](../product/history.md). The board
lives in `src/dashboard/`: `use-dashboard.ts` owns the snapshot request and
revision polling, `format.ts` owns Japan-time and event-status helpers,
`content.ts` holds static public links, and `components/` renders the header,
hero and spotlight, weekly schedule, X feed, videos, and archive. Its styles are
in `src/dashboard/dashboard.css` on top of the shared tokens in `src/index.css`. The hidden `/admin` console
lives in `src/admin/`: `index.tsx` registers the views and renders the shell,
`use-admin-data.ts` owns every admin request and job polling, `views/` holds
the workflow, schedule, detection-rule, LLM-provider, and content views, and
`admin.css` styles them with `adm-` prefixed selectors. `src/components/ui/` contains
basic UI primitives, while the CSS files contain layout and design tokens. The
presentation layer must not import `better-sqlite3` or call X, YouTube, OpenAI,
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
synthetic start time. Streams retain a three-hour live grace period, or their
explicit end time. A video linked to an event inherits its explicit end time for
both `nextStream` and `pickSpotlight`. YouTube watch, youtu.be, live, and shorts
links match by the video identity parsed locally from their public URL, ignoring
share/query parameters and supported YouTube subdomain differences. Other links
retain exact URL matching; different videos and lookalike hosts do not match.
Matching needs no protected source item IDs or additional public API fields.
At the end instant the video yields to later focus candidates. This derived video
`endsAt` belongs to local snapshot state. Without an explicit end, the three-hour
grace ends exactly three hours after the start. Confirmed cancellation flags prevent local
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

## Related Docs

- [Design and interaction](../development/design.md)
- [Testing](../development/testing.md)
- [Product overview](../overview.md)
- [History and visual archive](../product/history.md)
