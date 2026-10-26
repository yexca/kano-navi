import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import { fallbackProfile, POST_WINDOW_DAYS } from "./content"
import type { ArchiveSnapshot, Dashboard, CalendarSnapshot } from "@/types"
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

/** Inclusive Japan-date range of the board's calendar: one Monday-first week. */
export function weekRange(weekStart: Date) {
  return {
    from: localDateKey(weekStart),
    to: localDateKey(addDays(weekStart, 6)),
  }
}

export type ArchiveKind = "schedule" | "posts" | "videos"

export interface ArchiveRequest {
  kind: ArchiveKind
  monthStart: Date
  /** Post archives only: one X handle, or null for every account. */
  account?: string | null
}

/**
 * Inclusive Japan dates a month archive reads: the Monday-first weeks that
 * cover the month for the schedule grid (at most six, the API bound), or the
 * month's own days for posts and videos.
 */
export function archiveRange(kind: ArchiveKind, monthStart: Date) {
  const first = new Date(monthStart.getFullYear(), monthStart.getMonth(), 1)
  const last = new Date(first.getFullYear(), first.getMonth() + 1, 0)
  return kind === "schedule"
    ? {
        from: localDateKey(startOfWeek(first)),
        to: localDateKey(addDays(startOfWeek(last), 6)),
      }
    : { from: localDateKey(first), to: localDateKey(last) }
}

function archiveUrl(request: ArchiveRequest, page: number) {
  const { from, to } = archiveRange(request.kind, request.monthStart)
  const path = { schedule: "calendar", posts: "posts", videos: "videos" }[
    request.kind
  ]
  const account =
    request.kind === "posts" && request.account
      ? `&account=${encodeURIComponent(request.account)}`
      : ""
  return `/api/${path}?from=${from}&to=${to}&page=${page}${account}`
}

const rangeKey = ({ from, to }: { from: string; to: string }) => `${from}/${to}`

async function readScopedJson(url: string, signal: AbortSignal) {
  const response = await fetch(url, {
    signal,
    headers: { accept: "application/json" },
  })
  if (!response.ok) throw new Error(`API ${response.status}`)
  return response.json()
}

type PagedSnapshot = { page: number; hasNext: boolean; revision: number }

/**
 * Rereads the pages already loaded for one range and replaces them
 * atomically. If a write lands between pages, it restarts under the caller's
 * deadline; it never appends pages from different revisions.
 */
