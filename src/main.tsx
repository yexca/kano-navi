import { createRoot, type Root } from "react-dom/client"
import {
  Component,
  lazy,
  Suspense,
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
import "./index.css"

declare global {
  var __kanoReactRoot: Root | undefined
  var __kanoRootElement: HTMLElement | null | undefined
}

function withMessages(Page: ComponentType, messages: PageMessages) {
  return {
    default: () => (
      <PageMessagesProvider messages={messages}>
        <Page />
      </PageMessagesProvider>
    ),
  }
}

const routes: Record<string, ComponentType> = {
  "/about": lazy(async () => {
    const [page, copy] = await Promise.all([
      import("@/about/about-app"),
      import("@/about/about-i18n"),
    ])
    return withMessages(page.AboutApp, copy.aboutMessages)
  }),
  "/admin": lazy(async () => {
    const [page, copy] = await Promise.all([
      import("@/admin"),
      import("@/admin-i18n"),
    ])
    return withMessages(page.AdminApp, copy.adminMessages)
  }),
  "/history": lazy(async () => {
    const [page, copy] = await Promise.all([
      import("@/history/history-app"),
      import("@/history/history-i18n"),
    ])
    return withMessages(page.HistoryApp, copy.historyMessages)
  }),
}

function PageStatus({ failed = false }: { failed?: boolean }) {
  const { t } = useAppSettings()
  return (
    <main className="min-h-screen grid place-items-center p-6">
      <div
        className="rounded-2xl border bg-card p-8 text-center shadow-sm"
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

const rootElement = document.getElementById("root")
if (!rootElement) throw new Error("Missing application root element")
const normalizedPath = window.location.pathname.replace(/\/+$/u, "") || "/"
const PageApp = routes[normalizedPath] || DashboardApp
const reactRoot =
  globalThis.__kanoReactRoot && globalThis.__kanoRootElement === rootElement
    ? globalThis.__kanoReactRoot
    : createRoot(rootElement)
globalThis.__kanoReactRoot = reactRoot
globalThis.__kanoRootElement = rootElement
reactRoot.render(
  <AppSettingsProvider>
    <PageBoundary>
      <Suspense fallback={<PageStatus />}>
        <PageApp />
      </Suspense>
    </PageBoundary>
  </AppSettingsProvider>,
)

if (import.meta.hot) {
  import.meta.hot.dispose(() => {
    reactRoot.unmount()
    globalThis.__kanoReactRoot = undefined
    globalThis.__kanoRootElement = undefined
  })
}
