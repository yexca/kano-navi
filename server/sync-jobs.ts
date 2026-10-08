import type { Database as DatabaseConnection } from "better-sqlite3"

import crypto from "node:crypto"

import {
  bumpDashboardRevision,
  finishSyncRun,
  getSyncRun,
  startSyncRun,
} from "./database.ts"
import { extractPendingSchedules } from "./schedule-extractor.ts"
import { runSync } from "../scripts/sync.ts"
import {
  normalizeWorkflowSteps,
  WORKFLOW_STEP_IDS,
} from "./workflow-catalog.ts"

const DEFAULT_MAX_JOBS = 100

function now() {
  return new Date().toISOString()
}

function publicJob(state, database: DatabaseConnection) {
  const run = state.runId ? getSyncRun(database, state.runId) : null
  return {
    id: state.id,
    kind: state.kind,
    status: state.status,
    createdAt: state.createdAt,
    startedAt: state.startedAt,
    finishedAt: state.finishedAt,
    triggeredBy: state.triggeredBy,
    workflowId: state.workflowId || null,
    workflowName: state.workflowName || null,
    steps: state.steps ? [...state.steps] : null,
    fetchWindows: state.fetchWindows || null,
    progress: state.progress ? { ...state.progress } : null,
    error: state.error || null,
    result: state.result || null,
    run: run || null,
  }
}

function initialProgress(steps) {
  return Object.fromEntries(steps.map((step) => [step, "pending"]))
}

/**
 * In-process single-flight queue for operator-triggered work.  A sync can be
 * expensive and mutates one SQLite snapshot, so overlapping runs are rejected
 * instead of interleaving writes or external requests.
 */
export function createSyncJobManager({
  database,
  runSyncImpl = runSync,
  scanImpl = extractPendingSchedules,
  maxJobs = DEFAULT_MAX_JOBS,
}: {
  database?: DatabaseConnection
  runSyncImpl?: any
  scanImpl?: any
  maxJobs?: any
} = {}) {
  if (!database) throw new Error("createSyncJobManager requires a database")
  const jobs = new Map()
  let activeJobId = null

  function remember(state) {
    jobs.set(state.id, state)
    while (jobs.size > Math.max(10, Number(maxJobs) || DEFAULT_MAX_JOBS)) {
      const oldest = jobs.keys().next().value
      if (!oldest || oldest === activeJobId) break
      jobs.delete(oldest)
    }
  }

  async function execute(state) {
    state.status = "running"
    state.startedAt = now()
    activeJobId = state.id
    try {
      if (state.kind === "scan") {
        const runId = startSyncRun(database, "schedule", {
          triggeredBy: state.triggeredBy,
          jobId: state.id,
        })
        state.runId = runId
        try {
          const result = await scanImpl(database)
          finishSyncRun(database, runId, {
            status: Number(result?.failed || 0) > 0 ? "partial" : "success",
            message:
              Number(result?.failed || 0) > 0
                ? "自动扫描完成，但部分候选失败"
                : "自动扫描完成",
            counts: result || {},
          })
          state.result = result || {}
        } catch (error) {
          finishSyncRun(database, runId, {
            status: "failed",
            message: "自动扫描异常终止，保留已有快照",
            counts: { fatal: { error: String(error.message || error) } },
          })
          throw error
        } finally {
          bumpDashboardRevision(database)
        }
      } else {
        const result = await runSyncImpl({
          database,
          closeDatabase: false,
          triggeredBy: state.triggeredBy,
          jobId: state.id,
          setExitCode: false,
          fetchWindows: state.fetchWindows,
          ...(state.kind === "workflow"
            ? {
                steps: state.steps,
                runSource: "workflow",
                onStep: (step, status) => {
                  if (state.progress && step in state.progress)
                    state.progress[step] = status
                },
              }
            : {}),
        })
        state.result = result || {}
      }
      state.status = "completed"
    } catch (error) {
      state.status = "failed"
      state.error = String(error?.message || error)
    } finally {
      state.finishedAt = now()
      if (state.progress) {
        for (const [step, status] of Object.entries(state.progress)) {
          if (status === "pending" || status === "running")
            state.progress[step] =
              state.status === "failed" ? "cancelled" : "skipped"
        }
      }
      if (activeJobId === state.id) activeJobId = null
      remember(state)
    }
    const job = publicJob(state, database)
    try {
      state.onSettled?.(job)
    } catch {
      // Settlement hooks only record metadata and must not fail the job.
    }
    return job
  }

  /**
   * Start one operator job. `sync` runs every step, `scan` runs only the
   * schedule extractor, and `workflow` runs the selected catalog steps with
   * per-step progress for the admin console.
   */
  function start(
    kind = "sync",
    triggeredBy = "admin",
    options: Record<string, any> = {},
  ) {
    const normalizedKind = ["scan", "workflow"].includes(kind) ? kind : "sync"
    const steps =
      normalizedKind === "workflow"
        ? normalizeWorkflowSteps(options.steps ?? WORKFLOW_STEP_IDS)
        : null
    if (normalizedKind === "workflow" && !steps.length) {
      return { accepted: false, reason: "no_steps", job: null }
    }
    if (activeJobId) {
      const existing = jobs.get(activeJobId)
      return {
        accepted: false,
        reason: "already_running",
        job: existing ? publicJob(existing, database) : null,
      }
    }
    const state = {
      id: crypto.randomUUID(),
      kind: normalizedKind,
      status: "queued",
      createdAt: now(),
      startedAt: null,
      finishedAt: null,
      triggeredBy: String(triggeredBy || "admin").slice(0, 64),
      runId: null,
      workflowId: options.workflowId ? String(options.workflowId) : null,
      workflowName: options.workflowName
        ? String(options.workflowName).slice(0, 120)
        : null,
      steps,
      fetchWindows: options.fetchWindows
        ? structuredClone(options.fetchWindows)
        : null,
      progress: steps ? initialProgress(steps) : null,
      onSettled:
        typeof options.onSettled === "function" ? options.onSettled : null,
      result: null,
      error: null,
    }
    remember(state)
    // Start on a later turn so the HTTP handler can return 202 immediately.
    void execute(state)
    return { accepted: true, reason: null, job: publicJob(state, database) }
  }

  function get(id) {
    const state = jobs.get(String(id || ""))
    return state ? publicJob(state, database) : null
  }

  function list(limit = 20) {
    const bounded = Math.min(100, Math.max(1, Math.trunc(Number(limit)) || 20))
    return [...jobs.values()]
      .reverse()
      .slice(0, bounded)
      .map((state) => publicJob(state, database))
  }

  function active() {
    const state = activeJobId ? jobs.get(activeJobId) : null
    return state ? publicJob(state, database) : null
  }

  return { active, get, list, start }
}
