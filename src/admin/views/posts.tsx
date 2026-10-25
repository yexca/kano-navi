import { useEffect, useState } from "react"
import {
  ChevronLeft,
  ChevronRight,
  FileText,
  ImageOff,
  Search,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatDateTime } from "@/admin/format"
import type { AdminPost, Sources } from "@/admin/types"
import {
  Empty,
  IconButton,
  Loading,
  Modal,
  PageHeader,
  Pager,
  Panel,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { QuickFetch } from "@/admin/views/fetch-parts"
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
 * The post's images one at a time inside the page, fitted to the viewport.
 * The buttons and arrow keys step through a post with several images.
 */
function PostImageViewer({
  post,
  start,
  onClose,
}: {
  post: AdminPost
  start: number
  onClose: () => void
}) {
  const { t, locale } = useAppSettings()
  const [index, setIndex] = useState(start)
  const count = post.media.length
  const image = post.media[index]
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
          <img src={url} alt={image.alt || post.mediaAlt || ""} />
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

function PostList({ sources }: { sources: Sources | undefined }) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const [filters, setFilters] = useState({
    search: "",
    account: "",
    llm: "",
    media: "",
    translation: "",
    from: "",
    to: "",
  })
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
            placeholder={t("admin.posts.search")}
            aria-label={t("admin.posts.search")}
          />
        </label>
        <select
          value={filters.account}
          onChange={(e) => update({ account: e.target.value })}
          aria-label={t("admin.posts.account")}
        >
          <option value="">{t("admin.posts.anyAccount")}</option>
          {sources?.x.accounts.map(({ account }) => (
            <option key={account} value={account}>
              @{account}
            </option>
          ))}
        </select>
        <select
          value={filters.llm}
          onChange={(e) => update({ llm: e.target.value })}
          aria-label={t("admin.posts.llm")}
        >
          <option value="">{t("admin.posts.anyLlm")}</option>
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
        </select>
        <select
          value={filters.media}
          onChange={(e) => update({ media: e.target.value })}
          aria-label={t("admin.posts.media")}
        >
          <option value="">{t("admin.posts.anyMedia")}</option>
          <option value="present">{t("admin.posts.withMedia")}</option>
          <option value="missing">{t("admin.posts.withoutMedia")}</option>
        </select>
        <select
          value={filters.translation}
          onChange={(e) => update({ translation: e.target.value })}
          aria-label={t("admin.translation.filter")}
        >
          <option value="">{t("admin.translation.anyStatus")}</option>
          {["incomplete", "complete", "requested"].map((status) => (
            <option key={status} value={status}>
              {t(`admin.translation.filter.${status}`)}
            </option>
          ))}
        </select>
        <div className="adm-filter-dates">
          <label className="adm-field">
            <span className="adm-field-label">
              {t("admin.schedule.filter.from")}
            </span>
            <input
              type="date"
              value={filters.from}
              onChange={(e) => update({ from: e.target.value })}
              aria-label={t("admin.schedule.filter.from")}
            />
          </label>
          <label className="adm-field">
            <span className="adm-field-label">
              {t("admin.schedule.filter.to")}
            </span>
            <input
              type="date"
              value={filters.to}
              min={filters.from || undefined}
              onChange={(e) => update({ to: e.target.value })}
              aria-label={t("admin.schedule.filter.to")}
            />
          </label>
        </div>
        <TranslationBacklog owner="post" onQueued={() => void posts.reload()} />
      </form>
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
  // One read serves both the fetch panel and the account filter.
  const sources = useResource(() => api.sources(), [])
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.posts.title")}
        description={t("admin.posts.description")}
      />
      <QuickFetch source="x" sources={sources.data} navigate={navigate} />
      <Panel>
        <PostList sources={sources.data} />
      </Panel>
    </div>
  )
}
