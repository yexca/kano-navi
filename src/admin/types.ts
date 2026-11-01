// Wire shapes of /api/admin; see docs/architecture/admin-api.md.

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
export type FetchMethodId = "timeline" | "api" | "rss" | "ytdlp"

/**
 * What a method reads: the newest `count` per X account or for the channel,
 * the whole listing, or for the X API a Japan date range or a backfill of
 * `days` before the oldest stored post.
 */
export type FetchMode = "count" | "all" | "range" | "before"

/** The selection a manual fetch sends with its method. */
export interface FetchSelection {
  mode: FetchMode
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
export type ProxyArea = "x" | "youtube" | "bilibili" | "media" | "llm"

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
  group: "lineblog" | "history" | "media"
  originals: number
  ready: number
  unhashed: number
}

export interface VariantStatus {
  groups: VariantGroupStatus[]
  checkedAt: string
}

/**
 * One timer's next run. A due entry waits for the queue: a source timer
 * starts once the current run and the timers due before it have.
 */
export type QueueEntry = { nextRunAt: string; due: boolean } & (
  { kind: "source"; source: ScheduleSource } | { kind: "live-probe" }
)

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
  sources: { x: SourceHealth; youtube: SourceHealth; bilibili: SourceHealth }
  review: {
    pendingImages: number
    suspectedCancellations: number
    /** Posts with a failed extraction or translation. */
    failedPosts: number
    failedExtractions: number
    failedTranslations: number
    reprocessRequested: number
    /** Bilibili uploads the merge rule leaves to the operator. */
    videoMerges: number
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

export type ScheduleSource = "x" | "youtube" | "bilibili"

/** One listing method of a source's fallback chain (ADR-0004). */
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
  /** Bilibili's one method cannot be reordered. */
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

/** A source's own timer (ADR-0003). */
export interface SourceSchedule {
  source: ScheduleSource
  enabled: boolean
  intervalMinutes: number
  minIntervalMinutes: number
  maxIntervalMinutes: number
  /** X and YouTube only. */
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
    account?: string
    videos?: number
    /** A login cookie (BILIBILI_SESSDATA) is configured; it is never shown. */
    signedIn?: boolean
    minIntervalMinutes?: number
    lastAttemptAt?: string | null
    lastSuccessAt?: string | null
    /** Reads wait until then after Bilibili asked for a captcha. */
    blockedUntil?: string | null
    lastError?: string | null
  }
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
  timePrecision: "exact" | "approximate" | "unknown"
  status: string | null
  eventType: string | null
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
}

export interface EventInput {
  title: string
  detail?: string
  startsOn: string
  startsAt?: string
  endsAt?: string
  timePrecision?: string
  eventType?: string
  status?: string
  url?: string
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
  /** Null for posts the translate step does not cover (non-X). */
  translation: AdminTranslation | null
  /** The status the post quotes, read-only; null when it quotes none. */
  quote: AdminQuote | null
  /** When an operator hid the quote from the public pages. */
  quoteHiddenAt: string | null
  /** A refresh from the source waiting for the next X synchronization. */
  refreshRequestedAt: string | null
  /** When an operator hid the whole post from the public pages. */
  hiddenAt: string | null
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
 * A post's stored quote. Media lists only its ready cached images, apart
 * from the post's own; an unavailable quote has only its id and url.
 */
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

export type TranslationOwner = "post" | "video"
export type TranslationLocale = "en" | "zh-CN"

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

export interface TranslationCounts {
  total: number
  incomplete: number
  requested: number
}

export interface TranslationSummary {
  counts: Record<TranslationOwner, TranslationCounts>
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
  /** The App calendar label an operator pinned; null lets it read the title. */
  tag: string | null
  manualTag: string | null
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
  /** Length difference in seconds; null when a length is unknown. */
  durationDelta: number | null
  gapMinutes: number
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

export interface LlmCatalog {
  providers: Provider[]
  models: Model[]
  /** The post extraction queue both detection paths draw from. */
  queue: RouteTarget[]
  /** The separate schedule-image check queue; it never falls back. */
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
