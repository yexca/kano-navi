import { createRoot } from "react-dom/client"

import { AppSettingsProvider } from "@/app-settings"
import { AdminApp } from "@/admin"
import { DashboardApp } from "@/dashboard/dashboard-app"
import { HistoryApp } from "@/history/history-app"
import "./index.css"

const routes = {
  "/admin": AdminApp,
  "/history": HistoryApp,
}

const rootElement = document.getElementById("root")
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
    <PageApp />
  </AppSettingsProvider>,
)

if (import.meta.hot) {
  import.meta.hot.dispose(() => {
    reactRoot.unmount()
    globalThis.__kanoReactRoot = undefined
    globalThis.__kanoRootElement = undefined
  })
}
