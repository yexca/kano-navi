# Go workflow execution core

Phase nine adds a persistent execution core beside the TypeScript owner. It is
an execution library and read API; it does not call X, YouTube, a CDN, an LLM
provider, or MCP.

## Ownership and compatibility

TypeScript still owns source synchronization, schedule extraction, LLM routing,
media downloading, MCP control, and the production scheduler on `:7657`. The
Go service keeps the existing `workflows` definition and `sync_runs` history
compatible, while new workflow execution state lives in additive tables. A
future adapter can use the same executor without changing HTTP handlers.

The workflow tables are created only by the explicit development
`OpenWorkflowCore` opener. Migration version `1` is recorded in
`workflow_migrations` in the same transaction as the DDL. Existing
snapshot/event/media rows are never rewritten; failure rolls the transaction
back. The normal public read opener remains read-only and never migrates.

## Model

`workflows` is the saved definition and timer metadata. `workflow_runs` is
one operator, scheduler, or recovery execution. `workflow_run_steps` stores
ordered steps and an explicit `capability` (`synthetic`, `x_recent`, or
`capability_not_migrated`). `workflow_step_attempts` stores safe error
codes, retryability and bounded timing metadata for every try.
`workflow_leases` is the global single-flight lease; SQLite's partial unique
index also prevents two queued/running runs.

Run and step statuses are fixed enums: `queued`, `running`, `succeeded`,
`partial`, `failed`, `cancelled`, and `blocked`; steps additionally use
`pending`. Trigger source is always `operator`, `scheduler`, or
`recovery`. Idempotency keys are hashed before storage. Public admin
projections omit raw payloads, stack traces, paths, credentials and provider
text.

## Lease, retry, and recovery

Claiming a run takes the SQLite write lock, changes it to `running`, records an
expiry and owner, and reserves the global lease. Completion fences every step
and run write against that owner, generation, and expiry. A retryable typed
error is limited to timeout, rate-limited, or dependency-unavailable classes;
permanent, schema/validation, cancelled, and blocked errors finish without a
retry. Expired leases mark active attempts `lease_expired`, return the run to
`queued`, increment `recovery_count`, and preserve the attempt history.

The default scheduler is disabled. `Scheduler.Start` only starts a timer when
`Enabled` is explicitly true; `Tick` reads due definitions and submits them
to the same executor. An active run produces a durable busy result and remains
due for a later tick. Timer intervals are clamped to the TypeScript
15-minute to seven-day range.

## Rollback and boundary

To roll back, stop the Go executor and continue using the TypeScript workflow
owner. No production route or production database is changed in this phase.
Only synthetic executors are used until the X/YouTube adapter migration is
approved.
