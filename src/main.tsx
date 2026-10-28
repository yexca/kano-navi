import { createRoot, type Root } from "react-dom/client"
import { flushSync } from "react-dom"
import {
  Component,
  useEffect,
  useState,
  type ComponentType,
  type ReactNode,
} from "react"

import {
  AppSettingsProvider,
  PageMessagesProvider,
  useAppSettings,
} from "@/app-settings"
import type { PageMessages } from "@/i18n"
import { DashboardApp } from "@/dashboard/dashboard-app"
import { historyMove, inPlaceUrl, pagePath } from "@/lib/page-path"
import "./index.css"

declare global {
  var __kanoReactRoot: Root | undefined
  var __kanoRootElement: HTMLElement | null | undefined
}

function withMessages(Page: ComponentType, messages: PageMessages) {
  return function PageWithMessages() {
    return (
      <PageMessagesProvider messages={messages}>
        <Page />
      </PageMessagesProvider>
    )
  }
}

// Keep in step with spaPages in backend-go/internal/httpapi/static.go.
const routes: Record<string, () => Promise<ComponentType>> = {
  "/": async () => DashboardApp,
  "/about": async () => {
    const [page, copy] = await Promise.all([
      import("@/about/about-app"),
      import("@/about/about-i18n"),
    ])
    return withMessages(page.AboutApp, copy.aboutMessages)
  },
  "/admin": async () => {
    const [page, copy] = await Promise.all([
      import("@/admin"),
      import("@/admin-i18n"),
    ])
    return withMessages(page.AdminApp, copy.adminMessages)
  },
  "/history": async () => {
    const [page, copy] = await Promise.all([
      import("@/history/history-app"),
      import("@/history/history-i18n"),
    ])
    return withMessages(page.HistoryApp, copy.historyMessages)
  },
}

/** Public pages open each other in place; /admin always loads on its own. */
const publicPages = ["/", "/history", "/about"]

const loadedPages = new Map<string, ComponentType>([["/", DashboardApp]])
const pendingPages = new Map<string, Promise<ComponentType>>()

/** Each page's code loads once; a failed load may be tried again. */
function loadPage(path: string) {
  let pending = pendingPages.get(path)
  if (!pending) {
    pending = routes[path]().then(
      (Page) => {
        loadedPages.set(path, Page)
        return Page
      },
      (error: unknown) => {
        pendingPages.delete(path)
        throw error
      },
    )
    pendingPages.set(path, pending)
  }
  return pending
}

function PageStatus({ failed = false }: { failed?: boolean }) {
  const { t } = useAppSettings()
  return (
    <main className="min-h-screen grid place-items-center p-6">
      <div
        className={`rounded-2xl border bg-card p-8 text-center shadow-sm${failed ? "" : " page-status-pending"}`}
        role={failed ? "alert" : "status"}
      >
        <p>{t(failed ? "page.error" : "page.loading")}</p>
        {failed && (
          <button
            className="mt-4 rounded-lg border px-4 py-2 focus-visible:outline-ring"
            onClick={() => window.location.reload()}
          >
            {t("page.retry")}
          </button>
        )}
      </div>
    </main>
  )
}

/** Paths outside the routes; the server answers them with a 404 status. */
function NotFoundPage() {
  const { t } = useAppSettings()
  return (
    <main className="min-h-screen grid place-items-center p-6">
      <div className="rounded-2xl border bg-card p-8 text-center shadow-sm">
        <h1>{t("page.notFound")}</h1>
        <a
          className="mt-4 inline-block rounded-lg border px-4 py-2 focus-visible:outline-ring"
          href="/"
        >
          {t("page.home")}
        </a>
      </div>
    </main>
  )
}

class PageBoundary extends Component<
  { children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false }
  static getDerivedStateFromError() {
    return { failed: true }
  }
  render() {
    return this.state.failed ? <PageStatus failed /> : this.props.children
  }
}

const reducedMotion = window.matchMedia?.("(prefers-reduced-motion: reduce)")

function newEntry() {
  return `${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`
}

/**
 * A reloaded page restores its scroll once its content is tall enough,
 * giving up after a moment or as soon as the reader scrolls themselves.
 */
function settleScroll(top: number) {
  const deadline = performance.now() + 1500
  let timer = 0
  const stop = () => {
    window.clearTimeout(timer)
    for (const type of ["wheel", "touchstart", "keydown"])
      window.removeEventListener(type, stop)
  }
  const step = () => {
    window.scrollTo({ top, behavior: "instant" })
    if (Math.abs(window.scrollY - top) > 1 && performance.now() < deadline)
      timer = window.setTimeout(step, 50)
    else stop()
  }
  for (const type of ["wheel", "touchstart", "keydown"])
    window.addEventListener(type, stop, { passive: true })
  timer = window.setTimeout(step, 50)
  return stop
}

/**
 * Moving between public pages keeps this document: the next page's code
 * loads while the current page stays on screen, then the two cross-fade.
 * Each history entry remembers its own scroll position.
 */
