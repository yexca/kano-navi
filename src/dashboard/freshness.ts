import type { Dashboard, Translator } from "../types.ts"

// A local warning threshold, independent of the server's synchronization timer.
export const STALE_AFTER_MS = 24 * 60 * 60 * 1000
// A visitor clock a little behind the server still shows a fresh read; a
// timestamp further ahead is not trusted.
export const CLOCK_SKEW_MS = 5 * 60 * 1000

export const SYNC_SOURCES = ["x", "youtube", "bilibili"] as const
export type SyncSource = (typeof SYNC_SOURCES)[number]

type Freshness = "fresh" | "stale" | "waiting"

export interface SyncState {
  tone: "leaf" | "honey" | "danger" | "muted"
  label: string
  /** When the latest completed run finished, or null before any run. */
  runAt: string | null
  sources: {
    source: SyncSource
    timestamp: string | null
    state: Freshness
  }[]
}

export function freshnessAt(
  timestamp: string | null | undefined,
  now: number,
): Freshness {
  const at = Date.parse(timestamp || "")
  if (!Number.isFinite(at) || at > now + CLOCK_SKEW_MS) return "waiting"
  return now - at >= STALE_AFTER_MS ? "stale" : "fresh"
}

/**
 * The overall state is the latest run's outcome and age; each source keeps its
 * own last successful read. Recomputed from the retained snapshot when the
 * local clock changes.
 */
export function syncStateFor(
  meta: Dashboard["meta"],
  error: unknown,
  now: number,
  t: Translator,
): SyncState {
  const sources = SYNC_SOURCES.map((source) => {
    const timestamp = meta.sourceFetchedAt?.[source] ?? null
    const state = freshnessAt(timestamp, now)
    return { source, timestamp: state === "waiting" ? null : timestamp, state }
  })
  const run = meta.lastSync
  const runAt = run ? (run.finishedAt ?? run.startedAt ?? null) : null
  const runState = freshnessAt(runAt, now)
  let tone: SyncState["tone"] = "leaf"
  let key = "sync.snapshot"
  if (error) {
    tone = "danger"
    key = "sync.apiUnavailable"
  } else if (run && !["success", "partial"].includes(run.status)) {
    tone = "danger"
    key = "sync.needsReview"
  } else if (runState === "waiting") {
    tone = "muted"
    key = "sync.waiting"
  } else if (runState === "stale") {
    tone = "honey"
    key = "sync.stale"
  } else if (run?.status === "partial") {
    tone = "honey"
    key = "sync.partial"
  }
  return {
    tone,
    label: t(key),
    runAt: runState === "waiting" ? null : runAt,
    sources,
  }
}
