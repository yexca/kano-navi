import { useAppSettings } from "@/app-settings"
import { mediaSrcSet } from "@/lib/media"
import {
  archiveMonths,
  mediaUrl,
  neighbours,
  pagerPages,
  posts,
} from "./content"
import { type LineblogDesign } from "./designs"
import type { LineblogSummary } from "./types"
import {
  type Translation,
  useOwnTranslation,
  ProfileText,
  TranslationMark,
} from "./translation"
import { lineblogHref, articleLink } from "./navigation"
import { SIZES } from "./article"
import { monthLabel } from "./menus"

// ---------------------------------------------------------------------------
// Page chrome
// ---------------------------------------------------------------------------

export function Pager({
  page,
  pageCount,
}: {
  page: number
  pageCount: number
}) {
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
export function ArticlePager({ post }: { post: LineblogSummary }) {
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

export function Sidebar({
  current,
  design,
  offered,
  translationOn,
  translationRound,
}: {
  current: string | null
  design: LineblogDesign
  offered: Translation | null
  translationOn: boolean
  translationRound: number
}) {
  const { t } = useAppSettings()
  const [recentOn, toggleRecent] = useOwnTranslation(
    translationOn,
    translationRound,
  )
  const translation = recentOn ? offered : null
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
        <ProfileText
          design={design}
          offered={offered}
          translationOn={translationOn}
          translationRound={translationRound}
          className="lb-profile-text"
        />
      </section>
      <section className="lb-side" aria-labelledby="lb-recent">
        <div className="lb-side-head">
          <h2 className="lb-side-title" id="lb-recent">
            {t("lineblog.recent")}
          </h2>
          {offered ? (
            <TranslationMark on={recentOn} onToggle={toggleRecent} />
          ) : null}
        </div>
        <ul className="lb-recent">
          {posts.slice(0, 10).map((post) => {
            const thumbnail = post.thumbnail
            const translatedTitle = translation && post[translation.title]
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
                  <span className="lb-recent-title">
                    <span lang="ja">{post.title}</span>
                    {translatedTitle && translatedTitle !== post.title ? (
                      <small lang={translation.lang}>{translatedTitle}</small>
                    ) : null}
                  </span>
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
