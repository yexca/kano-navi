// Public feed addresses. The server renders them from the same snapshot as
// the board (backend-go/internal/feeds); calendar apps and feed readers poll
// this site, never X or YouTube.

import type { Locale } from "../types.ts"

export const CALENDAR_FEED_PATH = "/api/calendar.ics"
export const ATOM_FEED_PATH = "/api/feed.xml"

/**
 * A feed path in the reader's language. English is the server's base, so it
 * keeps the bare path that existing subscriptions use.
 */
export function feedPath(path: string, locale: Locale = "en"): string {
  return locale === "en" ? path : `${path}?lang=${encodeURIComponent(locale)}`
}

/**
 * The schedule subscription for an origin: the https address to paste into a
 * calendar, and the webcal form that opens a calendar app directly.
 */
export function calendarFeedUrls(
  origin: string,
  locale: Locale = "en",
): {
  https: string
  webcal: string
} {
  const url = new URL(feedPath(CALENDAR_FEED_PATH, locale), origin)
  const https = url.href
  return { https, webcal: `webcal://${url.host}${url.pathname}${url.search}` }
}

/** Points index.html's feed discovery links at the reader's language. */
export function syncFeedLinks(locale: Locale, root?: ParentNode) {
  root ??= typeof document === "undefined" ? undefined : document
  // Renderers without a full DOM (tests) have no links to update.
  if (typeof root?.querySelector !== "function") return
  for (const [type, path] of [
    ["application/atom+xml", ATOM_FEED_PATH],
    ["text/calendar", CALENDAR_FEED_PATH],
  ]) {
    root
      .querySelector(`link[rel="alternate"][type="${type}"]`)
      ?.setAttribute("href", feedPath(path, locale))
  }
}
