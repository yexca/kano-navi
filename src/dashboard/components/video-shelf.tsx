import type {
  ArchiveSnapshot,
  Dashboard,
  Locale,
  Translator,
  Video,
} from "@/types"
import {
  ArrowUpRight,
  ChevronUp,
  Clock3,
  History,
  ImageOff,
  Play,
  Radio,
} from "lucide-react"
import {
  memo,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react"

import { EmptyNote, Placeholder, SectionTitle } from "@/site/site-shell"
import {
  BilibiliLogo,
  ExternalLink,
  followCardLink,
  YouTubeLogo,
} from "@/site/links"
import {
  cleanVideoTitle,
  formatDuration,
  formatJapanDay,
  formatJapanTime,
  formatMonth,
  formatRelative,
  formatStamp,
  japanDateParts,
  videoFormLabel,
  videoTime,
} from "../format"
import {
  groupVideos,
  splitVideos,
  videoIsGone,
  videoIsStream,
  videoLinks,
  videoMatchesFilter,
  videoMonthKey,
  videoPhase,
  videoPlatforms,
  VIDEO_CATEGORIES,
  type VideoCategory,
  type VideoFilter,
  type VideoLink,
  type VideoPhase,
} from "../video-groups"
import { mediaSrcSet } from "@/lib/media"
import { ArchiveMore, ArchiveState } from "./archive-dialog"
import { EventTagIcon, VideoAvailabilityChip, VideoTagChip } from "./event-tag"
import { PlatformBadge, PlatformLinks } from "./platform-links"
import { TranslatedHeading, TranslatedTitle } from "./translated-title"

// Rendered widths for srcset: the lead card spans most of the section, the
// grid and rail cards a column of it.
const LEAD_THUMB_SIZES = "(max-width: 960px) 100vw, 60vw"
const CARD_THUMB_SIZES =
  "(max-width: 640px) 80vw, (max-width: 1200px) 40vw, 360px"

function Thumb({
  url,
  sizes = CARD_THUMB_SIZES,
  badge = null,
  seconds,
  video,
  links,
  t,
}: {
  url: string | null
  sizes?: string
  badge?: "play" | "upcoming" | null
  seconds?: number | null
  video: Video
  /** The video's platform links; their platforms mark the cover. */
  links: VideoLink[]
  t: Translator
}) {
  const length = formatDuration(seconds)
  // A private or removed video without another copy has nothing to open.
  const closed = videoIsGone(video) && !links.length
  return (
    <span className={`video-thumb${closed ? " is-closed" : ""}`}>
      {url ? (
        <img
          src={url}
          srcSet={mediaSrcSet(url)}
          sizes={sizes}
          alt=""
          loading="lazy"
        />
      ) : (
        <span className="video-thumb-missing">
          <ImageOff aria-hidden="true" />
          <span>{t("media.unavailable")}</span>
        </span>
      )}
      <PlatformBadge
        platforms={videoPlatforms(video, links)}
        members={video.availability === "members"}
      />
      {badge ? (
        <span className="video-thumb-badge" aria-hidden="true">
          {badge === "upcoming" ? <Clock3 /> : <Play fill="currentColor" />}
        </span>
      ) : null}
      {/* Read out beside the date instead, after the title. */}
      {length ? (
        <span className="video-length" aria-hidden="true">
          {length}
        </span>
      ) : null}
    </span>
  )
}

/** The length for screen readers; the thumbnail shows it as a badge. */
function LengthNote({ video, t }: { video: Video; t: Translator }) {
  const length = formatDuration(video.durationSeconds)
  return length ? (
    <span className="sr-only">
      {t("videos.length")} {length}
    </span>
  ) : null
}

/** The Japan date, with the time when it matters, and the full stamp on hover. */
function JapanDate({
  video,
  withTime = false,
  locale,
  t,
}: {
  video: Video
  withTime?: boolean
  locale: Locale
  t: Translator
}) {
  const time = videoTime(video)
  if (!time) return null
  return (
    <time
      dateTime={time}
      title={`${formatStamp(time)} ${t("common.timezoneShort")}`}
    >
      {formatJapanDay(time, locale)}
      {withTime
        ? ` ${formatJapanTime(time)} ${t("common.timezoneShort")}`
        : null}
    </time>
  )
}

/** What a reservation is now; a past video shows its kind instead. */
function PhaseTag({
  phase,
  video,
  now,
  t,
}: {
  phase: VideoPhase
  video: Video
  now: number
  t: Translator
}) {
  // The week guide's tones: live is berry, cancelled danger, unsure honey.
  if (phase === "live")
    return (
      <span className="tag tone-berry is-live">
        <Radio aria-hidden="true" />
        {t("spotlight.onAir")}
      </span>
    )
  if (phase === "started")
    return <span className="tag tone-honey">{t("spotlight.started")}</span>
  if (phase === "cancelled")
    return <span className="tag tone-danger">{t("videos.cancelled")}</span>
  if (phase === "upcoming")
    return <span className="tag tone-sky">{t("videos.reserved")}</span>
  return <FormChip video={video} now={now} t={t} />
}

/** A video's form chip, or nothing for a form without one (a premiere). */
function FormChip({
  video,
  now,
  t,
}: {
  video: Video
  now: number
  t: Translator
}) {
  const label = videoFormLabel(video, t, now)
  return label ? <span className="tag">{label}</span> : null
}

function Subhead({ children, count }: { children: ReactNode; count?: number }) {
  return (
    <h3 className="videos-subhead">
      {children}
      {count ? <small>{count}</small> : null}
    </h3>
  )
}

/**
 * The shelf's large card: the operator's featured video with its focus copy,
 * or else the newest cover, original or premiere.
 */
function LeadCard({
  video,
  isFeatured,
  focus,
  locale,
  now,
  t,
}: {
  video: Video
  isFeatured: boolean
  focus: Dashboard["focus"]
  locale: Locale
  now: number
  t: Translator
}) {
  const usesFocus = isFeatured && focus?.videoId === video.id
  const title = cleanVideoTitle((usesFocus && focus.title) || video.title)
  const thumbnail = (usesFocus && focus.imageUrl) || video.thumbnailUrl
  const phase = videoPhase(video, now)
  const length = formatDuration(video.durationSeconds)
  const links = videoLinks(video, locale, now)
  return (
    <article className="video-lead">
      <ExternalLink
        className="video-lead-art"
        href={links[0]?.url}
        aria-label={`${t("videos.watch")}: ${title}`}
      >
        <Thumb
          url={thumbnail}
          sizes={LEAD_THUMB_SIZES}
          badge={
            phase === "upcoming" ? "upcoming" : links.length ? "play" : null
          }
          video={video}
          links={links}
          t={t}
        />
      </ExternalLink>
      <div className="video-lead-copy">
        <p className="video-meta">
          <span className="tag tone-berry">
            {isFeatured ? t("videos.featured") : t("videos.latestWork")}
          </span>
          {/* A past work's form is its shelf's; a featured stream or Short
              keeps it. */}
          {phase !== "past" ? (
            <PhaseTag phase={phase} video={video} now={now} t={t} />
          ) : videoMatchesFilter(video, "works") ? null : (
            <FormChip video={video} now={now} t={t} />
          )}
          <VideoTagChip video={video} t={t} />
          <VideoAvailabilityChip video={video} t={t} />
          {usesFocus && focus.dateLabel ? (
            <span>{focus.dateLabel}</span>
          ) : (
            <JapanDate video={video} withTime locale={locale} t={t} />
          )}
          {length ? (
            <span className="video-meta-length">
              <span className="sr-only">{t("videos.length")} </span>
              {length}
            </span>
          ) : null}
        </p>
        {usesFocus && focus.title ? (
          <h3>{title}</h3>
        ) : (
          <TranslatedHeading title={title} video={video} t={t} />
        )}
        {usesFocus && focus.description ? <p>{focus.description}</p> : null}
        <div className="video-lead-actions">
          <PlatformLinks links={links} title={title} variant="pill" t={t} />
          {usesFocus && focus.sourceUrl ? (
            <ExternalLink className="text-link" href={focus.sourceUrl}>
              {t("videos.officialSource")}
              <ArrowUpRight aria-hidden="true" />
            </ExternalLink>
          ) : null}
        </div>
      </div>
    </article>
  )
}

/** A compact date ticket for a reservation or a broadcast on air. */
function VideoTicket({
  video,
  locale,
  now,
  t,
}: {
  video: Video
  locale: Locale
  now: number
  t: Translator
}) {
  const phase = videoPhase(video, now)
  const time = videoTime(video)
  const parts = japanDateParts(time, locale)
  const title = cleanVideoTitle(video.title)
  const links = videoLinks(video, locale, now)
  return (
    <li>
      <div
        className={`video-ticket link-card is-${phase}`}
        onClick={followCardLink}
      >
        <span className="video-ticket-date" aria-hidden="true">
          {parts ? (
            <>
              <small>{parts.month}</small>
              <strong>{parts.day}</strong>
              <small>{parts.weekday}</small>
            </>
          ) : (
            <Clock3 />
          )}
        </span>
        <span className="video-ticket-copy">
          <span className="video-meta">
            <PhaseTag phase={phase} video={video} now={now} t={t} />
            {videoIsStream(video) ? null : (
              <FormChip video={video} now={now} t={t} />
            )}
            <VideoTagChip video={video} t={t} />
            <VideoAvailabilityChip video={video} t={t} />
            {time ? (
              <time dateTime={time}>
                <span className="sr-only">{formatJapanDay(time, locale)} </span>
                {formatJapanTime(time)} {t("common.timezoneShort")}
              </time>
            ) : null}
            {links.length > 1 ? (
              <PlatformLinks links={links} title={title} variant="chip" t={t} />
            ) : null}
          </span>
          <TranslatedTitle
            className="video-ticket-title"
            title={title}
            translations={video.translations}
            sourceLanguage={video.sourceLanguage}
            href={links[0]?.url}
            t={t}
          />
        </span>
      </div>
    </li>
  )
}

/**
 * A grid card: large artwork, a two-line title, the date and platforms. Its
 * shelf names its form, so the card carries no form chip.
 */
function VideoCard({
  video,
  locale,
  now,
  t,
}: {
  video: Video
  locale: Locale
  now: number
  t: Translator
}) {
  const title = cleanVideoTitle(video.title)
  const links = videoLinks(video, locale, now)
  return (
    <li className="video-card">
      <div className="video-card-link link-card" onClick={followCardLink}>
        <Thumb
          url={video.thumbnailUrl}
          seconds={video.durationSeconds}
          video={video}
          links={links}
          t={t}
        />
        <TranslatedTitle
          className="video-card-title"
          title={title}
          translations={video.translations}
          sourceLanguage={video.sourceLanguage}
          href={links[0]?.url}
          t={t}
        />
      </div>
      <p className="video-meta">
        <JapanDate video={video} locale={locale} t={t} />
        <VideoTagChip video={video} t={t} />
        <VideoAvailabilityChip video={video} t={t} />
        <LengthNote video={video} t={t} />
        {links.length > 1 ? (
          <PlatformLinks links={links} title={title} variant="chip" t={t} />
        ) : null}
      </p>
    </li>
  )
}

function VideoWhen({
  video,
  absoluteTime = false,
  locale,
  now,
  t,
}: {
  video: Video
  absoluteTime?: boolean
  locale: Locale
  now: number
  t: Translator
}) {
  const time = videoTime(video)
  if (!time) return null
  const phase = videoPhase(video, now)
  return (
    <time
      dateTime={time}
      title={`${formatStamp(time)} ${t("common.timezoneShort")}`}
    >
      {phase === "upcoming" || absoluteTime
        ? `${formatJapanDay(time, locale)} ${formatJapanTime(time)} ${t("common.timezoneShort")}`
        : formatRelative(time, locale, now)}
    </time>
  )
}

/** A thumbnail row, for earlier works beside the lead. */
function VideoRow({
  video,
  absoluteTime = false,
  locale,
  now,
  t,
}: {
  video: Video
  /** Archives show the Japan date and time instead of "3 days ago". */
  absoluteTime?: boolean
  locale: Locale
  now: number
  t: Translator
}) {
  const title = cleanVideoTitle(video.title)
  const phase = videoPhase(video, now)
  const links = videoLinks(video, locale, now)
  return (
    <li>
      <div
        className={`video-row link-card${phase === "upcoming" ? " is-upcoming" : ""}`}
        onClick={followCardLink}
      >
        <Thumb
          url={video.thumbnailUrl}
          badge={
            phase === "upcoming" ? "upcoming" : links.length ? "play" : null
          }
          seconds={video.durationSeconds}
          video={video}
          links={links}
          t={t}
        />
        <span className="video-row-copy">
          <span className="video-meta">
            {phase === "upcoming" ? (
              <span className="tag tone-berry">{t("videos.reserved")}</span>
            ) : null}
            <VideoTagChip video={video} t={t} />
            <VideoAvailabilityChip video={video} t={t} />
            <VideoWhen
              video={video}
              absoluteTime={absoluteTime}
              locale={locale}
              now={now}
              t={t}
            />
            <LengthNote video={video} t={t} />
            {links.length > 1 ? (
              <PlatformLinks links={links} title={title} variant="chip" t={t} />
            ) : null}
          </span>
          <TranslatedTitle
            className="video-row-title"
            title={title}
            translations={video.translations}
            sourceLanguage={video.sourceLanguage}
            href={links[0]?.url}
            t={t}
          />
        </span>
      </div>
    </li>
  )
}

function TicketList({
  videos,
  locale,
  now,
  t,
}: {
  videos: Video[]
  locale: Locale
  now: number
  t: Translator
}) {
  return (
    <ol className="video-tickets">
      {videos.map((video) => (
        <VideoTicket
          key={video.id}
          video={video}
          locale={locale}
          now={now}
          t={t}
        />
      ))}
    </ol>
  )
}

function VideoGrid({
  videos,
  variant,
  locale,
  now,
  t,
}: {
  videos: Video[]
  /**
   * Shorts get 9:16 cards; a Shorts row keeps to one line of them. A preview
   * is the All view's sample of a kind, which phones cut to two rows.
   */
  variant?: "shorts" | "shorts-row" | "preview"
  locale: Locale
  now: number
  t: Translator
}) {
  const isShorts = variant === "shorts" || variant === "shorts-row"
  return (
    <ol
      className={`video-grid${isShorts ? " is-shorts" : ""}${variant === "shorts-row" ? " is-row" : ""}${variant === "preview" ? " is-preview" : ""}`}
    >
      {videos.map((video) => (
        <VideoCard
          key={video.id}
          video={video}
          locale={locale}
          now={now}
          t={t}
        />
      ))}
    </ol>
  )
}

/** Archives the All view shows: two rows of four on wide screens, two on phones. */
const ALL_ARCHIVES = 8
/** Shorts the All view shows: one row of six; narrower rows show fewer. */
const ALL_SHORTS = 6
/** Reservations beside the lead; the Streams view lists every one. */
const SIDE_RESERVATIONS = 4
/** Earlier works stay visible beside reservations instead of being crowded out. */
const SIDE_WORKS = 3
/** What a filtered view shows: three rows of four, or two rows of Shorts. */
const FILTERED_ITEMS = 12

export function VideoShelf({
  videos,
  featuredVideoId,
  projection,
  focus,
  isLoading,
  onOpenArchive,
  locale,
  now,
  t,
}: {
  videos: Video[]
  featuredVideoId: string | null
  projection?: Dashboard["projection"]
  focus: Dashboard["focus"]
  isLoading: boolean
  onOpenArchive: () => void
  locale: Locale
  now: number
  t: Translator
}) {
  const [filter, setFilter] = useState<VideoFilter>("all")
  // A kind gets a tab only when present; the filter only helps with two.
  const kinds = (
    [
      { value: "streams", label: t("videos.streams") },
      { value: "works", label: t("videos.works") },
      { value: "shorts", label: t("videos.shorts") },
    ] as const
  )
    .map((item) => ({
      ...item,
      count: videos.filter((video) => videoMatchesFilter(video, item.value))
        .length,
    }))
    .filter((item) => item.count > 0)
  const hasFilter = kinds.length > 1
  const active: VideoFilter =
    hasFilter && kinds.some((item) => item.value === filter) ? filter : "all"
  const filters: { value: VideoFilter; label: string; count: number }[] = [
    { value: "all", label: t("common.all"), count: videos.length },
    ...kinds,
  ]
  let body: ReactNode
  if (isLoading) body = <Placeholder lines={4} />
  else if (!videos.length) body = <EmptyNote title={t("videos.empty")} />
  else if (active === "all") {
    const groups = groupVideos(videos, featuredVideoId, now, projection)
    const reservations = groups.reservations.slice(0, SIDE_RESERVATIONS)
    const sideWorks = groups.works.slice(0, SIDE_WORKS)
    const archives = groups.archives.slice(0, ALL_ARCHIVES)
    const shorts = groups.shorts.slice(0, ALL_SHORTS)
    const hasSide = Boolean(reservations.length || sideWorks.length)
    body = (
      <>
        {groups.lead || hasSide ? (
          <div
            className={`videos-lead${groups.lead ? "" : " is-tickets"}${hasSide ? "" : " is-solo"}`}
          >
            {groups.lead ? (
              <LeadCard
                video={groups.lead}
                isFeatured={groups.leadIsFeatured}
                focus={focus}
                locale={locale}
                now={now}
                t={t}
              />
            ) : null}
            {hasSide ? (
              <div className="videos-side">
                {reservations.length ? (
                  <div>
                    <Subhead>{t("videos.comingUp")}</Subhead>
                    <TicketList
                      videos={reservations}
                      locale={locale}
                      now={now}
                      t={t}
                    />
                  </div>
                ) : null}
                {sideWorks.length ? (
                  <div>
                    <Subhead>
                      {groups.leadIsFeatured
                        ? t("videos.works")
                        : t("videos.moreWorks")}
                    </Subhead>
                    <ol className="video-list">
                      {sideWorks.map((video) => (
                        <VideoRow
                          key={video.id}
                          video={video}
                          absoluteTime
                          locale={locale}
                          now={now}
                          t={t}
                        />
                      ))}
                    </ol>
                  </div>
                ) : null}
              </div>
            ) : null}
          </div>
        ) : null}
        {archives.length ? (
          <div className="videos-block">
            <Subhead>{t("videos.archives")}</Subhead>
            <VideoGrid
              videos={archives}
              variant="preview"
              locale={locale}
              now={now}
              t={t}
            />
          </div>
        ) : null}
        {shorts.length ? (
          <div className="videos-block">
            <Subhead>{t("videos.shorts")}</Subhead>
            <VideoGrid
              videos={shorts}
              variant="shorts-row"
              locale={locale}
              now={now}
              t={t}
            />
          </div>
        ) : null}
      </>
    )
  } else {
    const split = splitVideos(
      videos.filter((video) => videoMatchesFilter(video, active)),
      now,
      projection,
    )
    const view = {
      streams: {
        items: split.archives,
        title: t("videos.archives"),
        empty: t("videos.noStreams"),
      },
      works: {
        items: split.works,
        title: t("videos.works"),
        empty: t("videos.noWorks"),
      },
      shorts: {
        items: split.shorts,
        title: t("videos.shorts"),
        empty: t("videos.noShorts"),
      },
    }[active]
    const items = view.items
    body =
      split.reservations.length || items.length ? (
        <>
          {split.reservations.length ? (
            <div className="videos-block">
              <Subhead count={split.reservations.length}>
                {t("videos.comingUp")}
              </Subhead>
              <div className="videos-tickets-wide">
                <TicketList
                  videos={split.reservations}
                  locale={locale}
                  now={now}
                  t={t}
                />
              </div>
            </div>
          ) : null}
          {items.length ? (
            <div className="videos-block">
              <Subhead count={items.length}>{view.title}</Subhead>
              <VideoGrid
                videos={items.slice(0, FILTERED_ITEMS)}
                variant={active === "shorts" ? "shorts" : undefined}
                locale={locale}
                now={now}
                t={t}
              />
            </div>
          ) : null}
        </>
      ) : (
        <EmptyNote title={view.empty} />
      )
  }

  return (
    <section
      className="section videos"
      id="videos"
      aria-labelledby="videos-title"
    >
      <SectionTitle
        id="videos-title"
        kicker={
          <>
            <YouTubeLogo className="kicker-icon" aria-hidden="true" /> YouTube
            {" · "}
            <BilibiliLogo className="kicker-icon" aria-hidden="true" /> bilibili
          </>
        }
        title={t("videos.subtitle")}
      >
        {hasFilter && !isLoading ? (
          <div
            className="segmented"
            role="group"
            aria-label={t("videos.filter")}
          >
            {filters.map((item) => (
              <button
                type="button"
                key={item.value}
                aria-pressed={active === item.value}
                onClick={() => setFilter(item.value)}
              >
                {item.label}
                <small>{item.count}</small>
              </button>
            ))}
          </div>
        ) : null}
        <button
          type="button"
          className="pill-button is-collapsible"
          onClick={onOpenArchive}
          aria-haspopup="dialog"
          aria-label={t("videos.archive")}
          title={t("videos.archive")}
        >
          <History aria-hidden="true" />
          <span className="pill-label">{t("videos.archive")}</span>
        </button>
      </SectionTitle>
      {body}
    </section>
  )
}

/**
 * An archive card: the cover above the title, then the Japan time and kind.
 * Memoized, so the timeline's scrolled-to month can change without rendering
 * every loaded card again.
 */
const ArchiveCard = memo(function ArchiveCard({
  video,
  locale,
  now,
  t,
}: {
  video: Video
  locale: Locale
  now: number
  t: Translator
}) {
  const phase = videoPhase(video, now)
  const time = videoTime(video)
  const title = cleanVideoTitle(video.title)
  const links = videoLinks(video, locale, now)
  return (
    <li className={`archive-card is-${phase}`} data-video-id={video.id}>
      <div className="archive-card-link link-card" onClick={followCardLink}>
        <Thumb
          url={video.thumbnailUrl}
          badge={phase === "upcoming" ? "upcoming" : null}
          seconds={video.durationSeconds}
          video={video}
          links={links}
          t={t}
        />
        <TranslatedTitle
          className="archive-card-title"
          title={title}
          translations={video.translations}
          sourceLanguage={video.sourceLanguage}
          href={links[0]?.url}
          t={t}
        />
      </div>
      <p className="video-meta">
        {time ? (
          <time
            dateTime={time}
            title={`${formatStamp(time)} ${t("common.timezoneShort")}`}
          >
            {formatJapanDay(time, locale)} {formatJapanTime(time)}{" "}
            {t("common.timezoneShort")}
          </time>
        ) : null}
        <PhaseTag phase={phase} video={video} now={now} t={t} />
        <VideoTagChip video={video} t={t} />
        <VideoAvailabilityChip video={video} t={t} />
        <LengthNote video={video} t={t} />
        {links.length > 1 ? (
          <PlatformLinks links={links} title={title} variant="chip" t={t} />
        ) : null}
      </p>
    </li>
  )
})

/** The video archive's category filter, with each category's monthly total. */
export function VideoArchiveFilter({
  category,
  setCategory,
  counts,
  t,
}: {
  category: VideoCategory | null
  setCategory: (category: VideoCategory | null) => void
  counts: Record<string, number> | undefined
  t: Translator
}) {
  const all = counts
    ? VIDEO_CATEGORIES.reduce((sum, item) => sum + (counts[item] ?? 0), 0)
    : null
  return (
    <div className="archive-tools">
      <div
        className="segmented"
        role="group"
        aria-label={t("videos.categoryFilter")}
      >
        <button
          type="button"
          aria-pressed={!category}
          onClick={() => setCategory(null)}
        >
          {t("common.all")}
          {all != null ? <small>{all}</small> : null}
        </button>
        {VIDEO_CATEGORIES.map((item) => (
          <button
            type="button"
            key={item}
            aria-pressed={category === item}
            onClick={() => setCategory(item)}
          >
            {item === "other" ? null : <EventTagIcon tag={item} work />}
            {t(`videos.category.${item}`)}
            {counts ? <small>{counts[item] ?? 0}</small> : null}
          </button>
        ))}
      </div>
    </div>
  )
}

/**
 * Keeps an element at a viewport offset in the dialog body while the months
 * around it settle: a month coming into view swaps its estimated height for
 * its real one, which would push the element away. Each resize of the list
 * re-aligns it, for a second at most, and the hold ends as soon as the reader
 * scrolls or presses a key.
 */
function holdInView(
  body: HTMLElement,
  list: HTMLElement,
  find: () => HTMLElement | null,
  top: number,
) {
  const events = ["wheel", "touchstart", "pointerdown", "keydown"]
  const align = () => {
    const element = find()
    const offset = element ? element.getBoundingClientRect().top - top : 0
    if (Math.abs(offset) >= 1) body.scrollTop += offset
  }
  const observer =
    typeof ResizeObserver === "function" ? new ResizeObserver(align) : null
  const stop = () => {
    window.clearTimeout(timer)
    observer?.disconnect()
    for (const name of events) body.removeEventListener(name, stop)
  }
  for (const name of events)
    body.addEventListener(name, stop, { passive: true })
  observer?.observe(list)
  const timer = window.setTimeout(stop, 1000)
  align()
  return stop
}

/** A video's card in the timeline. */
function videoCard(root: HTMLElement, id: string) {
  for (const card of root.querySelectorAll<HTMLElement>("[data-video-id]"))
    if (card.dataset.videoId === id) return card
  return null
}

/** How far below the body's top edge a month header still counts as shown. */
const VISIBLE_MONTH_OFFSET = 64

/**
 * The video archive dialog's body: every stored video, newest first, under a
 * header per Japan month. The next page loads as the end scrolls near, the
 * month in view is reported as the body scrolls, and `jump` scrolls a loaded
 * month to the top, once that month is loaded. A timeline that starts from an
 * earlier month offers the newer videos above it; when they load above, the
 * video that was first stays where it was on screen.
 */
export function VideoTimeline({
  archive,
  isLoading,
  error,
  onRetry,
  onLoadMore,
  monthCounts,
  jump,
  onVisibleMonth,
  onNewer,
  locale,
  now,
  t,
}: {
  archive: ArchiveSnapshot<Video> | null
  isLoading: boolean
  error: Error | null
  onRetry: () => unknown
  onLoadMore: () => unknown
  /** Videos per `YYYY-MM` in the chosen category; null while unknown. */
  monthCounts: ReadonlyMap<string, number> | null
  /** A loaded month to scroll to; a new nonce scrolls again. */
  jump: { month: string; nonce: number } | null
  onVisibleMonth: (month: string) => void
  onNewer: () => void
  locale: Locale
  now: number
  t: Translator
}) {
  const listRef = useRef<HTMLDivElement>(null)
  const items = archive?.items
  const groups = useMemo(() => {
    const months: { month: string; videos: Video[] }[] = []
    for (const video of items || []) {
      const month = videoMonthKey(video)
      const last = months[months.length - 1]
      if (last?.month === month) last.videos.push(video)
      else months.push({ month, videos: [video] })
    }
    return months
  }, [items])
  const monthList = groups.map((group) => group.month).join(",")

  const reportRef = useRef(onVisibleMonth)
  reportRef.current = onVisibleMonth
  const shownRef = useRef<string | null>(null)
  // The month in view is the last one whose header has reached the top.
  useEffect(() => {
    const body = listRef.current?.closest<HTMLElement>(".archive-dialog-body")
    if (!body || !monthList) return undefined
    let frame = 0
    const measure = () => {
      frame = 0
      const top = body.getBoundingClientRect().top + VISIBLE_MONTH_OFFSET
      let shown: string | undefined
      for (const section of body.querySelectorAll<HTMLElement>(
        "[data-month]",
      )) {
        if (shown && section.getBoundingClientRect().top > top) break
        shown = section.dataset.month
      }
      if (shown && shown !== shownRef.current) {
        shownRef.current = shown
        reportRef.current(shown)
      }
    }
    const onScroll = () => {
      if (!frame) frame = window.requestAnimationFrame(measure)
    }
    measure()
    body.addEventListener("scroll", onScroll, { passive: true })
    return () => {
      body.removeEventListener("scroll", onScroll)
      if (frame) window.cancelAnimationFrame(frame)
    }
  }, [monthList])

  // A jump waits for its month to load, then scrolls there once.
  const jumpedRef = useRef<number | null>(null)
  useEffect(() => {
    if (!jump || jumpedRef.current === jump.nonce) return
    const list = listRef.current
    const body = list?.closest<HTMLElement>(".archive-dialog-body")
    const section = body?.querySelector<HTMLElement>(
      `[data-month="${jump.month}"]`,
    )
    if (!list || !body || !section) return
    jumpedRef.current = jump.nonce
    if (section === body.querySelector("[data-month]")) {
      body.scrollTo?.({ top: 0 })
      return
    }
    section.scrollIntoView?.({ block: "start" })
    const month = jump.month
    return holdInView(
      body,
      list,
      () => body.querySelector<HTMLElement>(`[data-month="${month}"]`),
      section.getBoundingClientRect().top,
    )
  }, [jump, monthList])

  // Where the first video stood before newer ones loaded above it.
  const keepRef = useRef<{ id: string; top: number } | null>(null)
  const firstId = items?.[0]?.id
  const firstPage = archive?.firstPage ?? 1
  useLayoutEffect(() => {
    const keep = keepRef.current
    if (!keep || keep.id === firstId) return
    keepRef.current = null
    const list = listRef.current
    const body = list?.closest<HTMLElement>(".archive-dialog-body")
    if (!list || !body) return
    return holdInView(body, list, () => videoCard(body, keep.id), keep.top)
  }, [firstId])
  // Whether the reader asked for newer videos and they are still loading; a
  // background reread neither relabels nor disables the button, so it keeps
  // keyboard focus.
  const [newerPending, setNewerPending] = useState(false)
  useEffect(() => {
    if (isLoading) return
    // A read that settled without newer videos leaves nothing to hold.
    keepRef.current = null
    setNewerPending(false)
  }, [isLoading, newerPending])
  const showNewer = () => {
    if (isLoading) return
    const card =
      firstId && listRef.current ? videoCard(listRef.current, firstId) : null
    keepRef.current =
      card && firstPage > 1
        ? { id: firstId!, top: card.getBoundingClientRect().top }
        : null
    setNewerPending(true)
    onNewer()
  }

  const total = archive?.total ?? 0
  return (
    <div className="video-timeline" ref={listRef}>
      <ArchiveState
        error={error}
        onRetry={onRetry}
        loading={!archive && isLoading}
        empty={Boolean(archive && !archive.items.length)}
        emptyTitle={t("videos.archiveEmpty")}
        onJump={onNewer}
        locale={locale}
        t={t}
      />
      {archive?.adjacent.next || firstPage > 1 ? (
        <div className="archive-more is-newer">
          <button
            type="button"
            className="pill-button"
            aria-disabled={isLoading || undefined}
            aria-busy={newerPending || undefined}
            onClick={showNewer}
          >
            <ChevronUp aria-hidden="true" />
            {newerPending ? t("common.refreshing") : t("videos.newer")}
          </button>
        </div>
      ) : null}
      {groups.map(({ month, videos }) => {
        const [year, monthNumber] = month.split("-").map(Number)
        const count = monthCounts?.get(month) ?? videos.length
        return (
          <section
            key={month}
            className="video-month"
            data-month={month}
            aria-labelledby={`video-month-${month}`}
          >
            <h3 className="video-month-head" id={`video-month-${month}`}>
              <span>
                {month
                  ? formatMonth(new Date(year, monthNumber - 1, 1), locale)
                  : t("videos.archiveTitle")}
              </span>
              <small>{t("videos.monthCount", { count })}</small>
            </h3>
            <ol className="archive-videos">
              {videos.map((video) => (
                <ArchiveCard
                  key={video.id}
                  video={video}
                  locale={locale}
                  now={now}
                  t={t}
                />
              ))}
            </ol>
          </section>
        )
      })}
      {archive?.items.length ? (
        <p className="video-timeline-status" aria-live="polite">
          {t("videos.shown", { shown: archive.items.length, total })}
        </p>
      ) : null}
      {archive ? (
        <ArchiveMore
          hasNext={archive.hasNext}
          isLoading={isLoading}
          label={t("videos.loadMore")}
          onLoadMore={onLoadMore}
          auto={{ failed: Boolean(error), loaded: archive.items.length }}
          t={t}
        />
      ) : null}
    </div>
  )
}
