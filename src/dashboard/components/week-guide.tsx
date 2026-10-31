import type {
  ArchiveSnapshot,
  Locale,
  ScheduleAsset,
  ScheduleEvent,
  Translator,
} from "@/types"
import { useMemo, useRef, useState } from "react"
import {
  ArrowUpRight,
  BadgeCheck,
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  ImageIcon,
  PanelBottomClose,
  PanelRightClose,
  Radio,
  Sparkles,
} from "lucide-react"

import { EmptyNote, Placeholder, SectionTitle } from "@/site/site-shell"
import { ExternalLink, followCardLink } from "@/site/links"
import { eventDetailText } from "@/lib/event-text"
import { mediaSrcSet } from "@/lib/media"
import { pickTranslation } from "@/components/translated-text"
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
} from "../format"
import { archiveRange } from "../use-dashboard"
import { ArchiveMore, ArchiveState } from "./archive-dialog"
import { CalendarSubscribe } from "./calendar-subscribe"
import { TranslatedTitle } from "./translated-title"

function keyToDate(key: string) {
  const [year, month, day] = key.split("-").map(Number)
  return new Date(year, month - 1, day)
}

/** Events of an inclusive date range, in schedule order, keyed by Japan date. */
function groupByDay(
  events: ScheduleEvent[],
  range: { from: string; to: string },
) {
  const grouped = new Map<string, ScheduleEvent[]>()
  for (const event of sortEvents(events)) {
    const key = eventDateKey(event)
    if (key < range.from || key > range.to) continue
    let day = grouped.get(key)
    if (!day) {
      day = []
      grouped.set(key, day)
    }
    day.push(event)
  }
  return grouped
}

const countEvents = (byDay: Map<string, ScheduleEvent[]>) =>
  [...byDay.values()].reduce((count, items) => count + items.length, 0)

const typeTones: Record<string, string> = {
  stream: "berry",
  member: "lavender",
  event: "sky",
  release: "honey",
  video: "leaf",
}

const statusTones: Record<string, string> = {
  live: "berry",
  cancelled: "danger",
  cancellation_review: "honey",
}

function GuideEvent({
  event,
  locale,
  now,
  t,
}: {
  event: ScheduleEvent
  locale: Locale
  now: number
  t: Translator
}) {
  const status = eventStatus(event, now)
  const typeKey = eventTypeKey(event)
  const tone = typeTones[String(event.eventType || "").toLowerCase()] || "sky"
  const time = event.startsAt ? formatJapanTime(event.startsAt) : null
  const manual = eventIsManual(event)
  const cancellationNote =
    event.cancellationStatus === "llm_suspected"
      ? [
          t("schedule.cancellationReview"),
          event.cancellationReason,
          event.cancellationEvidence,
        ]
      : event.cancellationStatus === "manual_confirmed"
        ? [
            event.cancellationReason || t("schedule.cancellationManual"),
            event.cancellationEvidence,
          ]
        : null

  return (
    <li>
      <div
        className={`guide-event tone-${tone} is-${status}${event.url ? " link-card" : ""}`}
        onClick={event.url ? followCardLink : undefined}
      >
        <span className="guide-event-time">
          {time ? (
            <>
              <strong>{time}</strong>
              <small>{t("common.timezoneShort")}</small>
            </>
          ) : (
            <small>{t("common.pendingConfirmation")}</small>
          )}
          {event.url ? <ArrowUpRight aria-hidden="true" /> : null}
        </span>
        <TranslatedTitle
          className="guide-event-title"
          title={cleanVideoTitle(event.title)}
          translations={event.translations}
          href={event.url}
          locale={locale}
          t={t}
        />
        {event.detail ? (
          <span className="guide-event-detail">
            {eventDetailText(event.detail, t)}
          </span>
        ) : null}
        <span className="guide-event-tags">
          {typeKey ? <span className="tag">{t(typeKey)}</span> : null}
          {status !== "upcoming" ? (
            <span className={`tag tone-${statusTones[status] || "muted"}`}>
              {status === "live" ? <Radio aria-hidden="true" /> : null}
              {t(`schedule.status.${status}`)}
            </span>
          ) : null}
          <span
            className={`guide-provenance${manual ? " is-manual" : ""}`}
            title={t(
              manual ? "schedule.sourceManual" : "schedule.sourceAutomatic",
            )}
          >
            {manual ? (
              <BadgeCheck aria-hidden="true" />
            ) : (
              <Sparkles aria-hidden="true" />
            )}
            {t(manual ? "schedule.sourceManual" : "schedule.sourceAutomatic")}
          </span>
        </span>
        {cancellationNote ? (
          <span className="guide-event-note">
            {cancellationNote.filter(Boolean).join(" · ")}
          </span>
        ) : null}
      </div>
    </li>
  )
}

