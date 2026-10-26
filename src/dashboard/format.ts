import type { LiveStatus, ScheduleEvent, Translator, Video } from "../types.ts"

export const JAPAN_TIME_ZONE = "Asia/Tokyo"

/** Anything `new Date()` accepts; empty and invalid values format as blank. */
type DateInput = Date | string | number | null | undefined

const intlLocales: Record<string, string> = {
  "zh-CN": "zh-CN",
  ja: "ja-JP",
  en: "en-US",
}

export function intlLocale(locale: string) {
  return intlLocales[locale] || intlLocales.en
}

const formatterCache = new Map<string, Intl.DateTimeFormat>()

function formatter(locale: string, options: Intl.DateTimeFormatOptions) {
  const key = `${locale}|${JSON.stringify(options)}`
  let cached = formatterCache.get(key)
  if (!cached) {
    cached = new Intl.DateTimeFormat(intlLocale(locale), options)
    formatterCache.set(key, cached)
  }
  return cached
}

function parts(
  date: Date,
  locale: string,
  options: Intl.DateTimeFormatOptions,
) {
  const result: Partial<Record<Intl.DateTimeFormatPartTypes, string>> = {}
  for (const part of formatter(locale, options).formatToParts(date))
    result[part.type] = part.value
  return result
}

function toDate(value: DateInput) {
  if (!value) return null
  const date = value instanceof Date ? value : new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

const pad = (value: number) => String(value).padStart(2, "0")

// Calendar math uses local Date objects whose Y/M/D mirror the Japan date, so
// week navigation is independent of the visitor's own time zone.
export function dateKey(value: DateInput) {
  const date = toDate(value)
  if (!date) return ""
  const { year, month, day } = parts(date, "en", {
    timeZone: JAPAN_TIME_ZONE,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  })
  return `${year}-${month}-${day}`
}

export function localDateKey(date: Date) {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

export function japanToday() {
  const [year, month, day] = dateKey(new Date()).split("-").map(Number)
  return new Date(year, month - 1, day)
}

export function addDays(date: Date, amount: number) {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate() + amount)
}

export function startOfWeek(date: Date) {
  return addDays(date, -((date.getDay() + 6) % 7))
}

export function weekStartKey(value: DateInput) {
  const key = dateKey(value)
  if (!key) return ""
  const [year, month, day] = key.split("-").map(Number)
  return localDateKey(startOfWeek(new Date(year, month - 1, day)))
}

export function formatWeekRange(weekStart: Date, locale: string) {
  const weekEnd = addDays(weekStart, 6)
  const format = formatter(locale, { month: "short", day: "numeric" })
  return typeof format.formatRange === "function"
    ? format.formatRange(weekStart, weekEnd)
    : `${format.format(weekStart)} – ${format.format(weekEnd)}`
}

export function formatMonth(date: Date, locale: string) {
  return formatter(locale, { year: "numeric", month: "long" }).format(date)
}

/** The first day of a calendar date's month, as a local Date. */
export function monthStartOf(date: Date) {
  return new Date(date.getFullYear(), date.getMonth(), 1)
}

export function formatLongDate(date: Date, locale: string) {
  return formatter(locale, {
    month: "long",
    day: "numeric",
    weekday: "short",
  }).format(date)
}

/** "08.29 21:00" in Japan time. */
export function formatStamp(value: DateInput) {
  const date = toDate(value)
  if (!date) return "—"
  const p = parts(date, "en", {
    timeZone: JAPAN_TIME_ZONE,
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  })
  return `${p.month}.${p.day} ${p.hour}:${p.minute}`
}

export function formatJapanTime(value: DateInput) {
  const date = toDate(value)
  if (!date) return ""
  const p = parts(date, "en", {
    timeZone: JAPAN_TIME_ZONE,
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  })
  return `${p.hour}:${p.minute}`
}

export function formatJapanDay(value: DateInput, locale: string) {
  const date = toDate(value)
  if (!date) return ""
  return formatter(locale, {
    timeZone: JAPAN_TIME_ZONE,
    month: "short",
    day: "numeric",
    weekday: "short",
  }).format(date)
}

/** The Japan month, day and weekday of an instant, for a date block. */
export function japanDateParts(value: DateInput, locale: string) {
  const date = toDate(value)
  if (!date) return null
  const pick = (options: Intl.DateTimeFormatOptions) =>
    formatter(locale, { timeZone: JAPAN_TIME_ZONE, ...options }).format(date)
  return {
    day: pick({ day: "numeric" }).replace(/\D/gu, ""),
    month: pick({ month: "short" }),
    weekday: pick({ weekday: "short" }),
  }
}

export function formatVisitorTime(value: DateInput, locale: string) {
  const date = toDate(value)
  if (!date) return ""
  return formatter(locale, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    timeZoneName: "short",
  }).format(date)
}

