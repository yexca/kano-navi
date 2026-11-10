// Wire shapes of /api/admin; see docs/architecture/admin-api.md.

import type { VideoAvailability } from "../types.ts"

export interface Page<T> {
  items: T[]
  page: number
  pageSize: number
  total: number
  totalPages: number
}

export interface Session {
  authenticated: boolean
  mode: "production" | "development"
  requiresPassword: boolean
}

export type StepProgress =
  | "pending"
  | "running"
  | "completed"
  | "partial"
  | "failed"
  | "skipped"
  | "cancelled"

/** A manual fetch's listing method; see the server's method table. */
export type FetchMethodId =
  "timeline" | "api" | "rss" | "ytdlp" | "catalog" | "space"

/** The sources a manual fetch form reads through a method of their own. */
export type FetchSource = "x" | "youtube" | "bilibili" | "bilibili_dynamics"

/**
 * What a method reads: the newest `count` per X account or for the channel,
 * the whole listing, or for the X API a Japan date range or a backfill of
 * `days` before the oldest stored post.
 */
export type FetchMode = "count" | "all" | "range" | "before"

/** The selection a manual fetch sends with its method. An X fetch may name
 * one of the own accounts; without one it reads them all. */
export interface FetchSelection {
  mode: FetchMode
  account?: string
  days?: number
  count?: number
  startDate?: string
  endDate?: string
}

export interface FetchOptions {
  /** Run schedule extraction after an X fetch. */
  extract?: boolean
  translate?: boolean
  /** Rewrite stored records the method lists again (YouTube only). */
  refresh?: boolean
}

/** One listing method of a source and whether the server can run it now. */
export interface FetchMethod {
  id: FetchMethodId
  modes: FetchMode[]
  maxCount: number
  pageSize: number
  /** The environment variable the method needs, never its value. */
  requires?: string
  refresh: boolean
  extract: boolean
  available: boolean
}

/** A record a fetch stored for the first time. */
export interface FetchItem {
  id: string
  title: string
  account?: string
}

/**
 * How a source timer reads X or YouTube: until a stored record, at most
 * `max`, or the newest `count` every run.
 */
export type SourceSync =
  { mode: "incremental"; max: number } | { mode: "count"; count: number }

/**
 * A run's source window: an operator's fetch, or a source timer's frozen
 * sync. Runs frozen before fetches named their method carry no `method` and
 * may use the retired `recent` mode.
 */
export interface RunWindow extends Omit<FetchSelection, "mode"> {
  method?: FetchMethodId
  mode: FetchMode | SourceSync["mode"] | "recent"
  refresh?: boolean
  max?: number
}

export interface Run {
  id: string
  kind: "sync" | "scan" | "translate" | "variants" | "workflow"
  status: "queued" | "running" | "completed" | "failed"
  createdAt: string
  startedAt: string | null
  finishedAt: string | null
  triggeredBy: string
  /** The saved workflow a stored run belongs to; new runs have none. */
  workflowId: string | null
  /** The source whose chain the run is: its timer (triggeredBy
   * "source-schedule") or the operator's Run now. */
  sourceSchedule: ScheduleSource | null
  steps: string[]
  progress: Record<string, StepProgress>
  fetchWindows: Record<string, RunWindow> | null
  result: { status: string; results: Record<string, any> } | null
  error: string | null
  run: {
    status: string
    message: string | null
    counts: Record<string, any>
  } | null
}

export interface SourceHealth {
  skipped: boolean
  lastSuccessAt: string | null
  lastStep: {
    runId: string
    status: StepProgress
    finishedAt: string | null
  } | null
  push?: { enabled: boolean; verified: boolean }
}

export interface VisitorRegionSettings {
  enabled: boolean
  providerName: string
  providerUrl: string
  mirrorUrl: string
}

/** A part of the server's outbound requests with its own proxy route. */
export type ProxyArea = "x" | "youtube" | "bilibili" | "shop" | "media" | "llm"

export type ProxyScheme = "http" | "https" | "socks5" | "socks5h"

/** This process's recent attempts through one proxy. */
export interface ProxyHealth {
  lastSuccessAt: string | null
  lastFailureAt: string | null
  coolingUntil: string | null
}

export interface OutboundProxy {
  id: string
  name: string
  scheme: ProxyScheme
  host: string
  port: number
  username: string
  /** The password is write-only; these report whether one is stored. */
  hasPassword: boolean
  passwordUnreadable: boolean
  enabled: boolean
  health: ProxyHealth
}

/** An area's route: off, or every proxy (empty proxyIds) or a selection,
 * tried in queue order. */
export interface ProxyRoute {
  enabled: boolean
  proxyIds: string[]
}

