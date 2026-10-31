import type {
  ArchiveSnapshot,
  Locale,
  MediaItem,
  Post,
  PostQuote,
  Translator,
} from "@/types"
import type { CSSProperties, ReactElement, ReactNode, RefObject } from "react"
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import {
  ArrowUpRight,
  Heart,
  History,
  ImageOff,
  LayoutDashboard,
  MessageCircle,
  MessageSquareOff,
  Repeat2,
  Rows3,
  X,
} from "lucide-react"

import {
  TranslationToggle,
  useTranslatedText,
} from "@/components/translated-text"
import { EmptyNote, Placeholder, SectionTitle } from "@/site/site-shell"
import { ExternalLink, XLogo, followCardLink } from "@/site/links"
import { mediaSrcSet } from "@/lib/media"
import {
  formatCompact,
  formatJapanDay,
  formatJapanTime,
  formatRelative,
  formatStamp,
} from "../format"
import type { MasonryLayout } from "../post-rows"
import {
  MASONRY_MAX_COLUMNS,
  masonryColumns,
  masonryLayout,
  postWallCount,
} from "../post-rows"
import { ArchiveMore, ArchiveState } from "./archive-dialog"

// A post's images fill a card of the post grid or the archive's masonry
// wall; a lone image can take the whole card.
const POST_MEDIA_SIZES =
  "(max-width: 640px) 100vw, (max-width: 1200px) 34vw, 420px"

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

// A board quote shows three thumbnails; "+N" counts the rest.
const QUOTE_THUMBS = 3

/**
 * The status a post quotes, nested in the note: author, time, wording and
 * small thumbnails. The card follows the quoted post's x.com link, while its
 * thumbnails open the image viewer. Quoted wording is shown as written.
 */
