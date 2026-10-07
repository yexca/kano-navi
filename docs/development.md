# Local Development

## Prerequisites

- Node.js 24 LTS (the same major version used by CI and Docker)
- npm 10+
- Git

SQLite uses `better-sqlite3`; installation may require a prebuilt package or a
local compiler toolchain on the current platform.

## Start and Build

```bash
npm install
cp .env.example .env
# Configure ADMIN_PASSWORD, or explicitly set APP_MODE=development locally.
npm run dev
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

The root `docker-compose.yml` deploys the published production image. Copy
`.env.example` to `.env`, keep `APP_MODE=production`, and configure
`ADMIN_PASSWORD` with at least 12 characters after trimming surrounding
whitespace. Then run:

```bash
docker compose up -d
```

Compose pulls `yexca/kano-navi:latest` from Docker Hub on each startup. A deployment
directory needs only `docker-compose.yml` and `.env`; the `./data` directory
is created when needed and mounted at `/app/data`, preserving SQLite and media
across container replacement. Run the same command to apply an image update.
Set `KANO_IMAGE=yexca/kano-navi:0.1.0` in `.env` to pin the release,
and use `KANO_PORT` to change the default host port of `7657`. The container
listens on `7657`; the default public address is `http://localhost:7657`.
To use the equivalent GHCR image, set
`KANO_IMAGE=ghcr.io/yexca/kano-navi:latest`.

Fresh deployments start with empty X/YouTube snapshots and no LLM providers.
Add providers and routes in `/admin`, then run a workflow to populate source
data. Fixed history images are already bundled. Existing `./data` snapshots
and provider configuration survive upgrades.

Behind an HTTPS reverse proxy, preserve `Sec-Fetch-Site` from browser requests
and forward `X-Forwarded-Proto: https` so session cookies receive `Secure`.
Same-origin browser mutations work even if the proxy rewrites Host. Clients
without Fetch Metadata need the original public Host and forwarded protocol
to match any supplied `Origin`.

To build the production image from the local checkout:

```bash
docker build -t kano-navi:local .
KANO_IMAGE=kano-navi:local docker compose up -d --pull never
```

Version tags run the Release workflow, which builds once and publishes the
same image to Docker Hub and GHCR with version, major/minor, and `latest` tags.
Configure the repository secret `DOCKERHUB_TOKEN` with push access. The Docker
Hub username defaults to the GitHub repository owner; set the optional
repository variable `DOCKERHUB_USERNAME` when those accounts differ. GHCR
uses the workflow's `GITHUB_TOKEN` with package-write permission.

For Docker development, use the separate Compose file:

```bash
docker compose -f docker-compose.dev.yml up -d --build --renew-anon-volumes
```

Open `http://localhost:5173` for the dashboard or
`http://localhost:5173/admin` for the password-free development console.
The development image starts Vite and Express together. Only the Vite port is
published, on loopback; `/api`, `/media`, and `/mcp` are proxied to the API
inside the container. The repository is mounted for frontend hot reload,
while an anonymous volume keeps Linux dependencies separate from any host
`node_modules`. Rebuild after dependency changes; `--renew-anon-volumes`
refreshes that dependency volume. API source changes require
`docker compose -f docker-compose.dev.yml restart`. The existing `./data`
directory persists SQLite and media. A local `.env` is optional and is read
by the server when present; the Compose file sets development mode and disables
workflow timers.

```bash
docker compose -f docker-compose.dev.yml logs -f
docker compose -f docker-compose.dev.yml down
```

The Makefile provides the same entry points. Run `make help` to list targets;
`make dev` starts development and `make ci` runs the complete local check.

## Data Commands

The `/history` visual archive has an offline import command:
`npm run history:import -- --from /path/to/kano_official`. Run it in the server's
environment. See [History and visual archive](history.md) for details.

```bash
npm run seed                 # Idempotently add missing initial records
npm run seed -- --overwrite # Overwrite records with matching IDs from seed-data
npm run sync                 # Update the snapshot from public sources
```

Synchronization accesses the network and is therefore not part of the default
CI build. To debug one source, set `SKIP_X=1` or `SKIP_YOUTUBE=1`; never put a
temporary credential in shell history, source code, or the database.

Provider keys are managed in `/admin` and encrypted in SQLite with the
environment-only `LLM_SECRETS_KEY`. With no configured provider key, the
schedule-extraction stage reports a safe skip. To import a legacy
`OPENAI_API_KEY` once, set both environment variables and run
`npm run migrate:llm`; remove the legacy variable afterwards. `SCHEDULE_MESSAGE_ENABLED` controls the single-message detector;
`SCHEDULE_KEYWORD_ENABLED` and `SCHEDULE_VISION_ENABLED` control the board
path. `SCHEDULE_EXTRACTION_ENABLED` is retained for initial defaults and old
API compatibility; the stage flags determine what runs after configuration.
Provider API keys entered in `/admin` require `LLM_SECRETS_KEY` and are
encrypted before they reach SQLite; they are never returned by the API. Once
configured, keys are read only by the server-side synchronization process.
Provider eligibility is based on the original post modality: Text, Image, or
both for mixed input. A failed provider is retried up to three total calls
before the next compatible provider is used.
X synchronization reads both handles in `X_HANDLES` and keeps a separate cursor
for each account.

## Checks

| Command                    | Purpose                                                |
| -------------------------- | ------------------------------------------------------ |
| `npm run format`           | Format supported project files with Prettier           |
| `npm run format:check`     | Check project formatting without changing files        |
| `npm run build`            | Verify the Vite production build                       |
| `npm run test:sensitive`   | Run sensitive-information scanner unit tests           |
| `npm run check-sensitive`  | Scan auditable text in the current Git workspace       |
| `npm run docs:check-links` | Check local links in README and docs                   |
| `make ci`                  | Install dependencies and run the checks plus the build |
| `npm run test:server`      | Test SQLite, admin, sync, media, and LLM contracts     |

For UI changes, check both `/` and `/admin` in a browser at desktop and mobile
widths. For API or database changes, request `http://localhost:7657/api/health`,
`/api/dashboard?days=3`, and `/api/dashboard/revision`, then verify the fields
and stale-snapshot behavior.

## Suggested Workflow

1. Read `AGENTS.md` and the relevant topic document, then inspect `git status --short`.
2. Keep the change focused and follow existing names and component boundaries.
3. Run the smallest relevant check first, then run `make ci`.
4. Review the sensitive-information and documentation-link output.
5. Let the user decide whether to create a commit; Agents do not commit by default.

## Files and Environment

- `data/` is runtime state; SQLite, WAL, and SHM files stay out of version control.
- `data/` is ignored runtime state. The database lives in `data/database/`,
  downloaded X and YouTube media live in `data/x/` and `data/youtube/`, profile
  candidates follow their source namespace, and uploads plus selected profile
  media live in `data/avatar/`. Do not add runtime files to Git; use the media
  tables and cache routes.
- `dist/`, `node_modules/`, and local test artifacts should not be committed.
- Fixed branding fallbacks live in `public/assets/`; changing platform media
  belongs in the ignored `data/x/` or `data/youtube/` folders. Profile
  candidates follow those source folders, while uploads and selected profile
  media belong in `data/avatar/`.
- `.env` is ignored and holds local runtime values. `.env.example` is the
  committed inventory and intentionally contains empty credential fields.
- Environment variables are for local runtime parameters. When adding one,
  update `.env.example`, `AGENTS.md`, and `docs/data-and-sync.md` with a
  non-secret example.
