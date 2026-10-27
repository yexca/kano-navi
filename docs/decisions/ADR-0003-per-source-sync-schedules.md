# ADR-0003: Per-Source Synchronization Schedules

- Status: Accepted
- Date: 2026-10-27
- Amends: [ADR-0002](ADR-0002-count-based-source-sync.md) (where `sourceSync` is stored)

## Context

Saved workflows were the only timed way to read sources. Each workflow picked
steps and one timer, so an operator who wanted X every 30 minutes and Bilibili
every few hours had to keep several workflows in step. The pace of each
source was also split across four places:

- workflow timers (15 minutes to 7 days);
- `BILIBILI_MIN_INTERVAL_MINUTES`, which skips Bilibili reads inside a
  frequent workflow;
- the YouTube live-status probe, with its own timer in `app_settings`;
- YouTube WebSub pushes, which queue `[youtube, media]` directly.

The sources differ in their needs. X has no push channel, and its posts
announce schedules, so it is read often. YouTube has push and the live probe,
so a timer is only a fallback. Bilibili is limited by risk control, and the
hosting IP may get captchas.

The execution engine is not the problem. Ad hoc runs (`@live`, WebSub, the
operator's `[youtube, media]` refresh) already show that a trigger can submit
a fixed step list to the single-flight queue without a saved workflow.

The board has a single operator, so no existing installation needs a
migration path.

## Decision

### Source schedules

Each source (`x`, `youtube`, `bilibili`) gets a schedule stored in an
additive `source_schedules` table with these columns: `source` (the primary
key), `enabled`, `interval_minutes`, `sync_json`, `next_run_at` and
`updated_at`.

| Source     | Interval (default, bounds)                        | `sync_json`                                      | Detection                                    |
| ---------- | ------------------------------------------------- | ------------------------------------------------ | -------------------------------------------- |
| `x`        | 30 min, 15 min – 7 days                           | `incremental` (`max`) or `count`, as in ADR-0002 | none (X has no push)                         |
| `youtube`  | 60 min, 15 min – 7 days                           | `incremental` (`max`) or `count`, as in ADR-0002 | WebSub push and live-status probe (existing) |
| `bilibili` | 180 min, `BILIBILI_MIN_INTERVAL_MINUTES` – 7 days | none (one upload list page)                      | none                                         |

- Every schedule starts disabled. The operator enables the timers on the
  Sources page.
- The Bilibili interval cannot go below the environment floor. The
  adapter's own interval and backoff checks stay as the safety net.
- The WebSub and live-probe settings keep their storage and endpoints. The
  console shows them in the YouTube section next to its schedule.

### Fixed chains

When a source schedule is due, the scheduler submits an ad hoc run named
`@source-<source>`, triggered by `source-schedule`, with a fixed chain:

- `x` → `[x, media, schedule, translate]`
- `youtube` → `[youtube, media, translate]`
- `bilibili` → `[bilibili, media, translate]`

`reconcile` is appended as it is for every run with a source step. WebSub
pushes submit the `youtube` chain instead of `[youtube, media]`. The
operator's single-video refresh keeps its own step list. The live probe is
unchanged.

The follow-up steps (`media`, `schedule`, `translate`) always run in the
chain. They are not skipped when the source wrote nothing. Each one already
works from its own queue and makes no provider call when that queue is
empty, so failed extractions, pending image reviews, retried downloads and
operator translation requests keep moving even while a source is quiet.

The run freezes the source's `sync_json` into `workflow_runs.source_sync_json`,
which ADR-0002 introduced. Any other run with a source step reads that source
with its schedule's sync settings. This covers a WebSub push, a one-off step
run and a source's "run now". Full syncs keep the environment's incremental
options.

### Saved workflows are removed

Saved workflow definitions are removed, together with their timers. That
includes:

- the `/api/admin/workflows` endpoints;
- `POST /api/admin/runs` with a `workflowId`, which now answers
  `400 invalid_input`;
- the workflow editor;
- the seeded `full-refresh` and `sources-only` presets.

No migration is written. The operator sets the source timers again.

The `workflows` table, including the `source_sync_json` column ADR-0002
added, stays in the schema. Nothing reads or writes it. Tables and columns
are never dropped. Fresh installations still create the table, empty.

The execution engine stays as it is. Every run is an ad hoc run on it:
source timers, WebSub, the live probe, fetch windows, full sync, scan,
translate and MCP. That engine includes `workflow_runs`, steps, attempts,
leases, retries, recovery, the single-flight queue and run history. Older
runs whose `workflow_id` named a saved workflow stay in history. The console
shows that ID with a "retired workflow" label, without reading `workflows`.
Retrying a failed run no longer looks up a saved definition: every run keeps
its steps, sync settings and windows, so any failed run with a retryable
failed step can be retried.

Manual runs replace the saved presets:

- **Run now** on each source section submits that source's chain once,
  triggered by the operator. It does not move the schedule's `next_run_at`.
- A **one-off step run** on the Runs page picks steps from the catalog and
  runs them once without saving. The API already accepts this as
  `kind: "workflow"` with `steps`.
- Full sync, schedule scan, translate and fetch windows are unchanged.

### Scheduler

The scheduler ticks every 30 seconds over due source schedules only, and
submits the one with the earliest `next_run_at`, so a frequent source cannot
starve another. The single-flight queue is unchanged, and a busy queue leaves
the schedule due for the next tick. Enabling a schedule or changing its
interval plans the next run one interval later, never immediately.

- `WORKFLOW_SCHEDULER_ENABLED=0` stops the source schedules. The name stays
  so existing deployments keep working. Only its description changes.
- `SKIP_X`, `SKIP_YOUTUBE` and `SKIP_BILIBILI` disable that source's
  schedule, which reports `{ skipped: true, reason: "environment" }`.
- `kano-go sync` and MCP sync are unchanged: they run every step with the
  environment's incremental options.

### Admin API and console

- `GET /api/admin/sources` adds `schedules`. Each entry lists `source`,
  `enabled`, `intervalMinutes`, its bounds, `sync`, `nextRunAt` and the
  last run's id, status and time.
- `PUT /api/admin/sources/{source}/schedule` takes any subset of
  `{enabled, intervalMinutes, sync}`.
- `POST /api/admin/sources/{source}/run` starts **Run now**. It answers
  `202` with the run, or `409 busy` while the queue is occupied, and
  `400 invalid_input` while `SKIP_*` disables the source. Its run uses the
  same `@source-<source>` ID, so it shows as the schedule's last run.
- `GET /api/admin/runs/steps` lists the step catalog and the steps `SKIP_*`
  disables, for the one-off step run.
- The Sources page gets one section per source. Each has the timer, the
  interval, the sync mode (X and YouTube), **Run now**, the next run and the
  last run. The YouTube section also holds WebSub and the live-status probe.
- The Runs page drops the workflow list and editor. It keeps run history,
  the one-off step run and the existing run actions.
- The overview's "next timed run" counts source schedules only. Its API
  field becomes `timers: {scheduled, nextRunAt}`, replacing `workflows`,
  and runs no longer carry `workflowName`.
- Run history labels source runs by source ("X timer", "X · run now").

## Consequences

- Each source is configured in one place, with a pace that fits it. Bilibili
  no longer appears as a skipped step in frequent runs.
- The console loses a whole concept. The operator chooses how often each
  source is read, and can run any combination once.
- Runs are smaller and more frequent, and they still share one queue. A
  long X run (schedule extraction with LLM calls) can still delay the live
  probe or a WebSub run. This is unchanged from before, but it may happen
  more often.
- Translation runs after every source chain. With an empty queue it costs
  only a query.
- An installation upgraded from v0.3.x loses its saved workflows and their
  timers. The release note asks the operator to enable the source timers.

## Out of Scope

- Separate queue lanes (fetching vs. LLM processing) or letting the probe
  jump the queue. Revisit if live status is seen to lag.
- Translating Bilibili titles, if the translation queue does not cover them
  today.
- Smart detection for X or Bilibili. Neither platform offers a signal to
  build it on.

## Alternatives Considered

- Keeping saved workflows next to source schedules, with a one-time
  migration of their timers. This was the first version of this decision.
  It kept two timer concepts and migration code for a single operator who
  can simply set the timers again.
- Keeping workflows as the only timer and documenting a two-workflow setup.
  The pace of each source would still be spread across workflows,
  environment variables and probe settings.
- Running follow-up steps only when the source wrote something. This saves
  little, because empty queues are already cheap. It would stall retries and
  operator requests while a source is quiet.
- Chaining Bilibili after YouTube. Video merging is done by `reconcile`, not
  by step order, and coupling the two would request Bilibili on every
  YouTube push.
