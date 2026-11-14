import type {
  ArchiveSnapshot,
  Locale,
  MediaItem,
  Post,
  PostQuote,
  RichSegment,
  Translator,
} from "@/types"
import type { CSSProperties, ReactElement, ReactNode, RefObject } from "react"
import { useLayoutEffect, useMemo, useRef, useState } from "react"
import {
  ArrowUpRight,
  Heart,
  History,
  ImageOff,
  LayoutDashboard,
  MessageCircle,
  MessageSquareOff,
  Play,
  Repeat2,
  Rows3,
  X,
} from "lucide-react"

import type { TranslatedTextState } from "@/components/translated-text"
import {
  TranslationToggle,
  useTranslatedText,
} from "@/components/translated-text"
import { EmptyNote, Placeholder, SectionTitle } from "@/site/site-shell"
import { BilibiliLogo, ExternalLink, XLogo, followCardLink } from "@/site/links"
import { isMediaUrl, mediaSrcSet } from "@/lib/media"
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
import { SheetDialog } from "./sheet-dialog"

// A post's images fill a card of the post grid or the archive's masonry
// wall; a lone image can take the whole card.
const POST_MEDIA_SIZES =
  "(max-width: 640px) 100vw, (max-width: 1200px) 34vw, 420px"

/** A video's or GIF's still, which opens its post on X instead of the viewer. */
export function isPlayable(item: Pick<MediaItem, "kind">) {
  return item.kind === "video" || item.kind === "gif"
}

/** The accessible name of a video's or GIF's link. */
export function playLabel(item: Pick<MediaItem, "kind">, t: Translator) {
  return t(item.kind === "gif" ? "feed.playGif" : "feed.playVideo")
}

/** A set for the viewer, its videos and GIFs linked to the page they play on. */
function linkedMedia(media: MediaItem[], href: string): MediaItem[] {
  return media.map((item) => (isPlayable(item) ? { ...item, href } : item))
}

/** The mark over a video's or GIF's still: a play button or a GIF tag. */
export function MediaKindMark({ kind }: { kind?: MediaItem["kind"] }) {
  if (kind === "video")
    return (
      <span className="media-kind-mark is-video" aria-hidden="true">
        <Play />
      </span>
    )
  if (kind === "gif")
    return (
      <span className="media-kind-mark is-gif" aria-hidden="true">
        GIF
      </span>
    )
  return null
}

/**
 * One image of a post in the viewer. A video's or GIF's still carries its
 * mark and opens the page it plays on.
 */
export function PostMediaView({
  item,
  t,
}: {
  item: Pick<MediaItem, "url" | "alt" | "kind" | "href">
  t: Translator
}) {
  const image = <img src={item.url || undefined} alt={item.alt || ""} />
  return item.href && isPlayable(item) ? (
    <ExternalLink
      className="media-dialog-play"
      href={item.href}
      aria-label={playLabel(item, t)}
    >
      {image}
      <MediaKindMark kind={item.kind} />
    </ExternalLink>
  ) : (
    image
  )
}

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

/**
 * The wording shown: a translation as text, or the original with its
 * stickers inline at text height, each a cached local picture named by its
 * caption, or the caption itself until the picture is cached.
 */
export function PostWording({
  wording,
  rich,
}: {
  wording: TranslatedTextState
  rich?: RichSegment[] | null
}) {
  if (wording.shown || !rich?.length) return <>{wording.text}</>
  return (
    <>
      {rich.map((segment, index) => {
        if ("text" in segment) return segment.text
        return segment.url && isMediaUrl(segment.url) ? (
          <img
            key={index}
            className="post-sticker"
            src={segment.url}
            alt={segment.emoji}
            title={segment.emoji}
            loading="lazy"
          />
        ) : (
          segment.emoji
        )
      })}
    </>
  )
}

/** Whether there is wording to show: text, or stickers in the original. */
function hasWording(wording: TranslatedTextState, rich?: RichSegment[] | null) {
  return Boolean(wording.text) || (!wording.shown && Boolean(rich?.length))
}

/**
 * Whether a post is a Bilibili dynamic: it names its source instead of a
 * handle and has no engagement counts. A forward quotes the dynamic it
 * forwards.
 */
export function isBilibiliPost(post: Pick<Post, "source">) {
  return post.source === "bilibili"
}

/** One account the post filters offer: an X handle or a Bilibili mid. */
interface PostAccount {
  handle: string
  bilibili: boolean
}

