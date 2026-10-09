import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import { fallbackProfile, POST_WINDOW_DAYS } from "./content"
import type { Dashboard, CalendarSnapshot } from "@/types"
import { createRequestScope, startPolling } from "../lib/polling"
import { startOfWeek, japanToday, localDateKey, addDays } from "./format"
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

  const scope = useMemo(() => createRequestScope(), [])
  const loadedAtRef = useRef(0)
  const [weekStart, setWeekStart] = useState(() => startOfWeek(japanToday()))
  const [calendar, setCalendar] = useState<CalendarSnapshot>({
    events: [],
    scheduleImages: [],
    adjacent: { previous: null, next: null },
    total: 0,
    page: 1,
    hasNext: false,
    revision: 0,
    dayCounts: {},
  })
  const [calendarLoading, setCalendarLoading] = useState(true)
  const [calendarError, setCalendarError] = useState<Error | null>(null)
  const weekRef = useRef(weekStart)
  weekRef.current = weekStart
  const readJson = async (url: string, signal: AbortSignal) => {
    const response = await fetch(url, {
      signal,
      headers: { accept: "application/json" },
    })
    if (!response.ok) throw new Error(`API ${response.status}`)
    return response.json()
  }
  const loadCalendar = useCallback(async () => {
    const from = localDateKey(weekRef.current),
      to = localDateKey(addDays(weekRef.current, 6))
    setCalendarLoading(true)
    try {
      const payload = await scope.run("calendar", (signal) =>
        readJson(`/api/calendar?from=${from}&to=${to}&page=1`, signal),
      )
      if (from !== localDateKey(weekRef.current)) return
      if (
        !Array.isArray(payload.events) ||
        !payload.adjacent ||
        !Number.isInteger(payload.total)
      )
        throw new Error("Invalid calendar snapshot")
      setCalendar(payload)
      setCalendarError(null)
      setCalendarLoading(false)
      return { ok: true }
    } catch (error) {
      if (
        scope.active &&
        from === localDateKey(weekRef.current) &&
        error.name !== "AbortError"
      ) {
        setCalendarError(error)
        setCalendarLoading(false)
      }
      return { ok: false }
    }
  }, [scope])
  const calendarRef = useRef(calendar)
  calendarRef.current = calendar
  const loadMoreCalendar = useCallback(async () => {
    const current = calendarRef.current
    if (!current.hasNext || scope.busy("calendar")) return
    const from = localDateKey(weekRef.current),
      to = localDateKey(addDays(weekRef.current, 6))
    try {
      const next = await scope.run("calendar", (signal) =>
        readJson(
          `/api/calendar?from=${from}&to=${to}&page=${current.page + 1}`,
          signal,
        ),
      )
      if (from !== localDateKey(weekRef.current)) return
      if (next.revision !== current.revision) {
        await loadCalendar()
        return
      }
      setCalendar({ ...next, events: [...current.events, ...next.events] })
      setCalendarError(null)
    } catch (error) {
      if (
        scope.active &&
        from === localDateKey(weekRef.current) &&
        error.name !== "AbortError"
      )
        setCalendarError(error)
    }
  }, [scope, loadCalendar])
  const load = useCallback(async () => {
    setStatus((current) => ({
      ...current,
      isLoading: !hasLoadedRef.current,
      isRefreshing: true,
    }))
    try {
      const payload: Dashboard = await scope.run("snapshot", (signal) =>
        readJson(`/api/dashboard?days=${POST_WINDOW_DAYS}`, signal),
      )
      setDashboard({
        ...emptyDashboard,
        ...payload,
        profile: { ...fallbackProfile, ...(payload.profile || {}) },
        summary: { ...emptyDashboard.summary, ...(payload.summary || {}) },
        meta: { ...emptyDashboard.meta, ...(payload.meta || {}) },
      })
      revisionRef.current = payload.meta?.revision ?? null
      hasLoadedRef.current = true
      loadedAtRef.current = Date.now()
      setStatus({ isLoading: false, isRefreshing: false, error: null })
      return { ok: true }
    } catch (caught) {
      const error = caught instanceof Error ? caught : new Error(String(caught))
      if (scope.active && error.name !== "AbortError")
        setStatus({ isLoading: false, isRefreshing: false, error })
      return { ok: false, error }
    }
  }, [scope])
  useEffect(() => {
    scope.activate()
    void load()
    const stop = startPolling(
      async () => {
        if (document.visibilityState === "hidden" || scope.busy("snapshot"))
          return
        const payload = await scope.run("revision", (signal) =>
          readJson(
            `/api/dashboard/revision?since=${revisionRef.current ?? ""}`,
            signal,
          ),
        )
        if (
          !hasLoadedRef.current ||
          payload.revision !== revisionRef.current ||
          Date.now() - loadedAtRef.current >= 60000
        ) {
          const result = await load()
          if (!result.ok) throw result.error
          await loadCalendar()
        }
      },
      { delay: () => REVISION_POLL_MS },
    )
    return () => {
      stop()
      scope.dispose()
    }
  }, [scope, load, loadCalendar])
  useEffect(() => {
    void loadCalendar()
    const stop = startPolling(
      async () => {
        if (
          document.visibilityState === "hidden" ||
          scope.busy("calendar") ||
          !calendarRetryRef.current
        )
          return
        const result = await loadCalendar()
        if (!result?.ok) throw new Error("calendar_unavailable")
      },
      { delay: () => REVISION_POLL_MS },
    )
    return () => {
      stop()
      scope.cancelKey("calendar")
    }
  }, [weekStart, loadCalendar, scope])
  const calendarRetryRef = useRef(false)
  calendarRetryRef.current = Boolean(calendarError)
  const reload = useCallback(async () => {
    const result = await load()
    await loadCalendar()
    return result
  }, [load, loadCalendar])

  const currentDashboard = useMemo(
    () => deriveDashboardAt(dashboard, now),
    [dashboard, now],
  )
  return {
    dashboard: currentDashboard,
    ...status,
    reload,
    calendar,
    calendarLoading,
    calendarError,
    reloadCalendar: loadCalendar,
    loadMoreCalendar,
    weekStart,
    setWeekStart,
  }
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
