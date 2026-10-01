import { useState } from "react"
import {
  CalendarClock,
  Check,
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  Pencil,
  Plus,
  RotateCcw,
  Save,
  Search,
  Trash2,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import {
  adminMessage,
  formatAdminError,
  jsonBody,
  renderAdminMessage,
  request,
} from "@/admin/api"
import { japanParts } from "@/admin/format"
import {
  Btn,
  Card,
  Dialog,
  EmptyState,
  Field,
  IconBtn,
  PageHeader,
  Tag,
} from "@/admin/components/primitives"
import { cn } from "@/lib/utils"

const emptyEvent = {
  title: "",
  detail: "",
  startsOn: "",
  startTime: "",
  endTime: "",
  timePrecision: "exact",
  status: "",
  eventType: "event",
  url: "",
}

function eventToForm(event) {
  const start = japanParts(event.startsAt)
  const end = japanParts(event.endsAt)
  return {
    title: event.title || "",
    detail: event.detail || "",
    startsOn: event.startsOn || "",
    startTime: start ? `${start.hour}:${start.minute}` : "",
    endTime: end ? `${end.hour}:${end.minute}` : "",
    timePrecision: event.timePrecision || (start ? "exact" : "unknown"),
    status: event.status || "",
    eventType: event.eventType || "event",
    url: event.url || "",
  }
}

function eventPayload(form) {
  const startsAt = form.startTime
    ? new Date(`${form.startsOn}T${form.startTime}:00+09:00`)
    : null
  let endsAt = form.endTime
    ? new Date(`${form.startsOn}T${form.endTime}:00+09:00`)
    : null
  if (startsAt && endsAt && endsAt < startsAt) {
    endsAt = new Date(endsAt.getTime() + 24 * 60 * 60 * 1000)
  }
  return {
    ...form,
    startsAt: startsAt?.toISOString() || null,
    endsAt: endsAt?.toISOString() || null,
    timePrecision: startsAt ? form.timePrecision : "unknown",
  }
}

function formatEventTime(event, t) {
  if (!event.startsAt) return t("admin.event.timeUnknown")
  const parts = japanParts(event.startsAt)
  return parts
    ? `${parts.hour}:${parts.minute} ${t("common.timezoneShort")}`
    : t("admin.event.timeUnknown")
}

function eventStatus(event) {
  if (event.deletedAt) return "deleted"
  const explicit = String(event.statusCode || "").toLowerCase()
  const status = String(event.status || "")
    .trim()
    .toLowerCase()
  const normalized = explicit || status
  if (
    ["cancelled", "canceled", "cancel", "取消", "中止", "キャンセル"].includes(
      normalized,
    ) ||
    normalized.startsWith("cancel")
  )
    return "cancelled"
  if (
    [
      "pending",
      "tentative",
      "unknown",
      "待确认",
      "待补充",
      "確認待ち",
      "未定",
    ].includes(normalized) ||
    event.timePrecision === "unknown"
  )
    return "pending"
  if (
    ["upcoming", "scheduled", "已预约", "配信予定", "予約"].includes(
      normalized,
    ) ||
    event.isUpcoming
  )
    return "upcoming"
  if (
    ["completed", "success", "done", "已完成", "記録済み"].includes(normalized)
  )
    return "recorded"
  return "unknown"
}

const statusTones = {
  deleted: "danger",
  cancelled: "danger",
  pending: "honey",
  upcoming: "sky",
  recorded: "leaf",
  unknown: "neutral",
}

function EventEditor({ event, onClose, onSaved, t }) {
  const [form, setForm] = useState(() =>
    event ? eventToForm(event) : { ...emptyEvent },
  )
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(null)
  const editing = Boolean(event)
  const setField = (name) => (inputEvent) =>
    setForm((current) => ({ ...current, [name]: inputEvent.target.value }))

  const submit = async (submitEvent) => {
    submitEvent.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const result = await request(
        editing ? `/events/${encodeURIComponent(event.id)}` : "/events",
        {
          method: editing ? "PUT" : "POST",
          body: jsonBody(eventPayload(form)),
        },
      )
      onSaved(result.event)
    } catch (saveError) {
      setError(formatAdminError(saveError))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      title={t(editing ? "admin.event.editTitle" : "admin.event.newTitle")}
      description={t("admin.event.lockNote")}
      onClose={onClose}
      closeLabel={t("admin.action.close")}
      size="lg"
    >
      <form className="adm-form adm-form-3" onSubmit={submit}>
        <Field label={t("admin.event.title")} wide>
          <input
            value={form.title}
            onChange={setField("title")}
            maxLength={240}
            required
            data-autofocus
          />
        </Field>
        <Field label={t("admin.event.detail")} wide>
          <textarea
            value={form.detail}
            onChange={setField("detail")}
            maxLength={500}
            rows={3}
          />
        </Field>
        <Field label={t("admin.event.date")}>
          <input
            type="date"
            value={form.startsOn}
            onChange={setField("startsOn")}
            required
          />
        </Field>
        <Field label={t("admin.event.startTime")}>
          <input
            type="time"
            value={form.startTime}
            onChange={setField("startTime")}
          />
        </Field>
        <Field label={t("admin.event.endTime")}>
          <input
            type="time"
            value={form.endTime}
            onChange={setField("endTime")}
          />
        </Field>
        <Field label={t("admin.event.timePrecision")}>
          <select
            value={form.startTime ? form.timePrecision : "unknown"}
            onChange={setField("timePrecision")}
            disabled={!form.startTime}
          >
            <option value="exact">{t("admin.event.precision.exact")}</option>
            <option value="approximate">
              {t("admin.event.precision.approximate")}
            </option>
            <option value="unknown">
              {t("admin.event.precision.unknown")}
            </option>
          </select>
        </Field>
        <Field label={t("admin.event.type")}>
          <select value={form.eventType} onChange={setField("eventType")}>
            {["event", "stream", "member", "release", "appearance"].map(
              (type) => (
                <option key={type} value={type}>
                  {t(`admin.event.type.${type}`)}
                </option>
              ),
            )}
          </select>
        </Field>
        <Field label={t("admin.event.status")}>
          <input
            value={form.status}
            onChange={setField("status")}
            maxLength={80}
          />
        </Field>
        <Field label={t("admin.event.publicUrl")} wide>
          <input
            type="url"
            value={form.url}
            onChange={setField("url")}
            maxLength={2000}
          />
        </Field>
        {error ? (
          <p className="adm-inline-error is-wide">
            {renderAdminMessage(error, t)}
          </p>
        ) : null}
        <div className="adm-form-actions is-wide">
          <Btn variant="outline" onClick={onClose}>
            {t("admin.action.cancel")}
          </Btn>
          <Btn type="submit" variant="primary" icon={Save} busy={busy}>
            {t("admin.event.saveAndConfirm")}
          </Btn>
        </div>
      </form>
    </Dialog>
  )
}

export function SchedulesView({ data }) {
  const { t } = useAppSettings()
  const {
    eventsPage,
    eventFilters,
    updateFilters,
    resetFilters,
    loadEventsPage,
    notify,
    fail,
  } = data
  const [editor, setEditor] = useState(null)
  const events = eventsPage?.items || eventsPage?.events || []
  const page = eventsPage?.page || 1

  const reload = () => loadEventsPage({ page })

  const confirm = async (event) => {
    try {
      await request(`/events/${encodeURIComponent(event.id)}/confirm`, {
        method: "POST",
      })
      await reload()
      notify(adminMessage("admin.notice.scheduleConfirmed"))
    } catch (error) {
      fail(error)
    }
  }

  const remove = async (event) => {
    if (
      !window.confirm(t("admin.schedule.deleteConfirm", { title: event.title }))
    )
      return
    try {
      await request(`/events/${encodeURIComponent(event.id)}`, {
        method: "DELETE",
      })
      await reload()
      notify(adminMessage("admin.notice.scheduleDeleted"))
    } catch (error) {
      fail(error)
    }
  }

  const applyFilters = async (event) => {
    event.preventDefault()
    try {
      await loadEventsPage({ page: 1 })
    } catch (error) {
      fail(error)
    }
  }

  const hasFilters =
    eventFilters.search ||
    eventFilters.provenance ||
    eventFilters.from ||
    eventFilters.to ||
    eventFilters.includeDeleted

  return (
    <section className="adm-view" aria-labelledby="adm-schedules-title">
      <PageHeader
        id="adm-schedules-title"
        title={t("admin.schedule.title")}
        description={t("admin.schedule.description")}
        actions={
          <Btn
            variant="primary"
            icon={Plus}
            onClick={() => setEditor({ event: null })}
          >
            {t("admin.schedule.new")}
          </Btn>
        }
      />
      <Card className="adm-filter-card">
        <form className="adm-filters" onSubmit={applyFilters}>
          <label className="adm-search">
            <Search aria-hidden="true" />
            <input
              value={eventFilters.search}
              onChange={(event) => updateFilters("search", event.target.value)}
              placeholder={t("admin.schedule.searchPlaceholder")}
              aria-label={t("admin.schedule.search")}
            />
          </label>
          <select
            value={eventFilters.provenance}
            onChange={(event) =>
              updateFilters("provenance", event.target.value)
            }
            aria-label={t("admin.schedule.source")}
          >
            <option value="">{t("admin.schedule.allSources")}</option>
            <option value="automatic">{t("admin.schedule.automatic")}</option>
            <option value="manual">{t("admin.schedule.manual")}</option>
          </select>
          <input
            type="date"
            value={eventFilters.from}
            onChange={(event) => updateFilters("from", event.target.value)}
            aria-label={t("admin.schedule.startDate")}
          />
          <input
            type="date"
            value={eventFilters.to}
            onChange={(event) => updateFilters("to", event.target.value)}
            aria-label={t("admin.schedule.endDate")}
          />
          <label className="adm-check-line">
            <input
              type="checkbox"
              checked={eventFilters.includeDeleted}
              onChange={(event) =>
                updateFilters("includeDeleted", event.target.checked)
              }
            />
            {t("admin.schedule.includeDeleted")}
          </label>
          <Btn type="submit" variant="outline" icon={Search}>
            {t("admin.schedule.filter")}
          </Btn>
          {hasFilters ? (
            <IconBtn
              label={t("admin.schedule.reset")}
              icon={RotateCcw}
              onClick={async () => {
                resetFilters()
                await loadEventsPage({
                  page: 1,
                  filters: {
                    search: "",
                    provenance: "",
                    from: "",
                    to: "",
                    includeDeleted: false,
                  },
                }).catch(fail)
              }}
            />
          ) : null}
        </form>
      </Card>

      <Card className="adm-table-card">
        <div className="adm-table-meta">
          <span>
            {t("admin.schedule.total", { count: eventsPage?.total ?? 0 })}
          </span>
          <span>
            {t("admin.schedule.page", {
              page,
              totalPages: eventsPage?.totalPages || 1,
            })}
          </span>
        </div>
        {events.length ? (
          <ul className="adm-event-list">
            {events.map((event) => {
              const status = eventStatus(event)
              return (
                <li
                  key={event.id}
                  className={cn(
                    "adm-event-row",
                    event.deletedAt && "is-deleted",
                  )}
                >
                  <div className="adm-event-date">
                    <strong>{event.startsOn}</strong>
                    <small>{formatEventTime(event, t)}</small>
                  </div>
                  <div className="adm-event-copy">
                    <strong>
                      {event.title}
                      {event.url ? (
                        <a
                          href={event.url}
                          target="_blank"
                          rel="noreferrer noopener"
                          aria-label={t("admin.schedule.openSource")}
                        >
                          <ExternalLink aria-hidden="true" />
                        </a>
                      ) : null}
                    </strong>
                    <small>
                      {event.detail || t("admin.schedule.noDetails")}
                    </small>
                  </div>
                  <div className="adm-event-tags">
                    <Tag
                      tone={event.provenance === "manual" ? "leaf" : "honey"}
                    >
                      {event.provenance === "manual"
                        ? t("admin.schedule.manualSource")
                        : t("admin.schedule.automaticSource")}
                    </Tag>
                    <Tag tone={statusTones[status]}>
                      {t(`admin.schedule.status.${status}`)}
                    </Tag>
                  </div>
                  <div className="adm-event-actions">
                    {event.provenance !== "manual" && !event.deletedAt ? (
                      <Btn
                        variant="outline"
                        size="sm"
                        icon={Check}
                        onClick={() => confirm(event)}
                      >
                        {t("admin.action.confirm")}
                      </Btn>
                    ) : null}
                    {!event.deletedAt ? (
                      <IconBtn
                        label={t("admin.schedule.editLabel", {
                          title: event.title,
                        })}
                        icon={Pencil}
                        onClick={() => setEditor({ event })}
                      />
                    ) : null}
                    <IconBtn
                      label={t("admin.schedule.deleteLabel", {
                        title: event.title,
                      })}
                      icon={Trash2}
                      onClick={() => remove(event)}
                    />
                  </div>
                </li>
              )
            })}
          </ul>
        ) : (
          <EmptyState
            icon={CalendarClock}
            title={t("admin.schedule.noRecords")}
          />
        )}
        <div className="adm-pagination">
          <IconBtn
            label={t("admin.schedule.pagePrevious")}
            icon={ChevronLeft}
            disabled={!eventsPage?.hasPrevious}
            onClick={() => loadEventsPage({ page: page - 1 }).catch(fail)}
          />
          <span>
            {t("admin.schedule.page", {
              page,
              totalPages: eventsPage?.totalPages || 1,
            })}
          </span>
          <IconBtn
            label={t("admin.schedule.pageNext")}
            icon={ChevronRight}
            disabled={!eventsPage?.hasNext}
            onClick={() => loadEventsPage({ page: page + 1 }).catch(fail)}
          />
        </div>
      </Card>
      {editor ? (
        <EventEditor
          event={editor.event}
          onClose={() => setEditor(null)}
          onSaved={async () => {
            setEditor(null)
            await reload()
            notify(adminMessage("admin.notice.scheduleSaved"))
          }}
          t={t}
        />
      ) : null}
    </section>
  )
}
