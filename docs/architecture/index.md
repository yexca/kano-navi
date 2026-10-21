# Architecture

Read core boundaries before changing the relationship between page reads, synchronization, SQLite, and external sources.

- [Core boundaries](core-boundaries.md)
- [Frontend](frontend.md)
- [Backend and HTTP](backend.md)
- [Admin API](admin-api.md): the `/api/admin` contract used by the console
- [Go backend](go-standalone.md): commands, startup, route ownership, rollback
- Migration record: [phases](go-migration.md),
  [workflow core](go-workflows.md), [X recent sync](go-x-sync.md),
  [source workflows and media downloads](go-source-workflows.md)
- [Data model and event precedence](data-model.md)
- [Sources and schedule extraction](sources.md)
- [Workflows and historical fetch windows](workflows.md)
- [Media cache](media-cache.md)
- [Architecture decisions](../decisions/index.md)

[Documentation index](../README.md)
