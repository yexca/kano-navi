/** Sanitized snapshot fields shared by the public UI and its request hook. */
export type Locale = "en" | "ja" | "zh-CN"
export type Translator = (
  key: string,
  values?: Record<string, string | number>,
) => string

export interface Profile {
  displayName: string
  romanizedName: string
  bio: string
  avatarUrl: string | null
  bannerUrl: string | null
  xUrl: string
  youtubeUrl: string
}

export interface MediaItem {
  id: string | null
  url: string | null
  status: string
  sourceUrl: string | null
  position?: number
  width?: number | null
  height?: number | null
  alt?: string
}

/**
 * A broadcast state a watch page confirmed. The server sends live and upcoming
 * only while the check is fresh; null means only the start time is known.
 */
export type LiveStatus = "live" | "upcoming" | "ended"

export interface Post {
  id: string
  source: string
  accountHandle: string | null
  accountUrl: string | null
  publishedAt: string
  text: string
  url: string
  media: MediaItem[]
  mediaUrl: string | null
  mediaStatus: string
  likes?: number
  reposts?: number
  replies?: number
  type?: string
}

export interface ScheduleEvent {
  id: string
  source: string
  title: string
  detail: string | null
  startsOn: string
  startsAt: string | null
  endsAt: string | null
  timezone: string
  timePrecision: string
  status: string | null
  eventType: string
  url: string | null
  provenance: string
  manualLocked: boolean
  isUpcoming: boolean
  cancellationStatus: string
  cancellationReason?: string | null
  cancellationEvidence?: string | null
  /** The linked YouTube video's confirmed broadcast state. */
  liveStatus?: LiveStatus | null
}

export interface Video {
  id: string
  source: string
  title: string
  publishedAt: string | null
  scheduledAt: string | null
  /** Derived locally from the linked snapshot event for focus expiry. */
  endsAt?: string | null
  url: string
  thumbnailUrl: string | null
  thumbnailStatus: string
  kind: string
  isUpcoming: boolean
  isCancelled?: boolean
  liveStatus?: LiveStatus | null
}

export interface ScheduleAsset {
  id: string
  kind: string
  label: string | null
  alt?: string
  weekStart: string | null
  periodStart?: string | null
  periodEnd?: string | null
  periodBasis?: string
  updatedAt: string | null
  url: string | null
  sourceUrl: string | null
  mediaStatus: string
}

export interface CalendarSnapshot {
  events: ScheduleEvent[]
  scheduleImages: ScheduleAsset[]
  adjacent: { previous: string | null; next: string | null }
  dayCounts: Record<string, number>
  total: number
  page: number
  hasNext: boolean
  revision: number
}

/**
 * One loaded prefix of a month archive (schedule, posts or videos), newest
 * page last. Schedule pages also carry dayCounts; post pages carry the month's
 * per-account counts, keyed by lowercase handle.
 */
export interface ArchiveSnapshot<T> {
  items: T[]
  adjacent: { previous: string | null; next: string | null }
  total: number
  page: number
  hasNext: boolean
  revision: number
  dayCounts?: Record<string, number>
  accountCounts?: Record<string, number>
}

export interface Dashboard {
  profile: Profile
  summary: {
    nextEvent: ScheduleEvent | null
    nextStream: Video | null
    latestVideo: Video | null
    latestPost: Post | null
    counts: { posts: number; upcomingEvents: number; videos: number } | null
  }
  posts: Post[]
  events: ScheduleEvent[]
  videos: Video[]
  focus: {
    videoId: string | null
    title: string
    description: string | null
    updatedAt?: string | null
    dateLabel: string | null
    imageUrl: string | null
    url: string | null
    sourceUrl: string | null
  } | null
  timeline: { id: number; year: string; title: string; detail: string | null }[]
  resources: {
    id: number
    title: string
    detail: string | null
    url: string
    kind?: string
  }[]
  assets: ScheduleAsset[]
  scheduleImages: ScheduleAsset[]
  meta: {
    fetchedAt: string | null
    lastSync: { status: string; startedAt?: string; finishedAt?: string } | null
    postWindowDays: number
    xAccounts: string[]
    featuredVideoId: string | null
    eventWindow?: { from: string; to: string; paginated: boolean }
    revision?: number
  }
}
