import assert from "node:assert/strict"
import path from "node:path"
import test, { type TestContext } from "node:test"
import { createElement } from "react"
import { act, create, type ReactTestRenderer } from "react-test-renderer"
import { createServer } from "vite"

type ProbeProps = { request: unknown; revision?: number | null }

test("month archives page by revision, follow the board and cancel on close", async (t) => {
  // Load the actual hook with its normal frontend imports, without the runtime
  // Vite plugin, .env loading, a listener or the operator database.
  const vite = await createServer({
    configFile: false,
    envDir: false,
    server: { middlewareMode: true, watch: null },
    optimizeDeps: { noDiscovery: true, include: [] },
    resolve: { alias: { "@": path.resolve("src") } },
  })
  try {
    const { useArchive } = await vite.ssrLoadModule(
      "/src/dashboard/use-dashboard.ts",
    )
    await vite.close()
    const setup = async (context: TestContext, initial: ProbeProps) => {
      const previous = (globalThis as any).IS_REACT_ACT_ENVIRONMENT
      ;(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true
      const requests: {
        url: string
        signal: AbortSignal
        resolve: (response: Response) => void
      }[] = []
      context.mock.method(
        globalThis,
        "fetch",
        (url: RequestInfo | URL, options: { signal: AbortSignal }) => {
          return new Promise<Response>((resolve) =>
            requests.push({
              url: String(url),
              signal: options.signal,
              resolve,
            }),
          )
        },
      )
      let value: ReturnType<typeof useArchive>
      let root: ReactTestRenderer
      function Probe({ request, revision }: ProbeProps) {
        value = useArchive(request, revision)
        return null
      }
      await act(async () => {
        root = create(createElement(Probe, initial))
      })
      const page = (
        page: number,
        revision: number,
        items: string[],
        hasNext = false,
      ) => ({
        items: items.map((id) => ({ id })),
        adjacent: { previous: null, next: null },
        total: 9,
        page,
        hasNext,
        revision,
      })
      const respond = async (index: number, payload: any) =>
        act(async () =>
          requests[index].resolve({
            ok: true,
            status: 200,
            json: async () => payload,
          } as Response),
        )
      const rerender = async (props: ProbeProps) =>
        act(async () => root.update(createElement(Probe, props)))
      const unmount = async () => {
        await act(async () => root.unmount())
        ;(globalThis as any).IS_REACT_ACT_ENVIRONMENT = previous
      }
      const ids = () =>
        value.archive?.items.map((item: { id: string }) => item.id).join(",")
      return {
        requests,
        page,
        respond,
        rerender,
        unmount,
        ids,
        current: () => value,
      }
    }
    const october = new Date(2026, 9, 1)

    await t.test(
      "a later page from another revision rereads the loaded pages instead of appending",
      async (context) => {
        const request = {
          kind: "posts",
          monthStart: october,
          account: "kano_2525",
        }
        const fixture = await setup(context, { request, revision: 1 })
        try {
          assert.equal(
            fixture.requests[0].url,
            "/api/posts?from=2026-10-01&to=2026-10-31&page=1&account=kano_2525",
          )
          assert.equal(fixture.current().isLoading, true)
          await fixture.respond(0, fixture.page(1, 1, ["a"], true))
          assert.equal(fixture.ids(), "a")
          await act(async () => void fixture.current().loadMore())
          assert.match(fixture.requests[1].url, /&page=2&/)
          await fixture.respond(1, fixture.page(2, 2, ["b"]))
          // Page two moved under a write: reread page one of revision 2.
          assert.match(fixture.requests[2].url, /&page=1&/)
          await fixture.respond(2, fixture.page(1, 2, ["x"], true))
          assert.equal(fixture.ids(), "x")
          await act(async () => void fixture.current().loadMore())
          await fixture.respond(3, fixture.page(2, 2, ["y"]))
          assert.equal(fixture.ids(), "x,y")
          assert.equal(fixture.current().archive.hasNext, false)
          assert.equal(fixture.current().isLoading, false)
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "the schedule reads its Monday-first grid and follows the board revision",
      async (context) => {
        const request = { kind: "schedule", monthStart: october }
        const fixture = await setup(context, { request, revision: 4 })
        try {
          assert.equal(
            fixture.requests[0].url,
            "/api/calendar?from=2026-09-28&to=2026-11-01&page=1",
          )
          await fixture.respond(0, fixture.page(1, 4, ["a"], true))
          await act(async () => void fixture.current().loadMore())
          await fixture.respond(1, fixture.page(2, 4, ["b"]))
          assert.equal(fixture.ids(), "a,b")
          // An unchanged revision reads nothing; a new one rereads both pages.
          await fixture.rerender({ request, revision: 4 })
          assert.equal(fixture.requests.length, 2)
          await fixture.rerender({ request, revision: 5 })
          assert.match(fixture.requests[2].url, /&page=1$/)
          assert.equal(fixture.ids(), "a,b")
          await fixture.respond(2, fixture.page(1, 5, ["c"], true))
          assert.match(fixture.requests[3].url, /&page=2$/)
          await fixture.respond(3, fixture.page(2, 5, ["d"]))
          assert.equal(fixture.ids(), "c,d")
          assert.equal(fixture.current().archive.revision, 5)
        } finally {
          await fixture.unmount()
        }
      },
    )

    await t.test(
      "changing the month or closing cancels the read and ignores late pages",
      async (context) => {
        const fixture = await setup(context, {
          request: { kind: "videos", monthStart: october },
          revision: 1,
        })
        try {
          assert.equal(
            fixture.requests[0].url,
            "/api/videos?from=2026-10-01&to=2026-10-31&page=1",
          )
          const september = { kind: "videos", monthStart: new Date(2026, 8, 1) }
          await fixture.rerender({ request: september, revision: 1 })
          assert.equal(fixture.requests[0].signal.aborted, true)
          assert.equal(
            fixture.requests[1].url,
            "/api/videos?from=2026-09-01&to=2026-09-30&page=1",
          )
          await fixture.respond(0, fixture.page(1, 1, ["late"]))
          assert.equal(fixture.current().archive, null)
          await fixture.respond(1, fixture.page(1, 1, ["sept"]))
          assert.equal(fixture.ids(), "sept")
          await fixture.rerender({ request: null, revision: 2 })
          assert.equal(fixture.current().archive, null)
          assert.equal(fixture.current().isLoading, false)
          assert.equal(fixture.requests.length, 2)
        } finally {
          await fixture.unmount()
        }
      },
    )
  } finally {
    await vite.close()
  }
})
