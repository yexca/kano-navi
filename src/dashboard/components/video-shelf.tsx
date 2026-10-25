import type {
  ArchiveSnapshot,
  Dashboard,
  Locale,
  Translator,
  Video,
} from "@/types"
import { ArrowUpRight, Clock3, History, ImageOff, Play } from "lucide-react"

import { EmptyNote, Placeholder, SectionTitle } from "@/site/site-shell"
import { ExternalLink, YouTubeLogo } from "@/site/links"
import {
  cleanVideoTitle,
  formatJapanDay,
  formatJapanTime,
  formatRelative,
  formatStamp,
  videoKindLabel,
  videoTime,
} from "../format"
import { ArchiveMore, ArchiveState } from "./archive-dialog"

function Thumb({
  url,
  isUpcoming,
  t,
}: {
  url: string | null
  isUpcoming: boolean
  t: Translator
}) {
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
      <span className="video-thumb-badge" aria-hidden="true">
        {isUpcoming ? <Clock3 /> : <Play fill="currentColor" />}
      </span>
    </span>
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
        <Thumb url={video.thumbnailUrl} isUpcoming={video.isUpcoming} t={t} />
        <span className="video-row-copy">
          <span className="video-row-meta">
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
          </span>
          <span className="video-row-title">
            {cleanVideoTitle(video.title)}
          </span>
        </span>
      </ExternalLink>
    </li>
  )
}

function Featured({
  video,
  focus,
  locale,
  now,
  t,
}: {
  video: Video
  focus: Dashboard["focus"]
  locale: Locale
  now: number
  t: Translator
}) {
  const usesFocus = focus?.videoId === video.id
  const title = cleanVideoTitle((usesFocus && focus.title) || video.title)
  const thumbnail = (usesFocus && focus.imageUrl) || video.thumbnailUrl
  return (
    <article className="video-feature">
      <ExternalLink
        className="video-feature-art"
        href={video.url}
        aria-label={`${t("videos.watch")}: ${title}`}
      >
        <Thumb url={thumbnail} isUpcoming={video.isUpcoming} t={t} />
      </ExternalLink>
      <div className="video-feature-copy">
        <p className="video-row-meta">
          <span className="tag tone-berry">{t("videos.featured")}</span>
          <span className="tag">{videoKindLabel(video, t)}</span>
          {usesFocus && focus.dateLabel ? (
            <span>{focus.dateLabel}</span>
          ) : (
            <VideoWhen video={video} locale={locale} now={now} t={t} />
          )}
        </p>
        <h3>{title}</h3>
        {usesFocus && focus.description ? <p>{focus.description}</p> : null}
        <div className="video-feature-actions">
          <ExternalLink className="pill-button is-solid" href={video.url}>
            <Play fill="currentColor" aria-hidden="true" />
            {t("videos.watch")}
          </ExternalLink>
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
  const featured = featuredVideoId
    ? videos.find((video) => video.id === featuredVideoId) || null
    : null
  const rest = featured
    ? videos.filter((video) => video.id !== featured.id)
    : videos
  // Reservations first (soonest first), then the newest uploads and archives.
  const reserved = rest
    .filter((video) => video.isUpcoming)
    .sort(
      (a, b) => Date.parse(videoTime(a) || "") - Date.parse(videoTime(b) || ""),
    )
  const recent = rest.filter((video) => !video.isUpcoming)
  const list = [...reserved, ...recent].slice(0, featured ? 5 : 8)

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
          </>
        }
        title={t("videos.subtitle")}
      >
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

      {isLoading ? (
        <Placeholder lines={4} />
      ) : !videos.length ? (
        <EmptyNote title={t("videos.empty")} />
      ) : (
        <div className={`videos-layout${featured ? " has-feature" : ""}`}>
          {featured ? (
            <Featured
              video={featured}
              focus={focus}
              locale={locale}
              now={now}
              t={t}
            />
          ) : null}
          <ol className="video-list">
            {list.map((video) => (
              <VideoRow
                key={video.id}
                video={video}
                locale={locale}
                now={now}
                t={t}
              />
            ))}
          </ol>
        </div>
      )}
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
        empty={Boolean(archive) && !archive.items.length}
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