export interface ProxySettings {
  /** The queue, highest priority first. */
  proxies: OutboundProxy[]
  routes: Record<ProxyArea, ProxyRoute>
  directFallback: boolean
  areas: ProxyArea[]
  /** Areas with a fixed page a proxy test can reach. */
  testAreas: ProxyArea[]
}

/** One proxy in a save. Omitting password keeps the stored one; "" clears
 * it. */
export interface ProxyInput {
  id?: string
  name: string
  scheme: ProxyScheme
  host: string
  port: number
  username: string
  password?: string
  enabled: boolean
}

export interface ProxyTestResult {
  ok: boolean
  status?: number
  error?: "timeout" | "failed"
  latencyMs: number
  proxy: OutboundProxy
}

/**
 * How complete one group's smaller image copies are. Ready originals have
 * every width stored; unhashed ones were never read, so they count as
 * missing until a copies run reads them.
 */
export interface VariantGroupStatus {
  group: "lineblog" | "works" | "journey" | "media"
  originals: number
  ready: number
  unhashed: number
}

export interface VariantStatus {
  groups: VariantGroupStatus[]
  checkedAt: string
}

/**
 * How the Works page's upload covers stand. Due uploads are those the next
 * media step looks up or queues; waiting ones have an image or lookup on its
 * way or waiting to retry; uncovered ones have every video gone.
 */
export interface WorksCoverStatus {
  total: number
  covered: number
  due: number
  waiting: number
  uncovered: number
  /** Uploads without a ready cover, newest first. */
  uploads: WorksCoverUpload[]
  /** SKIP_MEDIA keeps the media step from running in this process. */
  skipped: boolean
  checkedAt: string
}

export interface WorksCoverUpload {
  id: string
  date: string
  state: "due" | "waiting" | "uncovered"
  videos: WorksCoverVideo[]
}

/** One video in cover order; code and retryAt come from its last failure. */
export interface WorksCoverVideo {
  platform: "youtube" | "niconico" | "bilibili"
  id: string
  members: boolean
  state: "due" | "waiting" | "gone"
  code: string | null
  retryAt: string | null
}

/**
 * One timer's next run. A due entry waits for the queue: a source timer
 * starts once the current run and the timers due before it have.
 */
export type QueueEntry = { nextRunAt: string; due: boolean } & (
  { kind: "source"; source: ScheduleSource } | { kind: "live-probe" }
)

/**
 * Operator requests waiting for the step that does the work: posts to
 * extract again, X posts and YouTube videos to read again from the source,
 * and machine translations.
 */
export interface Backlog {
  extraction: number
  postRefresh: number
  postTranslation: number
  videoRefresh: number
  videoTranslation: number
}

export interface Overview {
  /** Public mainland-China site suggestion; absent on older sidecars. */
  visitorRegionEnabled?: boolean
  visitorRegion?: VisitorRegionSettings
  activeRun: Run | null
  recentRuns: Run[]
  scheduler: { enabled: boolean; running: boolean; tickSeconds: number | null }
  /** The enabled source schedules this process runs. */
  timers: { scheduled: number; nextRunAt: string | null }
  /** Timed work in the order it reaches the single-flight queue. */
  queue: QueueEntry[]
  backlog: Backlog
  sources: Record<ScheduleSource, SourceHealth>
  review: {
    pendingImages: number
    suspectedCancellations: number
    uncertainEvents: number
    /** Posts with a failed extraction or translation. */
    failedPosts: number
    failedExtractions: number
    failedTranslations: number
    /** Bilibili uploads the merge rule leaves to the operator. */
    videoMerges: number
    /** Bilibili uploads waiting for the operator's form decision. */
    bilibiliForms: number
  }
  counts: {
    upcomingEvents: number
    events: number
    posts: number
    videos: number
  }
}

/** The steps a one-off step run can pick, in execution order. */
export interface StepCatalog {
  steps: { id: string; group: "source" | "process" }[]
  /** Steps disabled through SKIP_* in this process. */
  skippedSteps: string[]
}

/** How long finished runs stay in the history; 0 days keeps them. */
export interface RunRetention {
  succeededDays: number
  failedDays: number
  maxDays: number
  pruner: {
    /** False when this process does not clean the history. */
    available: boolean
    intervalHours?: number
    lastPrunedAt?: string | null
    lastDeleted?: number
    lastFailed?: boolean
  }
}

export interface PushStatus {
  available: boolean
  enabled: boolean
  callbackUrl: string | null
  callbackPath: string
  secretConfigured: boolean
  verified: boolean
  leaseExpiresAt: string | null
  nextRenewalAt: string | null
  pending: boolean
  lastNotificationAt: string | null
  lastTriggeredAt: string | null
  lastError: string | null
}

