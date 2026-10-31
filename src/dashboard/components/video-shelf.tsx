import type {
  ArchiveSnapshot,
  Dashboard,
  Locale,
  Translator,
  Video,
} from "@/types"
import {
  ArrowUpRight,
  Clock3,
  History,
  ImageOff,
  Play,
  Radio,
} from "lucide-react"
import { useState, type ReactNode } from "react"

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
  formatRelative,
  formatStamp,
  japanDateParts,
  videoKindLabel,
  videoTime,
} from "../format"
import {
  groupVideos,
  splitVideos,
  videoIsStream,
  videoLinks,
  videoMatchesFilter,
  videoPhase,
  type VideoFilter,
  type VideoLink,
  type VideoPhase,
} from "../video-groups"
import { mediaSrcSet } from "@/lib/media"
import { ArchiveMore, ArchiveState } from "./archive-dialog"
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
  links = [],
  t,
}: {
  url: string | null
  sizes?: string
  badge?: "play" | "upcoming" | null
  seconds?: number | null
  /** The video's platforms; two or more mark the cover. */
  links?: VideoLink[]
  t: Translator
}) {
  const length = formatDuration(seconds)
  return (
    <span className="video-thumb">
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
      <PlatformBadge links={links} />
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
  t,
}: {
  phase: VideoPhase
  video: Video
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
  return <span className="tag">{videoKindLabel(video, t)}</span>
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
        href={links[0].url}
        aria-label={`${t("videos.watch")}: ${title}`}
      >
        <Thumb
          url={thumbnail}
          sizes={LEAD_THUMB_SIZES}
          badge={phase === "upcoming" ? "upcoming" : "play"}
          links={links}
          t={t}
        />
      </ExternalLink>
      <div className="video-lead-copy">
        <p className="video-meta">
          <span className="tag tone-berry">
            {isFeatured ? t("videos.featured") : t("videos.latestWork")}
          </span>
          {phase === "past" ? (
            <span className="tag">{videoKindLabel(video, t)}</span>
          ) : (
            <PhaseTag phase={phase} video={video} t={t} />
          )}
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
            <PhaseTag phase={phase} video={video} t={t} />
            {videoIsStream(video) ? null : (
              <span className="tag">{videoKindLabel(video, t)}</span>
            )}
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
            href={links[0].url}
            t={t}
          />
        </span>
      </div>
    </li>
  )
}

/** A grid card: large artwork, a two-line title, the date and platforms. */
function VideoCard({
  video,
  showKind,
  locale,
  now,
  t,
}: {
  video: Video
  showKind: boolean
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
          links={links}
          t={t}
        />
        <TranslatedTitle
          className="video-card-title"
          title={title}
          translations={video.translations}
          href={links[0].url}
          t={t}
        />
      </div>
      <p className="video-meta">
        <JapanDate video={video} locale={locale} t={t} />
        {showKind ? (
          <span className="tag">{videoKindLabel(video, t)}</span>
        ) : null}
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
  return (
    <time
      dateTime={time}
      title={`${formatStamp(time)} ${t("common.timezoneShort")}`}
    >
      {video.isUpcoming || absoluteTime
        ? `${formatJapanDay(time, locale)} ${formatJapanTime(time)} ${t("common.timezoneShort")}`
        : formatRelative(time, locale, now)}
    </time>
  )
}

/** A thumbnail row, for earlier works beside the lead and the archive dialog. */
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
  const links = videoLinks(video, locale, now)
  return (
    <li>
      <div
        className={`video-row link-card${video.isUpcoming ? " is-upcoming" : ""}`}
        onClick={followCardLink}
      >
        <Thumb
          url={video.thumbnailUrl}
          badge={video.isUpcoming ? "upcoming" : "play"}
          seconds={video.durationSeconds}
          links={links}
          t={t}
        />
        <span className="video-row-copy">
          <span className="video-meta">
            <span className={`tag${video.isUpcoming ? " tone-berry" : ""}`}>
              {video.isUpcoming
                ? t("videos.reserved")
                : videoKindLabel(video, t)}
            </span>
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
            href={links[0].url}
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
  showKind,
  variant,
  locale,
  now,
  t,
}: {
  videos: Video[]
  showKind: boolean
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
          showKind={showKind}
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
  focus,
  isLoading,
  onOpenArchive,
  locale,
  now,
  t,
}: {
  videos: Video[]
  featuredVideoId: string | null
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
    const groups = groupVideos(videos, featuredVideoId, now)
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
              showKind={false}
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
              showKind={false}
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
                showKind={active === "works"}
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

/** An archive card: the cover above the title, then the Japan time and kind. */
function ArchiveCard({
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
    <li className={`archive-card is-${phase}`}>
      <div className="archive-card-link link-card" onClick={followCardLink}>
        <Thumb
          url={video.thumbnailUrl}
          badge={phase === "upcoming" ? "upcoming" : null}
          seconds={video.durationSeconds}
          links={links}
          t={t}
        />
        <TranslatedTitle
          className="archive-card-title"
          title={title}
          translations={video.translations}
          href={links[0].url}
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
        <PhaseTag phase={phase} video={video} t={t} />
        <LengthNote video={video} t={t} />
        {links.length > 1 ? (
          <PlatformLinks links={links} title={title} variant="chip" t={t} />
        ) : null}
      </p>
    </li>
  )
}

/** The video archive dialog's body: one month of stored videos, newest first. */
export function VideoArchive({
  archive,
  isLoading,
  error,
  onRetry,
  onLoadMore,
  onJump,
  locale,
  now,
  t,
}: {
  archive: ArchiveSnapshot<Video> | null
  isLoading: boolean
  error: Error | null
  onRetry: () => unknown
  onLoadMore: () => unknown
  onJump: (month: Date) => void
  locale: Locale
  now: number
  t: Translator
}) {
  return (
    <>
      <ArchiveState
        error={error}
        onRetry={onRetry}
        loading={!archive && isLoading}
        empty={Boolean(archive && !archive.items.length)}
        emptyTitle={t("videos.archiveEmpty")}
        adjacent={archive?.adjacent}
        onJump={onJump}
        locale={locale}
        t={t}
      />
      {archive?.items.length ? (
        <ol className="archive-videos">
          {archive.items.map((video) => (
            <ArchiveCard
              key={video.id}
              video={video}
              locale={locale}
              now={now}
              t={t}
            />
          ))}
        </ol>
      ) : null}
      {archive ? (
        <ArchiveMore
          hasNext={archive.hasNext}
          isLoading={isLoading}
          label={t("videos.moreInMonth")}
          onLoadMore={onLoadMore}
          t={t}
        />
      ) : null}
    </>
  )
}
