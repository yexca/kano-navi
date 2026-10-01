import assert from "node:assert/strict"
import test from "node:test"

import { createApp } from "./app.js"
import {
  getWorkflow,
  initializeDatabase,
  listDueWorkflows,
  listWorkflows,
  upsertWorkflow,
} from "./database.js"
import { createSyncJobManager } from "./sync-jobs.js"
import {
  clampWorkflowInterval,
  normalizeWorkflowSteps,
} from "./workflow-catalog.js"
import { createWorkflowScheduler } from "./workflow-scheduler.js"

process.env.SYNC_REQUEST_DELAY_MS = "0"
const { runSync } = await import("../scripts/sync.mjs")

function delay(milliseconds = 0) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds))
}

async function waitForJob(manager, id) {
  for (let attempt = 0; attempt < 200; attempt += 1) {
    const job = manager.get(id)
    if (job && ["completed", "failed"].includes(job.status)) return job
    await delay(2)
  }
  throw new Error("timed out waiting for workflow job")
}

async function listen(app) {
  return new Promise((resolve) => {
    const server = app.listen(0, "127.0.0.1", () => resolve(server))
  })
}

async function jsonRequest(origin, path, options = {}) {
  const response = await fetch(`${origin}${path}`, {
    ...options,
    headers: {
      accept: "application/json",
      ...(options.body ? { "content-type": "application/json" } : {}),
    },
  })
  return { response, payload: await response.json().catch(() => null) }
}

test("workflow steps are normalized into catalog order", () => {
  assert.deepEqual(normalizeWorkflowSteps(["schedule", "x", "nope"]), [
    "x",
    "schedule",
  ])
  assert.deepEqual(normalizeWorkflowSteps('["media","youtube"]'), [
    "youtube",
    "media",
  ])
  assert.equal(clampWorkflowInterval(1), 15)
  assert.equal(clampWorkflowInterval(999999), 7 * 24 * 60)
})

test("seeded workflows start unscheduled and schedules plan ahead", () => {
  const database = initializeDatabase({ seed: true, filename: ":memory:" })
  try {
    const seeded = listWorkflows(database)
    assert.deepEqual(
      seeded.map((workflow) => [workflow.id, workflow.scheduleEnabled]),
      [
        ["full-refresh", false],
        ["sources-only", false],
      ],
    )
    const now = new Date("2026-10-01T00:00:00.000Z")
    const scheduled = upsertWorkflow(
      database,
      { id: "sources-only", scheduleEnabled: true, intervalMinutes: 30 },
      { now },
    )
    assert.equal(scheduled.nextRunAt, "2026-10-01T00:30:00.000Z")
    assert.deepEqual(listDueWorkflows(database, { now }), [])
    assert.deepEqual(
      listDueWorkflows(database, {
        now: new Date("2026-10-01T00:31:00.000Z"),
      }).map((workflow) => workflow.id),
      ["sources-only"],
    )
    // Unrelated edits keep the existing plan.
    const renamed = upsertWorkflow(
      database,
      { id: "sources-only", name: "Renamed" },
      { now: new Date("2026-10-01T00:10:00.000Z") },
    )
    assert.equal(renamed.nextRunAt, "2026-10-01T00:30:00.000Z")
    assert.throws(() => upsertWorkflow(database, { id: "empty", steps: [] }))
  } finally {
    database.close()
  }
})

test("runSync executes only the requested steps and reports progress", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const previousSkipMedia = process.env.SKIP_MEDIA
  const progress = []
  try {
    delete process.env.SKIP_MEDIA
    const result = await runSync({
      database,
      closeDatabase: false,
      setExitCode: false,
      steps: ["media"],
      runSource: "workflow",
      onStep: (step, status) => progress.push(`${step}:${status}`),
    })
    assert.equal(result.status, "success")
    assert.deepEqual(Object.keys(result.results), ["media"])
    assert.deepEqual(progress, ["media:running", "media:completed"])

    process.env.SKIP_MEDIA = "1"
    const skipped = await runSync({
      database,
      closeDatabase: false,
      setExitCode: false,
      steps: ["media"],
    })
    assert.deepEqual(skipped.results.media, {
      skipped: true,
      reason: "environment",
    })
  } finally {
    if (previousSkipMedia == null) delete process.env.SKIP_MEDIA
    else process.env.SKIP_MEDIA = previousSkipMedia
    database.close()
  }
})

