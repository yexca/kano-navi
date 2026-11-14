import type {
  ArchiveSnapshot,
  Locale,
  ScheduleAsset,
  ScheduleEvent,
  Translator,
} from "@/types"
import type { ReactNode } from "react"
import { useEffect, useMemo, useRef, useState } from "react"
import {
  ArrowUpRight,
  BadgeCheck,
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  Radio,
  Sparkles,
  X,
} from "lucide-react"

import { EmptyNote, Placeholder, SectionTitle } from "@/site/site-shell"
import { ExternalLink, followCardLink } from "@/site/links"
import { eventDetailText } from "@/lib/event-text"
import { mediaSrcSet } from "@/lib/media"
import { useAppSettings } from "@/app-settings"
import { pickTranslation } from "@/components/translated-text"
import {
  addDays,
  cleanVideoTitle,
  dateKey,
  eventDateKey,
  eventIsManual,
  eventKinds,
  eventTagKey,
  eventAnnouncement,
  eventLink,
  eventLinks,
  eventShowsPlatformLinks,
  eventStatus,
  eventTags,
  formatJapanTime,
  formatLongDate,
  formatVisitorTime,
  formatWeekRange,
  japanToday,
  localDateKey,
  scheduleEntries,
  sortEvents,
  startOfWeek,
  visitorIsInJapan,
} from "../format"
import { archiveRange } from "../use-dashboard"
import { ArchiveMore, ArchiveState } from "./archive-dialog"
import { CalendarSubscribe } from "./calendar-subscribe"
import { EventTagIcon } from "./event-tag"
import { PlatformLinks } from "./platform-links"
import { SheetDialog } from "./sheet-dialog"
import { TranslatedTitle } from "./translated-title"

function keyToDate(key: string) {
  const [year, month, day] = key.split("-").map(Number)
  return new Date(year, month - 1, day)
}

/**
 * Calendar entries of an inclusive date range, in schedule order, keyed by
 * Japan date. A sale period shows at its opening and its close.
 */
