import type {
  ArchiveSnapshot,
  MediaItem,
  Post,
  ScheduleAsset,
  ScheduleEvent,
  Translator,
  Video,
} from "@/types"
import { useCallback, useEffect, useMemo, useState } from "react"
import { WifiOff } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { MediaDialog, stepIndex } from "@/site/media-dialog"
import { SiteFooter, SiteHeader, SkipLink } from "@/site/site-shell"
import { BilibiliLogo, XLogo, YouTubeLogo } from "@/site/links"
import { POST_WINDOW_DAYS, VIDEO_WINDOW_DAYS } from "./content"
import { featuredVideoIdOf } from "./derive-dashboard"
import { syncStateFor } from "./freshness"
import {
  addDays,
  japanToday,
  localDateKey,
  monthStartOf,
  startOfWeek,
} from "./format"
import {
  type ArchiveRequest,
  timelineStartPage,
  useArchive,
  useArchiveMonths,
  useDashboard,
  useNow,
} from "./use-dashboard"
import { ArchiveDialog } from "./components/archive-dialog"
import { monthKeyOf } from "./components/month-picker"
import { LinkDirectory } from "./components/link-directory"
import { NowStage } from "./components/now-stage"
import { SaleStrip } from "./components/sale-strip"
import {
  type PostArchiveLayout,
  PostArchive,
  PostArchiveFilter,
  PostArchiveLayoutToggle,
  PostMediaView,
  PostWall,
} from "./components/post-wall"
import {
  VideoArchiveFilter,
  VideoShelf,
  VideoTimeline,
} from "./components/video-shelf"
import { type VideoCategory, videoMonthKey } from "./video-groups"
import { ScheduleMonth, WeekGuide } from "./components/week-guide"
import "./board.css"

interface ViewerImage {
  url: string
  alt?: string
  /** A post's video or GIF still, and the page it plays on. */
  kind?: MediaItem["kind"]
  href?: string
}

/** Images offered to the viewer; entries whose media is not cached are skipped. */
type ViewerCandidate = Omit<ViewerImage, "url"> & { url: string | null }

/** This device remembers the post archive's layout. */
const POST_LAYOUT_KEY = "kano-post-archive-layout"

function readPostLayout(): PostArchiveLayout {
  try {
    return window.localStorage.getItem(POST_LAYOUT_KEY) === "list"
      ? "list"
      : "masonry"
  } catch {
    return "masonry"
  }
}

/** The first day of a `YYYY-MM` month, as a local Date. */
function monthFromKey(key: string) {
  const [year, month] = key.split("-").map(Number)
  return new Date(year, month - 1, 1)
}

/**
 * The last month a post or video jump grid offers: this Japan month, or a
 * later month holding a video reservation.
 */
function latestArchiveMonth(months: { month: string }[] | null) {
  const thisMonth = monthStartOf(japanToday())
  const last = months?.at(-1)?.month
  const indexed = last ? monthFromKey(last) : null
  return indexed && indexed > thisMonth ? indexed : thisMonth
}

/**
 * Heading copy of each archive dialog. The post and video kickers name the
 * platforms only, since the filter names each account. The total shows only
 * where no filter does: a filter's pressed button already counts it.
 */
function archiveCopy(
  view: ArchiveRequest,
  archive: ArchiveSnapshot<unknown> | null,
  accounts: string[],
  bilibiliAccounts: string[],
  t: Translator,
) {
  const total = archive ? archive.total : null
  if (view.kind === "schedule")
    return {
      kicker: t("nav.schedule"),
      title: t("schedule.monthTitle"),
      summary: total == null ? null : t("schedule.weekCount", { count: total }),
    }
  if (view.kind === "posts")
    return {
      kicker: (
        <>
          <XLogo className="kicker-icon" aria-hidden="true" /> X
          {bilibiliAccounts.length ? (
            <>
              {" · "}
              <BilibiliLogo className="kicker-icon" aria-hidden="true" />{" "}
              bilibili
            </>
          ) : null}
        </>
      ),
      title: t("feed.archiveTitle"),
      summary:
        total == null || accounts.length + bilibiliAccounts.length > 1
          ? null
          : t("feed.monthCount", { count: total }),
    }
  return {
    kicker: (
      <>
        <YouTubeLogo className="kicker-icon" aria-hidden="true" /> YouTube
        {" · "}
        <BilibiliLogo className="kicker-icon" aria-hidden="true" /> bilibili
      </>
    ),
    title: t("videos.archiveTitle"),
    summary: null,
  }
}

