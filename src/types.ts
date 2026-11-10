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

/** Readers see a manual translation, then a platform's, then a model's. */
export type TranslationOrigin = "manual" | "platform" | "machine"

export interface Translation {
  text: string
  origin: TranslationOrigin
  /** The platform of a platform translation, such as youtube or bilibili. */
  platform?: string | null
}

/**
 * The target locales. Japanese readers see a Japanese translation only of
 * wording whose source language is not Japanese.
 */
export type TranslationLocale = "en" | "ja" | "zh-CN"

/** At most one resolved translation per target locale. */
export type Translations = Partial<Record<TranslationLocale, Translation>>

/**
 * An X post, or a Bilibili dynamic (`source` "bilibili"): its handle is the
 * account's numeric mid, it has no engagement counts or quote, and its text
 * may be empty.
 */
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
  /** Translations of the text and its detected language (ja, en, zh, ...). */
  translations?: Translations
  sourceLanguage?: string | null
  /** The status this post quotes, shown nested in the post. */
  quote?: PostQuote | null
  /** A related account's status promoted onto the board, not Kano's own. */
  promoted?: boolean
}

/**
 * A quoted X status, or the dynamic a Bilibili forward forwards (authorHandle
 * is then the author's mid). Its translations are those of the stored post
 * with the same id, otherwise those of the quoted status itself, and media
 * lists only ready local images. An unavailable quote (deleted or withheld)
 * has only its id and url; a forward of a dynamic whose id is unknown has a
 * null id and links to the forward itself.
 */
export interface PostQuote {
  id: string | null
  authorHandle: string | null
  authorName: string | null
  text: string | null
  publishedAt: string | null
  url: string
  unavailable: boolean
  media: MediaItem[]
  translations?: Translations
  sourceLanguage?: string | null
}

export type SaleMark = "opens" | "closes"

/** A sale period as stored: its opening and its close. */
export interface SaleWindow {
  startsOn: string
  startsAt: string | null
  endsOn: string | null
  endsAt: string | null
}

/** Where an entry's broadcast or work is: its platform, when known. */
export type EventPlatform = "youtube" | "bilibili" | "other" | "unknown"

/**
 * One platform an entry is shown on, the link to follow there (null when it
 * has none) and that platform's confirmed broadcast state.
 */
export interface EventLink {
  platform: EventPlatform
  url: string | null
  liveStatus?: LiveStatus | null
}

export type EventViewState =
  | "upcoming"
  | "live"
  | "open"
  | "done"
  | "pending"
  | "cancelled"
  | "cancellation_review"

export type EventViewTypeCode =
  "stream" | "member" | "event" | "release" | "video" | "sale"

export type EventViewActivityCode = "live" | "game" | "talk" | "song" | "video"

export interface EventView {
  state: EventViewState
  typeCode: EventViewTypeCode | null
  activityCode: EventViewActivityCode | null
  activityContext: "stream" | "work" | ""
  isWork: boolean
  calendarEntries: CalendarEntryView[]
}

export interface CalendarEntryView {
  saleMark: "opens" | "closes" | null
  startsOn: string
  startsAt: string | null
  timePrecision: "exact" | "approximate" | "unknown"
}

export interface ScheduleEvent {
  id: string
  source: string
  platform?: EventPlatform | null
  title: string
  detail: string | null
  startsOn: string
  startsAt: string | null
  endsAt: string | null
  /** The Japan close date of a sale period; absent for every other entry. */
  endsOn?: string | null
  timezone: string
  timePrecision: string
  status: string | null
  eventType: string
  /** Which end of a sale period this calendar entry shows. */
  saleMark?: SaleMark
  saleWindow?: SaleWindow
  /** The entry's activity label code (`live`, `game`, `talk`, `song`, `video`). */
  tag?: string | null
  url: string | null
  provenance: string
  manualLocked: boolean
  isUpcoming: boolean
  cancellationStatus: string
  cancellationReason?: string | null
  cancellationEvidence?: string | null
  /**
   * The linked video's or Bilibili live room's confirmed broadcast state;
   * Bilibili never reports upcoming.
   */
  liveStatus?: LiveStatus | null
  /**
   * The Bilibili live room of a stream or membership entry shown on
   * Bilibili, once known.
   */
  liveUrl?: string | null
  /**
   * Every platform the entry is shown on, its own first: announced, lent by
   * the entries merged into it, or a simulcast the live room observed.
   */
  platforms?: EventPlatform[]
  /** One link per shown platform, in `platforms` order. */
  links?: EventLink[]
  /** The linked video's title translations while the event shows its title. */
  translations?: Translations
  sourceLanguage?: string | null
  view?: EventView
}

