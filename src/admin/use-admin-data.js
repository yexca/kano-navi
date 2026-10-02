import { useCallback, useEffect, useRef, useState } from "react"

import { adminMessage, formatAdminError, jsonBody, request } from "@/admin/api"

const emptyFilters = {
  search: "",
  provenance: "",
  from: "",
  to: "",
  includeDeleted: false,
}

function eventsQuery(query = {}) {
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

  const dismissToast = useCallback((id) => {
    setToasts((current) => current.filter((toast) => toast.id !== id))
  }, [])

  const pushToast = useCallback(
    (message, tone = "success") => {
      if (!message) return
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

  const loadEventsPage = useCallback(async (query = {}) => {
    const filters = query.filters || filtersRef.current
    const page = query.page || 1
    const payload = await request(
      `/events?${eventsQuery({ ...filters, page })}`,
    )
    pageRef.current = payload.page || page
    setEventsPage(payload)
    return payload
  }, [])

  const loadConfig = useCallback(async () => {
    const payload = await request("/config")
    setConfig(payload)
    return payload
  }, [])

  const loadWorkflows = useCallback(async () => {
    const payload = await request("/workflows")
    setWorkflowState(payload)
    return payload
  }, [])

  const loadActivity = useCallback(async () => {
    const [jobPayload, runPayload, workflowPayload] = await Promise.all([
      request("/sync/jobs?limit=10"),
      request("/sync/runs?limit=12"),
      request("/workflows"),
    ])
    setJobs(jobPayload.jobs || [])
    setRuns(runPayload.runs || [])
    setWorkflowState(workflowPayload)
    return jobPayload.jobs || []
  }, [])

  const loadVideos = useCallback(async () => {
    const payload = await request("/videos")
    setVideos(payload.videos || [])
  }, [])

  const loadPostLlm = useCallback(async () => {
    const payload = await request("/posts/llm?limit=60")
    setPostLlm(payload.posts || [])
    return payload.posts || []
  }, [])

  const loadScheduleAssets = useCallback(async () => {
    const payload = await request("/schedule-assets?limit=100")
    setScheduleAssets(payload.assets || [])
    return payload.assets || []
  }, [])

  const loadProfileMedia = useCallback(async () => {
    setProfileMedia(await request("/profile-media"))
  }, [])

  const loadAll = useCallback(async () => {
    await Promise.all([
      loadConfig(),
      loadActivity(),
      loadEventsPage({ page: 1 }),
      loadVideos(),
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
  ])

  useEffect(() => {
    let active = true
    request("/session")
      .then(async (payload) => {
        if (!active) return
        setSession(payload)
        if (payload.authenticated) await loadAll()
      })
      .catch((error) => active && fail(error))
      .finally(() => active && setIsLoading(false))
    return () => {
      active = false
    }
  }, [fail, loadAll])

  const activeJob = jobs.find(isActiveJob) || null

  // Poll job state; poll faster while a job is running and refresh the
  // records that a finished job may have changed.
  useEffect(() => {
    if (!session?.authenticated) return undefined
    const delay = activeJob ? 1500 : 6000
    const timer = window.setTimeout(async () => {
      try {
        const latestJobs = await loadActivity()
        const stillActive = latestJobs.some(isActiveJob)
        if (wasActiveRef.current && !stillActive) {
          await Promise.all([
            loadEventsPage({ page: pageRef.current }),
            loadConfig(),
            loadVideos(),
            loadPostLlm(),
            loadScheduleAssets(),
          ])
        }
        wasActiveRef.current = stillActive
      } catch (error) {
        if (error?.status === 401) {
          setSession((current) => ({ ...current, authenticated: false }))
        }
      }
    }, delay)
    return () => window.clearTimeout(timer)
  }, [
    session?.authenticated,
    activeJob,
    jobs,
    loadActivity,
    loadConfig,
    loadEventsPage,
    loadVideos,
    loadPostLlm,
    loadScheduleAssets,
  ])

  const login = useCallback(
    async (password) => {
      const payload = await request("/login", {
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
      await request("/logout", { method: "POST" })
      setSession((current) => ({ ...current, authenticated: false }))
      setConfig(null)
    } catch (error) {
      fail(error)
    }
  }, [fail])

  const updateFilters = useCallback((name, value) => {
    setEventFilters((current) => {
      const next = { ...current, [name]: value }
      filtersRef.current = next
      return next
    })
  }, [])

  const resetFilters = useCallback(() => {
    filtersRef.current = emptyFilters
    setEventFilters(emptyFilters)
  }, [])

  /** Start a job and report whether it was accepted. */
  const startJob = useCallback(
    async (path) => {
      try {
        const payload = await request(path, { method: "POST" })
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
      const payload = await request(
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
      const payload = await request(
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
  }
}
