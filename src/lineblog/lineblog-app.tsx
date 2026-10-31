import type {
  CSSProperties,
  MouseEvent,
  ReactNode,
  SyntheticEvent,
} from "react"
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from "react"
import { flushSync } from "react-dom"
import { ArrowUp, ChevronDown, ImageOff, Languages, Play } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { cn } from "@/lib/utils"
import { mediaSrcSet, mediaVariantUrl } from "@/lib/media"
import { ExternalLink, YouTubeLogo } from "@/site/links"
import { MediaDialog, stepIndex } from "@/site/media-dialog"
import {
  Placeholder,
  SiteFooter,
  SiteHeader,
  SkipLink,
} from "@/site/site-shell"
import {
  archiveMonths,
  articleHref,
  isEmoji,
  lineblogView,
  loadedPost,
  meaningfulAlt,
  media,
  mediaUrl,
  neighbours,
  pagerPages,
  postDate,
  postExcerpt,
  postImages,
  postMonth,
  posts,
  shortText,
} from "./content"
import {
  chosenDesign,
  designFor,
  designOptions,
  homeIcon,
  type LineblogDesign,
  type LineblogDesignOption,
} from "./designs"
import { loadedComments, loadView, viewLoaded } from "./post-files"
import type {
  LineblogBlock,
  LineblogComment,
  LineblogImage,
  LineblogInline,
  LineblogPost,
  LineblogSummary,
} from "./types"
import "./lineblog.css"

// The page's code waits for the posts its address shows, so the site's
// navigation reveals it, and restores a reload's scroll, with them in place.
await loadView(window.location.search).catch(() => undefined)

const TRANSLATION_KEY = "kano-lineblog-translation"
const DESIGN_KEY = "kano-lineblog-design"

/** The translation each interface language shows; Japanese shows none. */
const TRANSLATIONS = {
  "zh-CN": { blocks: "zh", title: "titleZh", lang: "zh-CN" },
  en: { blocks: "en", title: "titleEn", lang: "en" },
} as const

type Translation = (typeof TRANSLATIONS)[keyof typeof TRANSLATIONS]

/** Whether a post carries any of the translation's lines. */
function hasTranslation(post: LineblogPost, translation: Translation) {
  return (
    Boolean(post[translation.title]) ||
    post.blocks.some(
      (block) => block.type === "p" && Boolean(block[translation.blocks]),
    )
  )
}

/**
 * The same mark as a translated video title: AI while the translation lines
 * show, Original while they are hidden. The menu bar's mark switches every
 * article; an article's own mark switches that article until the menu bar's
 * is used.
 */
function TranslationMark({
  on,
  all = false,
  onToggle,
}: {
  on: boolean
  all?: boolean
  onToggle: () => void
}) {
  const { t } = useAppSettings()
  const label = t(on ? "translation.machineShort" : "translation.original")
  const spoken = on ? t("translation.machine") : label
  const action = t(on ? "translation.showOriginal" : "lineblog.translationOn")
  const scope = all ? `${t("lineblog.allArticles")} · ` : ""
  const hint = on ? `${t("translation.machineHint")} · ` : ""
  return (
    <button
      type="button"
      className={cn("translation-mark", !on && "is-original")}
      title={`${scope}${hint}${action}`}
      aria-label={`${scope}${spoken}: ${action}`}
      onClick={onToggle}
    >
      <Languages aria-hidden="true" />
      {label}
    </button>
  )
}

/** How wide each kind of picture shows (lineblog.css); viewers keep originals. */
const SIZES = {
  article: "(max-width: 660px) calc(100vw - 32px), 600px",
  card: "(max-width: 560px) 76px, 112px",
  video: "(max-width: 720px) calc(100vw - 32px), 660px",
  profile: "(max-width: 880px) 160px, 240px",
  recent: "60px",
}
/** A poster takes one address; this width suits the article column. */
const POSTER_WIDTH = 960

function readTranslation() {
  try {
    return window.localStorage.getItem(TRANSLATION_KEY) !== "off"
  } catch {
    return true
  }
}

function readDesign() {
  try {
    const id = window.localStorage.getItem(DESIGN_KEY)
    return chosenDesign(id) ? id : null
  } catch {
    return null
  }
}

function remember(key: string, value: string | null) {
  try {
    if (value === null) window.localStorage.removeItem(key)
    else window.localStorage.setItem(key, value)
  } catch {
    // The choice then lasts for this visit only.
  }
}

function newEntry() {
  return `${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`
}

function isLineblogPath(pathname: string) {
  return pathname.replace(/\/+$/u, "") === "/lineblog"
}

/** The address of a link to another view of this page, if it is one. */
function viewUrl(target: EventTarget | null) {
  const link = (target as Element | null)?.closest?.("a[href]")
  if (!(link instanceof HTMLAnchorElement) || link.target) return null
  const url = new URL(link.href, window.location.href)
  return url.origin === window.location.origin &&
    isLineblogPath(url.pathname) &&
    !url.hash
    ? url
    : null
}

/**
 * The query picks an article, a month or a page. Moving between them stays
 * in this document: the click pushes an entry, and the view follows once its
 * posts have loaded, keeping the current one on screen meanwhile. Back and
 * forward restore the query before the site's navigation restores the
 * entry's scroll position; a view whose posts are not loaded yet restores
 * it again once they are. Pointing at or focusing a link starts its load.
 */
