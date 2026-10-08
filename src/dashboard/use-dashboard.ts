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
  const revisionRef = useRef<number | null>(null)
  const hasLoadedRef = useRef(false)

  const load = useCallback(async () => {
    setStatus((current) => ({
      ...current,
      isLoading: !hasLoadedRef.current,
      isRefreshing: true,
    }))
    try {
      const response = await fetch(`/api/dashboard?days=${POST_WINDOW_DAYS}`, {
        headers: { accept: "application/json" },
      })
      if (!response.ok) throw new Error(`API ${response.status}`)
      const payload: Dashboard = await response.json()
      setDashboard({
        ...emptyDashboard,
        ...payload,
        profile: { ...fallbackProfile, ...(payload.profile || {}) },
        summary: { ...emptyDashboard.summary, ...(payload.summary || {}) },
        meta: { ...emptyDashboard.meta, ...(payload.meta || {}) },
      })
      revisionRef.current = payload.meta?.revision ?? null
      hasLoadedRef.current = true
      setStatus({ isLoading: false, isRefreshing: false, error: null })
      return { ok: true }
    } catch (caught) {
      const error = caught instanceof Error ? caught : new Error(String(caught))
      setStatus({ isLoading: false, isRefreshing: false, error })
      return { ok: false, error }
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  useEffect(() => {
    let active = true
    const checkRevision = async () => {
      if (document.visibilityState === "hidden") return
      try {
        const since = encodeURIComponent(String(revisionRef.current ?? ""))
        const response = await fetch(`/api/dashboard/revision?since=${since}`, {
          headers: { accept: "application/json" },
        })
        if (!response.ok || !active) return
        const payload = await response.json()
        if (
          revisionRef.current != null &&
          Number(payload.revision) !== Number(revisionRef.current)
        ) {
          await load()
        }
      } catch {
        // The regular dashboard request owns the visible error state.
      }
    }
    const timer = window.setInterval(checkRevision, REVISION_POLL_MS)
    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [load])

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
