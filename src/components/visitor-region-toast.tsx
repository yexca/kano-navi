import { useEffect, useState } from "react"

import { useAppSettings } from "@/app-settings"
import "./visitor-region-toast.css"

const SESSION_KEY = "kano-visitor-region-notice"
const PUBLIC_PATHS = new Set([
  "/",
  "/index.html",
  "/history",
  "/lineblog",
  "/about",
])

interface VisitorRegionPayload {
  enabled: boolean
  mainlandChina: boolean
  providerName: string
  providerUrl: string
  mirrorUrl: string
}

function wasHandled() {
  try {
    return window.sessionStorage.getItem(SESSION_KEY) === "handled"
  } catch {
    return false
  }
}

function rememberHandled() {
  try {
    window.sessionStorage.setItem(SESSION_KEY, "handled")
  } catch {
    // A blocked storage area should not prevent the visitor from dismissing
    // the notice for the current render.
  }
}

export function VisitorRegionToast() {
  const { t } = useAppSettings()
  const [config, setConfig] = useState<VisitorRegionPayload | null>(null)

  useEffect(() => {
    if (!PUBLIC_PATHS.has(window.location.pathname) || wasHandled()) return
    const controller = new AbortController()
    void fetch("/api/visitor-region", {
      headers: { accept: "application/json" },
      cache: "no-store",
      signal: controller.signal,
    })
      .then((response) => (response.ok ? response.json() : null))
      .then((payload: unknown) => {
        if (payload && typeof payload === "object") {
          const candidate = payload as Partial<VisitorRegionPayload>
          if (
            candidate.enabled === true &&
            candidate.mainlandChina === true &&
            typeof candidate.providerName === "string" &&
            typeof candidate.providerUrl === "string" &&
            typeof candidate.mirrorUrl === "string" &&
            candidate.providerName.trim() &&
            candidate.providerUrl.trim() &&
            candidate.mirrorUrl.trim() &&
            !wasHandled()
          ) {
            setConfig(candidate as VisitorRegionPayload)
          }
        }
      })
      .catch(() => undefined)
    return () => controller.abort()
  }, [])

  if (!config) return null

  const marker = "__VISITOR_REGION_PROVIDER__"
  const message = t("visitorRegion.message", { provider: marker })
  const markerAt = message.indexOf(marker)
  const messageBefore = markerAt < 0 ? message : message.slice(0, markerAt)
  const messageAfter =
    markerAt < 0 ? "" : message.slice(markerAt + marker.length)

  const dismiss = () => {
    rememberHandled()
    setConfig(null)
  }

  return (
    <aside className="visitor-region-toast" role="status" aria-live="polite">
      <div className="visitor-region-toast-copy">
        <strong>{t("visitorRegion.title")}</strong>
        <p>
          {messageBefore}
          <a
            className="visitor-region-toast-provider-link"
            href={config.providerUrl}
            target="_blank"
            rel="noopener noreferrer"
            onClick={rememberHandled}
          >
            {config.providerName}
          </a>
          {messageAfter}
        </p>
      </div>
      <div className="visitor-region-toast-actions">
        <a
          href={config.mirrorUrl}
          target="_blank"
          rel="noopener noreferrer"
          onClick={rememberHandled}
        >
          {t("visitorRegion.visit")}
        </a>
        <button type="button" onClick={dismiss}>
          {t("visitorRegion.dismiss")}
        </button>
      </div>
    </aside>
  )
}
