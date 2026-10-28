import assert from "node:assert/strict"
import path from "node:path"
import test, { type TestContext } from "node:test"
import { createElement } from "react"
import { act, create, type ReactTestRenderer } from "react-test-renderer"
import { createServer } from "vite"

test("admin hook gates polling on the session, tracks runs and coordinates requests", async (t) => {
  // Load the actual hook with its normal frontend imports, without the runtime
  // Vite plugin, .env loading, a listener or the operator database.
  const vite = await createServer({
    configFile: false,
    envDir: false,
    server: { middlewareMode: true, watch: null, ws: false },
    optimizeDeps: { noDiscovery: true, include: [] },
    resolve: { alias: { "@": path.resolve("src") } },
  })
  try {
    const { AdminContext, useAdmin, useAdminData, useResource } =
      await vite.ssrLoadModule("/src/admin/use-admin-data.ts")
    // Keep Vite's real watcher/optimizer timers outside the mocked clock.
    await vite.close()
    const setup = async (context: TestContext, { resource = false } = {}) => {
      context.mock.timers.enable({
        apis: ["Date", "setTimeout"],
        now: new Date("2025-01-01T00:00:00Z"),
      })
      const win = Object.assign(new EventTarget(), { setTimeout, clearTimeout })
      const doc = Object.assign(new EventTarget(), {
        visibilityState: "visible",
      })
      const previous = {
        window: globalThis.window,
        document: globalThis.document,
        act: (globalThis as any).IS_REACT_ACT_ENVIRONMENT,
      }
      Object.assign(globalThis, {
        window: win,
        document: doc,
        IS_REACT_ACT_ENVIRONMENT: true,
      })
      type Pending = {
        path: string
        method: string
        body: any
        signal: AbortSignal
        settled: boolean
        resolve: (response: Response) => void
      }
      const requests: Pending[] = []
      type FetchOptions = { method: string; body?: string; signal: AbortSignal }
      context.mock.method(
        globalThis,
        "fetch",
        (url: RequestInfo | URL, options: FetchOptions) => {
          return new Promise<Response>((resolve) =>
            requests.push({
              path: String(url).replace(/^\/api\/admin/, ""),
              method: options.method,
              body:
                options.body === undefined
                  ? undefined
                  : JSON.parse(options.body),
              signal: options.signal,
              settled: false,
              resolve,
            }),
          )
        },
      )
      let value: ReturnType<typeof useAdminData>
      let loaded: ReturnType<typeof useResource>
      let root: ReactTestRenderer
      function Resource() {
        const { api } = useAdmin()
        loaded = useResource(() => api.sources(), [])
        return null
      }
      function Probe() {
        value = useAdminData()
        return createElement(
          AdminContext.Provider,
          { value },
          resource ? createElement(Resource) : null,
        )
      }
      await act(async () => {
        root = create(createElement(Probe))
      })
      const all = (route: string, method = "GET") =>
        requests.filter((item) => item.path === route && item.method === method)
      const open = (route: string, method = "GET") => {
        const found = all(route, method).find((item) => !item.settled)
        assert.ok(found, `expected an open ${method} ${route}`)
        return found
      }
      const respond = async (item: Pending, payload: any, status = 200) => {
        item.settled = true
        return act(async () =>
          item.resolve({
            ok: status >= 200 && status < 300,
            status,
            json: async () => payload,
          } as Response),
        )
      }
      const tick = async (ms: number) =>
        act(async () => context.mock.timers.tick(ms))
      const unmount = async () => {
        await act(async () => root.unmount())
        Object.assign(globalThis, {
          window: previous.window,
          document: previous.document,
          IS_REACT_ACT_ENVIRONMENT: previous.act,
        })
      }
      return {
        doc,
        requests,
        all,
        open,
        respond,
        tick,
        unmount,
        current: () => value,
        resource: () => loaded,
      }
    }
    const signedIn = {
      authenticated: true,
      mode: "production",
      requiresPassword: true,
    }
    const signedOut = { ...signedIn, authenticated: false }
    const run = (id: string) => ({
      id,
      kind: "sync",
      status: "running",
      steps: [],
      progress: {},
    })
    const overview = (activeRun: ReturnType<typeof run> | null = null) => ({
      activeRun,
      recentRuns: [],
    })
    const failure = (code: string) => ({ error: code, message: code })

    await t.test(
      "the session loads first and overview polling starts only once authenticated",
      async (context) => {
        const fixture = await setup(context)
        try {
          assert.deepEqual(
            fixture.requests.map((item) => item.path),
            ["/session"],
          )
          await fixture.respond(fixture.open("/session"), signedOut)
          assert.equal(fixture.current().session.authenticated, false)
          await fixture.tick(60000)
          assert.equal(fixture.all("/overview").length, 0)

          let login: Promise<void> | undefined
          await act(async () => {
            login = fixture.current().login("synthetic-password")
          })
          const post = fixture.open("/session", "POST")
          assert.deepEqual(post.body, { password: "synthetic-password" })
          await fixture.respond(post, signedIn)
          await login
          assert.equal(fixture.current().session.authenticated, true)
          // The first overview read is immediate; idle polling waits 8s.
          await fixture.respond(fixture.open("/overview"), overview())
          await fixture.tick(7999)
          assert.equal(fixture.all("/overview").length, 1)
          await fixture.tick(1)
          assert.equal(fixture.all("/overview").length, 2)
          // An active run shortens the cadence to 1.5s.
          await fixture.respond(
            fixture.open("/overview"),
            overview(run("run-synthetic-1")),
          )
          await fixture.tick(1499)
          assert.equal(fixture.all("/overview").length, 2)
          await fixture.tick(1)
          assert.equal(fixture.all("/overview").length, 3)
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "a hidden console stops the overview poll and a visible one resumes it at once",
      async (context) => {
        const fixture = await setup(context)
        try {
          await fixture.respond(fixture.open("/session"), signedIn)
          await fixture.respond(fixture.open("/overview"), overview())
          fixture.doc.visibilityState = "hidden"
          await act(async () => {
            fixture.doc.dispatchEvent(new Event("visibilitychange"))
          })
          await fixture.tick(120000)
          assert.equal(fixture.all("/overview").length, 1)
          fixture.doc.visibilityState = "visible"
          await act(async () => {
            fixture.doc.dispatchEvent(new Event("visibilitychange"))
            fixture.doc.dispatchEvent(new Event("visibilitychange"))
          })
          assert.equal(fixture.all("/overview").length, 2)
          await fixture.respond(fixture.open("/overview"), overview())
          await fixture.tick(8000)
          assert.equal(fixture.all("/overview").length, 3)
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "a 401 outside /session signs the operator out, cancels reads and stops polling",
      async (context) => {
        const fixture = await setup(context)
        try {
          await fixture.respond(fixture.open("/session"), signedIn)
          await fixture.respond(fixture.open("/overview"), overview())

          // A rejected password on /session itself leaves other reads alone.
          const sources = fixture.current().api.sources()
          const sourcesOutcome = sources.then(
            () => "resolved",
            (error: { name: string }) => error.name,
          )
          const login = fixture.current().api.login("wrong-synthetic")
          const loginOutcome = login.then(
            () => "resolved",
            (error: { status: number }) => error.status,
          )
          await fixture.respond(
            fixture.open("/session", "POST"),
            failure("invalid_credentials"),
            401,
          )
          assert.equal(await loginOutcome, 401)
          assert.equal(fixture.open("/sources").signal.aborted, false)
          assert.equal(fixture.current().session.authenticated, true)

          // The next poll is in flight when another read is refused.
          await fixture.tick(8000)
          const poll = fixture.open("/overview")
          await fixture.respond(
            fixture.open("/sources"),
            failure("admin_auth_required"),
            401,
          )
          assert.equal(await sourcesOutcome, "ApiError")
          assert.equal(fixture.current().session.authenticated, false)
          assert.equal(poll.signal.aborted, true)
          assert.deepEqual(fixture.current().toasts, [])
          await fixture.tick(120000)
          assert.equal(fixture.all("/overview").length, 2)
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "a finished run advances revision and useResource reloads its data",
      async (context) => {
        const fixture = await setup(context, { resource: true })
        try {
          await fixture.respond(fixture.open("/sources"), { label: "first" })
          assert.deepEqual(fixture.resource().data, { label: "first" })
          await fixture.respond(fixture.open("/session"), signedIn)
          await fixture.respond(
            fixture.open("/overview"),
            overview(run("run-synthetic-2")),
          )
          assert.equal(fixture.current().revision, 0)

          // Polls arrive at the active cadence; the same run changes nothing.
          await fixture.tick(8000)
          await fixture.respond(
            fixture.open("/overview"),
            overview(run("run-synthetic-2")),
          )
          assert.equal(fixture.current().revision, 0)
          assert.equal(fixture.all("/sources").length, 1)

          await fixture.tick(1500)
          await fixture.respond(fixture.open("/overview"), overview())
          assert.equal(fixture.current().revision, 1)
          assert.equal(fixture.all("/sources").length, 2)
          assert.equal(fixture.resource().loading, true)
          await fixture.respond(fixture.open("/sources"), { label: "second" })
          assert.deepEqual(fixture.resource().data, { label: "second" })
          assert.equal(fixture.resource().loading, false)

          // An idle overview does not advance revision again.
          await fixture.tick(8000)
          await fixture.respond(fixture.open("/overview"), overview())
          assert.equal(fixture.current().revision, 1)
          assert.equal(fixture.all("/sources").length, 2)
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "start() reports a busy queue on 409 without throwing",
      async (context) => {
        const fixture = await setup(context)
        try {
          await fixture.respond(fixture.open("/session"), signedIn)
          await fixture.respond(fixture.open("/overview"), overview())

          let started: Promise<{ id: string } | undefined> | undefined
          await act(async () => {
            started = fixture.current().start({ kind: "sync" })
          })
          const post = fixture.open("/runs", "POST")
          assert.deepEqual(post.body, { kind: "sync" })
          await fixture.respond(
            post,
            { ...failure("busy"), run: run("run-synthetic-3") },
            409,
          )
          assert.equal(await started, undefined)
          assert.deepEqual(
            fixture
              .current()
              .toasts.map((toast: { tone: string; message: unknown }) => [
                toast.tone,
                toast.message,
              ]),
            [["success", { key: "admin.notice.busy", values: {} }]],
          )
          assert.equal(fixture.current().session.authenticated, true)

          // An accepted run is returned and the overview follows it at once.
          await act(async () => {
            started = fixture.current().start({ kind: "scan" })
          })
          await fixture.respond(fixture.open("/runs", "POST"), {
            run: run("run-synthetic-4"),
          })
          assert.equal((await started)?.id, "run-synthetic-4")
          assert.equal(
            fixture.current().toasts.at(-1).message.key,
            "admin.notice.runQueued",
          )
          assert.equal(fixture.all("/overview").length, 2)
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "reads on the same path supersede each other while writes never cancel",
      async (context) => {
        const fixture = await setup(context)
        try {
          await fixture.respond(fixture.open("/session"), signedOut)
          const { api } = fixture.current()
          const outcome = (promise: Promise<any>) =>
            promise.then(
              (payload) => payload,
              (error) => error.name,
            )

          const older = outcome(api.events({ page: 1 }))
          const other = outcome(api.sources())
          const newer = outcome(api.events({ page: 2 }))
          const [olderRead, newerRead] = fixture
            .all("/events?page=1")
            .concat(fixture.all("/events?page=2"))
          assert.equal(olderRead.signal.aborted, true)
          assert.equal(newerRead.signal.aborted, false)
          assert.equal(fixture.open("/sources").signal.aborted, false)
          assert.equal(await older, "AbortError")
          // A transport that ignores the signal cannot deliver the old page.
          await fixture.respond(olderRead, { items: [], page: 1 })
          await fixture.respond(newerRead, { items: [], page: 2 })
          assert.deepEqual(await newer, { items: [], page: 2 })
          await fixture.respond(fixture.open("/sources"), { label: "kept" })
          assert.deepEqual(await other, { label: "kept" })

          const event = { title: "Synthetic event" }
          const first = outcome(api.createEvent(event))
          const second = outcome(api.createEvent(event))
          const writes = fixture.all("/events", "POST")
          assert.equal(writes.length, 2)
          assert.deepEqual(
            writes.map((item) => item.signal.aborted),
            [false, false],
          )
          await fixture.respond(writes[1], { event: { id: "event-2" } })
          await fixture.respond(writes[0], { event: { id: "event-1" } })
          assert.deepEqual(await first, { event: { id: "event-1" } })
          assert.deepEqual(await second, { event: { id: "event-2" } })
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "a source timer is saved on its own path and a validation message is shown as sent",
      async (context) => {
        const fixture = await setup(context)
        try {
          await fixture.respond(fixture.open("/session"), signedIn)
          await fixture.respond(fixture.open("/overview"), overview())
          const { api, act: perform } = fixture.current()
          const body = {
            intervalMinutes: 45,
            sync: { mode: "count", count: 20 },
          } as const
          const saved = api.updateSourceSchedule("x", body)
          const put = fixture.open("/sources/x/schedule", "PUT")
          assert.deepEqual(put.body, body)
          const schedule = { source: "x", enabled: true, intervalMinutes: 45 }
          await fixture.respond(put, schedule)
          assert.deepEqual(await saved, schedule)

          // The server's invalid_input text reaches the operator unchanged.
          let refused: Promise<unknown> | undefined
          await act(async () => {
            refused = perform(
              () =>
                api.updateSourceSchedule("bilibili", { intervalMinutes: 60 }),
              "admin.notice.scheduleSaved",
            )
          })
          await fixture.respond(
            fixture.open("/sources/bilibili/schedule", "PUT"),
            {
              error: "invalid_input",
              message: "intervalMinutes must be an integer from 180 to 10080",
            },
            400,
          )
          assert.equal(await refused, undefined)
          assert.deepEqual(fixture.current().toasts.at(-1).message, {
            text: "intervalMinutes must be an integer from 180 to 10080",
          })
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "Run now and one-off step runs post to their own paths, and saved workflows are gone",
      async (context) => {
        const fixture = await setup(context)
        try {
          await fixture.respond(fixture.open("/session"), signedIn)
          await fixture.respond(fixture.open("/overview"), overview())
          const { api, start } = fixture.current()
          assert.equal("workflows" in api, false)
          assert.equal("createWorkflow" in api, false)

          const catalog = api.stepCatalog()
          const steps = {
            steps: [
              { id: "x", group: "source" },
              { id: "translate", group: "process" },
            ],
            skippedSteps: [],
          }
          await fixture.respond(fixture.open("/runs/steps"), steps)
          assert.deepEqual(await catalog, steps)

          // Run now: the source's chain, posted without a body.
          let started: Promise<unknown> | undefined
          await act(async () => {
            started = start({ kind: "source", source: "bilibili" })
          })
          const now = fixture.open("/sources/bilibili/run", "POST")
          assert.equal(now.body, undefined)
          await fixture.respond(now, { run: run("run-synthetic-now") }, 202)
          assert.equal(
            ((await started) as { id: string }).id,
            "run-synthetic-now",
          )
          assert.equal(fixture.all("/runs", "POST").length, 0)
          assert.deepEqual(fixture.current().toasts.at(-1).message, {
            key: "admin.notice.runQueued",
            values: {},
          })

          // A one-off step run sends the picked steps; a busy queue is a
          // notice, not an error.
          await act(async () => {
            started = start({ kind: "workflow", steps: ["x", "translate"] })
          })
          const post = fixture.open("/runs", "POST")
          assert.deepEqual(post.body, {
            kind: "workflow",
            steps: ["x", "translate"],
          })
          await fixture.respond(
            post,
            { error: "busy", run: run("run-synthetic-now") },
            409,
          )
          assert.equal(await started, undefined)
          assert.deepEqual(fixture.current().toasts.at(-1).message, {
            key: "admin.notice.busy",
            values: {},
          })
        } finally {
          await fixture.unmount()
        }
      },
    )
  } finally {
    await vite.close()
  }
})
