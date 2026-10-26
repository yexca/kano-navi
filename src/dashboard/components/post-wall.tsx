import type {
  ArchiveSnapshot,
  Locale,
  MediaItem,
  Post,
  Translator,
} from "@/types"
import type { ReactNode } from "react"
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import {
  ArrowUpRight,
  ChevronDown,
  ChevronUp,
  Heart,
  History,
  ImageOff,
  MessageCircle,
  Repeat2,
  X,
} from "lucide-react"

import {
  TranslationNote,
  useTranslatedText,
} from "@/components/translated-text"
import { EmptyNote, Placeholder, SectionTitle } from "@/site/site-shell"
import { ExternalLink, XLogo } from "@/site/links"
import {
  formatCompact,
  formatJapanDay,
  formatJapanTime,
  formatRelative,
  formatStamp,
} from "../format"
import { ArchiveMore, ArchiveState } from "./archive-dialog"

/** Ready local media can open in the viewer; anything else stays labelled. */
export function postMedia(post: Post): MediaItem[] {
  if (Array.isArray(post.media) && post.media.length) return post.media
  return post.mediaUrl
    ? [
        {
          id: null,
          url: post.mediaUrl,
          status: post.mediaStatus || "ready",
          sourceUrl: null,
        },
      ]
    : []
}

function Engagement({
  post,
  locale,
  t,
}: {
  post: Post
  locale: Locale
  t: Translator
}) {
  const items = [
    { icon: MessageCircle, value: post.replies, label: t("feed.replies") },
    { icon: Repeat2, value: post.reposts, label: t("feed.reposts") },
    { icon: Heart, value: post.likes, label: t("feed.likes") },
  ].filter((item) => Number(item.value) > 0)
  if (!items.length) return null
  return (
    <p className="post-engagement">
      {items.map(({ icon: Icon, value, label }) => (
        <span key={label} title={label}>
          <Icon aria-hidden="true" />
          <span className="sr-only">{label}</span>
          {formatCompact(value, locale)}
        </span>
      ))}
    </p>
  )
}

/**
 * Native modal sheet with a post's full wording. Escape and the backdrop
 * close it, focus returns to the opener, and the image viewer stacks above.
 */
