import { createRoot, type Root } from "react-dom/client"
import type { ComponentType } from "react"

import { AppSettingsProvider } from "@/app-settings"
import { AboutApp } from "@/about/about-app"
import { AdminApp } from "@/admin"
import { DashboardApp } from "@/dashboard/dashboard-app"
import { HistoryApp } from "@/history/history-app"
import "./index.css"

declare global {
  var __kanoReactRoot: Root | undefined
  var __kanoRootElement: HTMLElement | null | undefined
}

const routes: Record<string, ComponentType> = {
  "/about": AboutApp,
  "/admin": AdminApp,
  "/history": HistoryApp,
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
