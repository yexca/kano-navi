import type {
  ArchiveSnapshot,
  Post,
  ScheduleAsset,
  ScheduleEvent,
  Translator,
  Video,
} from "@/types"
import { useCallback, useEffect, useState } from "react"
import { WifiOff } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { MediaDialog, stepIndex } from "@/site/media-dialog"
import { SiteFooter, SiteHeader, SkipLink } from "@/site/site-shell"
import { XLogo, YouTubeLogo } from "@/site/links"
import { POST_WINDOW_DAYS } from "./content"
import { featuredVideoIdOf } from "./derive-dashboard"
import { syncStateFor } from "./freshness"
import {
  addDays,
  formatStamp,
  japanToday,
  localDateKey,
  monthStartOf,
  startOfWeek,
} from "./format"
import {
  type ArchiveRequest,
  useArchive,
  useDashboard,
  useNow,
} from "./use-dashboard"
import { ArchiveDialog } from "./components/archive-dialog"
import { LinkDirectory } from "./components/link-directory"
import { NowStage } from "./components/now-stage"
import {
  PostArchive,
  PostArchiveFilter,
  PostWall,
} from "./components/post-wall"
import { VideoArchive, VideoShelf } from "./components/video-shelf"
import { ScheduleMonth, WeekGuide } from "./components/week-guide"
import "./board.css"

interface ViewerImage {
  url: string
  alt?: string
}

/** Images offered to the viewer; entries whose media is not cached are skipped. */
type ViewerCandidate = Omit<ViewerImage, "url"> & { url: string | null }

/** Heading copy of each month dialog, with the loaded month's total. */
function archiveCopy(
  view: ArchiveRequest,
  archive: ArchiveSnapshot<unknown> | null,
  accounts: string[],
  t: Translator,
) {
  const total = archive ? archive.total : null
  if (view.kind === "schedule")
    return {
      kicker: t("nav.schedule"),
      title: t("schedule.monthTitle"),
      note: t("schedule.note"),
      summary: total == null ? null : t("schedule.weekCount", { count: total }),
    }
  if (view.kind === "posts")
    return {
      kicker: (
        <>
          <XLogo className="kicker-icon" aria-hidden="true" />{" "}
          {accounts.map((handle) => `@${handle}`).join(" · ")}
        </>
      ),
      title: t("feed.archiveTitle"),
      note: t("feed.archiveNote"),
      summary: total == null ? null : t("feed.monthCount", { count: total }),
    }
  return {
    kicker: (
      <>
        <YouTubeLogo className="kicker-icon" aria-hidden="true" /> YouTube
      </>
    ),
    title: t("videos.archiveTitle"),
    note: t("videos.archiveNote"),
    summary: total == null ? null : t("videos.monthCount", { count: total }),
  }
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
    calendarRange,
  } = useDashboard(now)
  // One month dialog at a time: the schedule grid, past posts or past videos.
  const [archiveView, setArchiveView] = useState<ArchiveRequest | null>(null)
  const archive = useArchive(archiveView, dashboard.meta.revision)
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

  const openImages = useCallback((items: ViewerCandidate[], index: number) => {
    const ready = items.filter((item): item is ViewerImage => Boolean(item.url))
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
  const openScheduleMonth = useCallback(() => {
    // The month follows the visible week, or today when it is inside it.
    const todayKey = localDateKey(japanToday())
    const inWeek =
      todayKey >= localDateKey(weekStart) &&
      todayKey <= localDateKey(addDays(weekStart, 6))
    setArchiveView({
      kind: "schedule",
      monthStart: monthStartOf(inWeek ? japanToday() : addDays(weekStart, 3)),
    })
  }, [weekStart])
  const openPostArchive = useCallback(
    (account: string | null) =>
      setArchiveView({
        kind: "posts",
        monthStart: monthStartOf(japanToday()),
        account,
      }),
    [],
  )
  const openVideoArchive = useCallback(
    () =>
      setArchiveView({
        kind: "videos",
        monthStart: monthStartOf(japanToday()),
      }),
    [],
  )
  const closeArchive = useCallback(() => setArchiveView(null), [])
  const setArchiveMonth = useCallback(
    (monthStart: Date) =>
      setArchiveView((current) =>
        current ? { ...current, monthStart } : null,
      ),
    [],
  )
  const setArchiveAccount = useCallback(
    (account: string | null) =>
      setArchiveView((current) => (current ? { ...current, account } : null)),
    [],
  )
  const openWeek = useCallback(
    (day: Date) => {
      setWeekStart(startOfWeek(day))
      setArchiveView(null)
    },
    [setWeekStart],
  )
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
  const syncState = syncStateFor(meta, error, now, t)
  const syncIso = meta.fetchedAt
  const syncTime = syncIso ? formatStamp(syncIso) : t("sync.waiting")
  const counts = summary.counts || {
    posts: dashboard.posts.length,
    upcomingEvents: 0,
    videos: dashboard.videos.length,
  }
  const featuredVideoId = featuredVideoIdOf(dashboard)

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
          onOpenMonth={openScheduleMonth}
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
          onOpenArchive={openPostArchive}
        />

        <VideoShelf
          videos={dashboard.videos}
          featuredVideoId={featuredVideoId}
          focus={dashboard.focus}
          channelUrl={profile.youtubeUrl}
          isLoading={isLoading}
          onOpenArchive={openVideoArchive}
          locale={locale}
          now={now}
          t={t}
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
      {archiveView ? (
        <ArchiveDialog
          open
          {...archiveCopy(
            archiveView,
            archive.archive,
            meta.xAccounts || [],
            t,
          )}
          monthStart={archiveView.monthStart}
          onMonthChange={setArchiveMonth}
          latestMonth={
            archiveView.kind === "posts"
              ? monthStartOf(japanToday())
              : undefined
          }
          toolbar={
            archiveView.kind === "posts" ? (
              <PostArchiveFilter
                accounts={meta.xAccounts || []}
                account={archiveView.account || null}
                setAccount={setArchiveAccount}
                counts={archive.archive?.accountCounts}
                t={t}
              />
            ) : null
          }
          onClose={closeArchive}
          locale={locale}
          t={t}
        >
          {archiveView.kind === "schedule" ? (
            <ScheduleMonth
              key={archiveView.monthStart.getTime()}
              monthStart={archiveView.monthStart}
              archive={archive.archive as ArchiveSnapshot<ScheduleEvent> | null}
              isLoading={archive.isLoading}
              error={archive.error}
              onRetry={archive.reload}
              onLoadMore={archive.loadMore}
              onJump={setArchiveMonth}
              onOpenWeek={openWeek}
              locale={locale}
              now={now}
              t={t}
            />
          ) : archiveView.kind === "posts" ? (
            <PostArchive
              archive={archive.archive as ArchiveSnapshot<Post> | null}
              isLoading={archive.isLoading}
              error={archive.error}
              onRetry={archive.reload}
              onLoadMore={archive.loadMore}
              onJump={setArchiveMonth}
              avatarUrl={profile.avatarUrl}
              locale={locale}
              now={now}
              t={t}
              onOpenMedia={openImages}
            />
          ) : (
            <VideoArchive
              archive={archive.archive as ArchiveSnapshot<Video> | null}
              isLoading={archive.isLoading}
              error={archive.error}
              onRetry={archive.reload}
              onLoadMore={archive.loadMore}
              onJump={setArchiveMonth}
              locale={locale}
              now={now}
              t={t}
            />
          )}
        </ArchiveDialog>
      ) : null}
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
