import { cleanVideoTitle, scheduleSortTime, eventIsUpcoming } from "./format.ts"
import { eventIsLive, LIVE_GRACE_MS } from "./derive-dashboard.ts"

export function pickSpotlight(summary, now) {
  const candidates = []
  if (summary?.nextStream?.scheduledAt) {
    const video = summary.nextStream
    candidates.push({
      mode: "next",
      time: video.scheduledAt,
      title: cleanVideoTitle(video.title),
      url: video.url,
      thumbnailUrl: video.thumbnailUrl,
      video,
    })
  }
  if (summary?.nextEvent) {
    const event = summary.nextEvent
    const duplicate = candidates.some(
      (candidate) => event.url && candidate.url === event.url,
    )
    if (!duplicate) {
      candidates.push({
        mode: "next",
        time: event.startsAt || null,
        dateOnly: event.startsAt ? null : event.startsOn,
        title: cleanVideoTitle(event.title),
        detail: event.detail,
        url: event.url,
        event,
      })
    }
  }
  const upcoming = candidates
    .filter((candidate) => {
      const time = Date.parse(candidate.time || "")
      if (candidate.event)
        return (
          eventIsUpcoming(candidate.event, now) ||
          eventIsLive(candidate.event, now)
        )
      return Number.isFinite(time) && time > now - LIVE_GRACE_MS
    })
    .sort(
      (a, b) =>
        scheduleSortTime(a.time, a.dateOnly) -
        scheduleSortTime(b.time, b.dateOnly),
    )
  if (upcoming[0]) return upcoming[0]

  const video = summary?.latestVideo
  if (video) {
    return {
      mode: "latest",
      time: video.publishedAt || video.scheduledAt,
      title: cleanVideoTitle(video.title),
      url: video.url,
      thumbnailUrl: video.thumbnailUrl,
      video,
    }
  }
  return null
}
