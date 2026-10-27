// Public feed addresses. The server renders them from the same snapshot as
// the board (backend-go/internal/feeds); calendar apps and feed readers poll
// this site, never X or YouTube.

export const CALENDAR_FEED_PATH = "/api/calendar.ics"
export const ATOM_FEED_PATH = "/api/feed.xml"

/**
 * The schedule subscription for an origin: the https address to paste into a
 * calendar, and the webcal form that opens a calendar app directly.
 */
export function calendarFeedUrls(origin: string): {
  https: string
  webcal: string
} {
  const url = new URL(CALENDAR_FEED_PATH, origin)
  const https = url.href
  return { https, webcal: `webcal://${url.host}${url.pathname}` }
}
