// Dependency-free catalog of the modular synchronization steps. The order of
// this list is the execution order: sources first, then post-processing that
// consumes what the sources stored. A new step needs one entry here and one
// runner in scripts/sync.mjs; the admin console renders it automatically.
export const WORKFLOW_STEPS = Object.freeze([
  Object.freeze({ id: "x", group: "source", skipEnv: "SKIP_X" }),
  Object.freeze({ id: "youtube", group: "source", skipEnv: "SKIP_YOUTUBE" }),
  Object.freeze({ id: "media", group: "process", skipEnv: "SKIP_MEDIA" }),
  Object.freeze({ id: "schedule", group: "process", skipEnv: "SKIP_LLM" }),
])

export const WORKFLOW_STEP_IDS = WORKFLOW_STEPS.map((step) => step.id)

// Scheduled workflows are bounded so a misconfiguration cannot turn the
// server into a tight polling loop against public platforms.
export const WORKFLOW_MIN_INTERVAL_MINUTES = 15
export const WORKFLOW_MAX_INTERVAL_MINUTES = 7 * 24 * 60

/**
 * Return known step IDs in catalog order. Unknown values are dropped, so the
 * result can always be passed to the synchronization runner.
 */
export function normalizeWorkflowSteps(value) {
  let values = value
  if (typeof value === "string") {
    try {
      const parsed = JSON.parse(value)
      values = Array.isArray(parsed) ? parsed : value.split(/[,\s]+/u)
    } catch {
      values = value.split(/[,\s]+/u)
    }
  }
  if (!Array.isArray(values)) return []
  const selected = new Set(
    values.map((item) =>
      String(item || "")
        .trim()
        .toLowerCase(),
    ),
  )
  return WORKFLOW_STEP_IDS.filter((id) => selected.has(id))
}

/** Steps an operator disabled for the whole process through SKIP_* flags. */
export function environmentSkippedSteps(env = process.env) {
  return WORKFLOW_STEPS.filter((step) => env[step.skipEnv] === "1").map(
    (step) => step.id,
  )
}

export function clampWorkflowInterval(value, fallback = 60) {
  const number = Math.trunc(Number(value))
  if (!Number.isFinite(number)) return fallback
  return Math.min(
    WORKFLOW_MAX_INTERVAL_MINUTES,
    Math.max(WORKFLOW_MIN_INTERVAL_MINUTES, number),
  )
}
