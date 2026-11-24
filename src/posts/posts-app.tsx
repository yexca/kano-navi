import type { ArchiveSnapshot, MediaItem, Post } from "@/types"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import { useAppSettings } from "@/app-settings"
import { cdnUrl } from "@/lib/cdn"
import { MediaDialog, stepIndex } from "@/site/media-dialog"
import { usePageMeta } from "@/site/page-meta"
import { SiteFooter, SiteHeader, SkipLink } from "@/site/site-shell"
import {
  ArchiveMonthBar,
  ArchiveState,
} from "@/dashboard/components/archive-dialog"
import { monthKeyOf } from "@/dashboard/components/month-picker"
import { PostMediaView } from "@/dashboard/components/post-wall"
import {
  dateKey,
  formatDayWeekday,
  japanToday,
  monthStartOf,
} from "@/dashboard/format"
import {
  timelineStartPage,
  useArchive,
  useNow,
  usePostArchiveIndex,
} from "@/dashboard/use-dashboard"
import { AccountBand } from "./account-band"
import { ActivityMap } from "./activity-map"
import { postsHref, usePostsLocation } from "./navigation"
import { PostTimeline } from "./post-timeline"
import { monthCount, nearestMonth } from "./timeline"
import "@/dashboard/styles/base.css"
import "@/dashboard/styles/posts.css"
import "@/dashboard/styles/month-dialogs.css"
import "./posts.css"

interface ViewerImage {
  url: string
  alt?: string
  kind?: MediaItem["kind"]
  href?: string
}

type ViewerCandidate = Omit<ViewerImage, "url"> & { url: string | null }

/** The first day of a `YYYY-MM` month, as a local Date. */
function monthFromKey(key: string) {
  const [year, month] = key.split("-").map(Number)
  return new Date(year, month - 1, 1)
}

/** A `YYYY-MM-DD` day, as a local Date. */
function dayFromKey(key: string) {
  const [year, month, day] = key.split("-").map(Number)
  return new Date(year, month - 1, day)
}

