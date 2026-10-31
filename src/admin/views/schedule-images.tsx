import {
  useId,
  useMemo,
  useState,
  type ReactNode,
  type SubmitEvent,
} from "react"
import {
  CalendarCheck,
  CalendarRange,
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
import { formatDateTime, formatDay } from "@/admin/format"
import type { ScheduleImage } from "@/admin/types"
import {
  Badge,
  Button,
  Empty,
  Facts,
  Field,
  Loading,
  Modal,
  Segmented,
  StatusBadge,
  type Tone,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"

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
export function ConfidenceMeter({
  value,
  tone,
}: {
  value: number
  tone: Tone
}) {
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

/** One schedule image as a decision card: the model's verdict, the covered
 * dates and the operator's label. The Review queue lists those that need a
 * decision; the Schedule page lists them all. */
export function ImageCard({
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

type ImageFilter = "review" | "approved" | "all"

function matches(image: ScheduleImage, filter: ImageFilter) {
  if (filter === "review") return needsReview(image)
  return filter === "approved" ? image.approved : true
}

/**
 * The newest stored schedule images under the first of the filters; more
 * than one filter shows them as tabs. A card that leaves the filter after a
 * decision leaves the list, and the overview's review count reads again.
 */
export function ScheduleImageList({
  filters,
  actions,
}: {
  filters: ImageFilter[]
  actions?: ReactNode
}) {
  const { t } = useAppSettings()
  const { api, refreshOverview } = useAdmin()
  const [filter, setFilter] = useState(filters[0])
  const images = useResource(() => api.scheduleImages(), [])
  const items = images.data?.items || []
  const visible = useMemo(
    () => items.filter((image) => matches(image, filter)),
    [items, filter],
  )
  if (!images.data) return <Loading />
  return (
    <>
      {filters.length > 1 || actions ? (
        <div className="adm-toolbar">
          {filters.length > 1 ? (
            <Segmented
              label={t("admin.review.filter")}
              value={filter}
              onChange={setFilter}
              options={filters.map((value) => ({
                value,
                label: t(`admin.review.filter.${value}`),
                count:
                  value === "approved"
                    ? undefined
                    : items.filter((image) => matches(image, value)).length,
              }))}
            />
          ) : (
            <span />
          )}
          {actions}
        </div>
      ) : null}
      {visible.length ? (
        <div className="adm-decision-list">
          {visible.map((image) => (
            <ImageCard
              key={image.id}
              image={image}
              onChanged={(next) => {
                images.setData({
                  items: items.map((item) =>
                    item.id === next.id ? next : item,
                  ),
                })
                void refreshOverview().catch(() => {})
              }}
            />
          ))}
        </div>
      ) : (
        <Empty
          icon={CheckCircle2}
          title={t(
            filter === "review"
              ? "admin.review.noImages"
              : "admin.schedule.images.empty",
          )}
        />
      )}
    </>
  )
}
