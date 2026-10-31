import type {
  CSSProperties,
  MouseEvent,
  ReactNode,
  SyntheticEvent,
} from "react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { flushSync } from "react-dom"
import {
  ArrowUp,
  ChevronLeft,
  ChevronRight,
  ImageOff,
  Languages,
  Play,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
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
  OFFICIAL_SITE,
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
  postImages,
  postMonth,
  posts,
} from "./content"
import { designFor, homeIcon, type LineblogDesign } from "./designs"
import { loadView, viewLoaded } from "./post-files"
import type {
  LineblogBlock,
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

/** The translation each interface language shows; Japanese shows none. */
const TRANSLATIONS = {
  "zh-CN": { blocks: "zh", title: "titleZh", lang: "zh-CN" },
  en: { blocks: "en", title: "titleEn", lang: "en" },
} as const

type Translation = (typeof TRANSLATIONS)[keyof typeof TRANSLATIONS]

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

  const onClick = useCallback((event: MouseEvent<HTMLElement>) => {
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

  const onIntent = useCallback((event: SyntheticEvent<HTMLElement>) => {
    const url = viewUrl(event.target)
    if (url) loadView(url.search).catch(() => undefined)
  }, [])

  return { search, onClick, onIntent }
}

/**
 * The view's posts, once loaded. Browsers keep a failed chunk load for the
 * document, so a failed view offers a reload rather than a second request.
 */
function useViewPosts(search: string, shown: LineblogSummary[]) {
  const ready = shown.every((post) => loadedPost(post.id))
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
      className="lb-picture"
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
      {image.source ? (
        <ExternalLink href={image.source}>
          {t("lineblog.recoveredFrom")}
        </ExternalLink>
      ) : null}
    </div>
  )
}

function Article({
  post,
  single,
  translation,
}: {
  post: LineblogPost
  single: boolean
  translation: Translation | null
}) {
  const { t } = useAppSettings()
  const images = useMemo(() => postImages(post.blocks), [post])
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
        renderDetails={(image) => <ImageDetails image={image} />}
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

function ArticleNav({ post }: { post: LineblogSummary }) {
  const { t } = useAppSettings()
  const { newer, older } = neighbours(post.id)
  return (
    <nav className="lb-article-nav" aria-label={t("lineblog.articleNav")}>
      {older ? (
        <a href={articleLink(older)} className="is-older">
          <ChevronLeft aria-hidden="true" />
          <span>
            <small>{t("lineblog.older")}</small>
            <span lang="ja">{older.title}</span>
          </span>
        </a>
      ) : (
        <span />
      )}
      <a href="/lineblog" className="lb-article-nav-list">
        {t("lineblog.backToList")}
      </a>
      {newer ? (
        <a href={articleLink(newer)} className="is-newer">
          <span>
            <small>{t("lineblog.newer")}</small>
            <span lang="ja">{newer.title}</span>
          </span>
          <ChevronRight aria-hidden="true" />
        </a>
      ) : (
        <span />
      )}
    </nav>
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

export function LineblogApp() {
  const { t, locale } = useAppSettings()
  const { search, onClick, onIntent } = useLineblogLocation()
  const view = useMemo(() => lineblogView(search), [search])
  const loaded = useViewPosts(search, view.shown)
  const [translationOn, setTranslationOn] = useState(readTranslation)
  const offered = locale === "ja" ? null : TRANSLATIONS[locale]
  const translation = translationOn ? offered : null

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
    try {
      window.localStorage.setItem(TRANSLATION_KEY, next ? "on" : "off")
    } catch {
      // The choice then lasts for this visit only.
    }
  }

  const shownPost = view.article ?? view.shown[0]
  const design = designFor(
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
            <ExternalLink href={OFFICIAL_SITE}>
              <span lang="ja">鹿乃オフィシャルサイト</span>
            </ExternalLink>
            {offered ? (
              <button
                type="button"
                className="lb-translation-toggle"
                aria-pressed={translationOn}
                onClick={toggleTranslation}
              >
                <Languages aria-hidden="true" />
                {t("lineblog.translationOn")}
              </button>
            ) : null}
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
                  translation={translation}
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
            {view.article ? <ArticleNav post={view.article} /> : null}
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

      <SiteFooter disclaimer={t("lineblog.disclaimer")} />
    </div>
  )
}
