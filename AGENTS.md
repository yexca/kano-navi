# Agent Guide

This repository contains an unofficial status board for Kano Mahoro
(鹿乃まほろ). Read this guide first, then the focused documentation for the
area you are changing.

## Read First

- [Project overview](docs/overview.md)
- [Core boundaries](docs/architecture/core-boundaries.md)
- [Data model](docs/architecture/data-model.md)
- [Sources and schedule extraction](docs/architecture/sources.md)
- [Workflows and fetch windows](docs/architecture/workflows.md)
- [Backend](docs/architecture/backend.md) and [Go backend](docs/architecture/go-standalone.md), or [Frontend](docs/architecture/frontend.md)
- [Design](docs/development/design.md) for UI work
- [Testing](docs/development/testing.md)
- [Security policy](SECURITY.md) and [Secure development](docs/development/security.md)
- [Documentation index](docs/README.md) for operations and other topics

## Product and Data Boundaries

The browser reads a prepared SQLite snapshot through the local API.
External reads belong to server-side synchronization and its helpers.
The backend is the Go service in backend-go/; releases, development and CI run
it. The TypeScript files in server/ and the sync/seed scripts are the previous
implementation, kept as the rollback path and comparison reference. Change
behavior in Go first and keep the TypeScript reference and its tests passing.

- Page loads, refreshes, and revision requests never fetch X, YouTube, or model
  providers. A failed source retains known snapshots and records its outcome.
- src/main.tsx composes /, /history, /about, and /admin. Keep board requests in
  src/dashboard/use-dashboard.ts and admin requests in src/admin/use-admin-data.ts.
  Reuse React, semantic Tailwind tokens, and the existing UI primitives.
- Manual event edits, confirmations, and deletions create durable locks or
  tombstones. Automatic sync/extraction must not replace or resurrect them.
  The LLM cancellation overlay may annotate a locked event without changing
  its schedule fields; only manual_confirmed is definitive cancellation.
- Schedule images require the source-text gate and image-model or manual approval.
  Pending images remain pending rather than being sent as text-only input.
- Historical source windows are inclusive Japan dates with bounded pages and
  atomic checkpoints. They must not overwrite normal incremental cursors.
  Workflow source lookback days are independent of timer intervals.
- Timed workflows run only in the API process and share the single-flight queue.
  Fresh initialization keeps source snapshots and LLM providers/models/routes
  empty. Seeding preserves existing snapshots and operator configuration.
- backend-go/internal/store/sqlite owns the schema (bootstrap.sql), additive
  upgrades (schema_upgrade.go), static seeds and query behavior. Update the
  data contract and verification with schema changes; never silently drop
  columns or clear snapshots. Store parseable ISO timestamps and display
  Asia/Tokyo times.

## Security and Media Boundaries

- Production is the default. Validate ADMIN_PASSWORD with at least 12
  non-padding characters before opening SQLite. Only explicit
  APP_MODE=development bypasses admin authentication; /admin stays unlinked.
- Public HTTP and MCP responses use sanitized projections. Keep raw errors,
  source item IDs, counters, job IDs, raw_json, secrets, and absolute paths in
  their protected boundaries; never expose provider plaintext or ciphertext.
- /mcp public reads need no key. Control requires its separate MCP_CONTROL_TOKEN
  and never grants manual confirmation/edit/delete, media selection/upload,
  SQL, arbitrary file access, or URL proxying.
- LLM_SECRETS_KEY, source API credentials, and MCP_CONTROL_TOKEN stay in the
  environment. OPENAI_API_KEY is accepted only by the explicit
  `kano-go migrate-llm` command.
  Model discovery is an operator action; preserve per-route provider/model order
  and original text/image capability requirements.
- New outbound paths need explicit destinations, timeout and response limits,
  redirect policy, error handling, and snapshot retention. Follow the existing
  source/downloader/provider protections; privacy allowlisting alone does not
  make a destination safe.
