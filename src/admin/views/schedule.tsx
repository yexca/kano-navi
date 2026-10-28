import { useState, type SubmitEvent } from "react"
import {
  CalendarCheck,
  CalendarPlus,
  CheckCheck,
  ExternalLink,
  Link2,
  Pencil,
  Search,
  Split,
  Trash2,
  Undo2,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import {
  eventStatusLabel,
  formatDateTime,
  formatDay,
  japanClock,
  japanDay,
  japanInstant,
  japanToday,
} from "@/admin/format"
import type { AdminEvent, EventInput } from "@/admin/types"
import { eventDetailText } from "@/lib/event-text"
import {
  Badge,
  Button,
  ConfirmButton,
  DateRange,
  Empty,
  Field,
  FilterReset,
  FilterSelect,
  FilterToggle,
  IconButton,
  Loading,
  Modal,
  PageHeader,
  Pager,
  Panel,
  Segmented,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import {
  CancellationBadge,
  EventWhen,
  MergeBadge,
  ProvenanceBadge,
} from "@/admin/views/event-parts"

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

function draftOf(event: AdminEvent | null): Draft {
  const startsAt = Date.parse(event?.startsAt || "")
  const minutes = (Date.parse(event?.endsAt || "") - startsAt) / 60_000
  return {
    title: event?.title || "",
    detail: event?.detail || "",
    startsOn: event?.startsOn || japanToday(),
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
  onClose,
}: {
  event: AdminEvent | null
  onClose: (saved?: boolean) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const [draft, setDraft] = useState(() => draftOf(event))
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

const blankFilters = {
  search: "",
  provenance: "",
  cancellation: "",
  merge: "",
  from: "",
  to: "",
  deleted: false,
}

export function ScheduleView(_: ViewProps) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  // The list opens on today onward; clearing the filters shows every date.
  const [filters, setFilters] = useState(() => ({
    ...blankFilters,
    from: japanToday(),
  }))
  const [search, setSearch] = useState("")
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState<AdminEvent | null | undefined>(
    undefined,
  )
  const events = useResource(
    () =>
      api.events({
        ...filters,
        page,
        pageSize: 25,
        order: filters.from ? "asc" : "desc",
      }),
    [filters, page],
  )
  const update = (patch: Partial<typeof filters>) => {
    setFilters((current) => ({ ...current, ...patch }))
    setPage(1)
  }
  const data = events.data
  const active = (
    Object.keys(blankFilters) as (keyof typeof blankFilters)[]
  ).filter((key) => filters[key] !== blankFilters[key]).length

  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.schedule.title")}
        description={t("admin.schedule.description")}
        actions={
          <Button
            variant="primary"
            icon={CalendarPlus}
            onClick={() => setEditing(null)}
          >
            {t("admin.schedule.create")}
          </Button>
        }
      />
      <Panel>
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
              onChange={(event) => setSearch(event.target.value)}
              onBlur={() => search !== filters.search && update({ search })}
              placeholder={t("admin.schedule.search")}
              aria-label={t("admin.schedule.search")}
            />
          </label>
          <FilterSelect
            label={t("admin.schedule.filter.provenance")}
            value={filters.provenance}
            onChange={(provenance) => update({ provenance })}
          >
            <option value="">{t("admin.filter.any")}</option>
            <option value="automatic">{t("admin.event.automatic")}</option>
            <option value="manual">{t("admin.event.manual")}</option>
          </FilterSelect>
          <FilterSelect
            label={t("admin.schedule.filter.cancellation")}
            value={filters.cancellation}
            onChange={(cancellation) => update({ cancellation })}
          >
            <option value="">{t("admin.filter.any")}</option>
            <option value="none">{t("admin.schedule.cancel.none")}</option>
            <option value="llm_suspected">{t("admin.event.suspected")}</option>
            <option value="manual_confirmed">
              {t("admin.event.cancelled")}
            </option>
          </FilterSelect>
          <FilterSelect
            label={t("admin.schedule.filter.merge")}
            value={filters.merge}
            onChange={(merge) => update({ merge })}
          >
            <option value="">{t("admin.schedule.filter.anyMerge")}</option>
            <option value="visible">
              {t("admin.schedule.filter.visible")}
            </option>
            <option value="merged">{t("admin.schedule.filter.merged")}</option>
          </FilterSelect>
          <DateRange
            from={filters.from}
            to={filters.to}
            onChange={(range) => update(range)}
          />
          <FilterToggle
            checked={filters.deleted}
            onChange={(deleted) => update({ deleted })}
          >
            {t("admin.schedule.filter.deleted")}
          </FilterToggle>
          <FilterReset
            count={active}
            onReset={() => {
              setSearch("")
              update(blankFilters)
            }}
          />
        </form>
        {!data ? (
          <Loading />
        ) : data.items.length ? (
          <>
            <div
              className="adm-table"
              role="table"
              aria-label={t("admin.schedule.title")}
            >
              {data.items.map((event) => (
                <div
                  key={event.id}
                  role="row"
                  className={`adm-event-row${event.deletedAt ? " is-deleted" : ""}${event.mergedInto ? " is-merged" : ""}`}
                >
                  <span role="cell">
                    <EventWhen event={event} />
                  </span>
                  <span role="cell" className="adm-event-main">
                    <strong>{event.title}</strong>
                    {event.detail ? (
                      <small>{eventDetailText(event.detail, t)}</small>
                    ) : null}
                    <span className="adm-chip-row">
                      <Badge>
                        {t(`admin.event.type.${event.eventType || "event"}`)}
                      </Badge>
                      <ProvenanceBadge event={event} />
                      <CancellationBadge event={event} />
                      <MergeBadge event={event} />
                      {event.status ? (
                        <Badge tone="sky">
                          {eventStatusLabel(event.status, t)}
                        </Badge>
                      ) : null}
                      {event.deletedAt ? (
                        <Badge tone="danger">
                          {t("admin.schedule.deleted")}
                        </Badge>
                      ) : null}
                    </span>
                  </span>
                  <span role="cell" className="adm-event-updated">
                    {formatDateTime(event.updatedAt, locale)}
                  </span>
                  <span role="cell" className="adm-row-actions">
                    {event.url ? (
                      <a
                        className="adm-icon-button"
                        href={event.url}
                        target="_blank"
                        rel="noreferrer"
                        aria-label={t("admin.review.eventLink")}
                        title={t("admin.review.eventLink")}
                      >
                        <ExternalLink aria-hidden />
                      </a>
                    ) : null}
                    {event.mergedInto && !event.deletedAt ? (
                      <IconButton
                        icon={Split}
                        label={t("admin.schedule.unmerge")}
                        onClick={async () => {
                          if (
                            await act(
                              () => api.unmergeEvent(event.id),
                              "admin.notice.eventUnmerged",
                            )
                          )
                            void events.reload()
                        }}
                      />
                    ) : null}
                    {!event.manualLocked && !event.deletedAt ? (
                      <IconButton
                        icon={CheckCheck}
                        label={t("admin.schedule.confirm")}
                        onClick={async () => {
                          if (
                            await act(
                              () => api.confirmEvent(event.id),
                              "admin.notice.eventConfirmed",
                            )
                          )
                            void events.reload()
                        }}
                      />
                    ) : null}
                    <IconButton
                      icon={event.deletedAt ? Undo2 : Pencil}
                      label={
                        event.deletedAt
                          ? t("admin.schedule.restore")
                          : t("admin.schedule.edit")
                      }
                      onClick={() => setEditing(event)}
                    />
                    {!event.deletedAt ? (
                      <ConfirmButton
                        size="sm"
                        variant="ghost"
                        icon={Trash2}
                        aria-label={t("admin.schedule.delete")}
                        onConfirm={async () => {
                          // A delete answers null; a failure is undefined.
                          const result = await act(
                            () => api.deleteEvent(event.id),
                            "admin.notice.eventDeleted",
                          )
                          if (result !== undefined) void events.reload()
                        }}
                      />
                    ) : null}
                  </span>
                </div>
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
          event={editing}
          onClose={(saved) => {
            setEditing(undefined)
            if (saved) void events.reload()
          }}
        />
      ) : null}
    </div>
  )
}
