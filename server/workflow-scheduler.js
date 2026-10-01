import {
  getWorkflow,
  listDueWorkflows,
  recordWorkflowResult,
  recordWorkflowStart,
} from "./database.js"

const DEFAULT_TICK_MS = 30_000

function settledStatus(job) {
  if (job?.status !== "completed") return "failed"
  return String(job.result?.status || "success")
}

/**
 * Starts saved workflows through the single-flight job manager. Scheduled
 * runs are checked by an in-process timer; a due workflow that finds another
 * job running simply stays due and is retried on the next tick. Nothing here
 * runs on page views: only this timer, an operator, or MCP starts a fetch.
 */
export function createWorkflowScheduler({
  database,
  jobs,
  tickMs = DEFAULT_TICK_MS,
  now = () => new Date(),
} = {}) {
  if (!database || !jobs)
    throw new Error("createWorkflowScheduler requires a database and jobs")
  let timer = null

  function run(workflowOrId, triggeredBy = "admin") {
    const workflow =
      typeof workflowOrId === "string"
        ? getWorkflow(database, workflowOrId)
        : workflowOrId
    if (!workflow)
      return { accepted: false, reason: "workflow_not_found", job: null }
    const result = jobs.start("workflow", triggeredBy, {
      steps: workflow.steps,
      workflowId: workflow.id,
      workflowName: workflow.name,
      onSettled: (job) =>
        recordWorkflowResult(database, workflow.id, {
          jobId: job.id,
          status: settledStatus(job),
        }),
    })
    if (result.accepted) {
      recordWorkflowStart(database, workflow.id, {
        jobId: result.job.id,
        startedAt: result.job.createdAt,
      })
    }
    return result
  }

  function tick() {
    // The job manager is single-flight, so at most one due workflow starts
    // per tick; the rest remain due.
    const [due] = listDueWorkflows(database, { now: now() })
    if (!due) return null
    try {
      return run(due, "scheduler")
    } catch (error) {
      console.error(`workflow scheduler failed: ${error.message}`)
      return null
    }
  }

  function start() {
    if (timer) return
    timer = setInterval(tick, Math.max(1000, Number(tickMs) || DEFAULT_TICK_MS))
    timer.unref?.()
  }

  function stop() {
    if (timer) clearInterval(timer)
    timer = null
  }

  function status() {
    return {
      running: Boolean(timer),
      tickSeconds: Math.round(
        Math.max(1000, Number(tickMs) || DEFAULT_TICK_MS) / 1000,
      ),
    }
  }

  return { run, start, status, stop, tick }
}
