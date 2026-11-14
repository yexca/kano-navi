import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react"

import { createRequestScope, startPolling } from "../lib/polling"

import {
  ApiError,
  errorMessage,
  message,
  request,
  type Message,
  type RequestOptions,
} from "@/admin/api"
import type {
  AdminEvent,
  AdminPost,
  AdminVideo,
  AdminVideosPage,
  DiscoveredModel,
  EventEvidence,
  EventInput,
  Extraction,
  FetchMethodId,
  FetchOptions,
  FetchSelection,
  FetchSource,
  LlmCatalog,
  LlmFailure,
  Model,
  Overview,
  Page,
  ProfileMedia,
  ProfileMediaState,
  ProfileSlot,
  Provider,
  ProxyArea,
  ProxyInput,
  ProxyRoute,
  ProxySettings,
  ProxyTestResult,
  QueueRoute,
  RefreshQueue,
  RouteTarget,
  Run,
  ScheduleAttention,
  ScheduleImage,
  ScheduleQueue,
  ScheduleSource,
  Session,
  SourceChainUpdate,
  SourceSchedule,
  Sources,
  RunRetention,
  StepCatalog,
  TranslationLocale,
  TranslationOwner,
  TranslationQueue,
  TranslationRequestResult,
  TranslationSummary,
  AdminTranslation,
  VariantStatus,
  VisitorRegionSettings,
  AdminBilibiliStatus,
  AdminXStatus,
  YoutubeQueue,
  XRelatedAccount,
  XRelatedAccounts,
  XStatusDecision,
  VideoCorrection,
  VideoHistory,
  VideoMergeSuggestion,
  BilibiliFormReview,
  WorksCoverStatus,
  WorksMergeConflict,
  WorksRecentReport,
  SyncSettingField,
  SyncSettings,
  WebBotAuthStatus,
  YTDLPStatus,
} from "@/admin/types"

type Runner = <T>(
  key: string,
  path: string,
  options?: RequestOptions,
) => Promise<T>

const enc = encodeURIComponent

function query(values: Record<string, any>) {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(values)) {
    if (
      value === undefined ||
      value === null ||
      value === "" ||
      value === false
    )
      continue
    params.set(key, value === true ? "1" : String(value))
  }
  const text = params.toString()
  return text ? `?${text}` : ""
}

export type StartRun =
  | { kind: "sync" | "scan" }
  /** The missing smaller image copies, within one step's budget. */
  | { kind: "variants" }
  /** A one-off step run: the picked steps once, nothing saved. */
  | { kind: "workflow"; steps: string[] }
  /** A manual fetch through one method; it never falls back to another. */
  | {
      kind: "fetch"
      source: FetchSource | "shop"
      method: FetchMethodId
      window: FetchSelection
      options?: FetchOptions
    }
  /** One related X account's timeline, read as its timer reads it. */
  | {
      kind: "fetch"
      source: "x_related"
      method: "timeline"
      window: { mode: "incremental"; account: string }
      options?: FetchOptions
    }

/** A run start, or a source's Run now: its fixed chain once. */
export type StartWork = StartRun | { kind: "source"; source: ScheduleSource }

/**
 * The useResource hook whose read is being started. Its requests are keyed to
 * that hook, so one view's read never cancels another view's read of the same
 * path.
 */
let currentReader: string | null = null
let readers = 0

/**
 * Every /api/admin endpoint the console calls. Reads are keyed by path, or by
 * the useResource hook that starts them, so a newer read supersedes an older
 * one of the same owner; writes never cancel each other.
 */
