import type { CSSProperties } from "react"
import { useEffect, useMemo, useState } from "react"
import { ArrowUp } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import {
  Placeholder,
  SiteFooter,
  SiteHeader,
  SkipLink,
} from "@/site/site-shell"
import {
  lineblogView,
  media,
  mediaUrl,
  postDate,
  postMonth,
  posts,
} from "./content"
import { chosenDesign, designFor, homeIcon } from "./designs"
import { loadedComments, loadView } from "./post-files"
import { TRANSLATIONS, TranslationMark } from "./translation"
import {
  useLineblogLocation,
  usePhoneLayout,
  useViewPosts,
  lineblogHref,
} from "./navigation"
import { Article } from "./article"
import { Comments } from "./comments"
import { OfficialSiteMenu, DesignPicker, monthLabel } from "./menus"
import { ArticlePager, Pager, Sidebar } from "./desktop"
import { SmartphoneBlog } from "./smartphone"
import "./lineblog.css"

// The page's code waits for the posts its address shows, so the site's
// navigation reveals it, and restores a reload's scroll, with them in place.
await loadView(window.location.search).catch(() => undefined)

const TRANSLATION_KEY = "kano-lineblog-translation"

const DESIGN_KEY = "kano-lineblog-design"

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

export function LineblogApp() {
  const { t, contentLocale } = useAppSettings()
  const { search, go, onClick, onIntent } = useLineblogLocation()
  const phone = usePhoneLayout()
  const view = useMemo(() => lineblogView(search), [search])
  const loaded = useViewPosts(search, view.shown)
  const [translationOn, setTranslationOn] = useState(readTranslation)
  const [translationRound, setTranslationRound] = useState(0)
  const [designChoice, setDesignChoice] = useState(readDesign)
  const offered = contentLocale === "ja" ? null : TRANSLATIONS[contentLocale]

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
            <span className="sr-only" lang="ja">
              鹿乃 オフィシャルブログ
            </span>
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
              <OfficialSiteMenu translation={translationOn ? offered : null} />
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
            <Sidebar
              current={currentMonth}
              design={design}
              offered={offered}
              translationOn={translationOn}
              translationRound={translationRound}
            />
          </div>
          <a href="#top" className="lb-to-top" aria-label={t("lineblog.toTop")}>
            <ArrowUp aria-hidden="true" />
          </a>
        </div>
      )}

      <SiteFooter />
    </div>
  )
}
