import { ArrowUpRight, Clock3, ImageIcon, Play } from "lucide-react"

import {
  cleanVideoTitle,
  formatRelative,
  formatStamp,
  videoKindLabel,
  videoTime,
} from "../format"
import {
  EmptyState,
  ExternalLink,
  SectionHeading,
  Skeleton,
  YouTubeLogo,
} from "./primitives"

function Thumbnail({ url, isUpcoming }) {
  return (
    <span className="video-thumb">
      {url ? (
        <img src={url} alt="" loading="lazy" />
      ) : (
        <span className="video-thumb-empty" aria-hidden="true">
          <ImageIcon />
        </span>
      )}
      <span className="video-play" aria-hidden="true">
        {isUpcoming ? <Clock3 /> : <Play fill="currentColor" />}
      </span>
    </span>
  )
}

function VideoCard({ video, locale, now, t }) {
  const time = videoTime(video)
  return (
    <ExternalLink
      className={`video-card${video.isUpcoming ? " is-upcoming" : ""}`}
      href={video.url}
    >
      <Thumbnail url={video.thumbnailUrl} isUpcoming={video.isUpcoming} />
      <span className="video-meta">
        <span className="chip">{videoKindLabel(video, t)}</span>
        <time dateTime={time || undefined} title={`${formatStamp(time)} JST`}>
          {video.isUpcoming
            ? `${t("videos.reserved")} ${formatStamp(time)}`
            : formatRelative(time, locale, now)}
        </time>
      </span>
      <span className="video-title">{cleanVideoTitle(video.title)}</span>
    </ExternalLink>
  )
}

function FeaturedVideo({ video, focus, locale, now, t }) {
  const usesFocus = focus?.videoId === video.id
  const title = cleanVideoTitle((usesFocus && focus.title) || video.title)
  const thumbnail = (usesFocus && focus.imageUrl) || video.thumbnailUrl
  const time = videoTime(video) || (usesFocus ? focus.updatedAt : null)

  return (
    <article className="video-featured">
      <ExternalLink
        href={video.url}
        className="video-featured-media"
        aria-label={`${t("videos.watch")}: ${title}`}
      >
        <Thumbnail url={thumbnail} isUpcoming={video.isUpcoming} />
      </ExternalLink>
      <div className="video-featured-copy">
        <p className="video-meta">
          <span className="chip is-accent">{t("videos.featured")}</span>
          <span className="chip">{videoKindLabel(video, t)}</span>
          <time dateTime={time || undefined}>
            {(usesFocus && focus.dateLabel) ||
              formatRelative(time, locale, now)}
          </time>
        </p>
        <h3>{title}</h3>
        {usesFocus && focus.description ? <p>{focus.description}</p> : null}
        <div className="video-featured-actions">
          <ExternalLink className="primary-action" href={video.url}>
            <Play fill="currentColor" aria-hidden="true" />
            {t("videos.watch")}
          </ExternalLink>
          {usesFocus && focus.sourceUrl ? (
            <ExternalLink className="quiet-action" href={focus.sourceUrl}>
              {t("videos.officialSource")}
              <ArrowUpRight aria-hidden="true" />
            </ExternalLink>
          ) : null}
        </div>
      </div>
    </article>
  )
}

export function VideoSection({
  videos,
  featuredVideoId,
  focus,
  channelUrl,
  isLoading,
  locale,
  now,
  t,
}) {
  const featured = featuredVideoId
    ? videos.find((video) => video.id === featuredVideoId) || null
    : null
  const others = (
    featured ? videos.filter((video) => video.id !== featured.id) : videos
  ).slice(0, featured ? 6 : 8)

  return (
    <section
      className="panel video-section"
      id="videos"
      aria-labelledby="videos-title"
    >
      <SectionHeading
        id="videos-title"
        eyebrow={<YouTubeLogo className="eyebrow-icon" aria-hidden="true" />}
        title={t("videos.title")}
        subtitle={t("videos.subtitle")}
      >
        <ExternalLink className="soft-button" href={`${channelUrl}/videos`}>
          {t("videos.openChannel")}
          <ArrowUpRight aria-hidden="true" />
        </ExternalLink>
      </SectionHeading>

      {isLoading ? (
        <Skeleton lines={4} />
      ) : !videos.length ? (
        <EmptyState icon={ImageIcon} title={t("videos.empty")} />
      ) : (
        <div className={`video-layout${featured ? " has-featured" : ""}`}>
          {featured ? (
            <FeaturedVideo
              video={featured}
              focus={focus}
              locale={locale}
              now={now}
              t={t}
            />
          ) : null}
          <div className="video-grid">
            {others.map((video) => (
              <VideoCard
                key={video.id}
                video={video}
                locale={locale}
                now={now}
                t={t}
              />
            ))}
          </div>
        </div>
      )}
    </section>
  )
}
