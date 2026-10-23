import { cleanVideoTitle, scheduleSortTime, eventIsUpcoming } from "./format.ts"
import { eventIsLive, videoIsInFocus } from "./derive-dashboard.ts"
import type { Dashboard, ScheduleEvent, Video } from "../types.ts"

type SpotlightSummary = Pick<
  Dashboard["summary"],
  "nextEvent" | "nextStream" | "latestVideo"
>

export type SpotlightLink = "watch" | "announcement" | "open"

interface SpotlightCandidate {
  mode: "next" | "latest"
  time: string | null
  dateOnly?: string | null
  title: string
  detail?: string | null
  url: string | null
  link: SpotlightLink | null
  thumbnailUrl?: string | null
  video?: Video
  event?: ScheduleEvent
}

export interface SpotlightItem extends SpotlightCandidate {
  layout: "full" | "compact"
}

const watchHosts = ["youtube.com", "youtu.be", "twitch.tv"]
const announcementHosts = ["x.com", "twitter.com"]

function hostMatches(host: string, domains: string[]) {
  return domains.some(
    (domain) => host === domain || host.endsWith(`.${domain}`),
  )
}

/** What following the ticket opens: the stream, its announcement, or a page. */
export function spotlightLink(
  url: string | null | undefined,
): SpotlightLink | null {
  if (!url) return null
  let host = ""
  try {
    host = new URL(url).hostname.toLowerCase()
  } catch {
    return "open"
  }
  if (hostMatches(host, watchHosts)) return "watch"
  if (hostMatches(host, announcementHosts)) return "announcement"
  return "open"
}

// Only artwork earns the full ticket; a schedule entry without it would leave
// an empty art band, so it gets the compact date ticket instead.
function withLayout(item: SpotlightCandidate): SpotlightItem {
  return { ...item, layout: item.thumbnailUrl ? "full" : "compact" }
}

export function pickSpotlight(
  summary: SpotlightSummary | null | undefined,
  now: number,
): SpotlightItem | null {
  const candidates: SpotlightCandidate[] = []
  if (summary?.nextStream?.scheduledAt) {
    const video = summary.nextStream
    candidates.push({
      mode: "next",
      time: video.scheduledAt,
      title: cleanVideoTitle(video.title),
      url: video.url,
      link: "watch",
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
        link: spotlightLink(event.url),
        event,
      })
    }
  }
  const upcoming = candidates
    .filter((candidate) => {
      if (candidate.event)
        return (
          eventIsUpcoming(candidate.event, now) ||
          eventIsLive(candidate.event, now)
        )
      return candidate.video ? videoIsInFocus(candidate.video, now) : false
    })
    .sort(
      (a, b) =>
        scheduleSortTime(a.time, a.dateOnly) -
        scheduleSortTime(b.time, b.dateOnly),
    )
  if (upcoming[0]) return withLayout(upcoming[0])

  const video = summary?.latestVideo
  if (video) {
    return {
      mode: "latest",
      layout: "full",
      time: video.publishedAt || video.scheduledAt,
      title: cleanVideoTitle(video.title),
      url: video.url,
      link: "watch",
      thumbnailUrl: video.thumbnailUrl,
      video,
    }
  }
  return null
}

/** The latest video shown under a compact ticket, so the stage keeps a picture. */
export function pickCompanion(
  summary: SpotlightSummary | null | undefined,
  item: SpotlightItem | null | undefined,
) {
  if (item?.layout !== "compact") return null
  const video = summary?.latestVideo
  if (!video || video.url === item.url) return null
  return {
    time: video.publishedAt || video.scheduledAt,
    title: cleanVideoTitle(video.title),
    url: video.url,
    thumbnailUrl: video.thumbnailUrl,
    video,
  }
}
