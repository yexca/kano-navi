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
import { BilibiliLogo, ExternalLink, YouTubeLogo } from "@/site/links"
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
  videoMatchesFilter,
  videoPhase,
  type VideoFilter,
  type VideoPhase,
} from "../video-groups"
import { ArchiveMore, ArchiveState } from "./archive-dialog"
import { TranslatedHeading, TranslatedTitle } from "./translated-title"

const platformNames: Record<string, string> = {
  youtube: "YouTube",
  bilibili: "bilibili",
}

function PlatformMark({ source }: { source: string }) {
  if (source === "youtube") return <YouTubeLogo aria-hidden="true" />
  if (source === "bilibili") return <BilibiliLogo aria-hidden="true" />
  return <ArrowUpRight aria-hidden="true" />
}

/**
 * One link per platform: the video itself, then every merged upload of the
 * same work, such as its bilibili copy.
 */
function PlatformLinks({
  video,
  title,
  variant,
  t,
}: {
  video: Video
  title: string
  variant: "pill" | "chip"
  t: Translator
}) {
  const links = [
    { source: video.source, url: video.url },
    ...(video.alternates || []),
  ]
  return (
    <span className="platform-links">
      {links.map((link, index) => {
        const name = platformNames[link.source] || link.source
        const className =
          variant === "pill"
            ? `pill-button${index === 0 ? " is-solid" : ""}`
            : "platform-chip"
        return (
          <ExternalLink
            key={link.url}
            className={className}
            href={link.url}
            aria-label={`${t("videos.watchOn", { platform: name })}: ${title}`}
          >
            <PlatformMark source={link.source} />
            {name}
          </ExternalLink>
        )
      })}
    </span>
  )
}

function Thumb({
  url,
  badge = null,
  seconds,
  t,
}: {
  url: string | null
  badge?: "play" | "upcoming" | null
  seconds?: number | null
  t: Translator
}) {
  const length = formatDuration(seconds)
  return (
    <span className="video-thumb">
      {url ? (
        <img src={url} alt="" loading="lazy" />
      ) : (
        <span className="video-thumb-missing">
          <ImageOff aria-hidden="true" />
          <span>{t("media.unavailable")}</span>
        </span>
      )}
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
  return (
    <article className="video-lead">
      <ExternalLink
        className="video-lead-art"
        href={video.url}
        aria-label={`${t("videos.watch")}: ${title}`}
      >
        <Thumb
          url={thumbnail}
          badge={phase === "upcoming" ? "upcoming" : "play"}
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
          <TranslatedHeading
            title={title}
            video={video}
            locale={locale}
            t={t}
          />
        )}
        {usesFocus && focus.description ? <p>{focus.description}</p> : null}
        <div className="video-lead-actions">
          <PlatformLinks video={video} title={title} variant="pill" t={t} />
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
  return (
    <li>
      <ExternalLink className={`video-ticket is-${phase}`} href={video.url}>
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
          </span>
          <TranslatedTitle
            className="video-ticket-title"
            title={cleanVideoTitle(video.title)}
            translations={video.translations}
            locale={locale}
            t={t}
          />
        </span>
      </ExternalLink>
    </li>
  )
}

