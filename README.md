# Kano Mahoro / Status Board

An unofficial fan-made status board built with React, Vite, and shadcn/ui-style
components. The page reads a local SQLite snapshot and never fetches X or
YouTube directly from the browser.

## Documentation

- [Agent guide](AGENTS.md): code boundaries, data contracts, and collaboration rules.
- [Documentation index](docs/README.md): overview, architecture, development, synchronization, and security.
- [Security policy](SECURITY.md): sensitive information and reporting boundaries.
- [Chinese README](README.zh-cn.md): the Chinese project introduction.

## Local Run

```bash
npm install
cp .env.example .env
npm run dev
```

Development mode starts both services:

- Vite frontend: `http://localhost:5173`
- Express API: `http://localhost:8787`

For a production build and server:

```bash
npm run build
npm start
```

To run the production container, set `ADMIN_PASSWORD` (at least 12 characters)
in a `.env` file and start the root Compose stack:

```bash
docker compose up -d --build
```

The stack keeps the SQLite database and downloaded media in `./data`. Set
`KANO_IMAGE=ghcr.io/yexca/kano-navi:latest` when using a published release
image instead of building locally.

The hidden administration page is available only by entering `/admin`
directly; it is not linked from the dashboard. `APP_MODE=development` bypasses
login. Before `npm start`, set `APP_MODE=production` and an
`ADMIN_PASSWORD` of at least 12 characters in the ignored `.env` file.
Provider API keys are managed in `/admin` and encrypted in SQLite. Set the
environment-only `LLM_SECRETS_KEY` before saving or using a provider. A legacy
`OPENAI_API_KEY` can be imported once with `npm run migrate:llm`, then removed.
The console opens on a one-click workflow page: choose which modules to update
(X, YouTube, media cache, AI schedule scan), run them now, or let the server run
a saved workflow on a timer. Other sections cover paginated schedules,
detection rules with per-route model order, a central LLM provider page (enter
an API host and key, fetch the model list, and tag models), and content/profile
media.

`/mcp` is a stateless integration endpoint. Public read tools work without a
key; only revision requests, synchronization, and automatic scans require
`Authorization: Bearer <MCP_CONTROL_TOKEN>`. The MCP token is separate from
the admin password and never grants event confirmation, editing, deletion, or
profile-media selection.

## Data and API

The SQLite file is `data/database/kano.sqlite`. When the server starts, it creates the
schema and fills missing tables with the initial snapshot in
`server/seed-data.js`. The main tables are `profiles`, `posts`, `events`,
`videos`, `focus`, `timeline`, `resources`, `assets`, `media_assets`,
`media_links`, `sync_runs`, `sync_state`, `event_sources`, `app_settings`,
`schedule_extractions`, `llm_providers`, `llm_models`, `llm_route_targets`
(with the migrated legacy `llm_route_providers`), and `workflows`.

Dashboard endpoint:

```text
GET /api/dashboard?days=3
```

The response contains the profile, a `summary` block (next event, next
scheduled stream, latest video, latest post even outside the window, and
counts), recent posts aggregated from both configured X accounts, all events, YouTube videos and scheduled streams, the manually
selected Featured item, timeline, resource links, schedule images, and
synchronization metadata. `days` can be set from 1 to 30.

Remote images are registered in `media_assets` and linked through
`media_links`. Ready files are exposed through a versioned
`GET /media/<opaque-id>?v=<content-sha256>` URL; pending or failed files are
returned as `null` media URLs so the browser does not make a direct CDN request.
SQLite and downloaded media live under the ignored `data/` runtime directory:
`data/database/`, `data/x/`, `data/youtube/`, and `data/avatar/`. New media is
stored content-addressably under its source namespace; the old cache layout is
read only for local migration compatibility.

Health endpoint:

```text
GET /api/health
```

## Updating the Snapshot

```bash
npm run sync
```

The server-side synchronization script:

- bootstraps at most seven days of posts for each configured X account, then
  requests only unknown status IDs plus a small configurable refresh budget;
- stores the latest six RSS videos on the first YouTube run, then only newer
  entries, while continuing to recheck active reservations;
- downloads allowlisted X images and YouTube thumbnails into the runtime cache;
- selects schedule-board candidates with the separately configured keyword
  stage, then sends their text and cached images to the ordered board provider
  route for strict structured extraction;
- also applies a lightweight date/notice heuristic to individual X messages and
  sends suspected schedule notices to the separate message provider route;
- filters each route by the original post modality: text requires `Text`, an
  image requires `Image`, and text plus image requires both capabilities. Each
  provider is attempted at most three times before the next compatible provider
  in the priority order is tried. Providers can use OpenAI Responses or Chat
  Completions.

The board and message detectors share the scan limit and event snapshot. A
model response classified as `uncertain` is cached for inspection but does not
create an event. `SCHEDULE_MESSAGE_ENABLED` controls the single-message stage;
the existing board keyword and board-extraction switches remain independent
controls.

Failures do not clear existing data. They are recorded in `sync_runs` together
with the failure reason. Invalid model output also leaves the previous schedule
intact. The legacy API key is read only from the process environment. Keys
entered for additional providers are encrypted with the environment-only
`LLM_SECRETS_KEY` master key and are never returned by the API. You can select
a source or adjust the timeout with:

```bash
X_HANDLES=kano_2525,_Kanotic YOUTUBE_CHANNEL_ID=UCShXNLMXCfstmWKH_q86B8w npm run sync
SKIP_X=1 npm run sync
SKIP_YOUTUBE=1 npm run sync
```

See [.env.example](.env.example) for bootstrap windows, request budgets, media
limits, and per-stage skip flags.

The admin page can create, edit, confirm, and delete events. Any such operation
marks the event as manually confirmed and locks it against later source or LLM
updates. The public calendar labels automatic and manually confirmed entries.
MCP automatic jobs never perform those human operations; they return an
asynchronous job ID, and completion advances the dashboard revision so an open
public page can reread its stored snapshot.

Public X and YouTube pages can be rate-limited, require a login, or change
their structure, so the original platform page remains the source of truth. The
page's refresh action only reads the SQLite API; it does not start a remote
fetch.

To keep snapshots current, run `npm run sync` from cron, launchd, or another
external scheduler daily or several times a day. The dashboard service itself
does not fetch external platforms when a visitor opens the page.

## Re-seeding Initial Data

```bash
npm run seed
```

This command idempotently adds missing seed records. To overwrite records with
the same IDs, use:

```bash
npm run seed -- --overwrite
```

`public/assets/` contains only fixed branding fallbacks. Changing thumbnails
and schedule images belong in the ignored `data/x/` or `data/youtube/` folders;
discovered profile candidates follow their source namespace, while uploaded and
selected profile media belongs in `data/avatar/`. This is a fan-made project,
and resource links point to the original public pages.

## Local Checks

```bash
make check-sensitive
make check-docs
make ci
```

`make ci` installs the locked dependencies, runs the sensitive-information and
documentation checks, runs the SQLite/media-cache tests, and verifies the
production build. It does not run a live X or YouTube synchronization.