/** The accounts in filter order: the X handles, then the Bilibili mids. */
function postAccounts(xAccounts: string[], bilibiliAccounts: string[]) {
  return [
    ...xAccounts.map((handle) => ({ handle, bilibili: false })),
    ...bilibiliAccounts.map((handle) => ({ handle, bilibili: true })),
  ]
}

/** A filter's name: the @handle, or the bilibili mark for a Bilibili mid. */
function PostAccountLabel({ account }: { account: PostAccount }) {
  return account.bilibili ? (
    <>
      <BilibiliLogo className="account-mark" aria-hidden="true" />
      bilibili
    </>
  ) : (
    <>@{account.handle}</>
  )
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
 * small thumbnails. The card follows the quoted post's link (its x.com page,
 * or the forwarded dynamic's Bilibili page), while its thumbnails and
 * translation switch keep their own actions. A quote of a tracked account's
 * post shows that post's translation. A forwarded dynamic's author shows by
 * name beside the bilibili mark, never by mid.
 */
function QuoteCard({
  quote,
  bilibili,
  wording,
  absoluteTime,
  clamp,
  textRef,
  locale,
  now,
  t,
  onOpenMedia,
}: {
  quote: PostQuote
  /** The quote of a Bilibili forward. */
  bilibili: boolean
  /** The note keeps the quote's switch, shared by the board and the dialog. */
  wording: TranslatedTextState
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
  const media = linkedMedia(
    quote.media.filter((item) => item.url),
    quote.url,
  )
  const shown = clamp ? media.slice(0, QUOTE_THUMBS) : media
  const hidden = media.length - shown.length
  const name = bilibili
    ? quote.authorName || "bilibili"
    : quote.authorName || (quote.authorHandle ? `@${quote.authorHandle}` : "X")
  return (
    <div
      className={`post-quote link-card${clamp ? "" : " is-full"}`}
      onClick={followCardLink}
    >
      <p className="post-quote-head">
        {bilibili ? (
          <BilibiliLogo className="post-quote-mark" aria-hidden="true" />
        ) : null}
        <ExternalLink
          className="card-link post-quote-name"
          href={quote.url}
          aria-label={t(
            bilibili ? "feed.openQuoteBilibili" : "feed.openQuote",
            {
              name,
            },
          )}
        >
          {name}
        </ExternalLink>
        {!bilibili && quote.authorName && quote.authorHandle ? (
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
        <TranslationToggle state={wording} t={t} />
      </p>
      {quote.text || hasWording(wording, quote.rich) ? (
        <p
          ref={textRef}
          className={`post-quote-text${clamp ? " is-clamped" : ""}`}
          lang={wording.lang}
        >
          <PostWording wording={wording} rich={quote.rich} />
        </p>
      ) : null}
      {shown.length ? (
        <div className="post-quote-media">
          {shown.map((item, index) => {
            // A quoted video or GIF plays on X, from the quote's page. The
            // tile that counts the hidden images opens the viewer, which
            // reaches them, whatever it shows.
            const more = hidden > 0 && index === shown.length - 1
            const content = (
              <>
                <img
                  src={item.url || undefined}
                  srcSet={mediaSrcSet(item.url, item.width)}
                  sizes={clamp ? "72px" : "(max-width: 640px) 30vw, 160px"}
                  alt={item.alt || ""}
                  loading="lazy"
                />
                <MediaKindMark kind={item.kind} />
                {more ? (
                  <span className="post-quote-more" aria-hidden="true">
                    +{hidden}
                  </span>
                ) : null}
              </>
            )
            return isPlayable(item) && !more ? (
              <ExternalLink
                className="media-opener is-playable"
                key={item.id || item.url}
                href={quote.url}
                aria-label={`${playLabel(item, t)} ${index + 1}`}
              >
                {content}
              </ExternalLink>
            ) : (
              <button
                className="media-opener"
                type="button"
                key={item.id || item.url}
                onClick={() => onOpenMedia(media, index)}
                aria-label={`${t("feed.openQuoteMedia")} ${index + 1}`}
              >
                {content}
              </button>
            )
          })}
        </div>
      ) : null}
    </div>
  )
}

/** The reposting accounts as one list in the locale's form ("@a and @b"). */
export function repostingAccounts(accounts: string[], locale: Locale) {
  const handles = accounts.map((account) => `@${account.replace(/^@/u, "")}`)
  return new Intl.ListFormat(locale, { type: "conjunction" }).format(handles)
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
  const media = linkedMedia(postMedia(post), post.url)
  const ready = media.filter((item) => item.url)
  const bilibili = isBilibiliPost(post)
  const wording = useTranslatedText(
    post.text,
    post.translations,
    post.sourceLanguage,
  )
  const quoteWording = useTranslatedText(
    post.quote?.text || "",
    post.quote?.translations,
    post.quote?.sourceLanguage,
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
  }, [clamp, wording.text, quoteWording.text])
  const quote = (full: boolean) =>
    post.quote ? (
      <QuoteCard
        quote={post.quote}
        bilibili={bilibili}
        wording={quoteWording}
        absoluteTime={absoluteTime}
        clamp={clamp && !full}
        textRef={full ? undefined : quoteRef}
        locale={locale}
        now={now}
        t={t}
        onOpenMedia={onOpenMedia}
      />
    ) : null

  const originalLabel = t(
    bilibili ? "feed.originalPostBilibili" : "feed.originalPost",
  )
  const engagement = bilibili ? null : (
    <Engagement post={post} locale={locale} t={t} />
  )
  const reposted = post.repostedBy?.length ? (
    <p className="post-note-reposted">
      <Repeat2 aria-hidden="true" />
      {t("feed.repostedBy", {
        accounts: repostingAccounts(post.repostedBy, locale),
      })}
    </p>
  ) : null
  const head = (close?: ReactNode) => (
    <header className="post-note-head">
      {post.promoted ? (
        // Another account's post shown for Kano: its initial, never her
        // portrait.
        <span className="post-note-initial" aria-hidden="true">
          {(post.accountHandle || "X").replace(/^@/u, "").charAt(0)}
        </span>
      ) : (
        <img src={avatarUrl || undefined} alt="" />
      )}
      {bilibili ? (
        <ExternalLink
          className="post-note-handle is-source"
          href={post.accountUrl || post.url}
        >
          <BilibiliLogo aria-hidden="true" />
          bilibili
        </ExternalLink>
      ) : post.accountHandle ? (
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
        aria-label={originalLabel}
        title={originalLabel}
      >
        <ArrowUpRight aria-hidden="true" />
      </ExternalLink>
      {close}
    </header>
  )
  const mediaGrid = media.length ? (
    <div className={`post-note-media count-${Math.min(4, media.length)}`}>
      {media.slice(0, 4).map((item, index) =>
        item.url && isPlayable(item) ? (
          // A video or GIF plays on X; its still never opens the viewer.
          <ExternalLink
            className="media-opener is-playable"
            key={item.id || item.url}
            href={post.url}
            aria-label={`${playLabel(item, t)} ${index + 1}`}
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
            <MediaKindMark kind={item.kind} />
          </ExternalLink>
        ) : item.url ? (
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
      {reposted}
      {head()}
      {hasWording(wording, post.rich) ? (
        <p
          ref={textRef}
          className={`post-note-text${clamp ? " is-clamped" : ""}`}
          lang={wording.lang}
        >
          <PostWording wording={wording} rich={post.rich} />
          <TranslationToggle
            state={wording}
            t={t}
            className={clamp && textClipped ? "is-clipped" : undefined}
          />
        </p>
      ) : null}
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
      {engagement}
      {reading ? (
        <SheetDialog
          label={t("feed.fullPost")}
          onClose={() => setReading(false)}
        >
          <article className="post-note is-full">
            {reposted}
            {head(
              <button
                type="button"
                className="round-button sheet-dialog-close"
                onClick={() => setReading(false)}
                aria-label={t("common.close")}
                title={t("common.close")}
              >
                <X aria-hidden="true" />
              </button>,
            )}
            {hasWording(wording, post.rich) ? (
              <p className="post-note-text" lang={wording.lang}>
                <PostWording wording={wording} rich={post.rich} />
                <TranslationToggle state={wording} t={t} />
              </p>
            ) : null}
            {mediaGrid}
            {quote(true)}
            {engagement}
          </article>
        </SheetDialog>
      ) : null}
    </article>
  )
}

export function PostWall({
  posts,
  latestPost,
  accounts,
  bilibiliAccounts = [],
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
  /** The mids of stored Bilibili posts, filtered as one more account. */
  bilibiliAccounts?: string[]
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
  const filters = postAccounts(accounts, bilibiliAccounts)
  const hasBilibili =
    bilibiliAccounts.length > 0 ||
    [...posts, latestPost].some((post) => post && isBilibiliPost(post))

  return (
    <section className="section posts" id="feed" aria-labelledby="posts-title">
      <SectionTitle
        id="posts-title"
        kicker={
          <>
            <XLogo className="kicker-icon" aria-hidden="true" />{" "}
            {accounts.map((handle) => `@${handle}`).join(" · ")}
            {hasBilibili ? (
              <>
                {" · "}
                <BilibiliLogo className="kicker-icon" aria-hidden="true" />{" "}
                bilibili
              </>
            ) : null}
          </>
        }
        title={t("feed.title")}
      >
        {filters.length > 1 ? (
          <div className="segmented" role="group" aria-label={t("feed.filter")}>
            <button
              type="button"
              aria-pressed={!account}
              onClick={() => setAccount(null)}
            >
              {t("common.all")}
              <small>{posts.length}</small>
            </button>
            {filters.map((item) => (
              <button
                type="button"
                key={item.handle}
                aria-pressed={account === item.handle}
                onClick={() => setAccount(item.handle)}
              >
                <PostAccountLabel account={item} />
                <small>{countFor(item.handle)}</small>
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

/**
 * The post archive's account filter, counting the month's posts per account
 * (keyed by lowercase handle or mid). All counts the month's total, which
 * includes promoted posts filed under their authors' handles.
 */
export function PostArchiveFilter({
  accounts,
  bilibiliAccounts = [],
  account,
  setAccount,
  counts,
  total,
  t,
}: {
  accounts: string[]
  bilibiliAccounts?: string[]
  account: string | null
  setAccount: (account: string | null) => void
  counts: Record<string, number> | undefined
  total?: number | null
  t: Translator
}) {
  const filters = postAccounts(accounts, bilibiliAccounts)
  if (filters.length < 2) return null
  const countFor = (handle: string) =>
    counts ? (counts[handle.toLowerCase()] ?? 0) : null
  const all =
    total ??
    (counts
      ? filters.reduce((sum, item) => sum + (countFor(item.handle) || 0), 0)
      : null)
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
      {filters.map((item) => (
        <button
          type="button"
          key={item.handle}
          aria-pressed={account?.toLowerCase() === item.handle.toLowerCase()}
          onClick={() => setAccount(item.handle)}
        >
          <PostAccountLabel account={item} />
          {counts ? <small>{countFor(item.handle)}</small> : null}
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

/**
 * The cards a month shows while its posts load: their text lines and whether
 * a picture follows, varied so the wall reads as posts of different lengths.
 */
const ARCHIVE_SKELETON = [
  { lines: 3, media: true },
  { lines: 2, media: false },
  { lines: 4, media: false },
  { lines: 2, media: false },
  { lines: 3, media: true },
  { lines: 5, media: false },
  { lines: 3, media: false },
  { lines: 2, media: true },
  { lines: 4, media: false },
]

/** One stand-in card: a name line, text lines and maybe a picture. */
function PostSkeleton({ lines, media }: { lines: number; media: boolean }) {
  return (
    <div className="post-note is-skeleton" aria-hidden="true">
      <div className="placeholder">
        <span style={{ width: "38%" }} />
        {Array.from({ length: lines }, (_, index) => (
          <span
            key={index}
            style={{ width: index === lines - 1 ? "56%" : "92%" }}
          />
        ))}
      </div>
      {media ? <span className="post-skeleton-media" /> : null}
    </div>
  )
}

/**
 * The post archive dialog's body: one month of stored posts, newest first.
 * Until a month's first page arrives, skeleton cards hold the wall's place
 * in the chosen layout.
 */
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
  const cells =
    !archive && isLoading
      ? ARCHIVE_SKELETON.map((card, index) => (
          <PostSkeleton key={`skeleton-${index}`} {...card} />
        ))
      : notes
  return (
    <>
      <ArchiveState
        error={error}
        onRetry={onRetry}
        loading={false}
        empty={Boolean(archive && !archive.items.length)}
        emptyTitle={t("feed.archiveEmpty")}
        adjacent={archive?.adjacent}
        onJump={onJump}
        locale={locale}
        t={t}
      />
      {!cells.length ? null : layout === "list" ? (
        <div className="post-wall is-archive is-list">{cells}</div>
      ) : (
        <PostMasonry>{cells}</PostMasonry>
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