async function readLoadedPages<T extends PagedSnapshot>(
  readPage: (page: number) => Promise<T>,
  current: T | null,
  join: (loaded: T, next: T) => T,
): Promise<T> {
  for (let attempt = 0; attempt < 3; attempt++) {
    let next = await readPage(1)
    // Public projections such as liveStatus can expire without a stored
    // write, so even an unchanged revision must replace the loaded pages.
    const revision = next.revision
    let consistent = true
    while (next.hasNext && next.page < (current?.page || 1)) {
      const page = await readPage(next.page + 1)
      if (page.revision !== revision) {
        consistent = false
        break
      }
      next = join(next, page)
    }
    if (consistent) return next
  }
  throw new Error("Snapshot changed during refresh")
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
  const [snapshotVersion, setSnapshotVersion] = useState(0)
  const [status, setStatus] = useState<{
    isLoading: boolean
    isRefreshing: boolean
    error: Error | null
  }>({
    isLoading: true,
    isRefreshing: false,
    error: null,
  })
  const scope = useMemo(() => createRequestScope(), [])
  const [weekStart, setWeekStart] = useState(() => startOfWeek(japanToday()))
  const range = weekRange(weekStart)
  const visibleKey = rangeKey(range)
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
  const rangeRef = useRef(range)
  rangeRef.current = range
  const calendarRef = useRef(calendar)
  calendarRef.current = calendar
  const calendarKeyRef = useRef<string | null>(null)
  const calendarRefreshRef = useRef(false)
  const loadCalendar = useCallback(async () => {
    const { from, to } = rangeRef.current
    const key = rangeKey(rangeRef.current)
    const current = calendarKeyRef.current === key ? calendarRef.current : null
    calendarRefreshRef.current = false
    setCalendarLoading(true)
    try {
      const payload = await scope.run("calendar", async (signal) => {
        const readPage = async (page: number): Promise<CalendarSnapshot> => {
          signal.throwIfAborted()
          const result = await readScopedJson(
            `/api/calendar?from=${from}&to=${to}&page=${page}`,
            signal,
          )
          signal.throwIfAborted()
          if (
            !Array.isArray(result.events) ||
            !result.adjacent ||
            !Number.isInteger(result.total) ||
            !Number.isInteger(result.revision) ||
            result.page !== page
          )
            throw new Error("Invalid calendar snapshot")
          return result
        }
        return readLoadedPages(readPage, current, (loaded, page) => ({
          ...page,
          events: [...loaded.events, ...page.events],
        }))
      })
      if (key !== rangeKey(rangeRef.current)) return
      calendarKeyRef.current = key
      calendarRef.current = payload
      setCalendar(payload)
      setCalendarError(null)
      setCalendarLoading(false)
      return { ok: true }
    } catch (caught) {
      const error = caught instanceof Error ? caught : new Error(String(caught))
      if (
        scope.active &&
        key === rangeKey(rangeRef.current) &&
        error.name !== "AbortError"
      ) {
        setCalendarError(error)
        setCalendarLoading(false)
      }
      return { ok: false }
    }
  }, [scope])
  const loadMoreCalendar = useCallback(async () => {
    const current = calendarRef.current
    if (!current.hasNext || scope.busy("calendar")) return
    const { from, to } = rangeRef.current
    const key = rangeKey(rangeRef.current)
    if (calendarKeyRef.current !== key) return
    try {
      const next = await scope.run("calendar", (signal) =>
        readScopedJson(
          `/api/calendar?from=${from}&to=${to}&page=${current.page + 1}`,
          signal,
        ),
      )
      if (key !== rangeKey(rangeRef.current)) return
      if (next.revision !== current.revision) {
        await loadCalendar()
        return
      }
      const combined = { ...next, events: [...current.events, ...next.events] }
      calendarRef.current = combined
      setCalendar(combined)
      setCalendarError(null)
    } catch (caught) {
      const error = caught instanceof Error ? caught : new Error(String(caught))
      if (
        scope.active &&
        key === rangeKey(rangeRef.current) &&
        error.name !== "AbortError"
      )
        setCalendarError(error)
    }
  }, [scope, loadCalendar])
  const calendarRetryRef = useRef(false)
  calendarRetryRef.current = Boolean(calendarError)
  const reloadRef = useRef<() => Promise<LoadResult>>(async () => ({
    ok: false,
    error: new Error("Page is unavailable"),
  }))
  const load = useCallback(() => reloadRef.current(), [])

  useEffect(() => {
    scope.activate()
    let active = true
    let loadedAt = 0
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
      if (hasLoaded && !needsSnapshot && failures === 0)
        delay = Math.min(delay, Math.max(0, loadedAt + 60000 - Date.now()))
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
            setSnapshotVersion((version) => version + 1)
            revision = payload.meta?.revision ?? null
            // A later full snapshot also refreshes time-based calendar fields.
            // If a page is in flight, the next revision poll consumes this flag.
            calendarRefreshRef.current ||= hasLoaded
            hasLoaded = true
            loadedAt = Date.now()
            needsSnapshot = false
            setStatus({ isLoading: false, isRefreshing: false, error: null })
            if (
              !scope.busy("calendar") &&
              !calendarRetryRef.current &&
              (calendarRefreshRef.current ||
                calendarKeyRef.current !== rangeKey(rangeRef.current) ||
                revision !== calendarRef.current.revision)
            )
              void loadCalendar()
          } else {
            changed = revision == null || Number(payload.revision) !== revision
            if (
              !changed &&
              (calendarRefreshRef.current ||
                Number(payload.revision) !== calendarRef.current.revision) &&
              !scope.busy("calendar") &&
              !calendarRetryRef.current
            )
              void loadCalendar()
          }
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
      void start(
        needsSnapshot || revision == null || Date.now() - loadedAt >= 60000
          ? "snapshot"
          : "revision",
      )
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
      scope.dispose()
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
  }, [scope, loadCalendar])

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
  }, [visibleKey, loadCalendar, scope])
  const currentDashboard = useMemo(
    () => deriveDashboardAt(dashboard, now),
    [dashboard, now],
  )
  return {
    dashboard: currentDashboard,
    snapshotVersion,
    ...status,
    reload: load,
    calendar,
    calendarLoading,
    calendarError,
    reloadCalendar: loadCalendar,
    loadMoreCalendar,
    weekStart,
    setWeekStart,
    calendarRange: range,
  }
}

/**
 * Reads one month of the stored schedule, posts or videos for a dialog, page
 * by page. Like the calendar it follows the board's revision: a change rereads
 * the loaded pages atomically, and a later page from another revision restarts
 * the read instead of being appended. Schedule and video archives also follow
 * successful board snapshots, whose minute/manual refresh expires time-based
 * projections without a revision change. A null request cancels the read.
 */
