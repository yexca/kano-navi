import { cleanVideoTitle, scheduleSortTime } from "./format.ts"

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
      // A stream that started up to three hours ago is probably still live.
      return Number.isNaN(time) || time > now - 3 * 3600 * 1000
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