function useLineblogLocation() {
  const [search, setSearch] = useState(() => window.location.search)
  const ticket = useRef(0)
  useEffect(() => {
    const onPopState = (event: PopStateEvent) => {
      if (!isLineblogPath(window.location.pathname)) return
      const next = window.location.search
      const current = ++ticket.current
      if (viewLoaded(next)) {
        flushSync(() => setSearch(next))
        return
      }
      const top: unknown = event.state?.kanoScroll
      const show = () => {
        if (current !== ticket.current || window.location.search !== next)
          return
        flushSync(() => setSearch(next))
        if (typeof top === "number")
          window.scrollTo({ top, behavior: "instant" })
      }
      void loadView(next).then(show, show)
    }
    window.addEventListener("popstate", onPopState)
    return () => window.removeEventListener("popstate", onPopState)
  }, [])

  const go = useCallback((url: URL) => {
    if (url.href !== window.location.href) {
      window.history.replaceState(
        { ...window.history.state, kanoScroll: window.scrollY },
        "",
      )
      window.history.pushState({ kanoEntry: newEntry() }, "", url.href)
    }
    const current = ++ticket.current
    const show = () => {
      if (current !== ticket.current) return
      flushSync(() => setSearch(url.search))
      const main = document.getElementById("lineblog-content")
      if (main && main.getBoundingClientRect().top < 0)
        main.scrollIntoView({ behavior: "instant" })
    }
    if (viewLoaded(url.search)) show()
    else void loadView(url.search).then(show, show)
  }, [])

  const onClick = useCallback(
    (event: MouseEvent<HTMLElement>) => {
      if (
        event.defaultPrevented ||
        event.button !== 0 ||
        event.metaKey ||
        event.ctrlKey ||
        event.shiftKey ||
        event.altKey
      )
        return
      const url = viewUrl(event.target)
      if (!url) return
      event.preventDefault()
      go(url)
    },
    [go],
  )

  const onIntent = useCallback((event: SyntheticEvent<HTMLElement>) => {
    const url = viewUrl(event.target)
    if (url) loadView(url.search).catch(() => undefined)
  }, [])

  return { search, go, onClick, onIntent }
}

/**
 * LINE BLOG served phones its smartphone template at the same addresses.
 * Phones of the blog's years were 320 to 480 CSS pixels wide upright, and
 * tablets from 600 got the PC page, so a window up to 600 pixels wide gets
 * the smartphone template.
 */
const PHONE_QUERY = "(max-width: 600px)"

function subscribePhone(onChange: () => void) {
  const query = window.matchMedia?.(PHONE_QUERY)
  query?.addEventListener("change", onChange)
  return () => query?.removeEventListener("change", onChange)
}

function usePhoneLayout() {
  return useSyncExternalStore(
    subscribePhone,
    () => window.matchMedia?.(PHONE_QUERY).matches ?? false,
  )
}

/**
 * The view's posts, once they and an article's comments have loaded.
 * Browsers keep a failed chunk load for the document, so a failed view offers
 * a reload rather than a second request.
 */
function useViewPosts(search: string, shown: LineblogSummary[]) {
  const ready = viewLoaded(search)
  const [, setLoaded] = useState(0)
  const [failed, setFailed] = useState<string | null>(null)
  useEffect(() => {
    if (ready) return
    let current = true
    loadView(search).then(
      () => {
        if (current) setLoaded((count) => count + 1)
      },
      () => {
        if (current) setFailed(search)
      },
    )
    return () => {
      current = false
    }
  }, [ready, search])
  return {
    posts: ready ? shown.map((post) => loadedPost(post.id)!) : null,
    failed: !ready && failed === search,
  }
}

function lineblogHref(query: Record<string, string | number> = {}) {
  const params = new URLSearchParams(
    Object.entries(query).map(([key, value]) => [key, String(value)]),
  )
  const text = params.toString()
  return text ? `/lineblog?${text}` : "/lineblog"
}

function articleLink(post: { id: string }) {
  return lineblogHref({ article: post.id })
}

// ---------------------------------------------------------------------------
// Post content
// ---------------------------------------------------------------------------

interface ContentContext {
  openImage: (image: LineblogImage) => void
}

function ContentLink({
  href,
  children,
}: {
  href: string
  children: ReactNode
}) {
  const local = articleHref(href)
  if (local) return <a href={local}>{children}</a>
  return <ExternalLink href={href}>{children}</ExternalLink>
}

function MissingImage({
  image,
  message,
}: {
  image: LineblogImage
  message: string
}) {
  const { t } = useAppSettings()
  const source = image.remote ?? image.source
  return (
    <span className="lb-missing">
      <ImageOff aria-hidden="true" />
      <span>{meaningfulAlt(image.alt) ?? message}</span>
      {meaningfulAlt(image.alt) ? <small>{message}</small> : null}
      {source ? (
        <ExternalLink href={source} className="lb-missing-link">
          {t("lineblog.originalImage")}
        </ExternalLink>
      ) : null}
    </span>
  )
}

function ArchiveImage({
  image,
  context,
  sizes = SIZES.article,
}: {
  image: LineblogImage
  context: ContentContext
  sizes?: string
}) {
  const { t } = useAppSettings()
  const [failed, setFailed] = useState(false)
  const item = image.media ? media.get(image.media) : undefined
  if (!item) {
    return (
      <MissingImage image={image} message={t("lineblog.imageNotArchived")} />
    )
  }
  if (failed) {
    return (
      <MissingImage image={image} message={t("lineblog.imageUnavailable")} />
    )
  }
  const alt = meaningfulAlt(image.alt)
  if (isEmoji(item.name)) {
    return (
      <img
        className="lb-emoji"
        src={mediaUrl(item.name)}
        alt={alt ?? ""}
        width={item.width}
        height={item.height}
        loading="lazy"
        decoding="async"
        onError={() => setFailed(true)}
      />
    )
  }
  return (
    <button
      type="button"
      className="lb-picture media-opener"
      onClick={() => context.openImage(image)}
      aria-label={alt ?? t("lineblog.openImage")}
    >
      <img
        src={mediaUrl(item.name)}
        srcSet={mediaSrcSet(mediaUrl(item.name), item.width)}
        sizes={sizes}
        alt={alt ?? t("lineblog.photo")}
        width={item.width}
        height={item.height}
        loading="lazy"
        decoding="async"
        onError={() => setFailed(true)}
      />
    </button>
  )
}

function Inlines({
  content,
  context,
}: {
  content: LineblogInline[]
  context: ContentContext
}) {
  return content.map((inline, index) => {
    if (inline.type === "text") return inline.text
    if (inline.type === "link") {
      return (
        <ContentLink key={index} href={inline.href}>
          <Inlines content={inline.content} context={context} />
        </ContentLink>
      )
    }
    return <ArchiveImage key={index} image={inline} context={context} />
  })
}

/** A paragraph of only pictures is a centred figure row, as on the blog. */
function isFigureRow(content: LineblogInline[]) {
  return content.every(
    (inline) =>
      (inline.type === "image" && !isEmoji(inline.media)) ||
      (inline.type === "text" && inline.text.trim() === ""),
  )
}

