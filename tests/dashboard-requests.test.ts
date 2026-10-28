import assert from "node:assert/strict"
import path from "node:path"
import test, { type TestContext } from "node:test"
import { createElement } from "react"
import { act, create, type ReactTestRenderer } from "react-test-renderer"
import { createServer } from "vite"

test("dashboard hook recovers, coordinates requests and releases its resources", async (t) => {
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
    const { useDashboard } = await vite.ssrLoadModule(
      "/src/dashboard/use-dashboard.ts",
    )
    // Keep Vite's real watcher/optimizer timers outside the mocked clock.
    await vite.close()
    const setup = async (context: TestContext, controlCalendar = false) => {
      context.mock.timers.enable({
        apis: ["Date", "setTimeout"],
        now: new Date("2025-01-01T00:00:00Z"),
      })
      const doc = Object.assign(new EventTarget(), {
        visibilityState: "visible",
      })
      const win = Object.assign(new EventTarget(), { setTimeout, clearTimeout })
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
      const requests: {
        url: string
        signal: AbortSignal
        resolve: (response: Response) => void
        reject: (error: Error) => void
      }[] = []
      const calendarRequests: typeof requests = []
      let calendarRevision = 0
      context.mock.method(
        globalThis,
        "fetch",
        (url: RequestInfo | URL, options: { signal: AbortSignal }) => {
          if (String(url).startsWith("/api/calendar") && controlCalendar)
            return new Promise<Response>((resolve, reject) =>
              calendarRequests.push({
                url: String(url),
                signal: options.signal,
                resolve,
                reject,
              }),
            )
          if (String(url).startsWith("/api/calendar"))
            return Promise.resolve({
              ok: true,
              status: 200,
              json: async () => ({
                events: [],
                scheduleImages: [],
                adjacent: { previous: null, next: null },
                total: 0,
                page: 1,
                hasNext: false,
                revision: calendarRevision,
                dayCounts: {},
              }),
            } as Response)
          return new Promise<Response>((resolve, reject) =>
            requests.push({
              url: String(url),
              signal: options.signal,
              resolve,
              reject,
            }),
          )
        },
      )
      let value: ReturnType<typeof useDashboard>
      let root: ReactTestRenderer
      function Probe() {
        value = useDashboard(Date.parse("2025-01-01T00:00:00Z"))
        return null
      }
      await act(async () => {
        root = create(createElement(Probe))
      })
      const tick = async (ms: number) =>
        act(async () => context.mock.timers.tick(ms))
      const respond = async (index: number, payload: any) => {
        if (payload.meta?.revision != null)
          calendarRevision = payload.meta.revision
        return act(async () =>
          requests[index].resolve({
            ok: true,
            status: 200,
            json: async () => payload,
          } as Response),
        )
      }
      const reject = async (index: number) =>
        act(async () => requests[index].reject(new Error("Synthetic outage")))
      const respondCalendar = async (index: number, payload: any) =>
        act(async () =>
          calendarRequests[index].resolve({
            ok: true,
            status: 200,
            json: async () => payload,
          } as Response),
        )
      const unmount = async () => {
        await act(async () => root.unmount())
        Object.assign(globalThis, {
          window: previous.window,
          document: previous.document,
          IS_REACT_ACT_ENVIRONMENT: previous.act,
        })
      }
      return {
        requests,
        calendarRequests,
        respondCalendar,
        doc,
        win,
        tick,
        respond,
        reject,
        unmount,
        current: () => value,
      }
    }
    const snapshot = (revision: number) => ({
      profile: { displayName: `Snapshot ${revision}` },
      meta: { revision },
    })
    const calendarPage = (
      page: number,
      liveStatus: string | null,
      hasNext = false,
    ) => ({
      events: [{ id: `event-${page}`, liveStatus }],
      scheduleImages: [],
      adjacent: { previous: null, next: null },
      total: 2,
      page,
      hasNext,
      revision: 1,
      dayCounts: {},
    })

    await t.test(
      "manual snapshot refresh updates every loaded calendar page at the same revision",
      async (context) => {
        const fixture = await setup(context, true)
        const states = () =>
          fixture
            .current()
            .calendar.events.map((event: any) => event.liveStatus)
        try {
          await fixture.respond(0, snapshot(1))
          await fixture.respondCalendar(0, calendarPage(1, "live", true))
          assert.equal(fixture.calendarRequests.length, 1)
          assert.equal(fixture.current().snapshotVersion, 1)
          await act(async () => void fixture.current().loadMoreCalendar())
          await fixture.respondCalendar(1, calendarPage(2, "upcoming"))
          await act(async () => void fixture.current().reload())
          await fixture.respond(1, snapshot(1))
          assert.equal(fixture.current().snapshotVersion, 2)
          await fixture.respondCalendar(2, calendarPage(1, null, true))
          assert.deepEqual(states(), ["live", "upcoming"])
          assert.match(fixture.calendarRequests[3].url, /&page=2$/)
          await fixture.respondCalendar(3, calendarPage(2, null))
          assert.deepEqual(states(), [null, null])
          assert.equal(fixture.current().calendar.page, 2)
          assert.equal(fixture.current().calendar.revision, 1)
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "a snapshot during calendar pagination keeps a pending time refresh for the next poll",
      async (context) => {
        const fixture = await setup(context, true)
        try {
          await fixture.respond(0, snapshot(1))
          await fixture.respondCalendar(0, calendarPage(1, "live", true))
          await act(async () => void fixture.current().loadMoreCalendar())
          await act(async () => void fixture.current().reload())
          await fixture.respond(1, snapshot(1))
          assert.equal(fixture.calendarRequests.length, 2)
          assert.equal(fixture.calendarRequests[1].signal.aborted, false)
          await fixture.respondCalendar(1, calendarPage(2, "live"))
          await fixture.tick(8000)
          await fixture.respond(2, { revision: 1 })
          assert.equal(fixture.calendarRequests.length, 3)
          await fixture.respondCalendar(2, calendarPage(1, null, true))
          await fixture.respondCalendar(3, calendarPage(2, null))
          assert.deepEqual(
            fixture
              .current()
              .calendar.events.map((event: any) => event.liveStatus),
            [null, null],
          )
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "only a week without a loaded snapshot reports calendar loading",
      async (context) => {
        const fixture = await setup(context, true)
        const emptyWeek = {
          ...calendarPage(1, null),
          events: [],
          total: 0,
        }
        try {
          await fixture.respond(0, snapshot(1))
          assert.equal(fixture.current().calendarLoading, true)
          await fixture.respondCalendar(0, emptyWeek)
          assert.equal(fixture.current().calendarLoading, false)
          // A background refresh of the same week keeps it rendered.
          await act(async () => void fixture.current().reload())
          await fixture.respond(1, snapshot(1))
          assert.equal(fixture.calendarRequests.length, 2)
          assert.equal(fixture.current().calendarLoading, false)
          await fixture.respondCalendar(1, emptyWeek)
          assert.equal(fixture.current().calendarLoading, false)
          // Another week has nothing loaded yet.
          await act(async () =>
            fixture
              .current()
              .setWeekStart(
                (week: Date) => new Date(week.getTime() + 7 * 864e5),
              ),
          )
          assert.equal(fixture.calendarRequests.length, 3)
          assert.equal(fixture.current().calendarLoading, true)
          await fixture.respondCalendar(2, emptyWeek)
          assert.equal(fixture.current().calendarLoading, false)
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "first failure retries with bounded backoff, and later failures retain the known snapshot",
      async (context) => {
        const fixture = await setup(context)
        try {
          await fixture.reject(0)
          assert.equal(fixture.current().isLoading, false)
          assert.equal(fixture.current().isRefreshing, false)
          await fixture.tick(7999)
          assert.equal(fixture.requests.length, 1)
          await fixture.tick(1)
          assert.equal(fixture.requests[1].url, "/api/dashboard?days=3")
          await fixture.reject(1)
          await fixture.tick(15999)
          assert.equal(fixture.requests.length, 2)
          await fixture.tick(1)
          await fixture.respond(2, snapshot(1))
          assert.equal(
            fixture.current().dashboard.profile.displayName,
            "Snapshot 1",
          )
          assert.equal(fixture.current().error, null)
          let reload: Promise<unknown> | undefined
          await act(async () => {
            reload = fixture.current().reload()
          })
          await fixture.reject(3)
          await reload
          assert.equal(
            fixture.current().dashboard.profile.displayName,
            "Snapshot 1",
          )
          assert.equal(fixture.current().isRefreshing, false)
          await fixture.tick(8000)
          assert.equal(fixture.requests[4].url, "/api/dashboard?days=3")
          await fixture.respond(4, snapshot(2))
          assert.equal(
            fixture.current().dashboard.profile.displayName,
            "Snapshot 2",
          )
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "manual refresh supersedes an old revision response and duplicate refreshes share one snapshot",
      async (context) => {
        const fixture = await setup(context)
        try {
          await fixture.respond(0, snapshot(1))
          await fixture.tick(8000)
          assert.match(fixture.requests[1].url, /revision\?since=1$/)
          let first: Promise<unknown> | undefined
          let second: Promise<unknown> | undefined
          await act(async () => {
            first = fixture.current().reload()
            second = fixture.current().reload()
          })
          assert.equal(first, second)
          assert.equal(fixture.requests[1].signal.aborted, true)
          assert.equal(fixture.requests.length, 3)
          await fixture.respond(2, snapshot(3))
          await first
          // A mock transport deliberately ignores AbortSignal and completes late.
          await fixture.respond(1, { revision: 2, changed: true })
          assert.equal(
            fixture.current().dashboard.profile.displayName,
            "Snapshot 3",
          )
          assert.equal(fixture.requests.length, 3)
          await fixture.tick(8000)
          assert.match(fixture.requests[3].url, /revision\?since=3$/)
          await fixture.respond(3, { revision: 4 })
          assert.equal(fixture.requests.length, 5)
          await fixture.respond(4, snapshot(4))
          assert.equal(
            fixture.current().dashboard.profile.displayName,
            "Snapshot 4",
          )
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "the deadline covers a stalled JSON body; a late snapshot cannot replace recovery",
      async (context) => {
        const fixture = await setup(context)
        let finishBody: (payload: any) => void
        try {
          const body = new Promise((resolve) => {
            finishBody = resolve
          })
          await act(async () =>
            fixture.requests[0].resolve({ ok: true, json: () => body } as any),
          )
          await fixture.tick(15000)
          assert.equal(fixture.requests[0].signal.aborted, true)
          assert.match(fixture.current().error.message, /timed out/)
          assert.equal(fixture.current().isRefreshing, false)
          await fixture.tick(8000)
          await fixture.respond(1, snapshot(2))
          await act(async () => finishBody(snapshot(1)))
          assert.equal(
            fixture.current().dashboard.profile.displayName,
            "Snapshot 2",
          )
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "hidden pages pause polling; visibility and focus coalesce without bypassing outage backoff",
      async (context) => {
        const fixture = await setup(context)
        try {
          await fixture.respond(0, snapshot(1))
          fixture.doc.visibilityState = "hidden"
          fixture.doc.dispatchEvent(new Event("visibilitychange"))
          await fixture.tick(60000)
          assert.equal(fixture.requests.length, 1)
          fixture.doc.visibilityState = "visible"
          fixture.doc.dispatchEvent(new Event("visibilitychange"))
          fixture.win.dispatchEvent(new Event("focus"))
          await fixture.tick(0)
          assert.equal(fixture.requests.length, 2)
          await fixture.reject(1)
          for (let i = 0; i < 10; i++)
            fixture.win.dispatchEvent(new Event("focus"))
          await fixture.tick(7999)
          assert.equal(fixture.requests.length, 2)
          await fixture.tick(1)
          assert.equal(fixture.requests.length, 3)
          await fixture.respond(2, { revision: 1 })
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "unchanged revisions refresh the snapshot and calendar after one visible minute",
      async (context) => {
        const fixture = await setup(context, true)
        try {
          await fixture.respond(0, snapshot(1))
          await fixture.respondCalendar(0, calendarPage(1, "live"))
          for (let index = 1; index <= 7; index++) {
            await fixture.tick(8000)
            assert.match(fixture.requests[index].url, /revision\?since=1$/)
            await fixture.respond(index, { revision: 1 })
          }
          await fixture.tick(3999)
          assert.equal(fixture.requests.length, 8)
          await fixture.tick(1)
          assert.equal(fixture.requests[8].url, "/api/dashboard?days=3")
          await fixture.respond(8, snapshot(1))
          assert.equal(fixture.current().snapshotVersion, 2)
          assert.equal(fixture.calendarRequests.length, 2)
          await fixture.respondCalendar(1, calendarPage(1, null))
          assert.equal(fixture.current().calendar.events[0].liveStatus, null)
          assert.equal(fixture.current().error, null)
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "unmount aborts the active request and removes polling and wake listeners",
      async (context) => {
        const fixture = await setup(context)
        await fixture.unmount()
        assert.equal(fixture.requests[0].signal.aborted, true)
        await fixture.tick(120000)
        fixture.doc.dispatchEvent(new Event("visibilitychange"))
        fixture.win.dispatchEvent(new Event("focus"))
        await fixture.tick(0)
        assert.equal(fixture.requests.length, 1)
        await fixture.respond(0, snapshot(9))
        assert.equal(fixture.requests.length, 1)
      },
    )
  } finally {
    await vite.close()
  }
})
