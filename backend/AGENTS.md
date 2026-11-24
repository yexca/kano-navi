# Backend Agent Guide

The root [AGENTS.md](../AGENTS.md) invariants apply. Paths are relative to
`backend/`. Each architecture link below is an index; open only the page a
change needs.

## Workflows and Sources

- Timed workflows run only in the API process and share the single-flight
  queue. Fresh initialization keeps source snapshots and LLM providers, models
  and routes empty; seeding preserves existing snapshots and operator
  configuration.
- Historical source windows are inclusive Japan dates with bounded pages and
  atomic checkpoints, and never overwrite normal incremental cursors. Workflow
  source lookback days are independent of timer intervals.
- Schedule images need the source-text gate and image-model or manual
  approval. Pending images stay pending rather than being sent as text-only
  input.
- The LLM cancellation overlay may annotate a locked event without changing its
  schedule fields; only `manual_confirmed` is definitive cancellation.
- Model discovery is an operator action. Preserve each queue's provider and
  model order and its original text and image capability requirements.
- A new outbound path needs an explicit destination, timeout and response
  limits, a redirect policy, error handling and snapshot retention, following
  the existing source, downloader and provider protections in
  [External request rules](../docs/development/security.md#external-request-rules).
  Privacy allowlisting alone does not make a destination safe.

## Schema

`internal/store/sqlite` owns the schema (`bootstrap.sql`), additive upgrades
(`schema_upgrade.go`), static seeds and query behavior. A schema change updates
the data contract and its verification; see
[Seeds, schema changes and persistence](../docs/architecture/data-model/seeds-and-schema.md).

## Security

- Validate `ADMIN_PASSWORD` with at least 12 non-padding characters before
  opening SQLite.
- `/mcp` public reads need no key. Control requires its separate
  `MCP_CONTROL_TOKEN` and never grants manual confirmation, edit or delete,
  media selection or upload, SQL, arbitrary file access, or URL proxying.
- `LLM_SECRETS_KEY`, source API credentials and `MCP_CONTROL_TOKEN` stay in the
  environment; never expose provider plaintext or ciphertext. `OPENAI_API_KEY`
  is accepted only by the explicit `kano-go migrate-llm` command.
- `.env.example` is the complete variable inventory; document a new variable in
  [Configuration](../docs/operations/configuration.md) and its affected
  contract.

## Media

Source media is ignored runtime data in `data/x/`, `data/youtube/`,
`data/bilibili/` or `data/niconico/`, registered through `media_assets` and
`media_links`. Profile candidates follow their source; uploads and selected
profile media use `data/avatar/`. Serve only ready, contained, opaque-ID
assets. Packaged Journey, LINE BLOG and Works media follow
[scripts/AGENTS.md](../scripts/AGENTS.md).

## Which Page To Open

- Commands, startup, package ownership, static serving:
  [Backend](../docs/architecture/backend.md)
- The page-read and synchronization boundary:
  [Core boundaries](../docs/architecture/core-boundaries.md)
- Public reads, feeds, MCP: [Public API](../docs/architecture/public-api.md)
- `/api/admin`: [Admin API](../docs/architecture/admin-api.md)
- Tables, seeds, event precedence, merging:
  [Data model](../docs/architecture/data-model.md)
- A source, extraction, models, translation:
  [Sources](../docs/architecture/sources.md)
- Manual fetches, schedules, steps, runs:
  [Workflows](../docs/architecture/workflows.md)
- Adding a step or source:
  [Registering a step or source](../docs/architecture/workflows/run-execution.md#registering-a-step-or-source)
- Downloads, `/media`, resized copies:
  [Media cache](../docs/architecture/media-cache.md)
- Proxies and request signing:
  [Outbound proxies](../docs/architecture/outbound-proxy.md),
  [Web Bot Auth](../docs/architecture/web-bot-auth.md)
- Upgrades, backups, account exports:
  [Database](../docs/operations/database.md)
- Tests: [Test scope](../docs/development/testing/test-scope.md)
