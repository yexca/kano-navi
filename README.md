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

The hidden administration page is available only by entering `/admin`
directly; it is not linked from the dashboard. `APP_MODE=development` bypasses
login. Before `npm start`, set `APP_MODE=production` and an
`ADMIN_PASSWORD` of at least 12 characters in the ignored `.env` file.
`OPENAI_API_KEY` may remain empty until automatic schedule extraction is needed.

## Data and API

The SQLite file is `data/kano.sqlite`. When the server starts, it creates the
schema and fills missing tables with the initial snapshot in
`server/seed-data.js`. The main tables are `profiles`, `posts`, `events`,
`videos`, `focus`, `timeline`, `resources`, `assets`, `media_assets`,
`media_links`, `sync_runs`, `sync_state`, `event_sources`, `app_settings`, and
`schedule_extractions`.

Dashboard endpoint:

```text
GET /api/dashboard?days=3
```

The response contains the profile, recent posts aggregated from both configured
X accounts, all events, YouTube videos and scheduled streams, the manually
selected Featured item, timeline, resource links, schedule images, and
synchronization metadata. `days` can be set from 1 to 30.

Remote images are registered in `media_assets` and linked through
`media_links`. Ready files are exposed through a versioned
`GET /media/<opaque-id>?v=<content-sha256>` URL; pending or failed files are
returned as `null` media URLs so the browser does not make a direct CDN request.
The runtime files live under the ignored `data/cache/` directory.

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
- sends posts matching the separately configured schedule keywords and cached
  images to the OpenAI Responses API for strict structured extraction when
  `OPENAI_API_KEY` is configured.

Failures do not clear existing data. They are recorded in `sync_runs` together
with the failure reason. Invalid model output also leaves the previous schedule
intact. The API key is read only from the process environment; the admin page
stores only non-secret settings, including the model, schedule keywords, and
Featured video. You can select a source or adjust the
timeout with:

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
and schedule images belong in the ignored runtime cache. This is a fan-made
project, and resource links point to the original public pages.

## Local Checks

```bash
make check-sensitive
make check-docs
make ci
```

`make ci` installs the locked dependencies, runs the sensitive-information and
documentation checks, runs the SQLite/media-cache tests, and verifies the
production build. It does not run a live X or YouTube synchronization.
