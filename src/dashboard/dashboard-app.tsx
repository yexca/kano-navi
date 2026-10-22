import type { Dashboard, ScheduleAsset, Translator } from "@/types"
import { useCallback, useEffect, useState } from "react"
import { WifiOff } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { MediaDialog, stepIndex } from "@/site/media-dialog"
import { SiteFooter, SiteHeader, SkipLink } from "@/site/site-shell"
import { POST_WINDOW_DAYS } from "./content"
import { formatStamp } from "./format"
import { useDashboard, useNow } from "./use-dashboard"
import { LinkDirectory } from "./components/link-directory"
import { NowStage, type SyncState } from "./components/now-stage"
import { PostWall } from "./components/post-wall"
import { VideoShelf } from "./components/video-shelf"
import { WeekGuide } from "./components/week-guide"
import "./board.css"

interface ViewerImage {
  url: string
  alt?: string
}

function syncStateFor(
  meta: Dashboard["meta"],
  error: Error | null,
  t: Translator,
): SyncState {
  if (error) return { tone: "danger", label: t("sync.apiUnavailable") }
  const status = meta?.lastSync?.status
  if (!status) return { tone: "muted", label: t("sync.waiting") }
  if (status === "success") return { tone: "leaf", label: t("sync.snapshot") }
  if (status === "partial") return { tone: "honey", label: t("sync.partial") }
  return { tone: "danger", label: t("sync.needsReview") }
}

export function DashboardApp() {
  const { locale, t } = useAppSettings()
  const now = useNow()
  const {
    dashboard,
    isLoading,
    isRefreshing,
    error,
    reload,
    calendar,
    calendarLoading,
    calendarError,
    reloadCalendar,
    loadMoreCalendar,
    weekStart,
    setWeekStart,
    monthStart,
    setMonthStart,
    calendarView,
    setCalendarView,
    calendarRange,
  } = useDashboard(now)
  const [toast, setToast] = useState<{
    key: string
    values?: Record<string, string>
  } | null>(null)
  const [viewer, setViewer] = useState<{
    items: ViewerImage[]
    index: number
  } | null>(null)

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
      result.ok === true
        ? { key: "toast.reloaded" }
        : { key: "toast.failed", values: { message: result.error.message } },
    )
  }, [reload])

  const openImages = useCallback((items: ViewerImage[], index: number) => {
    const ready = items.filter((item) => item.url)
    if (ready.length) setViewer({ items: ready, index: Math.max(0, index) })
  }, [])
  const openScheduleImage = useCallback(
    (asset: ScheduleAsset) =>
      openImages(
        [{ url: asset.url, alt: asset.alt || t("schedule.imageAlt") }],
        0,
      ),
    [openImages, t],
  )
  const closeViewer = useCallback(() => setViewer(null), [])
  const stepViewer = useCallback(
    (offset: number) =>
      setViewer((current) =>
        current
          ? {
              ...current,
              index: stepIndex(current.index, offset, current.items.length),
            }
          : current,
      ),
    [],
  )

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
  const featuredVideoId = Object.hasOwn(meta, "featuredVideoId")
    ? meta.featuredVideoId
    : dashboard.focus?.videoId

  return (
    <div className="site board" id="top">
      <SkipLink href="#schedule">{t("nav.schedule")}</SkipLink>
      <SiteHeader
        page="board"
        avatarUrl={profile.avatarUrl}
        status={
          <span
            className={`header-sync tone-${syncState.tone}`}
            title={syncState.label}
          >
            <span className="sync-dot" aria-hidden="true" />
            <span className="sr-only">{syncState.label}</span>
          </span>
        }
      />

      <main className="site-main">
        {error ? (
          <div className="inline-alert board-alert" role="status">
            <WifiOff aria-hidden="true" />
            <span>{t("error.api")}</span>
            <button type="button" className="pill-button" onClick={refresh}>
              {t("common.retry")}
            </button>
          </div>
        ) : null}

        <NowStage
          profile={profile}
          summary={summary}
          resources={dashboard.resources}
          counts={counts}
          postWindowDays={windowDays}
          syncState={syncState}
          syncTime={syncTime}
          syncIso={syncIso}
          isLoading={isLoading}
          isRefreshing={isRefreshing}
          onRefresh={refresh}
          locale={locale}
          now={now}
          t={t}
        />

        <WeekGuide
          view={calendarView}
          setView={setCalendarView}
          monthStart={monthStart}
          setMonthStart={setMonthStart}
          range={calendarRange}
          events={calendar.events}
          scheduleImages={calendar.scheduleImages}
          isLoading={calendarLoading}
          error={calendarError}
          onRetry={reloadCalendar}
          weekStart={weekStart}
          setWeekStart={setWeekStart}
          adjacent={calendar.adjacent}
          total={calendar.total}
          hasNext={calendar.hasNext}
          onLoadMore={loadMoreCalendar}
          dayCounts={calendar.dayCounts}
          onOpenImage={openScheduleImage}
          locale={locale}
          now={now}
          t={t}
        />

        <VideoShelf
          videos={dashboard.videos}
          featuredVideoId={featuredVideoId}
          focus={dashboard.focus}
          channelUrl={profile.youtubeUrl}
          isLoading={isLoading}
          locale={locale}
          now={now}
          t={t}
        />

        <PostWall
          posts={dashboard.posts}
          latestPost={summary.latestPost}
          accounts={meta.xAccounts || []}
          avatarUrl={profile.avatarUrl}
          windowDays={windowDays}
          isLoading={isLoading}
          locale={locale}
          now={now}
          t={t}
          onOpenMedia={openImages}
        />

        <LinkDirectory
          timeline={dashboard.timeline}
          resources={dashboard.resources}
          locale={locale}
          t={t}
        />
      </main>

      <SiteFooter disclaimer={t("footer.disclaimer")} />

      <div
        className={`toast${toast ? " is-visible" : ""}`}
        role="status"
        aria-live="polite"
      >
        {toast ? t(toast.key, toast.values) : ""}
      </div>
      <MediaDialog
        items={viewer?.items}
        index={viewer?.index || 0}
        label={t("feed.openMedia")}
        onClose={closeViewer}
        onStep={stepViewer}
        renderImage={(item) => <img src={item.url} alt={item.alt || ""} />}
      />
    </div>
  )
}
