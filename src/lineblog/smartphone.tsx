import { useRef } from "react"

import { useAppSettings } from "@/app-settings"
import { cn } from "@/lib/utils"
import { mediaSrcSet, mediaVariantUrl } from "@/lib/media"
import { Placeholder } from "@/site/site-shell"
import {
  lineblogView,
  media,
  mediaUrl,
  neighbours,
  postDate,
  postExcerpt,
  shortText,
} from "./content"
import { type LineblogDesign } from "./designs"
import { loadedComments } from "./post-files"
import type { LineblogPost, LineblogSummary } from "./types"
import {
  type Translation,
  ProfileText,
  MenuLabel,
  TranslationMark,
} from "./translation"
import { articleLink, lineblogHref, useViewPosts } from "./navigation"
import { Article } from "./article"
import { SmartphoneComments } from "./comments"
import {
  OfficialSiteMenu,
  DesignPicker,
  monthLabel,
  MonthSelect,
} from "./menus"

// ---------------------------------------------------------------------------
// Smartphone template
// ---------------------------------------------------------------------------

/**
 * The template's one-line profile under the menu bar opens this popup: the
 * profile photo, the name and the whole profile text.
 */
function ProfilePopup({
  design,
  offered,
  translationOn,
  translationRound,
}: {
  design: LineblogDesign
  offered: Translation | null
  translationOn: boolean
  translationRound: number
}) {
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
        <ProfileText
          design={design}
          offered={offered}
          translationOn={translationOn}
          translationRound={translationRound}
          className="lbl-profile-text"
        />
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

/**
 * The smartphone template: header art, a menu bar that scrolls sideways, the
 * one-line profile, then the content in white boxes on a grey page and the
 * blog's footer.
 */
export function SmartphoneBlog({
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
        <h1 className="lbl-blog-title" lang="ja">
          <span className="sr-only">鹿乃 オフィシャルブログ</span>
        </h1>
        <nav className="lbl-menu" aria-label={t("lineblog.menu")}>
          <ul>
            <li className={cn(home && "is-current")}>
              <a
                href="/lineblog"
                lang="ja"
                aria-current={home ? "page" : undefined}
              >
                <MenuLabel
                  label="ホーム"
                  item="home"
                  translation={translationOn ? offered : null}
                />
              </a>
            </li>
            <li>
              <OfficialSiteMenu translation={translationOn ? offered : null} />
            </li>
          </ul>
          <div className="lbl-menu-tools">
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
          </div>
        </nav>
        <ProfilePopup
          design={design}
          offered={offered}
          translationOn={translationOn}
          translationRound={translationRound}
        />
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