function QuoteCard({
  quote,
  absoluteTime,
  clamp,
  textRef,
  locale,
  now,
  t,
  onOpenMedia,
}: {
  quote: PostQuote
  absoluteTime: boolean
  /** The board clamps the wording; the full post shows all of it. */
  clamp: boolean
  textRef?: RefObject<HTMLParagraphElement | null>
  locale: Locale
  now: number
  t: Translator
  onOpenMedia: (media: MediaItem[], index: number) => void
}) {
  if (quote.unavailable) {
    return (
      <ExternalLink className="post-quote is-unavailable" href={quote.url}>
        <MessageSquareOff aria-hidden="true" />
        <span>{t("feed.quoteUnavailable")}</span>
        <ArrowUpRight aria-hidden="true" />
      </ExternalLink>
    )
  }
  const media = quote.media.filter((item) => item.url)
  const shown = clamp ? media.slice(0, QUOTE_THUMBS) : media
  const hidden = media.length - shown.length
  const name =
    quote.authorName || (quote.authorHandle ? `@${quote.authorHandle}` : "X")
  return (
    <div
      className={`post-quote link-card${clamp ? "" : " is-full"}`}
      onClick={followCardLink}
    >
      <p className="post-quote-head">
        <ExternalLink
          className="card-link post-quote-name"
          href={quote.url}
          aria-label={t("feed.openQuote", { name })}
        >
          {name}
        </ExternalLink>
        {quote.authorName && quote.authorHandle ? (
          <span className="post-quote-handle">@{quote.authorHandle}</span>
        ) : null}
        {quote.publishedAt ? (
          <>
            <span aria-hidden="true">·</span>
            <time
              dateTime={quote.publishedAt}
              title={`${formatJapanDay(quote.publishedAt, locale)} ${formatJapanTime(quote.publishedAt)} ${t("common.timezoneShort")}`}
            >
              {absoluteTime
                ? formatStamp(quote.publishedAt)
                : formatRelative(quote.publishedAt, locale, now)}
            </time>
          </>
        ) : null}
      </p>
      {quote.text ? (
        <p
          ref={textRef}
          className={`post-quote-text${clamp ? " is-clamped" : ""}`}
        >
          {quote.text}
        </p>
      ) : null}
      {shown.length ? (
        <div className="post-quote-media">
          {shown.map((item, index) => (
            <button
              className="media-opener"
              type="button"
              key={item.id || item.url}
              onClick={() => onOpenMedia(media, index)}
              aria-label={`${t("feed.openQuoteMedia")} ${index + 1}`}
            >
              <img
                src={item.url || undefined}
                srcSet={mediaSrcSet(item.url, item.width)}
                sizes={clamp ? "72px" : "(max-width: 640px) 30vw, 160px"}
                alt={item.alt || ""}
                loading="lazy"
              />
              {hidden > 0 && index === shown.length - 1 ? (
                <span className="post-quote-more" aria-hidden="true">
                  +{hidden}
                </span>
              ) : null}
            </button>
          ))}
        </div>
      ) : null}
    </div>
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
  )
  const textRef = useRef<HTMLParagraphElement>(null)
  const quoteRef = useRef<HTMLParagraphElement>(null)
  const [overflows, setOverflows] = useState(false)
  const [textClipped, setTextClipped] = useState(false)
  const [reading, setReading] = useState(false)
  // "Read more" appears only while the clamp hides some of the post's or
  // the quote's wording; the translation switch moves beside it while the
  // clamp hides the end of the post's own wording.
  useLayoutEffect(() => {
    const elements = [textRef.current, quoteRef.current].filter(
      (element): element is HTMLParagraphElement => Boolean(element),
    )
    if (!elements.length || !clamp) return
    const clipped = (element: HTMLParagraphElement | null) =>
      Boolean(element && element.scrollHeight > element.clientHeight + 1)
    const check = () => {
      setOverflows(elements.some(clipped))
      setTextClipped(clipped(textRef.current))
    }
    check()
    const observer = new ResizeObserver(check)
    for (const element of elements) observer.observe(element)
    return () => observer.disconnect()
  }, [clamp, wording.text, post.quote?.text])
  const quote = (full: boolean) =>
    post.quote ? (
      <QuoteCard
        quote={post.quote}
        absoluteTime={absoluteTime}
        clamp={clamp && !full}
        textRef={full ? undefined : quoteRef}
        locale={locale}
        now={now}
        t={t}
        onOpenMedia={onOpenMedia}
      />
    ) : null

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
            className="media-opener"
            type="button"
            key={item.id || item.url}
            onClick={() => onOpenMedia(ready, ready.indexOf(item))}
            aria-label={`${t("feed.openMedia")} ${index + 1}`}
          >
            <img
              src={item.url}
              srcSet={mediaSrcSet(item.url, item.width)}
              sizes={POST_MEDIA_SIZES}
              width={item.width ?? undefined}
              height={item.height ?? undefined}
              alt={item.alt || ""}
              loading="lazy"
            />
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
        <TranslationToggle
          state={wording}
          t={t}
          className={clamp && textClipped ? "is-clipped" : undefined}
        />
      </p>
      {clamp && overflows ? (
        <div className="post-note-more">
          <button
            type="button"
            className="post-note-expand"
            aria-haspopup="dialog"
            onClick={() => setReading(true)}
          >
            {t("feed.readMore")}
          </button>
          {textClipped ? <TranslationToggle state={wording} t={t} /> : null}
        </div>
      ) : null}
      {mediaGrid}
      {quote(false)}
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
              <TranslationToggle state={wording} t={t} />
            </p>
            {mediaGrid}
            {quote(true)}
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
  // The board shows whole rows of the wall's current column count (see
  // postWallCount); older posts stay in the Past posts dialog.
  const [wall, setWall] = useState<HTMLDivElement | null>(null)
  const [columns, setColumns] = useState(3)
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
  const shown = visible.slice(0, postWallCount(visible.length, columns))

  return (
    <section className="section posts" id="feed" aria-labelledby="posts-title">
      <SectionTitle
        id="posts-title"
        kicker={
          <>
            <XLogo className="kicker-icon" aria-hidden="true" />{" "}
            {accounts.map((handle) => `@${handle}`).join(" · ")}
          </>
        }
        title={t("feed.title")}
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
          className="pill-button is-collapsible"
          onClick={() => onOpenArchive(account)}
          aria-haspopup="dialog"
          aria-label={t("feed.archive")}
          title={t("feed.archive")}
        >
          <History aria-hidden="true" />
          <span className="pill-label">{t("feed.archive")}</span>
        </button>
      </SectionTitle>

      {isLoading ? (
        <Placeholder lines={5} />
      ) : visible.length ? (
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

export type PostArchiveLayout = "masonry" | "list"

/** The post archive's choice between the masonry wall and a single column. */
export function PostArchiveLayoutToggle({
  layout,
  setLayout,
  t,
}: {
  layout: PostArchiveLayout
  setLayout: (layout: PostArchiveLayout) => void
  t: Translator
}) {
  const options = [
    { id: "masonry", icon: LayoutDashboard, label: t("feed.layoutMasonry") },
    { id: "list", icon: Rows3, label: t("feed.layoutList") },
  ] as const
  return (
    <div
      className="segmented is-icons"
      role="group"
      aria-label={t("feed.layout")}
    >
      {options.map(({ id, icon: Icon, label }) => (
        <button
          type="button"
          key={id}
          aria-pressed={layout === id}
          aria-label={label}
          title={label}
          onClick={() => setLayout(id)}
        >
          <Icon aria-hidden="true" />
        </button>
      ))}
    </div>
  )
}

/**
 * Full posts in up to three columns. Each post goes under the column that
 * ends highest, so the wall reads row by row; the cells stay in post order
 * for keyboard and screen readers, and the layout follows their measured
 * heights as the wall resizes or a post's wording changes.
 */
function PostMasonry({ children }: { children: ReactElement[] }) {
  const [wall, setWall] = useState<HTMLDivElement | null>(null)
  const [layout, setLayout] = useState<
    (MasonryLayout & { count: number }) | null
  >(null)
  const keys = children.map((child) => child.key).join()
  useLayoutEffect(() => {
    if (!wall) return
    const measure = () => {
      const count = masonryColumns(wall.clientWidth)
      // Cell widths follow the column count, so set it before measuring.
      wall.style.setProperty("--masonry-columns", String(count))
      const cells = Array.from(wall.children) as HTMLElement[]
      const next = {
        count,
        ...masonryLayout(
          cells.map((cell) => cell.offsetHeight),
          count,
        ),
      }
      setLayout((current) =>
        current &&
        current.count === next.count &&
        current.height === next.height &&
        current.tops.join() === next.tops.join() &&
        current.columns.join() === next.columns.join()
          ? current
          : next,
      )
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(wall)
    for (const cell of Array.from(wall.children)) observer.observe(cell)
    return () => observer.disconnect()
  }, [wall, keys])
  return (
    <div
      className="post-wall is-archive is-masonry"
      ref={setWall}
      style={
        {
          "--masonry-columns": layout?.count ?? MASONRY_MAX_COLUMNS,
          height: layout?.height,
        } as CSSProperties
      }
    >
      {children.map((child, index) => (
        <div
          className="post-masonry-cell"
          key={child.key ?? index}
          style={
            {
              "--masonry-column": layout?.columns[index] ?? 0,
              top: layout?.tops[index] ?? 0,
            } as CSSProperties
          }
        >
          {child}
        </div>
      ))}
    </div>
  )
}

/** The post archive dialog's body: one month of stored posts, newest first. */
export function PostArchive({
  archive,
  layout,
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
  layout: PostArchiveLayout
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
  const notes = (archive?.items || []).map((post) => (
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
  ))
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
      {!notes.length ? null : layout === "list" ? (
        <div className="post-wall is-archive is-list">{notes}</div>
      ) : (
        <PostMasonry>{notes}</PostMasonry>
      )}
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
