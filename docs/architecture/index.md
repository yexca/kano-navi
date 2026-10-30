# Architecture

Kano Navi is organized around a single rule:

```text
Page reads serve a prepared SQLite snapshot; only server-side workflows reach
external sources.
```

## Architecture Topics

| Topic                                 | Contents                                                           |
| ------------------------------------- | ------------------------------------------------------------------ |
| [Core boundaries](core-boundaries.md) | System shape, request and failure flow, extension points           |
| [Backend](backend.md)                 | Go service, commands, startup, package ownership, static serving   |
| [Public API](public-api.md)           | Health, dashboard, calendar, archives, feeds, MCP, WebSub callback |
| [Admin API](admin-api.md)             | The `/api/admin` contract used by the console                      |
| [Frontend](frontend.md)               | Pages, request hooks, components, localization                     |
| [Data model](data-model.md)           | Schema, seeds, event precedence, locks and tombstones              |
| [Sources](sources.md)                 | X and YouTube adapters and schedule extraction                     |
| [Workflows](workflows.md)             | Step catalog, single-flight queue, timers, manual fetches          |
| [Media cache](media-cache.md)         | Media identity, downloader, layout, HTTP contract                  |

## When To Read What

- Read [Core boundaries](core-boundaries.md) before changing the relationship
  between page reads, synchronization, SQLite, and external sources.
- Read [Data model](data-model.md) before changing `bootstrap.sql`,
  `schema_upgrade.go`, seeds, or event precedence.
- Read [Sources](sources.md) and [Workflows](workflows.md) before changing
  fetching, extraction, timers, or historical windows.
- Read [Media cache](media-cache.md) before changing downloads or `/media`.
- Read [Backend](backend.md), [Public API](public-api.md),
  [Admin API](admin-api.md) and [Frontend](frontend.md) before changing module
  layout or API/UI responsibilities.

## Runtime Topology

```text
Browser
  -> React frontend (src/)
  -> Go HTTP API, MCP and static files (kano-go serve)
  -> SQLite snapshot (data/database/kano.sqlite) and cached media (data/)
  <- workflow steps: X, YouTube, media downloads, LLM providers
```

## Related Docs

- [Architecture decisions](../decisions/index.md)
- [Documentation index](../README.md)
