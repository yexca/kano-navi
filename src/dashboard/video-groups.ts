import type { Video } from "../types.ts"
import {
  confirmedLiveStatus,
  LIVE_GRACE_MS,
  scheduleSortTime,
  videoTime,
} from "./format.ts"

/** The board's video filter. */
export type VideoFilter = "all" | "streams" | "works" | "shorts"

/**
 * Where a video stands now. `started` is a reservation past its start that no
 * watch page has confirmed yet, so it is neither on air nor an archive.
 */
export type VideoPhase = "upcoming" | "live" | "started" | "past" | "cancelled"

// Synchronization gives every broadcast one of the first two kinds; the
// others are what the title classifier calls a talk or game upload.
const streamKinds = new Set([
  "LIVE ARCHIVE",
  "UPCOMING LIVE",
  "GAMEPLAY",
  "TALK",
])

/** A broadcast or its archive, rather than a cover, original, MV or clip. */
export function videoIsStream(video: Video) {
  return streamKinds.has(
    String(video.kind || "")
      .trim()
      .toUpperCase(),
  )
}

/**
 * A YouTube Short, as the feed's link or an operator names it. An unknown
 * format, as for older uploads, is never guessed from the length.
 */
export function videoIsShort(video: Video) {
  return video.isShort === true
}

/**
 * The stage's rule for one video: a fresh confirmed state decides first;
 * without one a stream keeps the three-hour grace after its start and any
 * other video stays current only until an explicit end.
 */
export function videoPhase(video: Video, now: number): VideoPhase {
  // An operator-confirmed cancellation is neither upcoming, live nor archived.
  if (video.isCancelled) return "cancelled"
  const start = Date.parse(video.scheduledAt || "")
  const end = Date.parse(video.endsAt || "")
  const confirmed = confirmedLiveStatus(video)
  if (confirmed === "live") return end <= now ? "past" : "live"
  if (confirmed === "upcoming") return "upcoming"
  if (confirmed === "ended") return "past"
  if (!Number.isFinite(start)) return video.isUpcoming ? "upcoming" : "past"
  if (start > now) return "upcoming"
  if (Number.isFinite(end)) return end > now ? "started" : "past"
  return videoIsStream(video) && start > now - LIVE_GRACE_MS
    ? "started"
    : "past"
}

/** Whether a work leads the shelf: a cover or original, or a premiere. */
function isLeadWork(video: Video) {
  return (
    String(video.kind || "")
      .trim()
      .toUpperCase() === "MUSIC" || Boolean(video.scheduledAt)
  )
}

export interface VideoSplit {
  /** Reservations, broadcasts on air and cancelled reservations, soonest first. */
  reservations: Video[]
  /** Uploads other than broadcasts and Shorts, newest first. */
  works: Video[]
  /** Broadcast archives, newest first. */
  archives: Video[]
  /** Shorts, newest first. Not works, so one leads only when featured. */
  shorts: Video[]
}

export interface VideoShelfGroups extends VideoSplit {
  /** The operator's featured video, else the newest cover or premiere. */
  lead: Video | null
  leadIsFeatured: boolean
}

const newestFirst = (a: Video, b: Video) =>
  scheduleSortTime(videoTime(b)) - scheduleSortTime(videoTime(a)) ||
  a.id.localeCompare(b.id)

/** Sorts videos into what is coming, what was uploaded and what aired. */
export function splitVideos(videos: Video[], now: number): VideoSplit {
  const reservations: Video[] = []
  const works: Video[] = []
  const archives: Video[] = []
  const shorts: Video[] = []
  for (const video of videos) {
    const phase = videoPhase(video, now)
    // A cancelled reservation stays listed until its start, then drops out.
    if (phase === "cancelled") {
      if (Date.parse(video.scheduledAt || "") > now) reservations.push(video)
    } else if (phase !== "past") reservations.push(video)
    // A Short's title can read like a stream's, so its format decides first.
    else if (videoIsShort(video)) shorts.push(video)
    else (videoIsStream(video) ? archives : works).push(video)
  }
  reservations.sort(
    (a, b) =>
      scheduleSortTime(a.scheduledAt) - scheduleSortTime(b.scheduledAt) ||
      a.id.localeCompare(b.id),
  )
  works.sort(newestFirst)
  archives.sort(newestFirst)
  shorts.sort(newestFirst)
  return { reservations, works, archives, shorts }
}

/** The shelf's lead and the rest, each video listed once. */
export function groupVideos(
  videos: Video[],
  featuredVideoId: string | null,
  now: number,
): VideoShelfGroups {
  const featured = featuredVideoId
    ? videos.find((video) => video.id === featuredVideoId) || null
    : null
  const split = splitVideos(
    videos.filter((video) => video !== featured),
    now,
  )
  let lead = featured
  if (!lead) {
    lead = split.works.find(isLeadWork) || split.works[0] || null
    if (lead) split.works.splice(split.works.indexOf(lead), 1)
  }
  return { ...split, lead, leadIsFeatured: Boolean(featured) }
}

/**
 * Whether a video belongs to a filter; `all` keeps every video and a Short
 * belongs to `shorts` alone.
 */
export function videoMatchesFilter(video: Video, filter: VideoFilter) {
  if (filter === "all") return true
  if (videoIsShort(video)) return filter === "shorts"
  if (filter === "shorts") return false
  return videoIsStream(video) === (filter === "streams")
}
