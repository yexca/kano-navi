import { useState } from "react"
import {
  CalendarPlus,
  CheckCheck,
  Combine,
  ExternalLink,
  Pencil,
  Split,
  Trash2,
  Undo2,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { eventStatusLabel, formatDay, formatTime } from "@/admin/format"
import type { AdminEvent } from "@/admin/types"
import { eventDetailText } from "@/lib/event-text"
import { Badge, Button, ConfirmButton, IconButton } from "@/admin/ui"
import { useAdmin } from "@/admin/use-admin-data"
import {
  CancellationBadge,
  MergeBadge,
  ProvenanceBadge,
  shiftDay,
} from "@/admin/views/event-parts"

// The schedule's entry rows and day headings, shared by its list and its
// calendar.

/** Consecutive entries on the same Japan date, in list order. */
export function groupByDay(events: AdminEvent[]) {
  const days: { day: string; events: AdminEvent[] }[] = []
  for (const event of events) {
    const last = days[days.length - 1]
    if (last?.day === event.startsOn) last.events.push(event)
    else days.push({ day: event.startsOn, events: [event] })
  }
  return days
}

/** The entry's time in Japan, or All day; date-only entries never gain one. */
export function EventTime({ event }: { event: AdminEvent }) {
  const { t, locale } = useAppSettings()
  if (!event.startsAt)
    return (
      <span className="adm-event-time is-all-day">
        <strong>{t("admin.schedule.allDay")}</strong>
      </span>
    )
  return (
    <span className="adm-event-time">
      <strong>
        {event.timePrecision === "approximate" ? "~" : ""}
        {formatTime(event.startsAt, locale)}
      </strong>
      <small>
        {event.endsAt ? `–${formatTime(event.endsAt, locale)} ` : ""}JST
      </small>
    </span>
  )
}

export function EventRow({
  event,
  onEdit,
  onMerge,
  onChanged,
}: {
  event: AdminEvent
  onEdit: (event: AdminEvent) => void
  onMerge: (event: AdminEvent) => void
  onChanged: () => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [confirming, setConfirming] = useState(false)
  const deleted = Boolean(event.deletedAt)
  return (
    <li
      className={`adm-event-row${deleted ? " is-deleted" : ""}${event.mergedInto ? " is-merged" : ""}`}
    >
      <EventTime event={event} />
      <div className="adm-event-main">
        <button
          type="button"
          className="adm-event-title"
          onClick={() => onEdit(event)}
          title={t("admin.schedule.editNamed", { title: event.title })}
        >
          {event.title}
        </button>
        {event.detail ? (
          <small>{eventDetailText(event.detail, t)}</small>
        ) : null}
        <span className="adm-chip-row">
          <Badge>{t(`admin.event.type.${event.eventType || "event"}`)}</Badge>
          <ProvenanceBadge event={event} />
          <CancellationBadge event={event} />
          <MergeBadge event={event} />
          {event.status ? (
            <Badge tone="sky">{eventStatusLabel(event.status, t)}</Badge>
          ) : null}
          {deleted ? (
            <Badge tone="danger">{t("admin.schedule.deleted")}</Badge>
          ) : null}
        </span>
      </div>
      <div className="adm-row-actions adm-event-actions">
        {deleted ? (
          <Button size="sm" icon={Undo2} onClick={() => onEdit(event)}>
            {t("admin.schedule.restore")}
          </Button>
        ) : !event.manualLocked ? (
          <Button
            size="sm"
            icon={CheckCheck}
            busy={confirming}
            title={t("admin.schedule.confirm")}
            onClick={async () => {
              setConfirming(true)
              const result = await act(
                () => api.confirmEvent(event.id),
                "admin.notice.eventConfirmed",
              )
              setConfirming(false)
              if (result) onChanged()
            }}
          >
            {t("admin.schedule.confirmShort")}
          </Button>
        ) : null}
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
        {deleted ? null : event.mergedInto ? (
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
                onChanged()
            }}
          />
        ) : (
          <IconButton
            icon={Combine}
            label={t("admin.schedule.merge.open")}
            onClick={() => onMerge(event)}
          />
        )}
        {deleted ? null : (
          <>
            <IconButton
              icon={Pencil}
              label={t("admin.schedule.edit")}
              onClick={() => onEdit(event)}
            />
            <ConfirmButton
              size="sm"
              variant="ghost"
              icon={Trash2}
              aria-label={t("admin.schedule.delete")}
              title={t("admin.schedule.delete")}
              onConfirm={async () => {
                // A delete answers null; a failure is undefined.
                const result = await act(
                  () => api.deleteEvent(event.id),
                  "admin.notice.eventDeleted",
                )
                if (result !== undefined) onChanged()
              }}
            />
          </>
        )}
      </div>
    </li>
  )
}

/** A day's heading: its date, how near it is, its count and a quick add. */
export function DayHeading({
  day,
  today,
  count,
  onCreate,
}: {
  day: string
  today: string
  count: number
  onCreate: (day: string) => void
}) {
  const { t, locale } = useAppSettings()
  const near =
    day === today
      ? "today"
      : day === shiftDay(today, 1)
        ? "tomorrow"
        : day === shiftDay(today, -1)
          ? "yesterday"
          : ""
  const label = formatDay(day, locale)
  return (
    <header className={`adm-day-head${day === today ? " is-today" : ""}`}>
      <h3>
        {label}
        {near ? <em>{t(`admin.schedule.day.${near}`)}</em> : null}
      </h3>
      <small>{t("admin.schedule.day.count", { count })}</small>
      <IconButton
        icon={CalendarPlus}
        label={t("admin.schedule.day.create", { day: label })}
        onClick={() => onCreate(day)}
      />
    </header>
  )
}
