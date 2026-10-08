const japanDateTime = new Intl.DateTimeFormat("en-CA", {
  timeZone: "Asia/Tokyo",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
})

const intlLocales = { "zh-CN": "zh-CN", ja: "ja-JP", en: "en-US" }
const dateTimeFormatters = new Map()
const relativeFormatters = new Map()

export function japanParts(value) {
  if (!value) return null
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null
  return japanDateTime
    .formatToParts(date)
    .reduce<Record<string, string>>((result, part) => {
      result[part.type] = part.value
      return result
    }, {})
}

export function formatDateTime(value, locale) {
  if (!value) return ""
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ""
  if (!dateTimeFormatters.has(locale)) {
    dateTimeFormatters.set(
      locale,
      new Intl.DateTimeFormat(intlLocales[locale] || intlLocales.en, {
        timeZone: "Asia/Tokyo",
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        hourCycle: "h23",
      }),
    )
  }
  return dateTimeFormatters.get(locale).format(date)
}

/** Relative time such as "in 12 min" or "3 hours ago". */
export function formatRelative(value, locale, now = Date.now()) {
  if (!value) return ""
  const time = Date.parse(value)
  if (Number.isNaN(time)) return ""
  if (!relativeFormatters.has(locale)) {
    relativeFormatters.set(
      locale,
      new Intl.RelativeTimeFormat(intlLocales[locale] || intlLocales.en, {
        numeric: "auto",
        style: "short",
      }),
    )
  }
  const formatter = relativeFormatters.get(locale)
  const seconds = Math.round((time - now) / 1000)
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ["day", 86_400],
    ["hour", 3_600],
    ["minute", 60],
  ]
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size) {
      return formatter.format(Math.round(seconds / size), unit)
    }
  }
  return formatter.format(0, "minute")
}

export function formatDuration(startedAt, finishedAt) {
  const start = Date.parse(startedAt)
  const end = finishedAt ? Date.parse(finishedAt) : Date.now()
  if (Number.isNaN(start) || Number.isNaN(end)) return ""
  const seconds = Math.max(0, Math.round((end - start) / 1000))
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, "0")}s`
}

export function formatInterval(minutes, t) {
  if (minutes % 1440 === 0)
    return t("admin.workflow.interval.days", { count: minutes / 1440 })
  if (minutes % 60 === 0)
    return t("admin.workflow.interval.hours", { count: minutes / 60 })
  return t("admin.workflow.interval.minutes", { count: minutes })
}

/** Normalize sync/job statuses into one small vocabulary for the UI. */
export function statusTone(status) {
  const value = String(status || "").toLowerCase()
  if (["running", "queued"].includes(value)) return "running"
  if (["completed", "success"].includes(value)) return "success"
  if (["partial", "skipped"].includes(value)) return "warning"
  if (["failed", "failure", "cancelled"].includes(value)) return "danger"
  return "neutral"
}

export function statusLabel(status, t) {
  const value = String(status || "").toLowerCase()
  const keys = {
    running: "admin.status.running",
    queued: "admin.status.queued",
    completed: "admin.status.completed",
    success: "admin.status.completed",
    partial: "admin.status.partial",
    failed: "admin.status.failed",
    failure: "admin.status.failed",
    skipped: "admin.status.skipped",
    cancelled: "admin.status.cancelled",
    pending: "admin.status.pending",
  }
  return t(keys[value] || "admin.status.unknown")
}

/** Hash a string into one of the palette hues used for provider avatars. */
export function paletteIndex(value, size = 6) {
  let hash = 0
  for (const character of String(value || "")) {
    hash = (hash * 31 + character.codePointAt(0)) >>> 0
  }
  return hash % size
}
