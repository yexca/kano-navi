import { useState, type SubmitEvent } from "react"
import { CalendarCheck, Link2 } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import {
  formatDay,
  japanClock,
  japanDay,
  japanInstant,
  japanStamp,
  japanToday,
} from "@/admin/format"
import {
  eventPlatforms,
  isLiveRoomLink,
  type AdminEvent,
  type EventInput,
  type EventPlatform,
} from "@/admin/types"
import { Button, Field, FieldGroup, Modal, Segmented } from "@/admin/ui"
import { useAdmin } from "@/admin/use-admin-data"
import { EventEvidencePanel } from "@/admin/views/event-evidence"

const eventTypes = [
  "stream",
  "member",
  "appearance",
  "release",
  "sale",
  "event",
]

/**
 * The calendar labels an entry can carry. "" is the operator's "no label";
 * "auto" hands the label back to the title inference and the entry's videos.
 */
const eventTags = ["live", "game", "talk", "song", "video"]

/** The form's limit on an entry's detail, shown by its counter. */
const detailLimit = 500

/** Preset lengths, in minutes, that save an end time. */
const lengths = [30, 60, 120, 180]

/** The type's length, saved as the end of a timed entry on "default" and the
 * calendar feed's length for one without an end: an hour for the stream
 * types and half an hour otherwise (see the public feeds). */
function defaultMinutes(eventType: string) {
  return eventType === "stream" || eventType === "member" ? 60 : 30
}

interface Draft {
  title: string
  detail: string
  startsOn: string
  timed: boolean
  start: string
  /**
   * "default" saves the type's length, "open" saves no end, a preset is
   * minutes and "custom" uses end.
   */
  length: string
  end: string
  approximate: boolean
  eventType: string
  /** A sale's close date and time; ignored for every other type. */
  closesOn: string
  closesAt: string
  tag: string
  status: string
  url: string
  /** The operator's platforms; both YouTube and bilibili for a simulcast. */
  platforms: EventPlatform[]
  cancellationStatus: "none" | "llm_suspected" | "manual_confirmed"
  cancellationReason: string
}

function draftOf(event: AdminEvent | null, day?: string): Draft {
  const startsAt = Date.parse(event?.startsAt || "")
  const minutes = (Date.parse(event?.endsAt || "") - startsAt) / 60_000
  // A sale's end is its close, not a length after the start.
  const sale = event?.eventType === "sale"
  return {
    title: event?.title || "",
    detail: event?.detail || "",
    startsOn: event?.startsOn || day || japanToday(),
    // A new entry starts timed so a date-only entry is a deliberate choice.
    timed: event ? Boolean(event.startsAt) : true,
    start: japanClock(event?.startsAt),
    // A new entry takes its type's length; a saved entry without an end
    // stays open, so an unrelated edit never adds one.
    length: !event
      ? "default"
      : !event.endsAt || sale
        ? "open"
        : lengths.includes(minutes)
          ? String(minutes)
          : "custom",
    end: sale ? "" : japanClock(event?.endsAt),
    approximate: event?.timePrecision === "approximate",
    eventType: event?.eventType || "stream",
    // A close time keeps its date, so saving never drops it.
    closesOn: event?.endsOn || (sale ? japanDay(event?.endsAt) : ""),
    closesAt: sale ? japanClock(event?.endsAt) : "",
    // An entry without an operator's label reads as automatic, so saving it
    // hands the label back rather than recording the operator's "no label".
    tag: event?.tag || (event?.tagSource === "manual" ? "" : "auto"),
    status: event?.status || "",
    // A stored live-room link reads as no link of a bilibili entry, whose
    // platforms already include bilibili.
    url:
      event?.url &&
      isLiveRoomLink(event.url) &&
      !event.platforms?.includes("other")
        ? ""
        : event?.url || "",
    platforms: event?.platforms || [],
    cancellationStatus: event?.cancellationStatus || "none",
    cancellationReason: event?.cancellationReason || "",
  }
}

/**
 * The draft with a live-room link moved to the bilibili platform, or the
 * same draft when its link is not one or the entry is in another room.
 */
function withoutRoomLink(draft: Draft): Draft {
  if (!isLiveRoomLink(draft.url) || draft.platforms.includes("other"))
    return draft
  return {
    ...draft,
    url: "",
    platforms: eventPlatforms.filter(
      (platform) =>
        platform === "bilibili" || draft.platforms.includes(platform),
    ),
  }
}

