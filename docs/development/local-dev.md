# Local Development

## Prerequisites

- Node.js 24 LTS (the same major version used by CI and Docker)
- npm 10+
- Git

SQLite uses `better-sqlite3`; installation may require a prebuilt package or a
local compiler toolchain on the current platform.

## Start and Build

```bash
make install
cp .env.example .env
# Configure ADMIN_PASSWORD, or explicitly set APP_MODE=development locally.
make dev
```

Development mode starts Vite (default `http://localhost:5173`) and the Express
API (default `http://localhost:7657`). Vite proxies `/api`, `/media`, and `/mcp`
to Express, so the browser and MCP clients use the same origin in development
and production.

The public dashboard is `/`. The admin interface is available only by entering
`/admin`; no public navigation links to it. The example environment and server
default to `APP_MODE=production`. Configure `ADMIN_PASSWORD` before starting,
or explicitly set `APP_MODE=development` in the ignored `.env` for password-free
local work. An unset `APP_MODE` never enables the development bypass, even when
`NODE_ENV=development`. Missing, whitespace-only, and shorter-than-12-character
passwords are rejected before the runtime database is opened or seeded.
The console opens on Workflows: pick modules, run them now, or enable a timer
for a saved workflow. LLM providers are configured once (API host, format, and
key), their model list is fetched from `<baseUrl>/models`, and each model is
tagged (`text`/`image` drive routing; `reasoning`/`tools`/`embedding` are
labels). Detection rules then order provider + model targets per route; each
target follows the fixed three-request failover policy. Content & media covers
the Featured video and profile media. Provider URLs that resolve to loopback or
private ranges are rejected, so local mock endpoints cannot be configured.
Set `WORKFLOW_SCHEDULER_ENABLED=0` to keep timed workflows from firing while
developing.

Workflow source lookback settings control recent days separately from the timer
interval (X defaults to 7 days; YouTube to 14). Post management and Content & media
also offer older-record backfill and date ranges. For historical searches,
optionally configure environment-only `X_API_BEARER_TOKEN` with full-archive
access and/or `YOUTUBE_API_KEY`, then restart the server. Without keys, recent
fetches retain the finite public profile/RSS coverage and report that limitation.
See [Fetch windows](../architecture/workflows.md#fetch-windows-and-historical-backfill).

Tailwind CSS 4 runs through `@tailwindcss/vite`; `src/index.css` loads the
existing `tailwind.config.js` with `@config`. The CSS runtime requires Safari
16.4+, Chromium 111+, or Firefox 128+. `tailwind-merge` uses its Tailwind 4
compatible major version. `concurrently` is development-only; its scoped
`shell-quote` override selects a patched version because the current upstream
release pins a vulnerable one. Recheck `npm audit` and development startup
when changing that override.

`/mcp` exposes public read-only tools without a key. Set `MCP_CONTROL_TOKEN` to
enable the protected control tools; send it only as an `Authorization: Bearer`
header. MCP-triggered sync and automatic scans are asynchronous jobs and can be
inspected through the status tool or `/api/admin/sync/jobs` after admin login.
Human confirmation and editing are intentionally unavailable through MCP.

For production, set `APP_MODE=production` and an `ADMIN_PASSWORD` containing at
least 12 characters in the ignored `.env` file, then run:

```bash
npm run build
npm start
```

In PowerShell, use Copy-Item .env.example .env. GNU Make is the canonical
validation entry point; npm scripts remain available for the individual tasks.
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