export interface LiveProbeStatus {
  enabled: boolean
  intervalMinutes: number
  minIntervalMinutes: number
  maxIntervalMinutes: number
  available: boolean
  lastRunAt: string | null
  nextRunAt: string | null
  activeVideos: number
  maxVideos: number
  videos: LiveProbeVideo[]
  stopped: LiveProbeStop[]
  /** The Bilibili live-room check around Bilibili stream events. */
  bilibili?: BilibiliLiveProbe
}

export interface BilibiliLiveProbe {
  enabled: boolean
  intervalMinutes: number
  minIntervalMinutes: number
  maxIntervalMinutes: number
  /** False when the process has no Bilibili reader. */
  available: boolean
  nextCheckAt: string | null
  targets: BilibiliLiveTarget[]
}

export interface BilibiliLiveTarget {
  eventId: string
  title: string
  startsAt: string
  phase: "prestart" | "starting" | "live" | "overdue" | "baseline"
  dueAt: string
}

export interface LiveProbeVideo {
  id: string
  title: string
  url: string
  scheduledAt: string
  phase: "prestart" | "starting" | "live" | "overdue"
  dueAt: string
}

export interface LiveProbeStop {
  id: string
  title: string
  url: string
  scheduledAt: string
  stoppedAt: string
  reason: "unavailable" | "operator"
}

export interface YTDLPStatus {
  available: boolean
  version?: string
}

/** The read bounds of source steps, manual fetches and translation. */
export type SyncSettingField =
  | "xBootstrapDays"
  | "xDiscoveryLimit"
  | "xMaxStatusRequests"
  | "xRefreshKnown"
  | "youtubeMaxDetailRequests"
  | "fetchMaxPages"
  | "fetchTimeoutMs"
  | "fetchRequestDelayMs"
  | "translationScanLimit"
  | "translationLookbackDays"
  | "llmModelAttempts"

export interface SyncSettings {
  settings: Record<SyncSettingField, number>
  fields: {
    field: SyncSettingField
    default: number
    min: number
    max: number
  }[]
}

export type ScheduleSource =
  "x" | "x_related" | "youtube" | "bilibili" | "bilibili_dynamics" | "shop"

/** One listing method of a source's fallback chain. */
export interface ChainMethod {
  id: string
  enabled: boolean
  /** Whether this process can run it; the variable it needs otherwise. */
  available: boolean
  requires?: string
}

/** The ordered fallback over listing methods that timed reads try. */
export interface SourceChain {
  methods: ChainMethod[]
  /** The one method of the related X accounts, Bilibili or the shop cannot
   * be reordered. */
  fixed: boolean
  /** X only: timeline failures before the API runs, and its daily cap. */
  apiGate?: number
  apiDailyCap?: number
  apiUsage?: { day: string; posts: number }
  timelineFailures?: Record<string, number>
}

/** What a schedule update may change in the chain. */
export interface SourceChainUpdate {
  methods?: { id: string; enabled: boolean }[]
  apiGate?: number
  apiDailyCap?: number
}

/** A source's own timer. */
export interface SourceSchedule {
  source: ScheduleSource
  enabled: boolean
  intervalMinutes: number
  minIntervalMinutes: number
  maxIntervalMinutes: number
  /** X, its related accounts (incremental only) and YouTube. */
  sync: SourceSync | null
  chain: SourceChain
  nextRunAt: string | null
  lastRun: { id: string; status: string; at: string } | null
  /** SKIP_* disables the source in this process. */
  skipped: boolean
  reason?: "environment"
}

export interface Sources {
  schedules: SourceSchedule[]
  x: {
    skipped: boolean
    /** The manual fetch methods, in the order the console offers them. */
    methods: FetchMethod[]
    accounts: { account: string; oldest: string | null }[]
  }
  youtube: {
    skipped: boolean
    methods: FetchMethod[]
    oldest: string | null
    push: PushStatus
    liveProbe: LiveProbeStatus
  }
  bilibili: {
    /** False when the process has no Bilibili reader. */
    available: boolean
    skipped: boolean
    /** The uploads' manual fetch methods. */
    methods: FetchMethod[]
    /** The oldest stored upload's publication time. */
    oldest?: string | null
    account?: string
    videos?: number
    /** A login cookie (BILIBILI_COOKIE or BILIBILI_SESSDATA) is configured; it is never shown. */
    signedIn?: boolean
    /** The cookie's state at the daily login check. */
    login?: BilibiliLogin
    loginCheckedAt?: string | null
    minIntervalMinutes?: number
    lastAttemptAt?: string | null
    lastSuccessAt?: string | null
    /** Reads wait until then after Bilibili asked for a captcha. */
    blockedUntil?: string | null
    lastError?: string | null
    dynamics?: BilibiliDynamicsStatus
    live?: BilibiliLiveRoom
  }
  shop: {
    skipped: boolean
    /** The read-all fetch. */
    methods: FetchMethod[]
    status: ShopStatus
  }
}

