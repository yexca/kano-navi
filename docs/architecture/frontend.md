# Frontend

## Presentation: `src/`

`src/main.jsx` routes between the public board, the static `/history`
page in `src/history/`, the static `/about` page in `src/about/`, and `/admin`. History copy and image metadata are
static; verified fixed images ship in `public/assets/history/` and `dist/`.
`scripts/package-history-media.mjs` updates these files from a local reference
archive without network requests; the runtime importer remains available for
legacy `/media` consumers.
See [History and visual archive](../product/history.md). The board
lives in `src/dashboard/`: `use-dashboard.js` owns the snapshot request and
revision polling, `format.js` owns Japan-time and event-status helpers,
`content.js` holds static public links, and `components/` renders the header,
hero and spotlight, weekly schedule, X feed, videos, and archive. Its styles are
in `src/dashboard/dashboard.css` on top of the shared tokens in `src/index.css`. The hidden `/admin` console
lives in `src/admin/`: `index.jsx` registers the views and renders the shell,
`use-admin-data.js` owns every admin request and job polling, `views/` holds
the workflow, schedule, detection-rule, LLM-provider, and content views, and
`admin.css` styles them with `adm-` prefixed selectors because it ships in the
same bundle as the board. `src/components/ui/` contains
basic UI primitives, while the CSS files contain layout and design tokens. The
presentation layer must not import `better-sqlite3` or call X, YouTube, OpenAI,
or a third-party proxy directly.

## Request and UI Ownership

Keep app composition in src/main.jsx, public snapshot requests in
src/dashboard/use-dashboard.js, and admin requests and polling in
src/admin/use-admin-data.js. Reuse the existing state and request patterns.
Shared primitives must remain independent of dashboard, history, and admin
features. Extract cohesive helpers when needed; avoid a repository-wide move
for a small change.

## Related Docs

- [Design and interaction](../development/design.md)
- [Testing](../development/testing.md)
- [Product overview](../overview.md)
- [History and visual archive](../product/history.md)
