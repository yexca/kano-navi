# Local Development

## Prerequisites

- Go 1.26.10 or newer (the version in `backend/go.mod`, matching CI and Docker)
- Node.js 24.22.1 or newer in the 24 LTS line (`.nvmrc` pins the version CI
  and Docker use)
- npm 10+
- Git
- A C compiler such as gcc, only for `make go-race` and `make ci-backend`

The backend is the Go module in `backend/`; it uses the pure-Go
`modernc.org/sqlite` driver, so building and running it needs no C toolchain.
Only the race detector needs cgo and a C compiler. Its package layout,
commands and startup order are in [Backend](../architecture/backend.md).

The frontend, the maintenance scripts in `scripts/` and the frontend tests in
`tests/` use TypeScript. Node 24 runs the scripts and tests through native
type stripping; Vite transforms React `.tsx` files. Runtime imports in the Node layer use explicit `.ts`
extensions and type-only imports use `import type`. Avoid enums, parameter
properties, and other syntax requiring runtime transformation.

`tsconfig.json` checks the browser with bundler module resolution;
`tsconfig.node.json` checks the scripts and tests with NodeNext.
Both use `noEmit`, `verbatimModuleSyntax`, and `erasableSyntaxOnly`.
Run `make typecheck` for a standalone check. The shared
public snapshot and UI primitive props have explicit types; some dynamic JSON
handling keeps permissive types. Strict mode is not enabled globally. The
backend's API input validation and sanitized projections are the runtime
boundary.

## Start and Build

```bash
make install
cp .env.example .env
# Configure ADMIN_PASSWORD, or explicitly set APP_MODE=development locally.
# Configure LLM_SECRETS_KEY in every mode before starting.
make dev
```

`make dev` (`scripts/run-app.mjs`) builds the Go backend into
`backend/bin/`, starts it on `127.0.0.1:KANO_API_PORT` (default `7658`)
and starts Vite at `http://localhost:7657`. Vite serves the frontend with hot
reload and proxies `/api`, `/media`, and `/mcp` to the backend with the
browser's Host header, so the page and the API stay on one origin. Either
process ending stops the other, so a refused backend start (for example a
missing `LLM_SECRETS_KEY`) is visible immediately. `npm run preview` does the
same with the built frontend and also defaults to `7657`, as does production.
Set `PORT` in the environment or `.env` to override the default. Development
and preview use `strictPort` and fail if that port is occupied.

`make dev-server` builds and starts only the backend on `KANO_API_PORT`, and
`make dev-client` starts only Vite; use the pair to keep the two logs apart.
Restart the backend after changing Go source. The backend reads `.env`,
`data/` and `dist/` relative to the repository root.

The public dashboard is `/`. The admin interface is available only by entering
`/admin`; no public navigation links to it. The example environment and server
default to `APP_MODE=production`. Configure `ADMIN_PASSWORD` before starting,
or explicitly set `APP_MODE=development` in the ignored `.env` for password-free
local work. An unset `APP_MODE` never enables the development bypass, even when
`NODE_ENV=development`. Missing, whitespace-only, and shorter-than-12-character
passwords are rejected before the runtime database is opened or seeded.
All modes also require a non-blank `LLM_SECRETS_KEY` before SQLite opens.
See [Configuration](../operations/configuration.md) for generating and retaining
the encryption key.
The console opens on Overview. Sources holds each source's timer and its
**Run now**; Runs picks any steps and runs them once. LLM providers are configured once (API host, format, and
key), their model list is fetched from `<baseUrl>/models`, and each model is
tagged (`text`/`image` drive routing; `reasoning`/`tools`/`embedding` are
labels). Detection settings then order provider + model targets in one
priority queue; each stage skips targets without the capability it needs, and
each target follows the fixed three-request failover policy. Content & media covers
the Featured video and profile media. Provider URLs that resolve to loopback or
private ranges are rejected, so local mock endpoints cannot be configured.
Set `WORKFLOW_SCHEDULER_ENABLED=0` to keep the source timers from firing
while developing.

Each source timer's sync setting controls how much a run reads, separately
from its interval (incremental with a cap of 50 by default). The Sources page
also starts manual fetches through one method each. Without keys, X fetches
page the public FxTwitter timeline (the newest posts, or everything) and
YouTube fetches read the RSS feed's newest 15 uploads. For X date ranges and
backfills, or the whole YouTube uploads playlist, optionally configure
environment-only `X_API_BEARER_TOKEN` with full-archive access and/or
`YOUTUBE_API_KEY`, then restart the server. The X token also lets the X
timer's method chain fall back to the X API, which stays off until it is
enabled on the Sources page.
See [Manual fetches](../architecture/workflows.md#manual-fetches-and-historical-backfill).

Tailwind CSS 4 runs through `@tailwindcss/vite`; `src/index.css` loads the
existing `tailwind.config.ts` with `@config`. The CSS runtime requires Safari
16.4+, Chromium 111+, or Firefox 128+. `tailwind-merge` uses its Tailwind 4
compatible major version.

`/mcp` exposes public read-only tools without a key. Set `MCP_CONTROL_TOKEN` to
enable the protected control tools; send it only as an `Authorization: Bearer`
header. MCP-triggered sync and automatic scans are asynchronous jobs and can be
inspected through the status tool or `/api/admin/runs` after admin login.
Human confirmation and editing are intentionally unavailable through MCP.

For production, set `APP_MODE=production` and an `ADMIN_PASSWORD` containing at
least 12 characters in the ignored `.env` file, then run:

```bash
npm run build
npm start
```

`npm start` (`make start`) builds the backend and runs `kano-go serve`, which
serves the API and `dist/` on `PORT`. `make seed` and `make sync` run the
one-shot maintenance commands; see
[Database and maintenance](../operations/database.md).

In PowerShell, use Copy-Item .env.example .env. GNU Make is the canonical
validation entry point; npm scripts are available for the individual tasks.
Run make help for targets. Validation targets use installed dependencies;
make ci installs the lockfile first and then runs the full portable sequence.

## Files and Environment

- data/ contains ignored runtime SQLite, source media, and selected profile media.
- dist/, node_modules/, and local test artifacts stay out of Git.
- public/assets/ contains fixed branding and reviewed history images.
- .env is ignored; .env.example is the complete non-secret runtime inventory.
- When adding configuration, update .env.example and
  [Runtime configuration](../operations/configuration.md), plus the relevant
  architecture contract and tests.

## Change Workflow

1. Read AGENTS.md and the relevant topic, then inspect git status --short.
2. Preserve existing user changes and follow the existing module boundaries.
3. Run the smallest applicable Make target from [Testing](testing.md).
4. Run make sensitive-check and review findings before handoff or a commit.
5. Create commits and push only when explicitly requested by the user.

## Related Docs

- [Testing and CI](testing.md)
- [Design](design.md)
- [Secure development](security.md)
- [Docker](../operations/docker.md)
- [Data maintenance](../operations/database.md)