/** The official shop's reads and the sale entries they keep. */
export interface ShopStatus {
  sales: number
  lastAttemptAt: string | null
  lastSuccessAt: string | null
  /** No read requests the shop before then, after it answered 429. */
  blockedUntil: string | null
  lastError: string | null
}

export type BilibiliLogin =
  "signed_in" | "expired" | "unchecked" | "not_configured"

/** The account's dynamics listing, read with the upload list. */
export interface BilibiliDynamicsStatus {
  posts: number
  /** The dynamics' manual fetch methods; they need the login cookie. */
  methods: FetchMethod[]
  /** The oldest stored dynamic's publication time. */
  oldest: string | null
  lastAttemptAt: string | null
  lastSuccessAt: string | null
  lastError: string | null
  /** Why the last read was skipped, or null when it ran. */
  skipped: "login_required" | "login_expired" | "interval" | "blocked" | null
}

/**
 * The account's live room as last checked: 0 off, 1 live, 2 rotation
 * (replays, not live).
 */
export interface BilibiliLiveRoom {
  /** The room the live check found; null until the first check. */
  roomId: string | null
  liveStatus: 0 | 1 | 2 | null
  title: string | null
  liveTime: string | null
  checkedAt: string | null
  /** The last check that saw the room live: the last broadcast's approximate end. */
  lastLiveAt: string | null
  blockedUntil: string | null
  lastError: string | null
}

export type Cancellation = "none" | "llm_suspected" | "manual_confirmed"

export interface AdminEvent {
  id: string
  source: string
  title: string
  detail: string | null
  startsOn: string
  startsAt: string | null
  endsAt: string | null
  /** The Japan close date of a sale period, or null. */
  endsOn: string | null
  timePrecision: "exact" | "approximate" | "unknown"
  status: string | null
  eventType: string | null
  /**
   * Calendar label, and who decided it: "manual", "llm" (extraction prompt or
   * merged X notice) or "title" (title inference; also set with a null tag
   * when the title gave no label).
   */
  tag: string | null
  tagSource: string | null
  url: string | null
  provenance: "automatic" | "manual"
  manualLocked: boolean
  confidence: number | null
  cancellationStatus: Cancellation
  cancellationSource: string | null
  cancellationReason: string | null
  cancellationEvidence: string | null
  cancellationConfidence: number | null
  cancellationAt: string | null
  deletedAt: string | null
  createdAt: string
  updatedAt: string
  /** Set while the entry is hidden as a duplicate of that event. */
  mergedInto: string | null
  mergedIntoTitle: string | null
  /** "rule" or "manual"; manual without a target keeps the entry separate. */
  mergeSource: string | null
  mergeReason: string | null
  mergedAt: string | null
  /** An automatic entry whose every source post is hidden: the public pages
   * leave it out until a source post is shown or the entry is confirmed. */
  hiddenByPosts: boolean
  /** The announced platforms, inferred when a source default gave them. */
  platforms?: EventPlatform[]
  platformsInferred?: boolean
  /** What the public pages show: announced, merged entries' and observed. */
  shownPlatforms?: EventPlatform[]
  /** Why the entry needs a look, or null; the server's review rule. */
  review?: EventReview | null
}

/**
 * Why an entry needs a look: the model suspects its cancellation, or it is an
 * uncertain addition (an unconfirmed automatic entry from today on with low
 * model confidence or a tentative status), with the reasons.
 */
export interface EventReview {
  kind: "suspected" | "uncertain"
  reasons: ("lowConfidence" | "tentative")[]
}

/** One week of the Review queue's schedule work. */
export interface AttentionWeek {
  /** The Monday the week starts on. */
  weekStart: string
  /** Pending schedule images whose dates start in the week. */
  images: ScheduleImage[]
  /** Entries in the week that need a look. */
  events: AdminEvent[]
}

/** The Review queue's schedule work by week, oldest first. */
export interface ScheduleAttention {
  today: string
  weeks: AttentionWeek[]
  /** Pending schedule images without dates. */
  undated: ScheduleImage[]
  /** A kind had more items than one read lists. */
  truncated: boolean
}

/** Where an entry happens; none means not known. */
export type EventPlatform = "youtube" | "bilibili" | "other"

export const eventPlatforms: EventPlatform[] = ["youtube", "bilibili", "other"]

/** A bilibili live-room link, which is never an entry's own link. */
export function isLiveRoomLink(value: string) {
  try {
    const host = new URL(value.trim()).hostname.toLowerCase()
    return host === "live.bilibili.com" || host.endsWith(".live.bilibili.com")
  } catch {
    return false
  }
}

