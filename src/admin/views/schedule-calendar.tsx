import { useEffect, useState } from "react"

import { useAppSettings } from "@/app-settings"
import {
  formatDay,
  formatTime,
  formatWeekday,
  japanToday,
} from "@/admin/format"
import type { AdminEvent } from "@/admin/types"
import { Loading } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { shiftDay } from "@/admin/views/event-parts"
import { DayHeading, EventRow } from "@/admin/views/schedule-parts"

/** Chips a day cell shows before "+N more". */
const chipLimit = 3

/** The first day of the month `months` away from the month of `day`. */
export function shiftMonth(day: string, months: number) {
  const [year, month] = day.split("-").map(Number)
  return new Date(Date.UTC(year, month - 1 + months, 1))
    .toISOString()
    .slice(0, 10)
}

/** The Monday on or before a Japan date; weeks start on Monday, as on the
 * board. */
function weekStart(day: string) {
  const weekday = new Date(`${day}T12:00:00+09:00`).getUTCDay()
  return shiftDay(day, -((weekday + 6) % 7))
}

/** Whole weeks covering the month that starts on `first`. */
function monthDays(first: string) {
  const start = weekStart(first)
  const end = shiftDay(weekStart(shiftDay(shiftMonth(first, 1), -1)), 6)
  const days: string[] = []
  for (let day = start; day <= end; day = shiftDay(day, 1)) days.push(day)
  return days
}

/** The review state a chip's style shows; the legend names each. */
function chipState(event: AdminEvent) {
  if (event.deletedAt) return "is-deleted"
  if (event.cancellationStatus === "manual_confirmed") return "is-cancelled"
  if (event.cancellationStatus === "llm_suspected") return "is-suspected"
  if (event.provenance === "automatic" && !event.manualLocked)
    return "is-unconfirmed"
  return "is-confirmed"
}

function Legend() {
  const { t } = useAppSettings()
  return (
    <ul className="adm-cal-legend">
      {(
        [
          ["is-unconfirmed", "admin.schedule.status.unconfirmed"],
          ["is-confirmed", "admin.schedule.legend.confirmed"],
          ["is-suspected", "admin.event.suspected"],
          ["is-cancelled", "admin.event.cancelled"],
        ] as const
      ).map(([state, key]) => (
        <li key={state}>
          <i className={`adm-cal-swatch ${state}`} aria-hidden />
          {t(key)}
        </li>
      ))}
    </ul>
  )
}

/**
 * A month of entries as a Monday-first grid. A chip opens its entry; a day
 * opens its entries below the grid, where they keep the list's actions and
 * a new entry can start on that day. On a narrow grid the chips become dots.
 */
export function ScheduleCalendar({
  month,
  filters,
  reloadKey,
  onEdit,
  onMerge,
  onCreate,
}: {
  /** The first day of the month shown. */
  month: string
  filters: Record<string, string>
  reloadKey: number
  onEdit: (event: AdminEvent) => void
  onMerge: (event: AdminEvent) => void
  onCreate: (day: string) => void
}) {
  const { t, locale } = useAppSettings()
  const { api } = useAdmin()
  const today = japanToday()
  const days = monthDays(month)
  const from = days[0]
  const to = days[days.length - 1]
  const filterKey = JSON.stringify(filters)
  const events = useResource(
    () =>
      api.events({
        ...filters,
        from,
        to,
        order: "asc",
        pageSize: 100,
      }),
    [from, to, filterKey, reloadKey],
  )
  // Today is open when it is in the month shown; another month opens closed.
  const [selected, setSelected] = useState<string | null>(null)
  useEffect(() => {
    setSelected(today.slice(0, 7) === month.slice(0, 7) ? today : null)
  }, [month, today])

  const data = events.data
  const byDay = new Map<string, AdminEvent[]>()
  for (const event of data?.items ?? []) {
    const list = byDay.get(event.startsOn) ?? []
    list.push(event)
    byDay.set(event.startsOn, list)
  }
  const inMonth = (day: string) => day.slice(0, 7) === month.slice(0, 7)
  const selectedEvents = selected ? (byDay.get(selected) ?? []) : []

  return (
    <div className="adm-cal-wrap">
      <div className="adm-cal" role="group" aria-busy={!data}>
        <div className="adm-cal-weekdays" aria-hidden>
          {days.slice(0, 7).map((day) => (
            <span key={day}>{formatWeekday(day, locale)}</span>
          ))}
        </div>
        <div className="adm-cal-grid">
          {days.map((day) => {
            const items = byDay.get(day) ?? []
            const more = items.length - chipLimit
            const label = `${formatDay(day, locale)} · ${t("admin.schedule.day.count", { count: items.length })}`
            return (
              <div
                key={day}
                className={`adm-cal-cell${inMonth(day) ? "" : " is-outside"}${day === today ? " is-today" : ""}${day === selected ? " is-selected" : ""}`}
                onClick={() => setSelected(day)}
              >
                <button
                  type="button"
                  className="adm-cal-date"
                  aria-pressed={day === selected}
                  aria-label={label}
                  onClick={(event) => {
                    event.stopPropagation()
                    setSelected(day)
                  }}
                >
                  {Number(day.slice(8))}
                </button>
                {items.length ? (
                  <ul className="adm-cal-chips">
                    {items.slice(0, chipLimit).map((item) => (
                      <li key={item.id}>
                        <button
                          type="button"
                          className={`adm-cal-chip ${chipState(item)}`}
                          title={item.title}
                          onClick={(event) => {
                            event.stopPropagation()
                            onEdit(item)
                          }}
                        >
                          {item.startsAt ? (
                            <span className="adm-cal-chip-time">
                              {item.timePrecision === "approximate" ? "~" : ""}
                              {formatTime(item.startsAt, locale)}
                            </span>
                          ) : null}
                          <span className="adm-cal-chip-title">
                            {item.title}
                          </span>
                        </button>
                      </li>
                    ))}
                    {more > 0 ? (
                      <li className="adm-cal-more">
                        {t("admin.schedule.calendar.more", { count: more })}
                      </li>
                    ) : null}
                  </ul>
                ) : null}
                {items.length ? (
                  <span className="adm-cal-dots" aria-hidden>
                    {items.slice(0, 4).map((item) => (
                      <i key={item.id} className={chipState(item)} />
                    ))}
                  </span>
                ) : null}
              </div>
            )
          })}
        </div>
      </div>
      <div className="adm-cal-foot">
        <Legend />
        {data && data.total > data.items.length ? (
          <span className="adm-muted">
            {t("admin.schedule.calendar.truncated", {
              count: data.items.length,
            })}
          </span>
        ) : null}
      </div>
      {!data ? (
        <Loading />
      ) : selected ? (
        <section className="adm-day adm-cal-agenda" aria-live="polite">
          <DayHeading
            day={selected}
            today={today}
            count={selectedEvents.length}
            onCreate={onCreate}
          />
          {selectedEvents.length ? (
            <ul className="adm-event-list">
              {selectedEvents.map((event) => (
                <EventRow
                  key={event.id}
                  event={event}
                  onEdit={onEdit}
                  onMerge={onMerge}
                  onChanged={() => void events.reload()}
                />
              ))}
            </ul>
          ) : (
            <p className="adm-muted adm-cal-empty">
              {t("admin.schedule.day.empty")}
            </p>
          )}
        </section>
      ) : null}
    </div>
  )
}
