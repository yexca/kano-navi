import { useMemo, useState } from "react"
import {
  ArrowUpRight,
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  ImageIcon,
  Undo2,
} from "lucide-react"

import {
  addDays,
  cleanVideoTitle,
  dateKey,
  eventDateKey,
  eventIsManual,
  eventStatus,
  eventTypeKey,
  formatJapanTime,
  formatLongDate,
  formatWeekRange,
  japanToday,
  localDateKey,
  sortEvents,
  startOfWeek,
  weekStartKey,
} from "../format"
import {
  EmptyState,
  ExternalLink,
  SectionHeading,
  Skeleton,
} from "./primitives"

function keyToDate(key) {
  const [year, month, day] = key.split("-").map(Number)
  return new Date(year, month - 1, day)
}

function EventRow({ event, locale, now, t, showDay }) {
  const status = eventStatus(event, now)
  const typeKey = eventTypeKey(event)
  const Container = event.url ? ExternalLink : "div"
  const time = event.startsAt ? formatJapanTime(event.startsAt) : null
  const day = keyToDate(eventDateKey(event))

  return (
    <li>
      <Container
        className={`event-row is-${status}`}
        href={event.url || undefined}
      >
        <span className="event-time">
          {showDay ? (
            <span className="event-day">{formatLongDate(day, locale)}</span>
          ) : null}
          <strong>{time || "--:--"}</strong>
          {time ? <small>{t("common.timezoneShort")}</small> : null}
        </span>
        <span className="event-rail" aria-hidden="true" />
        <span className="event-body">
          <span className="event-title">{cleanVideoTitle(event.title)}</span>
          <span className="event-meta">
            {typeKey ? <span className="chip">{t(typeKey)}</span> : null}
            <span>{event.detail || t("schedule.publicEvent")}</span>
            {!time ? <span>{t("common.pendingConfirmation")}</span> : null}
          </span>
        </span>
        <span className="event-flags">
          <span className={`status-badge is-${status}`}>
            {t(`schedule.status.${status}`)}
          </span>
          <span
            className={`provenance ${eventIsManual(event) ? "is-manual" : "is-auto"}`}
          >
            {t(
              eventIsManual(event)
                ? "schedule.sourceManual"
                : "schedule.sourceAutomatic",
            )}
          </span>
        </span>
        {event.url ? (
          <ArrowUpRight className="event-arrow" aria-hidden="true" />
        ) : null}
      </Container>
    </li>
  )
}