export interface EventInput {
  title: string
  detail?: string
  startsOn: string
  startsAt?: string
  endsAt?: string
  /** A sale's close date; the API rejects it for any other type. */
  endsOn?: string
  timePrecision?: string
  eventType?: string
  /** Empty string clears the label. */
  tag?: string
  status?: string
  url?: string
  /** The operator's platforms; absent keeps the stored ones. */
  platforms?: EventPlatform[]
  cancellationStatus?: Cancellation
  cancellationReason?: string
}

export interface ScheduleImage {
  id: string
  url: string
  sourceUrl: string | null
  alt: string | null
  weekStart: string | null
  periodStart: string | null
  periodEnd: string | null
  periodBasis: string | null
  inferredPeriodStart: string | null
  inferredPeriodEnd: string | null
  periodReason: string | null
  sourceAccount: string | null
  llmStatus: string
  llmConfidence: number | null
  llmText: string | null
  llmReason: string | null
  llmModel: string | null
  llmCheckedAt: string | null
  manualStatus: "unreviewed" | "schedule" | "not_schedule"
  manualReason: string | null
  effectiveStatus: string
  sourceMatchesBoard: boolean
  approved: boolean
  previewUrl: string | null
  mediaStatus: string
  /** The stored post that published the image; null when it is not stored. */
  sourcePost: { text: string; publishedAt: string } | null
}

export interface AdminPostImage {
  /** Local /media URL of the cached file; null until it is ready. */
  url: string | null
  /** ready, pending, failed or missing. */
  status: string
  alt: string | null
}

export interface AdminPost {
  id: string
  source: string
  accountHandle: string | null
  text: string
  publishedAt: string
  url: string
  mediaUrl: string | null
  mediaAlt: string | null
  media: AdminPostImage[]
  /** "none" for a text-only post, else the first image's status. */
  mediaStatus: string
  llmStatus: string
  llmRoute: string | null
  llmLastAttemptAt: string | null
  llmLastError: string | null
  llmReprocessRequested: boolean
  /** Earliest automatic retry of a post whose extraction keeps failing. */
  llmRetryAt: string | null
  /** The automatic scan no longer retries this post's current input. */
  llmRetryStopped: boolean
  /** Null for posts the translate step does not cover. */
  translation: AdminTranslation | null
  /** The status the post quotes, read-only; null when it quotes none. */
  quote: AdminQuote | null
  /** When an operator hid the quote from the public pages. */
  quoteHiddenAt: string | null
  /** A refresh from the source waiting for the next X synchronization. */
  refreshRequestedAt: string | null
  /** When an operator hid the whole post from the public pages. */
  hiddenAt: string | null
  /** When a related account's status was promoted onto the board; null for
   * an own post. External posts holds its promote and demote decisions. */
  promotedAt: string | null
  /** The post's schedule image record; null when it has none. */
  scheduleImage: AdminPostSchedule | null
}

/**
 * Which of a post's own images its schedule image shows (null for none of
 * them), its labels, and whether an operator picked the image.
 */
export interface AdminPostSchedule {
  id: string
  index: number | null
  llmStatus: string
  manualStatus: "unreviewed" | "schedule" | "not_schedule"
  picked: boolean
}

/**
 * A post's stored quote: an X status, or the dynamic a Bilibili forward
 * forwards (authorHandle is then its author's mid). Media lists only its
 * ready cached images, apart from the post's own; an unavailable quote has
 * only its id and url, and the id is empty for a forwarded dynamic the
 * listing did not name.
 */
/** What an entry was read from: its stored posts with their roles, links of
 * evidence that is not a stored post, and the same accounts' posts around
 * them, each oldest first. */
export interface EventEvidence {
  event: AdminEvent
  posts: (AdminPost & { roles: EvidenceRole[] })[]
  links: { source: string; url: string; role: EvidenceRole }[]
  nearby: AdminPost[]
}

/** A post the entry was extracted from, one of an entry merged into it, or
 * the post that suggested its cancellation. */
export type EvidenceRole = "source" | "merged" | "cancellation"

export interface AdminQuote {
  id: string
  authorHandle: string | null
  authorName: string | null
  text: string | null
  publishedAt: string | null
  url: string
  unavailable: boolean
  media: AdminPostImage[]
}

export type TranslationOwner = "post" | "video" | "x_status" | "bilibili_status"
export type TranslationLocale = "en" | "ja" | "zh-CN"

/** One target locale of a post or video in the console. */
export interface AdminTranslationLocale {
  locale: TranslationLocale
  /**
   * translated, missing, stale (only an outdated machine row), source (the
   * wording is already in this language) or not_needed (nothing to translate).
   */
  status: "translated" | "missing" | "stale" | "source" | "not_needed"
  origin: "manual" | "platform" | "machine" | null
  platform: string | null
  /** The translation readers see. */
  text: string | null
  /** The operator's translation, which may predate the current wording. */
  manual: string | null
  manualStale: boolean
  machine: string | null
  machineAt: string | null
  model: string | null
}