function PostDialog({
  label,
  onClose,
  children,
}: {
  label: string
  onClose: () => void
  children: ReactNode
}) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const dialog = dialogRef.current
    if (!dialog) return undefined
    const previous = document.activeElement as HTMLElement | null
    if (!dialog.open) dialog.showModal?.()
    dialog.querySelector<HTMLElement>(".post-dialog-close")?.focus()
    document.body.classList.add("has-sheet")
    return () => {
      if (dialog.open) dialog.close()
      document.body.classList.remove("has-sheet")
      previous?.focus?.()
    }
  }, [])
  return (
    <dialog
      ref={dialogRef}
      className="post-dialog"
      aria-label={label}
      onCancel={(event) => {
        event.preventDefault()
        onClose()
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      {children}
    </dialog>
  )
}

function PostNote({
  post,
  avatarUrl,
  absoluteTime = false,
  clamp = true,
  locale,
  now,
  t,
  onOpenMedia,
}: {
  post: Post
  avatarUrl: string | null
  /** Archives show the Japan date and time ("10.24 21:00"), not "3 days ago". */
  absoluteTime?: boolean
  /** The board shows six lines and opens long posts in a dialog. */
  clamp?: boolean
  locale: Locale
  now: number
  t: Translator
  onOpenMedia: (media: MediaItem[], index: number) => void
}) {
  const media = postMedia(post)
  const ready = media.filter((item) => item.url)
  const wording = useTranslatedText(
    post.text,
    post.translations,
    post.sourceLanguage,
    locale,
  )
  const textRef = useRef<HTMLParagraphElement>(null)
  const [overflows, setOverflows] = useState(false)
  const [reading, setReading] = useState(false)
  // "Read more" appears only while six lines hide some wording.
  useLayoutEffect(() => {
    const element = textRef.current
    if (!element || !clamp) return
    const check = () =>
      setOverflows(element.scrollHeight > element.clientHeight + 1)
    check()
    const observer = new ResizeObserver(check)
    observer.observe(element)
    return () => observer.disconnect()
  }, [clamp, wording.text])

  const head = (close?: ReactNode) => (
    <header className="post-note-head">
      <img src={avatarUrl || undefined} alt="" />
      {post.accountHandle ? (
        <ExternalLink
          className="post-note-handle"
          href={post.accountUrl || `https://x.com/${post.accountHandle}`}
        >
          @{post.accountHandle}
        </ExternalLink>
      ) : (
        <span className="post-note-handle">X</span>
      )}
      <time
        dateTime={post.publishedAt}
        title={`${formatJapanDay(post.publishedAt, locale)} ${formatJapanTime(post.publishedAt)} ${t("common.timezoneShort")}`}
      >
        {absoluteTime
          ? formatStamp(post.publishedAt)
          : formatRelative(post.publishedAt, locale, now)}
      </time>
      <ExternalLink
        className="post-note-open"
        href={post.url}
        aria-label={t("feed.originalPost")}
        title={t("feed.originalPost")}
      >
        <ArrowUpRight aria-hidden="true" />
      </ExternalLink>
      {close}
    </header>
  )
  const mediaGrid = media.length ? (
    <div className={`post-note-media count-${Math.min(4, media.length)}`}>
      {media.slice(0, 4).map((item, index) =>
        item.url ? (
          <button
            type="button"
            key={item.id || item.url}
            onClick={() => onOpenMedia(ready, ready.indexOf(item))}
            aria-label={`${t("feed.openMedia")} ${index + 1}`}
          >
            <img src={item.url} alt={item.alt || ""} loading="lazy" />
          </button>
        ) : (
          <span key={item.id || index} className="post-note-missing">
            <ImageOff aria-hidden="true" />
            <span>{t("media.unavailable")}</span>
          </span>
        ),
      )}
    </div>
  ) : null

  return (
    <article className="post-note">
      {head()}
      <p
        ref={textRef}
        className={`post-note-text${clamp ? " is-clamped" : ""}`}
        lang={wording.lang}
      >
        {wording.text}
      </p>
      {clamp && overflows ? (
        <button
          type="button"
          className="post-note-expand"
          aria-haspopup="dialog"
          onClick={() => setReading(true)}
        >
          {t("feed.readMore")}
        </button>
      ) : null}
      <TranslationNote state={wording} t={t} />
      {mediaGrid}
      <Engagement post={post} locale={locale} t={t} />
      {reading ? (
        <PostDialog
          label={t("feed.fullPost")}
          onClose={() => setReading(false)}
        >
          <article className="post-note is-full">
            {head(
              <button
                type="button"
                className="round-button post-dialog-close"
                onClick={() => setReading(false)}
                aria-label={t("common.close")}
                title={t("common.close")}
              >
                <X aria-hidden="true" />
              </button>,
            )}
            <p className="post-note-text" lang={wording.lang}>
              {wording.text}
            </p>
            <TranslationNote state={wording} t={t} />
            {mediaGrid}
            <Engagement post={post} locale={locale} t={t} />
          </article>
        </PostDialog>
      ) : null}
    </article>
  )
}

export function PostWall({
  posts,
  latestPost,
  accounts,
  avatarUrl,
  windowDays,
  isLoading,
  locale,
  now,
  t,
  onOpenMedia,
  onOpenArchive,
}: {
  posts: Post[]
  latestPost: Post | null
  accounts: string[]
  avatarUrl: string | null
  windowDays: number
  isLoading: boolean
  locale: Locale
  now: number
  t: Translator
  onOpenMedia: (media: MediaItem[], index: number) => void
  onOpenArchive: (account: string | null) => void
}) {
  const [account, setAccount] = useState<string | null>(null)
  const visible = useMemo(
    () =>
      account
        ? posts.filter(
            (post) =>
              String(post.accountHandle || "").toLowerCase() ===
              account.toLowerCase(),
          )
        : posts,
    [account, posts],
  )
  const countFor = (handle: string) =>
    posts.filter(
      (post) =>
        String(post.accountHandle || "").toLowerCase() === handle.toLowerCase(),
    ).length
  // The board shows two rows of the wall's current column count; the rest
  // waits behind "Show all".
  const sectionRef = useRef<HTMLElement>(null)
  const [wall, setWall] = useState<HTMLDivElement | null>(null)
  const [columns, setColumns] = useState(3)
  const [showAll, setShowAll] = useState(false)
  useLayoutEffect(() => {
    if (!wall) return
    const measure = () =>
      setColumns(
        Math.max(
          1,
          getComputedStyle(wall).gridTemplateColumns.split(" ").filter(Boolean)
            .length,
        ),
      )
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(wall)
    return () => observer.disconnect()
  }, [wall])
  const limit = columns * 2
  const shown = showAll ? visible : visible.slice(0, limit)
  const toggleAll = () => {
    if (showAll) {
      const top = sectionRef.current?.getBoundingClientRect().top ?? 0
      if (top < 0) sectionRef.current?.scrollIntoView({ block: "start" })
    }
    setShowAll((value) => !value)
  }

  return (
    <section
      ref={sectionRef}
      className="section posts"
      id="feed"
      aria-labelledby="posts-title"
    >
      <SectionTitle
        id="posts-title"
        kicker={
          <>
            <XLogo className="kicker-icon" aria-hidden="true" />{" "}
            {accounts.map((handle) => `@${handle}`).join(" · ")}
          </>
        }
        title={t("feed.title")}
        note={t("feed.subtitle", { days: windowDays })}
      >
        {accounts.length > 1 ? (
          <div className="segmented" role="group" aria-label={t("feed.filter")}>
            <button
              type="button"
              aria-pressed={!account}
              onClick={() => setAccount(null)}
            >
              {t("common.all")}
              <small>{posts.length}</small>
            </button>
            {accounts.map((handle) => (
              <button
                type="button"
                key={handle}
                aria-pressed={account === handle}
                onClick={() => setAccount(handle)}
              >
                @{handle}
                <small>{countFor(handle)}</small>
              </button>
            ))}
          </div>
        ) : null}
        <button
          type="button"
          className="pill-button"
          onClick={() => onOpenArchive(account)}
          aria-haspopup="dialog"
        >
          <History aria-hidden="true" />
          {t("feed.archive")}
        </button>
      </SectionTitle>

      {isLoading ? (
        <Placeholder lines={5} />
      ) : visible.length ? (
        <>
          <div className="post-wall" ref={setWall}>
            {shown.map((post) => (
              <PostNote
                key={post.id}
                post={post}
                avatarUrl={avatarUrl}
                locale={locale}
                now={now}
                t={t}
                onOpenMedia={onOpenMedia}
              />
            ))}
          </div>
          {visible.length > limit ? (
            <div className="posts-more">
              <button
                type="button"
                className="pill-button"
                aria-expanded={showAll}
                onClick={toggleAll}
              >
                {showAll ? (
                  <ChevronUp aria-hidden="true" />
                ) : (
                  <ChevronDown aria-hidden="true" />
                )}
                {showAll
                  ? t("feed.showFewer")
                  : t("feed.showAll", { count: visible.length })}
              </button>
            </div>
          ) : null}
        </>
      ) : (
        <div className="post-empty">
          <EmptyNote title={t("feed.empty", { days: windowDays })} />
          {latestPost && !account ? (
            <div className="post-last">
              <p className="kicker">{t("feed.lastSeen")}</p>
              <PostNote
                post={latestPost}
                avatarUrl={avatarUrl}
                locale={locale}
                now={now}
                t={t}
                onOpenMedia={onOpenMedia}
              />
            </div>
          ) : null}
        </div>
      )}
    </section>
  )
}

/** The post archive's account filter, counting the month's posts per account. */
export function PostArchiveFilter({
  accounts,
  account,
  setAccount,
  counts,
  t,
}: {
  accounts: string[]
  account: string | null
  setAccount: (account: string | null) => void
  counts: Record<string, number> | undefined
  t: Translator
}) {
  if (accounts.length < 2) return null
  const countFor = (handle: string) =>
    counts ? (counts[handle.toLowerCase()] ?? 0) : null
  const all = counts
    ? accounts.reduce((sum, handle) => sum + (countFor(handle) || 0), 0)
    : null
  return (
    <div className="segmented" role="group" aria-label={t("feed.filter")}>
      <button
        type="button"
        aria-pressed={!account}
        onClick={() => setAccount(null)}
      >
        {t("common.all")}
        {all != null ? <small>{all}</small> : null}
      </button>
      {accounts.map((handle) => (
        <button
          type="button"
          key={handle}
          aria-pressed={account?.toLowerCase() === handle.toLowerCase()}
          onClick={() => setAccount(handle)}
        >
          @{handle}
          {counts ? <small>{countFor(handle)}</small> : null}
        </button>
      ))}
    </div>
  )
}

/** The post archive dialog's body: one month of stored posts, newest first. */
export function PostArchive({
  archive,
  isLoading,
  error,
  onRetry,
  onLoadMore,
  onJump,
  avatarUrl,
  locale,
  now,
  t,
  onOpenMedia,
}: {
  archive: ArchiveSnapshot<Post> | null
  isLoading: boolean
  error: Error | null
  onRetry: () => unknown
  onLoadMore: () => unknown
  onJump: (month: Date) => void
  avatarUrl: string | null
  locale: Locale
  now: number
  t: Translator
  onOpenMedia: (media: MediaItem[], index: number) => void
}) {
  return (
    <>
      <ArchiveState
        error={error}
        onRetry={onRetry}
        loading={!archive && isLoading}
        empty={Boolean(archive && !archive.items.length)}
        emptyTitle={t("feed.archiveEmpty")}
        adjacent={archive?.adjacent}
        onJump={onJump}
        locale={locale}
        t={t}
      />
      {archive?.items.length ? (
        <div className="post-wall is-masonry">
          {archive.items.map((post) => (
            <PostNote
              key={post.id}
              post={post}
              avatarUrl={avatarUrl}
              absoluteTime
              clamp={false}
              locale={locale}
              now={now}
              t={t}
              onOpenMedia={onOpenMedia}
            />
          ))}
        </div>
      ) : null}
      {archive ? (
        <ArchiveMore
          hasNext={archive.hasNext}
          isLoading={isLoading}
          label={t("feed.moreInMonth")}
          onLoadMore={onLoadMore}
          t={t}
        />
      ) : null}
    </>
  )
}
