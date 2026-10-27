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
  EventInput,
  Extraction,
  FetchWindow,
  LlmCatalog,
  Model,
  Overview,
  Page,
  ProfileMedia,
  ProfileMediaState,
  Provider,
  QueueRoute,
  RouteTarget,
  Run,
  ScheduleImage,
  ScheduleSource,
  Session,
  SourceSchedule,
  Sources,
  TranslationLocale,
  TranslationOwner,
  TranslationRequestResult,
  TranslationSummary,
  AdminTranslation,
  VideoCorrection,
  VideoHistory,
  VideoMergeSuggestion,
  Workflow,
  WorkflowCatalog,
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
  | { kind: "workflow"; workflowId: string }
  | { kind: "fetch"; source: "x" | "youtube"; window: FetchWindow }

/**
 * Every /api/admin endpoint the console calls. Reads are keyed by path so a
 * newer read supersedes an older one; writes never cancel each other.
 */
function createClient(run: Runner, slow: Runner) {
  let writes = 0
  const get = <T>(path: string) => run<T>(path.split("?")[0], path)
  const send = <T>(method: string, path: string, body?: unknown) =>
    run<T>(`write:${++writes}`, path, { method, body })
  const sendSlow = <T>(method: string, path: string, body?: unknown) =>
    slow<T>(`slow:${++writes}`, path, { method, body })
  return {
    session: () => get<Session>("/session"),
    login: (password: string) =>
      send<Session>("POST", "/session", { password }),
    logout: () => send<null>("DELETE", "/session"),

    overview: () => get<Overview>("/overview"),
    runs: (page: number) =>
      get<Page<Run>>(`/runs${query({ page, pageSize: 15 })}`),
    run: (id: string) => get<{ run: Run }>(`/runs/${enc(id)}`),
    startRun: (body: StartRun) => send<{ run: Run }>("POST", "/runs", body),
    cancelRun: (id: string) =>
      send<{ run: Run }>("POST", `/runs/${enc(id)}/cancel`),
    retryRun: (id: string) =>
      send<{ run: Run }>("POST", `/runs/${enc(id)}/retry`),

    workflows: () => get<WorkflowCatalog>("/workflows"),
    createWorkflow: (body: Partial<Workflow>) =>
      send<{ workflow: Workflow }>("POST", "/workflows", body),
    updateWorkflow: (id: string, body: Partial<Workflow>) =>
      send<{ workflow: Workflow }>("PUT", `/workflows/${enc(id)}`, body),
    deleteWorkflow: (id: string) =>
      send<null>("DELETE", `/workflows/${enc(id)}`),

    sources: () => get<Sources>("/sources"),
    updateSourceSchedule: (
      source: ScheduleSource,
      body: Partial<
        Pick<SourceSchedule, "enabled" | "intervalMinutes" | "sync">
      >,
    ) => send<SourceSchedule>("PUT", `/sources/${enc(source)}/schedule`, body),
    updatePush: (body: { enabled?: boolean; callbackUrl?: string | null }) =>
      send<Sources["youtube"]["push"]>("PUT", "/sources/youtube/push", body),
    updateLiveProbe: (body: { enabled?: boolean; intervalMinutes?: number }) =>
      send<Sources["youtube"]["liveProbe"]>(
        "PUT",
        "/sources/youtube/live-probe",
        body,
      ),
    rotatePushSecret: () =>
      send<Sources["youtube"]["push"]>(
        "POST",
        "/sources/youtube/push/rotate-secret",
      ),

    events: (filters: Record<string, any>) =>
      get<Page<AdminEvent>>(`/events${query(filters)}`),
    createEvent: (body: EventInput) =>
      send<{ event: AdminEvent }>("POST", "/events", body),
    updateEvent: (id: string, body: EventInput) =>
      send<{ event: AdminEvent }>("PUT", `/events/${enc(id)}`, body),
    confirmEvent: (id: string, body?: Partial<EventInput>) =>
      send<{ event: AdminEvent }>("POST", `/events/${enc(id)}/confirm`, body),
    deleteEvent: (id: string) => send<null>("DELETE", `/events/${enc(id)}`),
    unmergeEvent: (id: string) =>
      send<{ event: AdminEvent }>("POST", `/events/${enc(id)}/unmerge`),

    scheduleImages: () =>
      get<{ items: ScheduleImage[] }>("/schedule-images?limit=100"),
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
    reprocessPost: (id: string, route?: string) =>
      send<{ state: unknown }>(
        "POST",
        `/posts/${enc(id)}/reprocess`,
        route ? { route } : {},
      ),

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
        | { missing: true; ownerType?: TranslationOwner }
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
    refreshVideo: (id: string) =>
      send<{ video: AdminVideo; started: boolean; run: Run | null }>(
        "POST",
        `/videos/${enc(id)}/refresh`,
        {},
      ),
    addBilibiliVideo: (input: string) =>
      send<{ video: AdminVideo; started: boolean; run: Run | null }>(
        "POST",
        "/videos/bilibili",
        { input },
      ),
    /** Uploads the merge rule would attach, or leaves to the operator. */
    videoMerges: () => get<{ items: VideoMergeSuggestion[] }>("/videos/merges"),
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

    profileMedia: () => get<ProfileMediaState>("/profile-media"),
    addProfileMedia: (slot: string, source: string, sourceUrl: string) =>
      send<ProfileMediaState>("POST", "/profile-media", {
        slot,
        source,
        sourceUrl,
      }),
    discoverProfileMedia: (slot: string, source: string) =>
      sendSlow<ProfileMediaState & { candidates: ProfileMedia[] }>(
        "POST",
        "/profile-media/discover",
        { slot, source },
      ),
    uploadProfileMedia: (slot: string, file: Blob) =>
      sendSlow<ProfileMediaState>(
        "POST",
        `/profile-media/upload${query({ slot })}`,
        file,
      ),
    downloadProfileMedia: (id: string) =>
      sendSlow<ProfileMediaState>("POST", `/profile-media/${enc(id)}/download`),
    selectProfileMedia: (id: string) =>
      send<ProfileMediaState>("POST", `/profile-media/${enc(id)}/select`),

    extraction: () => get<Extraction>("/extraction"),
    updateExtraction: (body: Partial<Extraction>) =>
      send<Extraction>("PUT", "/extraction", body),
    llm: () => get<LlmCatalog>("/llm"),
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

  // Session first; the overview poll starts once authenticated.
  useEffect(() => {
    let cancelled = false
    api
      .session()
      .then((next) => !cancelled && setSession(next))
      .catch((error) => {
        if (cancelled || error?.name === "AbortError") return
        fail(error)
        setSession({
          authenticated: false,
          mode: "production",
          requiresPassword: true,
        })
      })
    return () => {
      cancelled = true
    }
  }, [api, fail])

  const authenticated = Boolean(session?.authenticated)
  useEffect(() => {
    if (!authenticated) return undefined
    void refreshOverview().catch(fail)
    return startPolling(refreshOverview, {
      delay: () => (activeRef.current ? 1500 : 8000),
    })
  }, [authenticated, refreshOverview, fail])

  const login = useCallback(
    async (password: string) => {
      setSession(await api.login(password))
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
    async (body: StartRun) => {
      try {
        const { run } = await api.startRun(body)
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
 * A failed reload keeps the last good data.
 */
export function useResource<T>(load: () => Promise<T>, deps: unknown[]) {
  const { revision, fail } = useAdmin()
  const [data, setData] = useState<T | undefined>(undefined)
  const [error, setError] = useState<any>(null)
  const [loading, setLoading] = useState(true)
  const loader = useRef(load)
  loader.current = load

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      const next = await loader.current()
      setData(next)
      setError(null)
      setLoading(false)
      return next
    } catch (failure) {
      // A superseded read leaves the newer one in charge of the state.
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

  return { data, setData, error, loading, reload }
}
