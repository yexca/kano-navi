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

export interface FetchWindow {
  /** "count" reads each X account's or the channel's newest records. */
  mode: "recent" | "before" | "range" | "count"
  days?: number
  count?: number
  startDate?: string
  endDate?: string
}

/**
 * How a saved workflow reads a source: until a stored record, at most `max`,
 * or the newest `count` every run.
 */
export type SourceSync =
  { mode: "incremental"; max: number } | { mode: "count"; count: number }

/** A run's source window: an operator's, or a saved workflow's frozen sync. */
export interface RunWindow extends Omit<FetchWindow, "mode"> {
  mode: FetchWindow["mode"] | SourceSync["mode"]
  max?: number
}

export interface Run {
  id: string
  kind: "sync" | "scan" | "translate" | "workflow"
  status: "queued" | "running" | "completed" | "failed"
  createdAt: string
  startedAt: string | null
  finishedAt: string | null
  triggeredBy: string
  workflowId: string | null
  workflowName: string | null
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

export interface Overview {
  activeRun: Run | null
  recentRuns: Run[]
  scheduler: { enabled: boolean; running: boolean; tickSeconds: number | null }
  workflows: { total: number; scheduled: number; nextRunAt: string | null }
  sources: { x: SourceHealth; youtube: SourceHealth }
  review: {
    pendingImages: number
    suspectedCancellations: number
    failedPosts: number
    reprocessRequested: number
  }
  counts: {
    upcomingEvents: number
    events: number
    posts: number
    videos: number
  }
}

export interface Workflow {
  id: string
  name: string
  steps: string[]
  sourceSync: Record<"x" | "youtube", SourceSync>
  scheduleEnabled: boolean
  intervalMinutes: number
  lastRunAt: string | null
  lastJobId: string | null
  lastStatus: string | null
  nextRunAt: string | null
}

export interface WorkflowCatalog {
  items: Workflow[]
  steps: { id: string; group: "source" | "process" }[]
  limits: { minIntervalMinutes: number; maxIntervalMinutes: number }
  skippedSteps: string[]
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

export interface Sources {
  x: {
    skipped: boolean
    /** The official API credential is configured. */
    historyAvailable: boolean
    /** The window modes the server can run now. */
    windows: FetchWindow["mode"][]
    accounts: { account: string; oldest: string | null }[]
  }
  youtube: {
    skipped: boolean
    historyAvailable: boolean
    windows: FetchWindow["mode"][]
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
  /** Null for posts the translate step does not cover (non-X). */
  translation: AdminTranslation | null
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
  thumbnail?: VideoThumbnailPin | null
  isShort?: boolean | null
}

export interface AdminVideosPage extends Page<AdminVideo> {
  featuredVideoId: string | null
  /** The pinned video itself, whatever page or filter is listed. */
  featuredVideo: AdminVideo | null
}

export interface ProfileMedia {
  id: string
  slot: "avatar" | "banner"
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

export type QueueRoute = "schedule" | "translate"

export interface RouteTarget {
  providerId: string
  modelId: string
}

export interface LlmCatalog {
  providers: Provider[]
  models: Model[]
  /** The single model priority queue every extraction stage draws from. */
  queue: RouteTarget[]
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