function WeekDays({
  weekStart,
  byDay,
  dayCounts,
  todayKey,
  locale,
  now,
  t,
}: {
  weekStart: Date
  byDay: Map<string, ScheduleEvent[]>
  dayCounts: Record<string, number>
  todayKey: string
  locale: Locale
  now: number
  t: Translator
}) {
  const days = Array.from({ length: 7 }, (_, index) =>
    addDays(weekStart, index),
  )
  return (
    <ol className="guide-days">
      {days.map((day, index) => {
        const key = localDateKey(day)
        const items = byDay.get(key) || []
        const count = dayCounts[key] ?? items.length
        const isToday = key === todayKey
        const isPast = key < todayKey
        return (
          <li
            key={key}
            className={`guide-day${isToday ? " is-today" : ""}${isPast ? " is-past" : ""}${count ? "" : " is-empty"}${index >= 5 ? " is-weekend" : ""}`}
            aria-current={isToday ? "date" : undefined}
          >
            <div className="guide-day-head">
              <span className="sr-only">
                {t("schedule.dayEvents", {
                  date: formatLongDate(day, locale),
                  count,
                })}
              </span>
              <span className="guide-day-name" aria-hidden="true">
                {t("schedule.weekdays")[index]}
              </span>
              <span className="guide-day-number" aria-hidden="true">
                {day.getDate()}
              </span>
              {isToday ? (
                <span className="guide-day-today">{t("schedule.today")}</span>
              ) : null}
            </div>
            {items.length ? (
              <ol className="guide-day-events">
                {items.map((event) => (
                  <GuideEvent
                    key={event.id}
                    event={event}
                    locale={locale}
                    now={now}
                    t={t}
                  />
                ))}
              </ol>
            ) : (
              <span className="guide-day-none" aria-hidden="true">
                —
              </span>
            )}
            {count > items.length ? (
              <span className="guide-day-more">
                {t("schedule.moreOnDay", { count: count - items.length })}
              </span>
            ) : null}
          </li>
        )
      })}
    </ol>
  )
}

const MONTH_CHIPS = 3

/**
 * Month overview: Monday-first weeks covering the month. Each day shows up to
 * three compact entries (dots on phones); its cell selects that day.
 */
function MonthGrid({
  monthStart,
  byDay,
  dayCounts,
  todayKey,
  selectedKey,
  onPickDay,
  locale,
  now,
  t,
}: {
  monthStart: Date
  byDay: Map<string, ScheduleEvent[]>
  dayCounts: Record<string, number>
  todayKey: string
  selectedKey: string | null
  onPickDay: (day: Date) => void
  locale: Locale
  now: number
  t: Translator
}) {
  const month = monthStart.getMonth()
  const first = startOfWeek(monthStart)
  const last = new Date(monthStart.getFullYear(), month + 1, 0)
  const length =
    Math.round(
      (addDays(startOfWeek(last), 7).getTime() - first.getTime()) / 86400000,
    ) || 35
  const days = Array.from({ length }, (_, index) => addDays(first, index))
  const weekdays = t("schedule.weekdays") as unknown as string[]

  return (
    <div className="month">
      <div className="month-weekdays" aria-hidden="true">
        {weekdays.map((name, index) => (
          <span key={index} className={index >= 5 ? "is-weekend" : undefined}>
            {name}
          </span>
        ))}
      </div>
      <ol className="month-days">
        {days.map((day, index) => {
          const key = localDateKey(day)
          const items = byDay.get(key) || []
          const count = dayCounts[key] ?? items.length
          const isToday = key === todayKey
          const outside = day.getMonth() !== month
          const weekday = index % 7
          return (
            <li
              key={key}
              className={`month-day${isToday ? " is-today" : ""}${key < todayKey ? " is-past" : ""}${outside ? " is-outside" : ""}${count ? " has-events" : ""}${key === selectedKey ? " is-selected" : ""}${weekday === 5 ? " is-saturday" : ""}${weekday === 6 ? " is-sunday" : ""}`}
              aria-current={isToday ? "date" : undefined}
              onClick={(event) => {
                // Entry links keep their own target; the rest of the cell
                // picks the day, as its date button does for keyboards.
                if (!(event.target as Element).closest("a, button"))
                  onPickDay(day)
              }}
            >
              <button
                type="button"
                className="month-day-number"
                onClick={() => onPickDay(day)}
                aria-pressed={key === selectedKey}
                aria-label={t("schedule.dayEvents", {
                  date: formatLongDate(day, locale),
                  count,
                })}
              >
                {day.getDate()}
              </button>
              {items.length ? (
                <ol className="month-events">
                  {items.slice(0, MONTH_CHIPS).map((event) => {
                    const status = eventStatus(event, now)
                    const tone =
                      typeTones[String(event.eventType || "").toLowerCase()] ||
                      "sky"
                    const time = event.startsAt
                      ? formatJapanTime(event.startsAt)
                      : null
                    const title = cleanVideoTitle(
                      pickTranslation(event.translations, locale)?.text ||
                        event.title,
                    )
                    const label = `${time || t("common.pendingConfirmation")} ${title} · ${t(`schedule.status.${status}`)}`
                    const Wrapper = event.url ? ExternalLink : "span"
                    return (
                      <li key={event.id}>
                        <Wrapper
                          className={`month-event tone-${tone} is-${status}`}
                          href={event.url || undefined}
                          title={label}
                        >
                          <span className="sr-only">{label}</span>
                          <span aria-hidden="true">
                            {time ? <strong>{time}</strong> : null}
                            {title}
                          </span>
                        </Wrapper>
                      </li>
                    )
                  })}
                </ol>
              ) : null}
              {count > Math.min(items.length, MONTH_CHIPS) ? (
                <span className="month-more">
                  {t("schedule.moreOnDay", {
                    count: count - Math.min(items.length, MONTH_CHIPS),
                  })}
                </span>
              ) : null}
            </li>
          )
        })}
      </ol>
    </div>
  )
}

