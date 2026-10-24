import { streamIdentity } from "../lib/stream-identity.ts"
import { cleanVideoTitle, scheduleSortTime, eventIsUpcoming } from "./format.ts"
import {
  eventIsLive,
  leadingCandidate,
  videoIsInFocus,
} from "./derive-dashboard.ts"

const watchHosts = ["youtube.com", "youtu.be", "twitch.tv"]
const announcementHosts = ["x.com", "twitter.com"]

function hostMatches(host, domains) {
  return domains.some(
    (domain) => host === domain || host.endsWith(`.${domain}`),
  )
}

/** What following the ticket opens: the stream, its announcement, or a page. */
export function spotlightLink(url) {
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
function withLayout(item) {
  return { ...item, layout: item.thumbnailUrl ? "full" : "compact" }
}

export function pickSpotlight(summary, now) {
  const candidates = []
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
      (candidate) =>
        event.url &&
        streamIdentity(candidate.url) === streamIdentity(event.url),
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
      return videoIsInFocus(candidate.video, now)
    })
    .sort(
      (a, b) =>
        scheduleSortTime(a.time, a.dateOnly) -
        scheduleSortTime(b.time, b.dateOnly),
    )
  const lead = leadingCandidate(
    upcoming,
    now,
    (candidate) => Date.parse(candidate.time || ""),
    (candidate) =>
      Date.parse((candidate.event || candidate.video).endsAt || ""),
  )
  if (lead) return withLayout(lead)

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
export function pickCompanion(summary, item) {
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
