# Documentation Index

These documents describe the Kano Mahoro status board for Agents, developers,
and maintainers. They are organized around understanding the project, changing
code, maintaining data, and reviewing security.

## Start Here

- [Project overview](overview.md)
- [Architecture](architecture.md)
- [Local development](development.md)
- [Data and synchronization](data-and-sync.md)
- [Media cache](media-cache.md)
- [Security and privacy](security.md)
- [SQLite-first architecture decision](decisions/ADR-0001-sqlite-first.md)
- [Root Agent guide](../AGENTS.md)

## Reading by Task

| Task | Recommended reading |
| --- | --- |
| Taking over the project | `overview.md`, `architecture.md` |
| Changing the page or components | `architecture.md`, `development.md` |
| Adjusting synchronization or data | `data-and-sync.md`, `media-cache.md`, `security.md` |
| Preparing a merge or release | `development.md`, `security.md`, `AGENTS.md` |

## Current Implementation at a Glance

- Stack: React 19, Vite, Tailwind CSS, shadcn/ui-style components, Express 5,
  and better-sqlite3.
- Page data comes from the local `data/kano.sqlite` snapshot. An empty database
  is initialized with the public snapshot in `server/seed-data.js`.
- `scripts/sync.mjs` reads public X pages/APIs and YouTube RSS/pages to collect
  updates and scheduled streams.
- `server/media-cache.js` defines the ignored runtime media store, safe path
  helpers, source identities, and atomic-write primitive.
- CI runs server/cache and sensitive-scanner tests, checks documentation and
  sensitive information, builds the frontend, and runs an API smoke check. It
  does not fetch live platform data.

## Documentation Conventions

- User-facing page behavior belongs in the overview or architecture document.
- Runtime commands belong in the development guide.
- Sources, fields, and freshness rules belong in the data and synchronization document.
- Security boundaries and scanner rules belong in the security document.
- Durable trade-offs should be recorded as an ADR.
- Paths in examples are relative to the repository root. Examples must not contain
  personal machine paths or real credentials.