export function visitorIsInJapan() {
  try {
    return (
      Intl.DateTimeFormat().resolvedOptions().timeZone === JAPAN_TIME_ZONE ||
      new Date().getTimezoneOffset() === -540
    )
  } catch {
    return false
  }
}

const relativeCache = new Map<string, Intl.RelativeTimeFormat>()

export function formatRelative(
  value: DateInput,
  locale: string,
  now = Date.now(),
) {
  const date = toDate(value)
  if (!date) return ""
  let format = relativeCache.get(locale)
  if (!format) {
    format = new Intl.RelativeTimeFormat(intlLocale(locale), {
      numeric: "auto",
    })
    relativeCache.set(locale, format)
  }
  const seconds = Math.round((date.getTime() - now) / 1000)
  const abs = Math.abs(seconds)
  if (abs < 60) return format.format(0, "second")
  if (abs < 3600) return format.format(Math.round(seconds / 60), "minute")
  if (abs < 86400) return format.format(Math.round(seconds / 3600), "hour")
  if (abs < 86400 * 30) return format.format(Math.round(seconds / 86400), "day")
  return format.format(Math.round(seconds / (86400 * 30)), "month")
}

export function countdownParts(value: DateInput, now = Date.now()) {
  const date = toDate(value)
  if (!date) return null
  const remaining = date.getTime() - now
  if (remaining <= 0) return { past: true, days: 0, hours: 0, minutes: 0 }
  const totalMinutes = Math.floor(remaining / 60000)
  return {
    past: false,
    days: Math.floor(totalMinutes / 1440),
    hours: Math.floor((totalMinutes % 1440) / 60),
    minutes: totalMinutes % 60,
  }
}

