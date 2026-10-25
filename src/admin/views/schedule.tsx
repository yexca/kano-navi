import { useState, type SubmitEvent } from "react"
import {
  CalendarPlus,
  CheckCheck,
  ExternalLink,
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
  Empty,
  Field,
  IconButton,
  Loading,
  Modal,
  PageHeader,
  Pager,
  Panel,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import {
  CancellationBadge,
  EventWhen,
  MergeBadge,
  ProvenanceBadge,
} from "@/admin/views/event-parts"

const eventTypes = ["event", "stream", "member", "release", "appearance"]

interface Draft {
  title: string
  detail: string
  startsOn: string
  start: string
  end: string
  approximate: boolean
  eventType: string
  status: string
  url: string
  cancellationStatus: "none" | "llm_suspected" | "manual_confirmed"
  cancellationReason: string
}

function draftOf(event: AdminEvent | null): Draft {
  return {
    title: event?.title || "",
    detail: event?.detail || "",
    startsOn: event?.startsOn || japanToday(),
    start: japanClock(event?.startsAt),
    end: japanClock(event?.endsAt),
    approximate: event?.timePrecision === "approximate",
    eventType: event?.eventType || "stream",
    status: event?.status || "",
    url: event?.url || "",
    cancellationStatus: event?.cancellationStatus || "none",
    cancellationReason: event?.cancellationReason || "",
  }
}

/** Build the request; an end clock before the start means the next day. */
function inputOf(draft: Draft): EventInput {
  const startsAt = draft.start ? japanInstant(draft.startsOn, draft.start) : ""
  let endsAt = ""
  if (startsAt && draft.end) {
    let endDay = draft.startsOn
    if (draft.end < draft.start) {
      const next = new Date(`${draft.startsOn}T12:00:00+09:00`)
      next.setUTCDate(next.getUTCDate() + 1)
      endDay = japanDay(next.toISOString())
    }
    endsAt = japanInstant(endDay, draft.end)
  }
  return {
    title: draft.title,
    detail: draft.detail,
    startsOn: draft.startsOn,
    startsAt,
    endsAt,
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
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [draft, setDraft] = useState(() => draftOf(event))
  const [busy, setBusy] = useState(false)
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
  return (
    <Modal
      open
      wide
      title={event ? t("admin.schedule.edit") : t("admin.schedule.create")}
      onClose={() => onClose()}
    >
      <form className="adm-form" onSubmit={save}>
        <p className="adm-note">{t("admin.schedule.lockHint")}</p>
        <Field label={t("admin.schedule.field.title")}>
          <input
            value={draft.title}
            onChange={(e) => set({ title: e.target.value })}
            maxLength={240}
            required
            autoFocus
          />
        </Field>
        <div className="adm-form-row is-three">
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
          <Field
            label={t("admin.schedule.field.start")}
            hint={t("admin.schedule.field.startHint")}
          >
            <input
              type="time"
              value={draft.start}
              onChange={(e) => set({ start: e.target.value })}
            />
          </Field>
          <Field label={t("admin.schedule.field.end")}>
            <input
              type="time"
              value={draft.end}
              disabled={!draft.start}
              onChange={(e) => set({ end: e.target.value })}
            />
          </Field>
        </div>
        <div className="adm-form-row">
          <Field label={t("admin.schedule.field.type")}>
            <select
              value={draft.eventType}
              onChange={(e) => set({ eventType: e.target.value })}
            >
              {eventTypes.map((type) => (
                <option key={type} value={type}>
                  {t(`admin.event.type.${type}`)}
                </option>
              ))}
            </select>
          </Field>
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
        </div>
        {draft.start ? (
          <label className="adm-check">
            <input
              type="checkbox"
              checked={draft.approximate}
              onChange={(e) => set({ approximate: e.target.checked })}
            />
            {t("admin.schedule.field.approximate")}
          </label>
        ) : null}
        <Field label={t("admin.schedule.field.url")}>
          <input
            type="url"
            value={draft.url}
            onChange={(e) => set({ url: e.target.value })}
            maxLength={2000}
            placeholder="https://"
          />
        </Field>
        <Field label={t("admin.schedule.field.detail")}>
          <textarea
            value={draft.detail}
            onChange={(e) => set({ detail: e.target.value })}
            maxLength={500}
            rows={3}
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
        <div className="adm-form-actions">
          <Button onClick={() => onClose()}>{t("admin.action.cancel")}</Button>
          <Button type="submit" variant="primary" busy={busy}>
            {event?.deletedAt
              ? t("admin.schedule.restore")
              : t("admin.action.save")}
          </Button>
        </div>
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
  const [filters, setFilters] = useState({
    ...blankFilters,
    from: japanToday(),
  })
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
              placeholder={t("admin.schedule.search")}
              aria-label={t("admin.schedule.search")}
            />
          </label>
          <select
            value={filters.provenance}
            onChange={(e) => update({ provenance: e.target.value })}
            aria-label={t("admin.schedule.filter.provenance")}
          >
            <option value="">{t("admin.schedule.filter.anySource")}</option>
            <option value="automatic">{t("admin.event.automatic")}</option>
            <option value="manual">{t("admin.event.manual")}</option>
          </select>
          <select
            value={filters.cancellation}
            onChange={(e) => update({ cancellation: e.target.value })}
            aria-label={t("admin.schedule.filter.cancellation")}
          >
            <option value="">{t("admin.schedule.filter.anyState")}</option>
            <option value="none">{t("admin.schedule.cancel.none")}</option>
            <option value="llm_suspected">{t("admin.event.suspected")}</option>
            <option value="manual_confirmed">
              {t("admin.event.cancelled")}
            </option>
          </select>
          <select
            value={filters.merge}
            onChange={(e) => update({ merge: e.target.value })}
            aria-label={t("admin.schedule.filter.merge")}
          >
            <option value="">{t("admin.schedule.filter.anyMerge")}</option>
            <option value="visible">
              {t("admin.schedule.filter.visible")}
            </option>
            <option value="merged">{t("admin.schedule.filter.merged")}</option>
          </select>
          <input
            type="date"
            value={filters.from}
            onChange={(e) => update({ from: e.target.value })}
            aria-label={t("admin.schedule.filter.from")}
          />
          <input
            type="date"
            value={filters.to}
            onChange={(e) => update({ to: e.target.value })}
            aria-label={t("admin.schedule.filter.to")}
          />
          <label className="adm-check">
            <input
              type="checkbox"
              checked={filters.deleted}
              onChange={(e) => update({ deleted: e.target.checked })}
            />
            {t("admin.schedule.filter.deleted")}
          </label>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => {
              setSearch("")
              update(blankFilters)
            }}
          >
            {t("admin.schedule.filter.allDates")}
          </Button>
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
                      <small>{eventDetailText(event.detail)}</small>
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
