# Run Execution

How runs execute in the single-flight queue and which packages own them.

Every run is a durable `workflow_runs` row with its ordered
`workflow_run_steps` and one `workflow_step_attempts` row per try; the
console's job list and the `sync_runs` history are projections of them (see
[Backend](../backend.md#jobs-and-workflows) for the reserved run IDs). A run
freezes what its steps read: `fetch_windows_json` for operator windows,
`source_sync_json` for schedule sync settings, `source_chain_json` for their
method chains and `source_lookback_json` for per-step days. Retry and recovery therefore repeat the same work without a
saved definition.

Statuses are fixed enums. Runs use `queued`, `running`, `succeeded`,
`partial`, `failed`, `cancelled` and `blocked`; steps use the same set with
`pending` in place of `queued`; attempts use `running` and the terminal
values. `trigger_source` and `execution_source` are `operator`, `scheduler`
or `recovery`. Idempotency keys are stored as SHA-256 hashes. Admin
projections omit raw payloads, stack traces, paths, credentials and provider
text.

Each step records the adapter that serves it in `adapter_capability`
(`x_recent`, `x_related`, `youtube_recent`, `bilibili_recent`, `bilibili_dynamics`,
`shop_sales`, `youtube_live`,
`media_download`, `schedule_extract`, `text_translate`, `media_variants` or
`event_reconcile`), which projections read before the constrained
`capability` column (`synthetic`, `x_recent` or `capability_not_migrated`).
A step without an adapter finishes `blocked` with `capability_not_migrated`
and never counts as successful work.

**Lease.** Claiming a queued run takes the SQLite write lock, sets it
`running` and writes the single `workflow_leases` row (scope `global`) with
an owner, a generation and an expiry two minutes ahead; a heartbeat renews
it every 40 seconds. Every step, attempt and run write, and every source
transaction, checks owner, generation and expiry in the same transaction,
so a worker that lost its lease cannot commit. A renewal that finds the
lease lost cancels the run's outbound requests; a renewal that fails to write,
such as on a busy database, is left to the next one, and the renewal after an
expiry finds the lease lost. A partial unique index allows at most one
`queued` or `running` run, so a second start answers `409` with `busy` and the
active run. A dispatched run has at most 20 minutes. A run stopped by that
limit, or by a lost lease, finishes `failed` with `timeout` or
`lease_expired` and its stopped step is retryable; only an operator's
cancellation or a shutdown finishes it `cancelled`.

**Retry.** A step error carries a category. `timeout`, `rate_limited` and
`dependency_unavailable` are retryable and get one more attempt in the same
run, five seconds later; `permanent`, `schema_failure`, `validation_failure`, `cancelled` and
`blocked` are not. The final failure of `x`, `x_related`, `youtube`,
`bilibili`, `bilibili_dynamics`, `shop` or `translate` is recorded with its
code and the run continues, finishing `partial`, or `failed` when no step
completed; a source step that carries a
[manual fetch](manual-fetches.md) is the exception. Any
other final failure ends the run and marks its pending steps `blocked`.
`POST /api/admin/runs/{id}/retry` requeues a `failed`, `partial` or
`blocked` run while no other run is active, when it has a retryable failed
step and no permanent failure: retryable failed and blocked steps return to
`pending`. A retried source step (`x`, `x_related`, `youtube`, `bilibili`,
`bilibili_dynamics`, `shop` or `live`) can store new posts and videos, so the
completed steps after the first of them that are not sources, such as
`media`, `schedule`, `translate` and `reconcile`, return to `pending` as well.
Other completed steps and earlier attempts stay.

**Recovery.** At startup and every 30 seconds the server recovers `running`
runs whose lease is missing or expired: their running attempts become
`failed` with `lease_expired` (retryable), their running steps return to
`pending`, and the run returns to `queued` with `execution_source =
recovery` and `recovery_count` incremented. A queued run is then dispatched;
its completed steps and the failures it already tolerated stay as they are,
and it continues with the next pending step.
`POST /api/admin/runs/{id}/cancel` records the request; a queued run is
cancelled at once, and a running one stops its network requests and finishes
`cancelled` with its pending steps. Finishing a run updates its `sync_runs`
row and releases the lease. It leaves the dashboard revision alone: each write
that changes what public readers see advances the revision in its own
transaction, so a run that changed nothing, such as a live probe that found
no change, never makes open boards reload.

**Retention.** The server process deletes finished runs past their
retention window when it starts and then once a day. Succeeded and
cancelled runs are kept 30 days by default; failed, partial and blocked runs
90 days. Both windows are operator settings in `app_settings`
(`run_retention_succeeded_days`, `run_retention_failed_days`, 0–3650; 0 keeps
those runs forever), edited on `/admin` → Runs → History cleanup, and the
next pass applies a saved value. A run's age counts from `finished_at`
(`created_at` when it has none). A pass deletes a run with its
`workflow_run_steps`, `workflow_step_attempts` and `sync_runs` rows in one
transaction, at most 200 runs per transaction, then applies the same windows
to `sync_runs` rows that belong to no stored run (`success`; `failed` and
`partial`). It never deletes a queued, running or leased run, the newest
run of each `workflow_id` (the live-status probe paces itself and the
Sources page reads each schedule's last run from it) or the newest
successful `sync_runs` row (the board's freshness reads it), whatever their
age. A deleted run can no longer be retried. The pass is local database
work, not a run: it does not use the queue, and
`WORKFLOW_SCHEDULER_ENABLED=0` does not stop it.

The same daily maintenance pass prunes the source response archive on its own
30-day success and 90-day HTTP-error windows and enforces its 512 MiB compressed
body limit. Capture retention does not depend on workflow-run retention.

Schedule-step outcomes combine post extraction and image verification. An
image-only failure cannot be reported as success: a scan with failures and no
successful/cached/uncertain work is `failed`; mixed outcomes are `partial`.
Workflow progress, persisted `sync_runs`, and standalone scans use the same
aggregation. Completed sync/workflow jobs expose their persisted run, including
its failure status, and always release the single-flight queue after settlement.

## Implementation Ownership

`backend/internal/jobs` owns the step execution order (`StepOrder`) and
presents durable workflow runs as operator jobs. `internal/workflow` maps steps
to their adapters (`SourceSteps`), executes runs with leases, retries and
recovery, and owns the single-flight queue, and the timer that starts due
source schedules (`SourceChains`) through it. `jobs.QueueManager` submits Run
now and step runs and freezes each source step's schedule sync and method
chain on them. See [Backend](../backend.md#jobs-and-workflows).

### Registering a Step or Source

Step and source IDs are listed by hand wherever they are needed; no list is
derived from another. In the console, only the step-run dialog reads its
steps from `GET /api/admin/runs/steps`. A new step or source goes into every
list below that applies to it. A list marked _partial_ holds a subset on
purpose and takes the new ID only when it shares that list's property.
Searching for an existing ID constant, such as `sqlite.ShopSource` or
`sqlite.BilibiliDynamicsSource`, finds these lists and the per-source code
beside them.

Every step:

- `backend/internal/jobs/manager.go`: `StepOrder`, the order that full syncs
  and step runs follow. `live` and `variants` are outside it.
- `backend/internal/adminapi/runs.go`: `workflowSteps`, the `{id, group}`
  list `GET /runs/steps` answers, a second copy of `StepOrder`.
- `backend/cmd/kano-go/standalone.go`: `stepSkipFlags`, each step's `SKIP_*`
  variable (`reconcile`, `live` and `variants` have none); the executor's
  `SyntheticSteps`, which names every step with a real adapter, `live` and
  `variants` included; and `TolerateFailure` (partial: the source steps and
  `translate`, whose final failure lets the run continue).
- `backend/internal/store/sqlite/workflow_commands.go`: the switch in
  `CreateWorkflowRun` that names each step's `adapter_capability`, and
  `scanStep` in `workflow_store.go`, which reads an adapter name it does not
  list as `capability_not_migrated`.
- `backend/internal/workflow/source_steps.go`: the `SourceSteps.Execute`
  switch that runs the step.
- `backend/internal/workflow/executor.go`: `withReconcile` (partial: the
  steps that write events or videos, which append `reconcile`).
- `src/admin/views/run-parts.tsx`: `stepIcons`; `src/admin-i18n.ts`: the
  `admin.step.<id>` label.

A step missing from `SyntheticSteps`, from the adapter switch or from
`Execute` finishes `blocked` with `capability_not_migrated`.

A source step also needs:

- `backend/internal/store/sqlite/workflow_commands.go`: `retrySourceSteps`,
  the source steps and `live`, after which a retry reruns completed
  processing steps.
- `backend/internal/adminapi/runs.go`: `sources` (`GET /sources`) and
  `overview` (`GET /overview`), which build their per-source objects key by
  key.
- `backend/internal/alerts/send.go`: `subjectNames`, and
  `src/admin-i18n.ts`: `admin.alerts.subject.<id>`, the alert subject's name.

A source with a timer:

- `backend/internal/store/sqlite/source_schedules.go`: `ScheduleSources`, in
  catalog order, and `defaultScheduleInterval` or `SourceScheduleBounds` when
  their defaults do not fit.
- `backend/internal/workflow/scheduler.go`: `SourceChains`, the steps that
  its timer and Run now submit.
- `backend/internal/store/sqlite/source_chain.go`: `chainMethods`, its
  default method chain, and `ChainSources` (partial: the sources whose chain
  the operator orders; `x_related` has one fixed method).
- `backend/internal/store/sqlite/workflow_sync.go`: `SyncSources` (partial:
  the sources whose schedule has a sync setting).
- `backend/internal/store/sqlite/admin.go`: `SourceLastSuccess` (partial: the
  sources whose console last success comes from `sync_state`;
  `bilibili_dynamics` takes its own from the Bilibili account status).
- `src/admin/types.ts`: `ScheduleSource`; `src/admin/views/sources.tsx`:
  `sourceIds` (the tab order) and `skipFlags`;
  `src/admin/views/overview.tsx`: a `SourceRow`.

A source with a [manual fetch](manual-fetches.md):

- `backend/internal/source/history`: its `Source` constant in `fetch.go` and
  its methods in the `methods` table of `methods.go`;
  `backend/internal/workflow/history_steps.go`: `MethodAvailable`.
- `fetchSources` in `backend/internal/jobs/manager.go` and again in
  `backend/internal/adminapi/runs.go`.
- `src/admin/types.ts`: `FetchSource` (partial: the sources whose fetch form
  reads through a method of its own); `src/admin/views/run-parts.tsx`:
  `fetchSteps` (partial: a stored run frozen without a method reads as a
  fetch only when all its steps are in it); `src/admin-i18n.ts`: the
  `admin.fetch.method.<source>.<method>` labels.

A source with a public freshness time (`meta.sourceFetchedAt`):

- `backend/internal/store/sqlite/freshness.go`: `sourceFetchedAt`, and
  `backend/internal/alerts/rules.go`: `freshSources`, which the
  `source_stale` alert checks. Both are partial: `x`, `youtube`, `bilibili`
  and `shop`.

Adapters, candidate selection, model routes, modality, retries, and per-post
reprocess behavior are defined in [Sources and extraction](../sources.md).
Register media candidates before bounded downloads; only successfully fetched
records are upserted. A failed step records its outcome and retains known state.