function createClient(run: Runner, slow: Runner) {
  let writes = 0
  const get = <T>(path: string) =>
    run<T>(currentReader ?? path.split("?")[0], path)
  const send = <T>(method: string, path: string, body?: unknown) =>
    run<T>(`write:${++writes}`, path, { method, body })
  const sendSlow = <T>(method: string, path: string, body?: unknown) =>
    slow<T>(`slow:${++writes}`, path, { method, body })
  return {
    session: () => get<Session>("/session"),
    login: (password: string, code?: string) =>
      send<Session>(
        "POST",
        "/session",
        code === undefined ? { password } : { password, code },
      ),
    logout: () => send<null>("DELETE", "/session"),

    overview: () => get<Overview>("/overview"),
    visitorRegion: () => get<VisitorRegionSettings>("/settings/visitor-region"),
    variantStatus: (refresh = false) =>
      get<VariantStatus>(`/media/variants${refresh ? "?refresh=1" : ""}`),
    worksCovers: () => get<WorksCoverStatus>("/media/works-covers"),
    updateVisitorRegion: (settings: VisitorRegionSettings) =>
      send<VisitorRegionSettings>("PUT", "/settings/visitor-region", settings),
    runs: (page: number) =>
      get<Page<Run>>(`/runs${query({ page, pageSize: 15 })}`),
    run: (id: string) => get<{ run: Run }>(`/runs/${enc(id)}`),
    startRun: (body: StartRun) => send<{ run: Run }>("POST", "/runs", body),
    cancelRun: (id: string) =>
      send<{ run: Run }>("POST", `/runs/${enc(id)}/cancel`),
    retryRun: (id: string) =>
      send<{ run: Run }>("POST", `/runs/${enc(id)}/retry`),
    stepCatalog: () => get<StepCatalog>("/runs/steps"),
    /** What each step would work through if a run started now. */
    scheduleQueue: () =>
      get<{ queue: ScheduleQueue; skipped: boolean }>("/queue/schedule"),
    youtubeQueue: () => get<{ queue: YoutubeQueue }>("/queue/youtube"),
    translationQueue: () =>
      get<{ queue: TranslationQueue; skipped: boolean }>("/queue/translate"),
    refreshQueue: () => get<{ queue: RefreshQueue }>("/queue/x-refresh"),
    runRetention: () => get<RunRetention>("/runs/retention"),
    updateRunRetention: (body: {
      succeededDays?: number
      failedDays?: number
    }) => send<RunRetention>("PUT", "/runs/retention", body),

    sources: () => get<Sources>("/sources"),
    updateSourceSchedule: (
      source: ScheduleSource,
      body: Partial<
        Pick<SourceSchedule, "enabled" | "intervalMinutes" | "sync">
      > & { chain?: SourceChainUpdate },
    ) => send<SourceSchedule>("PUT", `/sources/${enc(source)}/schedule`, body),
    runSource: (source: ScheduleSource) =>
      send<{ run: Run }>("POST", `/sources/${enc(source)}/run`),
    /** The related X accounts, their keyword rules and their last reads. */
    relatedAccounts: () => get<XRelatedAccounts>("/sources/x/related"),
    updateRelatedAccounts: (accounts: XRelatedAccount[]) =>
      send<XRelatedAccounts>("PUT", "/sources/x/related", { accounts }),
    /** The X status pool behind External posts. */
    xStatuses: (filters: Record<string, any>) =>
      get<Page<AdminXStatus>>(`/x-statuses${query(filters)}`),
    /** The Bilibili status pool: the dynamics own posts forward. */
    bilibiliStatuses: (filters: Record<string, any>) =>
      get<Page<AdminBilibiliStatus>>(`/bilibili-statuses${query(filters)}`),
    /** A promote or demote lock with its reason, or back to the rules. */
    decideXStatus: (id: string, decision: XStatusDecision, reason = "") =>
      send<{ status: AdminXStatus }>("PUT", `/x-statuses/${enc(id)}/decision`, {
        decision,
        reason,
      }),
    startSourceSchedule: (source: ScheduleSource) =>
      send<SourceSchedule>("POST", `/sources/${enc(source)}/schedule/start`),
    updatePush: (body: { enabled?: boolean; callbackUrl?: string | null }) =>
      send<Sources["youtube"]["push"]>("PUT", "/sources/youtube/push", body),
    updateLiveProbe: (body: {
      enabled?: boolean
      intervalMinutes?: number
      bilibiliEnabled?: boolean
      bilibiliIntervalMinutes?: number
    }) =>
      send<Sources["youtube"]["liveProbe"]>(
        "PUT",
        "/sources/youtube/live-probe",
        body,
      ),
    stopLiveProbeVideo: (id: string, stopped: boolean) =>
      send<Sources["youtube"]["liveProbe"]>(
        "PUT",
        `/sources/youtube/live-probe/videos/${enc(id)}`,
        { stopped },
      ),
    updateWebBotAuth: (body: { origin: string | null }) =>
      send<WebBotAuthStatus>("PUT", "/sources/shop/web-bot-auth", body),
    generateWebBotAuthKey: () =>
      send<WebBotAuthStatus>("POST", "/sources/shop/web-bot-auth/key"),
    removeWebBotAuthKey: () =>
      send<WebBotAuthStatus>("DELETE", "/sources/shop/web-bot-auth/key"),
    rotatePushSecret: () =>
      send<Sources["youtube"]["push"]>(
        "POST",
        "/sources/youtube/push/rotate-secret",
      ),
    ytdlp: () => get<YTDLPStatus>("/sources/youtube/ytdlp"),
    updateYTDLP: () =>
      send<YTDLPStatus>("POST", "/sources/youtube/ytdlp/update"),
    proxySettings: () => get<ProxySettings>("/proxy"),
    updateProxySettings: (body: {
      proxies?: ProxyInput[]
      routes?: Partial<Record<ProxyArea, ProxyRoute>>
      directFallback?: boolean
    }) => send<ProxySettings>("PUT", "/proxy", body),
    testProxy: (id: string, area: ProxyArea) =>
      send<ProxyTestResult>("POST", `/proxy/${enc(id)}/test`, { area }),
    syncSettings: () => get<SyncSettings>("/sync-settings"),
    updateSyncSettings: (body: Partial<Record<SyncSettingField, number>>) =>
      send<SyncSettings>("PUT", "/sync-settings", body),

    events: (filters: Record<string, any>) =>
      get<Page<AdminEvent>>(`/events${query(filters)}`),
    eventEvidence: (id: string) =>
      get<EventEvidence>(`/events/${enc(id)}/evidence`),
    createEvent: (body: EventInput) =>
      send<{ event: AdminEvent }>("POST", "/events", body),
    updateEvent: (id: string, body: EventInput) =>
      send<{ event: AdminEvent }>("PUT", `/events/${enc(id)}`, body),
    confirmEvent: (id: string, body?: Partial<EventInput>) =>
      send<{ event: AdminEvent }>("POST", `/events/${enc(id)}/confirm`, body),
    deleteEvent: (id: string) => send<null>("DELETE", `/events/${enc(id)}`),
    mergeEvent: (id: string, into: string) =>
      send<{ event: AdminEvent }>("POST", `/events/${enc(id)}/merge`, {
        into,
      }),
    unmergeEvent: (id: string) =>
      send<{ event: AdminEvent }>("POST", `/events/${enc(id)}/unmerge`),

    /** The newest schedule images; a range keeps those whose period
     * overlaps it. */
    scheduleImages: (range?: { from: string; to: string }) =>
      get<{ items: ScheduleImage[] }>(
        `/schedule-images${query({ limit: 100, ...range })}`,
      ),
    /** What needs a look on the schedule, grouped by week. */
    scheduleAttention: () => get<ScheduleAttention>("/schedule-attention"),
    reviewImage: (id: string, status: string, reason: string) =>
      send<{ image: ScheduleImage }>(
        "POST",
        `/schedule-images/${enc(id)}/review`,
        { status, reason },
      ),
    setImagePeriod: (id: string, start: string, end: string, reason: string) =>
      send<{ image: ScheduleImage }>(
        "PUT",
        `/schedule-images/${enc(id)}/period`,
        { start, end, reason },
      ),

    posts: (filters: Record<string, any>) =>
      get<Page<AdminPost>>(`/posts${query(filters)}`),
    reprocessPost: (id: string) =>
      send<{ state: unknown }>("POST", `/posts/${enc(id)}/reprocess`, {}),
    dismissPost: (id: string) =>
      send<{ state: unknown }>("POST", `/posts/${enc(id)}/dismiss`, {}),
    /** Queue up to 200 posts for the next schedule scan at once. */
    reprocessPosts: (ids: string[]) =>
      send<{ queued: number }>("POST", "/posts/reprocess", { ids }),
    /** Dismiss the failed extractions among up to 200 posts. */
    dismissPosts: (ids: string[]) =>
      send<{ dismissed: number }>("POST", "/posts/dismiss", { ids }),
    /** Queue up to 200 own X posts for a refresh from the source at once. */
    refreshPosts: (ids: string[]) =>
      send<{ queued: number }>("POST", "/posts/refresh", { ids }),
    /** Make one of the post's own images its schedule image. */
    pickScheduleImage: (
      id: string,
      body: { index: number; reason: string; start?: string; end?: string },
    ) =>
      send<{ image: ScheduleImage }>(
        "PUT",
        `/posts/${enc(id)}/schedule-image`,
        body,
      ),
    setPostHidden: (id: string, hidden: boolean) =>
      send<{ state: unknown }>("PUT", `/posts/${enc(id)}/hidden`, { hidden }),
    setQuoteHidden: (id: string, hidden: boolean) =>
      send<{ state: unknown }>("PUT", `/posts/${enc(id)}/quote/hidden`, {
        hidden,
      }),
    refreshPost: (id: string) =>
      send<{ state: unknown }>("POST", `/posts/${enc(id)}/refresh`),

    translations: () => get<TranslationSummary>("/translations"),
    setTranslation: (
      owner: TranslationOwner,
      id: string,
      locale: TranslationLocale,
      text: string,
    ) =>
      send<{ translation: AdminTranslation }>(
        "PUT",
        `/translations/${owner}/${enc(id)}/${enc(locale)}`,
        { text },
      ),
    clearTranslation: (
      owner: TranslationOwner,
      id: string,
      locale: TranslationLocale,
    ) =>
      send<{ translation: AdminTranslation }>(
        "DELETE",
        `/translations/${owner}/${enc(id)}/${enc(locale)}`,
      ),
    /** Queue machine passes for owners, or for everything missing one. */
    requestTranslations: (
      body:
        | { missing: true; ownerType?: TranslationOwner; source?: string }
        | { ownerType: TranslationOwner; ids: string[] },
    ) => send<TranslationRequestResult>("POST", "/translations/request", body),
    videos: (filters: Record<string, any>) =>
      get<AdminVideosPage>(`/videos${query(filters)}`),
    setFeatured: (videoId: string | null) =>
      send<{ featuredVideoId: string | null }>("PUT", "/videos/featured", {
        videoId,
      }),
    videoHistory: (id: string) =>
      get<VideoHistory>(`/videos/${enc(id)}/history`),
    correctVideo: (id: string, correction: VideoCorrection) =>
      send<{ video: AdminVideo }>("PUT", `/videos/${enc(id)}`, correction),
    /**
     * `force` sends a Bilibili upload's read once past the captcha backoff,
     * after the server offered it (`bilibili_blocked` with `force`).
     */
    refreshVideo: (id: string, force = false) =>
      send<{ video: AdminVideo; started: boolean; run: Run | null }>(
        "POST",
        `/videos/${enc(id)}/refresh`,
        force ? { force: true } : {},
      ),
    addBilibiliVideo: (input: string, force = false) =>
      send<{ video: AdminVideo; started: boolean; run: Run | null }>(
        "POST",
        "/videos/bilibili",
        force ? { input, force: true } : { input },
      ),
    /** `input` is a YouTube video ID or link of the configured channel. */
    addYoutubeVideo: (input: string) =>
      send<{ video: AdminVideo; started: boolean; run: Run | null }>(
        "POST",
        "/videos/youtube",
        { input },
      ),
    /** `input` is a bilibili.com/opus or t.bilibili.com dynamic link. */
    addBilibiliPost: (input: string, force = false) =>
      send<{ post: AdminPost; started: boolean; run: Run | null }>(
        "POST",
        "/posts/bilibili",
        force ? { input, force: true } : { input },
      ),
    /** Uploads the merge rule would attach, or leaves to the operator. */
    videoMerges: () => get<{ items: VideoMergeSuggestion[] }>("/videos/merges"),
    /** Bilibili uploads waiting for the operator's form decision. */
    videoForms: () => get<{ items: BilibiliFormReview[] }>("/videos/forms"),
    worksConflicts: () =>
      get<{ items: WorksMergeConflict[] }>("/videos/works-conflicts"),
    worksRecent: () => get<WorksRecentReport>("/videos/works-recent"),
    /** One upload's merge state and candidates, whatever its state. */
    uploadMerges: (id: string) =>
      get<{ items: VideoMergeSuggestion[] }>(`/videos/${enc(id)}/merges`),
    /** `into` is a YouTube video ID or watch link. */
    mergeVideo: (id: string, into: string) =>
      send<{ video: AdminVideo }>("POST", `/videos/${enc(id)}/merge`, {
        into,
      }),
    unmergeVideo: (id: string) =>
      send<{ video: AdminVideo }>("POST", `/videos/${enc(id)}/unmerge`, {}),
    /** Records that an unmerged upload has no YouTube copy. */
    keepVideoSeparate: (id: string) =>
      send<{ video: AdminVideo }>("POST", `/videos/${enc(id)}/separate`, {}),

    profileMedia: () => get<ProfileMediaState>("/profile-media"),
    addProfileMedia: (source: string, sourceUrl: string) =>
      send<ProfileMediaState>("POST", "/profile-media", { source, sourceUrl }),
    /** Without a slot, discovery reads both the avatar and the banner. */
    discoverProfileMedia: (source: string) =>
      sendSlow<ProfileMediaState & { candidates: ProfileMedia[] }>(
        "POST",
        "/profile-media/discover",
        { source },
      ),
    uploadProfileMedia: (file: Blob) =>
      sendSlow<ProfileMediaState>("POST", "/profile-media/upload", file),
    downloadProfileMedia: (id: string) =>
      sendSlow<ProfileMediaState>("POST", `/profile-media/${enc(id)}/download`),
    selectProfileMedia: (id: string, slot: ProfileSlot) =>
      send<ProfileMediaState>("POST", `/profile-media/${enc(id)}/select`, {
        slot,
      }),
    deleteProfileMedia: (id: string) =>
      send<ProfileMediaState>("DELETE", `/profile-media/${enc(id)}`),

    extraction: () => get<Extraction>("/extraction"),
    updateExtraction: (body: Partial<Extraction>) =>
      send<Extraction>("PUT", "/extraction", body),
    llm: () => get<LlmCatalog>("/llm"),
    llmFailures: () => get<{ items: LlmFailure[] }>("/llm/failures"),
    createProvider: (body: Record<string, any>) =>
      send<{ provider: Provider }>("POST", "/llm/providers", body),
    updateProvider: (id: string, body: Record<string, any>) =>
      send<{ provider: Provider }>("PUT", `/llm/providers/${enc(id)}`, body),
    deleteProvider: (id: string) =>
      send<null>("DELETE", `/llm/providers/${enc(id)}`),
    testProvider: (id: string, model?: string) =>
      sendSlow<{ ok: boolean; method: string; model?: string }>(
        "POST",
        `/llm/providers/${enc(id)}/test`,
        model ? { model } : {},
      ),
    discoverModels: (id: string) =>
      sendSlow<{ provider: Provider; models: DiscoveredModel[] }>(
        "POST",
        `/llm/providers/${enc(id)}/discover`,
      ),
    addModels: (id: string, models: Partial<Model>[], origin = "manual") =>
      send<{ provider: Provider; models: Model[] }>(
        "POST",
        `/llm/providers/${enc(id)}/models`,
        { models, origin },
      ),
    updateModel: (id: string, model: Partial<Model>) =>
      send<{ provider: Provider; models: Model[] }>(
        "PUT",
        `/llm/providers/${enc(id)}/models`,
        model,
      ),
    removeModels: (id: string, ids: string[]) =>
      send<{ provider: Provider; models: Model[] }>(
        "POST",
        `/llm/providers/${enc(id)}/models/remove`,
        { ids },
      ),
    updateQueue: (targets: RouteTarget[], route: QueueRoute = "schedule") =>
      send<{ route: QueueRoute; queue: RouteTarget[] }>("PUT", "/llm/queue", {
        route,
        targets,
      }),
  }
}

