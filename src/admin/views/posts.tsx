import { useEffect, useState } from "react"
import {
  ChevronLeft,
  ChevronRight,
  Download,
  ExternalLink,
  Eye,
  EyeOff,
  FileText,
  ImageOff,
  Quote,
  RefreshCw,
  Search,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatDateTime } from "@/admin/format"
import type { AdminPost, AdminPostImage, Sources } from "@/admin/types"
import {
  Badge,
  Button,
  DateRange,
  Empty,
  FilterReset,
  FilterSelect,
  IconButton,
  Loading,
  Modal,
  PageHeader,
  Pager,
  Panel,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { PostActions, PostStatus, RouteSelect } from "@/admin/views/review"
import {
  TranslationBacklog,
  TranslationCell,
} from "@/admin/views/translation-parts"

/** Only local, already cached media is shown; never a remote CDN URL. */
function localUrl(value: string | null | undefined) {
  return value && value.startsWith("/") ? value : null
}

/** The message key for an image that has no cached file to show yet. */
function missingKey(status: string) {
  return `admin.review.media.${["pending", "failed"].includes(status) ? status : "missing"}`
}

function PostAccount({ post }: { post: AdminPost }) {
  const { t } = useAppSettings()
  return post.accountHandle
    ? `@${post.accountHandle.replace(/^@/, "")}`
    : t(`admin.step.${post.source}`)
}

/**
 * A post's images, or its quote's, one at a time inside the page, fitted to
 * the viewport. The buttons and arrow keys step through several images.
 */
function PostImageViewer({
  post,
  images = post.media,
  start,
  onClose,
}: {
  post: AdminPost
  images?: AdminPostImage[]
  start: number
  onClose: () => void
}) {
  const { t, locale } = useAppSettings()
  const [index, setIndex] = useState(start)
  const count = images.length
  const image = images[index]
  const url = localUrl(image?.url)
  const step = (delta: number) =>
    setIndex((current) => (current + delta + count) % count)
  useEffect(() => {
    if (count < 2) return undefined
    const onKey = (event: KeyboardEvent) => {
      const delta =
        event.key === "ArrowLeft" ? -1 : event.key === "ArrowRight" ? 1 : 0
      if (delta) setIndex((current) => (current + delta + count) % count)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [count])
  return (
    <Modal
      open
      className="adm-image-viewer"
      title={
        <>
          <PostAccount post={post} /> ·{" "}
          {formatDateTime(post.publishedAt, locale)}
        </>
      }
      onClose={onClose}
    >
      <div className="adm-image-viewer-stage">
        {url ? (
          <img
            src={url}
            alt={
              image.alt || (images === post.media ? post.mediaAlt : "") || ""
            }
          />
        ) : (
          <span>
            <ImageOff aria-hidden />
            {t(missingKey(image?.status || ""))}
          </span>
        )}
      </div>
      {count > 1 ? (
        <div className="adm-image-viewer-nav">
          <IconButton
            icon={ChevronLeft}
            label={t("admin.posts.imagePrevious")}
            onClick={() => step(-1)}
          />
          <span aria-live="polite">
            {index + 1} / {count}
          </span>
          <IconButton
            icon={ChevronRight}
            label={t("admin.posts.imageNext")}
            onClick={() => step(1)}
          />
        </div>
      ) : null}
    </Modal>
  )
}

/**
 * The post's first cached image, or why there is none to show yet. Clicking
 * it enlarges the images in the page.
 */
function PostImage({ post }: { post: AdminPost }) {
  const { t } = useAppSettings()
  const [viewing, setViewing] = useState(false)
  if (!post.media.length) return <span className="adm-post-no-image">—</span>
  const first = post.media.findIndex((image) => localUrl(image.url))
  const preview = first < 0 ? null : localUrl(post.media[first].url)
  const count =
    post.media.length > 1 ? (
      <span className="adm-post-thumb-count">×{post.media.length}</span>
    ) : null
  if (!preview) {
    const label = t(missingKey(post.mediaStatus))
    return (
      <span className="adm-post-thumb is-empty" title={label}>
        <ImageOff aria-label={label} />
        {count}
      </span>
    )
  }
  return (
    <>
      <button
        type="button"
        className="adm-post-thumb"
        onClick={() => setViewing(true)}
        aria-label={t("admin.posts.imageView")}
        title={t("admin.posts.imageView")}
      >
        <img src={preview} alt="" loading="lazy" />
        {count}
      </button>
      {viewing ? (
        <PostImageViewer
          post={post}
          start={first}
          onClose={() => setViewing(false)}
        />
      ) : null}
    </>
  )
}

/**
 * The status a post quotes, read-only and apart from the post's own images:
 * author, time, wording and its cached thumbnails. An operator can hide it
 * from the public pages, show it again, or queue a refresh from the source
 * for the next X synchronization.
 */
function PostQuote({ post, onDone }: { post: AdminPost; onDone: () => void }) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const [busy, setBusy] = useState<"" | "visibility" | "refresh">("")
  const [viewing, setViewing] = useState<number | null>(null)
  const quote = post.quote
  const queued = post.refreshRequestedAt
  if (!quote && !queued) return null
  const hidden = Boolean(post.quoteHiddenAt)
  const images = (quote?.media || []).filter((image) => localUrl(image.url))
  const run = async (
    kind: "visibility" | "refresh",
    call: () => Promise<unknown>,
    notice: string,
  ) => {
    setBusy(kind)
    const result = await act(call, notice)
    setBusy("")
    if (result) onDone()
  }
  const name =
    quote?.authorName || (quote?.authorHandle ? `@${quote.authorHandle}` : "")
  return (
    <section
      className={`adm-post-quote${hidden ? " is-hidden" : ""}`}
      aria-label={t("admin.posts.quote.label")}
    >
      <header className="adm-post-quote-head">
        <Quote aria-hidden />
        <span className="adm-post-quote-kicker">
          {t("admin.posts.quote.label")}
        </span>
        {quote && !quote.unavailable ? (
          <span className="adm-post-quote-author">
            <strong>{name}</strong>
            {quote.authorName && quote.authorHandle ? (
              <span>@{quote.authorHandle}</span>
            ) : null}
            {quote.publishedAt ? (
              <time dateTime={quote.publishedAt}>
                {formatDateTime(quote.publishedAt, locale)}
              </time>
            ) : null}
          </span>
        ) : null}
      </header>
      {hidden || queued ? (
        <div className="adm-chip-row">
          {hidden ? (
            <Badge tone="warning" icon={EyeOff}>
              {t("admin.posts.quote.hidden")}
            </Badge>
          ) : null}
          {queued ? (
            <Badge tone="running" icon={RefreshCw}>
              {t("admin.posts.refreshQueued", {
                time: formatDateTime(queued, locale),
              })}
            </Badge>
          ) : null}
        </div>
      ) : null}
      {quote?.unavailable ? (
        <p className="adm-post-quote-missing">
          {t("admin.posts.quote.unavailable")}
        </p>
      ) : quote?.text ? (
        <p className="adm-post-quote-text">{quote.text}</p>
      ) : null}
      {images.length ? (
        <div className="adm-post-quote-media">
          {images.map((image, index) => (
            <button
              type="button"
              key={image.url}
              className="adm-post-thumb"
              onClick={() => setViewing(index)}
              aria-label={`${t("admin.posts.quote.imageView")} ${index + 1}`}
              title={t("admin.posts.quote.imageView")}
            >
              <img
                src={localUrl(image.url) || undefined}
                alt=""
                loading="lazy"
              />
            </button>
          ))}
        </div>
      ) : null}
      <div className="adm-post-quote-actions">
        {quote ? (
          <Button
            size="sm"
            variant="ghost"
            icon={hidden ? Eye : EyeOff}
            busy={busy === "visibility"}
            onClick={() =>
              run(
                "visibility",
                () => api.setQuoteHidden(post.id, !hidden),
                hidden ? "admin.notice.quoteShown" : "admin.notice.quoteHidden",
              )
            }
          >
            {t(hidden ? "admin.posts.quote.show" : "admin.posts.quote.hide")}
          </Button>
        ) : null}
        {post.source === "x" ? (
          <Button
            size="sm"
            variant="ghost"
            icon={RefreshCw}
            busy={busy === "refresh"}
            disabled={Boolean(queued)}
            onClick={() =>
              run(
                "refresh",
                () => api.refreshPost(post.id),
                "admin.notice.refreshQueued",
              )
            }
          >
            {t("admin.posts.refresh")}
          </Button>
        ) : null}
        {quote ? (
          <a
            className="adm-link"
            href={quote.url}
            target="_blank"
            rel="noreferrer"
          >
            {t("admin.posts.quote.open")}
            <ExternalLink aria-hidden />
          </a>
        ) : null}
      </div>
      {viewing !== null ? (
        <PostImageViewer
          post={post}
          images={images}
          start={viewing}
          onClose={() => setViewing(null)}
        />
      ) : null}
    </section>
  )
}

function PostTableRow({
  post,
  onDone,
}: {
  post: AdminPost
  onDone: () => void
}) {
  const { locale } = useAppSettings()
  const [route, setRoute] = useState("")
  return (
    <div role="row" className="adm-post-table-row">
      <div role="cell" className="adm-post-origin">
        <strong>
          <PostAccount post={post} />
        </strong>
        <time dateTime={post.publishedAt}>
          {formatDateTime(post.publishedAt, locale)}
        </time>
      </div>
      <div role="cell" className="adm-post-body">
        <p className="adm-post-text">{post.text}</p>
        <TranslationCell
          owner="post"
          id={post.id}
          original={post.text}
          translation={post.translation}
          onChanged={onDone}
        />
      </div>
      {post.quote || post.refreshRequestedAt ? (
        <div role="cell" className="adm-post-quote-cell">
          <PostQuote post={post} onDone={onDone} />
        </div>
      ) : null}
      <div role="cell" className="adm-post-image">
        <PostImage post={post} />
      </div>
      <div role="cell" className="adm-chip-row adm-post-state">
        <PostStatus post={post} />
      </div>
      <div role="cell" className="adm-post-route">
        <RouteSelect value={route} onChange={setRoute} />
      </div>
      <div role="cell" className="adm-post-controls">
        <PostActions post={post} route={route} onDone={onDone} compact />
      </div>
    </div>
  )
}

const blankFilters = {
  search: "",
  account: "",
  llm: "",
  media: "",
  translation: "",
  from: "",
  to: "",
}

function PostList({ sources }: { sources: Sources | undefined }) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const [filters, setFilters] = useState(blankFilters)
  const [search, setSearch] = useState("")
  const [page, setPage] = useState(1)
  const posts = useResource(
    () => api.posts({ ...filters, page, pageSize: 30 }),
    [filters, page],
  )
  const update = (patch: Partial<typeof filters>) => {
    setFilters((current) => ({ ...current, ...patch }))
    setPage(1)
  }
  const active = (
    Object.keys(blankFilters) as (keyof typeof blankFilters)[]
  ).filter((key) => filters[key] !== blankFilters[key]).length
  return (
    <>
      <form
        className="adm-filters"
        onSubmit={(event) => {
          event.preventDefault()
          update({ search })
        }}
      >
        <label className="adm-search">
          <Search aria-hidden />
          <input
            type="search"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onBlur={() => search !== filters.search && update({ search })}
            placeholder={t("admin.posts.search")}
            aria-label={t("admin.posts.search")}
          />
        </label>
        <FilterSelect
          label={t("admin.posts.account")}
          value={filters.account}
          onChange={(account) => update({ account })}
        >
          <option value="">{t("admin.filter.any")}</option>
          {sources?.x.accounts.map(({ account }) => (
            <option key={account} value={account}>
              @{account}
            </option>
          ))}
        </FilterSelect>
        <FilterSelect
          label={t("admin.posts.llm")}
          value={filters.llm}
          onChange={(llm) => update({ llm })}
        >
          <option value="">{t("admin.filter.any")}</option>
          {[
            "never",
            "pending",
            "processed",
            "uncertain",
            "failed",
            "skipped",
          ].map((status) => (
            <option key={status} value={status}>
              {t(`admin.status.${status}`)}
            </option>
          ))}
        </FilterSelect>
        <FilterSelect
          label={t("admin.posts.media")}
          value={filters.media}
          onChange={(media) => update({ media })}
        >
          <option value="">{t("admin.filter.any")}</option>
          <option value="present">{t("admin.posts.withMedia")}</option>
          <option value="missing">{t("admin.posts.withoutMedia")}</option>
        </FilterSelect>
        <FilterSelect
          label={t("admin.translation.filter")}
          value={filters.translation}
          onChange={(translation) => update({ translation })}
        >
          <option value="">{t("admin.filter.any")}</option>
          {["incomplete", "complete", "requested"].map((status) => (
            <option key={status} value={status}>
              {t(`admin.translation.filter.${status}`)}
            </option>
          ))}
        </FilterSelect>
        <DateRange
          from={filters.from}
          to={filters.to}
          onChange={(range) => update(range)}
        />
        <FilterReset
          count={active}
          onReset={() => {
            setSearch("")
            update(blankFilters)
          }}
        />
      </form>
      <div className="adm-list-bar">
        <span className="adm-muted">
          {posts.data
            ? t("admin.pager.total", { total: posts.data.total })
            : ""}
        </span>
        <TranslationBacklog owner="post" onQueued={() => void posts.reload()} />
      </div>
      {!posts.data ? (
        <Loading />
      ) : posts.data.items.length ? (
        <>
          <div
            className="adm-table adm-post-table"
            role="table"
            aria-label={t("admin.posts.title")}
          >
            <div role="row" className="adm-post-table-row adm-table-head">
              <span role="columnheader">{t("admin.posts.account")}</span>
              <span role="columnheader">{t("admin.posts.column.text")}</span>
              <span role="columnheader">{t("admin.posts.column.image")}</span>
              <span role="columnheader">{t("admin.posts.llm")}</span>
              <span role="columnheader">{t("admin.posts.route")}</span>
              <span role="columnheader">{t("admin.posts.column.actions")}</span>
            </div>
            {posts.data.items.map((post) => (
              <PostTableRow
                key={post.id}
                post={post}
                onDone={() => void posts.reload()}
              />
            ))}
          </div>
          <Pager
            page={posts.data.page}
            totalPages={posts.data.totalPages}
            total={posts.data.total}
            onPage={setPage}
          />
        </>
      ) : (
        <Empty icon={FileText} title={t("admin.posts.empty")} />
      )}
    </>
  )
}

export function PostsView({ navigate }: ViewProps) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  // The account filter lists the configured accounts.
  const sources = useResource(() => api.sources(), [])
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.posts.title")}
        description={t("admin.posts.description")}
        actions={
          <Button
            size="sm"
            variant="ghost"
            icon={Download}
            onClick={() => navigate("sources")}
          >
            {t("admin.fetch.openSources")}
          </Button>
        }
      />
      <Panel>
        <PostList sources={sources.data} />
      </Panel>
    </div>
  )
}
