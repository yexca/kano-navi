import crypto from "node:crypto"

import {
  bumpDashboardRevision,
  finishSyncRun,
  getSyncRun,
  startSyncRun,
} from "./database.js"
import { extractPendingSchedules } from "./schedule-extractor.js"
import { runSync } from "../scripts/sync.mjs"

const DEFAULT_MAX_JOBS = 100

function now() {
  return new Date().toISOString()
}

function publicJob(state, database) {
  const run = state.runId ? getSyncRun(database, state.runId) : null
  return {
    id: state.id,
    kind: state.kind,
    status: state.status,
    createdAt: state.createdAt,
    startedAt: state.startedAt,
    finishedAt: state.finishedAt,
    triggeredBy: state.triggeredBy,
    error: state.error || null,
    result: state.result || null,
    run: run || null,
  }
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
        })
        state.result = result || {}
      }
      state.status = "completed"
    } catch (error) {
      state.status = "failed"
      state.error = String(error?.message || error)
    } finally {
      state.finishedAt = now()
      if (activeJobId === state.id) activeJobId = null
      remember(state)
    }
    return publicJob(state, database)
  }

  function start(kind = "sync", triggeredBy = "admin") {
    const normalizedKind = kind === "scan" ? "scan" : "sync"
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

  return { get, list, start }
}