/** A small note under pictures added after the recovery archive. */
function RecoveryNote({ content }: { content: LineblogInline[] }) {
  const { t } = useAppSettings()
  const kind = content.find(
    (inline): inline is LineblogImage =>
      inline.type === "image" && inline.recovery !== undefined,
  )?.recovery
  if (!kind) return null
  return (
    <p className="lb-recovery-note">
      <small>
        {t(kind === "stand-in" ? "lineblog.standIn" : "lineblog.tweetPictures")}
      </small>
    </p>
  )
}

function YoutubeBlock({
  block,
}: {
  block: Extract<LineblogBlock, { type: "youtube" }>
}) {
  const { t } = useAppSettings()
  const [failed, setFailed] = useState(false)
  const video = block.video && media.get(block.video)
  const cover = block.cover && media.get(block.cover)
  return (
    <figure className="lb-video">
      {video && !failed ? (
        <video
          controls
          preload={cover ? "none" : "metadata"}
          poster={
            cover
              ? mediaVariantUrl(mediaUrl(cover.name), POSTER_WIDTH)
              : undefined
          }
          src={mediaUrl(video.name)}
          onError={() => setFailed(true)}
        />
      ) : (
        <div className="lb-video-missing">
          {cover && !failed ? (
            <img
              src={mediaUrl(cover.name)}
              srcSet={mediaSrcSet(mediaUrl(cover.name), cover.width)}
              sizes={SIZES.video}
              alt=""
              loading="lazy"
            />
          ) : null}
          <p>
            <Play aria-hidden="true" />
            {t("lineblog.videoUnavailable")}
          </p>
        </div>
      )}
      <figcaption>
        <ExternalLink href={block.watch} className="lb-youtube-link">
          <YouTubeLogo aria-hidden="true" />
          {t("lineblog.watchOnYouTube")}
        </ExternalLink>
        {block.archive ? (
          <ExternalLink href={block.archive}>
            {t("lineblog.capture")}
          </ExternalLink>
        ) : null}
      </figcaption>
    </figure>
  )
}

function CardBlock({
  block,
  context,
}: {
  block: Extract<LineblogBlock, { type: "card" }>
  context: ContentContext
}) {
  const { t } = useAppSettings()
  const host = (() => {
    try {
      return new URL(block.url).hostname.replace(/^www\./u, "")
    } catch {
      return block.url
    }
  })()
  return (
    <aside className="lb-card">
      {block.image?.media && media.has(block.image.media) ? (
        <div className="lb-card-image">
          <ArchiveImage
            image={block.image}
            context={context}
            sizes={SIZES.card}
          />
        </div>
      ) : null}
      <div className="lb-card-copy">
        <ContentLink href={block.url}>
          <strong>{block.title}</strong>
        </ContentLink>
        {block.description ? <p>{block.description}</p> : null}
        <small>
          {host}
          {block.archive ? (
            <>
              {" · "}
              <ExternalLink href={block.archive}>
                {t("lineblog.capture")}
              </ExternalLink>
            </>
          ) : null}
        </small>
      </div>
    </aside>
  )
}

function Block({
  block,
  context,
  translation,
}: {
  block: LineblogBlock
  context: ContentContext
  translation: Translation | null
}) {
  switch (block.type) {
    case "p": {
      const translated = translation && block[translation.blocks]
      return (
        <>
          {isFigureRow(block.content) ? (
            <div className="lb-figures">
              <Inlines content={block.content} context={context} />
            </div>
          ) : (
            <p>
              <Inlines content={block.content} context={context} />
            </p>
          )}
          <RecoveryNote content={block.content} />
          {translated
            ? translated.map((paragraph, index) => (
                <p
                  key={index}
                  className="lb-translation"
                  lang={translation.lang}
                >
                  <Inlines content={paragraph} context={context} />
                </p>
              ))
            : null}
        </>
      )
    }
    case "quote":
      return (
        <blockquote>
          <Inlines content={block.content} context={context} />
        </blockquote>
      )
    case "hr":
      return <hr />
    case "video": {
      const video = media.get(block.video)
      return video ? (
        <figure className="lb-video">
          <video controls preload="metadata" src={mediaUrl(video.name)} />
        </figure>
      ) : null
    }
    case "youtube":
      return <YoutubeBlock block={block} />
    case "card":
      return <CardBlock block={block} context={context} />
  }
}

function sourceHost(address: string) {
  try {
    return new URL(address).hostname
  } catch {
    return address
  }
}

/**
 * The site a picture's archived bytes came from, as the viewer's small print
 * rather than a link: most of those addresses no longer open.
 */
function SourceNote({ source }: { source: string }) {
  const { t } = useAppSettings()
  return <>{t("lineblog.recoveredFrom", { host: sourceHost(source) })}</>
}

/** Whether a picture has a caption or a link for the viewer's panel. */
function hasImageDetails(image: LineblogImage) {
  return Boolean(meaningfulAlt(image.alt) || image.href)
}

/** The viewer's panel for a picture with a caption or a link. */
function ImageDetails({ image }: { image: LineblogImage }) {
  const { t } = useAppSettings()
  const alt = meaningfulAlt(image.alt)
  return (
    <div className="lb-image-details">
      {alt ? <p>{alt}</p> : null}
      {image.href ? (
        <ExternalLink href={image.href}>
          {t("lineblog.linkInPost")}
        </ExternalLink>
      ) : null}
    </div>
  )
}