function usePageNavigation() {
  const [path, setPath] = useState(() => pagePath(window.location.pathname))

  useEffect(() => {
    let shownPath = pagePath(window.location.pathname)
    // Other pages always load on their own, so they keep the browser's scroll
    // restoration and their own fragment moves.
    const inPlace = publicPages.includes(shownPath)
    // The browser would restore an entry's scroll before the page it belongs
    // to is back on screen, so entries keep their own position instead.
    if (inPlace) window.history.scrollRestoration = "manual"
    const scrolls = new Map<string, number>()
    const saved: unknown = window.history.state?.kanoScroll
    let entry = String(window.history.state?.kanoEntry ?? newEntry())
    window.history.replaceState(
      { ...window.history.state, kanoEntry: entry },
      "",
    )
    let ticket = 0
    let stopSettling = () => {}
    if (inPlace && typeof saved === "number" && saved > 0) {
      stopSettling = settleScroll(saved)
    }

    let saveTimer = 0
    const remember = () => {
      window.clearTimeout(saveTimer)
      scrolls.set(entry, window.scrollY)
      window.history.replaceState(
        { ...window.history.state, kanoScroll: window.scrollY },
        "",
      )
    }
    // A reload starts from the entry's state, so save once scrolling rests.
    const onScroll = () => {
      window.clearTimeout(saveTimer)
      saveTimer = window.setTimeout(remember, 250)
    }

    const show = async (url: URL, top: number) => {
      const next = pagePath(url.pathname)
      const current = ++ticket
      try {
        await loadPage(next)
      } catch {
        window.location.reload()
        return
      }
      if (current !== ticket) return
      const samePage = next === shownPath
      shownPath = next
      const commit = () => {
        flushSync(() => setPath(next))
        const target = url.hash
          ? document.getElementById(decodeURIComponent(url.hash.slice(1)))
          : null
        if (target) target.scrollIntoView({ behavior: "instant" })
        else window.scrollTo({ top, behavior: samePage ? "smooth" : "instant" })
      }
      if (samePage || !document.startViewTransition || reducedMotion?.matches) {
        commit()
        return
      }
      // A page that cannot render a frame never reaches the update, so the
      // swap goes ahead without the fade rather than waiting for one.
      const transition = document.startViewTransition(commit)
      const skip = window.setTimeout(() => transition.skipTransition(), 400)
      const settle = () => window.clearTimeout(skip)
      transition.updateCallbackDone.then(settle, settle)
      transition.ready.catch(() => undefined)
    }

    const onClick = (event: MouseEvent) => {
      if (
        event.defaultPrevented ||
        !publicPages.includes(shownPath) ||
        event.button !== 0 ||
        event.metaKey ||
        event.ctrlKey ||
        event.shiftKey ||
        event.altKey
      )
        return
      const link = (event.target as Element | null)?.closest?.("a[href]")
      if (
        !(link instanceof HTMLAnchorElement) ||
        (link.target && link.target !== "_self") ||
        link.hasAttribute("download")
      )
        return
      const url = inPlaceUrl(link.href, window.location.href, publicPages)
      if (!url) return
      event.preventDefault()
      if (url.href !== window.location.href) {
        remember()
        entry = newEntry()
        window.history.pushState({ kanoEntry: entry }, "", url.href)
      }
      void show(url, 0)
    }

    const onPopState = (event: PopStateEvent) => {
      const url = new URL(window.location.href)
      const next = pagePath(url.pathname)
      const move = historyMove(shownPath, next, publicPages)
      if (move === "stay") return
      if (move === "load") {
        window.location.reload()
        return
      }
      stopSettling()
      window.clearTimeout(saveTimer)
      scrolls.set(entry, window.scrollY)
      entry = String(event.state?.kanoEntry ?? newEntry())
      const top: unknown = scrolls.get(entry) ?? event.state?.kanoScroll
      if (next !== shownPath || typeof top === "number")
        void show(url, typeof top === "number" ? top : 0)
    }

    // Start loading a page as soon as a pointer or focus reaches its link.
    const onIntent = (event: Event) => {
      const link = (event.target as Element | null)?.closest?.("a[href]")
      if (!(link instanceof HTMLAnchorElement)) return
      const url = inPlaceUrl(link.href, window.location.href, publicPages)
      if (url) loadPage(pagePath(url.pathname)).catch(() => undefined)
    }

    document.addEventListener("click", onClick)
    document.addEventListener("pointerover", onIntent, { passive: true })
    document.addEventListener("focusin", onIntent)
    window.addEventListener("popstate", onPopState)
    if (inPlace) {
      window.addEventListener("pagehide", remember)
      window.addEventListener("scroll", onScroll, { passive: true })
    }
    return () => {
      stopSettling()
      window.clearTimeout(saveTimer)
      window.removeEventListener("scroll", onScroll)
      document.removeEventListener("click", onClick)
      document.removeEventListener("pointerover", onIntent)
      document.removeEventListener("focusin", onIntent)
      window.removeEventListener("popstate", onPopState)
      window.removeEventListener("pagehide", remember)
    }
  }, [])

  return path
}

function PageRouter() {
  const path = usePageNavigation()
  const [, setLoaded] = useState(0)
  const [failed, setFailed] = useState(false)
  const Page = Object.hasOwn(routes, path)
    ? loadedPages.get(path)
    : NotFoundPage

  useEffect(() => {
    if (Page) return
    let current = true
    loadPage(path).then(
      () => {
        if (current) setLoaded((count) => count + 1)
      },
      () => {
        if (current) setFailed(true)
      },
    )
    return () => {
      current = false
    }
  }, [Page, path])

  if (Page) {
    return (
      <PageBoundary key={path}>
        <Page />
      </PageBoundary>
    )
  }
  return <PageStatus failed={failed} />
}

const rootElement = document.getElementById("root")
if (!rootElement) throw new Error("Missing application root element")
const reactRoot =
  globalThis.__kanoReactRoot && globalThis.__kanoRootElement === rootElement
    ? globalThis.__kanoReactRoot
    : createRoot(rootElement)
globalThis.__kanoReactRoot = reactRoot
globalThis.__kanoRootElement = rootElement
reactRoot.render(
  <AppSettingsProvider>
    <PageRouter />
  </AppSettingsProvider>,
)

if (import.meta.hot) {
  import.meta.hot.dispose(() => {
    reactRoot.unmount()
    globalThis.__kanoReactRoot = undefined
    globalThis.__kanoRootElement = undefined
  })
}