function ProvenanceLegend({ t }: { t: Translator }) {
  return (
    <>
      <span className="guide-provenance">
        <Sparkles aria-hidden="true" />
        {t("schedule.sourceAutomatic")}
      </span>
      <span className="guide-provenance is-manual">
        <BadgeCheck aria-hidden="true" />
        {t("schedule.sourceManual")}
      </span>
    </>
  )
}

/**
 * The month dialog's body: the month grid and, beside it on wide screens
 * (below it on narrow ones), the selected day's full entries until they are
 * hidden. Remount it per month so the selection starts over.
 */
export function ScheduleMonth({
  monthStart,
  archive,
  isLoading,
  error,
  onRetry,
  onLoadMore,
  onJump,
  onOpenWeek,
  locale,
  now,
  t,
}: {
  monthStart: Date
  archive: ArchiveSnapshot<ScheduleEvent> | null
  isLoading: boolean
  error: Error | null
  onRetry: () => unknown
  onLoadMore: () => unknown
  onJump: (month: Date) => void
  onOpenWeek: (day: Date) => void
  locale: Locale
  now: number
  t: Translator
}) {
  const todayKey = dateKey(new Date(now))
  const range = archiveRange("schedule", monthStart)
  const items = archive?.items
  const byDay = useMemo(
    () => groupByDay(items || [], range),
    [items, range.from, range.to],
  )
  const dayCounts = archive?.dayCounts || {}
  const monthPrefix = localDateKey(monthStart).slice(0, 8)
  const [selectedKey, setSelectedKey] = useState<string | null>(() =>
    todayKey.startsWith(monthPrefix) ? todayKey : null,
  )
  const detailRef = useRef<HTMLDivElement>(null)
  const selected = selectedKey ? keyToDate(selectedKey) : null
  const selectedItems = (selectedKey && byDay.get(selectedKey)) || []
  const selectedCount = selectedKey
    ? (dayCounts[selectedKey] ?? selectedItems.length)
    : 0

  const pickDay = (day: Date) => {
    setSelectedKey(localDateKey(day))
    // On narrow screens the details sit below the grid.
    window.requestAnimationFrame?.(() =>
      detailRef.current?.scrollIntoView?.({
        block: "nearest",
        behavior: "smooth",
      }),
    )
  }

  return (
    <>
      <ArchiveState
        error={error}
        onRetry={onRetry}
        loading={!archive && isLoading}
        empty={Boolean(archive && !archive.total)}
        emptyTitle={t("schedule.emptyMonth")}
        hint={t("schedule.emptyHint")}
        adjacent={archive?.adjacent}
        onJump={onJump}
        locale={locale}
        t={t}
      />
      {archive ? (
        <div className={`month-layout${selected ? "" : " is-collapsed"}`}>
          <MonthGrid
            monthStart={monthStart}
            byDay={byDay}
            dayCounts={dayCounts}
            todayKey={todayKey}
            selectedKey={selectedKey}
            onPickDay={pickDay}
            locale={locale}
            now={now}
            t={t}
          />
          {selected ? (
            <div className="month-detail" ref={detailRef} aria-live="polite">
              <div className="month-detail-head">
                <h3>{formatLongDate(selected, locale)}</h3>
                <span>{t("schedule.weekCount", { count: selectedCount })}</span>
                <button
                  type="button"
                  className="round-button"
                  onClick={() => setSelectedKey(null)}
                  aria-label={t("schedule.hideDay")}
                  title={t("schedule.hideDay")}
                >
                  <PanelRightClose
                    className="month-detail-wide"
                    aria-hidden="true"
                  />
                  <PanelBottomClose
                    className="month-detail-narrow"
                    aria-hidden="true"
                  />
                </button>
              </div>
              {selectedItems.length ? (
                <ol className="guide-day-events">
                  {selectedItems.map((event) => (
                    <GuideEvent
                      key={event.id}
                      event={event}
                      locale={locale}
                      now={now}
                      t={t}
                    />
                  ))}
                </ol>
              ) : (
                <p className="month-detail-empty">
                  {selectedCount
                    ? t("schedule.moreOnDay", { count: selectedCount })
                    : t("schedule.dayEmpty")}
                </p>
              )}
              {selectedItems.length && selectedCount > selectedItems.length ? (
                <span className="guide-day-more">
                  {t("schedule.moreOnDay", {
                    count: selectedCount - selectedItems.length,
                  })}
                </span>
              ) : null}
              <button
                type="button"
                className="pill-button"
                onClick={() => onOpenWeek(selected)}
              >
                {t("schedule.openWeek")}
                <ChevronRight aria-hidden="true" />
              </button>
            </div>
          ) : null}
        </div>
      ) : null}
      {archive ? (
        <div className="guide-foot">
          <p className="guide-legend">
            <ProvenanceLegend t={t} />
          </p>
          <ArchiveMore
            hasNext={archive.hasNext}
            isLoading={isLoading}
            label={t("schedule.moreInMonth")}
            onLoadMore={onLoadMore}
            t={t}
          />
        </div>
      ) : null}
    </>
  )
}

