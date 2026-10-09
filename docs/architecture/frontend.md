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
`admin.css` styles them with `adm-` prefixed selectors because it ships in the
same bundle as the board. `src/components/ui/` contains
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

## Polling and Calendar Recovery

Both request-owning hooks use a shared lifecycle/deadline helper. Requests have
a 12-second deadline including body consumption, are cancelled on unmount, and
reject superseded responses. Automatic polls wait for an active request rather
than starting overlapping work. Their timer is scheduled after settlement,
including failure, with exponential backoff capped at 60 seconds. Public polls
continue to pause while hidden. A failed initial snapshot retries even before
any revision has been recorded; manual retry remains available. Admin 401 stops
polling and requires login. Failed initial admin loads also retry.

The calendar's selected week lives in `use-dashboard.ts`. It reads one bounded
page, shows the complete week/day counts, and offers more-event pagination.
Changing weeks cancels old work, errors keep a retry control, and a revision
change between pages reloads the first page instead of combining generations.
Adjacent-date hints support jumping to stored activity in either direction.
Images match their applicable inclusive period, without an updated-at fallback.
The bounded focus snapshot refreshes at least once per minute while visible to
advance its shortlist as time passes, even when the durable revision is unchanged.