/** The saved end instant; a custom end clock before the start is the next day. */
function endOf(draft: Draft, startsAt: string) {
  if (!startsAt || draft.length === "open") return ""
  if (draft.length !== "custom") {
    const minutes =
      draft.length === "default"
        ? defaultMinutes(draft.eventType)
        : Number(draft.length)
    const end = new Date(Date.parse(startsAt) + minutes * 60_000)
    return japanInstant(
      japanDay(end.toISOString()),
      japanClock(end.toISOString()),
    )
  }
  if (!draft.end) return ""
  let endDay = draft.startsOn
  if (draft.end < draft.start) {
    const next = new Date(`${draft.startsOn}T12:00:00+09:00`)
    next.setUTCDate(next.getUTCDate() + 1)
    endDay = japanDay(next.toISOString())
  }
  return japanInstant(endDay, draft.end)
}

function inputOf(draft: Draft): EventInput {
  const startsAt =
    draft.timed && draft.start ? japanInstant(draft.startsOn, draft.start) : ""
  const sale = draft.eventType === "sale"
  const endsOn = sale ? draft.closesOn : ""
  return {
    title: draft.title,
    detail: draft.detail,
    startsOn: draft.startsOn,
    startsAt,
    endsAt: sale
      ? endsOn && draft.closesAt
        ? japanInstant(endsOn, draft.closesAt)
        : ""
      : endOf(draft, startsAt),
    endsOn,
    timePrecision: startsAt
      ? draft.approximate
        ? "approximate"
        : "exact"
      : undefined,
    eventType: draft.eventType,
    tag: draft.tag,
    status: draft.status,
    url: draft.url,
    platforms: draft.platforms,
    cancellationStatus: draft.cancellationStatus,
    cancellationReason:
      draft.cancellationStatus === "manual_confirmed"
        ? draft.cancellationReason
        : "",
  }
}

