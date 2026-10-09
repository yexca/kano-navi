import React, { useState } from "react"
import { createRoot } from "react-dom/client"
import { useDashboard } from "/src/dashboard/use-dashboard.ts"
import { useAdminData } from "/src/admin/use-admin-data.ts"
const originalFetch = window.fetch.bind(window)
const originalTimer = window.setTimeout.bind(window)
const originalClear = window.clearTimeout.bind(window)
let hidden = false
Object.defineProperty(document, "visibilityState", {
  configurable: true,
  get: () => (hidden ? "hidden" : "visible"),
})
let clock = 0,
  timerId = 0,
  timers = new Map(),
  scenario = "success",
  counts = {},
  requests = [],
  release = null
window.setTimeout = (callback, delay, ...args) => {
  const id = ++timerId
  timers.set(id, { at: clock + delay, callback: () => callback(...args) })
  return id
}
window.clearTimeout = (id) => timers.delete(id)
window.fetch = async (url, options) => {
  const key = String(url).split("?")[0]
  counts[key] = (counts[key] || 0) + 1
  requests.push({ key, at: clock })
  if (
    (scenario === "first-failure" &&
      key === "/api/dashboard" &&
      counts[key] === 1) ||
    (scenario === "one-503" &&
      key === "/api/admin/sync/jobs" &&
      counts[key] === 2) ||
    (scenario === "persistent" &&
      (key === "/api/dashboard" || key === "/api/admin/sync/jobs"))
  )
    return new Response("{}", { status: 503 })
  if (
    scenario === "unauthorized" &&
    key === "/api/admin/sync/jobs" &&
    counts[key] > 1
  )
    return new Response("{}", { status: 401 })
  if (scenario === "workflow-race") {
    if (key === "/api/admin/sync/jobs" && counts[key] === 2)
      return new Promise((resolve) => {
        release = () => resolve(new Response('{"jobs":[]}'))
      })
    if (key === "/api/admin/workflows" && counts[key] > 1)
      return new Response(
        JSON.stringify({ marker: counts[key] === 2 ? "older" : "newer" }),
      )
  }
  if (
    scenario === "superseded" &&
    (key === "/api/dashboard" || key === "/api/admin/sync/jobs")
  ) {
    if (counts[key] === 1)
      return new Promise((resolve) => {
        release = () =>
          resolve(
            new Response(
              JSON.stringify(
                key === "/api/dashboard"
                  ? { posts: [{ id: "stale", text: "stale response" }] }
                  : { jobs: [{ id: "stale", status: "completed" }] },
              ),
            ),
          )
      })
    if (key === "/api/admin/sync/jobs")
      return new Response(
        JSON.stringify({ jobs: [{ id: "fresh", status: "completed" }] }),
      )
  }
  if (
    scenario === "stall" &&
    (key === "/api/dashboard" || key === "/api/admin/sync/jobs")
  ) {
    return new Promise((resolve, reject) => {
      release = () => resolve(new Response("{}"))
      options?.signal?.addEventListener(
        "abort",
        () => reject(new DOMException("Aborted", "AbortError")),
        { once: true },
      )
    })
  }
  return originalFetch(url, options)
}
const settle = () => new Promise((resolve) => originalTimer(resolve, 100))
async function waitUntil(predicate, message) {
  for (let attempt = 0; attempt < 100; attempt++) {
    if (predicate()) return
    await settle()
  }
  throw new Error(message)
}
async function advance(ms) {
  const end = clock + ms
  while (true) {
    const ready = [...timers]
      .filter(([, timer]) => timer.at <= end)
      .sort((a, b) => a[1].at - b[1].at)[0]
    if (!ready) break
    clock = ready[1].at
    timers.delete(ready[0])
    ready[1].callback()
    await settle()
  }
  clock = end
  await settle()
}
let state = null
function PublicProbe() {
  const data = useDashboard()
  state = data
  return (
    <pre id="probe">
      {JSON.stringify({
        loaded: data.dashboard.posts.length > 0,
        loading: data.isLoading,
        error: data.error?.name,
        calendar: data.calendar.events.length,
      })}
    </pre>
  )
}
function AdminProbe() {
  const data = useAdminData()
  state = data
  return (
    <pre id="probe">
      {JSON.stringify({
        authenticated: data.session?.authenticated,
        loading: data.isLoading,
        jobs: data.jobs.length,
      })}
    </pre>
  )
}
function App() {
  const [mounted, setMounted] = useState(false),
    [kind, setKind] = useState("public"),
    [output, setOutput] = useState("Ready")
  const check = (value, message) => {
    if (!value) throw new Error(message)
  }
  async function run() {
    try {
      const results = []
      for (const [type, mode] of [
        ["public", "first-failure"],
        ["admin", "one-503"],
        ["public", "persistent"],
        ["admin", "persistent"],
        ["admin", "unauthorized"],
        ["public", "stall"],
        ["admin", "stall"],
        ["public", "hidden"],
        ["public", "superseded"],
        ["admin", "superseded"],
        ["admin", "workflow-race"],
        ["public", "calendar-pages"],
      ]) {
        setMounted(false)
        await settle()
        timers.clear()
        counts = {}
        requests = []
        clock = 0
        scenario = mode
        hidden = false
        release = null
        setKind(type)
        setMounted(true)
        await settle()
        await waitUntil(
          () =>
            type === "public"
              ? counts["/api/dashboard"] && counts["/api/calendar"]
              : counts["/api/admin/profile-media"],
          "initial requests must start before virtual time advances",
        )
        if (mode === "workflow-race")
          await waitUntil(
            () => state.config && state.workflowState && !state.isLoading,
            "initial admin snapshot must settle before the workflow race",
          )
        if (mode === "hidden") hidden = true
        await advance(
          ["superseded", "workflow-race", "calendar-pages"].includes(mode)
            ? 0
            : mode === "persistent"
              ? 190000
              : mode === "stall"
                ? 12000
                : 60000,
        )
        const key =
          type === "public" ? "/api/dashboard" : "/api/admin/sync/jobs"
        if (mode === "first-failure") {
          check(counts[key] >= 2, "initial snapshot retry")
          check(
            !state.error && state.dashboard.posts.length > 0,
            "snapshot recovered",
          )
        }
        if (mode === "one-503") {
          check(counts[key] >= 4, "admin continues after 503")
          check(
            state.session?.authenticated && state.config,
            "admin snapshot recovered",
          )
        }
        if (mode === "persistent") {
          check(counts[key] >= 3 && counts[key] < 12, "bounded retry cadence")
          const attempts = requests.filter((item) => item.key === key)
          check(
            attempts.some(
              (item, index) =>
                index > 0 && item.at - attempts[index - 1].at >= 30000,
            ),
            "backoff grows",
          )
          scenario = "success"
          await advance(60000)
          check(
            type === "public"
              ? state.dashboard.posts.length > 0
              : counts[key] > attempts.length && state.workflowState,
            "persistent failure recovery",
          )
        }
        if (mode === "unauthorized") {
          check(state.session?.authenticated === false, "401 requires login")
          const before = counts[key]
          await advance(60000)
          check(counts[key] === before, "401 stops polling")
        }
        if (mode === "stall") {
          const attempts = requests.filter((item) => item.key === key)
          check(
            attempts.length <= 2 &&
              attempts.every(
                (item, index) =>
                  index === 0 || item.at - attempts[index - 1].at >= 12000,
              ),
            "stalled request does not overlap",
          )
          scenario = "success"
          await advance(60000)
          check(counts[key] > 1, "deadline recovery")
        }
        if (mode === "hidden") {
          check(!counts["/api/dashboard/revision"], "hidden skips polling")
          hidden = false
          await advance(8000)
          check(
            counts["/api/dashboard/revision"] === 1,
            "visible resumes polling",
          )
        }
        if (mode === "superseded") {
          if (type === "public") await state.reload()
          else await state.loadActivity()
          await settle()
          release()
          await settle()
          check(
            type === "public"
              ? state.dashboard.posts[0]?.text === "Synthetic local snapshot"
              : state.jobs[0]?.id === "fresh",
            "late response cannot overwrite new state",
          )
        }
        if (mode === "workflow-race") {
          const activity = state.loadActivity()
          await settle()
          await state.loadWorkflows()
          release()
          await activity
          await settle()
          check(
            state.workflowState.marker === "newer",
            "late grouped workflow read cannot overwrite direct refresh",
          )
        }
        if (mode === "calendar-pages") {
          state.setWeekStart(new Date(state.weekStart.getTime() - 7 * 86400000))
          await settle()
          check(
            state.calendar.events.length === 100 &&
              state.calendar.total === 101 &&
              state.calendar.hasNext,
            "bounded first calendar page",
          )
          await state.loadMoreCalendar()
          await settle()
          check(
            state.calendar.events.length === 101 && !state.calendar.hasNext,
            "all weekly events remain reachable",
          )
          state.setWeekStart(
            new Date(state.weekStart.getTime() + 14 * 86400000),
          )
          await settle()
          check(
            state.calendar.events[0]?.title === "Synthetic next week activity",
            "forward calendar navigation",
          )
        }
        const before = { ...counts }
        setMounted(false)
        await settle()
        if (release) release()
        await advance(120000)
        check(
          JSON.stringify(counts) === JSON.stringify(before),
          "unmount stops requests",
        )
        results.push(type + " " + mode + " PASS")
        setOutput(results.join("\n"))
      }
      setOutput((value) => value + "\nALL 12 BROWSER SCENARIOS PASS")
    } catch (error) {
      setOutput(
        "FAIL: " +
          error.message +
          "\n" +
          JSON.stringify(counts) +
          "\n" +
          JSON.stringify(requests),
      )
    }
  }
  return (
    <>
      <h1>Isolated hook regression</h1>
      <button onClick={run}>Run browser regressions</button>
      <pre id="results">{output}</pre>
      {mounted ? kind === "public" ? <PublicProbe /> : <AdminProbe /> : null}
      <p>
        <a href="/">Desktop board</a> <a href="/__mobile">Mobile board</a>{" "}
        <a href="/admin">Admin fixture</a>
      </p>
    </>
  )
}
createRoot(document.getElementById("root")).render(<App />)