/**
 * The schedule guide: one Monday-first week (seven day columns on wide
 * screens, a day list on narrow ones), with the month in a dialog. Every time
 * is Japan time; date-only entries never get a synthetic start.
 */
export function WeekGuide({
  events,
  scheduleImages,
  weekStart,
  setWeekStart,
  range,
  shownRange,
  adjacent,
  error,
  onRetry,
  total,
  hasNext,
  onLoadMore,
  dayCounts = {},
  isLoading,
  onOpenImage,
  onOpenMonth,
  locale,
  now,
  t,
}: {
  events: ScheduleEvent[]
  scheduleImages: ScheduleAsset[]
  weekStart: Date
  setWeekStart: (value: Date | ((value: Date) => Date)) => void
  range: { from: string; to: string }
  /** The week the data belongs to; null until the first snapshot. */
  shownRange: { from: string; to: string } | null
  adjacent: { previous: string | null; next: string | null }
  error: Error | null
  onRetry: () => unknown
  total: number
  hasNext: boolean
  onLoadMore: () => unknown
  dayCounts: Record<string, number>
  isLoading: boolean
  onOpenImage: (asset: ScheduleAsset) => void
  onOpenMonth: () => void
  locale: Locale
  now: number
  t: Translator
}) {
  const todayKey = dateKey(new Date(now))
  // Until the selected week's snapshot arrives, the last loaded week stays
  // rendered, dimmed, so a week change swaps the layout once.
  const dataRange = shownRange || range
  const stale = shownRange != null && shownRange.from !== range.from
  const shownWeek = keyToDate(dataRange.from)
  const byDay = useMemo(
    () => groupByDay(events, dataRange),
    [events, dataRange.from, dataRange.to],
  )
  const loaded = countEvents(byDay)
  const today = japanToday()
  const isCurrent = localDateKey(weekStart) === localDateKey(startOfWeek(today))
  const adjacentKey = adjacent.next || adjacent.previous
  const jumpTarget =
    !loaded && !total && adjacentKey ? keyToDate(adjacentKey) : null
  const weekStartId = dataRange.from
  const weekEndId = dataRange.to
  const scheduleImage = scheduleImages.find((asset) =>
    asset.url && asset.periodStart && asset.periodEnd
      ? asset.periodStart <= weekEndId && asset.periodEnd >= weekStartId
      : asset.url && asset.weekStart === weekStartId,
  )
  const label = formatWeekRange(weekStart, locale)

  return (
    <section
      className="section guide"
      id="schedule"
      aria-labelledby="guide-title"
    >
      <SectionTitle id="guide-title" kicker={label} title={t("schedule.title")}>
        <CalendarSubscribe t={t} />
        <button
          type="button"
          className="pill-button"
          onClick={onOpenMonth}
          aria-haspopup="dialog"
        >
          <CalendarDays aria-hidden="true" />
          {t("schedule.openMonth")}
        </button>
        <div className="guide-nav" role="group" aria-label={label}>
          <button
            type="button"
            className="round-button"
            onClick={() => setWeekStart((current) => addDays(current, -7))}
            aria-label={t("schedule.previousWeek")}
            title={t("schedule.previousWeek")}
          >
            <ChevronLeft aria-hidden="true" />
          </button>
          <button
            type="button"
            className="pill-button"
            disabled={isCurrent}
            onClick={() => setWeekStart(startOfWeek(today))}
          >
            {t("schedule.thisWeek")}
          </button>
          <button
            type="button"
            className="round-button"
            onClick={() => setWeekStart((current) => addDays(current, 7))}
            aria-label={t("schedule.nextWeek")}
            title={t("schedule.nextWeek")}
          >
            <ChevronRight aria-hidden="true" />
          </button>
        </div>
      </SectionTitle>

      {error ? (
        <div className="inline-alert" role="alert">
          <span>{t("error.api")}</span>
          <button type="button" className="pill-button" onClick={onRetry}>
            {t("common.retry")}
          </button>
        </div>
      ) : null}

      {!shownRange ? (
        isLoading ? (
          <Placeholder lines={4} />
        ) : null
      ) : (
        <div
          className={`guide-body${stale ? " is-stale" : ""}`}
          aria-busy={isLoading || undefined}
          inert={stale}
        >
          {!loaded && !total ? (
            <EmptyNote
              title={t("schedule.emptyWeek")}
              hint={t("schedule.emptyHint")}
            >
              {jumpTarget ? (
                <button
                  type="button"
                  className="pill-button"
                  onClick={() => setWeekStart(startOfWeek(jumpTarget))}
                >
                  {t("schedule.jumpTo", {
                    range: formatWeekRange(startOfWeek(jumpTarget), locale),
                  })}
                  <ChevronRight aria-hidden="true" />
                </button>
              ) : null}
            </EmptyNote>
          ) : (
            <WeekDays
              weekStart={shownWeek}
              byDay={byDay}
              dayCounts={dayCounts}
              todayKey={todayKey}
              locale={locale}
              now={now}
              t={t}
            />
          )}

          <div className="guide-foot">
            <p className="guide-legend">
              <ProvenanceLegend t={t} />
              <span>{t("schedule.weekCount", { count: total })}</span>
            </p>
            {hasNext ? (
              <button
                type="button"
                className="pill-button"
                onClick={onLoadMore}
              >
                {t("schedule.moreEvents")}
              </button>
            ) : null}
          </div>

          {/* The thumbnail slot keeps its size with or without an image. */}
          <div className="guide-image">
            {scheduleImage ? (
              <>
                <button
                  type="button"
                  className="guide-image-thumb media-opener"
                  onClick={() => onOpenImage(scheduleImage)}
                  aria-label={t("schedule.imageView")}
                >
                  <img
                    src={scheduleImage.url ?? undefined}
                    srcSet={mediaSrcSet(scheduleImage.url)}
                    sizes="240px"
                    alt={scheduleImage.alt || t("schedule.imageAlt")}
                    loading="lazy"
                  />
                </button>
                <div className="guide-image-copy">
                  <p className="kicker">{t("schedule.imageTitle")}</p>
                  <p>{t("schedule.imageNote")}</p>
                  <div className="guide-image-actions">
                    <button
                      type="button"
                      className="pill-button"
                      onClick={() => onOpenImage(scheduleImage)}
                    >
                      {t("schedule.imageView")}
                    </button>
                    {scheduleImage.sourceUrl ? (
                      <ExternalLink
                        className="text-link"
                        href={scheduleImage.sourceUrl}
                      >
                        {t("schedule.imageOpen")}
                        <ArrowUpRight aria-hidden="true" />
                      </ExternalLink>
                    ) : null}
                  </div>
                </div>
              </>
            ) : (
              <>
                <span className="guide-image-thumb is-empty" aria-hidden="true">
                  <ImageIcon />
                </span>
                <div className="guide-image-copy">
                  <p className="kicker">{t("schedule.imageTitle")}</p>
                  <p>{t("schedule.imageEmpty")}</p>
                </div>
              </>
            )}
          </div>
        </div>
      )}
    </section>
  )
}