- Register new public hosts with a reason in scripts/privacy-allowlist.json.
  When the scanner reports a finding, fix the source; do not hide it with an
  allow comment or formatting change.
- Source media is ignored runtime data in data/x/ or data/youtube/, registered
  through media_assets/media_links. Profile candidates follow their source;
  uploads and selected profile media use data/avatar/. Serve only ready,
  contained opaque-ID assets. See [Media cache](docs/architecture/media-cache.md).
- The verified fixed history collection is tracked in public/assets/history/.
  Preserve original bytes, catalog hashes, credits, and provenance when changing it.

## Code Ownership

Backend paths are relative to backend-go/.

| Area                             | Main paths                                                                                         |
| -------------------------------- | -------------------------------------------------------------------------------------------------- |
| Public UI and history            | src/dashboard/, src/history/, src/about/, src/components/ui/                                       |
| Operator UI and requests         | src/admin/, src/admin/views/, src/admin/use-admin-data.ts                                          |
| Entry point and commands         | cmd/kano-go/ (serve, seed, sync, migrate-llm, healthcheck), ../Dockerfile                          |
| API, authentication, projections | internal/httpapi/, internal/adminauth/, internal/mcpapi/, internal/publicview/                     |
| SQLite and static initialization | internal/store/sqlite/ (bootstrap.sql, static-seed.json, schema_upgrade.go)                        |
| Sources and historical windows   | internal/source/x/, internal/source/youtube/, internal/source/history/                             |
| Jobs, steps, timers              | internal/jobs/, internal/workflow/                                                                 |
| Model routing and extraction     | internal/schedule/, internal/llmadmin/, internal/secretstore/                                      |
| Dynamic and profile media        | internal/mediadownload/, internal/contentadmin/, internal/httpapi/media.go                         |
| Fixed history packaging          | ../scripts/package-history-media.ts, ../scripts/import-history-media.ts, ../public/assets/history/ |
| TypeScript reference (legacy)    | ../server/, ../scripts/sync.ts, ../scripts/seed.ts, ../ts.Dockerfile                               |

## Validation and Handoff

The Makefile is the canonical validation entry point for local work and Actions.

- Begin with git status --short and preserve existing user changes. Do not use
  destructive reset or checkout commands.
- Choose the smallest sufficient target: make docs-check for docs, make ci-style
  for style/tooling, make go-test for Go-only changes, make ci-backend for
  data/API/sync (Go and the TypeScript reference), and make ci-frontend for UI.
  Check affected UI in a browser at desktop and mobile widths.
- Use make smoke for seeded snapshot/API integration and make docker-build plus
  make production-smoke for runtime/container work. Smoke uses disposable state;
  do not run live synchronization to validate CI changes.
- make check runs installed-dependency checks without Docker; it needs Go as
  well as Node. make ci-local adds Docker build/runtime checks. make ci
  installs first and runs the full sequence.
- Before every commit, run make ci and make sensitive-check and review scanner
  findings and git diff --cached manually. Stage only the requested scope.
- Before handoff, run proportional validation and make sensitive-check. Tests
  protect concrete behaviors and contracts and use synthetic records and URLs.
- Never commit .env, .npmrc, credentials, personal paths, logs, databases/WAL,
  runtime media, or unrelated changes. Ignore rules are not a secrecy boundary.
- Agents create commits, amend/rebase, push, or rewrite history only when the user
  explicitly requests it. Follow [Commit and release](docs/development/commit-and-release.md)
  for the exact Conventional Commit format and publication rules.

## Documentation Maintenance

Use architecture for contracts, product for page behavior, operations for
runtime/recovery, development for checks and contribution, and ADRs for durable
choices. .env.example owns the complete variable inventory; document new fields
in [Configuration](docs/operations/configuration.md) and their affected contract.
Old flat docs are navigation pages. Update focused documents and their indexes
instead of duplicating contracts in this guide.
