# Local Development

## Prerequisites

- Node.js 20.19+ (or a currently supported LTS release)
- npm 10+
- Git

SQLite uses `better-sqlite3`; installation may require a prebuilt package or a
local compiler toolchain on the current platform.

## Start and Build

```bash
npm install
npm run dev
```

Development mode starts Vite (default `http://localhost:5173`) and the Express
API (default `http://localhost:8787`). For production:

Vite proxies both `/api` and `/media` to Express, so cached media has the same
browser URL in development and production.

```bash
npm run build
npm start
```

The Makefile provides the same entry points. Run `make help` to list targets;
`make dev` starts development and `make ci` runs the complete local check.

## Data Commands

```bash
npm run seed                 # Idempotently add missing initial records
npm run seed -- --overwrite # Overwrite records with matching IDs from seed-data
npm run sync                 # Update the snapshot from public sources
```

Synchronization accesses the network and is therefore not part of the default
CI build. To debug one source, set `SKIP_X=1` or `SKIP_YOUTUBE=1`; never put a
temporary credential in shell history, source code, or the database.

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
| `npm run test:server`      | Test SQLite and media-cache contracts                  |

For UI changes, an additional browser check is useful. For API or database
changes, request `http://localhost:8787/api/health` and
`/api/dashboard?days=3` and verify the fields and stale-snapshot behavior.

## Suggested Workflow

1. Read `AGENTS.md` and the relevant topic document, then inspect `git status --short`.
2. Keep the change focused and follow existing names and component boundaries.
3. Run the smallest relevant check first, then run `make ci`.
4. Review the sensitive-information and documentation-link output.
5. Let the user decide whether to create a commit; Agents do not commit by default.

## Files and Environment

- `data/` is runtime state; SQLite, WAL, and SHM files stay out of version control.
- `data/cache/` is ignored runtime media state. Do not add downloaded thumbnails
  or schedule images to Git; use the media tables and cache route.
- `dist/`, `node_modules/`, and local test artifacts should not be committed.
- Fixed branding fallbacks live in `public/assets/`; changing platform media
  belongs in the ignored runtime cache.
- Environment variables are for local runtime parameters. When adding one, update `AGENTS.md` and `docs/data-and-sync.md` with a non-secret example.
