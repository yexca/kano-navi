import { useCallback, useEffect, useState } from "react"
import { ArrowUp, WifiOff } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { POST_WINDOW_DAYS } from "./content"
import { formatStamp } from "./format"
import { useDashboard, useNow } from "./use-dashboard"
import { ArchiveSection } from "./components/archive-section"
import { FeedPanel } from "./components/feed-panel"
import { Hero } from "./components/hero"
import { Lightbox } from "./components/lightbox"
import { SchedulePanel } from "./components/schedule-panel"
import { SiteHeader } from "./components/site-header"
import { VideoSection } from "./components/video-section"
import "./dashboard.css"

function syncStateFor(meta, error, t) {
  if (error) return { tone: "danger", label: t("sync.apiUnavailable") }
  const status = meta?.lastSync?.status
  if (!status) return { tone: "muted", label: t("sync.waiting") }
  if (!status || status === "success")
    return { tone: "leaf", label: t("sync.snapshot") }
  if (status === "partial") return { tone: "honey", label: t("sync.partial") }
  return { tone: "danger", label: t("sync.needsReview") }
}

export function DashboardApp() {
  const { locale, t } = useAppSettings()
  const { dashboard, isLoading, isRefreshing, error, reload } = useDashboard()
  const now = useNow()
  const [toast, setToast] = useState(null)
  const [lightbox, setLightbox] = useState(null)

  useEffect(() => {
    document.title = t("meta.title")
    document
      .querySelector('meta[name="description"]')
      ?.setAttribute("content", t("meta.description"))
  }, [t])

  useEffect(() => {
    if (!toast) return undefined
    const timer = window.setTimeout(() => setToast(null), 2600)
    return () => window.clearTimeout(timer)
  }, [toast])

  const refresh = useCallback(async () => {
    const result = await reload()
    setToast(
      result.ok
        ? { key: "toast.reloaded" }
        : { key: "toast.failed", values: { message: result.error.message } },
    )
  }, [reload])

  const openMedia = useCallback((media, index) => {
    if (media?.length) setLightbox({ media, index })
  }, [])
  const closeLightbox = useCallback(() => setLightbox(null), [])
  const stepLightbox = useCallback((offset) => {
    setLightbox((current) =>
      current
        ? {
            ...current,
            index:
              (current.index + offset + current.media.length) %
              current.media.length,
          }
        : current,
    )
  }, [])

  const { profile, summary, meta } = dashboard
  const windowDays = meta.postWindowDays || POST_WINDOW_DAYS
  const syncState = syncStateFor(meta, error, t)
  const syncIso = meta.fetchedAt
  const syncTime = syncIso ? formatStamp(syncIso) : t("sync.waiting")
  const counts = summary.counts || {
    posts: dashboard.posts.length,
    upcomingEvents: 0,
    videos: dashboard.videos.length,
  }
  const scheduleImages = dashboard.scheduleImages?.length
    ? dashboard.scheduleImages
    : (dashboard.assets || []).filter((asset) => asset.kind === "schedule")

  return (
    <div className="board">
      <a className="skip-link" href="#schedule">
        {t("nav.schedule")}
      </a>
      <SiteHeader
        profile={profile}
        syncState={syncState}
        syncTime={syncTime}
        syncIso={syncIso}
        t={t}
      />

      <main className="board-main">
        {error ? (
          <div className="api-alert" role="status">
            <WifiOff aria-hidden="true" />
            <span>{t("error.api")}</span>
            <button type="button" onClick={refresh}>
              {t("common.retry")}
            </button>
          </div>
        ) : null}

        <Hero
          profile={profile}
          summary={summary}
          counts={counts}
          postWindowDays={windowDays}
          syncState={syncState}
          syncTime={syncTime}
          isLoading={isLoading}
          isRefreshing={isRefreshing}
          onRefresh={refresh}
          resources={dashboard.resources}
          locale={locale}
          now={now}
          t={t}
        />

        <div className="board-grid">
          <SchedulePanel
            events={dashboard.events}
            scheduleImages={scheduleImages}
            isLoading={isLoading}
            locale={locale}
            now={now}
            t={t}
          />
          <FeedPanel
            posts={dashboard.posts}
            latestPost={summary.latestPost}
            accounts={meta.xAccounts || []}
            avatarUrl={profile.avatarUrl}
            windowDays={windowDays}
            isLoading={isLoading}
            locale={locale}
            now={now}
            t={t}
            onOpenMedia={openMedia}
          />
        </div>

        <VideoSection
          videos={dashboard.videos}
          featuredVideoId={
            Object.hasOwn(meta, "featuredVideoId")
              ? meta.featuredVideoId
              : dashboard.focus?.videoId
          }
          focus={dashboard.focus}
          channelUrl={profile.youtubeUrl}
          isLoading={isLoading}
          locale={locale}
          now={now}
          t={t}
        />

        <ArchiveSection
          timeline={dashboard.timeline}
          resources={dashboard.resources}
          t={t}
        />
      </main>

      <footer className="site-footer">
        <div className="site-footer-brand">
          <span aria-hidden="true">🍓🦌</span>
          <strong>{t("footer.fanMade")}</strong>
        </div>
        <p>{t("footer.disclaimer")}</p>
        <a href="#now" className="soft-button">
          <ArrowUp aria-hidden="true" />
          {t("footer.backToTop")}
        </a>
      </footer>

      <div
        className={`toast${toast ? " is-visible" : ""}`}
        role="status"
        aria-live="polite"
      >
        {toast ? t(toast.key, toast.values) : ""}
      </div>
      <Lightbox
        media={lightbox?.media}
        index={lightbox?.index || 0}
        onClose={closeLightbox}
        onStep={stepLightbox}
        t={t}
      />
    </div>
  )
}
