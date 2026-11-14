import assert from "node:assert/strict"
import path from "node:path"
import test from "node:test"
import { act, createElement, useState } from "react"
import { createServer } from "vite"
import { browser, click, enter, render, submit, type Rendered } from "./dom.ts"

const overview = {
  activeRun: null,
  recentRuns: [],
  scheduler: { enabled: false, running: false, tickSeconds: 30 },
  timers: { scheduled: 0, nextRunAt: null },
  sources: {
    x: { skipped: true, lastSuccessAt: null, lastStep: null },
    x_related: { skipped: true, lastSuccessAt: null, lastStep: null },
    youtube: { skipped: true, lastSuccessAt: null, lastStep: null },
    bilibili: { skipped: true, lastSuccessAt: null, lastStep: null },
    bilibili_dynamics: { skipped: true, lastSuccessAt: null, lastStep: null },
    shop: { skipped: true, lastSuccessAt: null, lastStep: null },
  },
  review: {
    pendingImages: 0,
    suspectedCancellations: 0,
    uncertainEvents: 0,
    failedPosts: 0,
    failedExtractions: 0,
    failedTranslations: 0,
    videoMerges: 0,
  },
  backlog: {
    extraction: 0,
    postRefresh: 0,
    postTranslation: 0,
    videoRefresh: 0,
    videoTranslation: 0,
  },
  counts: { upcomingEvents: 0, events: 0, posts: 0, videos: 0 },
}

const visitorRegion = {
  enabled: false,
  providerName: "",
  providerUrl: "",
  mirrorUrl: "",
}

const variantStatus = {
  checkedAt: "2026-03-02T03:00:00.000Z",
  groups: [
    { group: "lineblog", originals: 4, ready: 4, unhashed: 0, widths: [] },
    { group: "works", originals: 2, ready: 0, unhashed: 2, widths: [] },
    { group: "journey", originals: 3, ready: 1, unhashed: 2, widths: [] },
    { group: "media", originals: 0, ready: 0, unhashed: 0, widths: [] },
  ],
}

const worksCovers = {
  total: 3,
  covered: 1,
  due: 1,
  waiting: 0,
  uncovered: 1,
  skipped: false,
  checkedAt: "2026-03-02T03:00:00.000Z",
  uploads: [
    {
      id: "2026-01-02-abcdef",
      date: "2026-01-02",
      state: "due",
      videos: [
        {
          platform: "niconico",
          id: "sm2",
          members: false,
          state: "gone",
          code: "niconico_not_found",
          retryAt: null,
        },
        {
          platform: "bilibili",
          id: "BV1BBBBBBBBB",
          members: false,
          state: "due",
          code: null,
          retryAt: null,
        },
      ],
    },
    {
      id: "2026-01-01-abcdef",
      date: "2026-01-01",
      state: "uncovered",
      videos: [
        {
          platform: "niconico",
          id: "sm1",
          members: false,
          state: "gone",
          code: "niconico_not_found",
          retryAt: null,
        },
      ],
    },
  ],
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
      extractionLookbackDays: 30,
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
      ["extractionLookbackDays", 0, 3650],
      ["translationScanLimit", 1, 200],
      ["translationLookbackDays", 1, 3650],
      ["llmModelAttempts", 1, 10],
    ].map(([field, min, max]) => ({ field, default: min, min, max })),
  }
}

