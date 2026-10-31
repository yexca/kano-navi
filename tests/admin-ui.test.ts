import assert from "node:assert/strict"
import path from "node:path"
import test from "node:test"
import { createElement, useState } from "react"
import { act, create, type ReactTestRenderer } from "react-test-renderer"
import { createServer } from "vite"

const overview = {
  activeRun: null,
  recentRuns: [],
  scheduler: { enabled: false, running: false, tickSeconds: 30 },
  timers: { scheduled: 0, nextRunAt: null },
  sources: {
    x: { skipped: true, lastSuccessAt: null, lastStep: null },
    youtube: { skipped: true, lastSuccessAt: null, lastStep: null },
    bilibili: { skipped: true, lastSuccessAt: null, lastStep: null },
  },
  review: {
    pendingImages: 0,
    suspectedCancellations: 0,
    failedPosts: 0,
    reprocessRequested: 0,
    videoMerges: 0,
  },
  counts: { upcomingEvents: 0, events: 0, posts: 0, videos: 0 },
}

const visitorRegion = {
  enabled: false,
  providerName: "",
  providerUrl: "",
  mirrorUrl: "",
}

function syncSettings() {
  return {
    settings: {
      xBootstrapDays: 7,
      xDiscoveryLimit: 50,
      xMaxStatusRequests: 12,
      xRefreshKnown: 1,
      youtubeMaxDetailRequests: 12,
      fetchMaxPages: 4,
      fetchTimeoutMs: 7000,
      fetchRequestDelayMs: 0,
      translationScanLimit: 20,
      translationLookbackDays: 30,
      llmModelAttempts: 3,
    },
    fields: [
      ["xBootstrapDays", 1, 30],
      ["xDiscoveryLimit", 1, 200],
      ["xMaxStatusRequests", 0, 50],
      ["xRefreshKnown", 0, 5],
      ["youtubeMaxDetailRequests", 1, 50],
      ["fetchMaxPages", 1, 20],
      ["fetchTimeoutMs", 1000, 60000],
      ["fetchRequestDelayMs", 0, 10000],
      ["translationScanLimit", 1, 200],
      ["translationLookbackDays", 1, 3650],
      ["llmModelAttempts", 1, 10],
    ].map(([field, min, max]) => ({ field, default: min, min, max })),
  }
}

function installBrowser(hash: string, pathname = "/admin") {
  const previous = {
    window: globalThis.window,
    document: globalThis.document,
    fetch: globalThis.fetch,
    act: (globalThis as any).IS_REACT_ACT_ENVIRONMENT,
  }
  const events = new EventTarget()
  const location = { hash, pathname }
  const storage = new Map<string, string>()
  const window = Object.assign(events, {
    location,
    history: {
      pushState: (_state: unknown, _title: string, value: string) => {
        const hashAt = value.indexOf("#")
        location.hash = hashAt < 0 ? "" : value.slice(hashAt)
      },
    },
    localStorage: {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => storage.set(key, value),
      removeItem: (key: string) => storage.delete(key),
    },
    matchMedia: () => ({
      matches: false,
      addEventListener() {},
      removeEventListener() {},
    }),
    scrollTo() {},
    setTimeout,
    clearTimeout,
  })
  const document = Object.assign(new EventTarget(), {
    visibilityState: "visible",
    title: "",
    documentElement: {
      lang: "",
      classList: { toggle() {} },
    },
    head: { append() {} },
    querySelector: () => null,
    createElement: () => ({
      setAttribute() {},
      getAttribute: () => null,
      remove() {},
    }),
  })
  Object.assign(globalThis, {
    window,
    document,
    IS_REACT_ACT_ENVIRONMENT: true,
  })
  return { previous, window, document }
}

function restoreBrowser(
  previous: ReturnType<typeof installBrowser>["previous"],
) {
  Object.assign(globalThis, {
    window: previous.window,
    document: previous.document,
    fetch: previous.fetch,
    IS_REACT_ACT_ENVIRONMENT: previous.act,
  })
}

