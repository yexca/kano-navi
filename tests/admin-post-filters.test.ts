import assert from "node:assert/strict"
import path from "node:path"
import test from "node:test"
import { createElement } from "react"
import { act, create, type ReactTestRenderer } from "react-test-renderer"
import { createServer } from "vite"
import type { AdminPost } from "../src/admin/types.ts"

test("post date filters reach server queries, survive pagination and clear independently", async () => {
  const vite = await createServer({
    configFile: false,
    envDir: false,
    server: { middlewareMode: true, watch: null, ws: false },
    optimizeDeps: { noDiscovery: true, include: [] },
    resolve: { alias: { "@": path.resolve("src") } },
  })
  const previous = {
    window: globalThis.window,
    document: globalThis.document,
    act: (globalThis as any).IS_REACT_ACT_ENVIRONMENT,
  }
  let root: ReactTestRenderer | undefined
  try {
    const { PostsView } = await vite.ssrLoadModule("/src/admin/views/posts.tsx")
    const { AdminContext } = await vite.ssrLoadModule(
      "/src/admin/use-admin-data.ts",
    )
    const { AppSettingsProvider } = await vite.ssrLoadModule(
      "/src/app-settings.tsx",
    )
    await vite.close()
    Object.assign(globalThis, {
      IS_REACT_ACT_ENVIRONMENT: true,
      window: { localStorage: { getItem: () => null, setItem: () => {} } },
      document: {
        documentElement: { classList: { toggle: () => {} } },
        addEventListener: () => {},
        removeEventListener: () => {},
      },
    })
    const post: AdminPost = {
      id: "synthetic-post",
      source: "x",
      accountHandle: "example",
      text: "Synthetic post",
      publishedAt: "2026-01-01T15:00:00Z",
      url: "https://example.invalid/post",
      mediaUrl: null,
      mediaAlt: null,
      media: [],
      mediaStatus: "none",
      llmStatus: "never",
      llmRoute: null,
      llmLastAttemptAt: null,
      llmLastError: null,
      llmReprocessRequested: false,
      llmRetryAt: null,
      llmRetryStopped: false,
      translation: null,
      quote: null,
      quoteHiddenAt: null,
      refreshRequestedAt: null,
      hiddenAt: null,
    }
    const queries: Record<string, string | number>[] = []
    const context = {
      revision: 0,
      fail: (error: unknown) => {
        throw error
      },
      api: {
        sources: async () => undefined,
        translations: async () => ({ counts: {} }),
        posts: async (query: Record<string, string | number>) => {
          queries.push({ ...query })
          return { items: [post], page: query.page, total: 61, totalPages: 3 }
        },
      },
    }
    await act(async () => {
      root = create(
        createElement(
          AppSettingsProvider,
          null,
          createElement(
            AdminContext.Provider,
            { value: context },
            createElement(PostsView, { navigate: () => {} }),
          ),
        ),
      )
    })
    const input = (key: string) => root!.root.findByProps({ "aria-label": key })
    const change = (key: string, value: string) =>
      act(async () => {
        input(key).props.onChange({ target: { value } })
      })
    const latest = () => queries.at(-1)!

    await change("admin.schedule.filter.from", "2026-01-02")
    await change("admin.schedule.filter.to", "2026-01-03")
    assert.equal(latest().from, "2026-01-02")
    assert.equal(latest().to, "2026-01-03")
    assert.equal(latest().page, 1)
    await act(async () => input("admin.pager.next").props.onClick())
    assert.equal(latest().page, 2)
    assert.equal(latest().from, "2026-01-02")
    assert.equal(latest().to, "2026-01-03")
    // A page number and the jump field page the same query; a page past the
    // end goes to the last one.
    const pageButton = (number: number) =>
      root!.root
        .findAllByProps({ className: "adm-pager-page" })
        .find((button) => button.props.children === number)!
    await act(async () => pageButton(3).props.onClick())
    assert.equal(latest().page, 3)
    await act(async () =>
      root!.root
        .findByProps({ inputMode: "numeric" })
        .props.onChange({ target: { value: "99" } }),
    )
    await act(async () =>
      root!.root
        .findByProps({ className: "adm-pager-jump" })
        .props.onSubmit({ preventDefault: () => {} }),
    )
    assert.equal(latest().page, 3)
    assert.equal(latest().from, "2026-01-02")
    // Picking a choice from a filter list narrows the query from page 1.
    await act(async () =>
      root!.root
        .findAllByProps({ "aria-haspopup": "listbox" })[1]
        .props.onClick(),
    )
    await act(async () =>
      root!.root
        .findAllByProps({ role: "option" })
        .find((option) => option.props.id.endsWith("-5"))!
        .props.onClick(),
    )
    assert.equal(latest().llm, "failed")
    assert.equal(latest().page, 1)
    await act(async () => input("admin.pager.next").props.onClick())
    assert.equal(latest().page, 2)
    await change("admin.posts.search", "needle")
    await act(async () =>
      root!.root
        .findByProps({ className: "adm-filters" })
        .props.onSubmit({ preventDefault: () => {} }),
    )
    assert.equal(latest().search, "needle")
    assert.equal(latest().page, 1)
    assert.equal(latest().from, "2026-01-02")
    assert.equal(latest().to, "2026-01-03")
    await change("admin.schedule.filter.to", "")
    assert.equal(latest().from, "2026-01-02")
    assert.equal(latest().to, "")
    await change("admin.schedule.filter.from", "")
    assert.equal(latest().from, "")
    assert.equal(latest().search, "needle")
  } finally {
    if (root) await act(async () => root!.unmount())
    Object.assign(globalThis, {
      window: previous.window,
      document: previous.document,
      IS_REACT_ACT_ENVIRONMENT: previous.act,
    })
    await vite.close()
  }
})
