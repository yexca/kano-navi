import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import { createRequestScope, startPolling } from "../lib/polling"

import { adminMessage, formatAdminError, jsonBody, request } from "@/admin/api"

const emptyFilters = {
  search: "",
  provenance: "",
  from: "",
  to: "",
  includeDeleted: false,
}

function eventsQuery(query: Record<string, any> = {}) {
  const params = new URLSearchParams({
    page: String(query.page || 1),
    pageSize: String(query.pageSize || 20),
  })
  if (query.search) params.set("search", query.search)
  if (query.provenance) params.set("provenance", query.provenance)
  if (query.from) params.set("from", query.from)
  if (query.to) params.set("to", query.to)
  if (query.includeDeleted) params.set("includeDeleted", "1")
  return params.toString()
}

function isActiveJob(job) {
  return job?.status === "running" || job?.status === "queued"
}

/**
 * Owns every admin request and the polling of job state. Views receive data
 * and narrow callbacks; they do not keep their own copies of server state.
 */
export function useAdminData() {
  const [session, setSession] = useState(null)
  const [isLoading, setIsLoading] = useState(true)
  const [config, setConfig] = useState(null)
  const [workflowState, setWorkflowState] = useState(null)
  const [jobs, setJobs] = useState([])
  const [runs, setRuns] = useState([])
  const [eventsPage, setEventsPage] = useState(null)
  const [eventFilters, setEventFilters] = useState(emptyFilters)
  const [videos, setVideos] = useState([])
  const [sourceFetch, setSourceFetch] = useState(null)
  const [postLlm, setPostLlm] = useState([])
  const [scheduleAssets, setScheduleAssets] = useState([])
  const [profileMedia, setProfileMedia] = useState({
    items: [],
    active: { avatar: null, banner: null },
  })
  const [toasts, setToasts] = useState([])
  const toastId = useRef(0)
  const filtersRef = useRef(emptyFilters)
  const pageRef = useRef(1)
  const wasActiveRef = useRef(false)
  const workflowReadRef = useRef(0)
  const scope = useMemo(() => createRequestScope(), [])
  const scopedRequest = useCallback(
    (path, options: Record<string, any> = {}) =>
      scope.run(path.split("?")[0], (signal) =>
        request(path, { ...options, signal }),
      ),
    [scope],
  )
  useEffect(() => {
    scope.activate()
    return () => scope.dispose()
  }, [scope])

  const dismissToast = useCallback(
    (id) => {
      if (scope.active)
        setToasts((current) => current.filter((toast) => toast.id !== id))
    },
    [scope],
  )

  const pushToast = useCallback(
    (message, tone = "success") => {
      if (!message || !scope.active) return
      toastId.current += 1
      const id = toastId.current
      setToasts((current) => [...current.slice(-3), { id, message, tone }])
      if (tone !== "danger") {
        window.setTimeout(() => dismissToast(id), 4200)
      }
    },
    [dismissToast],
  )

  const notify = useCallback((message) => pushToast(message), [pushToast])
  const fail = useCallback(
    (error) =>
      pushToast(
        error && (error.key || error.text) ? error : formatAdminError(error),
        "danger",
      ),
    [pushToast],
  )

  const loadEventsPage = useCallback(
    async (query: Record<string, any> = {}) => {
      const filters = query.filters || filtersRef.current
      const page = query.page || 1
      const payload = await scopedRequest(
        `/events?${eventsQuery({ ...filters, page })}`,
      )
      pageRef.current = payload.page || page
      setEventsPage(payload)
      return payload
    },
    [scopedRequest],
  )

  const loadConfig = useCallback(async () => {
    const payload = await scopedRequest("/config")
    setConfig(payload)
    return payload
  }, [scopedRequest])

  const loadWorkflows = useCallback(async () => {
    const read = ++workflowReadRef.current
    const payload = await scopedRequest("/workflows")
    if (read === workflowReadRef.current) setWorkflowState(payload)
    return payload
  }, [scopedRequest])

  const loadActivity = useCallback(async () => {
    const workflowRead = ++workflowReadRef.current
    const [jobPayload, runPayload, workflowPayload] = await scope.run(
      "activity",
      (signal) =>
        Promise.all([
          request("/sync/jobs?limit=10", { signal }),
          request("/sync/runs?limit=12", { signal }),
          request("/workflows", { signal }),
        ]),
    )
    if (!scope.active) return []
    setJobs(jobPayload.jobs || [])
    setRuns(runPayload.runs || [])
    if (workflowRead === workflowReadRef.current)
      setWorkflowState(workflowPayload)
    return jobPayload.jobs || []
  }, [scopedRequest])

  const loadVideos = useCallback(async () => {
    const payload = await scopedRequest("/videos")
    setVideos(payload.videos || [])
  }, [scopedRequest])
  const loadSourceFetch = useCallback(async () => {
    setSourceFetch(await scopedRequest("/sources/fetch"))
  }, [scopedRequest])

  const loadPostLlm = useCallback(async () => {
    const payload = await scopedRequest("/posts/llm?limit=60")
    setPostLlm(payload.posts || [])
    return payload.posts || []
  }, [scopedRequest])

  const loadScheduleAssets = useCallback(async () => {
    const payload = await scopedRequest("/schedule-assets?limit=100")
    setScheduleAssets(payload.assets || [])
    return payload.assets || []
  }, [scopedRequest])

  const loadProfileMedia = useCallback(async () => {
    setProfileMedia(await scopedRequest("/profile-media"))
  }, [scopedRequest])

  const loadAll = useCallback(async () => {
    await Promise.all([
      loadConfig(),
      loadActivity(),
      loadEventsPage({ page: 1 }),
      loadVideos(),
      loadSourceFetch(),
      loadPostLlm(),
      loadScheduleAssets(),
      loadProfileMedia(),
    ])
  }, [
    loadActivity,
    loadConfig,
    loadEventsPage,
    loadPostLlm,
    loadScheduleAssets,
    loadProfileMedia,
    loadVideos,
    loadSourceFetch,
  ])

  useEffect(() => {
    let active = true,
      loaded = false,
      initialInFlight = false
    const initialLoad = async () => {
      if (initialInFlight) return
      initialInFlight = true
      try {
        const payload = await scopedRequest("/session")
        if (!active) return
        setSession(payload)
        if (payload.authenticated) await loadAll()
        loaded = true
      } finally {
        initialInFlight = false
      }
    }
    void initialLoad()
      .catch((error) => {
        if (active && error.name !== "AbortError") fail(error)
      })
      .finally(() => active && setIsLoading(false))
    const stop = startPolling(
      async () => {
        if (!loaded) await initialLoad()
      },
      {
        delay: () => 6000,
        onError: (error) => {
          if (error?.status === 401) {
            loaded = true
            scope.cancel()
            setSession((current) => ({ ...current, authenticated: false }))
          }
        },
      },
    )
    return () => {
      active = false
      stop()
    }
  }, [fail, loadAll, scopedRequest, scope])

  const activeJob = jobs.find(isActiveJob) || null

  const activeJobRef = useRef(false)
  activeJobRef.current = Boolean(activeJob)
  // The loop owns its timer, independent of successful React state updates.
  useEffect(() => {
    if (!session?.authenticated) return undefined
    return startPolling(
      async () => {
        if (scope.busy("activity")) return
        const latestJobs = await loadActivity()
        const stillActive = latestJobs.some(isActiveJob)
        if (wasActiveRef.current && !stillActive) {
          await Promise.all([
            loadEventsPage({ page: pageRef.current }),
            loadConfig(),
            loadVideos(),
            loadSourceFetch(),
            loadPostLlm(),
            loadScheduleAssets(),
          ])
        }
        wasActiveRef.current = stillActive
      },
      {
        delay: () => (activeJobRef.current ? 1500 : 6000),
        onError: (error) => {
          if (error?.status === 401) {
            scope.cancel()
            setSession((current) => ({ ...current, authenticated: false }))
          }
        },
      },
    )
  }, [
    session?.authenticated,
    scope,
    loadActivity,
    loadConfig,
    loadEventsPage,
    loadVideos,
    loadSourceFetch,
    loadPostLlm,
    loadScheduleAssets,
  ])

  const login = useCallback(
    async (password) => {
      const payload = await scopedRequest("/login", {
        method: "POST",
        body: jsonBody({ password }),
      })
      setSession(payload)
      await loadAll()
    },
    [loadAll],
  )

  const logout = useCallback(async () => {
    try {
      await scopedRequest("/logout", { method: "POST" })
      setSession((current) => ({ ...current, authenticated: false }))
      setConfig(null)
    } catch (error) {
      fail(error)
    }
  }, [fail])

  const updateFilters = useCallback(
    (name, value) => {
      setEventFilters((current) => {
        const next = { ...current, [name]: value }
        filtersRef.current = next
        return next
      })
    },
    [scopedRequest],
  )

  const resetFilters = useCallback(() => {
    filtersRef.current = emptyFilters
    setEventFilters(emptyFilters)
  }, [scopedRequest])

  /** Start a job and report whether it was accepted. */
  const startJob = useCallback(
    async (path, body = null) => {
      try {
        const payload = await scopedRequest(path, {
          method: "POST",
          ...(body ? { body: jsonBody(body) } : {}),
        })
        await loadActivity()
        wasActiveRef.current = true
        notify(adminMessage("admin.notice.queued"))
        return payload
      } catch (error) {
        if (error.status === 409) {
          await loadActivity().catch(() => {})
          notify(adminMessage("admin.notice.alreadyRunning"))
          return null
        }
        fail(error)
        return null
      }
    },
    [fail, loadActivity, notify],
  )

  const reprocessPostLlm = useCallback(
    async (postId, route = null) => {
      const payload = await scopedRequest(
        `/posts/${encodeURIComponent(postId)}/llm/reprocess`,
        { method: "POST", body: jsonBody(route ? { route } : {}) },
      )
      await loadPostLlm()
      return payload.state
    },
    [loadPostLlm],
  )

  const reviewScheduleAsset = useCallback(
    async (assetId, status, reason) => {
      const payload = await scopedRequest(
        `/schedule-assets/${encodeURIComponent(assetId)}/manual-review`,
        {
          method: "POST",
          body: jsonBody({ status, reason }),
        },
      )
      await loadScheduleAssets()
      return payload.asset
    },
    [loadScheduleAssets],
  )

  const correctSchedulePeriod = useCallback(
    async (assetId, start, end, reason) => {
      const payload = await scopedRequest(
        `/schedule-assets/${encodeURIComponent(assetId)}/period`,
        { method: "POST", body: jsonBody({ start, end, reason }) },
      )
      await loadScheduleAssets()
      return payload.asset
    },
    [scopedRequest, loadScheduleAssets],
  )

  return {
    session,
    isLoading,
    config,
    setConfig,
    workflowState,
    setWorkflowState,
    jobs,
    runs,
    activeJob,
    eventsPage,
    eventFilters,
    videos,
    sourceFetch,
    postLlm,
    scheduleAssets,
    profileMedia,
    setProfileMedia,
    toasts,
    dismissToast,
    notify,
    fail,
    login,
    logout,
    loadAll,
    loadConfig,
    loadWorkflows,
    loadActivity,
    loadEventsPage,
    updateFilters,
    resetFilters,
    startJob,
    loadPostLlm,
    loadScheduleAssets,
    reprocessPostLlm,
    reviewScheduleAsset,
    correctSchedulePeriod,
  }
}