function Article({
  post,
  single,
  offered,
  translationOn,
  translationRound,
}: {
  post: LineblogPost
  single: boolean
  offered: Translation | null
  translationOn: boolean
  /** Counts uses of the menu bar's switch. */
  translationRound: number
}) {
  const { t } = useAppSettings()
  const images = useMemo(() => postImages(post.blocks), [post])
  const translated = offered && hasTranslation(post, offered) ? offered : null
  // The article's own switch holds until the menu bar's is used.
  const [own, setOwn] = useState<{ round: number; on: boolean } | null>(null)
  const on = own?.round === translationRound ? own.on : translationOn
  const translation = on ? translated : null
  const translatedTitle = translation && post[translation.title]
  const [open, setOpen] = useState(-1)
  const context = useMemo<ContentContext>(
    () => ({ openImage: (image) => setOpen(images.indexOf(image)) }),
    [images],
  )
  const Title = single ? "h1" : "h2"
  const titleId = `lb-title-${post.id}`
  return (
    <article className="lb-article" aria-labelledby={titleId}>
      <header className="lb-article-header">
        <Title className="lb-article-title" id={titleId} lang="ja">
          {single ? post.title : <a href={articleLink(post)}>{post.title}</a>}
        </Title>
        {translatedTitle && translatedTitle !== post.title ? (
          <p
            className="lb-translation lb-title-translation"
            lang={translation.lang}
          >
            {translatedTitle}
          </p>
        ) : null}
        <p className="lb-article-date">
          <time dateTime={post.published}>{postDate(post.published)}</time>
          {translated ? (
            <TranslationMark
              on={on}
              onToggle={() => setOwn({ round: translationRound, on: !on })}
            />
          ) : null}
        </p>
      </header>
      <div className="lb-article-body" lang="ja">
        {post.blocks.map((block, index) => (
          <Block
            key={index}
            block={block}
            context={context}
            translation={translation}
          />
        ))}
      </div>
      <footer className="lb-article-footer">
        <span>{t("lineblog.provenance")}</span>
        <ExternalLink href={post.url}>
          {t("lineblog.originalAddress")}
        </ExternalLink>
        <ExternalLink href={post.archive}>{t("lineblog.capture")}</ExternalLink>
      </footer>
      <MediaDialog
        items={open >= 0 ? images : null}
        index={Math.max(open, 0)}
        label={t("lineblog.imageViewer")}
        onClose={() => setOpen(-1)}
        onStep={(offset) =>
          setOpen((index) => stepIndex(index, offset, images.length))
        }
        renderImage={(image) => {
          const item = image.media ? media.get(image.media) : undefined
          return item ? (
            <img
              src={mediaUrl(item.name)}
              alt={meaningfulAlt(image.alt) ?? t("lineblog.photo")}
              width={item.width}
              height={item.height}
            />
          ) : null
        }}
        renderDetails={(image) =>
          hasImageDetails(image) ? <ImageDetails image={image} /> : null
        }
        renderNote={(image) =>
          image.source ? <SourceNote source={image.source} /> : null
        }
      />
    </article>
  )
}

// ---------------------------------------------------------------------------
// Page chrome
// ---------------------------------------------------------------------------

function Pager({ page, pageCount }: { page: number; pageCount: number }) {
  const { t } = useAppSettings()
  const numbers = pagerPages(page, pageCount)
  return (
    <nav className="lb-pager" aria-label={t("lineblog.pager")}>
      {page > 1 ? (
        <a
          href={lineblogHref(page === 2 ? {} : { page: page - 1 })}
          aria-label={t("lineblog.previousPage")}
          rel="prev"
        >
          «
        </a>
      ) : null}
      <ol>
        {numbers.map((number) => (
          <li key={number}>
            {number === page ? (
              <span aria-current="page">{number}</span>
            ) : (
              <a
                href={lineblogHref(number === 1 ? {} : { page: number })}
                aria-label={t("lineblog.pageNumber", { page: number })}
              >
                {number}
              </a>
            )}
          </li>
        ))}
      </ol>
      {numbers.at(-1)! < pageCount ? (
        <a
          className="lb-pager-last"
          href={lineblogHref({ page: pageCount })}
          aria-label={t("lineblog.lastPage")}
        >
          {pageCount}
        </a>
      ) : null}
      {page < pageCount ? (
        <a
          href={lineblogHref({ page: page + 1 })}
          aria-label={t("lineblog.nextPage")}
          rel="next"
        >
          »
        </a>
      ) : null}
    </nav>
  )
}

/**
 * The blog's article pager: 前の記事 for the older article and 次の記事 for
 * the newer one, kept in Japanese like the menu bar, with the article's title
 * as the link title.
 */
function ArticlePager({ post }: { post: LineblogSummary }) {
  const { t } = useAppSettings()
  const { newer, older } = neighbours(post.id)
  return (
    <nav className="lb-article-pager" aria-label={t("lineblog.articleNav")}>
      <ul>
        {older ? (
          <li className="is-prev">
            <a
              href={articleLink(older)}
              rel="prev"
              title={older.title}
              aria-label={`${t("lineblog.older")}: ${older.title}`}
              lang="ja"
            >
              前の記事
            </a>
          </li>
        ) : null}
        {newer ? (
          <li className="is-next">
            <a
              href={articleLink(newer)}
              rel="next"
              title={newer.title}
              aria-label={`${t("lineblog.newer")}: ${newer.title}`}
              lang="ja"
            >
              次の記事
            </a>
          </li>
        ) : null}
      </ul>
    </nav>
  )
}

/** The old comment form's face ratings, from 1 (worst) to 5 (best). */
const RATING_FACES = ["😣", "😟", "😐", "🙂", "😊"]

function CommentRating({
  rating,
}: {
  rating: NonNullable<LineblogComment["rating"]>
}) {
  const { t } = useAppSettings()
  return (
    <span
      className={cn("lb-comment-rating", `is-${rating.icon}`)}
      role="img"
      aria-label={t("lineblog.commentRating", { value: rating.value })}
    >
      {rating.icon === "star"
        ? "★".repeat(rating.value) + "☆".repeat(5 - rating.value)
        : RATING_FACES[rating.value - 1]}
    </span>
  )
}

/**
 * The comment list an archived copy of the article page kept, newest first,
 * in the commenters' own words; their blog links are not kept.
 */
function Comments({ comments }: { comments: LineblogComment[] }) {
  const { t } = useAppSettings()
  if (!comments.length) return null
  return (
    <section className="lb-comments" aria-labelledby="lb-comments-title">
      <h2 className="lb-comments-title" id="lb-comments-title">
        {t("lineblog.comments")}
      </h2>
      <p className="lb-comments-note">{t("lineblog.commentsNote")}</p>
      <ol className="lb-comment-list" lang="ja">
        {comments.map((comment) => (
          <li
            key={comment.number}
            className={cn("lb-comment", comment.rating && "has-rating")}
          >
            <p className="lb-comment-head">
              <span className="lb-comment-author">
                {comment.number}.{comment.author}
              </span>
              <time className="lb-comment-date" dateTime={comment.posted}>
                {postDate(comment.posted)}
              </time>
            </p>
            {comment.rating ? <CommentRating rating={comment.rating} /> : null}
            {comment.body ? (
              <p className="lb-comment-body">{comment.body}</p>
            ) : null}
          </li>
        ))}
      </ol>
    </section>
  )
}