test("the scheduler starts due workflows through the single-flight queue", async () => {
  const database = initializeDatabase({ seed: true, filename: ":memory:" })
  const calls = []
  const jobs = createSyncJobManager({
    database,
    runSyncImpl: async (options) => {
      calls.push(options)
      options.onStep("x", "running")
      options.onStep("x", "completed")
      return { status: "success", results: {} }
    },
  })
  let clock = new Date("2026-10-01T00:00:00.000Z")
  const scheduler = createWorkflowScheduler({
    database,
    jobs,
    now: () => clock,
  })
  try {
    upsertWorkflow(
      database,
      { id: "sources-only", scheduleEnabled: true, intervalMinutes: 15 },
      { now: clock },
    )
    assert.equal(scheduler.tick(), null)
    clock = new Date("2026-10-01T00:16:00.000Z")
    const started = scheduler.tick()
    assert.equal(started.accepted, true)
    assert.equal(started.job.kind, "workflow")
    assert.deepEqual(started.job.steps, ["x", "youtube"])
    const job = await waitForJob(jobs, started.job.id)
    assert.equal(job.status, "completed")
    assert.deepEqual(job.progress, { x: "completed", youtube: "skipped" })
    assert.equal(calls[0].runSource, "workflow")
    assert.equal(calls[0].triggeredBy, "scheduler")
    assert.deepEqual(calls[0].steps, ["x", "youtube"])
    const workflow = getWorkflow(database, "sources-only")
    assert.equal(workflow.lastJobId, started.job.id)
    assert.equal(workflow.lastStatus, "success")
    assert.ok(Date.parse(workflow.nextRunAt) > Date.parse(clock.toISOString()))
  } finally {
    scheduler.stop()
    database.close()
  }
})

test("admin workflow endpoints validate, persist, and run workflows", async () => {
  const database = initializeDatabase({ seed: true, filename: ":memory:" })
  let release
  const gate = new Promise((resolve) => {
    release = resolve
  })
  const syncJobs = createSyncJobManager({
    database,
    runSyncImpl: async () => {
      await gate
      return { status: "success", results: {} }
    },
  })
  const server = await listen(
    createApp({
      database,
      databaseLabel: ":memory:",
      adminMode: "development",
      syncJobs,
    }),
  )
  try {
    const origin = `http://127.0.0.1:${server.address().port}`
    const listed = await jsonRequest(origin, "/api/admin/workflows")
    assert.equal(listed.response.status, 200)
    assert.deepEqual(
      listed.payload.steps.map((step) => step.id),
      ["x", "youtube", "media", "schedule"],
    )
    assert.equal(listed.payload.scheduler.running, false)

    const rejected = await jsonRequest(origin, "/api/admin/workflows", {
      method: "POST",
      body: JSON.stringify({ name: "Bad", steps: ["x", "unknown"] }),
    })
    assert.equal(rejected.response.status, 400)
    const tooFrequent = await jsonRequest(origin, "/api/admin/workflows", {
      method: "POST",
      body: JSON.stringify({
        name: "Fast",
        steps: ["x"],
        scheduleEnabled: true,
        intervalMinutes: 1,
      }),
    })
    assert.equal(tooFrequent.response.status, 400)

    const created = await jsonRequest(origin, "/api/admin/workflows", {
      method: "POST",
      body: JSON.stringify({
        name: "Media only",
        steps: ["media"],
        scheduleEnabled: true,
        intervalMinutes: 120,
      }),
    })
    assert.equal(created.response.status, 201)
    const id = created.payload.workflow.id
    assert.match(id, /^media-only-[0-9a-f]{6}$/u)
    assert.ok(created.payload.workflow.nextRunAt)

    const updated = await jsonRequest(origin, `/api/admin/workflows/${id}`, {
      method: "PUT",
      body: JSON.stringify({ scheduleEnabled: false }),
    })
    assert.equal(updated.response.status, 200)
    assert.equal(updated.payload.workflow.nextRunAt, null)
    assert.deepEqual(updated.payload.workflow.steps, ["media"])

    const run = await jsonRequest(origin, `/api/admin/workflows/${id}/run`, {
      method: "POST",
    })
    assert.equal(run.response.status, 202)
    assert.equal(run.payload.job.workflowId, id)
    const busy = await jsonRequest(
      origin,
      "/api/admin/workflows/full-refresh/run",
      { method: "POST" },
    )
    assert.equal(busy.response.status, 409)
    release()
    await waitForJob(syncJobs, run.payload.job.id)

    const missing = await jsonRequest(
      origin,
      "/api/admin/workflows/missing/run",
      { method: "POST" },
    )
    assert.equal(missing.response.status, 404)
    const removed = await jsonRequest(origin, `/api/admin/workflows/${id}`, {
      method: "DELETE",
    })
    assert.equal(removed.response.status, 200)
    assert.equal(getWorkflow(database, id), null)
  } finally {
    release()
    await new Promise((resolve) => server.close(resolve))
    database.close()
  }
})