export type AdminClient = ReturnType<typeof createClient>

export interface Toast {
  id: number
  message: Message
  tone: "success" | "danger"
}

/**
 * Owns every admin request: the session, the overview poll that also tracks
 * the active run, and the endpoint client the views use. When a run finishes
 * `revision` advances so views reload what the run may have changed.
 */
export function useAdminData() {
  const [session, setSession] = useState<Session | null>(null)
  // Why the session could not be read. The login requirements are unknown
  // until a read succeeds, so the console offers a retry instead of guessing.
  const [sessionError, setSessionError] = useState<Message | null>(null)
  const [discovery, setDiscovery] = useState(0)
  const [overview, setOverview] = useState<Overview | null>(null)
  const [revision, setRevision] = useState(0)
  const [toasts, setToasts] = useState<Toast[]>([])
  const toastId = useRef(0)
  const activeRef = useRef<string | null>(null)
  const scope = useMemo(() => createRequestScope(), [])
  const slowScope = useMemo(() => createRequestScope(130_000), [])

  useEffect(() => {
    scope.activate()
    slowScope.activate()
    return () => {
      scope.dispose()
      slowScope.dispose()
    }
  }, [scope, slowScope])

  const signedOut = useCallback(() => {
    scope.cancel()
    slowScope.cancel()
    setSession((current) =>
      current ? { ...current, authenticated: false } : current,
    )
  }, [scope, slowScope])

  const api = useMemo(() => {
    const runner =
      (target: typeof scope): Runner =>
      async (key, path, options = {}) => {
        try {
          return await target.run(key, (signal) =>
            request(path, { ...options, signal }),
          )
        } catch (error) {
          if (
            error instanceof ApiError &&
            error.status === 401 &&
            !path.startsWith("/session")
          )
            signedOut()
          throw error
        }
      }
    return createClient(runner(scope), runner(slowScope))
  }, [scope, slowScope, signedOut])

  const dismissToast = useCallback((id: number) => {
    setToasts((current) => current.filter((toast) => toast.id !== id))
  }, [])

  const pushToast = useCallback(
    (value: Message, tone: Toast["tone"]) => {
      if (!scope.active) return
      const id = ++toastId.current
      setToasts((current) => [
        ...current.slice(-3),
        { id, message: value, tone },
      ])
      if (tone === "success") window.setTimeout(() => dismissToast(id), 4200)
    },
    [dismissToast, scope],
  )

  const notify = useCallback(
    (key: string, values?: Record<string, any>) =>
      pushToast(message(key, values), "success"),
    [pushToast],
  )
  const fail = useCallback(
    (error: any) => {
      if (error?.name === "AbortError" || error?.status === 401) return
      pushToast(errorMessage(error), "danger")
    },
    [pushToast],
  )

  const refreshOverview = useCallback(async () => {
    const next = await api.overview()
    setOverview(next)
    const active = next.activeRun?.id || null
    if (activeRef.current && activeRef.current !== active) {
      setRevision((value) => value + 1)
    }
    activeRef.current = active
    return next
  }, [api])

  // Session first; the overview poll starts once authenticated. A failed
  // read leaves the session unknown, never password-only, until a retry.
  useEffect(() => {
    let cancelled = false
    api
      .session()
      .then((next) => {
        if (cancelled) return
        setSessionError(null)
        setSession(next)
      })
      .catch((error) => {
        if (cancelled || error?.name === "AbortError") return
        setSessionError(errorMessage(error))
      })
    return () => {
      cancelled = true
    }
  }, [api, discovery])

  const retrySession = useCallback(() => {
    setSessionError(null)
    setDiscovery((count) => count + 1)
  }, [])

  const authenticated = Boolean(session?.authenticated)
  // The overview poll runs while the console is visible; a hidden page stops
  // it, and becoming visible reads at once and polls again.
  useEffect(() => {
    if (!authenticated) return undefined
    let stop: (() => void) | undefined
    const follow = () => {
      if (document.visibilityState === "hidden") {
        stop?.()
        stop = undefined
        return
      }
      if (stop) return
      void refreshOverview().catch(fail)
      stop = startPolling(refreshOverview, {
        delay: () => (activeRef.current ? 1500 : 8000),
      })
    }
    follow()
    document.addEventListener("visibilitychange", follow)
    return () => {
      document.removeEventListener("visibilitychange", follow)
      stop?.()
    }
  }, [authenticated, refreshOverview, fail])

  const login = useCallback(
    async (password: string, code?: string) => {
      try {
        setSession(await api.login(password, code))
      } catch (error) {
        // A refused login reads the requirements again, so a deployment that
        // turned the one-time code on or off since the page loaded shows the
        // right form for the next attempt.
        if (error instanceof ApiError && error.status === 401)
          await api
            .session()
            .then(setSession)
            .catch(() => {})
        throw error
      }
    },
    [api],
  )

  const logout = useCallback(async () => {
    try {
      await api.logout()
      signedOut()
      setOverview(null)
    } catch (error) {
      fail(error)
    }
  }, [api, fail, signedOut])

  /**
   * Run an operator action, report its outcome and return its result, or
   * undefined when it failed.
   */
  const act = useCallback(
    async <T>(
      work: () => Promise<T>,
      success?: string,
      values?: Record<string, any>,
    ): Promise<T | undefined> => {
      try {
        const result = await work()
        if (success) notify(success, values)
        // Review counts and the queue badge follow the change at once.
        void refreshOverview().catch(() => {})
        return result
      } catch (error) {
        fail(error)
        return undefined
      }
    },
    [fail, notify, refreshOverview],
  )

  /** Start a run; a busy queue reports the run that holds it. */
  const start = useCallback(
    async (body: StartWork) => {
      try {
        const { run } = await (body.kind === "source"
          ? api.runSource(body.source)
          : api.startRun(body))
        notify("admin.notice.runQueued")
        activeRef.current = run.id
        void refreshOverview().catch(() => {})
        return run
      } catch (error) {
        if (error instanceof ApiError && error.status === 409)
          notify("admin.notice.busy")
        else fail(error)
        return undefined
      }
    },
    [api, fail, notify, refreshOverview],
  )

  return {
    session,
    sessionError,
    retrySession,
    overview,
    revision,
    toasts,
    api,
    login,
    logout,
    act,
    start,
    notify,
    fail,
    dismissToast,
    refreshOverview,
  }
}