export function PostsApp() {
  const { locale, t } = useAppSettings()
  const now = useNow()
  const [view, show] = usePostsLocation()
  const { index, error: indexError, retry: retryIndex } = usePostArchiveIndex()

  usePageMeta(t("posts.metaTitle"), t("posts.metaDescription"))

  const account = view.account
  const filter = account?.toLowerCase() || null

  // The month the timeline starts from: the one the address named when the
  // page opened or a month was chosen that is not loaded; null starts from
  // the newest post.
  const [anchor, setAnchor] = useState<string | null>(view.month)
  const [jump, setJump] = useState<{ month: string; nonce: number } | null>(
    view.month ? { month: view.month, nonce: 1 } : null,
  )
  const jumpTo = useCallback(
    (month: string) =>
      setJump((current) => ({ month, nonce: (current?.nonce || 0) + 1 })),
    [],
  )

  // The timeline waits for the month index, which places a chosen month on
  // its page; without the index it starts from the month itself.
  const request = useMemo(() => {
    if (!index && !indexError) return null
    return {
      kind: "posts" as const,
      monthStart: monthStartOf(japanToday()),
      account,
      anchor: anchor ? monthFromKey(anchor) : null,
      startPage:
        anchor && index
          ? timelineStartPage(index.months, anchor, filter)
          : undefined,
    }
  }, [index, indexError, account, anchor, filter])
  const archive = useArchive<Post>(request, index?.revision)
  const posts = archive.archive as ArchiveSnapshot<Post> | null
  const firstPage = posts?.firstPage ?? 1

  const loadedMonths = useMemo(
    () => [
      ...new Set(
        (posts?.items || []).map((post) =>
          dateKey(post.publishedAt).slice(0, 7),
        ),
      ),
    ],
    [posts],
  )
  const loadedRef = useRef(loadedMonths)
  loadedRef.current = loadedMonths

  const monthCounts = useMemo(
    () =>
      index
        ? new Map(
            index.months.map((month) => [
              month.month,
              monthCount(month, filter),
            ]),
          )
        : null,
    [index, filter],
  )

  // The month in view follows the scroll, and the address follows it without
  // a new history entry; the newest month leaves the address bare.
  const [visible, setVisible] = useState<string | null>(null)
  // The day in view, which the narrow screens' month bar names.
  const [visibleDay, setVisibleDay] = useState<string | null>(null)
  const atNewest = firstPage === 1 && (!visible || visible === loadedMonths[0])
  useEffect(() => {
    if (!visible) return
    const href = postsHref({
      month:
        firstPage === 1 && visible === loadedRef.current[0] ? null : visible,
      account,
    })
    if (`${window.location.pathname}${window.location.search}` !== href)
      window.history.replaceState(window.history.state, "", href)
  }, [visible, firstPage, account])

  // The timeline's top, to return to when it restarts from the newest post.
  const timelineRef = useRef<HTMLDivElement>(null)
  const toTimelineTop = useCallback(() => {
    const top = timelineRef.current?.getBoundingClientRect().top
    if (top != null && top < 0)
      window.scrollBy({ top: top - 16, behavior: "instant" })
  }, [])

  // A view from the address (a choice, or back and forward) scrolls to its
  // month when it is loaded and otherwise restarts the timeline there.
  const shownView = useRef(view)
  useEffect(() => {
    const previous = shownView.current
    if (previous === view) return
    shownView.current = view
    const sameAccount = previous.account === view.account
    if (!view.month) {
      setAnchor(null)
      setJump(null)
      setVisible(null)
      setVisibleDay(null)
      toTimelineTop()
      return
    }
    if (!sameAccount || !loadedRef.current.includes(view.month))
      setAnchor(view.month)
    jumpTo(view.month)
  }, [view, jumpTo, toTimelineTop])

  const chooseMonth = useCallback(
    (month: string) => {
      const target =
        (index ? nearestMonth(index.months, month, filter) : month) ?? month
      show({ month: target, account })
    },
    [index, filter, account, show],
  )
  const setAccount = useCallback(
    (next: string | null) => {
      const nextFilter = next?.toLowerCase() || null
      const target =
        index && visible && !atNewest
          ? nearestMonth(index.months, visible, nextFilter)
          : null
      const newest = index ? nearestMonth(index.months, null, nextFilter) : null
      show({ month: target === newest ? null : target, account: next })
    },
    [index, visible, atNewest, show],
  )
  const showNewest = useCallback(
    () => show({ month: null, account }),
    [show, account],
  )

  const pickerButtonRef = useRef<HTMLButtonElement>(null)
  const [pickerOpen, setPickerOpen] = useState(false)
  const shownMonth = visible ?? loadedMonths[0] ?? anchor
  const shownDay =
    visibleDay && shownMonth && visibleDay.startsWith(shownMonth)
      ? formatDayWeekday(dayFromKey(visibleDay), locale)
      : null
  const thisMonth = monthStartOf(japanToday())
  const newestKey = index?.months.at(-1)?.month ?? null
  const latestMonth =
    newestKey && monthFromKey(newestKey) > thisMonth
      ? monthFromKey(newestKey)
      : thisMonth

  const [viewer, setViewer] = useState<{
    items: ViewerImage[]
    index: number
  } | null>(null)
  const openImages = useCallback((items: ViewerCandidate[], at: number) => {
    const ready = items.filter((item): item is ViewerImage => Boolean(item.url))
    if (ready.length) setViewer({ items: ready, index: Math.max(0, at) })
  }, [])
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

  const kanoAvatar = cdnUrl("/assets/kano-avatar.jpg")
  // Wide windows show the account cards in the rail, narrower ones above
  // the timeline; CSS shows one of the two.
  const accountProps = index
    ? {
        accounts: index.accounts,
        bilibiliAccounts: index.bilibiliAccounts,
        months: index.months,
        avatars: posts?.accountAvatars ?? index.accountAvatars,
        kanoAvatar,
        account,
        setAccount,
        locale,
        t,
      }
    : null
  return (
    <div className="site posts" id="top">
      <SkipLink href="#posts-content">{t("posts.skip")}</SkipLink>
      <SiteHeader page="posts" />

      <main className="site-main" id="posts-content">
        <section className="posts-page" aria-labelledby="posts-title">
          <h1 id="posts-title" className="sr-only">
            {t("posts.title")}
          </h1>

          {accountProps ? (
            <AccountBand placement="top" {...accountProps} />
          ) : null}

          <div className="posts-bar">
            <ArchiveMonthBar
              monthStart={shownMonth ? monthFromKey(shownMonth) : thisMonth}
              onMonthChange={(month) => chooseMonth(monthKeyOf(month))}
              latestMonth={latestMonth}
              detail={shownDay}
              picker={{
                counts: monthCounts,
                countLabel: (count) => t("feed.monthCount", { count }),
              }}
              timeline={{
                onLatest: showNewest,
                atLatest: atNewest,
                resetKey: `${anchor || "newest"}:${account || ""}`,
              }}
              pickerOpen={pickerOpen}
              setPickerOpen={setPickerOpen}
              pickerButtonRef={pickerButtonRef}
              locale={locale}
              t={t}
            />
          </div>

          <div className="posts-layout">
            {index?.months.length ? (
              <aside className="posts-rail">
                {accountProps ? (
                  <AccountBand placement="rail" {...accountProps} />
                ) : null}
                <ActivityMap
                  months={index.months}
                  filter={filter}
                  shown={shownMonth}
                  onChoose={chooseMonth}
                  locale={locale}
                  t={t}
                />
              </aside>
            ) : null}
            <div className="posts-column" ref={timelineRef}>
              {indexError && !index && !posts ? (
                <ArchiveState
                  error={indexError}
                  onRetry={retryIndex}
                  loading={false}
                  empty={false}
                  emptyTitle=""
                  onJump={() => {}}
                  locale={locale}
                  t={t}
                />
              ) : (
                <PostTimeline
                  archive={posts}
                  isLoading={archive.isLoading || (!index && !indexError)}
                  error={archive.error}
                  onRetry={archive.reload}
                  onLoadMore={archive.loadMore}
                  onNewer={firstPage > 1 ? archive.loadNewer : showNewest}
                  monthCounts={monthCounts}
                  jump={jump}
                  onVisibleMonth={setVisible}
                  onVisibleDay={setVisibleDay}
                  avatarUrl={kanoAvatar}
                  accountAvatars={
                    posts?.accountAvatars ?? index?.accountAvatars
                  }
                  locale={locale}
                  now={now}
                  t={t}
                  onOpenMedia={openImages}
                />
              )}
            </div>
          </div>
        </section>
      </main>

      <SiteFooter />
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
