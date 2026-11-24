import { useEffect, useLayoutEffect, useRef, useState } from "react"
import {
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronUp,
  ExternalLink,
  Eye,
  EyeOff,
  ImageOff,
  Quote,
  Ban,
  Languages,
  RefreshCw,
  Sparkles,
  X,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { japanStamp } from "@/admin/format"
import type { AdminPost, AdminPostImage } from "@/admin/types"
import { Badge, Button, IconButton, Modal, StatusBadge } from "@/admin/ui"
import { useAdmin } from "@/admin/use-admin-data"
import { cn } from "@/lib/utils"

/** Only local, already cached media is shown; never a remote CDN URL. */
export function localUrl(value: string | null | undefined) {
  return value && value.startsWith("/") ? value : null
}

/** The message key for an image that has no cached file to show yet. */
export function missingKey(status: string) {
  return `admin.review.media.${["pending", "failed"].includes(status) ? status : "missing"}`
}

/**
 * A post's images, or its quote's, one at a time inside the page, fitted to
 * the viewport. The buttons and arrow keys step through several images.
 */
export function PostImageViewer({
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
export const shownImages = 3

/**
 * The post's cached images, or why there are none to show yet; a text-only
 * post shows nothing. Clicking an image enlarges the images in the page.
 */
export function PostImages({
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
 * (hide from the public pages or show again, open it), then its wording and
 * its cached thumbnails. Wording past three lines folds behind a toggle;
 * `full` shows it all. A Bilibili forward shows the dynamic it forwards, its
 * author by name or mid.
 */
export function PostQuote({
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
  const [busy, setBusy] = useState(false)
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
  const bilibili = post.source === "bilibili"
  if ((post.source !== "x" && !bilibili) || !quote) return null
  const hidden = Boolean(post.quoteHiddenAt)
  const images = (quote.media || []).filter((image) => localUrl(image.url))
  const toggle = async () => {
    setBusy(true)
    const result = await act(
      () => api.setQuoteHidden(post.id, !hidden),
      hidden ? "admin.notice.quoteShown" : "admin.notice.quoteHidden",
    )
    setBusy(false)
    if (result) onDone()
  }
  const available = !quote.unavailable
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
                (quote.authorHandle && !bilibili
                  ? `@${quote.authorHandle}`
                  : quote.authorHandle || "")}
            </strong>
            {quote.authorName && quote.authorHandle ? (
              <span>
                {bilibili ? quote.authorHandle : `@${quote.authorHandle}`}
              </span>
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
        <span className="adm-post-quote-actions">
          <IconButton
            icon={hidden ? Eye : EyeOff}
            label={t(
              hidden ? "admin.posts.quote.show" : "admin.posts.quote.hide",
            )}
            busy={busy}
            onClick={() => void toggle()}
          />
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
        </span>
      </header>
      {quote.unavailable ? (
        <p className="adm-post-quote-missing">
          {t("admin.posts.quote.unavailable")}
        </p>
      ) : quote.text ? (
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
 * Who posted: an X post's @handle, or the source's name for another source
 * (a Bilibili dynamic's handle is the account's numeric mid).
 */
export function PostAccount({ post }: { post: AdminPost }) {
  const { t } = useAppSettings()
  return post.source === "x" && post.accountHandle
    ? `@${post.accountHandle.replace(/^@/, "")}`
    : t(`admin.step.${post.source}`)
}

/**
 * The extraction state, last error, failure backoff and queue flag of one
 * post. A failure names the schedule extraction, apart from a failed
 * translation. A queued state already says the post waits, so the flag is
 * not repeated.
 */
export function PostStatus({ post }: { post: AdminPost }) {
  const { t } = useAppSettings()
  return (
    <>
      {post.llmStatus === "failed" ? (
        <Badge tone="danger">{t("admin.review.failure.extraction")}</Badge>
      ) : (
        <StatusBadge status={post.llmStatus} />
      )}
      {post.llmLastError ? (
        <Badge tone="danger">
          {t(`admin.posts.error.${post.llmLastError}`)}
        </Badge>
      ) : null}
      {post.llmRetryStopped ? (
        <Badge tone="warning" className="adm-retry-badge">
          {t("admin.posts.retryStopped")}
        </Badge>
      ) : post.llmRetryAt ? (
        <Badge tone="neutral" className="adm-retry-badge">
          {t("admin.posts.retryAt", {
            time: japanStamp(post.llmRetryAt),
          })}
        </Badge>
      ) : null}
      {post.llmReprocessRequested && post.llmStatus !== "queued" ? (
        <Badge tone="sky">{t("admin.posts.queued")}</Badge>
      ) : null}
    </>
  )
}

/**
 * Whether the X synchronization can read the post again from the source:
 * an own X post, not a related account's promoted status.
 */
export function refreshable(post: AdminPost) {
  return post.source === "x" && !post.promotedAt
}

/** A refresh from the source that waits for the next X synchronization. */
export function RefreshQueued({ post }: { post: AdminPost }) {
  const { t } = useAppSettings()
  return post.refreshRequestedAt ? (
    <Badge tone="running" icon={RefreshCw} className="adm-retry-badge">
      {t("admin.posts.refreshQueued", {
        time: japanStamp(post.refreshRequestedAt),
      })}
    </Badge>
  ) : null
}

/**
 * Have the LLM process a post again in the next schedule scan, queue an own
 * X post's refresh from the source for the next X synchronization, and open
 * it. Compact actions are icons that fit a row; the caller lays them out.
 */
export function PostActions({
  post,
  onDone,
  compact = false,
  reprocess: withReprocess = true,
}: {
  post: AdminPost
  onDone: () => void
  compact?: boolean
  /** False leaves only the link, for a post that failed only translation. */
  reprocess?: boolean
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [busy, setBusy] = useState<"" | "reprocess" | "refresh">("")
  const run = async (
    kind: "reprocess" | "refresh",
    call: () => Promise<unknown>,
    notice: string,
  ) => {
    setBusy(kind)
    const result = await act(call, notice)
    setBusy("")
    if (result) onDone()
  }
  const reprocess = () =>
    run(
      "reprocess",
      () => api.reprocessPost(post.id),
      "admin.notice.postQueued",
    )
  const refresh = () =>
    run("refresh", () => api.refreshPost(post.id), "admin.notice.refreshQueued")
  const queued = Boolean(post.refreshRequestedAt)
  if (compact)
    return (
      <>
        {withReprocess ? (
          <IconButton
            icon={Sparkles}
            label={t("admin.posts.reprocess")}
            busy={busy === "reprocess"}
            onClick={reprocess}
          />
        ) : null}
        {refreshable(post) ? (
          <IconButton
            icon={RefreshCw}
            label={t("admin.posts.refresh")}
            busy={busy === "refresh"}
            disabled={queued}
            onClick={refresh}
          />
        ) : null}
        <a
          className="adm-icon-button"
          href={post.url}
          target="_blank"
          rel="noreferrer"
          aria-label={t("admin.posts.open")}
          title={t("admin.posts.open")}
        >
          <ExternalLink aria-hidden />
        </a>
      </>
    )
  return (
    <>
      {withReprocess ? (
        <Button
          size="sm"
          icon={Sparkles}
          busy={busy === "reprocess"}
          onClick={reprocess}
        >
          {t("admin.posts.reprocess")}
        </Button>
      ) : null}
      {refreshable(post) ? (
        <Button
          size="sm"
          variant="ghost"
          icon={RefreshCw}
          busy={busy === "refresh"}
          disabled={queued}
          onClick={refresh}
        >
          {t("admin.posts.refresh")}
        </Button>
      ) : null}
      <a className="adm-link" href={post.url} target="_blank" rel="noreferrer">
        {t("admin.posts.open")}
        <ExternalLink aria-hidden />
      </a>
    </>
  )
}

export type BulkAction = "dismiss" | "reprocess" | "refresh" | "translate"

/**
 * The selection bar over a selectable post list: a page-wide box that reads
 * checked, mixed or empty, the count, and the filter's bulk actions. It
 * stays under the top bar while the list scrolls.
 */
export function PostSelectionBar({
  actions,
  selected,
  pageCount,
  busy,
  onSelectPage,
  onClear,
  onAction,
}: {
  actions: BulkAction[]
  selected: number
  pageCount: number
  busy: BulkAction | ""
  onSelectPage: (all: boolean) => void
  onClear: () => void
  onAction: (action: BulkAction) => void
}) {
  const { t } = useAppSettings()
  const all = selected > 0 && selected === pageCount
  const mixed = selected > 0 && !all
  const icons = {
    dismiss: Ban,
    reprocess: Sparkles,
    refresh: RefreshCw,
    translate: Languages,
  }
  const labels = {
    dismiss: t("admin.posts.dismiss"),
    reprocess: t("admin.review.bulkReprocess"),
    refresh: t("admin.posts.refresh"),
    translate: t("admin.review.retryTranslation"),
  }
  const titles: Partial<Record<BulkAction, string>> = {
    dismiss: t("admin.posts.dismissTitle"),
    reprocess: t("admin.posts.reprocess"),
  }
  return (
    <div className={cn("adm-select-bar", selected > 0 && "is-active")}>
      <label className="adm-check">
        <input
          type="checkbox"
          checked={all}
          ref={(input) => {
            if (input) input.indeterminate = mixed
          }}
          onChange={() => onSelectPage(!all)}
        />
        {selected ? (
          <strong aria-live="polite">
            {t("admin.review.selected", { count: selected })}
          </strong>
        ) : (
          t("admin.review.selectPage")
        )}
      </label>
      {selected ? (
        <IconButton
          icon={X}
          label={t("admin.review.clearSelection")}
          onClick={onClear}
        />
      ) : (
        <span className="adm-select-hint">{t("admin.review.selectHint")}</span>
      )}
      <div className="adm-actions">
        {actions.map((action) => (
          <Button
            key={action}
            size="sm"
            variant={
              action === "dismiss" || action === "refresh" ? "ghost" : undefined
            }
            icon={icons[action]}
            title={titles[action]}
            disabled={!selected || (Boolean(busy) && busy !== action)}
            busy={busy === action}
            onClick={() => onAction(action)}
          >
            {labels[action]}
          </Button>
        ))}
      </div>
    </div>
  )
}