export type AdminData = ReturnType<typeof useAdminData>

export const AdminContext = createContext<AdminData | null>(null)

export function useAdmin() {
  const value = useContext(AdminContext)
  if (!value) throw new Error("useAdmin must be used inside the admin console")
  return value
}

/**
 * Load one view resource and reload it when `deps` change or a run finishes.
 * A failed reload keeps the last good data. Only the newest load of the hook
 * writes its state, and a load supersedes only this hook's earlier request.
 */
export function useResource<T>(load: () => Promise<T>, deps: unknown[]) {
  const { revision, fail } = useAdmin()
  const [data, setData] = useState<T | undefined>(undefined)
  const [error, setError] = useState<any>(null)
  const [loading, setLoading] = useState(true)
  const loader = useRef(load)
  loader.current = load
  const reader = useRef("")
  if (!reader.current) reader.current = `read:${++readers}`
  const latest = useRef(0)

  const reload = useCallback(async () => {
    const call = ++latest.current
    setLoading(true)
    try {
      let pending: Promise<T>
      currentReader = reader.current
      try {
        pending = loader.current()
      } finally {
        currentReader = null
      }
      const next = await pending
      if (call !== latest.current) return next
      setData(next)
      setError(null)
      setLoading(false)
      return next
    } catch (failure) {
      // A newer load of this hook is in charge of the state, and a cancelled
      // request (a newer one of this hook, or signing out) has nothing to show.
      if (call !== latest.current) return undefined
      if (failure instanceof Error && failure.name === "AbortError")
        return undefined
      setError(failure)
      setLoading(false)
      fail(failure)
      return undefined
    }
  }, [fail])

  useEffect(() => {
    void reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reload, revision, ...deps])

  // An unmounted view takes no late result.
  useEffect(
    () => () => {
      latest.current++
    },
    [],
  )

  return { data, setData, error, loading, reload }
}