/** A related X account and the keyword rules that promote its statuses. */
export interface XRelatedAccount {
  handle: string
  keywords: string[]
}

/** A related account's last timeline read and its pool. */
export interface XRelatedStatus {
  handle: string
  lastSuccessAt: string | null
  lastError: string | null
  statuses: number
  promoted: number
}

export interface XRelatedAccounts {
  accounts: XRelatedAccount[]
  defaultKeywords: string[]
  status: XRelatedStatus[]
}

export type XStatusReason =
  "keyword" | "manual" | "quoted" | "reposted" | "none"
export type XStatusState = "promoted" | "demoted" | "stored"
export type XStatusDecision = "promote" | "demote" | "automatic"

/**
 * A status of the X status pool: a related account's status, or one an own
 * account quoted or reposted. Only a promoted one is a post; the rest never
 * reach the board except as a quote's wording.
 */
export interface AdminXStatus {
  id: string
  authorHandle: string
  authorName: string | null
  text: string
  publishedAt: string | null
  url: string
  images: number
  reasons: XStatusReason[]
  state: XStatusState
  /** Its author is a related account, so it can be promoted by hand. */
  related: boolean
  decision: "promote" | "demote" | null
  decisionReason: string | null
  decidedAt: string | null
  quotedAt: string | null
  repostedAt: string | null
  listedAt: string | null
  firstSeenAt: string
  /** Own posts quoting it, newest first, and own accounts that reposted it. */
  quotedBy: string[]
  repostedBy: string[]
  /** Its own translation; null when its post owns the translations. */
  translation: AdminTranslation | null
}

/** An own post that forwards a pooled Bilibili dynamic. */
export interface BilibiliForward {
  postId: string
  hidden: boolean
  quoteHidden: boolean
  quoteAvailable: boolean
}

/**
 * A pooled Bilibili dynamic that an own post forwards. It is never promoted:
 * the forward's quote card shows it, so only its translations are edited.
 */
export interface AdminBilibiliStatus {
  id: string
  authorMid: string
  authorName: string | null
  text: string
  publishedAt: string | null
  url: string
  kind: "dynamic" | "video"
  images: number
  reasons: "forwarded"[]
  forwardedAt: string | null
  firstSeenAt: string
  /** Up to five forwarding posts, newest first. */
  forwardedBy: BilibiliForward[]
  /** Whether a forward shows it on the board, or none shows it available. */
  shown: "public" | "hidden" | "gone"
  translation: AdminTranslation | null
}

export interface AdminTranslation {
  status: "complete" | "incomplete"
  sourceLanguage: string | null
  /** never, success, failed, skipped, or outdated after the wording changed. */
  machineStatus: string
  lastAttemptAt: string | null
  lastError: string | null
  /** An operator asked for a machine pass the translate step has not run. */
  requested: boolean
  locales: AdminTranslationLocale[]
}

export interface TranslationTotals {
  total: number
  incomplete: number
  requested: number
}

export interface TranslationCounts extends TranslationTotals {
  /** The same totals per source: x or bilibili posts, youtube or bilibili videos. */
  sources?: Record<string, TranslationTotals>
}

export interface TranslationSummary {
  /** Posts and videos only. */
  counts: Partial<Record<TranslationOwner, TranslationCounts>>
  locales: TranslationLocale[]
}

export interface TranslationRequestResult {
  queued: number
  run: Run | null
  /** The queue was busy; the requests wait for the next translate step. */
  busy: boolean
}

