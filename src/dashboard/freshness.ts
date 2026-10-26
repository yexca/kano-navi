import type { Dashboard, Translator } from "../types.ts"

// A local warning threshold, independent of the server's synchronization timer.
export const STALE_AFTER_MS = 24 * 60 * 60 * 1000
// A visitor clock a little behind the server still shows a fresh read; a
// timestamp further ahead is not trusted.
export const CLOCK_SKEW_MS = 5 * 60 * 1000

type Freshness = "fresh" | "stale" | "waiting"

export interface SyncState {
  tone: "leaf" | "honey" | "danger" | "muted"
  label: string
  sources: {
    source: "x" | "youtube"
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

/** Recomputed from the retained snapshot when the local clock changes. */
export function syncStateFor(
  meta: Dashboard["meta"],
  error: unknown,
  now: number,
  t: Translator,
): SyncState {
  const sourceFetchedAt = meta.sourceFetchedAt
  const sources: SyncState["sources"] = sourceFetchedAt
    ? (["x", "youtube"] as const).map((source) => {
        const timestamp = sourceFetchedAt[source]
        const state = freshnessAt(timestamp, now)
        return {
          source,
          timestamp: state === "waiting" ? null : timestamp,
          state,
        }
      })
    : []
  // Older APIs have one snapshot timestamp, without per-source assurances.
  const states = sources.length
    ? sources.map(({ state }) => state)
    : [freshnessAt(meta.fetchedAt, now)]
  let tone: SyncState["tone"] = "leaf"
  let key = "sync.snapshot"
  if (error) {
    tone = "danger"
    key = "sync.apiUnavailable"
  } else if (
    meta.lastSync &&
    !["success", "partial"].includes(meta.lastSync.status)
  ) {
    tone = "danger"
    key = "sync.needsReview"
  } else if (
    freshnessAt(meta.fetchedAt, now) === "waiting" ||
    states.every((state) => state === "waiting")
  ) {
    tone = "muted"
    key = "sync.waiting"
  } else if (states.includes("stale")) {
    tone = "honey"
    key = "sync.stale"
  } else if (
    states.includes("waiting") ||
    meta.lastSync?.status === "partial"
  ) {
    tone = "honey"
    key = "sync.partial"
  }
  return { tone, label: t(key), sources }
}
