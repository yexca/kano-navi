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

## Data and API

The SQLite file is `data/kano.sqlite`. When the server starts, it creates the
schema and fills missing tables with the initial snapshot in
`server/seed-data.js`. The main tables are `profiles`, `posts`, `events`,
`videos`, `focus`, `timeline`, `resources`, `assets`, `media_assets`,
`media_links`, and `sync_runs`.

Dashboard endpoint:

```text
GET /api/dashboard?days=3
```

The response contains the profile, recent X posts, all events, YouTube videos
and scheduled streams, the latest focus item, timeline, resource links, schedule
images, and synchronization metadata. `days` can be set from 1 to 30.

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

- reads public profile status IDs and public status JSON for X;
- parses channel RSS and scheduled-video metadata for YouTube.

Failures do not clear existing data. They are recorded in `sync_runs` together
with the failure reason. Discovered media is registered during the same sync,
but downloading is a separate cache-worker step. You can select a source or
adjust the timeout with:

```bash
X_HANDLE=kano_2525 YOUTUBE_CHANNEL_ID=UCShXNLMXCfstmWKH_q86B8w npm run sync
SKIP_X=1 npm run sync
SKIP_YOUTUBE=1 npm run sync
```

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
