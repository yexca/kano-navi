# Agent Guide

This repository contains an unofficial status board for Kano Mahoro (鹿乃まほろ).
This file is the entry point for Agents and developers working in the repository.
Read it first, then use the focused documents in `docs/` for more detail.

## Read First

- [Project overview](docs/overview.md): product goals, scope, and current status.
- [Architecture](docs/architecture.md): boundaries between the browser, API, SQLite, and synchronization.
- [Development guide](docs/development.md): local setup, checks, and change workflow.
- [Data and synchronization](docs/data-and-sync.md): tables, sources, snapshots, and failure behavior.
- [Media cache](docs/media-cache.md): runtime layout, media lifecycle, and HTTP contract.
- [Security](docs/security.md): sensitive information, external links, and CI checks.
- [Documentation index](docs/README.md): the complete reading map.
- [Chinese README](README.zh-cn.md): the Chinese project introduction.

## Product Boundaries

- This is a fan-made, read-mostly status board. It is not operated by Kano Mahoro or any affiliated organization.
- The frontend uses React, Vite, and shadcn/ui-style components. It only calls the local API; it must not fetch X, YouTube, or other platforms directly.
- The Express service reads SQLite and exposes `/api/health`, `/api/dashboard`,
  guarded `/api/admin/*` endpoints, and the guarded `/media/:id` cache route.
- External reads belong to the synchronization path in `scripts/sync.mjs` and
  its server-side media/schedule helpers. The results are stored as SQLite
  snapshots; opening the page or clicking refresh must not trigger an external fetch.
- If a source fails or is temporarily unavailable, retain the existing snapshot and record the outcome in `sync_runs`. Never replace known data with an empty result just because a fetch failed.
- Public platform links are product data. When adding an external host, document the reason in `scripts/privacy-allowlist.json` and make the sensitive-information scan pass.

## Code Map

| Path                           | Responsibility                                                  |
| ------------------------------ | --------------------------------------------------------------- |
| `src/main.jsx`                 | Dashboard page, interactions, and API data mapping              |
| `src/admin.jsx`                | Hidden `/admin` configuration and schedule editor               |
| `src/index.css`                | Global design tokens, layout, and responsive styling            |
| `src/admin.css`                | Admin-specific responsive layout                                |
| `src/components/ui/`           | Reusable shadcn/ui-style primitives                             |
| `server/database.js`           | SQLite schema, seeding, upserts, and queries                    |
| `server/media-cache.js`        | Runtime media paths, identities, and atomic-write helpers       |
| `server/media-downloader.js`   | Bounded X/YouTube image downloader                              |
| `server/schedule-extractor.js` | OpenAI structured schedule extraction                           |
| `server/admin-api.js`          | Authenticated model/schedule/video configuration and event CRUD |
| `server/admin-auth.js`         | Development bypass and production session authentication        |
| `server/app.js`                | Testable Express application, APIs, and guarded media route     |
| `server/index.js`              | Runtime database and HTTP listener assembly                     |
| `server/seed-data.js`          | Initial public snapshot and resource directory                  |
| `scripts/sync.mjs`             | Server-side X and YouTube synchronization adapters              |
| `scripts/seed.mjs`             | Idempotent initial snapshot seeding                             |
| `public/assets/`               | Tracked fixed branding fallbacks                                |
| `data/`                        | Local runtime SQLite and ignored media cache files              |
| `docs/`                        | Documentation for Agents, developers, and maintainers           |

## Data Contract

- `GET /api/health` returns service status, the database path relative to the project, and the latest synchronization summary.
- `GET /api/dashboard?days=3` returns the profile, aggregated posts in the requested window, all events, videos, the manually selected focus item, the timeline, resource links, image assets, media-cache status, and synchronization metadata (including configured X accounts and the Featured video ID). The server clamps `days` to 1 through 30.
- `GET /media/<opaque-id>` serves a cached file only when its database row is `ready` and its resolved path remains below `data/cache/media/`; invalid or unready IDs return `404`.
- `/api/admin/*` is password-free only when `APP_MODE=development`. Production
  requires `ADMIN_PASSWORD` with at least 12 characters and uses an HttpOnly
  session cookie. The `/admin` page is intentionally absent from public navigation.
- Timestamps are stored as parseable ISO 8601 strings. The display layer formats them in `Asia/Tokyo`.
- Manual event edits, confirmations, and deletions set a durable lock. Source
  synchronization and OpenAI extraction must not overwrite or resurrect them.
- X posts carry `accountHandle` so the public feed can identify the source
  account without exposing the internal classification used by extraction.
- The current tables are created by the schema constant in `server/database.js`. When changing the schema, update the documentation, seed data, and verification steps together. Do not silently drop columns or clear snapshots.

## Common Commands

```bash
npm install              # Install dependencies for the first run
npm run dev              # Start Vite and Express
npm run build            # Create the production bundle
npm run seed             # Add missing seed records
npm run sync             # Read public sources and update SQLite
npm run test:server      # Test SQLite and media-cache contracts
make check-sensitive     # Scan for sensitive information
make check-docs          # Check local Markdown links
make ci                  # Run the full local CI check
```

Synchronization accepts the source, bootstrap, request-budget, media-limit,
and skip variables documented in `.env.example`. `OPENAI_API_KEY` is the only
credential used by synchronization: it stays in the process environment and
must never be written to source code, SQLite, a URL, API output, or a log.

## Change and Verification Rules

- Begin with `git status --short` and preserve existing user changes. Do not use destructive reset or checkout commands.
- Follow the existing React, Tailwind token, and UI-component patterns. Do not add a new state or request layer for a one-off page change.
- New external requests belong in the server-side synchronization scripts and must have a timeout, error handling, and snapshot-retention behavior.
- Remote images belong in `data/cache/` after synchronization; register them with `media_assets`/`media_links` and do not commit downloaded files.
- For UI changes, at minimum run `npm run build`. For data or API changes, also run `npm run seed` and a health check or relevant script. For documentation changes, run `make check-docs`.
- Before a commit, run `make ci` and `make check-sensitive`, then review the scanner output manually. CI must not depend on live X or YouTube requests.
- An Agent must not create commits, push, or rewrite someone else's changes unless the user explicitly asks for it.

## Commit Convention

- Use Conventional Commits with the exact subject form `<type>(<scope>): <description>`.
- Use one of `feat`, `fix`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`, `perf`, `style`, or `revert` as the type.
- Keep the scope short, lowercase, and tied to the primary subsystem, such as `app`, `media`, `sync`, `docs`, or `ci`.
- Write the description in lowercase imperative language, keep the subject at 72 characters or fewer, and do not end it with a period.
- Keep each commit focused on one coherent change. Use a body when the reason or migration behavior is not evident from the subject.
- Mark an incompatible change with `!` before the colon and a `BREAKING CHANGE:` footer.
- Before committing, run the required checks, review `git diff --cached`, and confirm that staged files match the user's requested boundary. Never stage ignored runtime data, secrets, or unrelated user changes.
- Create, amend, rebase, or otherwise rewrite a commit only when the user explicitly requests it.

## Privacy and Security Baseline

- Never commit `.env`, `.npmrc`, private keys, access tokens, personal paths, runtime logs, or SQLite/WAL files.
- `.gitignore` is a convenience, not a confidentiality boundary; sensitive files are still rejected by the scanner.
- Use `example.invalid`, placeholders, and synthetic values in tests and documentation. Real public resource links may remain, but every new host needs an allowlist reason.
- When the scanner reports a finding, fix the source content. Do not suppress it with an `allow` comment or a formatting change.