export type VideoAvailability =
  "public" | "unlisted" | "members" | "private" | "removed"

export interface Video {
  id: string
  source: string
  title: string
  publishedAt: string | null
  scheduledAt: string | null
  /** Derived from the linked snapshot event for focus expiry. */
  endsAt?: string | null
  url: string
  thumbnailUrl: string | null
  thumbnailStatus: string
  /** The form: stream, premiere, upload, or unknown before a detail check. */
  kind: string
  /** What the video is about: game, talk or song; null for anything else. */
  tag?: string | null
  isUpcoming: boolean
  isCancelled?: boolean
  liveStatus?: LiveStatus | null
  /** The length a watch page reported; null while unknown or on air. */
  durationSeconds?: number | null
  /**
   * Whether YouTube's feed (or an operator) names this a Short; null while
   * unknown, which the board treats as an ordinary upload.
   */
  isShort?: boolean | null
  /**
   * Who can watch it: public, unlisted, members, private or removed; null
   * until a check could tell, which the board treats as watchable.
   */
  availability?: VideoAvailability | null
  /** Translations of the title and its detected language (ja, en, zh, ...). */
  translations?: Translations
  sourceLanguage?: string | null
  /** The same work on other platforms, such as a merged Bilibili upload. */
  alternates?: VideoAlternate[]
  view?: VideoView
}

export interface VideoView {
  phase: "upcoming" | "live" | "started" | "past" | "cancelled"
  formCode:
    "short" | "upcoming" | "stream" | "archive" | "premiere" | "video" | null
  shelfGroup: "reservation" | "work" | "archive" | "short" | ""
  latestEligible: boolean
  focusEligible: boolean
  tagContext: "stream" | "work"
}

export interface VideoAlternate {
  source: string
  url: string
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
 * One Japan month that holds posts or videos, as `YYYY-MM`, with its total and
 * its records per key: a lowercase handle for posts, a category for videos.
 */
export interface ArchiveMonth {
  month: string
  total: number
  counts: Record<string, number>
}

/**
 * One loaded prefix of an archive (a schedule or post month, or the video
 * timeline), newest page last. Schedule pages also carry dayCounts; post pages
 * carry the month's per-account counts, keyed by lowercase handle, and video
 * pages the per-category counts of everything they page through.
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
  categoryCounts?: Record<string, number>
}

/**
 * A live Bilibili broadcast that no schedule entry matched, present only
 * while the room is live.
 */
export interface LiveNow {
  platform: "bilibili"
  url: string
  title: string
  startedAt: string
}

export interface Dashboard {
  profile: Profile
  /** An unannounced broadcast on air now; absent in older snapshots. */
  liveNow?: LiveNow | null
  projection?: DashboardProjection
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
    /** Successful current-source reads; historical backfills do not refresh these. */
    sourceFetchedAt?: Partial<
      Record<"x" | "youtube" | "bilibili" | "shop", string | null>
    >
    /** The latest completed run, whatever its outcome. */
    lastSync: {
      status: string
      startedAt?: string
      finishedAt?: string | null
    } | null
    postWindowDays: number
    xAccounts: string[]
    /** The mids of stored Bilibili posts; the post filters offer each. */
    bilibiliAccounts?: string[]
    featuredVideoId: string | null
    eventWindow?: { from: string; to: string; paginated: boolean }
    revision?: number
  }
}

export interface DashboardProjection {
  version: number
  nextTransitionAt: string | null
  focus: { eventIds: string[]; videoIds: string[] }
  spotlight: {
    mode: "next" | "latest"
    eventId: string | null
    videoId: string | null
    liveNow: boolean
    isLive: boolean
    membersChip: boolean
    companionVideoId: string | null
  } | null
  sales: string[]
  shelf: {
    leadId: string | null
    leadIsFeatured: boolean
    reservations: string[]
    works: string[]
    archives: string[]
    shorts: string[]
  }
}