export function SchedulePanel({
  events,
  scheduleImages,
  isLoading,
  locale,
  now,
  t,
}) {
  const [weekStart, setWeekStart] = useState(() => startOfWeek(japanToday()))
  const [focusDay, setFocusDay] = useState(null)
  const todayKey = dateKey(new Date(now))

  const days = useMemo(
    () => Array.from({ length: 7 }, (_, index) => addDays(weekStart, index)),
    [weekStart],
  )
  const weekKeys = useMemo(() => new Set(days.map(localDateKey)), [days])
  const eventsByDay = useMemo(() => {
    const grouped = new Map()
    for (const event of events) {
      const key = eventDateKey(event)
      if (!grouped.has(key)) grouped.set(key, [])
      grouped.get(key).push(event)
    }
    return grouped
  }, [events])

  const weekEvents = useMemo(
    () =>
      sortEvents(events.filter((event) => weekKeys.has(eventDateKey(event)))),
    [events, weekKeys],
  )
  const visibleEvents = focusDay
    ? weekEvents.filter((event) => eventDateKey(event) === focusDay)
    : weekEvents

  // When the visible week is empty, offer the closest week that has events:
  // the next upcoming one, otherwise the most recent past one.
  const jumpTarget = useMemo(() => {
    if (weekEvents.length || !events.length) return null
    const currentKey = localDateKey(weekStart)
    const keys = [...new Set(events.map(eventDateKey).filter(Boolean))].sort()
    const next = keys.find((key) => key >= currentKey)
    const target = next || keys[keys.length - 1]
    return target ? startOfWeek(keyToDate(target)) : null
  }, [events, weekEvents.length, weekStart])

  const weekStartId = localDateKey(weekStart)
  const scheduleImage = scheduleImages.find(
    (asset) =>
      asset.url &&
      (asset.weekStart || weekStartKey(asset.updatedAt)) === weekStartId,
  )
  const range = formatWeekRange(weekStart, locale)

  const moveWeek = (offset) => {
    setFocusDay(null)
    setWeekStart((current) => addDays(current, offset * 7))
  }

  return (
    <section
      className="panel schedule-panel"
      id="schedule"
      aria-labelledby="schedule-title"
    >
      <SectionHeading
        id="schedule-title"
        eyebrow={range}
        title={t("schedule.title")}
        subtitle={t("schedule.note")}
      >
        <div className="week-controls">
          <button
            type="button"
            className="round-button"
            onClick={() => moveWeek(-1)}
            aria-label={t("schedule.previousWeek")}
            title={t("schedule.previousWeek")}
          >
            <ChevronLeft aria-hidden="true" />
          </button>
          <button
            type="button"
            className="text-button"
            onClick={() => {
              setFocusDay(null)
              setWeekStart(startOfWeek(japanToday()))
            }}
          >
            {t("schedule.thisWeek")}
          </button>
          <button
            type="button"
            className="round-button"
            onClick={() => moveWeek(1)}
            aria-label={t("schedule.nextWeek")}
            title={t("schedule.nextWeek")}
          >
            <ChevronRight aria-hidden="true" />
          </button>
        </div>
      </SectionHeading>

      <div
        className="week-strip"
        role="group"
        aria-label={t("schedule.weekLabel", { range })}
      >
        {days.map((date, index) => {
          const key = localDateKey(date)
          const count = eventsByDay.get(key)?.length || 0
          const isToday = key === todayKey
          const isFocused = key === focusDay
          return (
            <button
              key={key}
              type="button"
              className={`week-day${isToday ? " is-today" : ""}${isFocused ? " is-focused" : ""}${count ? " has-events" : ""}${index >= 5 ? " is-weekend" : ""}`}
              aria-pressed={isFocused}
              aria-current={isToday ? "date" : undefined}
              aria-label={t("schedule.dayEvents", {
                date: formatLongDate(date, locale),
                count,
              })}
              onClick={() => setFocusDay(isFocused ? null : key)}
            >
              <span className="week-day-name">
                {t("schedule.weekdays")[index]}
              </span>
              <span className="week-day-number">{date.getDate()}</span>
              <span className="week-day-dots" aria-hidden="true">
                {Array.from({ length: Math.min(count, 3) }, (_, dot) => (
                  <i key={dot} />
                ))}
              </span>
            </button>
          )
        })}
      </div>

      <div className="schedule-body">
        <div className="agenda">
          <div className="agenda-toolbar">
            <span className="agenda-count">
              <CalendarDays aria-hidden="true" />
              {focusDay
                ? formatLongDate(keyToDate(focusDay), locale)
                : t("schedule.weekCount", { count: weekEvents.length })}
            </span>
            {focusDay ? (
              <button
                type="button"
                className="text-button"
                onClick={() => setFocusDay(null)}
              >
                <Undo2 aria-hidden="true" />
                {t("schedule.showWeek")}
              </button>
            ) : null}
          </div>

          {isLoading ? (
            <Skeleton lines={4} />
          ) : visibleEvents.length ? (
            <ol className="event-list">
              {visibleEvents.map((event) => (
                <EventRow
                  key={event.id}
                  event={event}
                  locale={locale}
                  now={now}
                  t={t}
                  showDay={!focusDay}
                />
              ))}
            </ol>
          ) : (
            <EmptyState
              icon={CalendarDays}
              title={t(focusDay ? "schedule.emptyDay" : "schedule.emptyWeek")}
              hint={t("schedule.emptyHint")}
            >
              {jumpTarget && !focusDay ? (
                <button
                  type="button"
                  className="soft-button"
                  onClick={() => setWeekStart(jumpTarget)}
                >
                  {t("schedule.jumpTo", {
                    range: formatWeekRange(jumpTarget, locale),
                  })}
                  <ChevronRight aria-hidden="true" />
                </button>
              ) : null}
            </EmptyState>
          )}
        </div>

        <aside className="schedule-image" aria-label={t("schedule.imageTitle")}>
          {scheduleImage ? (
            <ExternalLink
              className="schedule-image-link"
              href={scheduleImage.sourceUrl || scheduleImage.url}
            >
              <img
                src={scheduleImage.url}
                alt={scheduleImage.alt || t("schedule.imageAlt")}
                loading="lazy"
              />
              <span className="schedule-image-caption">
                <strong>{t("schedule.imageTitle")}</strong>
                <span>
                  {t("schedule.imageOpen")}
                  <ArrowUpRight aria-hidden="true" />
                </span>
              </span>
            </ExternalLink>
          ) : (
            <div className="schedule-image-empty">
              <ImageIcon aria-hidden="true" />
              <span>{t("schedule.imageEmpty")}</span>
            </div>
          )}
        </aside>
      </div>
    </section>
  )
}