export function formatCompact(value: unknown, locale: string) {
  const number = Number(value)
  if (!Number.isFinite(number) || number <= 0) return ""
  return new Intl.NumberFormat(intlLocale(locale), {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(number)
}

// ---------------------------------------------------------------------------
// Events

export function eventDateKey(event: ScheduleEvent) {
  return event.startsOn || dateKey(event.startsAt)
}

export function eventIsUpcoming(event: ScheduleEvent, now = Date.now()) {
  const timestamp = Date.parse(event.startsAt || "")
  if (!Number.isNaN(timestamp)) return timestamp >= now
  const day = eventDateKey(event)
  if (day) return day >= dateKey(new Date(now))
  return Boolean(event.isUpcoming)
}

export const LIVE_GRACE_MS = 3 * 3600 * 1000

/** The probe-confirmed broadcast state, or null when only the start is known. */
export function confirmedLiveStatus(
  item: { liveStatus?: LiveStatus | null } | null | undefined,
): LiveStatus | null {
  const status = item?.liveStatus
  return status === "live" || status === "upcoming" || status === "ended"
    ? status
    : null
}

// A confirmed state decides first. Without one, only streams and memberships
// are presumed live without an end; other YouTube activities, such as a
// premiere, stay live only until their explicit end.
export function eventIsLive(event: ScheduleEvent, now: number) {
  const isStream = ["stream", "member"].includes(event.eventType)
  if (!isStream && event.source !== "youtube") return false
  const start = Date.parse(event.startsAt || "")
  const end = Date.parse(event.endsAt || "")
  const confirmed = confirmedLiveStatus(event)
  if (confirmed) return confirmed === "live" && !(end <= now)
  if (!(start <= now)) return false
  if (Number.isFinite(end)) return end > now
  return isStream && start > now - LIVE_GRACE_MS
}

const cancelledStatuses = [
  "cancelled",
  "canceled",
  "cancel",
  "取消",
  "已取消",
  "中止",
  "キャンセル",
]
const pendingStatuses = [
  "pending",
  "tentative",
  "unknown",
  "待确认",
  "待补充",
  "確認待ち",
  "未定",
]

/** upcoming | live | done | pending | cancelled | cancellation_review */
export function eventStatus(event: ScheduleEvent, now = Date.now()) {
  if (event.cancellationStatus === "manual_confirmed") return "cancelled"
  if (event.cancellationStatus === "llm_suspected") return "cancellation_review"
  const status = String(event.status || "")
    .trim()
    .toLowerCase()
  if (cancelledStatuses.includes(status) || status.startsWith("cancel"))
    return "cancelled"
  // The stage's On air rule, so the schedule never calls it ended meanwhile.
  if (eventIsLive(event, now)) return "live"
  // A waiting room past its reserved time has not started, as the stage shows.
  if (confirmedLiveStatus(event) === "upcoming") return "upcoming"
  if (eventIsUpcoming(event, now)) return "upcoming"
  if (event.timePrecision === "unknown" || pendingStatuses.includes(status))
    return "pending"
  return "done"
}

export function eventIsManual(event: ScheduleEvent) {
  return event.provenance === "manual" || Boolean(event.manualLocked)
}

export function eventTypeKey(event: ScheduleEvent) {
  const type = String(event.eventType || "").toLowerCase()
  return ["stream", "member", "event", "release", "video"].includes(type)
    ? `event.type.${type}`
    : null
}

export function sortEvents(events: ScheduleEvent[]) {
  return [...events].sort(
    (a, b) =>
      scheduleSortTime(a.startsAt, a.startsOn) -
        scheduleSortTime(b.startsAt, b.startsOn) ||
      String(a.id || "").localeCompare(String(b.id || "")),
  )
}

// Date-only entries follow timed entries on the same Japan date. This is a
// sorting boundary only; it does not supply an event start or countdown.
export function scheduleSortTime(
  time: string | null | undefined,
  dateOnly: string | null = null,
) {
  const instant = Date.parse(time || "")
  if (Number.isFinite(instant)) return instant
  const dayEnd = Date.parse(`${dateOnly}T23:59:59.999+09:00`)
  return Number.isFinite(dayEnd) ? dayEnd : Infinity
}

// ---------------------------------------------------------------------------
// Videos

const videoKindKeys: Record<string, string> = {
  GAMEPLAY: "media.kind.gameplay",
  TALK: "media.kind.talk",
  "LIVE ARCHIVE": "media.kind.liveArchive",
  "UPCOMING LIVE": "media.kind.upcoming",
  MUSIC: "media.kind.music",
  VIDEO: "media.kind.video",
}

export function videoKindLabel(video: Video, t: Translator) {
  // The title classifier can call a Short a stream; its format says more.
  if (video?.isShort === true) return t("media.kind.short")
  const kind = String(video?.kind || "")
    .trim()
    .toUpperCase()
  // A reservation keeps its "UPCOMING LIVE" kind past its start until a watch
  // page is read again; it may be on air, so it is a stream, not an archive.
  if (kind === "UPCOMING LIVE" && !video.isUpcoming)
    return t("event.type.stream")
  if (videoKindKeys[kind]) return t(videoKindKeys[kind])
  if (video?.kind && !/[぀-ヿ㐀-鿿]/u.test(video.kind)) return video.kind
  return t(video?.isUpcoming ? "media.kind.upcoming" : "media.kind.video")
}

/** Strip the trailing 【鹿乃まほろ/ミリプロ】 credit that every title repeats. */
export function cleanVideoTitle(title: string | null | undefined) {
  return String(title || "")
    .replace(/\s*【\s*鹿乃まほろ\s*[/／]\s*ミリプロ\s*】\s*/gu, " ")
    .trim()
}

export function videoTime(video: Video) {
  return video.scheduledAt || video.publishedAt || null
}

/** A video length as the platforms show it: "4:49" or "2:01:05". */
export function formatDuration(seconds: number | null | undefined) {
  const total = Math.floor(Number(seconds))
  if (!Number.isFinite(total) || total <= 0) return ""
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const rest = pad(total % 60)
  return hours ? `${hours}:${pad(minutes)}:${rest}` : `${minutes}:${rest}`
}

/** The same length as an HTML duration string, such as "PT2H1M5S". */
export function durationAttribute(seconds: number) {
  const total = Math.floor(seconds)
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  return `PT${hours ? `${hours}H` : ""}${minutes ? `${minutes}M` : ""}${total % 60}S`
}
