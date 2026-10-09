import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import { fallbackProfile, POST_WINDOW_DAYS } from "./content"
import type { Dashboard } from "@/types"
import { deriveDashboardAt } from "./derive-dashboard"

const emptyDashboard: Dashboard = {
  profile: fallbackProfile,
  summary: {
    nextEvent: null,
    nextStream: null,
    latestVideo: null,
    latestPost: null,
    counts: null,
  },
  posts: [],
  events: [],
  videos: [],
  focus: null,
  timeline: [],
  resources: [],
  assets: [],
  scheduleImages: [],
  meta: {
    fetchedAt: null,
    lastSync: null,
    postWindowDays: POST_WINDOW_DAYS,
    xAccounts: ["kano_2525", "_Kanotic"],
    featuredVideoId: null,
  },
}

const REVISION_POLL_MS = 8000
const REQUEST_TIMEOUT_MS = 15000
const MAX_RETRY_MS = 60000
type LoadResult = { ok: true } | { ok: false; error: Error }

/**
 * Reads the stored dashboard snapshot and follows the durable revision
 * counter, so MCP or admin changes appear without a manual reload. Nothing
 * here reaches an external platform.
 */
export function useDashboard(now = Date.now()) {
  const [dashboard, setDashboard] = useState(emptyDashboard)
  const [status, setStatus] = useState<{
    isLoading: boolean
    isRefreshing: boolean
    error: Error | null
  }>({
    isLoading: true,
    isRefreshing: false,
    error: null,
  })
  const reloadRef = useRef<() => Promise<LoadResult>>(async () => ({
    ok: false,
    error: new Error("Page is unavailable"),
  }))
  const load = useCallback(() => reloadRef.current(), [])

  useEffect(() => {
    let active = true
    let hasLoaded = false
    let revision: number | null = null
    let needsSnapshot = true
    let failures = 0
    let generation = 0
    let timer: number | undefined
    let nextCheckAt = 0
    let lastStartedAt = 0
    let flight: {
      kind: "snapshot" | "revision"
      controller: AbortController
      promise: Promise<LoadResult>
    } | null = null

    const clearTimer = () => {
      if (timer !== undefined) window.clearTimeout(timer)
      timer = undefined
    }
    const retryDelay = () =>
      Math.min(MAX_RETRY_MS, REVISION_POLL_MS * 2 ** Math.max(0, failures - 1))
    const schedule = (delay = retryDelay()) => {
      clearTimer()
      nextCheckAt = Date.now() + delay
      if (active && document.visibilityState !== "hidden")
        timer = window.setTimeout(check, delay)
    }

    // One deadline includes headers and JSON body consumption. Aborting also
    // rejects the logical request if a stale transport ignores cancellation.
    const readJson = async (url: string, controller: AbortController) => {
      let deadline: number
      let onAbort: () => void
      const cancelled = new Promise<never>((_, reject) => {
        onAbort = () =>
          reject(controller.signal.reason || new Error("Request cancelled"))
        controller.signal.addEventListener("abort", onAbort, { once: true })
        deadline = window.setTimeout(
          () => controller.abort(new Error("API request timed out")),
          REQUEST_TIMEOUT_MS,
        )
      })
      try {
        return await Promise.race([
          cancelled,
          (async () => {
            const response = await fetch(url, {
              headers: { accept: "application/json" },
              signal: controller.signal,
              cache: "no-store",
            })
            if (!response.ok) throw new Error(`API ${response.status}`)
            return response.json()
          })(),
        ])
      } finally {
        window.clearTimeout(deadline!)
        controller.signal.removeEventListener("abort", onAbort!)
      }
    }

    const start = (kind: "snapshot" | "revision"): Promise<LoadResult> => {
      if (flight?.kind === "snapshot" || flight?.kind === kind)
        return flight.promise
      // Manual refresh supersedes a revision read. Its eventual completion is
      // guarded by generation, even if its body was already being decoded.
      flight?.controller.abort(new Error("Request superseded"))
      clearTimer()
      const currentGeneration = ++generation
      const controller = new AbortController()
      lastStartedAt = Date.now()
      if (kind === "snapshot")
        setStatus((current) => ({
          ...current,
          isLoading: !hasLoaded,
          isRefreshing: true,
        }))
      const isCurrent = () => active && generation === currentGeneration
      const promise = (async (): Promise<LoadResult> => {
        let changed = false
        try {
          const payload = await readJson(
            kind === "snapshot"
              ? `/api/dashboard?days=${POST_WINDOW_DAYS}`
              : `/api/dashboard/revision?since=${encodeURIComponent(String(revision ?? ""))}`,
            controller,
          )
          if (!isCurrent())
            return { ok: false, error: new Error("Request superseded") }
          if (kind === "snapshot") {
            setDashboard({
              ...emptyDashboard,
              ...payload,
              profile: { ...fallbackProfile, ...(payload.profile || {}) },
              summary: {
                ...emptyDashboard.summary,
                ...(payload.summary || {}),
              },
              meta: { ...emptyDashboard.meta, ...(payload.meta || {}) },
            })
            revision = payload.meta?.revision ?? null
            hasLoaded = true
            needsSnapshot = false
            setStatus({ isLoading: false, isRefreshing: false, error: null })
          } else
            changed = revision == null || Number(payload.revision) !== revision
          failures = 0
          return { ok: true }
        } catch (caught) {
          const error =
            caught instanceof Error ? caught : new Error(String(caught))
          if (isCurrent()) {
            failures = Math.min(5, failures + 1)
            if (kind === "snapshot") {
              needsSnapshot = true
              setStatus({ isLoading: false, isRefreshing: false, error })
            }
          }
          return { ok: false, error }
        } finally {
          if (isCurrent()) {
            flight = null
            if (changed) void start("snapshot")
            else schedule()
          }
        }
      })()
      flight = { kind, controller, promise }
      return promise
    }

    function check() {
      if (!active || document.visibilityState === "hidden" || flight) return
      void start(needsSnapshot || revision == null ? "snapshot" : "revision")
    }
    const wake = () => {
      clearTimer()
      if (!active || document.visibilityState === "hidden" || flight) return
      // Visibility + focus often arrive together. Coalesce them, and preserve
      // failure backoff so repeated focus cannot turn an outage into a storm.
      const due = failures ? nextCheckAt : lastStartedAt + 1000
      timer = window.setTimeout(check, Math.max(0, due - Date.now()))
    }
    reloadRef.current = () => start("snapshot")
    void start("snapshot")
    document.addEventListener("visibilitychange", wake)
    window.addEventListener("focus", wake)
    return () => {
      active = false
      generation++
      clearTimer()
      flight?.controller.abort(new Error("Page unmounted"))
      reloadRef.current = async () => ({
        ok: false,
        error: new Error("Page is unavailable"),
      })
      document.removeEventListener("visibilitychange", wake)
      window.removeEventListener("focus", wake)
    }
  }, [])

  const currentDashboard = useMemo(
    () => deriveDashboardAt(dashboard, now),
    [dashboard, now],
  )
  return { dashboard: currentDashboard, ...status, reload: load }
}

/** Re-renders on an interval so countdowns and relative times stay honest. */
export function useNow(intervalMs = 30000) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const update = () => setNow(Date.now())
    const timer = window.setInterval(update, intervalMs)
    document.addEventListener("visibilitychange", update)
    window.addEventListener("focus", update)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener("visibilitychange", update)
      window.removeEventListener("focus", update)
    }
  }, [intervalMs])
  return now
}
