import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import { fallbackProfile, POST_WINDOW_DAYS } from "./content"
import type {
  ArchiveMonth,
  ArchiveSnapshot,
  Dashboard,
  CalendarSnapshot,
} from "@/types"
import { createRequestScope, startPolling } from "../lib/polling"
import { startOfWeek, japanToday, localDateKey, addDays } from "./format"
import { deriveDashboardAt } from "./derive-dashboard"
import type { VideoCategory } from "./video-groups"

const emptyDashboard: Dashboard = {
  profile: fallbackProfile,
  projection: {
    version: 0,
    nextTransitionAt: null,
    focus: { eventIds: [], videoIds: [] },
    spotlight: null,
    sales: [],
    shelf: {
      leadId: null,
      leadIsFeatured: false,
      reservations: [],
      works: [],
      archives: [],
      shorts: [],
    },
  },
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
  liveNow: null,
  meta: {
    fetchedAt: null,
    sourceFetchedAt: { x: null, youtube: null, bilibili: null, shop: null },
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
  /** The schedule or post month shown; the video timeline ignores it. */
  monthStart: Date
  /** Post archives only: an X handle or Bilibili mid, or null for all. */
  account?: string | null
  /** Video archives only: one category, or null for every video. */
  category?: VideoCategory | null
  /**
   * Video archives only: the month the timeline starts from; null starts from
   * the newest video. With `startPage`, the timeline reads the newest-first
   * pages from the one holding that month, so the newer pages before it can
   * load later; without it, from that month's last day, as nothing newer.
   */
  anchor?: Date | null
  /** Video archives only: the page of the whole timeline holding `anchor`. */
  startPage?: number
}

/** Videos per timeline page; the board's page numbers count in these. */
export const VIDEO_TIMELINE_PAGE_SIZE = 30

/**
 * The page of the newest-first video timeline that holds a month's newest
 * video: every video of the later months comes before it. The month index
 * counts per category, so a chosen category counts its own videos alone.
 */
export function timelineStartPage(
  months: readonly ArchiveMonth[],
  month: string,
  category: string | null | undefined,
) {
  let newer = 0
  for (const entry of months)
    if (entry.month > month)
      newer += category ? entry.counts[category] || 0 : entry.total
  return Math.floor(newer / VIDEO_TIMELINE_PAGE_SIZE) + 1
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

/** The inclusive Japan date the video timeline ends on, or "" for none. */
function timelineUntil(anchor: Date | null | undefined) {
  return anchor
    ? localDateKey(new Date(anchor.getFullYear(), anchor.getMonth() + 1, 0))
    : ""
}

function archiveUrl(request: ArchiveRequest, page: number) {
  if (request.kind === "videos") {
    const until = request.startPage ? "" : timelineUntil(request.anchor)
    const category = request.category ? `&category=${request.category}` : ""
    return `/api/videos?page=${page}&pageSize=${VIDEO_TIMELINE_PAGE_SIZE}${until ? `&until=${until}` : ""}${category}`
  }
  const { from, to } = archiveRange(request.kind, request.monthStart)
  const path = request.kind === "schedule" ? "calendar" : "posts"
  const account =
    request.kind === "posts" && request.account
      ? `&account=${encodeURIComponent(request.account)}`
      : ""
  return `/api/${path}?from=${from}&to=${to}&page=${page}${account}`
}

const rangeKey = ({ from, to }: { from: string; to: string }) => `${from}/${to}`

/** What identifies one archive read: its month or timeline start and filter. */
function archiveKey(request: ArchiveRequest) {
  const span =
    request.kind === "videos"
      ? request.startPage
        ? `from-page:${request.startPage}`
        : `until:${timelineUntil(request.anchor)}`
      : rangeKey(archiveRange(request.kind, request.monthStart))
  return `${request.kind}:${span}:${request.account || ""}:${request.category || ""}`
}

async function readScopedJson(url: string, signal: AbortSignal) {
  const response = await fetch(url, {
    signal,
    headers: { accept: "application/json" },
    cache: "no-cache",
  })
  if (!response.ok) throw new Error(`API ${response.status}`)
  return response.json()
}

type PagedSnapshot = { page: number; hasNext: boolean; revision: number }

/**
 * Rereads the pages already loaded for one range, from `first` through
 * `through`, and replaces them atomically. If a write lands between pages, it
 * restarts under the caller's deadline; it never appends pages from different
 * revisions.
 */
async function readLoadedPages<T extends PagedSnapshot>(
  readPage: (page: number) => Promise<T>,
  current: T | null,
  join: (loaded: T, next: T) => T,
  first = 1,
  through = current?.page || 1,
): Promise<T> {
  for (let attempt = 0; attempt < 3; attempt++) {
    let next = await readPage(first)
    // Public projections such as liveStatus can expire without a stored
    // write, so even an unchanged revision must replace the loaded pages.
    const revision = next.revision
    let consistent = true
    while (next.hasNext && next.page < through) {
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

/** Revision polling, used only while the revision stream is unavailable. */
const REVISION_POLL_MS = 30000
/** First retry after a failed read; it doubles up to MAX_RETRY_MS. */
const RETRY_MS = 8000
const REQUEST_TIMEOUT_MS = 15000
const MAX_RETRY_MS = 60000
/** Time-derived projections such as liveStatus expire without a revision. */
const SNAPSHOT_MS = 60000
/** Pushed changes spread their snapshot reads over this window. */
const PUSH_JITTER_MS = 3000
/** Stream errors without a message in between before polling takes over. */
const STREAM_FAILURES = 3
/** How long polling stands in before the stream is tried again. */
const STREAM_RETRY_MS = 300000
const REVISION_STREAM_URL = "/api/dashboard/events"
type LoadResult = { ok: true } | { ok: false; error: Error }

/**
 * The last snapshot this browser read, shown on the next visit until the
 * first read replaces it. Additive fields are merged with safe defaults.
 */
const SNAPSHOT_CACHE_KEY = "kano-dashboard-snapshot-v1"
/** An older stored snapshot is dropped instead of shown. */
const SNAPSHOT_CACHE_MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000

/** A snapshot payload over the defaults, as the board renders it. */
function mergeSnapshot(payload: Partial<Dashboard>): Dashboard {
  return {
    ...emptyDashboard,
    ...payload,
    profile: { ...fallbackProfile, ...(payload.profile || {}) },
    summary: { ...emptyDashboard.summary, ...(payload.summary || {}) },
    projection: payload.projection
      ? {
          ...emptyDashboard.projection,
          ...payload.projection,
          focus: {
            ...emptyDashboard.projection!.focus,
            ...(payload.projection.focus || {}),
          },
          shelf: {
            ...emptyDashboard.projection!.shelf,
            ...(payload.projection.shelf || {}),
          },
        }
      : emptyDashboard.projection,
    meta: { ...emptyDashboard.meta, ...(payload.meta || {}) },
  }
}

function readCachedSnapshot(): Dashboard | null {
  try {
    const stored = JSON.parse(
      window.localStorage.getItem(SNAPSHOT_CACHE_KEY) || "null",
    )
    const snapshot = stored?.snapshot
    const age = Date.now() - stored?.savedAt
    if (
      !(age >= 0 && age <= SNAPSHOT_CACHE_MAX_AGE_MS) ||
      !snapshot ||
      typeof snapshot !== "object" ||
      (["posts", "events", "videos", "timeline", "resources"] as const).some(
        (key) => key in snapshot && !Array.isArray(snapshot[key]),
      )
    )
      return null
    // A stored broadcast may have ended; only a fresh read says it is live.
    return mergeSnapshot({ ...snapshot, liveNow: null })
  } catch {
    return null
  }
}

function writeCachedSnapshot(snapshot: unknown) {
  try {
    window.localStorage.setItem(
      SNAPSHOT_CACHE_KEY,
      JSON.stringify({ savedAt: Date.now(), snapshot }),
    )
  } catch {
    // Without storage the next visit starts from the placeholder.
  }
}

/**
 * Reads the stored dashboard snapshot and follows the durable revision
 * counter, so MCP or admin changes appear without a manual reload. The
 * server pushes revision changes over an EventSource stream while the page
 * is visible; when the stream is unavailable the hook polls the revision
 * instead. Nothing here reaches an external platform. A visit starts from
 * the last snapshot this browser stored, refreshing it, rather than from the
 * loading placeholder.
 */
export function useDashboard(now = Date.now()) {
  const [cached] = useState(readCachedSnapshot)
  const shownCacheRef = useRef(cached != null)
  const [dashboard, setDashboard] = useState(cached || emptyDashboard)
  const [snapshotVersion, setSnapshotVersion] = useState(0)
  const [status, setStatus] = useState<{
    isLoading: boolean
    isRefreshing: boolean
    error: Error | null
  }>({
    isLoading: !cached,
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
  // The week the calendar snapshot belongs to. While another week loads, the
  // board keeps this one rendered instead of pairing its data with new dates.
  const [calendarShownRange, setCalendarShownRange] = useState<{
    from: string
    to: string
  } | null>(null)
  const [calendarLoading, setCalendarLoading] = useState(true)
  const [calendarFailure, setCalendarFailure] = useState<{
    key: string
    error: Error
  } | null>(null)
  const calendarError =
    calendarFailure?.key === visibleKey ? calendarFailure.error : null
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
    // Only a week without a loaded snapshot shows the placeholder; a refresh
    // keeps the current week rendered and swaps its data in place.
    if (!current) setCalendarLoading(true)
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
      setCalendarShownRange({ from, to })
      setCalendarFailure(null)
      setCalendarLoading(false)
      return { ok: true }
    } catch (caught) {
      const error = caught instanceof Error ? caught : new Error(String(caught))
      if (
        scope.active &&
        key === rangeKey(rangeRef.current) &&
        error.name !== "AbortError"
      ) {
        setCalendarFailure({ key, error })
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
      setCalendarFailure(null)
    } catch (caught) {
      const error = caught instanceof Error ? caught : new Error(String(caught))
      if (
        scope.active &&
        key === rangeKey(rangeRef.current) &&
        error.name !== "AbortError"
      )
        setCalendarFailure({ key, error })
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
    // The revision stream, while open or reconnecting. Without it the hook
    // polls the revision; pollUntil holds polling after the stream failed.
    let stream: EventSource | null = null
    let streamErrors = 0
    let pollUntil = 0
    let pushTimer: number | undefined
    // A change pushed during a snapshot read is checked when the read ends.
    let pushedRevision: number | null = null
    let pushPending = false

    const clearTimer = () => {
      if (timer !== undefined) window.clearTimeout(timer)
      timer = undefined
    }
    const retryDelay = () =>
      failures
        ? Math.min(MAX_RETRY_MS, RETRY_MS * 2 ** (failures - 1))
        : stream
          ? SNAPSHOT_MS
          : REVISION_POLL_MS
    const schedule = (delay = retryDelay()) => {
      clearTimer()
      if (hasLoaded && !needsSnapshot && failures === 0)
        delay = Math.min(
          delay,
          Math.max(0, loadedAt + SNAPSHOT_MS - Date.now()),
        )
      nextCheckAt = Date.now() + delay
      if (active && document.visibilityState !== "hidden")
        timer = window.setTimeout(check, delay)
    }
    const calendarStale = (value: number) =>
      (calendarRefreshRef.current || value !== calendarRef.current.revision) &&
      !scope.busy("calendar") &&
      !calendarRetryRef.current

    const clearPush = () => {
      if (pushTimer !== undefined) window.clearTimeout(pushTimer)
      pushTimer = undefined
    }
    // Every open board hears a change at once; a random delay spreads their
    // snapshot reads.
    const pushSnapshot = () => {
      if (pushTimer !== undefined) return
      pushTimer = window.setTimeout(() => {
        pushTimer = undefined
        if (!active || document.visibilityState === "hidden") return
        if (flight?.kind === "snapshot") pushPending = true
        else void start("snapshot")
      }, Math.random() * PUSH_JITTER_MS)
    }
    const onPushedRevision = (value: number) => {
      pushedRevision = value
      if (flight?.kind === "snapshot") pushPending = true
      else if (revision == null || value !== revision) pushSnapshot()
      else if (calendarStale(value)) void loadCalendar()
    }
    const closeStream = () => {
      stream?.close()
      stream = null
      clearPush()
    }
    const openStream = () => {
      if (
        stream ||
        typeof EventSource !== "function" ||
        !active ||
        document.visibilityState === "hidden" ||
        Date.now() < pollUntil
      )
        return
      const source = new EventSource(REVISION_STREAM_URL)
      stream = source
      source.onmessage = (event) => {
        if (stream !== source) return
        streamErrors = 0
        const value = Number(event.data)
        if (Number.isSafeInteger(value)) onPushedRevision(value)
      }
      source.onerror = () => {
        if (stream !== source) return
        streamErrors++
        // EventSource reconnects by itself after a dropped connection, and
        // the reconnected stream resends the current revision. A refused
        // stream stays closed; that or repeated drops hand over to polling.
        if (
          source.readyState === EventSource.CLOSED ||
          streamErrors >= STREAM_FAILURES
        ) {
          closeStream()
          streamErrors = 0
          pollUntil = Date.now() + STREAM_RETRY_MS
          if (!flight) schedule()
        }
      }
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
            // The snapshot carries an ETag, so an unchanged one revalidates
            // with a bodiless 304; the revision reply is never stored.
            const response = await fetch(url, {
              headers: { accept: "application/json" },
              signal: controller.signal,
              cache: "no-cache",
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
          isLoading: !hasLoaded && !shownCacheRef.current,
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
            setDashboard(mergeSnapshot(payload))
            writeCachedSnapshot(payload)
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
            if (!changed && calendarStale(Number(payload.revision)))
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
            if (kind === "snapshot" && pushPending) {
              pushPending = false
              if (pushedRevision != null && pushedRevision !== revision)
                pushSnapshot()
              else if (pushedRevision != null && calendarStale(pushedRevision))
                void loadCalendar()
            }
          }
        }
      })()
      flight = { kind, controller, promise }
      return promise
    }

    function check() {
      if (!active || document.visibilityState === "hidden" || flight) return
      openStream()
      if (
        needsSnapshot ||
        revision == null ||
        Date.now() - loadedAt >= SNAPSHOT_MS
      )
        void start("snapshot")
      // The stream reports changes; between them only the minute's
      // revalidation is due.
      else if (stream) schedule()
      else void start("revision")
    }
    const wake = () => {
      clearTimer()
      if (!active) return
      // A hidden page holds no stream; a visible one reconnects, and the
      // stream's first message is the current revision.
      if (document.visibilityState === "hidden") {
        closeStream()
        return
      }
      openStream()
      if (flight) return
      // Visibility + focus often arrive together. Coalesce them, and preserve
      // failure backoff so repeated focus cannot turn an outage into a storm.
      const due = failures ? nextCheckAt : lastStartedAt + 1000
      timer = window.setTimeout(check, Math.max(0, due - Date.now()))
    }
    reloadRef.current = () => start("snapshot")
    void start("snapshot")
    openStream()
    document.addEventListener("visibilitychange", wake)
    window.addEventListener("focus", wake)
    return () => {
      active = false
      scope.dispose()
      generation++
      clearTimer()
      closeStream()
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
      { delay: () => RETRY_MS },
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
    calendarShownRange,
    // The selected week is loading until its own snapshot arrives; a failed
    // read stops reporting it until a retry starts.
    calendarLoading:
      calendarShownRange == null || rangeKey(calendarShownRange) !== visibleKey
        ? calendarLoading || !calendarError
        : false,
    calendarError,
    reloadCalendar: loadCalendar,
    loadMoreCalendar,
    weekStart,
    setWeekStart,
    calendarRange: range,
  }
}

const monthKeyPattern = /^\d{4}-(0[1-9]|1[0-2])$/

/** Where each month index lives and which field holds its per-key counts. */
const monthIndexes = {
  posts: { url: "/api/posts/months", counts: "accountCounts" },
  videos: { url: "/api/videos/months", counts: "categoryCounts" },
} as const

function readArchiveMonth(
  value: unknown,
  countsField: string,
): ArchiveMonth | null {
  if (!value || typeof value !== "object") return null
  const month = value as Record<string, unknown>
  const counts = month[countsField]
  if (
    typeof month.month !== "string" ||
    !monthKeyPattern.test(month.month) ||
    !Number.isInteger(month.total) ||
    !counts ||
    typeof counts !== "object"
  )
    return null
  return {
    month: month.month,
    total: month.total as number,
    counts: counts as Record<string, number>,
  }
}

/**
 * Reads the month index of the post archive or the video timeline while
 * `enabled`: every Japan month holding records, oldest first. It rereads when
 * the board's revision changes. A failed read keeps the known index, and with
 * none `months` stays null, so the picker offers every month.
 */
export function useArchiveMonths(
  kind: "posts" | "videos",
  enabled: boolean,
  revision?: number | null,
) {
  const scope = useMemo(() => createRequestScope(), [])
  const [state, setState] = useState<{
    kind: string
    months: ArchiveMonth[]
  } | null>(null)

  useEffect(() => {
    scope.activate()
    return () => scope.dispose()
  }, [scope])

  useEffect(() => {
    if (!enabled) return undefined
    const index = monthIndexes[kind]
    scope
      .run("months", async (signal) => {
        const result = await readScopedJson(index.url, signal)
        if (!Array.isArray(result?.months))
          throw new Error("Invalid month index")
        return (result.months as unknown[])
          .map((month) => readArchiveMonth(month, index.counts))
          .filter((month): month is ArchiveMonth => month !== null)
      })
      .then(
        (months) => setState({ kind, months }),
        () => {
          // A failed read keeps a known index; the picker works without one.
        },
      )
    return () => scope.cancelKey("months")
  }, [kind, enabled, revision, scope])

  return state?.kind === kind ? state.months : null
}

/**
 * Reads one schedule or post month, or the video timeline, for a dialog, page
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
  const key = request ? archiveKey(request) : null
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
    // A refresh rereads the run it has loaded, from its first page. A timeline
    // from a chosen month first reads the page after the month's too, so the
    // month always has enough below it to scroll to the top.
    const first = loaded?.firstPage ?? current.startPage ?? 1
    const through = loaded ? loaded.page : current.startPage ? first + 1 : 1
    try {
      const read = await scope.run("archive", (signal) =>
        readLoadedPages(
          (page) => readPage(current, page, signal),
          loaded,
          (before, page) => ({
            ...page,
            firstPage: before.firstPage ?? before.page,
            items: [...before.items, ...page.items],
          }),
          first,
          through,
        ),
      )
      if (keyRef.current !== currentKey) return
      // The first read names its page: a shorter timeline clamps it.
      const snapshot = { ...read, firstPage: read.firstPage ?? read.page }
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
        snapshot: {
          ...next,
          firstPage: snapshot.firstPage,
          items: [...snapshot.items, ...next.items],
        },
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

  // Reads the page before the first loaded one and puts its newer records
  // above the loaded ones; a write in between rereads the whole run from that
  // page instead.
  const loadNewer = useCallback(async () => {
    const current = requestRef.current
    const { key: currentKey, snapshot } = stateRef.current
    const first = snapshot?.firstPage ?? 1
    if (
      !current ||
      !snapshot ||
      first <= 1 ||
      currentKey !== keyRef.current ||
      scope.busy("archive")
    )
      return
    setState((value) => ({ ...value, isLoading: true }))
    try {
      const newer = await scope.run("archive", (signal) =>
        readPage(current, first - 1, signal),
      )
      if (keyRef.current !== currentKey) return
      if (newer.revision !== snapshot.revision || newer.page !== first - 1) {
        // Reread the whole run from the newer page, so the request still
        // brings its newer records.
        requestedTargetRef.current = refreshTargetRef.current
        const read = await scope.run("archive", (signal) =>
          readLoadedPages(
            (page) => readPage(current, page, signal),
            snapshot,
            (before, page) => ({
              ...page,
              firstPage: before.firstPage ?? before.page,
              items: [...before.items, ...page.items],
            }),
            first - 1,
            snapshot.page,
          ),
        )
        if (keyRef.current !== currentKey) return
        setState({
          key: currentKey,
          snapshot: { ...read, firstPage: read.firstPage ?? read.page },
          isLoading: false,
          error: null,
        })
        return
      }
      setState({
        key: currentKey,
        snapshot: {
          ...snapshot,
          firstPage: newer.page,
          items: [...newer.items, ...snapshot.items],
        },
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
  }, [scope])

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
    loadNewer,
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