export function DashboardApp() {
  const { locale, t } = useAppSettings()
  const now = useNow()
  const {
    dashboard,
    snapshotVersion,
    isLoading,
    error,
    reload,
    calendar,
    calendarShownRange,
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
  const archive = useArchive(
    archiveView,
    dashboard.meta.revision,
    snapshotVersion,
  )
  const indexKind = archiveView?.kind === "videos" ? "videos" : "posts"
  const archiveMonths = useArchiveMonths(
    indexKind,
    archiveView?.kind === "posts" || archiveView?.kind === "videos",
    dashboard.meta.revision,
  )
  const indexFilter =
    archiveView?.kind === "posts"
      ? archiveView.account?.toLowerCase() || null
      : archiveView?.kind === "videos"
        ? archiveView.category || null
        : null
  // The month index narrowed to the chosen account or category.
  const archiveMonthCounts = useMemo(() => {
    if (!archiveMonths) return null
    return new Map(
      archiveMonths.map(({ month, total, counts }) => [
        month,
        indexFilter ? counts[indexFilter] || 0 : total,
      ]),
    )
  }, [archiveMonths, indexFilter])
  // The shown post month's counts per account from the index, known before
  // the month's posts arrive, so the account filter keeps its numbers.
  const postMonthKey =
    archiveView?.kind === "posts" ? monthKeyOf(archiveView.monthStart) : null
  const postMonthIndex = useMemo(() => {
    if (indexKind !== "posts" || !archiveMonths || !postMonthKey)
      return undefined
    return (
      archiveMonths.find(({ month }) => month === postMonthKey) ?? {
        month: postMonthKey,
        total: 0,
        counts: {},
      }
    )
  }, [indexKind, archiveMonths, postMonthKey])
  const postAccountCounts = postMonthIndex?.counts
  // Every video's category over the whole index, so the filter keeps its
  // totals wherever the timeline starts.
  const videoCategoryCounts = useMemo(() => {
    if (indexKind !== "videos" || !archiveMonths) return undefined
    const totals: Record<string, number> = {}
    for (const { counts } of archiveMonths)
      for (const [key, count] of Object.entries(counts))
        totals[key] = (totals[key] || 0) + count
    return totals
  }, [indexKind, archiveMonths])
  // The video timeline's month in view, and a loaded month to scroll to.
  const [timelineMonth, setTimelineMonth] = useState<string | null>(null)
  const [timelineJump, setTimelineJump] = useState<{
    month: string
    nonce: number
  } | null>(null)
  const videoArchive =
    archiveView?.kind === "videos"
      ? (archive.archive as ArchiveSnapshot<Video> | null)
      : null
  const loadedVideoMonths = useMemo(
    () => [...new Set((videoArchive?.items || []).map(videoMonthKey))],
    [videoArchive],
  )
  const timelineAnchor =
    archiveView?.kind === "videos" ? archiveView.anchor || null : null
  // Whether the loaded videos reach the newest one: a timeline from a chosen
  // month does once its newer pages are loaded, or when it starts on page 1.
  const timelineStartsAtNewest =
    !timelineAnchor ||
    (archiveView?.kind === "videos" && archiveView.startPage
      ? (videoArchive?.firstPage ?? archiveView.startPage) === 1
      : false)
  const timelineShown =
    timelineMonth ||
    loadedVideoMonths[0] ||
    (timelineAnchor ? monthKeyOf(timelineAnchor) : null)
  const [postLayout, setPostLayoutState] =
    useState<PostArchiveLayout>(readPostLayout)
  const setPostLayout = useCallback((layout: PostArchiveLayout) => {
    setPostLayoutState(layout)
    try {
      window.localStorage.setItem(POST_LAYOUT_KEY, layout)
    } catch {
      // The choice still applies until the page closes.
    }
  }, [])
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
  const openVideoArchive = useCallback(() => {
    setTimelineMonth(null)
    setTimelineJump(null)
    setArchiveView({
      kind: "videos",
      monthStart: monthStartOf(japanToday()),
      anchor: null,
    })
  }, [])
  const jumpTimeline = useCallback(
    (month: string) =>
      setTimelineJump((current) => ({
        month,
        nonce: (current?.nonce || 0) + 1,
      })),
    [],
  )
  // A loaded month scrolls into view. Any other restarts the timeline at the
  // page holding it, found from the month index, and scrolls to it once read.
  const videoMonths = indexKind === "videos" ? archiveMonths : null
  const chooseTimelineMonth = useCallback(
    (month: Date) => {
      const key = monthKeyOf(month)
      if (loadedVideoMonths.includes(key)) {
        jumpTimeline(key)
        return
      }
      setTimelineMonth(null)
      setArchiveView((current) =>
        current
          ? {
              ...current,
              anchor: monthStartOf(month),
              startPage: videoMonths
                ? timelineStartPage(videoMonths, key, current.category)
                : undefined,
            }
          : null,
      )
      jumpTimeline(key)
    },
    [loadedVideoMonths, videoMonths, jumpTimeline],
  )
  const showLatestVideos = useCallback(() => {
    if (!timelineStartsAtNewest) {
      setTimelineMonth(null)
      setArchiveView((current) =>
        current ? { ...current, anchor: null, startPage: undefined } : null,
      )
    } else if (loadedVideoMonths[0]) {
      const month = loadedVideoMonths[0]
      setTimelineJump((current) => ({
        month,
        nonce: (current?.nonce || 0) + 1,
      }))
    }
  }, [timelineStartsAtNewest, loadedVideoMonths])
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
  // A timeline from a chosen month keeps that month, at its page among the
  // category's videos.
  const setArchiveCategory = useCallback(
    (category: VideoCategory | null) => {
      setTimelineMonth(null)
      const anchor = archiveView?.kind === "videos" ? archiveView.anchor : null
      setArchiveView((current) =>
        current
          ? {
              ...current,
              category,
              startPage:
                current.anchor && videoMonths
                  ? timelineStartPage(
                      videoMonths,
                      monthKeyOf(current.anchor),
                      category,
                    )
                  : undefined,
            }
          : null,
      )
      if (anchor) jumpTimeline(monthKeyOf(anchor))
    },
    [archiveView, videoMonths, jumpTimeline],
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
          projection={dashboard.projection}
          liveNow={dashboard.liveNow}
          resources={dashboard.resources}
          counts={counts}
          postWindowDays={windowDays}
          videoWindowDays={VIDEO_WINDOW_DAYS}
          syncState={syncState}
          isLoading={isLoading}
          locale={locale}
          now={now}
          t={t}
          sales={(placement) => (
            <SaleStrip
              events={dashboard.events}
              salesIds={dashboard.projection?.sales}
              placement={placement}
              locale={locale}
              now={now}
              t={t}
            />
          )}
        />

        <WeekGuide
          range={calendarRange}
          shownRange={calendarShownRange}
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
          bilibiliAccounts={meta.bilibiliAccounts || []}
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
          projection={dashboard.projection}
          focus={dashboard.focus}
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

      <SiteFooter />

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
            meta.bilibiliAccounts || [],
            t,
          )}
          monthStart={
            archiveView.kind === "videos"
              ? timelineShown
                ? monthFromKey(timelineShown)
                : monthStartOf(japanToday())
              : archiveView.monthStart
          }
          onMonthChange={
            archiveView.kind === "videos"
              ? chooseTimelineMonth
              : setArchiveMonth
          }
          latestMonth={
            archiveView.kind === "schedule"
              ? undefined
              : latestArchiveMonth(
                  archiveView.kind === "videos" ? archiveMonths : null,
                )
          }
          picker={
            archiveView.kind === "schedule"
              ? // The schedule has no month index: every month is offered.
                { counts: null }
              : {
                  counts: archiveMonthCounts,
                  countLabel: (count) =>
                    t(
                      archiveView.kind === "posts"
                        ? "feed.monthCount"
                        : "videos.monthCount",
                      { count },
                    ),
                }
          }
          timeline={
            archiveView.kind === "videos"
              ? {
                  onLatest: showLatestVideos,
                  atLatest:
                    timelineStartsAtNewest &&
                    (!timelineMonth || timelineMonth === loadedVideoMonths[0]),
                  resetKey: `${timelineAnchor ? monthKeyOf(timelineAnchor) : "latest"}:${archiveView.category || ""}`,
                }
              : undefined
          }
          toolbar={
            archiveView.kind === "posts" ? (
              <div className="archive-tools">
                <PostArchiveFilter
                  accounts={meta.xAccounts || []}
                  bilibiliAccounts={meta.bilibiliAccounts || []}
                  account={archiveView.account || null}
                  setAccount={setArchiveAccount}
                  counts={postAccountCounts ?? archive.archive?.accountCounts}
                  total={
                    postMonthIndex?.total ??
                    (archiveView.account ? null : archive.archive?.total)
                  }
                  t={t}
                />
                <PostArchiveLayoutToggle
                  layout={postLayout}
                  setLayout={setPostLayout}
                  t={t}
                />
              </div>
            ) : archiveView.kind === "schedule" ? null : (
              <VideoArchiveFilter
                category={archiveView.category || null}
                setCategory={setArchiveCategory}
                counts={videoCategoryCounts ?? archive.archive?.categoryCounts}
                t={t}
              />
            )
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
              layout={postLayout}
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
            <VideoTimeline
              archive={videoArchive}
              isLoading={archive.isLoading}
              error={archive.error}
              onRetry={archive.reload}
              onLoadMore={archive.loadMore}
              monthCounts={archiveMonthCounts}
              jump={timelineJump}
              onVisibleMonth={setTimelineMonth}
              onNewer={
                (videoArchive?.firstPage ?? 1) > 1
                  ? archive.loadNewer
                  : showLatestVideos
              }
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
        renderImage={(item) => <PostMediaView item={item} t={t} />}
      />
    </div>
  )
}
