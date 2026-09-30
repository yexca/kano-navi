export const JAPAN_TIME_ZONE = "Asia/Tokyo"

const intlLocales = {
  "zh-CN": "zh-CN",
  ja: "ja-JP",
  en: "en-US",
}

export function intlLocale(locale) {
  return intlLocales[locale] || intlLocales.en
}

const formatterCache = new Map()

function formatter(locale, options) {
  const key = `${locale}|${JSON.stringify(options)}`
  if (!formatterCache.has(key)) {
    formatterCache.set(
      key,
      new Intl.DateTimeFormat(intlLocale(locale), options),
    )
  }
  return formatterCache.get(key)
}

function parts(date, locale, options) {
  return formatter(locale, options)
    .formatToParts(date)
    .reduce((result, part) => {
      result[part.type] = part.value
      return result
    }, {})
}

function toDate(value) {
  if (!value) return null
  const date = value instanceof Date ? value : new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

const pad = (value) => String(value).padStart(2, "0")

// Calendar math uses local Date objects whose Y/M/D mirror the Japan date, so
// week navigation is independent of the visitor's own time zone.
export function dateKey(value) {
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

export function localDateKey(date) {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

export function japanToday() {
  const [year, month, day] = dateKey(new Date()).split("-").map(Number)
  return new Date(year, month - 1, day)
}

export function addDays(date, amount) {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate() + amount)
}

export function startOfWeek(date) {
  return addDays(date, -((date.getDay() + 6) % 7))
}

export function weekStartKey(value) {
  const key = dateKey(value)
  if (!key) return ""
  const [year, month, day] = key.split("-").map(Number)
  return localDateKey(startOfWeek(new Date(year, month - 1, day)))
}

export function formatWeekRange(weekStart, locale) {
  const weekEnd = addDays(weekStart, 6)
  const format = formatter(locale, { month: "short", day: "numeric" })
  return typeof format.formatRange === "function"
    ? format.formatRange(weekStart, weekEnd)
    : `${format.format(weekStart)} – ${format.format(weekEnd)}`
}

export function formatLongDate(date, locale) {
  return formatter(locale, {
    month: "long",
    day: "numeric",
    weekday: "short",
  }).format(date)
}

/** "08.29 21:00" in Japan time. */
export function formatStamp(value) {
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

export function formatJapanTime(value) {
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

export function formatJapanDay(value, locale) {
  const date = toDate(value)
  if (!date) return ""
  return formatter(locale, {
    timeZone: JAPAN_TIME_ZONE,
    month: "short",
    day: "numeric",
    weekday: "short",
  }).format(date)
}

export function formatVisitorTime(value, locale) {
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

const relativeCache = new Map()

export function formatRelative(value, locale, now = Date.now()) {
  const date = toDate(value)
  if (!date) return ""
  if (!relativeCache.has(locale)) {
    relativeCache.set(
      locale,
      new Intl.RelativeTimeFormat(intlLocale(locale), { numeric: "auto" }),
    )
  }
  const format = relativeCache.get(locale)
  const seconds = Math.round((date.getTime() - now) / 1000)
  const abs = Math.abs(seconds)
  if (abs < 60) return format.format(0, "second")
  if (abs < 3600) return format.format(Math.round(seconds / 60), "minute")
  if (abs < 86400) return format.format(Math.round(seconds / 3600), "hour")
  if (abs < 86400 * 30) return format.format(Math.round(seconds / 86400), "day")
  return format.format(Math.round(seconds / (86400 * 30)), "month")
}

export function countdownParts(value, now = Date.now()) {
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

export function formatCompact(value, locale) {
  const number = Number(value)
  if (!Number.isFinite(number) || number <= 0) return ""
  return new Intl.NumberFormat(intlLocale(locale), {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(number)
}

// ---------------------------------------------------------------------------
// Events

export function eventDateKey(event) {
  return event.startsOn || dateKey(event.startsAt)
}

export function eventIsUpcoming(event, now = Date.now()) {
  const timestamp = Date.parse(event.startsAt || "")
  if (!Number.isNaN(timestamp)) return timestamp >= now
  return Boolean(event.isUpcoming)
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

/** upcoming | done | pending | cancelled */
export function eventStatus(event, now = Date.now()) {
  const explicit = String(event.statusCode || "").toLowerCase()
  const status = String(event.status || "")
    .trim()
    .toLowerCase()
  if (
    cancelledStatuses.includes(explicit) ||
    cancelledStatuses.includes(status) ||
    status.startsWith("cancel")
  )
    return "cancelled"
  if (eventIsUpcoming(event, now)) return "upcoming"
  if (event.timePrecision === "unknown" || pendingStatuses.includes(status))
    return "pending"
  return "done"
}

export function eventIsManual(event) {
  return event.provenance === "manual" || Boolean(event.manualLocked)
}

export function eventTypeKey(event) {
  const type = String(event.eventType || "").toLowerCase()
  return ["stream", "member", "event", "release", "video"].includes(type)
    ? `event.type.${type}`
    : null
}

export function sortEvents(events) {
  return [...events].sort((a, b) =>
    String(a.startsAt || `${a.startsOn}T99`).localeCompare(
      String(b.startsAt || `${b.startsOn}T99`),
    ),
  )
}

// ---------------------------------------------------------------------------
// Videos

const videoKindKeys = {
  GAMEPLAY: "media.kind.gameplay",
  TALK: "media.kind.talk",
  "LIVE ARCHIVE": "media.kind.liveArchive",
  "UPCOMING LIVE": "media.kind.upcoming",
  MUSIC: "media.kind.music",
}

export function videoKindLabel(video, t) {
  const kind = String(video?.kind || "")
    .trim()
    .toUpperCase()
  // A reservation keeps its "UPCOMING LIVE" kind after it airs.
  if (kind === "UPCOMING LIVE" && !video.isUpcoming)
    return t("media.kind.liveArchive")
  if (videoKindKeys[kind]) return t(videoKindKeys[kind])
  if (video?.kind && !/[぀-ヿ㐀-鿿]/u.test(video.kind)) return video.kind
  return t(video?.isUpcoming ? "media.kind.upcoming" : "media.kind.video")
}

/** Strip the trailing 【鹿乃まほろ/ミリプロ】 credit that every title repeats. */
export function cleanVideoTitle(title) {
  return String(title || "")
    .replace(/\s*【\s*鹿乃まほろ\s*[/／]\s*ミリプロ\s*】\s*/gu, " ")
    .trim()
}

export function videoTime(video) {
  return video.scheduledAt || video.publishedAt || null
}
