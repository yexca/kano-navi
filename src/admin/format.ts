import type { Translator } from "@/types"

// All operator times are shown in Asia/Tokyo, whatever the browser zone.
const intlLocales = { "zh-CN": "zh-CN", ja: "ja-JP", en: "en-US" }
const formatters = new Map<string, Intl.DateTimeFormat>()
const relativeFormatters = new Map<string, Intl.RelativeTimeFormat>()

function formatter(locale: string, options: Intl.DateTimeFormatOptions) {
  const key = `${locale}:${JSON.stringify(options)}`
  if (!formatters.has(key)) {
    formatters.set(
      key,
      new Intl.DateTimeFormat(intlLocales[locale] || intlLocales.en, {
        timeZone: "Asia/Tokyo",
        ...options,
      }),
    )
  }
  return formatters.get(key)
}

function valid(value: string | null | undefined) {
  if (!value) return null
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

export function formatDateTime(
  value: string | null | undefined,
  locale: string,
) {
  const date = valid(value)
  return date
    ? formatter(locale, {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        hourCycle: "h23",
      }).format(date)
    : ""
}

export function formatTime(value: string | null | undefined, locale: string) {
  const date = valid(value)
  return date
    ? formatter(locale, {
        hour: "2-digit",
        minute: "2-digit",
        hourCycle: "h23",
      }).format(date)
    : ""
}

/** A stored Japan date (YYYY-MM-DD) shown with its weekday. */
export function formatDay(day: string | null | undefined, locale: string) {
  if (!day || !/^\d{4}-\d{2}-\d{2}$/u.test(day)) return day || ""
  return formatter(locale, {
    month: "short",
    day: "numeric",
    weekday: "short",
  }).format(new Date(`${day}T12:00:00+09:00`))
}

export function formatRelative(
  value: string | null | undefined,
  locale: string,
  now = Date.now(),
) {
  const date = valid(value)
  if (!date) return ""
  if (!relativeFormatters.has(locale)) {
    relativeFormatters.set(
      locale,
      new Intl.RelativeTimeFormat(intlLocales[locale] || intlLocales.en, {
        numeric: "auto",
        style: "short",
      }),
    )
  }
  const relative = relativeFormatters.get(locale)
  const seconds = Math.round((date.getTime() - now) / 1000)
  for (const [unit, size] of [
    ["day", 86_400],
    ["hour", 3_600],
    ["minute", 60],
  ] as [Intl.RelativeTimeFormatUnit, number][]) {
    if (Math.abs(seconds) >= size)
      return relative.format(Math.round(seconds / size), unit)
  }
  return relative.format(0, "minute")
}

export function formatDuration(
  startedAt: string | null | undefined,
  finishedAt: string | null | undefined,
) {
  const start = Date.parse(startedAt || "")
  const end = finishedAt ? Date.parse(finishedAt) : Date.now()
  if (Number.isNaN(start) || Number.isNaN(end)) return ""
  const seconds = Math.max(0, Math.round((end - start) / 1000))
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, "0")}s`
}

export function formatInterval(minutes: number, t: Translator) {
  if (minutes % 1440 === 0)
    return t("admin.interval.days", { count: minutes / 1440 })
  if (minutes % 60 === 0)
    return t("admin.interval.hours", { count: minutes / 60 })
  return t("admin.interval.minutes", { count: minutes })
}

/** Today's date in Asia/Tokyo as YYYY-MM-DD. */
export function japanToday(now = new Date()) {
  return japanDay(now.toISOString())
}

/** An ISO instant as its Japan date "YYYY-MM-DD". */
export function japanDay(value: string | null | undefined) {
  const date = valid(value)
  if (!date) return ""
  const parts = formatter("en", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(date)
  const get = (type: string) => parts.find((part) => part.type === type)?.value
  return `${get("year")}-${get("month")}-${get("day")}`
}

/** An ISO instant as the Japan wall-clock time "HH:mm". */
export function japanClock(value: string | null | undefined) {
  const date = valid(value)
  if (!date) return ""
  const parts = formatter("en", {
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).formatToParts(date)
  const get = (type: string) => parts.find((part) => part.type === type)?.value
  return `${get("hour")}:${get("minute")}`
}

/** Combine a Japan date and wall-clock time into an ISO instant. */
export function japanInstant(day: string, clock: string) {
  if (!day || !clock) return ""
  return `${day}T${clock}:00+09:00`
}

const tones: Record<string, string> = {
  running: "running",
  queued: "running",
  pending: "neutral",
  completed: "success",
  succeeded: "success",
  success: "success",
  processed: "success",
  schedule: "success",
  partial: "warning",
  skipped: "neutral",
  uncertain: "warning",
  never: "neutral",
  unreviewed: "neutral",
  not_schedule: "neutral",
  failed: "danger",
  blocked: "danger",
  cancelled: "danger",
}

/** Normalize run, step, model and review statuses to one tone vocabulary. */
export function statusTone(status: string | null | undefined): any {
  return tones[String(status || "").toLowerCase()] || "neutral"
}

export function statusLabel(status: string | null | undefined, t: Translator) {
  const value = String(status || "").toLowerCase()
  return value in tones ? t(`admin.status.${value}`) : t("admin.status.unknown")
}
