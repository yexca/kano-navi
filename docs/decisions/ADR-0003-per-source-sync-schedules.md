# ADR-0003: Per-Source Synchronization Schedules

- Status: Accepted
- Date: 2026-10-27
- Amends: [ADR-0002](ADR-0002-count-based-source-sync.md) (where `sourceSync` is stored)

## Context

Saved workflows are the only timed way to read sources. Each workflow picks
steps and one timer, so an operator who wants X every 30 minutes and Bilibili
every few hours has to keep several workflows in step. The pace of each
source is also split across four places:

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

- A fresh installation keeps every schedule disabled, as seeded workflows
  have no timers today.
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
which ADR-0002 introduced.

### Where sync settings live

`sourceSync` moves from saved workflows to source schedules. The setting
from ADR-0002 is unreleased, so the workflow API stops accepting it now and
rejects it with `400 invalid_input`, as it does `sourceLookbackDays`. A
manual run of a saved workflow reads each source with that source's
schedule settings. The column `workflows.source_sync_json` stays in the
schema and is no longer read. Columns are never dropped.

### Saved workflows

Saved workflows remain as operator presets for manual runs and custom
combinations, such as a translate-only sweep, and they keep their timers.
To avoid reading the same source twice, an upgrade runs one migration,
guarded by the `app_settings` key `source_schedules_migrated`:

- For each timed workflow that contains a source step, enable that source's
  schedule. Use the smallest interval among such workflows, raised to the
  Bilibili floor where it applies. Copy the workflow's stored `sourceSync`,
  or the default.
- Then disable that workflow's timer. Its steps and history stay untouched.
- Timed workflows without a source step keep their timers.

After the migration, the API refuses to enable a timer on a workflow that
contains a source step. It answers `400 invalid_input` and names the source
schedule to use instead.

### Scheduler

The existing scheduler ticks every 30 seconds over due workflows and due
source schedules together. It submits the item with the earliest
`next_run_at`, so a frequent source cannot starve another. The single-flight
queue is unchanged, and a busy queue leaves the item due for the next tick.
Enabling a schedule or changing its interval plans the next run one interval
later, never immediately, matching workflow timers.

- `WORKFLOW_SCHEDULER_ENABLED=0` stops source schedules as well.
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
- The Sources page gets one section per source with the timer, the
  interval, the sync mode (X and YouTube), the next run and the last run.
  The YouTube section also holds WebSub and the live-status probe.
- On the Runs & workflows page, workflows lose the source sync editor.
  Their timer control is disabled, with an explanation, when they contain a
  source step.
- Run history labels these runs by source.

## Consequences

- Each source is configured in one place, with a pace that fits it. Bilibili
  no longer appears as a skipped step in frequent runs.
- Runs are smaller and more frequent, and they still share one queue. A
  long X run (schedule extraction with LLM calls) can still delay the live
  probe or a WebSub run. This is unchanged from today, but it may happen
  more often.
- Translation runs after every source chain. With an empty queue it costs
  only a query.
- Saved workflows remain for manual work. Operators who used them as timers
  are migrated once and see the change in the console.

## Out of Scope

- Separate queue lanes (fetching vs. LLM processing) or letting the probe
  jump the queue. Revisit if live status is seen to lag.
- Translating Bilibili titles, if the translation queue does not cover them
  today.
- Smart detection for X or Bilibili. Neither platform offers a signal to
  build it on.

## Alternatives Considered

- Keeping workflows as the only timer and documenting a two-workflow setup.
  It is possible today, but the pace of each source would still be spread
  across workflows, environment variables and probe settings.
- Running follow-up steps only when the source wrote something. This saves
  little, because empty queues are already cheap. It would stall retries and
  operator requests while a source is quiet.
- Chaining Bilibili after YouTube. Video merging is done by `reconcile`, not
  by step order, and coupling the two would request Bilibili on every
  YouTube push.
