import { useEffect, useLayoutEffect, useRef, useState } from "react"
import {
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronUp,
  Download,
  ExternalLink,
  Eye,
  EyeOff,
  FileText,
  ImageOff,
  PencilLine,
  Quote,
  RefreshCw,
  Search,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { japanStamp } from "@/admin/format"
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
  Switch,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { PostActions, PostStatus } from "@/admin/views/review"
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
  const { t } = useAppSettings()
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
          <PostAccount post={post} /> · {japanStamp(post.publishedAt)}
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

/** How many cached images a feed item shows before the rest fold into a count. */
const shownImages = 3

/**
 * The post's cached images, or why there are none to show yet; a text-only
 * post shows nothing. Clicking an image enlarges the images in the page.
 */
function PostImages({
  post,
  limit = shownImages,
}: {
  post: AdminPost
  limit?: number
}) {
  const { t } = useAppSettings()
  const [viewing, setViewing] = useState<number | null>(null)
  if (!post.media.length) return null
  const cached = post.media.flatMap((image, index) => {
    const url = localUrl(image.url)
    return url ? [{ url, index }] : []
  })
  if (!cached.length) {
    const label = t(missingKey(post.mediaStatus))
    return (
      <div className="adm-post-media">
        <span className="adm-post-thumb is-empty" title={label}>
          <ImageOff aria-label={label} />
          {post.media.length > 1 ? (
            <span className="adm-post-thumb-count">×{post.media.length}</span>
          ) : null}
        </span>
      </div>
    )
  }
  const shown = cached.slice(0, limit)
  const more = post.media.length - shown.length
  return (
    <div className="adm-post-media">
      {shown.map(({ url, index }, position) => (
        <button
          type="button"
          key={url}
          className="adm-post-thumb"
          onClick={() => setViewing(index)}
          aria-label={`${t("admin.posts.imageView")} ${index + 1}`}
          title={t("admin.posts.imageView")}
        >
          <img src={url} alt="" loading="lazy" />
          {more > 0 && position === shown.length - 1 ? (
            <span className="adm-post-thumb-count">+{more}</span>
          ) : null}
        </button>
      ))}
      {viewing !== null ? (
        <PostImageViewer
          post={post}
          start={viewing}
          onClose={() => setViewing(null)}
        />
      ) : null}
    </div>
  )
}

/**
 * The status a post quotes, read-only and apart from the post's own images,
 * drawn as an embedded card: the author and time with the quote's actions
 * (hide from the public pages or show again, queue a refresh from the source
 * for the next X synchronization, open it), then its wording and its cached
 * thumbnails. Wording past three lines folds behind a toggle; `full` shows it
 * all.
 */
function PostQuote({
  post,
  onDone,
  full = false,
}: {
  post: AdminPost
  onDone: () => void
  full?: boolean
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [busy, setBusy] = useState<"" | "visibility" | "refresh">("")
  const [viewing, setViewing] = useState<number | null>(null)
  const [expanded, setExpanded] = useState(full)
  const [clamped, setClamped] = useState(false)
  const textRef = useRef<HTMLParagraphElement>(null)
  const quote = post.quote
  useLayoutEffect(() => {
    const element = textRef.current
    if (!element || expanded) return undefined
    const measure = () =>
      setClamped(element.scrollHeight > element.clientHeight + 1)
    measure()
    if (typeof ResizeObserver === "undefined") return undefined
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [expanded, quote?.text])
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
  const available = quote && !quote.unavailable
  return (
    <section
      className={`adm-post-quote${hidden ? " is-hidden" : ""}`}
      aria-label={t("admin.posts.quote.label")}
    >
      <header className="adm-post-quote-head">
        <Quote aria-hidden />
        {available ? (
          <span className="adm-post-quote-author">
            <strong>
              {quote.authorName ||
                (quote.authorHandle ? `@${quote.authorHandle}` : "")}
            </strong>
            {quote.authorName && quote.authorHandle ? (
              <span>@{quote.authorHandle}</span>
            ) : null}
            {quote.publishedAt ? (
              <time dateTime={quote.publishedAt}>
                {japanStamp(quote.publishedAt)}
              </time>
            ) : null}
          </span>
        ) : (
          <span className="adm-post-quote-author">
            {t("admin.posts.quote.label")}
          </span>
        )}
        {hidden ? (
          <Badge tone="warning" icon={EyeOff}>
            {t("admin.posts.quote.hidden")}
          </Badge>
        ) : null}
        {queued ? (
          <Badge tone="running" icon={RefreshCw}>
            {t("admin.posts.refreshQueued", { time: japanStamp(queued) })}
          </Badge>
        ) : null}
        <span className="adm-post-quote-actions">
          {quote ? (
            <IconButton
              icon={hidden ? Eye : EyeOff}
              label={t(
                hidden ? "admin.posts.quote.show" : "admin.posts.quote.hide",
              )}
              busy={busy === "visibility"}
              onClick={() =>
                run(
                  "visibility",
                  () => api.setQuoteHidden(post.id, !hidden),
                  hidden
                    ? "admin.notice.quoteShown"
                    : "admin.notice.quoteHidden",
                )
              }
            />
          ) : null}
          {post.source === "x" ? (
            <IconButton
              icon={RefreshCw}
              label={t("admin.posts.refresh")}
              busy={busy === "refresh"}
              disabled={Boolean(queued)}
              onClick={() =>
                run(
                  "refresh",
                  () => api.refreshPost(post.id),
                  "admin.notice.refreshQueued",
                )
              }
            />
          ) : null}
          {quote ? (
            <a
              className="adm-icon-button"
              href={quote.url}
              target="_blank"
              rel="noreferrer"
              aria-label={t("admin.posts.quote.open")}
              title={t("admin.posts.quote.open")}
            >
              <ExternalLink aria-hidden />
            </a>
          ) : null}
        </span>
      </header>
      {quote?.unavailable ? (
        <p className="adm-post-quote-missing">
          {t("admin.posts.quote.unavailable")}
        </p>
      ) : quote?.text ? (
        <>
          <p
            ref={textRef}
            className={`adm-post-quote-text${expanded ? " is-expanded" : ""}`}
          >
            {quote.text}
          </p>
          {!full && (clamped || expanded) ? (
            <button
              type="button"
              className="adm-post-quote-toggle"
              aria-expanded={expanded}
              onClick={() => setExpanded((current) => !current)}
            >
              {t(
                expanded ? "admin.posts.quote.less" : "admin.posts.quote.more",
              )}
              {expanded ? (
                <ChevronUp aria-hidden />
              ) : (
                <ChevronDown aria-hidden />
              )}
            </button>
          ) : null}
        </>
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

/**
 * The post in full: its wording, images, translations and quote, its
 * extraction state and the operator's decisions. Hiding the post keeps it
 * stored and processed but leaves it off the public pages.
 */
function PostEditor({
  post,
  onClose,
  onDone,
}: {
  post: AdminPost
  onClose: () => void
  onDone: () => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [hidden, setHidden] = useState(Boolean(post.hiddenAt))
  const [busy, setBusy] = useState(false)
  useEffect(() => setHidden(Boolean(post.hiddenAt)), [post.hiddenAt])
  const toggle = async (next: boolean) => {
    setBusy(true)
    const result = await act(
      () => api.setPostHidden(post.id, next),
      next ? "admin.notice.postHidden" : "admin.notice.postShown",
    )
    setBusy(false)
    if (!result) return
    setHidden(next)
    onDone()
  }
  return (
    <Modal
      open
      wide
      className="adm-post-editor"
      title={
        <>
          <PostAccount post={post} /> · {japanStamp(post.publishedAt)}
        </>
      }
      onClose={onClose}
      footer={<PostActions post={post} onDone={onDone} />}
    >
      <div className="adm-chip-row">
        <PostStatus post={post} />
      </div>
      <p className="adm-post-editor-text">{post.text}</p>
      <PostImages post={post} limit={post.media.length} />
      {post.translation ? (
        <section className="adm-post-editor-section">
          <h3>{t("admin.posts.translations")}</h3>
          <TranslationCell
            owner="post"
            id={post.id}
            original={post.text}
            translation={post.translation}
            onChanged={onDone}
          />
        </section>
      ) : null}
      <PostQuote post={post} onDone={onDone} full />
      <div className="adm-post-editor-section">
        <Switch
          checked={hidden}
          disabled={busy}
          onChange={(next) => void toggle(next)}
          label={t("admin.posts.hide")}
        />
      </div>
    </Modal>
  )
}

/**
 * One post in the feed, one row of columns: who and when, the wording and
 * quote, the post's own images, its translations, its extraction state and
 * its actions. A narrower feed stacks them (see the stylesheet).
 */
function PostItem({ post, onDone }: { post: AdminPost; onDone: () => void }) {
  const { t } = useAppSettings()
  const [editing, setEditing] = useState(false)
  return (
    <li className={`adm-post${post.hiddenAt ? " is-hidden" : ""}`}>
      <span className="adm-post-origin">
        <strong>
          <PostAccount post={post} />
        </strong>
        <time dateTime={post.publishedAt}>{japanStamp(post.publishedAt)}</time>
      </span>
      <div className="adm-post-body">
        <p className="adm-post-text">{post.text}</p>
        <PostQuote post={post} onDone={onDone} />
      </div>
      <PostImages post={post} />
      <div className="adm-post-translation">
        <TranslationCell
          owner="post"
          id={post.id}
          original={post.text}
          translation={post.translation}
          onChanged={onDone}
        />
      </div>
      <span className="adm-chip-row adm-post-state">
        {post.hiddenAt ? (
          <Badge tone="warning" icon={EyeOff}>
            {t("admin.posts.hidden")}
          </Badge>
        ) : null}
        <PostStatus post={post} />
      </span>
      <span className="adm-post-controls">
        <IconButton
          icon={PencilLine}
          label={t("admin.posts.edit")}
          onClick={() => setEditing(true)}
        />
        <PostActions post={post} onDone={onDone} compact />
      </span>
      {editing ? (
        <PostEditor
          post={post}
          onClose={() => setEditing(false)}
          onDone={onDone}
        />
      ) : null}
    </li>
  )
}

const blankFilters = {
  search: "",
  account: "",
  llm: "",
  translation: "",
  from: "",
  to: "",
}

const llmStatuses = [
  "never",
  "pending",
  "processed",
  "uncertain",
  "failed",
  "skipped",
]
const translationStatuses = ["incomplete", "complete", "requested"]

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
  const any = { value: "", label: t("admin.filter.any") }
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
          options={[
            any,
            ...(sources?.x.accounts || []).map(({ account }) => ({
              value: account,
              label: `@${account}`,
            })),
          ]}
        />
        <FilterSelect
          label={t("admin.posts.llm")}
          value={filters.llm}
          onChange={(llm) => update({ llm })}
          options={[
            any,
            ...llmStatuses.map((status) => ({
              value: status,
              label: t(`admin.status.${status}`),
            })),
          ]}
        />
        <FilterSelect
          label={t("admin.translation.filter")}
          value={filters.translation}
          onChange={(translation) => update({ translation })}
          options={[
            any,
            ...translationStatuses.map((status) => ({
              value: status,
              label: t(`admin.translation.filter.${status}`),
            })),
          ]}
        />
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
          <ul className="adm-post-feed" aria-label={t("admin.posts.title")}>
            {posts.data.items.map((post) => (
              <PostItem
                key={post.id}
                post={post}
                onDone={() => void posts.reload()}
              />
            ))}
          </ul>
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