export interface AdminVideo {
  id: string
  source: string
  title: string
  publishedAt: string | null
  scheduledAt: string | null
  url: string
  /** Local /media URL of the cached file; null until it is ready. */
  thumbnailUrl: string | null
  thumbnailStatus: string
  kind: string | null
  isUpcoming: boolean
  /** title and kind show an operator correction; these keep the platform's. */
  sourceTitle: string
  sourceKind: string | null
  manualTitle: string | null
  manualKind: string | null
  manualThumbnail: VideoThumbnailPin | null
  durationSeconds: number | null
  /**
   * isShort shows an operator's format correction; sourceIsShort keeps what
   * the YouTube feed linked. Null is unknown.
   */
  isShort: boolean | null
  sourceIsShort: boolean | null
  manualIsShort: boolean | null
  /**
   * availability shows an operator's correction; sourceAvailability keeps
   * what the checks recorded. Null is unknown.
   */
  availability: VideoAvailability | null
  sourceAvailability: VideoAvailability | null
  manualAvailability: VideoAvailability | null
  /**
   * The Works decision: true lists the video among the Works page's uploads
   * after the collection date, false keeps it out, null leaves it to its
   * tag, form and format.
   */
  manualWork: boolean | null
  /**
   * The shown content tag (game, talk or song): manualTag, else the label of
   * the video's schedule entry, else the title inference's. inferredTagSource
   * is set once the inference judged the video, with or without a tag.
   */
  tag: string | null
  manualTag: string | null
  scheduleTag: string | null
  inferredTag: string | null
  inferredTagSource: string | null
  /** The last successful watch-page read. */
  checkedAt: string | null
  /** A refresh request the next YouTube step has not answered yet. */
  refreshRequestedAt: string | null
  revisions: number
  /** Null for videos the translate step does not cover (non-YouTube). */
  translation: AdminTranslation | null
  /** A merged Bilibili upload names its YouTube video; mergeSource manual
   * without a target is an undone merge the rules never redo. */
  mergedInto: string | null
  mergedIntoTitle: string | null
  mergeSource: "rule" | "manual" | null
  mergeReason: string | null
  mergedAt: string | null
  /** The uploads merged into this video. */
  alternates: AdminVideoAlternate[]
  /**
   * Who settled the form: manual (manualKind), or rule or llm for a Bilibili
   * upload; null for an unsettled upload or a YouTube video's own facts.
   */
  formSource: "manual" | "rule" | "llm" | null
  /** Why a Bilibili upload waits for the operator's form decision. */
  formReview: "uncertain" | "failed" | null
  /** Public readers list the video. */
  public: boolean
}

/** A Bilibili upload waiting for the operator's form decision. */
export interface BilibiliFormReview {
  video: AdminVideo
  review: "uncertain" | "failed"
  /** The broadcast the replay rule read, if any. */
  broadcast: {
    source: "entry" | "room"
    eventId: string | null
    title: string | null
    startsAt: string
    endsAt: string
  } | null
}

export interface AdminVideoAlternate {
  id: string
  source: string
  title: string
  url: string
  mergeSource: "rule" | "manual" | null
  mergeReason: string | null
  mergedAt: string | null
}

/** A YouTube video published within six hours of a Bilibili upload. */
export interface VideoMergeCandidate {
  youtube: AdminVideo
  /** Every rule holds: titles, lengths within three seconds and timing. */
  complete: boolean
  titleMatch: boolean
  /** The stream archive of the broadcast a live replay replays. */
  broadcast: boolean
  /** Length difference in seconds; null when a length is unknown. */
  durationDelta: number | null
  gapMinutes: number
}

/** A stored Bilibili upload whose merge the Works list pairs differently. */
export interface WorksMergeConflict {
  upload: string
  bilibili: string
  mergedInto: string | null
  worksYoutube: string[]
}

export interface VideoMergeSuggestion {
  bilibili: AdminVideo
  status: "rule" | "ambiguous" | "unverified" | "merged" | "separate" | "none"
  candidates: VideoMergeCandidate[]
}

export interface VideoThumbnailPin {
  mediaId: string
  sha256: string
}

/** A title or thumbnail the platform replaced, newest first. */
export interface VideoRevision {
  id: number
  origin: "sync" | "media"
  title: string
  kind: string | null
  thumbnailUrl: string | null
  thumbnailStatus: string
  replacedAt: string
}

/** A cached thumbnail file an operator may pin. */
export interface VideoThumbnail extends VideoThumbnailPin {
  url: string
  fetchedAt: string | null
  current: boolean
  selected: boolean
}

export interface VideoHistory {
  video: AdminVideo
  revisions: VideoRevision[]
  thumbnails: VideoThumbnail[]
}

/**
 * A string, file or boolean sets a correction, null clears it, omitted
 * keeps it.
 */
export interface VideoCorrection {
  title?: string | null
  kind?: string | null
  tag?: string | null
  thumbnail?: VideoThumbnailPin | null
  isShort?: boolean | null
  availability?: VideoAvailability | null
  work?: boolean | null
}

/**
 * A video after the Works collection date: one the Works page lists (origin
 * auto or operator) or one the operator excluded (origin null).
 */
export interface WorksRecentItem {
  id: string
  source: string
  title: string
  url: string
  publishedAt: string | null
  scheduledAt: string | null
  kind: string | null
  tag: string | null
  isShort: boolean | null
  availability: VideoAvailability | null
  work: boolean | null
  origin: "auto" | "operator" | null
  thumbnailUrl: string | null
}

export interface WorksRecentReport {
  /** The Japan date the packaged collection was checked through. */
  updatedAt: string
  /** The newest of them, at most 200. */
  items: WorksRecentItem[]
  /** How many are listed or excluded in all. */
  total: number
}

export interface AdminVideosPage extends Page<AdminVideo> {
  featuredVideoId: string | null
  /** The pinned video itself, whatever page or filter is listed. */
  featuredVideo: AdminVideo | null
}