export function useArchive<T>(
  request: ArchiveRequest | null,
  revision?: number | null,
  snapshotVersion?: number,
) {
  const scope = useMemo(() => createRequestScope(), [])
  const key = request
    ? `${request.kind}:${rangeKey(archiveRange(request.kind, request.monthStart))}:${request.account || ""}`
    : null
  const [state, setState] = useState<{
    key: string | null
    snapshot: ArchiveSnapshot<T> | null
    isLoading: boolean
    error: Error | null
  }>({ key: null, snapshot: null, isLoading: false, error: null })
  const keyRef = useRef(key)
  keyRef.current = key
  const requestRef = useRef(request)
  requestRef.current = request
  const stateRef = useRef(state)
  stateRef.current = state
  const refreshTarget = `${revision ?? ""}/${request?.kind === "posts" ? "" : (snapshotVersion ?? "")}`
  const refreshTargetRef = useRef(refreshTarget)
  refreshTargetRef.current = refreshTarget
  // Consume a target when its read starts. A newer target waits for active
  // work to settle; a failed target retries on the next snapshot, not in a loop.
  const requestedTargetRef = useRef(refreshTarget)

  const readPage = async (
    current: ArchiveRequest,
    page: number,
    signal: AbortSignal,
  ): Promise<ArchiveSnapshot<T>> => {
    signal.throwIfAborted()
    const result = await readScopedJson(archiveUrl(current, page), signal)
    signal.throwIfAborted()
    if (
      !Array.isArray(result.items) ||
      !result.adjacent ||
      !Number.isInteger(result.total) ||
      !Number.isInteger(result.revision) ||
      !Number.isInteger(result.page)
    )
      throw new Error("Invalid archive page")
    return result
  }

  const load = useCallback(async () => {
    const current = requestRef.current
    const currentKey = keyRef.current
    if (!current || !currentKey) return
    requestedTargetRef.current = refreshTargetRef.current
    const loaded =
      stateRef.current.key === currentKey ? stateRef.current.snapshot : null
    setState((value) =>
      value.key === currentKey
        ? { ...value, isLoading: true }
        : { key: currentKey, snapshot: null, isLoading: true, error: null },
    )
    try {
      const snapshot = await scope.run("archive", (signal) =>
        readLoadedPages(
          (page) => readPage(current, page, signal),
          loaded,
          (before, page) => ({
            ...page,
            items: [...before.items, ...page.items],
          }),
        ),
      )
      if (keyRef.current !== currentKey) return
      setState({ key: currentKey, snapshot, isLoading: false, error: null })
    } catch (caught) {
      const error = caught instanceof Error ? caught : new Error(String(caught))
      if (
        scope.active &&
        keyRef.current === currentKey &&
        error.name !== "AbortError"
      )
        setState((value) => ({ ...value, isLoading: false, error }))
    }
  }, [scope])

  const loadMore = useCallback(async () => {
    const current = requestRef.current
    const { key: currentKey, snapshot } = stateRef.current
    if (
      !current ||
      !snapshot?.hasNext ||
      currentKey !== keyRef.current ||
      scope.busy("archive")
    )
      return
    setState((value) => ({ ...value, isLoading: true }))
    try {
      const next = await scope.run("archive", (signal) =>
        readPage(current, snapshot.page + 1, signal),
      )
      if (keyRef.current !== currentKey) return
      // A write moved the pages: reread the loaded ones instead of mixing.
      if (
        next.revision !== snapshot.revision ||
        next.page !== snapshot.page + 1
      ) {
        await load()
        return
      }
      setState({
        key: currentKey,
        snapshot: { ...next, items: [...snapshot.items, ...next.items] },
        isLoading: false,
        error: null,
      })
    } catch (caught) {
      const error = caught instanceof Error ? caught : new Error(String(caught))
      if (
        scope.active &&
        keyRef.current === currentKey &&
        error.name !== "AbortError"
      )
        setState((value) => ({ ...value, isLoading: false, error }))
    }
  }, [scope, load])

  useEffect(() => {
    scope.activate()
    return () => scope.dispose()
  }, [scope])

  useEffect(() => {
    if (key) void load()
    return () => scope.cancelKey("archive")
  }, [key, load, scope])

  useEffect(() => {
    const { key: currentKey, snapshot } = stateRef.current
    if (
      currentKey === keyRef.current &&
      snapshot &&
      requestedTargetRef.current !== refreshTarget &&
      !stateRef.current.isLoading &&
      !scope.busy("archive")
    )
      void load()
  }, [refreshTarget, state.isLoading, load, scope])

  const current = state.key === key ? state : null
  return {
    archive: current?.snapshot || null,
    isLoading: Boolean(key) && (!current || current.isLoading),
    error: current?.error || null,
    reload: load,
    loadMore,
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