function textOf(node: any): string {
  if (typeof node === "string" || typeof node === "number") return String(node)
  return (node?.children || []).map(textOf).join("")
}

test("the real admin Site route loads and saves visitor-region settings", async () => {
  const vite = await createServer({
    configFile: false,
    envDir: false,
    server: { middlewareMode: true, watch: null, ws: false },
    optimizeDeps: { noDiscovery: true, include: [] },
    resolve: { alias: { "@": path.resolve("src") } },
  })
  const browser = installBrowser("#site")
  const requests: { path: string; method: string; body?: any }[] = []
  const saved = {
    enabled: true,
    providerName: "Synthetic provider",
    providerUrl: "https://provider.example.test",
    mirrorUrl: "https://mirror.example.test",
  }
  globalThis.fetch = async (input, options: RequestInit = {}) => {
    const url = String(input)
    const path = url.replace(/^\/api\/admin/, "")
    const method = options.method || "GET"
    const body = options.body ? JSON.parse(String(options.body)) : undefined
    requests.push({ path, method, body })
    const payload =
      method === "PUT" && path === "/settings/visitor-region"
        ? body
        : path === "/session"
          ? {
              authenticated: true,
              mode: "development",
              requiresPassword: false,
            }
          : path === "/overview"
            ? overview
            : path === "/settings/visitor-region"
              ? visitorRegion
              : {}
    return new Response(JSON.stringify(payload), {
      status: 200,
      headers: { "content-type": "application/json" },
    })
  }
  let root: ReactTestRenderer | undefined
  try {
    const [{ AdminApp }, { AppSettingsProvider, PageMessagesProvider }, copy] =
      await Promise.all([
        vite.ssrLoadModule("/src/admin/index.tsx"),
        vite.ssrLoadModule("/src/app-settings.tsx"),
        vite.ssrLoadModule("/src/admin-i18n.ts"),
      ])
    await act(async () => {
      root = create(
        createElement(
          AppSettingsProvider,
          null,
          createElement(
            PageMessagesProvider,
            { messages: copy.adminMessages },
            createElement(AdminApp),
          ),
        ),
      )
      await new Promise((resolve) => setImmediate(resolve))
    })
    assert.match(textOf(root!.toJSON()), /Mainland China site suggestion/)
    const inputs = root!.root.findAllByType("input")
    const name = inputs.find(
      (input) => input.props.placeholder === "e.g. a provider",
    )
    const providerUrl = inputs.find(
      (input) => input.props.placeholder === "https://",
    )
    const mirrorUrl = inputs.filter(
      (input) => input.props.placeholder === "https://",
    )[1]
    assert.ok(name)
    assert.ok(providerUrl)
    assert.ok(mirrorUrl)
    await act(async () =>
      name!.props.onChange({ target: { value: "New provider" } }),
    )
    await act(async () =>
      providerUrl!.props.onChange({
        target: { value: "https://provider.example.test" },
      }),
    )
    await act(async () =>
      mirrorUrl!.props.onChange({
        target: { value: "https://mirror.example.test" },
      }),
    )
    const switchInput = root!.root.findByProps({ role: "switch" })
    await act(async () =>
      switchInput.props.onChange({ target: { checked: true } }),
    )
    const form = root!.root.findByType("form")
    await act(async () => form.props.onSubmit({ preventDefault() {} }))
    const update = requests.find(
      (request) =>
        request.method === "PUT" && request.path === "/settings/visitor-region",
    )
    assert.deepEqual(update?.body, {
      ...saved,
      providerName: "New provider",
    })
  } finally {
    await act(async () => root?.unmount())
    restoreBrowser(browser.previous)
    await vite.close()
  }
})