export type ProfileSlot = "avatar" | "banner"

export interface ProfileMedia {
  id: string
  /** The slot the candidate is active for, or the one that found it. */
  slot: ProfileSlot
  source: "x" | "youtube" | "upload"
  sourceUrl: string | null
  downloadStatus: string | null
  status: string
  isActive: boolean
  width: number | null
  height: number | null
  byteSize: number | null
  lastError: string | null
  previewUrl: string | null
  updatedAt: string
}

export interface ProfileMediaState {
  items: ProfileMedia[]
  active: { avatar: ProfileMedia | null; banner: ProfileMedia | null }
}

export interface Extraction {
  enabled: boolean
  keywordEnabled: boolean
  visionEnabled: boolean
  messageEnabled: boolean
  messageAllPosts: boolean
  keywords: string[]
}

export interface Provider {
  id: string
  name: string
  protocol: string
  baseUrl: string
  enabled: boolean
  timeoutMs: number
  maxRetries: number
  apiKeyConfigured: boolean
  lastStatus: string | null
  lastError: string | null
  lastCheckedAt: string | null
}

export interface Model {
  providerId: string
  id: string
  name: string | null
  tags: string[]
  capabilities: string[]
  enabled: boolean
  origin: string
  ownedBy: string | null
}

export type QueueRoute = "schedule" | "verify" | "translate"

export interface RouteTarget {
  providerId: string
  modelId: string
}

/**
 * One stored failed model result. `message` is one of the fixed English
 * messages the schedule and translate steps store.
 */
export interface LlmFailure {
  route: QueueRoute
  subject: "post" | "image" | "video"
  subjectId: string
  /** The post or video address; an image carries its post's. */
  url: string | null
  providerId: string | null
  model: string | null
  message: string
  at: string
}

export interface LlmCatalog {
  providers: Provider[]
  models: Model[]
  /** The text schedule extraction queue. */
  queue: RouteTarget[]
  /** The image schedule extraction queue; it never falls back. */
  verificationQueue?: RouteTarget[]
  /** The separate queue of the translate step; it never falls back. */
  translationQueue?: RouteTarget[]
  modelTags: string[]
}

export interface DiscoveredModel {
  id: string
  name: string | null
  ownedBy: string | null
  suggestedTags: string[]
  added: boolean
  tags: string[] | null
}

// --- queue preview -----------------------------------------------------------
// What a run started now would work through, per step. Each is computed when
// the page asks for it; nothing is stored.

export interface ScheduleQueueEntry {
  postId: string
  source: "x" | "bilibili"
  route: "board" | "message"
  /** call: a model call; skip: recorded as skipped, using a place. */
  outcome: "call" | "skip"
  /** requested, new, changed or backoffDue for a call; a skip's code. */
  reason: string
  inputMode?: string
  publishedAt?: string
  lastAttemptAt?: string
  url?: string
  excerpt: string
}

export interface QueueHold {
  count: number
  nextAt?: string
}

export interface ScheduleQueue {
  generatedAt: string
  disabled: boolean
  boardEnabled: boolean
  messageEnabled: boolean
  allPosts: boolean
  limit: number
  postsRead: number
  candidates: number
  requested: number
  candidateMs: number
  elapsedMs: number
  run: ScheduleQueueEntry[]
  later: number
  cached: number
  backoff: QueueHold
  retryStopped: QueueHold
}

export interface YoutubeQueueEntry {
  id: string
  title: string
  url: string
  group: "refresh" | "queue" | "active" | "followUp"
  publishedAt?: string
  scheduledAt?: string
  upcoming: boolean
}

export interface YoutubeQueue {
  generatedAt: string
  skipped: boolean
  api: boolean
  apiVideos: number
  watchPages: number
  limit: number
  counts: Record<YoutubeQueueEntry["group"], number>
  entries: YoutubeQueueEntry[]
}

export interface TranslationQueueEntry {
  ownerType: string
  ownerId: string
  source: string
  requested: boolean
  excerpt: string
}

export interface TranslationQueue {
  generatedAt: string
  disabled: boolean
  reason?: string
  limit: number
  lookbackDays: number
  candidates: TranslationQueueEntry[]
}

export interface RefreshQueueEntry {
  postId: string
  source: string
  accountHandle: string
  text: string
  url: string
  publishedAt: string
  requestedAt: string
  /** next: this run; later: beyond the account's share; notRead: an account the X step does not read. */
  status: "next" | "later" | "notRead"
}

export interface RefreshQueue {
  generatedAt: string
  skipped: boolean
  handles: string[]
  perAccount: number
  detailBudget: number
  total: number
  limit: number
  entries: RefreshQueueEntry[]
}