/** A grid card: large artwork, a two-line title, the date and platforms. */
function VideoCard({
  video,
  showKind,
  locale,
  t,
}: {
  video: Video
  showKind: boolean
  locale: Locale
  t: Translator
}) {
  const title = cleanVideoTitle(video.title)
  return (
    <li className="video-card">
      <ExternalLink className="video-card-link" href={video.url}>
        <Thumb url={video.thumbnailUrl} seconds={video.durationSeconds} t={t} />
        <TranslatedTitle
          className="video-card-title"
          title={title}
          translations={video.translations}
          locale={locale}
          t={t}
        />
      </ExternalLink>
      <p className="video-meta">
        <JapanDate video={video} locale={locale} t={t} />
        {showKind ? (
          <span className="tag">{videoKindLabel(video, t)}</span>
        ) : null}
        <LengthNote video={video} t={t} />
        {video.alternates?.length ? (
          <PlatformLinks video={video} title={title} variant="chip" t={t} />
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
  return (
    <li>
      <ExternalLink
        className={`video-row${video.isUpcoming ? " is-upcoming" : ""}`}
        href={video.url}
      >
        <Thumb
          url={video.thumbnailUrl}
          badge={video.isUpcoming ? "upcoming" : "play"}
          seconds={video.durationSeconds}
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
          </span>
          <TranslatedTitle
            className="video-row-title"
            title={cleanVideoTitle(video.title)}
            translations={video.translations}
            locale={locale}
            t={t}
          />
        </span>
      </ExternalLink>
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
  locale,
  t,
}: {
  videos: Video[]
  showKind: boolean
  locale: Locale
  t: Translator
}) {
  return (
    <ol className="video-grid">
      {videos.map((video) => (
        <VideoCard
          key={video.id}
          video={video}
          showKind={showKind}
          locale={locale}
          t={t}
        />
      ))}
    </ol>
  )
}

/** Archives the All view shows: two rows of four on wide screens. */
const ALL_ARCHIVES = 8
/** Reservations beside the lead; the Streams view lists every one. */
const SIDE_RESERVATIONS = 4
/** Archives or works a filtered view shows: three rows of four. */
const FILTERED_ITEMS = 12

export function VideoShelf({
  videos,
  featuredVideoId,
  focus,
  channelUrl,
  isLoading,
  onOpenArchive,
  locale,
  now,
  t,
}: {
  videos: Video[]
  featuredVideoId: string | null
  focus: Dashboard["focus"]
  channelUrl: string
  isLoading: boolean
  onOpenArchive: () => void
  locale: Locale
  now: number
  t: Translator
}) {
  const [filter, setFilter] = useState<VideoFilter>("all")
  const streamCount = videos.filter((video) =>
    videoMatchesFilter(video, "streams"),
  ).length
  const workCount = videos.length - streamCount
  // The filter only helps when both kinds are present.
  const hasFilter = streamCount > 0 && workCount > 0
  const active: VideoFilter = hasFilter ? filter : "all"
  const filters: { value: VideoFilter; label: string; count: number }[] = [
    { value: "all", label: t("common.all"), count: videos.length },
    { value: "streams", label: t("videos.streams"), count: streamCount },
    { value: "works", label: t("videos.works"), count: workCount },
  ]
  // Under a shortened grid, the month archive holds the rest.
  const more = (
    <div className="videos-more">
      <button
        type="button"
        className="pill-button"
        onClick={onOpenArchive}
        aria-haspopup="dialog"
      >
        <History aria-hidden="true" />
        {t("videos.archive")}
      </button>
    </div>
  )

  let body: ReactNode
  if (isLoading) body = <Placeholder lines={4} />
  else if (!videos.length) body = <EmptyNote title={t("videos.empty")} />
  else if (active === "all") {
    const groups = groupVideos(videos, featuredVideoId, now)
    const reservations = groups.reservations.slice(0, SIDE_RESERVATIONS)
    const sideWorks = groups.works.slice(0, reservations.length ? 1 : 3)
    const archives = groups.archives.slice(0, ALL_ARCHIVES)
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
              locale={locale}
              t={t}
            />
            {groups.archives.length > archives.length ? more : null}
          </div>
        ) : null}
      </>
    )
  } else {
    const split = splitVideos(
      videos.filter((video) => videoMatchesFilter(video, active)),
      now,
    )
    const items = active === "streams" ? split.archives : split.works
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
              <Subhead count={items.length}>
                {active === "streams"
                  ? t("videos.archives")
                  : t("videos.works")}
              </Subhead>
              <VideoGrid
                videos={items.slice(0, FILTERED_ITEMS)}
                showKind={active === "works"}
                locale={locale}
                t={t}
              />
              {items.length > FILTERED_ITEMS ? more : null}
            </div>
          ) : null}
        </>
      ) : (
        <EmptyNote
          title={
            active === "streams" ? t("videos.noStreams") : t("videos.noWorks")
          }
        />
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
          className="pill-button"
          onClick={onOpenArchive}
          aria-haspopup="dialog"
        >
          <History aria-hidden="true" />
          {t("videos.archive")}
        </button>
        <ExternalLink className="pill-button" href={`${channelUrl}/videos`}>
          {t("videos.openChannel")}
          <ArrowUpRight aria-hidden="true" />
        </ExternalLink>
      </SectionTitle>
      {body}
    </section>
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
        <ol className="video-list archive-videos">
          {archive.items.map((video) => (
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
