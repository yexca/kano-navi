import assert from "node:assert/strict"
import test from "node:test"

import {
  getDashboardRevision,
  getSyncRun,
  initializeDatabase,
} from "./database.js"
import { createSyncJobManager } from "./sync-jobs.js"

function delay(milliseconds = 0) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds))
}

async function waitForJob(manager, id) {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    const job = manager.get(id)
    if (job && ["completed", "failed"].includes(job.status)) return job
    await delay(2)
  }
  throw new Error("timed out waiting for sync job")
}

test("sync jobs are single-flight and pass non-exiting runtime options", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  let release
  let started
  const gate = new Promise((resolve) => {
    release = resolve
  })
  const manager = createSyncJobManager({
    database,
    runSyncImpl: async (options) => {
      started = options
      await gate
      return { status: "success", results: { test: true } }
    },
  })
  try {
    const first = manager.start("sync", "mcp")
    assert.equal(first.accepted, true)
    assert.ok(first.job.id)
    const second = manager.start("scan", "admin")
    assert.equal(second.accepted, false)
    assert.equal(second.reason, "already_running")
    assert.equal(second.job.id, first.job.id)

    release()
    const completed = await waitForJob(manager, first.job.id)
    assert.equal(completed.status, "completed")
    assert.deepEqual(completed.result, {
      status: "success",
      results: { test: true },
    })
    assert.equal(started.closeDatabase, false)
    assert.equal(started.setExitCode, false)
    assert.equal(started.triggeredBy, "mcp")
    assert.equal(started.jobId, first.job.id)
    assert.equal(manager.list(1)[0].id, first.job.id)
  } finally {
    release()
    database.close()
  }
})

test("automatic scan jobs persist a run and advance the dashboard revision", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const before = getDashboardRevision(database)
  const manager = createSyncJobManager({
    database,
    scanImpl: async () => ({ success: 2, cached: 1, failed: 0 }),
  })
  try {
    const accepted = manager.start("scan", "mcp")
    const completed = await waitForJob(manager, accepted.job.id)
    assert.equal(completed.status, "completed")
    assert.deepEqual(completed.result, { success: 2, cached: 1, failed: 0 })
    assert.equal(getDashboardRevision(database), before + 1)
    const run = getSyncRun(database, accepted.job.id)
    assert.equal(run.source, "schedule")
    assert.equal(run.status, "success")
    assert.equal(run.triggeredBy, "mcp")
    assert.equal(run.jobId, accepted.job.id)
    assert.equal(completed.run.id, run.id)
  } finally {
    database.close()
  }
})

test("automatic scan failures retain a failed run and still release the queue", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const manager = createSyncJobManager({
    database,
    scanImpl: async () => {
      throw new Error("scan failed placeholder")
    },
  })
  try {
    const accepted = manager.start("scan", "admin")
    const failed = await waitForJob(manager, accepted.job.id)
    assert.equal(failed.status, "failed")
    assert.match(failed.error, /scan failed placeholder/u)
    const run = getSyncRun(database, accepted.job.id)
    assert.equal(run.status, "failed")
    assert.match(run.message, /保留已有快照/u)
    const restarted = manager.start("scan", "admin")
    assert.equal(restarted.accepted, true)
    await waitForJob(manager, restarted.job.id)
  } finally {
    database.close()
  }
})
