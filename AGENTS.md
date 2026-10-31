# Agent Guide

This repository is an unofficial status board for Kano Mahoro (鹿乃まほろ).

Read these first:

- `README.md`
- `docs/how-it-works.md`
- `docs/architecture/core-boundaries.md`

Before changing an area, read its docs. Backend paths are relative to
`backend/`.

| Area                                  | Main paths                                                                                         | Docs                                                                                                |
| ------------------------------------- | -------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| Public UI and history                 | src/site/, src/dashboard/, src/history/, src/lineblog/, src/about/, src/components/ui/             | `docs/architecture/frontend.md`, `docs/development/design.md`, `docs/user/index.md`                 |
| Operator UI and requests              | src/admin/, src/admin/views/, src/admin/use-admin-data.ts                                          | `docs/architecture/frontend.md`, `docs/architecture/admin-api.md`                                   |
| Entry point, commands, image          | cmd/kano-go/, ../Dockerfile                                                                        | `docs/architecture/backend.md`, `docs/operations/docker.md`                                         |
| API, authentication, projections, MCP | internal/httpapi/, internal/adminapi/, internal/adminauth/, internal/mcpapi/, internal/publicview/ | `docs/architecture/backend.md`, `docs/architecture/public-api.md`, `docs/architecture/admin-api.md` |
| SQLite schema, upgrades, seeds        | internal/store/sqlite/ (bootstrap.sql, schema_upgrade.go, static-seed.json)                        | `docs/architecture/data-model.md`, `docs/operations/database.md`                                    |
| Sources and historical windows        | internal/source/x/, internal/source/youtube/, internal/source/history/                             | `docs/architecture/sources.md`, `docs/architecture/workflows.md`                                    |
| Jobs, steps, timers                   | internal/jobs/, internal/workflow/                                                                 | `docs/architecture/workflows.md`                                                                    |
| Model routing and extraction          | internal/schedule/, internal/llmadmin/, internal/secretstore/                                      | `docs/architecture/sources.md`, `docs/operations/configuration.md`                                  |
| Dynamic and profile media             | internal/mediadownload/, internal/contentadmin/, internal/httpapi/media.go                         | `docs/architecture/media-cache.md`                                                                  |
| Fixed history packaging               | ../scripts/package-history-media.ts, ../public/assets/history/                                     | `docs/development/history-archive.md`                                                               |
| LINE BLOG archive packaging           | ../scripts/package-lineblog.ts, ../src/lineblog/, internal/httpapi/lineblog.go                     | `docs/development/lineblog-archive.md`                                                              |
| Outbound HTTP, auth, secrets, privacy | any of the above                                                                                   | `SECURITY.md`, `docs/development/security.md`, `docs/operations/security.md`                        |
| Tests                                 | ../tests/, `*_test.go`                                                                             | `docs/development/testing.md`                                                                       |
| Commits and releases                  | package.json version, docs/history/                                                                | `docs/development/commit-and-release.md`                                                            |

## Product Boundaries

Critical rule:

```text
The browser reads a prepared SQLite snapshot through the local API. External
reads belong to server-side workflows and their helpers.
```

- The backend is the Go service in `backend/`; releases, development and CI
  run it. TypeScript is the frontend and the maintenance scripts only.
- Page loads, refreshes, and revision requests never fetch X, YouTube, or model
  providers. A failed source retains known snapshots and records its outcome.
- `src/main.tsx` composes `/`, `/history`, `/lineblog`, `/about`, and `/admin`. Keep board
  requests in `src/dashboard/use-dashboard.ts` and admin requests in
  `src/admin/use-admin-data.ts`. Reuse React, semantic Tailwind tokens, and the
  existing UI primitives.
