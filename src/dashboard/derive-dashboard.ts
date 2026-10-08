import type { Dashboard } from "../types.ts"
import {
  eventIsUpcoming,
  eventStatus,
  sortEvents,
  scheduleSortTime,
} from "./format.ts"

export const LIVE_GRACE_MS = 3 * 3600 * 1000

/** Match supported YouTube links using only their public video identity. */
function streamIdentity(value: string) {
  try {
    const url = new URL(value)
    if (["http:", "https:"].includes(url.protocol)) {
      const id =
        url.hostname === "youtu.be"
          ? url.pathname.match(/^\/([^/]+)\/?$/u)?.[1]
          : /(?:^|\.)youtube\.com$/u.test(url.hostname)
            ? url.searchParams.get("v") ||
              url.pathname.match(/^\/(?:live|shorts)\/([^/]+)\/?$/u)?.[1]
            : null
      if (id && /^[A-Za-z0-9_-]+$/u.test(id)) return `youtube:${id}`
    }
  } catch {
    // Non-YouTube or unparseable links keep exact URL matching.
  }
  return `url:${value}`
}

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

export function videoIsInFocus(video, now: number) {
  if (video.isCancelled) return false
  const start = Date.parse(video.scheduledAt || "")
  const end = Date.parse(video.endsAt || "")
  if (Number.isFinite(end)) return end > now && Number.isFinite(start)
  return Number.isFinite(start)
    ? start > now - LIVE_GRACE_MS
    : Boolean(video.isUpcoming)
}

/** Recompute time-dependent state from the local snapshot, without network work. */
export function deriveDashboardAt(snapshot: Dashboard, now: number): Dashboard {
  const events = snapshot.events.map((event) => ({
    ...event,
    isUpcoming: eventIsUpcoming(event, now),
  }))
  const streamEnds = new Map<string, string>()
  for (const event of events) {
    if (event.url && Number.isFinite(Date.parse(event.endsAt || "")))
      streamEnds.set(streamIdentity(event.url), event.endsAt)
  }
  const videos = snapshot.videos.map((video) => ({
    ...video,
    endsAt: streamEnds.get(streamIdentity(video.url)) || null,
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
    .filter((video) => videoIsInFocus(video, now))
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