function groupByDay(
  events: ScheduleEvent[],
  range: { from: string; to: string },
) {
  const grouped = new Map<string, ScheduleEvent[]>()
  for (const event of sortEvents(events.flatMap(scheduleEntries))) {
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

// Members-only entries take YouTube's members green, as the video chips do.
const typeTones: Record<string, string> = {
  stream: "berry",
  member: "leaf",
  event: "sky",
  release: "honey",
  video: "lavender",
  sale: "lavender",
}

const statusTones: Record<string, string> = {
  live: "berry",
  open: "leaf",
  cancelled: "danger",
  cancellation_review: "honey",
}

/** What an entry's card and its sheet both show. */
function entryFacts(
  event: ScheduleEvent,
  now: number,
  locale: Locale,
  t: Translator,
) {
  const status = eventStatus(event, now)
  const { typeKey, tag, work } = eventKinds(event)
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
  return {
    status,
    typeKey,
    tag,
    work,
    tone: typeTones[String(event.eventType || "").toLowerCase()] || "sky",
    time: event.startsAt ? formatJapanTime(event.startsAt) : null,
    manual: eventIsManual(event),
    note: cancellationNote?.filter(Boolean).join(" · ") || null,
    saleMark: event.saleMark || null,
    link: eventLink(event),
    ...entryLinks(event, locale, now),
  }
}

/**
 * An entry's platform links and, when they show, its own announcement link
 * beside them.
 */
function entryLinks(event: ScheduleEvent, locale: Locale, now: number) {
  const links = eventLinks(event, locale, now)
  const shown = eventShowsPlatformLinks(event, links)
  return {
    links: shown ? links : [],
    announcement: shown ? eventAnnouncement(event, links) : null,
  }
}

type EntryFacts = ReturnType<typeof entryFacts>

/**
 * Auto-detected or Confirmed. Cards show the icon alone, named by the legend
 * under the schedule and by the entry's sheet.
 */
function Provenance({
  manual,
  labelled = false,
  t,
}: {
  manual: boolean
  labelled?: boolean
  t: Translator
}) {
  const label = t(manual ? "schedule.sourceManual" : "schedule.sourceAutomatic")
  const Icon = manual ? BadgeCheck : Sparkles
  return (
    <span
      className={`guide-provenance${manual ? " is-manual" : ""}`}
      title={labelled ? undefined : label}
    >
      <Icon aria-hidden="true" />
      {labelled ? label : <span className="sr-only">{label}</span>}
    </span>
  )
}

/**
 * Statuses an entry names in words. Upcoming is the default and an ended
 * entry says so by its faded, folded card, so neither gets a chip.
 */
const quietStatuses = ["upcoming", "done"]

/** The entry's type, activity, and any status worth a chip. */
function EntryTags({
  facts,
  t,
  children,
}: {
  facts: EntryFacts
  t: Translator
  children?: ReactNode
}) {
  const { typeKey, tag, work, status, saleMark } = facts
  const showStatus = !quietStatuses.includes(status)
  if (!typeKey && !tag && !showStatus && !children) return null
  return (
    <span className="guide-event-tags">
      {typeKey ? <span className="tag">{t(typeKey)}</span> : null}
      {saleMark ? (
        <span className="tag">{t(`schedule.sale.${saleMark}`)}</span>
      ) : null}
      {tag ? (
        <span className="tag guide-event-kind">
          <EventTagIcon tag={tag} work={work} />
          {t(eventTagKey(tag, work))}
        </span>
      ) : null}
      {showStatus ? (
        <span className={`tag tone-${statusTones[status] || "muted"}`}>
          {status === "live" ? <Radio aria-hidden="true" /> : null}
          {t(`schedule.status.${status}`)}
        </span>
      ) : null}
      {children}
    </span>
  )
}

/**
 * One entry in full: its whole title, Japan time (and the visitor's when
 * they are elsewhere), labels, provenance in words, and its link.
 */
function EntrySheet({
  event,
  facts,
  locale,
  onClose,
  t,
}: {
  event: ScheduleEvent
  facts: EntryFacts
  locale: Locale
  onClose: () => void
  t: Translator
}) {
  return (
    <SheetDialog label={t("schedule.entryDetails")} onClose={onClose}>
      <article className={`entry-sheet tone-${facts.tone} is-${facts.status}`}>
        <header className="entry-sheet-head">
          <p className="kicker">
            {formatLongDate(keyToDate(eventDateKey(event)), locale)}
          </p>
          <button
            type="button"
            className="round-button sheet-dialog-close"
            onClick={onClose}
            aria-label={t("common.close")}
            title={t("common.close")}
          >
            <X aria-hidden="true" />
          </button>
        </header>
        <p className="entry-sheet-when">
          <strong>
            {facts.time
              ? `${facts.time} ${t("common.timezoneShort")}`
              : t("common.pendingConfirmation")}
          </strong>
          {event.startsAt && !visitorIsInJapan() ? (
            <small>
              {t("spotlight.localTime", {
                time: formatVisitorTime(event.startsAt, locale),
              })}
            </small>
          ) : null}
        </p>
        <h3 className="entry-sheet-title">
          <TranslatedTitle
            className="entry-sheet-title-text"
            title={cleanVideoTitle(event.title)}
            translations={event.translations}
            sourceLanguage={event.sourceLanguage}
            t={t}
          />
        </h3>
        {event.detail ? (
          <p className="entry-sheet-detail">
            {eventDetailText(event.detail, t)}
          </p>
        ) : null}
        <EntryTags facts={facts} t={t}>
          <Provenance manual={facts.manual} labelled t={t} />
        </EntryTags>
        {facts.note ? <p className="guide-event-note">{facts.note}</p> : null}
        {facts.links.length ? (
          <PlatformLinks
            links={facts.links}
            title={cleanVideoTitle(event.title)}
            variant="pill"
            t={t}
          />
        ) : null}
        {(facts.links.length ? facts.announcement : facts.link) ? (
          <ExternalLink
            className="pill-button entry-sheet-open"
            href={(facts.links.length ? facts.announcement : facts.link)!}
          >
            {t("schedule.openLink")}
            <ArrowUpRight aria-hidden="true" />
          </ExternalLink>
        ) : null}
      </article>
    </SheetDialog>
  )
}

/**
 * Scrolls `element` into view the way `block: "nearest"` does, moving only
 * its nearest scrolling ancestor. Safari's own scrollIntoView also scrolls
 * the overflow-hidden ancestors around a dialog body when the element is
 * taller than the body, pushing the dialog's header off screen.
 */
function revealNearest(element: HTMLElement) {
  const view = element.ownerDocument.defaultView
  let scroller = element.parentElement
  while (view && scroller && scroller !== element.ownerDocument.body) {
    const { overflowY } = view.getComputedStyle(scroller)
    if (
      (overflowY === "auto" || overflowY === "scroll") &&
      scroller.scrollHeight > scroller.clientHeight
    )
      break
    scroller = scroller.parentElement
  }
  if (!view || !scroller || scroller === element.ownerDocument.body) {
    element.scrollIntoView?.({ block: "nearest", behavior: "smooth" })
    return
  }
  const rect = element.getBoundingClientRect()
  const top = scroller.getBoundingClientRect().top + scroller.clientTop
  const bottom = top + scroller.clientHeight
  const above = rect.top < top
  const below = rect.bottom > bottom
  if (above === below) return
  // An edge outside the view lines up with its side, unless the element is
  // taller than the view, when its start does.
  const taller = rect.height > scroller.clientHeight
  const delta = above !== taller ? rect.top - top : rect.bottom - bottom
  scroller.scrollBy?.({ top: delta, behavior: "smooth" })
}

/**
 * An entry's card. The card opens the entry's sheet; the arrow in its
 * bottom corner follows the entry's link directly.
 * The month's day details show it `full`, with the whole title, and mark
 * the entry picked in the grid. The week shows a past entry `folded`: its
 * activity icon, time and provenance over two lines of title, with the
 * rest in its sheet.
 */
function GuideEvent({
  event,
  full = false,
  folded = false,
  pickTurn = 0,
  locale,
  now,
  t,
}: {
  event: ScheduleEvent
  full?: boolean
  folded?: boolean
  /** The grid pick that marked this entry, 0 when none; each pick scrolls. */
  pickTurn?: number
  locale: Locale
  now: number
  t: Translator
}) {
  const [open, setOpen] = useState(false)
  const itemRef = useRef<HTMLLIElement>(null)
  const highlighted = pickTurn > 0
  useEffect(() => {
    if (pickTurn > 0 && itemRef.current) revealNearest(itemRef.current)
  }, [pickTurn])
  const facts = entryFacts(event, now, locale, t)

  return (
    <li ref={itemRef}>
      <div
        className={`guide-event link-card tone-${facts.tone} is-${facts.status}${full ? " is-full" : ""}${folded ? " is-folded" : ""}${highlighted ? " is-highlighted" : ""}`}
        onClick={followCardLink}
      >
        <span className="guide-event-time">
          {folded && facts.tag ? (
            <EventTagIcon tag={facts.tag} work={facts.work} />
          ) : null}
          {facts.time ? (
            <>
              <strong>{facts.time}</strong>
              <small>{t("common.timezoneShort")}</small>
            </>
          ) : (
            <small>{t("common.pendingConfirmation")}</small>
          )}
          <Provenance manual={facts.manual} t={t} />
        </span>
        <TranslatedTitle
          className="guide-event-title"
          title={cleanVideoTitle(event.title)}
          translations={event.translations}
          sourceLanguage={event.sourceLanguage}
          onOpen={() => setOpen(true)}
          t={t}
        />
        {folded ? null : (
          <>
            {event.detail ? (
              <span className="guide-event-detail">
                {eventDetailText(event.detail, t)}
              </span>
            ) : null}
            <EntryTags facts={facts} t={t}>
              {facts.links.length ? (
                <PlatformLinks
                  links={facts.links}
                  title={cleanVideoTitle(event.title)}
                  variant="chip"
                  t={t}
                />
              ) : null}
              {(facts.links.length ? facts.announcement : facts.link) ? (
                <ExternalLink
                  className="guide-event-open"
                  href={(facts.links.length ? facts.announcement : facts.link)!}
                  aria-label={t("schedule.openLink")}
                  title={t("schedule.openLink")}
                >
                  <ArrowUpRight aria-hidden="true" />
                </ExternalLink>
              ) : null}
            </EntryTags>
            {facts.note ? (
              <span className="guide-event-note">{facts.note}</span>
            ) : null}
          </>
        )}
      </div>
      {open ? (
        <EntrySheet
          event={event}
          facts={facts}
          locale={locale}
          onClose={() => setOpen(false)}
          t={t}
        />
      ) : null}
    </li>
  )
}

/**
 * Whether the week folds an entry: everything on a day before today, and
 * today's entries once they end, except one still on air.
 */
function entryIsPast(event: ScheduleEvent, dayIsPast: boolean, now: number) {
  const status = eventStatus(event, now)
  return status !== "live" && (dayIsPast || status === "done")
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
                    folded={entryIsPast(event, isPast, now)}
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
 * three compact entries (dots on phones), whose titles take more lines the
 * fewer a day has; its cell selects that day, and an entry selects its day
 * and marks itself in the day details.
 */
function MonthGrid({
  monthStart,
  byDay,
  dayCounts,
  todayKey,
  selectedKey,
  loading,
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
  /** The month's entries are on their way: its days shimmer meanwhile. */
  loading: boolean
  onPickDay: (day: Date, eventId?: string) => void
  locale: Locale
  now: number
  t: Translator
}) {
  const { contentLocale } = useAppSettings()
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
    <div
      className={`month${loading ? " is-loading" : ""}`}
      aria-busy={loading || undefined}
    >
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
                // Entries pick the day themselves; the rest of the cell picks
                // it as its date button does for keyboards.
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
                <ol
                  className="month-events"
                  data-count={Math.min(items.length, MONTH_CHIPS)}
                >
                  {items.slice(0, MONTH_CHIPS).map((event) => {
                    const status = eventStatus(event, now)
                    const { tag, work } = eventKinds(event)
                    const tone =
                      typeTones[String(event.eventType || "").toLowerCase()] ||
                      "sky"
                    const time = event.startsAt
                      ? formatJapanTime(event.startsAt)
                      : null
                    const title = cleanVideoTitle(
                      pickTranslation(
                        event.translations,
                        contentLocale,
                        event.sourceLanguage,
                      )?.text || event.title,
                    )
                    const label = [
                      `${time || t("common.pendingConfirmation")} ${title}`,
                      event.saleMark
                        ? t(`schedule.sale.${event.saleMark}`)
                        : null,
                      tag ? t(eventTagKey(tag, work)) : null,
                      quietStatuses.includes(status)
                        ? null
                        : t(`schedule.status.${status}`),
                    ]
                      .filter(Boolean)
                      .join(" · ")
                    return (
                      <li key={event.id}>
                        <button
                          type="button"
                          className={`month-event tone-${tone} is-${status}`}
                          onClick={() => onPickDay(day, event.id)}
                          title={label}
                        >
                          <span className="sr-only">{label}</span>
                          <span aria-hidden="true">
                            {tag || time ? (
                              <span className="month-event-lead">
                                {tag ? (
                                  <EventTagIcon tag={tag} work={work} />
                                ) : null}
                                {time ? <strong>{time}</strong> : null}
                              </span>
                            ) : null}{" "}
                            {title}
                          </span>
                        </button>
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

/**
 * What the schedule's marks mean: the provenance icons, then each activity
 * label's icon, which month-grid entries show alone.
 */
function ScheduleLegend({ t }: { t: Translator }) {
  return (
    <>
      <Provenance manual={false} labelled t={t} />
      <Provenance manual labelled t={t} />
      <span className="guide-legend-tags">
        {eventTags.map((tag) => (
          <span key={tag} className="guide-legend-tag">
            <EventTagIcon tag={tag} />
            {t(`event.tag.${tag}`)}
          </span>
        ))}
      </span>
    </>
  )
}

/**
 * The month dialog's body: the month grid and, beside it on wide screens
 * (below it on narrow ones), an empty month's note and the selected day's full
 * entries. Remount it per month so the selection starts over.
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
  // The entry picked in the grid, with a turn that grows on every pick, so
  // picking the same entry again scrolls to it again.
  const [picked, setPicked] = useState<{ id: string; turn: number } | null>(
    null,
  )
  const detailRef = useRef<HTMLDivElement>(null)
  const selected = selectedKey ? keyToDate(selectedKey) : null
  const selectedItems = (selectedKey && byDay.get(selectedKey)) || []
  const selectedCount = selectedKey
    ? (dayCounts[selectedKey] ?? selectedItems.length)
    : 0

  const pickDay = (day: Date, eventId?: string) => {
    setSelectedKey(localDateKey(day))
    setPicked((last) =>
      eventId ? { id: eventId, turn: (last?.turn ?? 0) + 1 } : null,
    )
    // On narrow screens the details sit below the grid; a picked entry
    // scrolls itself into view.
    if (eventId) return
    window.requestAnimationFrame?.(() => {
      if (detailRef.current) revealNearest(detailRef.current)
    })
  }

  // Until the month arrives the grid stands with its dates and no entries,
  // so a month change never collapses the layout. A failed read shows its
  // error instead of outlines until a retry starts.
  const loading = !archive && (isLoading || !error)
  const unavailable = !archive && !loading
  const empty = Boolean(archive && !archive.total)

  return (
    <>
      <div className="month-layout">
        <MonthGrid
          monthStart={monthStart}
          byDay={byDay}
          dayCounts={dayCounts}
          todayKey={todayKey}
          selectedKey={selectedKey}
          loading={loading && isLoading}
          onPickDay={pickDay}
          locale={locale}
          now={now}
          t={t}
        />
        <div className="month-side">
          <ArchiveState
            error={error}
            onRetry={onRetry}
            loading={false}
            empty={empty}
            emptyTitle={t("schedule.emptyMonth")}
            hint={t("schedule.emptyHint")}
            adjacent={archive?.adjacent}
            onJump={onJump}
            locale={locale}
            t={t}
          />
          {!selected && !empty ? (
            <div className="month-detail is-unpicked" aria-live="polite">
              <p className="month-detail-empty">{t("schedule.pickDay")}</p>
            </div>
          ) : null}
          {selected ? (
            <div className="month-detail" ref={detailRef}>
              {/* Only the day and its count are announced, never an entry
                  sheet opened inside the list. */}
              <div
                className="month-detail-head"
                aria-live="polite"
                aria-atomic="true"
              >
                <h3>{formatLongDate(selected, locale)}</h3>
                {loading || unavailable ? null : (
                  <span>
                    {t("schedule.weekCount", { count: selectedCount })}
                  </span>
                )}
              </div>
              {loading ? (
                <Placeholder lines={2} />
              ) : unavailable ? null : selectedItems.length ? (
                <ol className="guide-day-events">
                  {selectedItems.map((event) => (
                    <GuideEvent
                      key={event.id}
                      event={event}
                      full
                      pickTurn={event.id === picked?.id ? picked.turn : 0}
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
      </div>
      <div className="guide-foot">
        <p className="guide-legend">
          <ScheduleLegend t={t} />
        </p>
        {archive ? (
          <ArchiveMore
            hasNext={archive.hasNext}
            isLoading={isLoading}
            label={t("schedule.moreInMonth")}
            onLoadMore={onLoadMore}
            t={t}
          />
        ) : null}
      </div>
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
              <ScheduleLegend t={t} />
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

          {/* A week without a schedule image shows no image strip. */}
          {scheduleImage ? (
            <div className="guide-image">
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
            </div>
          ) : null}
        </div>
      )}
    </section>
  )
}