function installBrowser(hash: string, pathname = "/admin") {
  const previous = {
    fetch: globalThis.fetch,
    act: (globalThis as any).IS_REACT_ACT_ENVIRONMENT,
  }
  browser.happyDOM.setURL(new URL(pathname + hash, "https://example.test").href)
  browser.localStorage.clear()
  ;(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true
  return { previous }
}

function restoreBrowser(
  previous: ReturnType<typeof installBrowser>["previous"],
) {
  globalThis.fetch = previous.fetch
  ;(globalThis as any).IS_REACT_ACT_ENVIRONMENT = previous.act
}

test("the real admin Site route saves visitor-region settings, makes image copies and fills Works covers", async () => {
  const vite = await createServer({
    configFile: false,
    envDir: false,
    server: { middlewareMode: true, watch: null, ws: false },
    optimizeDeps: { noDiscovery: true, include: [] },
    resolve: { alias: { "@": path.resolve("src") } },
  })
  const installed = installBrowser("#site")
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
              requiresCode: false,
            }
          : path === "/overview"
            ? overview
            : path === "/settings/visitor-region"
              ? visitorRegion
              : path.startsWith("/media/variants")
                ? variantStatus
                : path === "/media/works-covers"
                  ? worksCovers
                  : method === "POST" && path === "/runs"
                    ? { run: { id: "synthetic-run" } }
                    : {}
    return new Response(JSON.stringify(payload), {
      status: 200,
      headers: { "content-type": "application/json" },
    })
  }
  let root: Rendered | undefined
  try {
    const [{ AdminApp }, { AppSettingsProvider, PageMessagesProvider }, copy] =
      await Promise.all([
        vite.ssrLoadModule("/src/admin/index.tsx"),
        vite.ssrLoadModule("/src/app-settings.tsx"),
        vite.ssrLoadModule("/src/admin-i18n.ts"),
      ])
    await act(async () => {
      root = render(
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
    assert.match(root!.container.textContent, /Mainland China site suggestion/)
    const name = root!.container.querySelector(
      'input[placeholder="e.g. a provider"]',
    )
    const [providerUrl, mirrorUrl] = root!.container.querySelectorAll(
      'input[placeholder="https://"]',
    )
    assert.ok(name)
    assert.ok(providerUrl)
    assert.ok(mirrorUrl)
    await enter(name, "New provider")
    await enter(providerUrl, "https://provider.example.test")
    await enter(mirrorUrl, "https://mirror.example.test")
    const switchInput = root!.container.querySelector('[role="switch"]')
    assert.equal((switchInput as HTMLInputElement | null)?.checked, false)
    await click(switchInput)
    await submit(root!.container.querySelector("form"))
    const update = requests.find(
      (request) =>
        request.method === "PUT" && request.path === "/settings/visitor-region",
    )
    assert.deepEqual(update?.body, {
      ...saved,
      providerName: "New provider",
    })
    // The copies card shows each group's ready images; Check again asks for
    // a new listing and Make missing copies queues the variants run.
    const text = root!.container.textContent
    assert.match(text, /LINE BLOG archive4\/ 4 imagesAll copies made/)
    assert.match(text, /Works covers0\/ 2 images2 without copies/)
    assert.match(text, /Journey images1\/ 3 images2 without copies/)
    const button = (label: string, panel?: string) =>
      [
        ...(panel
          ? [...root!.container.querySelectorAll("section.adm-panel")].find(
              (section) => section.querySelector("h2")?.textContent === panel,
            )!
          : root!.container
        ).querySelectorAll("button"),
      ].find((element) => element.textContent === label)
    await click(button("Check again", "Image copies"))
    assert.ok(
      requests.some(
        (request) =>
          request.method === "GET" &&
          request.path === "/media/variants?refresh=1",
      ),
    )
    await click(button("Make missing copies"))
    const run = requests.find(
      (request) => request.method === "POST" && request.path === "/runs",
    )
    assert.deepEqual(run?.body, { kind: "variants" })
    // The covers card counts the uploads, lists those without a cover with
    // their videos' states, and Fill missing covers queues a media step.
    assert.match(text, /Covered1\/ 3 uploads/)
    assert.match(text, /Every video gone1\/ 3 uploads/)
    assert.match(text, /Uploads without a cover \(2\)/)
    assert.match(text, /2026-01-022026-01-02-abcdefDue/)
    assert.match(text, /niconicosm2Goneniconico_not_found/)
    await click(button("Fill missing covers"))
    const runs = requests.filter(
      (request) => request.method === "POST" && request.path === "/runs",
    )
    assert.deepEqual(runs.at(-1)?.body, { kind: "workflow", steps: ["media"] })
  } finally {
    await act(async () => root?.unmount())
    restoreBrowser(installed.previous)
    await vite.close()
  }
})

test("the admin login asks for the one-time code only when it is configured, after a failed session read too", async () => {
  const vite = await createServer({
    configFile: false,
    envDir: false,
    server: { middlewareMode: true, watch: null, ws: false },
    optimizeDeps: { noDiscovery: true, include: [] },
    resolve: { alias: { "@": path.resolve("src") } },
  })
  const installed = installBrowser("")
  const logins: unknown[] = []
  let requiresCode = true
  let sessionFailures = 1
  globalThis.fetch = async (input, options: RequestInit = {}) => {
    const route = String(input).replace(/^\/api\/admin/, "")
    if (
      route === "/session" &&
      options.method !== "POST" &&
      sessionFailures > 0
    ) {
      sessionFailures--
      return new Response(
        JSON.stringify({ error: "unavailable", message: "unavailable" }),
        { status: 503, headers: { "content-type": "application/json" } },
      )
    }
    const session = {
      authenticated: false,
      mode: "production",
      requiresPassword: true,
      requiresCode,
    }
    if (route === "/session" && options.method === "POST") {
      logins.push(JSON.parse(String(options.body)))
      return new Response(
        JSON.stringify({
          error: "invalid_credentials",
          message: "invalid_credentials",
        }),
        { status: 401, headers: { "content-type": "application/json" } },
      )
    }
    return new Response(JSON.stringify(route === "/session" ? session : {}), {
      status: 200,
      headers: { "content-type": "application/json" },
    })
  }
  let root: Rendered | undefined
  try {
    const [{ AdminApp }, { AppSettingsProvider, PageMessagesProvider }, copy] =
      await Promise.all([
        vite.ssrLoadModule("/src/admin/index.tsx"),
        vite.ssrLoadModule("/src/app-settings.tsx"),
        vite.ssrLoadModule("/src/admin-i18n.ts"),
      ])
    const mount = async () => {
      await act(async () => {
        root = render(
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
    }
    await mount()
    // The first read failed, so the requirements are unknown: no password
    // form, a reason and a retry, which finds the code required.
    assert.equal(root!.container.querySelector('input[type="password"]'), null)
    assert.match(
      root!.container.textContent,
      /unavailable on this server\.Retry$/,
    )
    await act(async () => {
      await submit(root!.container.querySelector("form"))
      await new Promise((resolve) => setImmediate(resolve))
    })
    const code = root!.container.querySelector<HTMLInputElement>(
      'input[autocomplete="one-time-code"]',
    )
    assert.ok(code)
    assert.equal(code.inputMode, "numeric")
    await enter(
      root!.container.querySelector('input[type="password"]')!,
      "synthetic-password",
    )
    await enter(code, "123456")
    await act(async () => {
      await submit(root!.container.querySelector("form"))
      await new Promise((resolve) => setImmediate(resolve))
    })
    assert.deepEqual(logins, [
      { password: "synthetic-password", code: "123456" },
    ])
    // A refused login names both factors and clears the spent code.
    assert.match(root!.container.textContent, /Wrong password or code\./)
    assert.equal(code.value, "")
    await act(async () => root?.unmount())

    requiresCode = false
    await mount()
    assert.equal(
      root!.container.querySelector('input[autocomplete="one-time-code"]'),
      null,
    )
  } finally {
    await act(async () => root?.unmount())
    restoreBrowser(installed.previous)
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
  const installed = installBrowser("#sources")
  let root: Rendered | undefined
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
      root = render(
        createElement(AppSettingsProvider, null, createElement(Harness)),
      )
      await new Promise((resolve) => setImmediate(resolve))
    })
    const inputs = () => root!.container.querySelectorAll("input")
    const timed = () => inputs()[0]
    const manual = () => inputs()[4]
    assert.equal(manual().value, "4")
    assert.equal(timed().value, "12")
    await enter(manual(), "9")
    await enter(timed(), "20")
    await submit(root!.container.querySelector("form"))
    assert.equal(manual().value, "9")
  } finally {
    await act(async () => root?.unmount())
    restoreBrowser(installed.previous)
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
  const installed = installBrowser("", "/")
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
  let root: Rendered | undefined
  try {
    const [{ VisitorRegionToast }, { AppSettingsProvider }] = await Promise.all(
      [
        vite.ssrLoadModule("/src/components/visitor-region-toast.tsx"),
        vite.ssrLoadModule("/src/app-settings.tsx"),
      ],
    )
    await act(async () => {
      root = render(
        createElement(
          AppSettingsProvider,
          null,
          createElement(VisitorRegionToast),
        ),
      )
      await new Promise((resolve) => setImmediate(resolve))
    })
    const links = [...root!.container.querySelectorAll("a")]
    assert.deepEqual(
      links.map((link) => link.getAttribute("href")),
      ["https://provider.example.test", "https://mirror.example.test"],
    )
    assert.match(root!.container.textContent, /Synthetic provider/)
    await click(root!.container.querySelector("button"))
    assert.equal(root!.container.querySelectorAll("aside").length, 0)
  } finally {
    await act(async () => root?.unmount())
    restoreBrowser(installed.previous)
    await vite.close()
  }
})

test("the overview queue lists timers in order, starts one now or next and links to the requests waiting for a run", async () => {
  const vite = await createServer({
    configFile: false,
    envDir: false,
    server: { middlewareMode: true, watch: null, ws: false },
    optimizeDeps: { noDiscovery: true, include: [] },
    resolve: { alias: { "@": path.resolve("src") } },
  })
  const installed = installBrowser("")
  let root: Rendered | undefined
  try {
    const [
      { AdminContext },
      { AppSettingsProvider, PageMessagesProvider },
      { OverviewView },
      copy,
    ] = await Promise.all([
      vite.ssrLoadModule("/src/admin/use-admin-data.ts"),
      vite.ssrLoadModule("/src/app-settings.tsx"),
      vite.ssrLoadModule("/src/admin/views/overview.tsx"),
      vite.ssrLoadModule("/src/admin-i18n.ts"),
    ])
    const started: string[] = []
    const visits: string[] = []
    const queued = {
      ...overview,
      scheduler: { enabled: true, running: true, tickSeconds: 30 },
      backlog: { ...overview.backlog, extraction: 2, videoRefresh: 1 },
      queue: [
        {
          kind: "source",
          source: "x",
          nextRunAt: "2026-03-02T03:00:00.000Z",
          due: true,
        },
        {
          kind: "live-probe",
          nextRunAt: "2026-03-02T03:10:00.000Z",
          due: false,
        },
        {
          kind: "source",
          source: "youtube",
          nextRunAt: "2026-03-02T04:00:00.000Z",
          due: false,
        },
      ],
    }
    const admin = {
      overview: queued,
      api: {
        startSourceSchedule: async (source: string) => {
          started.push(source)
          return {}
        },
      },
      act: async (work: () => Promise<unknown>) => work(),
    }
    function Harness({ busy }: { busy: boolean }) {
      const value = {
        ...admin,
        overview: {
          ...queued,
          activeRun: busy
            ? {
                id: "synthetic-run",
                kind: "workflow",
                status: "running",
                createdAt: "2026-03-02T02:58:00.000Z",
                startedAt: "2026-03-02T02:58:00.000Z",
                steps: ["bilibili"],
                progress: { bilibili: "running" },
                sourceSchedule: "bilibili",
              }
            : null,
        },
      }
      return createElement(
        AdminContext.Provider,
        { value: value as any },
        createElement(OverviewView, {
          navigate: (view: string, tab = "") => visits.push(`${view}/${tab}`),
        }),
      )
    }
    const mount = async (busy: boolean) => {
      await act(async () => root?.unmount())
      await act(async () => {
        root = render(
          createElement(
            AppSettingsProvider,
            null,
            createElement(
              PageMessagesProvider,
              { messages: copy.adminMessages },
              createElement(Harness, { busy }),
            ),
          ),
        )
      })
      return [...root!.container.querySelectorAll(".adm-queue > li")]
    }
    // A due timer waits with no button; the live probe has none either.
    let rows = await mount(false)
    assert.deepEqual(
      rows.map((row) => row.querySelector("strong")?.textContent),
      ["X", "Live status", "YouTube"],
    )
    assert.match(rows[0].textContent, /Starting shortly.*Due/)
    assert.equal(rows[0].querySelector("button"), null)
    assert.equal(rows[1].querySelector("button"), null)
    await click(rows[2].querySelector("button"))
    assert.match(rows[2].textContent, /Start now/)
    // While a run holds the queue the button queues the timer next.
    rows = await mount(true)
    assert.match(rows[0].textContent, /Waiting for the current run/)
    assert.match(rows[2].textContent, /Run next/)
    await click(rows[2].querySelector("button"))
    assert.deepEqual(started, ["youtube", "youtube"])
    // Only the kinds with open requests list, each with the run that takes
    // it, and each opens the list of what waits.
    const backlog = [
      ...root!.container.querySelectorAll(".adm-backlog-row"),
    ] as HTMLElement[]
    assert.deepEqual(
      backlog.map((row) => row.textContent),
      [
        "2 posts to extract againWith the next schedule scan",
        "1 video to read againWith the next YouTube run",
      ],
    )
    await click(backlog[0])
    await click(backlog[1])
    assert.deepEqual(visits, ["posts/queued-extraction", "videos/queued"])
  } finally {
    await act(async () => root?.unmount())
    restoreBrowser(installed.previous)
    await vite.close()
  }
})