export function EventDialog({
  event,
  day,
  onClose,
}: {
  event: AdminEvent | null
  /** The date a new entry starts on; today when unset. */
  day?: string
  onClose: (saved?: boolean) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const [draft, setDraft] = useState(() => draftOf(event, day))
  const [busy, setBusy] = useState(false)
  // The rarer fields open on their own only when the entry already uses them.
  const [moreOpen] = useState(
    () => Boolean(draft.status) || draft.cancellationStatus !== "none",
  )
  const set = (patch: Partial<Draft>) =>
    setDraft((current) => ({ ...current, ...patch }))
  // A live-room link is the bilibili platform, not the entry's link, unless
  // the entry is in someone else's room (Other). It turns once the field is
  // left or the entry saved, so a link typed letter by letter stays whole.
  const [roomHint, setRoomHint] = useState(false)
  const settleRoomLink = () => {
    const settled = withoutRoomLink(draft)
    if (settled !== draft) {
      setRoomHint(true)
      setDraft(settled)
    }
    return settled
  }
  const togglePlatform = (platform: EventPlatform) =>
    set({
      platforms: eventPlatforms.filter((candidate) =>
        candidate === platform
          ? !draft.platforms.includes(candidate)
          : draft.platforms.includes(candidate),
      ),
    })
  const save = async (submit: SubmitEvent<HTMLFormElement>) => {
    submit.preventDefault()
    setBusy(true)
    const input = inputOf(settleRoomLink())
    const result = await act(
      () => (event ? api.updateEvent(event.id, input) : api.createEvent(input)),
      event ? "admin.notice.eventSaved" : "admin.notice.eventCreated",
    )
    setBusy(false)
    if (result) onClose(true)
  }
  const minutesLabel = (minutes: number) =>
    minutes < 60
      ? t("admin.interval.minutes", { count: minutes })
      : t("admin.interval.hours", { count: minutes / 60 })
  const fallback = minutesLabel(defaultMinutes(draft.eventType))

  // What a calendar subscriber sees for this entry once it is saved.
  const input = inputOf(draft)
  let preview = `${formatDay(draft.startsOn, locale)} · ${t("admin.schedule.allDay")}`
  if (input.endsOn) {
    // A sale shows at its opening and its close.
    const opening = input.startsAt
      ? `${formatDay(draft.startsOn, locale)} ${draft.start} JST`
      : formatDay(draft.startsOn, locale)
    const closing = input.endsAt
      ? `${formatDay(input.endsOn, locale)} ${draft.closesAt} JST`
      : formatDay(input.endsOn, locale)
    preview =
      input.endsOn === draft.startsOn && !input.startsAt
        ? closing
        : `${opening} → ${closing}`
  } else if (input.startsAt) {
    const end =
      input.endsAt ||
      new Date(
        Date.parse(input.startsAt) + defaultMinutes(draft.eventType) * 60_000,
      ).toISOString()
    const nextDay = japanDay(end) !== draft.startsOn ? " (+1)" : ""
    preview = `${formatDay(draft.startsOn, locale)} · ${draft.approximate ? "~" : ""}${draft.start}–${japanClock(end)}${nextDay} JST`
  } else if (draft.timed) {
    preview = t("admin.schedule.preview.needsStart")
  }

  return (
    <Modal
      open
      wide
      className={`adm-entry-dialog${event ? " has-evidence" : ""}`}
      title={event ? t("admin.schedule.edit") : t("admin.schedule.create")}
      onClose={() => onClose()}
      footer={
        <>
          <Button onClick={() => onClose()}>{t("admin.action.cancel")}</Button>
          <Button
            type="submit"
            form="adm-entry-form"
            variant="primary"
            busy={busy}
          >
            {event?.deletedAt
              ? t("admin.schedule.restore")
              : t("admin.action.save")}
          </Button>
        </>
      }
    >
      <div className="adm-entry-columns">
        <form id="adm-entry-form" className="adm-form" onSubmit={save}>
          {event ? (
            <p className="adm-muted adm-entry-updated">
              {t("admin.schedule.updated", {
                time: japanStamp(event.updatedAt),
              })}
            </p>
          ) : null}
          <Field label={t("admin.schedule.field.title")}>
            <input
              className="adm-entry-title"
              value={draft.title}
              onChange={(e) => set({ title: e.target.value })}
              maxLength={240}
              required
              autoFocus
            />
          </Field>
          <div className="adm-field">
            <span className="adm-field-label">
              {t("admin.schedule.field.type")}
            </span>
            <Segmented
              label={t("admin.schedule.field.type")}
              value={draft.eventType}
              options={eventTypes.map((type) => ({
                value: type,
                label: t(`admin.event.type.${type}`),
              }))}
              onChange={(eventType) => set({ eventType })}
            />
          </div>
          <Field
            label={t("admin.schedule.field.tag")}
            hint={
              event
                ? t(
                    event.tagSource === "manual"
                      ? "admin.schedule.tag.decidedManual"
                      : "admin.schedule.tag.decidedAutomatic",
                  )
                : undefined
            }
          >
            <select
              value={draft.tag}
              onChange={(e) => set({ tag: e.target.value })}
            >
              <option value="auto">{t("admin.schedule.tag.auto")}</option>
              <option value="">{t("admin.schedule.tag.none")}</option>
              {eventTags.map((tag) => (
                <option key={tag} value={tag}>
                  {t(`admin.event.tag.${tag}`)}
                </option>
              ))}
            </select>
          </Field>

          <section
            className="adm-entry-when"
            aria-label={t("admin.schedule.when")}
          >
            <header>
              <strong>{t("admin.schedule.when")}</strong>
              <Segmented
                label={t("admin.schedule.when")}
                value={draft.timed ? "timed" : "allDay"}
                options={[
                  { value: "timed", label: t("admin.schedule.timed") },
                  { value: "allDay", label: t("admin.schedule.allDay") },
                ]}
                onChange={(mode) => set({ timed: mode === "timed" })}
              />
            </header>
            <div
              className={`adm-form-row is-top${draft.timed ? " is-three" : ""}`}
            >
              <Field
                label={t("admin.schedule.field.date")}
                hint={t("admin.schedule.field.dateHint")}
              >
                <input
                  type="date"
                  value={draft.startsOn}
                  onChange={(e) => set({ startsOn: e.target.value })}
                  required
                />
              </Field>
              {draft.timed ? (
                <>
                  <Field
                    label={t("admin.schedule.field.start")}
                    hint={t("admin.schedule.field.startHint")}
                  >
                    <input
                      type="time"
                      value={draft.start}
                      onChange={(e) => set({ start: e.target.value })}
                      required
                    />
                  </Field>
                  {draft.eventType === "sale" ? null : (
                    <Field
                      label={t("admin.schedule.field.length")}
                      hint={
                        draft.length === "open"
                          ? t("admin.schedule.length.openHint", {
                              length: fallback,
                            })
                          : undefined
                      }
                    >
                      <select
                        value={draft.length}
                        onChange={(e) => set({ length: e.target.value })}
                      >
                        <option value="default">
                          {t("admin.schedule.length.default", {
                            length: fallback,
                          })}
                        </option>
                        <option value="open">
                          {t("admin.schedule.length.open")}
                        </option>
                        {lengths.map((minutes) => (
                          <option key={minutes} value={minutes}>
                            {minutesLabel(minutes)}
                          </option>
                        ))}
                        <option value="custom">
                          {t("admin.schedule.length.custom")}
                        </option>
                      </select>
                    </Field>
                  )}
                </>
              ) : null}
            </div>
            {draft.eventType === "sale" ? (
              <div className="adm-form-row is-top is-three">
                <Field
                  label={t("admin.schedule.field.closesOn")}
                  hint={t("admin.schedule.field.closesOnHint")}
                >
                  <input
                    type="date"
                    value={draft.closesOn}
                    min={draft.startsOn}
                    onChange={(e) => set({ closesOn: e.target.value })}
                  />
                </Field>
                <Field
                  label={t("admin.schedule.field.closesAt")}
                  hint={t("admin.schedule.field.closesAtHint")}
                >
                  <input
                    type="time"
                    value={draft.closesAt}
                    disabled={!draft.closesOn}
                    onChange={(e) => set({ closesAt: e.target.value })}
                  />
                </Field>
              </div>
            ) : null}
            {draft.eventType !== "sale" &&
            draft.timed &&
            draft.length === "custom" ? (
              <div className="adm-form-row is-three">
                <Field
                  label={t("admin.schedule.field.end")}
                  hint={t("admin.schedule.field.endHint")}
                >
                  <input
                    type="time"
                    value={draft.end}
                    onChange={(e) => set({ end: e.target.value })}
                    required
                  />
                </Field>
              </div>
            ) : null}
            {draft.timed ? (
              <label className="adm-check">
                <input
                  type="checkbox"
                  checked={draft.approximate}
                  onChange={(e) => set({ approximate: e.target.checked })}
                />
                {t("admin.schedule.field.approximate")}
              </label>
            ) : null}
            <p className="adm-entry-preview" aria-live="polite">
              <CalendarCheck aria-hidden />
              <span>
                <small>{t("admin.schedule.preview")}</small>
                {preview}
              </span>
            </p>
          </section>

          <FieldGroup
            label={t("admin.schedule.field.platforms")}
            hint={t("admin.schedule.field.platformsHint")}
          >
            <div
              className="adm-segmented"
              role="group"
              aria-label={t("admin.schedule.field.platforms")}
            >
              {eventPlatforms.map((platform) => (
                <button
                  key={platform}
                  type="button"
                  aria-pressed={draft.platforms.includes(platform)}
                  className={
                    draft.platforms.includes(platform) ? "is-active" : undefined
                  }
                  onClick={() => togglePlatform(platform)}
                >
                  {t(`admin.event.platform.${platform}`)}
                </button>
              ))}
            </div>
          </FieldGroup>
          <Field
            label={t("admin.schedule.field.url")}
            hint={roomHint ? t("admin.schedule.roomLinkHint") : undefined}
          >
            <span className="adm-input-icon">
              <Link2 aria-hidden />
              <input
                type="url"
                value={draft.url}
                onChange={(e) => set({ url: e.target.value })}
                onBlur={settleRoomLink}
                maxLength={2000}
                placeholder="https://"
              />
            </span>
          </Field>
          <Field label={t("admin.schedule.field.detail")}>
            <span className="adm-entry-detail">
              <textarea
                value={draft.detail}
                onChange={(e) => set({ detail: e.target.value })}
                maxLength={detailLimit}
                rows={2}
                placeholder={t("admin.schedule.field.detailPlaceholder")}
              />
              <span
                className={`adm-entry-count${draft.detail.length > detailLimit - 50 ? " is-near" : ""}`}
                aria-hidden
              >
                {draft.detail.length} / {detailLimit}
              </span>
            </span>
          </Field>

          <details className="adm-disclosure adm-entry-more" open={moreOpen}>
            <summary>{t("admin.schedule.more")}</summary>
            <div className="adm-form-row is-top">
              <Field label={t("admin.schedule.field.status")}>
                <input
                  value={draft.status}
                  onChange={(e) => set({ status: e.target.value })}
                  maxLength={80}
                />
              </Field>
              <Field label={t("admin.schedule.field.cancellation")}>
                <select
                  value={draft.cancellationStatus}
                  onChange={(e) =>
                    set({
                      cancellationStatus: e.target
                        .value as Draft["cancellationStatus"],
                    })
                  }
                >
                  <option value="none">
                    {t("admin.schedule.cancel.none")}
                  </option>
                  {event?.cancellationStatus === "llm_suspected" ? (
                    <option value="llm_suspected">
                      {t("admin.schedule.cancel.keep")}
                    </option>
                  ) : null}
                  <option value="manual_confirmed">
                    {t("admin.schedule.cancel.confirmed")}
                  </option>
                </select>
              </Field>
            </div>
            {draft.cancellationStatus === "manual_confirmed" ? (
              <Field label={t("admin.review.cancelReason")}>
                <input
                  value={draft.cancellationReason}
                  onChange={(e) => set({ cancellationReason: e.target.value })}
                  maxLength={500}
                  required
                />
              </Field>
            ) : null}
          </details>
        </form>
        {event ? (
          <aside className="adm-entry-evidence">
            <EventEvidencePanel eventId={event.id} />
          </aside>
        ) : null}
      </div>
    </Modal>
  )
}