function monthLabel(t: ReturnType<typeof useAppSettings>["t"], month: string) {
  return t("lineblog.month", {
    year: month.slice(0, 4),
    month: month.slice(5, 7),
  })
}

function Sidebar({
  current,
  design,
}: {
  current: string | null
  design: LineblogDesign
}) {
  const { t } = useAppSettings()
  const currentYear = current?.slice(0, 4) ?? archiveMonths[0]?.year
  return (
    <aside className="lb-sidebar">
      <section className="lb-profile" aria-label={t("lineblog.profile")}>
        <img
          className="lb-profile-photo"
          src={mediaUrl(design.profile.media)}
          srcSet={mediaSrcSet(mediaUrl(design.profile.media))}
          sizes={SIZES.profile}
          alt=""
          width={240}
          height={240}
          decoding="async"
        />
        <h2 lang="ja">鹿乃</h2>
        <p className="lb-profile-ruby">Kano</p>
        <p className="lb-profile-job" lang="ja">
          ボーカリスト
        </p>
        <p className="lb-profile-text" lang="ja">
          {design.profileText}
        </p>
      </section>
      <section className="lb-side" aria-labelledby="lb-recent">
        <h2 className="lb-side-title" id="lb-recent">
          {t("lineblog.recent")}
        </h2>
        <ul className="lb-recent">
          {posts.slice(0, 10).map((post) => {
            const thumbnail = post.thumbnail
            return (
              <li key={post.id}>
                <a href={articleLink(post)}>
                  {thumbnail ? (
                    <img
                      src={mediaUrl(thumbnail)}
                      srcSet={mediaSrcSet(mediaUrl(thumbnail))}
                      sizes={SIZES.recent}
                      alt=""
                      width={60}
                      height={60}
                      loading="lazy"
                      decoding="async"
                    />
                  ) : (
                    <span className="lb-recent-blank" aria-hidden="true" />
                  )}
                  <span lang="ja">{post.title}</span>
                </a>
              </li>
            )
          })}
        </ul>
      </section>
      <section className="lb-side" aria-labelledby="lb-archive">
        <h2 className="lb-side-title" id="lb-archive">
          {t("lineblog.archive")}
        </h2>
        {archiveMonths.map(({ year, months }) => (
          <details key={year} className="lb-years" open={year === currentYear}>
            <summary>
              {year}
              <small>
                ({months.reduce((sum, item) => sum + item.count, 0)})
              </small>
            </summary>
            <ul>
              {months.map(({ month, count }) => (
                <li key={month}>
                  <a
                    href={lineblogHref({ month })}
                    aria-current={month === current ? "page" : undefined}
                  >
                    {monthLabel(t, month)} ({count})
                  </a>
                </li>
              ))}
            </ul>
          </details>
        ))}
      </section>
    </aside>
  )
}

function designPeriod(
  t: ReturnType<typeof useAppSettings>["t"],
  option: LineblogDesignOption,
) {
  const month = (date: string) => date.slice(0, 7).replace("-", "/")
  // The first design dates from before the blog's first post.
  const oldest = posts.at(-1)?.published.slice(0, 10) ?? option.id
  const from = month(option.id < oldest ? oldest : option.id)
  const to = option.until ? month(option.until) : ""
  // A design shown for part of one month reads as that month.
  if (to === from) return from
  return t("lineblog.designPeriod", { from, to })
}

/**
 * Picks the blog's header art and profile photo: Auto follows the articles
 * shown, as the blog did, and a period keeps that design on every view.
 */
