import type { Dashboard } from "../types.ts"
import {
  eventIsUpcoming,
  eventStatus,
  sortEvents,
  scheduleSortTime,
} from "./format.ts"

export const LIVE_GRACE_MS = 3 * 3600 * 1000

export function eventIsLive(event, now: number) {
  if (
    !["stream", "member"].includes(event.eventType) &&
    event.source !== "youtube"
  )
    return false
  const start = Date.parse(event.startsAt || "")
  const end = Date.parse(event.endsAt || "")
  return (
    start <= now &&
    (Number.isFinite(end) ? end > now : start > now - LIVE_GRACE_MS)
  )
}

/** Recompute time-dependent state from the local snapshot, without network work. */
export function deriveDashboardAt(snapshot: Dashboard, now: number): Dashboard {
  const events = snapshot.events.map((event) => ({
    ...event,
    isUpcoming: eventIsUpcoming(event, now),
  }))
  const videos = snapshot.videos.map((video) => ({
    ...video,
    isUpcoming: video.scheduledAt
      ? Date.parse(video.scheduledAt) >= now
      : Boolean(video.isUpcoming),
  }))
  const eligible = events.filter(
    (event) =>
      !["cancelled", "cancellation_review"].includes(eventStatus(event, now)),
  )
  const upcomingEvents = eligible.filter((event) => event.isUpcoming)
  const focusEvents = sortEvents(
    eligible.filter((event) => event.isUpcoming || eventIsLive(event, now)),
  )
  const focusVideos = videos
    .filter(
      (video) =>
        !video.isCancelled &&
        (video.isUpcoming ||
          Date.parse(video.scheduledAt || "") > now - LIVE_GRACE_MS),
    )
    .sort(
      (a, b) =>
        scheduleSortTime(a.scheduledAt) - scheduleSortTime(b.scheduledAt) ||
        a.id.localeCompare(b.id),
    )
  const latestVideos = videos
    .filter((video) => !video.isUpcoming)
    .sort(
      (a, b) =>
        scheduleSortTime(b.scheduledAt || b.publishedAt) -
          scheduleSortTime(a.scheduledAt || a.publishedAt) ||
        a.id.localeCompare(b.id),
    )
  return {
    ...snapshot,
    events,
    videos,
    summary: {
      ...snapshot.summary,
      nextEvent: focusEvents[0] || null,
      nextStream: focusVideos[0] || null,
      latestVideo: latestVideos[0] || null,
      counts: snapshot.summary.counts
        ? { ...snapshot.summary.counts, upcomingEvents: upcomingEvents.length }
        : null,
    },
  }
}