test("admin sync-setting drafts survive a save in the other Sources column", async () => {
  const vite = await createServer({
    configFile: false,
    envDir: false,
    server: { middlewareMode: true, watch: null, ws: false },
    optimizeDeps: { noDiscovery: true, include: [] },
    resolve: { alias: { "@": path.resolve("src") } },
  })
  const browser = installBrowser("#sources")
  let root: ReactTestRenderer | undefined
  try {
    const [{ AdminContext }, { AppSettingsProvider }, { SyncSettingsForm }] =
      await Promise.all([
        vite.ssrLoadModule("/src/admin/use-admin-data.ts"),
        vite.ssrLoadModule("/src/app-settings.tsx"),
        vite.ssrLoadModule("/src/admin/views/sync-settings.tsx"),
      ])
    let current = syncSettings()
    const api = {
      updateSyncSettings: async (body: Record<string, number>) => {
        const settings = { ...current.settings }
        for (const [field, value] of Object.entries(body))
          settings[field as keyof typeof settings] = value
        current = { ...current, settings }
        return current
      },
    }
    const admin = {
      api,
      act: async (work: () => Promise<unknown>) => work(),
      revision: 0,
      fail() {},
    }
    function Harness() {
      const [data, setData] = useState(current)
      return createElement(
        AdminContext.Provider,
        { value: admin as any },
        createElement(
          "div",
          null,
          createElement(SyncSettingsForm, {
            group: "x",
            section: "timed",
            data,
            onSaved: setData,
          }),
          createElement(SyncSettingsForm, {
            group: "x",
            section: "manual",
            data,
            onSaved: setData,
            rows: true,
          }),
        ),
      )
    }
    await act(async () => {
      root = create(
        createElement(AppSettingsProvider, null, createElement(Harness)),
      )
      await new Promise((resolve) => setImmediate(resolve))
    })
    const inputs = () => root!.root.findAllByType("input")
    const timed = inputs()[0]
    const manual = inputs()[4]
    assert.equal(manual.props.value, "4")
    assert.equal(timed.props.value, "12")
    await act(async () => manual.props.onChange({ target: { value: "9" } }))
    await act(async () => timed.props.onChange({ target: { value: "20" } }))
    const forms = root!.root.findAllByType("form")
    await act(async () => forms[0].props.onSubmit({ preventDefault() {} }))
    assert.equal(manual.props.value, "9")
  } finally {
    await act(async () => root?.unmount())
    restoreBrowser(browser.previous)
    await vite.close()
  }
})

test("the public visitor-region toast follows the persisted CN response", async () => {
  const vite = await createServer({
    configFile: false,
    envDir: false,
    server: { middlewareMode: true, watch: null, ws: false },
    optimizeDeps: { noDiscovery: true, include: [] },
    resolve: { alias: { "@": path.resolve("src") } },
  })
  const browser = installBrowser("", "/")
  globalThis.fetch = async () =>
    new Response(
      JSON.stringify({
        enabled: true,
        mainlandChina: true,
        providerName: "Synthetic provider",
        providerUrl: "https://provider.example.test",
        mirrorUrl: "https://mirror.example.test",
      }),
      { status: 200, headers: { "content-type": "application/json" } },
    )
  let root: ReactTestRenderer | undefined
  try {
    const [{ VisitorRegionToast }, { AppSettingsProvider }] = await Promise.all(
      [
        vite.ssrLoadModule("/src/components/visitor-region-toast.tsx"),
        vite.ssrLoadModule("/src/app-settings.tsx"),
      ],
    )
    await act(async () => {
      root = create(
        createElement(
          AppSettingsProvider,
          null,
          createElement(VisitorRegionToast),
        ),
      )
      await new Promise((resolve) => setImmediate(resolve))
    })
    const links = root!.root.findAllByType("a")
    assert.deepEqual(
      links.map((link) => link.props.href),
      ["https://provider.example.test", "https://mirror.example.test"],
    )
    assert.match(textOf(root!.toJSON()), /Synthetic provider/)
    const dismiss = root!.root.findAllByType("button")[0]
    await act(async () => dismiss.props.onClick())
    assert.equal(root!.root.findAllByType("aside").length, 0)
  } finally {
    await act(async () => root?.unmount())
    restoreBrowser(browser.previous)
    await vite.close()
  }
})
