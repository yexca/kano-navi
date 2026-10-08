import type { Database as DatabaseConnection } from "better-sqlite3"

const DAY_MS = 86_400_000
export const DEFAULT_SOURCE_LOOKBACK = Object.freeze({ x: 7, youtube: 14 })

export function sourceXHandles() {
  const values = String(
    process.env.X_HANDLES || process.env.X_HANDLE || "kano_2525,_Kanotic",
  ).split(/[,，\s]+/u)
  const handles = [
    ...new Set(
      values
        .map((value) => value.replace(/^@/u, ""))
        .filter((value) => /^[A-Za-z0-9_]{1,30}$/u.test(value)),
    ),
  ]
  return handles.length ? handles : ["kano_2525", "_Kanotic"]
}

export function sourceFetchStatus(database: DatabaseConnection) {
  return {
    x: {
      historyAvailable: Boolean(process.env.X_API_BEARER_TOKEN?.trim()),
      accounts: sourceXHandles().map((account) => ({
        account,
        oldest: oldestSourceTimestamp(database, "x", account),
      })),
    },
    youtube: {
      historyAvailable: Boolean(process.env.YOUTUBE_API_KEY?.trim()),
      oldest: oldestSourceTimestamp(database, "youtube"),
    },
  }
}

function daysValue(value) {
  if (!Number.isInteger(value) || value < 1 || value > 365)
    throw new Error("days must be an integer from 1 to 365")
  return value
}

function japanMidnight(value) {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}$/u.test(value))
    throw new Error("dates must use YYYY-MM-DD")
  const parsed = new Date(`${value}T00:00:00+09:00`)
  if (
    !Number.isFinite(parsed.getTime()) ||
    new Date(parsed.getTime() + 9 * 3_600_000).toISOString().slice(0, 10) !==
      value
  )
    throw new Error("date is invalid")
  return parsed.getTime()
}

export function normalizeFetchWindow(value) {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("fetch window is required")
  const mode = value.mode
  if (mode === "recent" || mode === "before")
    return { mode, days: daysValue(value.days) }
  if (mode === "range") {
    const start = japanMidnight(value.startDate)
    const end = japanMidnight(value.endDate) + DAY_MS
    if (end <= start || end - start > 365 * DAY_MS)
      throw new Error("date range must cover 1 to 365 days")
    return { mode, startDate: value.startDate, endDate: value.endDate }
  }
  throw new Error("unsupported fetch mode")
}

export function normalizeSourceLookback(
  value: Record<string, unknown> = DEFAULT_SOURCE_LOOKBACK,
) {
  if (
    !value ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    Object.keys(value).some((key) => !["x", "youtube"].includes(key))
  )
    throw new Error("sourceLookbackDays must contain x and/or youtube")
  return Object.fromEntries(
    Object.entries(DEFAULT_SOURCE_LOOKBACK).map(([source, fallback]) => [
      source,
      daysValue(value[source] ?? fallback),
    ]),
  )
}

// Bounds are half-open; range dates include both selected days in Japan.
// Backfill starts strictly before the oldest stored item for this account.
export function resolveFetchWindow(
  value,
  { oldest = null, now = Date.now() }: { oldest?: any; now?: any } = {},
) {
  const window = normalizeFetchWindow(value)
  if (window.mode === "range")
    return {
      ...window,
      startTime: new Date(japanMidnight(window.startDate)).toISOString(),
      endTime: new Date(japanMidnight(window.endDate) + DAY_MS).toISOString(),
    }
  const end = window.mode === "before" ? Date.parse(oldest || "") : now
  if (!Number.isFinite(end))
    throw new Error("no stored records to backfill; select a date range first")
  return {
    ...window,
    startTime: new Date(end - window.days * DAY_MS).toISOString(),
    endTime: new Date(end).toISOString(),
  }
}

export function isInFetchWindow(value, window) {
  const timestamp = Date.parse(value || "")
  return (
    Number.isFinite(timestamp) &&
    timestamp >= Date.parse(window.startTime) &&
    timestamp < Date.parse(window.endTime)
  )
}

export function oldestSourceTimestamp(
  database: DatabaseConnection,
  source,
  account = null,
) {
  if (source === "x")
    return (
      database
        .prepare<unknown[], Record<string, any>>(
          "SELECT published_at AS oldest FROM posts WHERE source = 'x' AND lower(account_handle) = lower(?) AND julianday(published_at) IS NOT NULL ORDER BY julianday(published_at) ASC LIMIT 1",
        )
        .get(account)?.oldest || null
    )
  if (source === "youtube")
    return (
      database
        .prepare<unknown[], Record<string, any>>(
          "SELECT published_at AS oldest FROM videos WHERE source = 'youtube' AND julianday(published_at) IS NOT NULL ORDER BY julianday(published_at) ASC LIMIT 1",
        )
        .get()?.oldest || null
    )
  throw new Error("unsupported source")
}
