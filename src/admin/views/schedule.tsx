import { useState, type SubmitEvent } from "react"
import {
  CalendarCheck,
  CalendarPlus,
  ChevronLeft,
  ChevronRight,
  Link2,
  Search,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import {
  formatDateTime,
  formatDay,
  formatMonth,
  japanClock,
  japanDay,
  japanInstant,
  japanToday,
} from "@/admin/format"
import type { AdminEvent, EventInput } from "@/admin/types"
import {
  Button,
  DateRange,
  Empty,
  Field,
  IconButton,
  Loading,
  Modal,
  PageHeader,
  Pager,
  Panel,
  Segmented,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { EventMergeDialog, shiftDay } from "@/admin/views/event-parts"
import { ScheduleCalendar, shiftMonth } from "@/admin/views/schedule-calendar"
import { DayHeading, EventRow, groupByDay } from "@/admin/views/schedule-parts"

const eventTypes = ["stream", "member", "appearance", "release", "event"]

/** The form's limit on an entry's detail, shown by its counter. */
const detailLimit = 500

/** Preset lengths, in minutes, that save an end time. */
const lengths = [30, 60, 120, 180]

/** The calendar feed's length for a timed entry without an end: an hour for
 * the stream types and half an hour otherwise (see the public feeds). */
function defaultMinutes(eventType: string) {
  return eventType === "stream" || eventType === "member" ? 60 : 30
}

interface Draft {
  title: string
  detail: string
  startsOn: string
  timed: boolean
  start: string
  /** "" keeps the end open, a preset is minutes, "custom" uses end. */
  length: string
  end: string
  approximate: boolean
  eventType: string
  status: string
  url: string
  cancellationStatus: "none" | "llm_suspected" | "manual_confirmed"
  cancellationReason: string
}

function draftOf(event: AdminEvent | null, day?: string): Draft {
  const startsAt = Date.parse(event?.startsAt || "")
  const minutes = (Date.parse(event?.endsAt || "") - startsAt) / 60_000
  return {
    title: event?.title || "",
    detail: event?.detail || "",
    startsOn: event?.startsOn || day || japanToday(),
    // A new entry starts timed so a date-only entry is a deliberate choice.
    timed: event ? Boolean(event.startsAt) : true,
    start: japanClock(event?.startsAt),
    length: !event?.endsAt
      ? ""
      : lengths.includes(minutes)
        ? String(minutes)
        : "custom",
    end: japanClock(event?.endsAt),
    approximate: event?.timePrecision === "approximate",
    eventType: event?.eventType || "stream",
    status: event?.status || "",
    url: event?.url || "",
    cancellationStatus: event?.cancellationStatus || "none",
    cancellationReason: event?.cancellationReason || "",
  }
}

/** The saved end instant; a custom end clock before the start is the next day. */
function endOf(draft: Draft, startsAt: string) {
  if (!startsAt || !draft.length) return ""
  if (draft.length !== "custom") {
    const end = new Date(Date.parse(startsAt) + Number(draft.length) * 60_000)
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
  return {
    title: draft.title,
    detail: draft.detail,
    startsOn: draft.startsOn,
    startsAt,
    endsAt: endOf(draft, startsAt),
    timePrecision: startsAt
      ? draft.approximate
        ? "approximate"
        : "exact"
      : undefined,
    eventType: draft.eventType,
    status: draft.status,
    url: draft.url,
    cancellationStatus: draft.cancellationStatus,
    cancellationReason:
      draft.cancellationStatus === "manual_confirmed"
        ? draft.cancellationReason
        : "",
  }
}

function EventDialog({
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
  const save = async (submit: SubmitEvent<HTMLFormElement>) => {
    submit.preventDefault()
    setBusy(true)
    const input = inputOf(draft)
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
  if (input.startsAt) {
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
      className="adm-entry-dialog"
      title={event ? t("admin.schedule.edit") : t("admin.schedule.create")}
      onClose={() => onClose()}
      footer={
        <>
          <span className="adm-muted">{t("admin.schedule.lockHint")}</span>
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
      <form id="adm-entry-form" className="adm-form" onSubmit={save}>
        {event ? (
          <p className="adm-muted adm-entry-updated">
            {t("admin.schedule.updated", {
              time: formatDateTime(event.updatedAt, locale),
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
                <Field
                  label={t("admin.schedule.field.length")}
                  hint={
                    draft.length
                      ? undefined
                      : t("admin.schedule.length.openHint", {
                          length: fallback,
                        })
                  }
                >
                  <select
                    value={draft.length}
                    onChange={(e) => set({ length: e.target.value })}
                  >
                    <option value="">
                      {t("admin.schedule.length.default", {
                        length: fallback,
                      })}
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
              </>
            ) : null}
          </div>
          {draft.timed && draft.length === "custom" ? (
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

        <Field label={t("admin.schedule.field.url")}>
          <span className="adm-input-icon">
            <Link2 aria-hidden />
            <input
              type="url"
              value={draft.url}
              onChange={(e) => set({ url: e.target.value })}
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
            <Field
              label={t("admin.schedule.field.status")}
              hint={t("admin.schedule.field.statusHint")}
            >
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
                <option value="none">{t("admin.schedule.cancel.none")}</option>
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
    </Modal>
  )
}

type Scope = "upcoming" | "past" | "all"

/** The date range each scope tab sets; any other range is custom. */
function scopeRange(scope: Scope, today: string) {
  if (scope === "upcoming") return { from: today, to: "" }
  if (scope === "past") return { from: "", to: shiftDay(today, -1) }
  return { from: "", to: "" }
}

function scopeOf(from: string, to: string, today: string): Scope | "" {
  return (
    (["upcoming", "past", "all"] as const).find((scope) => {
      const range = scopeRange(scope, today)
      return range.from === from && range.to === to
    }) ?? ""
  )
}

type ScheduleMode = "list" | "calendar"
type Status = "all" | "unconfirmed" | "suspected" | "deleted"

/** Each status tab is a review task; All keeps merged duplicates, dimmed. */
const statusFilters: Record<Status, Record<string, string>> = {
  all: {},
  unconfirmed: { provenance: "unconfirmed" },
  suspected: { cancellation: "llm_suspected" },
  deleted: { deleted: "only" },
}

const modeKey = "adm-schedule-view"

/** The view this browser used last; storage may be unavailable. */
function storedMode(): ScheduleMode {
  try {
    return window.localStorage.getItem(modeKey) === "calendar"
      ? "calendar"
      : "list"
  } catch {
    return "list"
  }
}

export function ScheduleView(_: ViewProps) {
  const { t, locale } = useAppSettings()
  const { api } = useAdmin()
  const today = japanToday()
  const [mode, setMode] = useState(storedMode)
  const [status, setStatus] = useState<Status>("all")
  const [search, setSearch] = useState("")
  const [query, setQuery] = useState("")
  // The list opens on today onward; the calendar on this month.
  const [range, setRange] = useState(() => scopeRange("upcoming", today))
  const [month, setMonth] = useState(() => shiftMonth(today, 0))
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState<
    { event: AdminEvent | null; day?: string } | undefined
  >(undefined)
  const [merging, setMerging] = useState<AdminEvent | null>(null)
  // Bumped after a dialog saves, so whichever view is shown reads again.
  const [reloadKey, setReloadKey] = useState(0)
  const reload = () => setReloadKey((value) => value + 1)
  const filters = { ...statusFilters[status], search: query }
  const scope = scopeOf(range.from, range.to, today)
  const events = useResource(
    () =>
      mode === "list"
        ? api.events({
            ...filters,
            ...range,
            page,
            pageSize: 50,
            // A range with a start reads forward from it; the past and all
            // dates read back from the newest.
            order: range.from ? "asc" : "desc",
          })
        : Promise.resolve(null),
    [mode, status, query, range, page, reloadKey],
  )
  const changeMode = (next: ScheduleMode) => {
    setMode(next)
    try {
      window.localStorage.setItem(modeKey, next)
    } catch {
      // The choice simply is not remembered.
    }
  }
  const changeRange = (patch: { from?: string; to?: string }) => {
    setRange((current) => ({ ...current, ...patch }))
    setPage(1)
  }
  const submitSearch = () => {
    if (search === query) return
    setQuery(search)
    setPage(1)
  }
  const data = events.data

  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.schedule.title")}
        description={t("admin.schedule.description")}
        actions={
          <Button
            variant="primary"
            icon={CalendarPlus}
            onClick={() => setEditing({ event: null })}
          >
            {t("admin.schedule.create")}
          </Button>
        }
      />
      <Panel>
        <div className="adm-schedule-bar">
          <Segmented
            label={t("admin.schedule.view")}
            value={mode}
            options={[
              { value: "list", label: t("admin.schedule.view.list") },
              { value: "calendar", label: t("admin.schedule.view.calendar") },
            ]}
            onChange={changeMode}
          />
          {mode === "list" ? (
            <div className="adm-schedule-dates">
              <Segmented
                label={t("admin.schedule.scope")}
                value={scope}
                options={(["upcoming", "past", "all"] as const).map(
                  (value) => ({
                    value,
                    label: t(`admin.schedule.scope.${value}`),
                  }),
                )}
                onChange={(value) => {
                  if (value) changeRange(scopeRange(value, today))
                }}
              />
              <DateRange
                from={range.from}
                to={range.to}
                onChange={changeRange}
              />
            </div>
          ) : (
            <div className="adm-month-nav">
              <IconButton
                icon={ChevronLeft}
                label={t("admin.schedule.month.previous")}
                onClick={() => setMonth(shiftMonth(month, -1))}
              />
              <strong aria-live="polite">{formatMonth(month, locale)}</strong>
              <IconButton
                icon={ChevronRight}
                label={t("admin.schedule.month.next")}
                onClick={() => setMonth(shiftMonth(month, 1))}
              />
              <Button
                size="sm"
                variant="ghost"
                disabled={month === shiftMonth(today, 0)}
                onClick={() => setMonth(shiftMonth(today, 0))}
              >
                {t("admin.schedule.month.today")}
              </Button>
            </div>
          )}
        </div>
        <div className="adm-list-tools">
          <Segmented
            label={t("admin.schedule.status")}
            value={status}
            options={(
              ["all", "unconfirmed", "suspected", "deleted"] as const
            ).map((value) => ({
              value,
              label: t(`admin.schedule.status.${value}`),
            }))}
            onChange={(value) => {
              setStatus(value)
              setPage(1)
            }}
          />
          <form
            className="adm-search"
            role="search"
            onSubmit={(event) => {
              event.preventDefault()
              submitSearch()
            }}
          >
            <Search aria-hidden />
            <input
              type="search"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              onBlur={submitSearch}
              placeholder={t("admin.schedule.search")}
              aria-label={t("admin.schedule.search")}
            />
          </form>
        </div>
        {mode === "calendar" ? (
          <ScheduleCalendar
            month={month}
            // Merged duplicates stay off the grid; the list shows them.
            filters={{ ...filters, merge: "visible" }}
            reloadKey={reloadKey}
            onEdit={(event) => setEditing({ event })}
            onMerge={setMerging}
            onCreate={(day) => setEditing({ event: null, day })}
          />
        ) : !data ? (
          <Loading />
        ) : data.items.length ? (
          <>
            <div className="adm-schedule-days">
              {groupByDay(data.items).map(({ day, events: items }) => (
                <section key={day} className="adm-day">
                  <DayHeading
                    day={day}
                    today={today}
                    count={items.length}
                    onCreate={(chosen) =>
                      setEditing({ event: null, day: chosen })
                    }
                  />
                  <ul className="adm-event-list">
                    {items.map((event) => (
                      <EventRow
                        key={event.id}
                        event={event}
                        onEdit={(chosen) => setEditing({ event: chosen })}
                        onMerge={setMerging}
                        onChanged={reload}
                      />
                    ))}
                  </ul>
                </section>
              ))}
            </div>
            <Pager
              page={data.page}
              totalPages={data.totalPages}
              total={data.total}
              onPage={setPage}
            />
          </>
        ) : (
          <Empty icon={CalendarPlus} title={t("admin.schedule.empty")}>
            {t("admin.schedule.emptyHint")}
          </Empty>
        )}
      </Panel>
      {editing !== undefined ? (
        <EventDialog
          event={editing.event}
          day={editing.day}
          onClose={(saved) => {
            setEditing(undefined)
            if (saved) reload()
          }}
        />
      ) : null}
      {merging ? (
        <EventMergeDialog
          event={merging}
          onClose={(merged) => {
            setMerging(null)
            if (merged) reload()
          }}
        />
      ) : null}
    </div>
  )
}
