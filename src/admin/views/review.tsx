import { useId, useMemo, useState, type SubmitEvent } from "react"
import {
  CalendarCheck,
  CalendarRange,
  CalendarX,
  Check,
  CheckCircle2,
  ExternalLink,
  ImageOff,
  RotateCcw,
  Sparkles,
  TriangleAlert,
  X,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatDateTime, formatDay } from "@/admin/format"
import type { AdminEvent, AdminPost, ScheduleImage } from "@/admin/types"
import {
  Badge,
  Button,
  Empty,
  Facts,
  Field,
  IconButton,
  Loading,
  Modal,
  PageHeader,
  Panel,
  Segmented,
  StatusBadge,
  type Tone,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { eventToInput, EventWhen } from "@/admin/views/event-parts"
import { eventDetailText } from "@/lib/event-text"

const tabs = ["images", "cancellations", "posts"] as const
type Tab = (typeof tabs)[number]

/** A record without an image URL (mediaStatus "none") has nothing to check,
 * so it never waits for review; the operator can still label it. */
export function needsReview(image: ScheduleImage) {
  return (
    image.manualStatus === "unreviewed" &&
    image.mediaStatus !== "none" &&
    ["pending", "uncertain", "failed"].includes(image.llmStatus)
  )
}

function mediaLabel(image: ScheduleImage) {
  if (image.mediaStatus === "none")
    return image.periodBasis === "legacy" ? "noneLegacy" : "none"
  return ["pending", "failed"].includes(image.mediaStatus)
    ? image.mediaStatus
    : "missing"
}

function PeriodDialog({
  image,
  onClose,
}: {
  image: ScheduleImage
  onClose: (changed?: ScheduleImage) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [start, setStart] = useState(image.periodStart || "")
  const [end, setEnd] = useState(image.periodEnd || "")
  const [reason, setReason] = useState(image.periodReason || "")
  const [busy, setBusy] = useState(false)
  const save = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    const result = await act(
      () => api.setImagePeriod(image.id, start, end, reason),
      "admin.notice.periodSaved",
    )
    setBusy(false)
    if (result) onClose(result.image)
  }
  return (
    <Modal open title={t("admin.review.periodTitle")} onClose={() => onClose()}>
      <form className="adm-form" onSubmit={save}>
        <p className="adm-muted">{t("admin.review.periodHint")}</p>
        <div className="adm-form-row">
          <Field label={t("admin.review.periodStart")}>
            <input
              type="date"
              value={start}
              onChange={(e) => setStart(e.target.value)}
              required
            />
          </Field>
          <Field label={t("admin.review.periodEnd")}>
            <input
              type="date"
              value={end}
              min={start}
              onChange={(e) => setEnd(e.target.value)}
              required
            />
          </Field>
        </div>
        <Field label={t("admin.review.reason")}>
          <input
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            maxLength={500}
            required
          />
        </Field>
        <div className="adm-form-actions">
          <Button onClick={() => onClose()}>{t("admin.action.cancel")}</Button>
          <Button type="submit" variant="primary" busy={busy}>
            {t("admin.action.save")}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

/** Where an image stands, as the colour of its card's edge. */
function imageTone(image: ScheduleImage): Tone {
  if (needsReview(image)) return "honey"
  if (image.approved) return "leaf"
  return image.manualStatus === "not_schedule" ? "neutral" : "sky"
}

/** A model's confidence as a short bar beside the percentage. */
function ConfidenceMeter({ value, tone }: { value: number; tone: Tone }) {
  const percent = Math.round(value * 100)
  return (
    <span className={`adm-confidence adm-tone-${tone}`}>
      <span className="adm-confidence-bar" aria-hidden>
        <i style={{ width: `${percent}%` }} />
      </span>
      <strong>{percent}%</strong>
    </span>
  )
}

function ImageCard({
  image,
  onChanged,
}: {
  image: ScheduleImage
  onChanged: (image: ScheduleImage) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const [reason, setReason] = useState("")
  const [busy, setBusy] = useState("")
  const [editing, setEditing] = useState(false)
  const reasonId = useId()
  const noImage = image.mediaStatus === "none"
  const review = async (status: string) => {
    setBusy(status)
    const result = await act(
      () =>
        api.reviewImage(
          image.id,
          status,
          reason.trim() || t(`admin.review.defaultReason.${status}`),
        ),
      "admin.notice.imageReviewed",
    )
    setBusy("")
    if (result) {
      setReason("")
      onChanged(result.image)
    }
  }
  const tone = imageTone(image)
  return (
    <article className={`adm-decision adm-tone-${tone}`}>
      <a
        className="adm-decision-media"
        href={image.previewUrl || undefined}
        target="_blank"
        rel="noreferrer"
      >
        {image.previewUrl ? (
          <img src={image.previewUrl} alt={image.alt || ""} loading="lazy" />
        ) : (
          <span>
            <ImageOff aria-hidden />
            {t(`admin.review.media.${mediaLabel(image)}`)}
          </span>
        )}
      </a>
      <div className="adm-decision-body">
        <header className="adm-decision-head">
          <div className="adm-chip-row">
            <StatusBadge status={image.effectiveStatus} />
            {image.approved ? (
              <Badge tone="leaf" icon={Check}>
                {t("admin.review.approved")}
              </Badge>
            ) : null}
            {image.manualStatus !== "unreviewed" ? (
              <Badge tone="sky">{t("admin.review.manualLabel")}</Badge>
            ) : null}
            {!image.sourceMatchesBoard ? (
              <Badge tone="honey" icon={TriangleAlert}>
                {t("admin.review.textMismatch")}
              </Badge>
            ) : null}
          </div>
          {image.sourceUrl ? (
            <a
              className="adm-link"
              href={image.sourceUrl}
              target="_blank"
              rel="noreferrer"
            >
              {image.sourceAccount
                ? `@${image.sourceAccount}`
                : t("admin.review.sourcePost")}
              <ExternalLink aria-hidden />
            </a>
          ) : image.sourceAccount ? (
            <span className="adm-muted">@{image.sourceAccount}</span>
          ) : null}
        </header>
        <section className="adm-verdict">
          <div className="adm-verdict-head">
            <span className="adm-verdict-label">
              <Sparkles aria-hidden />
              {t("admin.review.model")}
            </span>
            <strong>
              {noImage
                ? t("admin.review.media.none")
                : image.llmCheckedAt
                  ? t(`admin.status.${image.llmStatus}`)
                  : t("admin.review.notChecked")}
            </strong>
            {!noImage && image.llmConfidence != null ? (
              <ConfidenceMeter value={image.llmConfidence} tone={tone} />
            ) : null}
          </div>
          {noImage ? (
            image.manualStatus === "unreviewed" ? (
              <p className="adm-muted">{t("admin.review.noImageHint")}</p>
            ) : null
          ) : image.llmReason ? (
            <p className="adm-verdict-reason">{image.llmReason}</p>
          ) : null}
          {image.manualReason ? (
            <p className="adm-quote is-manual">{image.manualReason}</p>
          ) : null}
        </section>
        <Facts
          items={[
            [
              t("admin.review.period"),
              image.periodStart ? (
                <>
                  {`${formatDay(image.periodStart, locale)} – ${formatDay(image.periodEnd, locale)}`}
                  {image.periodBasis === "manual" ? (
                    <Badge tone="sky">{t("admin.review.periodManual")}</Badge>
                  ) : null}
                </>
              ) : null,
            ],
            [
              t("admin.review.checked"),
              formatDateTime(image.llmCheckedAt, locale) || null,
            ],
          ]}
        />
        <footer className="adm-decision-bar">
          <label className="adm-visually-hidden" htmlFor={reasonId}>
            {t("admin.review.reason")}
          </label>
          <input
            id={reasonId}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            placeholder={t("admin.review.reasonPlaceholder")}
            maxLength={500}
          />
          <div className="adm-actions">
            <Button
              size="sm"
              variant="primary"
              icon={CalendarCheck}
              busy={busy === "schedule"}
              onClick={() => review("schedule")}
            >
              {t("admin.review.isSchedule")}
            </Button>
            <Button
              size="sm"
              icon={X}
              busy={busy === "not_schedule"}
              onClick={() => review("not_schedule")}
            >
              {t("admin.review.notSchedule")}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              icon={CalendarRange}
              onClick={() => setEditing(true)}
            >
              {t("admin.review.fixPeriod")}
            </Button>
            {image.manualStatus !== "unreviewed" ? (
              <Button
                size="sm"
                variant="ghost"
                icon={RotateCcw}
                busy={busy === "unreviewed"}
                onClick={() => review("unreviewed")}
              >
                {t("admin.review.clearLabel")}
              </Button>
            ) : null}
          </div>
        </footer>
      </div>
      {editing ? (
        <PeriodDialog
          image={image}
          onClose={(changed) => {
            setEditing(false)
            if (changed) onChanged(changed)
          }}
        />
      ) : null}
    </article>
  )
}

function ImagesTab() {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const [filter, setFilter] = useState<"review" | "approved" | "all">("review")
  const images = useResource(() => api.scheduleImages(), [])
  const items = images.data?.items || []
  const visible = useMemo(
    () =>
      items.filter((image) =>
        filter === "review"
          ? needsReview(image)
          : filter === "approved"
            ? image.approved
            : true,
      ),
    [items, filter],
  )
  if (!images.data) return <Loading />
  return (
    <>
      <div className="adm-toolbar">
        <Segmented
          label={t("admin.review.filter")}
          value={filter}
          onChange={setFilter}
          options={[
            {
              value: "review",
              label: t("admin.review.filter.review"),
              count: items.filter(needsReview).length,
            },
            { value: "approved", label: t("admin.review.filter.approved") },
            {
              value: "all",
              label: t("admin.review.filter.all"),
              count: items.length,
            },
          ]}
        />
      </div>
      {visible.length ? (
        <div className="adm-decision-list">
          {visible.map((image) => (
            <ImageCard
              key={image.id}
              image={image}
              onChanged={(next) =>
                images.setData({
                  items: items.map((item) =>
                    item.id === next.id ? next : item,
                  ),
                })
              }
            />
          ))}
        </div>
      ) : (
        <Empty icon={CheckCircle2} title={t("admin.review.noImages")} />
      )}
    </>
  )
}

function CancellationCard({
  event,
  onDone,
}: {
  event: AdminEvent
  onDone: () => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [reason, setReason] = useState(event.cancellationReason || "")
  const [busy, setBusy] = useState("")
  const reasonId = useId()
  const decide = async (status: "manual_confirmed" | "none") => {
    setBusy(status)
    const result = await act(
      () =>
        api.updateEvent(event.id, {
          ...eventToInput(event),
          cancellationStatus: status,
          cancellationReason: status === "manual_confirmed" ? reason : "",
        }),
      status === "none"
        ? "admin.notice.cancellationDismissed"
        : "admin.notice.cancellationConfirmed",
    )
    setBusy("")
    if (result) onDone()
  }
  return (
    <article className="adm-decision is-event adm-tone-honey">
      <div className="adm-decision-when">
        <EventWhen event={event} />
      </div>
      <div className="adm-decision-body">
        <header className="adm-decision-head">
          <div className="adm-decision-title">
            <h3>{event.title}</h3>
            {event.detail ? (
              <p className="adm-muted">{eventDetailText(event.detail, t)}</p>
            ) : null}
          </div>
          {event.url ? (
            <a
              className="adm-link"
              href={event.url}
              target="_blank"
              rel="noreferrer"
            >
              {t("admin.review.eventLink")}
              <ExternalLink aria-hidden />
            </a>
          ) : null}
        </header>
        <section className="adm-verdict">
          <div className="adm-verdict-head">
            <span className="adm-verdict-label">
              <Sparkles aria-hidden />
              {t("admin.review.cancelSuspected")}
            </span>
            {event.cancellationConfidence != null ? (
              <ConfidenceMeter
                value={event.cancellationConfidence}
                tone="honey"
              />
            ) : null}
          </div>
          {event.cancellationEvidence ? (
            <blockquote className="adm-verdict-reason">
              {event.cancellationEvidence}
            </blockquote>
          ) : null}
        </section>
        <footer className="adm-decision-bar">
          <label className="adm-visually-hidden" htmlFor={reasonId}>
            {t("admin.review.cancelReason")}
          </label>
          <input
            id={reasonId}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder={t("admin.review.cancelReasonPlaceholder")}
            maxLength={500}
          />
          <div className="adm-actions">
            <Button
              size="sm"
              variant="danger"
              icon={CalendarX}
              disabled={!reason.trim()}
              busy={busy === "manual_confirmed"}
              onClick={() => decide("manual_confirmed")}
            >
              {t("admin.review.confirmCancelled")}
            </Button>
            <Button
              size="sm"
              icon={CalendarCheck}
              busy={busy === "none"}
              onClick={() => decide("none")}
            >
              {t("admin.review.notCancelled")}
            </Button>
          </div>
        </footer>
      </div>
    </article>
  )
}

function CancellationsTab() {
  const { t } = useAppSettings()
  const { api, refreshOverview } = useAdmin()
  const events = useResource(
    () =>
      api.events({ cancellation: "llm_suspected", order: "asc", pageSize: 50 }),
    [],
  )
  if (!events.data) return <Loading />
  return events.data.items.length ? (
    <div className="adm-decision-list">
      <p className="adm-note">{t("admin.review.cancellationHint")}</p>
      {events.data.items.map((event) => (
        <CancellationCard
          key={event.id}
          event={event}
          onDone={() => {
            void events.reload()
            void refreshOverview().catch(() => {})
          }}
        />
      ))}
    </div>
  ) : (
    <Empty icon={CheckCircle2} title={t("admin.review.noCancellations")} />
  )
}

/**
 * The extraction state, last error, failure backoff and queue flag of one
 * post. A queued state already says the post waits, so the flag is not
 * repeated.
 */
export function PostStatus({ post }: { post: AdminPost }) {
  const { t, locale } = useAppSettings()
  return (
    <>
      <StatusBadge status={post.llmStatus} />
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
            time: formatDateTime(post.llmRetryAt, locale),
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
 * Have the LLM process a post again in the next schedule scan, and open it.
 * Compact actions are icons that fit a row; the caller lays them out.
 */
export function PostActions({
  post,
  onDone,
  compact = false,
}: {
  post: AdminPost
  onDone: () => void
  compact?: boolean
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [busy, setBusy] = useState(false)
  const reprocess = async () => {
    setBusy(true)
    const result = await act(
      () => api.reprocessPost(post.id),
      "admin.notice.postQueued",
    )
    setBusy(false)
    if (result) onDone()
  }
  if (compact)
    return (
      <>
        <IconButton
          icon={Sparkles}
          label={t("admin.posts.reprocess")}
          busy={busy}
          onClick={reprocess}
        />
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
      <Button size="sm" icon={Sparkles} busy={busy} onClick={reprocess}>
        {t("admin.posts.reprocess")}
      </Button>
      <a className="adm-link" href={post.url} target="_blank" rel="noreferrer">
        {t("admin.posts.open")}
        <ExternalLink aria-hidden />
      </a>
    </>
  )
}

export function PostRow({
  post,
  onDone,
}: {
  post: AdminPost
  onDone: () => void
}) {
  const { t, locale } = useAppSettings()
  return (
    <article className="adm-post-row">
      <div className="adm-post-meta">
        <strong>{post.accountHandle || t(`admin.step.${post.source}`)}</strong>
        <time dateTime={post.publishedAt}>
          {formatDateTime(post.publishedAt, locale)}
        </time>
        <PostStatus post={post} />
      </div>
      <p className="adm-post-text">{post.text}</p>
      <div className="adm-actions">
        <PostActions post={post} onDone={onDone} />
      </div>
    </article>
  )
}

function PostsTab() {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const [filter, setFilter] = useState<"failed" | "uncertain" | "queued">(
    "failed",
  )
  const posts = useResource(
    () =>
      api.posts(
        filter === "queued"
          ? { reprocess: true, pageSize: 50 }
          : { llm: filter, pageSize: 50 },
      ),
    [filter],
  )
  return (
    <>
      <div className="adm-toolbar">
        <Segmented
          label={t("admin.review.filter")}
          value={filter}
          onChange={setFilter}
          options={[
            { value: "failed", label: t("admin.status.failed") },
            { value: "uncertain", label: t("admin.status.uncertain") },
            { value: "queued", label: t("admin.posts.queued") },
          ]}
        />
      </div>
      {!posts.data ? (
        <Loading />
      ) : posts.data.items.length ? (
        <div className="adm-post-list">
          {posts.data.items.map((post) => (
            <PostRow
              key={post.id}
              post={post}
              onDone={() => void posts.reload()}
            />
          ))}
        </div>
      ) : (
        <Empty icon={CheckCircle2} title={t("admin.review.noPosts")} />
      )}
    </>
  )
}

export function ReviewView({ tab, navigate }: ViewProps) {
  const { t } = useAppSettings()
  const { overview } = useAdmin()
  const active: Tab = (tabs as readonly string[]).includes(tab)
    ? (tab as Tab)
    : "images"
  const counts = {
    images: overview?.review.pendingImages,
    cancellations: overview?.review.suspectedCancellations,
    posts: overview?.review.failedPosts,
  }
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.review.title")}
        description={t("admin.review.description")}
      />
      <Panel>
        <div
          className="adm-tabs"
          role="tablist"
          aria-label={t("admin.review.title")}
        >
          {tabs.map((id) => (
            <button
              key={id}
              type="button"
              role="tab"
              aria-selected={active === id}
              className={active === id ? "is-active" : undefined}
              onClick={() => navigate("review", id)}
            >
              {t(`admin.review.tab.${id}`)}
              {counts[id] ? <em>{counts[id]}</em> : null}
            </button>
          ))}
        </div>
        <div role="tabpanel" className="adm-tab-panel">
          {active === "images" ? <ImagesTab /> : null}
          {active === "cancellations" ? <CancellationsTab /> : null}
          {active === "posts" ? <PostsTab /> : null}
        </div>
      </Panel>
    </div>
  )
}