function DesignPicker({
  choice,
  design,
  onChoose,
}: {
  choice: string | null
  design: LineblogDesign
  onChoose: (id: string | null) => void
}) {
  const { t } = useAppSettings()
  const chosen = designOptions.find((option) => option.id === choice)
  const current = chosen ? designPeriod(t, chosen) : t("lineblog.designAuto")
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          className={cn("lb-design-button", chosen && "is-chosen")}
          aria-label={`${t("lineblog.design")}: ${current}`}
          title={t("lineblog.design")}
        >
          <img
            src={mediaVariantUrl(mediaUrl(design.profile.media), 320)}
            alt=""
            width={20}
            height={20}
            decoding="async"
          />
          <span>{current}</span>
          <ChevronDown aria-hidden="true" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        className="lb-design-menu overflow-y-auto"
      >
        <DropdownMenuLabel>{t("lineblog.design")}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuRadioGroup
          value={choice ?? "auto"}
          onValueChange={(value) => onChoose(value === "auto" ? null : value)}
        >
          <DropdownMenuRadioItem value="auto" className="lb-design-item">
            <span className="lb-design-copy">
              <strong>{t("lineblog.designAuto")}</strong>
              <small>{t("lineblog.designAutoHint")}</small>
            </span>
          </DropdownMenuRadioItem>
          {designOptions.map((option) => (
            <DropdownMenuRadioItem
              key={option.id}
              value={option.id}
              className="lb-design-item"
            >
              <img
                className="lb-design-photo"
                src={mediaVariantUrl(
                  mediaUrl(option.design.profile.media),
                  320,
                )}
                alt=""
                width={32}
                height={32}
                loading="lazy"
                decoding="async"
              />
              <span
                className="lb-design-header"
                style={{
                  backgroundImage: `url("${mediaVariantUrl(mediaUrl(option.design.header.media), 640)}")`,
                }}
                aria-hidden="true"
              />
              <span className="lb-design-copy">
                <strong>{designPeriod(t, option)}</strong>
              </span>
              <span
                className="lb-design-accent"
                style={{ background: option.design.accent }}
                aria-hidden="true"
              />
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

// ---------------------------------------------------------------------------
// Smartphone template
// ---------------------------------------------------------------------------

/**
 * The template's one-line profile under the menu bar opens this popup: the
 * profile photo, the name and the whole profile text.
 */
function ProfilePopup({ design }: { design: LineblogDesign }) {
  const { t } = useAppSettings()
  const dialog = useRef<HTMLDialogElement>(null)
  const close = () => dialog.current?.close()
  return (
    <div className="lbl-title-line">
      <button
        type="button"
        className="lbl-short-profile"
        aria-haspopup="dialog"
        aria-label={t("lineblog.profile")}
        onClick={() => dialog.current?.showModal()}
      >
        <span lang="ja">{shortText(design.profileText, 50)}</span>
      </button>
      <dialog
        ref={dialog}
        className="lbl-profile-popup"
        aria-labelledby="lbl-profile-name"
        onClick={(event) => {
          // A click on the backdrop lands on the dialog outside its box.
          const box = event.currentTarget.getBoundingClientRect()
          if (
            event.target === event.currentTarget &&
            (event.clientX < box.left ||
              event.clientX > box.right ||
              event.clientY < box.top ||
              event.clientY > box.bottom)
          )
            close()
        }}
      >
        <button
          type="button"
          className="lbl-close"
          aria-label={t("lineblog.closeProfile")}
          onClick={close}
        />
        <img
          className="lbl-profile-photo"
          src={mediaVariantUrl(mediaUrl(design.profile.media), 320)}
          alt=""
          width={100}
          height={100}
          loading="lazy"
          decoding="async"
        />
        <h2 id="lbl-profile-name" lang="ja">
          鹿乃
        </h2>
        <p className="lbl-profile-text" lang="ja">
          {design.profileText}
        </p>
      </dialog>
    </div>
  )
}

/** An article in the smartphone list: title, picture, opening words, date. */
function ArticleCard({
  summary,
  post,
  translation,
}: {
  summary: LineblogSummary
  post: LineblogPost
  translation: Translation | null
}) {
  const thumbnail = summary.thumbnail
  const item = thumbnail ? media.get(thumbnail) : undefined
  const translatedTitle = translation && post[translation.title]
  const excerpt = postExcerpt(post.blocks)
  return (
    <li className="lbl-list-item">
      <a className="lbl-list-link" href={articleLink(post)}>
        <h2 className="lbl-list-title" lang="ja">
          {post.title}
        </h2>
        {translatedTitle && translatedTitle !== post.title ? (
          <p className="lbl-list-translation" lang={translation.lang}>
            {translatedTitle}
          </p>
        ) : null}
        {thumbnail ? (
          <img
            className="lbl-list-image"
            src={mediaUrl(thumbnail)}
            srcSet={mediaSrcSet(mediaUrl(thumbnail), item?.width)}
            sizes="100vw"
            alt=""
            width={item?.width}
            height={item?.height}
            loading="lazy"
            decoding="async"
          />
        ) : null}
        {excerpt ? (
          <p className="lbl-list-description" lang="ja">
            {excerpt}
          </p>
        ) : null}
      </a>
      <p className="lbl-list-footer">
        <time dateTime={post.published}>{postDate(post.published)}</time>
      </p>
    </li>
  )
}

/**
 * The template's article pager under an article: 前の記事 for the older
 * article, ホーム for the front page and 次の記事 for the newer one.
 */
function SmartphoneArticlePager({ post }: { post: LineblogSummary }) {
  const { t } = useAppSettings()
  const { newer, older } = neighbours(post.id)
  return (
    <nav className="lbl-article-pager" aria-label={t("lineblog.articleNav")}>
      <ul lang="ja">
        <li className="is-prev">
          {older ? (
            <a
              href={articleLink(older)}
              rel="prev"
              title={older.title}
              aria-label={`${t("lineblog.older")}: ${older.title}`}
            >
              前の記事
            </a>
          ) : null}
        </li>
        <li className="is-home">
          <a href="/lineblog" aria-label={t("lineblog.home")}>
            ホーム
          </a>
        </li>
        <li className="is-next">
          {newer ? (
            <a
              href={articleLink(newer)}
              rel="next"
              title={newer.title}
              aria-label={`${t("lineblog.newer")}: ${newer.title}`}
            >
              次の記事
            </a>
          ) : null}
        </li>
      </ul>
    </nav>
  )
}

/**
 * The template's comment list: a count beside the comment icon, then each
 * comment's number and name, its text and its time, newest first.
 */
function SmartphoneComments({ comments }: { comments: LineblogComment[] }) {
  const { t } = useAppSettings()
  if (!comments.length) return null
  return (
    <section
      className="lbl-comments"
      id="lb-comments"
      aria-labelledby="lbl-comments-title"
    >
      <h2 className="lbl-comments-title" id="lbl-comments-title">
        <span className="lbl-comment-icon" aria-hidden="true" />
        <span className="sr-only">{t("lineblog.comments")}</span>
        <span className="lbl-comments-count">{comments.length}</span>
      </h2>
      <p className="lbl-comments-note">{t("lineblog.commentsNote")}</p>
      <ol className="lbl-comment-list" lang="ja">
        {comments.map((comment) => (
          <li
            key={comment.number}
            className={cn("lbl-comment", comment.rating && "has-rating")}
          >
            <p className="lbl-comment-author">
              <span className="lbl-comment-id">{comment.number}.</span>
              {comment.author}
            </p>
            {comment.body ? (
              <p className="lbl-comment-body">{comment.body}</p>
            ) : null}
            <time className="lbl-comment-time" dateTime={comment.posted}>
              {postDate(comment.posted)}
            </time>
            {comment.rating ? <CommentRating rating={comment.rating} /> : null}
          </li>
        ))}
      </ol>
    </section>
  )
}

/**
 * The template's bottom pager: the page out of all pages, then round
 * buttons to the first, previous, next and last pages, dimmed where there is
 * none to go to.
 */
function SmartphonePager({
  page,
  pageCount,
}: {
  page: number
  pageCount: number
}) {
  const { t } = useAppSettings()
  const steps = [
    { kind: "first", to: 1, label: t("lineblog.firstPage") },
    { kind: "prev", to: page - 1, label: t("lineblog.previousPage") },
    { kind: "next", to: page + 1, label: t("lineblog.nextPage") },
    { kind: "last", to: pageCount, label: t("lineblog.lastPage") },
  ]
  return (
    <nav className="lbl-pager" aria-label={t("lineblog.pager")}>
      <p className="lbl-fraction">
        {page} / {pageCount}
      </p>
      <ul>
        {steps.map(({ kind, to, label }) => {
          const open = to >= 1 && to <= pageCount && to !== page
          return (
            <li key={kind} className={cn(`is-${kind}`, !open && "is-off")}>
              {open ? (
                <a
                  href={lineblogHref(to === 1 ? {} : { page: to })}
                  rel={
                    kind === "prev"
                      ? "prev"
                      : kind === "next"
                        ? "next"
                        : undefined
                  }
                >
                  <span className="sr-only">{label}</span>
                </a>
              ) : (
                <span aria-hidden="true" />
              )}
            </li>
          )
        })}
      </ul>
    </nav>
  )
}

/** The template's monthly archive: a pull-down of every month. */
function MonthSelect({
  current,
  onChoose,
}: {
  current: string | null
  onChoose: (month: string) => void
}) {
  const { t } = useAppSettings()
  return (
    <section className="lbl-months">
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            className="lbl-months-button"
            aria-label={
              current
                ? `${t("lineblog.monthArchive")}: ${monthLabel(t, current)}`
                : t("lineblog.monthArchive")
            }
          >
            {current ? monthLabel(t, current) : t("lineblog.monthArchive")}
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="start"
          className="lbl-months-menu overflow-y-auto"
        >
          <DropdownMenuRadioGroup
            // The menu opens at the month shown, once it has its size.
            ref={(group) => {
              if (!group) return
              requestAnimationFrame(() => {
                const menu = group.closest<HTMLElement>(".lbl-months-menu")
                const item = group.querySelector<HTMLElement>(
                  '[data-state="checked"]',
                )
                if (menu && item)
                  menu.scrollTop =
                    item.offsetTop - (menu.clientHeight - item.offsetHeight) / 2
              })
            }}
            value={current ?? ""}
            onValueChange={(month) => {
              if (month !== current) onChoose(month)
            }}
          >
            {archiveMonths.map(({ year, months }) => (
              <div key={year} role="group" aria-label={year}>
                <DropdownMenuLabel className="lbl-months-year">
                  {year}
                </DropdownMenuLabel>
                {months.map(({ month, count }) => (
                  <DropdownMenuRadioItem
                    key={month}
                    value={month}
                    className="lbl-months-item"
                  >
                    <span>{monthLabel(t, month)}</span>
                    <small aria-hidden="true">{count}</small>
                    <span className="sr-only">
                      {t("lineblog.monthCount", { count })}
                    </span>
                  </DropdownMenuRadioItem>
                ))}
              </div>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </section>
  )
}

/**
 * The smartphone template: header art, a menu bar that scrolls sideways, the
 * one-line profile, then the content in white boxes on a grey page and the
 * blog's footer.
 */
function SmartphoneBlog({
  view,
  loaded,
  offered,
  translationOn,
  translationRound,
  onToggleTranslation,
  designChoice,
  design,
  onChooseDesign,
  onChooseMonth,
}: {
  view: ReturnType<typeof lineblogView>
  loaded: ReturnType<typeof useViewPosts>
  offered: Translation | null
  translationOn: boolean
  translationRound: number
  onToggleTranslation: () => void
  designChoice: string | null
  design: LineblogDesign
  onChooseDesign: (id: string | null) => void
  onChooseMonth: (month: string) => void
}) {
  const { t } = useAppSettings()
  const home = !view.article && !view.month && view.page === 1
  const comments = view.article ? loadedComments(view.article.id) : undefined
  return (
    <div className="lb lb-lite">
      <header className="lbl-header">
        <h1 className="lbl-blog-title">
          <a href="/lineblog" lang="ja">
            <span className="sr-only">鹿乃 オフィシャルブログ</span>
          </a>
        </h1>
        <nav className="lbl-menu" aria-label={t("lineblog.menu")}>
          <ul>
            <li className={cn(home && "is-current")}>
              <a
                href="/lineblog"
                lang="ja"
                aria-current={home ? "page" : undefined}
              >
                ホーム
              </a>
            </li>
            <li>
              <ExternalLink href={design.officialSite} lang="ja">
                鹿乃オフィシャルサイト
              </ExternalLink>
            </li>
            <li className="lbl-menu-tools">
              {offered ? (
                <TranslationMark
                  on={translationOn}
                  all
                  onToggle={onToggleTranslation}
                />
              ) : null}
              <DesignPicker
                choice={designChoice}
                design={design}
                onChoose={onChooseDesign}
              />
            </li>
          </ul>
        </nav>
        <ProfilePopup design={design} />
      </header>

      <main className="lbl-content" id="lineblog-content" tabIndex={-1}>
        {view.month ? (
          <h1 className="lbl-archives-title">
            {monthLabel(t, view.month)}
            <span>
              {" "}
              ({t("lineblog.monthCount", { count: view.shown.length })})
            </span>
          </h1>
        ) : null}
        {!view.article && !view.month ? (
          <h1 className="sr-only">{t("lineblog.kicker")}</h1>
        ) : null}
        {view.missingArticle ? (
          <p className="lb-empty lbl-note">
            {t("lineblog.notFound")}{" "}
            <a href="/lineblog">{t("lineblog.backToList")}</a>
          </p>
        ) : null}
        {view.month && !view.shown.length ? (
          <p className="lb-empty lbl-note">{t("lineblog.monthEmpty")}</p>
        ) : null}
        {loaded.posts ? (
          view.article ? (
            loaded.posts.map((post) => (
              <div key={post.id} className="lbl-entry">
                <Article
                  post={post}
                  single
                  offered={offered}
                  translationOn={translationOn}
                  translationRound={translationRound}
                />
                <ul className="lbl-counts">
                  <li>
                    <a
                      href="#lb-comments"
                      className="lbl-comment-count"
                      aria-label={t("lineblog.commentCount", {
                        count: comments?.length ?? 0,
                      })}
                    >
                      {comments?.length ?? 0}
                    </a>
                  </li>
                </ul>
                <SmartphoneArticlePager post={post} />
              </div>
            ))
          ) : loaded.posts.length ? (
            <ul className="lbl-list">
              {loaded.posts.map((post, index) => (
                <ArticleCard
                  key={post.id}
                  summary={view.shown[index]}
                  post={post}
                  translation={translationOn ? offered : null}
                />
              ))}
            </ul>
          ) : null
        ) : loaded.failed ? (
          <div className="lb-loading lbl-note" role="alert">
            <p>{t("lineblog.loadFailed")}</p>
            <button
              type="button"
              className="lb-retry"
              onClick={() => window.location.reload()}
            >
              {t("lineblog.retry")}
            </button>
          </div>
        ) : (
          <div className="lb-loading lbl-note" role="status">
            <p>{t("lineblog.loading")}</p>
            <Placeholder lines={6} />
          </div>
        )}
        {view.article && loaded.posts ? (
          <SmartphoneComments comments={comments ?? []} />
        ) : null}
        {!view.article && !view.month ? (
          <SmartphonePager page={view.page} pageCount={view.pageCount} />
        ) : null}
      </main>

      <MonthSelect current={view.month} onChoose={onChooseMonth} />

      <footer className="lbl-footer">
        <p className="lbl-pagetop">
          <a href="#top" lang="ja" aria-label={t("lineblog.toTop")}>
            ページ先頭へ
          </a>
        </p>
        <p className="lbl-copyright" lang="ja">
          鹿乃 オフィシャルブログ
        </p>
      </footer>
    </div>
  )
}

export function LineblogApp() {
  const { t, locale } = useAppSettings()
  const { search, go, onClick, onIntent } = useLineblogLocation()
  const phone = usePhoneLayout()
  const view = useMemo(() => lineblogView(search), [search])
  const loaded = useViewPosts(search, view.shown)
  const [translationOn, setTranslationOn] = useState(readTranslation)
  const [translationRound, setTranslationRound] = useState(0)
  const [designChoice, setDesignChoice] = useState(readDesign)
  const offered = locale === "ja" ? null : TRANSLATIONS[locale]

  useEffect(() => {
    const title = view.article
      ? `${view.article.title} · ${t("lineblog.metaTitle")}`
      : t("lineblog.metaTitle")
    document.title = title
    document
      .querySelector('meta[name="description"]')
      ?.setAttribute("content", t("lineblog.metaDescription"))
  }, [t, view.article])

  const toggleTranslation = () => {
    const next = !translationOn
    setTranslationOn(next)
    setTranslationRound((round) => round + 1)
    remember(TRANSLATION_KEY, next ? "on" : "off")
  }

  const chooseDesign = (id: string | null) => {
    setDesignChoice(id)
    remember(DESIGN_KEY, id)
  }

  const shownPost = view.article ?? view.shown[0]
  const design =
    chosenDesign(designChoice) ??
    designFor(
      shownPost &&
        postDate(shownPost.published).slice(0, 10).replaceAll("/", "-"),
    )
  const designStyle = {
    "--lb-design": design.accent,
    "--lb-header": `url("${mediaUrl(design.header.media)}")`,
  } as CSSProperties

  const currentMonth = view.article
    ? postMonth(view.article.published)
    : view.month

  return (
    <div
      className="site lineblog"
      id="top"
      style={designStyle}
      onClick={onClick}
      onPointerOver={onIntent}
      onFocus={onIntent}
    >
      <SkipLink href="#lineblog-content">{t("lineblog.skip")}</SkipLink>
      <SiteHeader page="lineblog" />

      {phone ? (
        <SmartphoneBlog
          view={view}
          loaded={loaded}
          offered={offered}
          translationOn={translationOn}
          translationRound={translationRound}
          onToggleTranslation={toggleTranslation}
          designChoice={designChoice}
          design={design}
          onChooseDesign={chooseDesign}
          onChooseMonth={(month) =>
            go(new URL(lineblogHref({ month }), window.location.href))
          }
        />
      ) : (
        <div className="lb">
          <header className="lb-banner">
            <a href="/lineblog" className="lb-banner-link" lang="ja">
              <span className="sr-only">鹿乃 オフィシャルブログ</span>
            </a>
          </header>

          <nav className="lb-menu" aria-label={t("lineblog.menu")}>
            <div className="lb-menu-inner">
              <a
                href="/lineblog"
                className="lb-menu-home"
                aria-current={
                  !view.article && !view.month && view.page === 1
                    ? "page"
                    : undefined
                }
              >
                <img
                  src={mediaUrl(homeIcon.media)}
                  alt={t("lineblog.home")}
                  width={30}
                  height={66}
                  decoding="async"
                />
              </a>
              <ExternalLink href={design.officialSite} lang="ja">
                鹿乃オフィシャルサイト
              </ExternalLink>
              <div className="lb-menu-tools">
                {offered ? (
                  <TranslationMark
                    on={translationOn}
                    all
                    onToggle={toggleTranslation}
                  />
                ) : null}
                <DesignPicker
                  choice={designChoice}
                  design={design}
                  onChoose={chooseDesign}
                />
              </div>
            </div>
          </nav>

          <div className="lb-container">
            <main className="lb-main" id="lineblog-content" tabIndex={-1}>
              {view.month ? (
                <header className="lb-list-header">
                  <p>{t("lineblog.monthArchive")}</p>
                  <h1>{monthLabel(t, view.month)}</h1>
                  <small>
                    {t("lineblog.monthCount", { count: view.shown.length })}
                  </small>
                </header>
              ) : null}
              {view.missingArticle ? (
                <p className="lb-empty">
                  {t("lineblog.notFound")}{" "}
                  <a href="/lineblog">{t("lineblog.backToList")}</a>
                </p>
              ) : null}
              {view.month && !view.shown.length ? (
                <p className="lb-empty">{t("lineblog.monthEmpty")}</p>
              ) : null}
              {!view.article && !view.month ? (
                <h1 className="sr-only">{t("lineblog.kicker")}</h1>
              ) : null}
              {loaded.posts ? (
                loaded.posts.map((post) => (
                  <Article
                    key={post.id}
                    post={post}
                    single={Boolean(view.article)}
                    offered={offered}
                    translationOn={translationOn}
                    translationRound={translationRound}
                  />
                ))
              ) : loaded.failed ? (
                <div className="lb-loading" role="alert">
                  <p>{t("lineblog.loadFailed")}</p>
                  <button
                    type="button"
                    className="lb-retry"
                    onClick={() => window.location.reload()}
                  >
                    {t("lineblog.retry")}
                  </button>
                </div>
              ) : (
                <div className="lb-loading" role="status">
                  <p>{t("lineblog.loading")}</p>
                  <Placeholder lines={6} />
                </div>
              )}
              {view.article ? <ArticlePager post={view.article} /> : null}
              {view.article && loaded.posts ? (
                <Comments comments={loadedComments(view.article.id) ?? []} />
              ) : null}
              {!view.article && !view.month ? (
                <Pager page={view.page} pageCount={view.pageCount} />
              ) : null}
            </main>
            <Sidebar current={currentMonth} design={design} />
          </div>
          <a href="#top" className="lb-to-top" aria-label={t("lineblog.toTop")}>
            <ArrowUp aria-hidden="true" />
          </a>
        </div>
      )}

      <SiteFooter disclaimer={t("lineblog.disclaimer")} />
    </div>
  )
}