- English is the base locale and the fallback for missing messages. Sync and
  extraction store codes or English text, never localized display strings; the
  frontend translates the codes (see the data model's stored event text).

## Data and Workflow Boundaries

- Manual event edits, confirmations, and deletions create durable locks or
  tombstones. Automatic sync/extraction must not replace or resurrect them.
  The LLM cancellation overlay may annotate a locked event without changing its
  schedule fields; only `manual_confirmed` is definitive cancellation.
- Schedule images require the source-text gate and image-model or manual
  approval. Pending images remain pending rather than being sent as text-only
  input.
- Historical source windows are inclusive Japan dates with bounded pages and
  atomic checkpoints. They must not overwrite normal incremental cursors.
  Workflow source lookback days are independent of timer intervals.
- Timed workflows run only in the API process and share the single-flight
  queue. Fresh initialization keeps source snapshots and LLM
  providers/models/routes empty. Seeding preserves existing snapshots and
  operator configuration.
- `internal/store/sqlite` owns the schema (`bootstrap.sql`), additive upgrades
  (`schema_upgrade.go`), static seeds and query behavior. Update the data
  contract and verification with schema changes; never silently drop columns
  or clear snapshots. Store parseable ISO timestamps and display Asia/Tokyo
  times.

## Security and Media Boundaries

- Production is the default. Validate `ADMIN_PASSWORD` with at least 12
  non-padding characters before opening SQLite. Only explicit
  `APP_MODE=development` bypasses admin authentication; `/admin` stays
  unlinked.
- Public HTTP and MCP responses use sanitized projections. Keep raw errors,
  source item IDs, counters, job IDs, `raw_json`, secrets, and absolute paths in
  their protected boundaries; never expose provider plaintext or ciphertext.
- `/mcp` public reads need no key. Control requires its separate
  `MCP_CONTROL_TOKEN` and never grants manual confirmation/edit/delete, media
  selection/upload, SQL, arbitrary file access, or URL proxying.
- `LLM_SECRETS_KEY`, source API credentials, and `MCP_CONTROL_TOKEN` stay in
  the environment. `OPENAI_API_KEY` is accepted only by the explicit
  `kano-go migrate-llm` command. Model discovery is an operator action;
  preserve the single queue's provider/model order and original text/image
  capability requirements.
- New outbound paths need explicit destinations, timeout and response limits,
  redirect policy, error handling, and snapshot retention. Follow the existing
  source/downloader/provider protections in
  [Secure development](docs/development/security.md#external-request-rules);
  privacy allowlisting alone does not make a destination safe.
- Source media is ignored runtime data in `data/x/` or `data/youtube/`,
  registered through `media_assets`/`media_links`. Profile candidates follow
  their source; uploads and selected profile media use `data/avatar/`. Serve
  only ready, contained opaque-ID assets.
- The verified fixed history collection is tracked in `public/assets/history/`.
  Preserve original bytes, catalog hashes, credits, and provenance when
  changing it. LINE BLOG archive media are ignored runtime data in
  `data/lineblog/`, packaged from the local recovery archive and listed in the
  tracked `src/lineblog/media-catalog.json`.

## UI and Test Contracts

- Follow `docs/development/design.md` for UI work. Check affected UI in a
  browser at desktop and mobile widths.
- Tests protect concrete behaviors and contracts. Choose the lowest sufficient
  layer and use synthetic records, reserved domains and synthetic URLs.

## Documentation

Docs describe the current system and its current decisions, not how it got
there. Where each topic belongs is in
[Documentation Rules](docs/README.md#documentation-rules).

- Write in present tense. Do not narrate change: no "previously", "no longer",
  "now", "as before", "was removed", "retired", "migration phase", version-dated
  asides, PR/issue/commit references, or before/after comparisons.
- When behavior changes, rewrite the affected section as if it had always been
  this way instead of appending a paragraph. Search for and remove every
  statement the change contradicts, including operations and development pages.
- Compatibility that still runs is current behavior. State it as a rule ("an
  `events` table without event sources is rebuilt on startup"), not as a story.
- History belongs only in `docs/history/` (release notes, with pending changes
  and per-release upgrade steps in `unreleased.md`) and in the context of ADRs
  in `docs/decisions/`. Follow
  [Release Notes](docs/development/commit-and-release.md#release-notes).
- Reference docs contain no TODOs, plans, roadmaps, or dated measurement logs.
- State each rule or fact in one place and link to it instead of restating it.
  Keep one topic per architecture document; add a new document to its index
  rather than growing an unrelated one.
- The [User guide](docs/user/index.md) and `README.md` describe features as
  visitors see them: no file paths, endpoints, schema, or implementation
  detail. When visitor-visible behavior changes, update the matching user page.
  How the system works belongs in [How it works](docs/how-it-works.md) and the
  architecture, operations, and development docs.
- Documentation, including `README.md`, is English only.
- `.env.example` owns the complete variable inventory; document new fields in
  [Configuration](docs/operations/configuration.md) and their affected
  contract.

## Validation Commands

The `Makefile` is the canonical validation entry point for local work and
GitHub Actions. Prefer its targets over reconstructing CI commands by hand.

- Begin with `git status --short` and preserve existing user changes. Do not
  use destructive reset or checkout commands.
- Use the smallest sufficient target: `make ci-fast` for ordinary local
  feedback, `make docs-check` for docs,
  `make ci-style` for style/tooling (including actionlint), `make ci-backend`
  for data/API/sync (golangci-lint, vet and race-enabled Go tests), and
  `make ci-frontend` for UI (type check, build and frontend tests).
- Use `make smoke` for seeded snapshot/API integration and `make docker-build`
  plus `make production-smoke` for runtime/container work. Smoke uses
  disposable state; do not run live synchronization to validate CI changes.
- `make check` runs installed-dependency checks without Docker; it needs Go as
  well as Node. `make ci-local` adds Docker build/runtime checks. `make ci`
  installs first and runs the full sequence. Use `make ci` for releases,
  major refactors, dependency or CI infrastructure changes, and other broad
  validation boundaries.
- Register new public hosts with a reason in `scripts/privacy-allowlist.json`.
  When the scanner reports a finding, fix the source; do not hide it with an
  allow comment or formatting change.

## Commits, Release and Handoff

- Before a routine commit, run `make ci-fast` and the smallest affected-area
  target, then run `make sensitive-check` and review scanner findings and
  `git diff --cached` manually. Run `make ci` before releases, major
  refactors, dependency or CI infrastructure changes, and other broad
  validation boundaries. Stage only the requested scope.
- Before handoff, run proportional validation and `make sensitive-check`.
- Never commit `.env`, `.npmrc`, credentials, personal paths, logs,
  databases/WAL, runtime media, or unrelated changes. Ignore rules are not a
  secrecy boundary.
- Agents create commits, amend/rebase, push, or rewrite history only when the
  user explicitly requests it. Follow
  [Commit and release](docs/development/commit-and-release.md) for the exact
  Conventional Commit format and publication rules.
- When merging into `main`, judge whether the branch's commits are worth
  keeping: squash merge when they are not (the usual case, and when unsure),
  and create a merge commit only when each commit is a focused, passing step
  worth reading or reverting on its own. See
  [Merging into Main](docs/development/commit-and-release.md#merging-into-main).
- Do not invent identifiers. Versions come from `package.json`; issue and PR
  numbers and release tags come from the repository or the user.
