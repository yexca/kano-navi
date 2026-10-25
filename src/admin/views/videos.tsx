import { useState } from "react"
import { ExternalLink, ImageOff, Sparkles, Star, StarOff } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatDateTime } from "@/admin/format"
import type { AdminVideo } from "@/admin/types"
import {
  Badge,
  Button,
  Empty,
  Loading,
  PageHeader,
  Pager,
  Panel,
  Segmented,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { QuickFetch } from "@/admin/views/fetch-parts"

/** Only local, already cached media is shown; never a remote CDN URL. */
function localUrl(value: string | null | undefined) {
  return value && value.startsWith("/") ? value : null
}

function VideoWhen({ video }: { video: AdminVideo }) {
  const { t, locale } = useAppSettings()
  return (
    <small>
      {video.isUpcoming
        ? t("admin.videos.scheduled", {
            time: formatDateTime(video.scheduledAt, locale),
          })
        : formatDateTime(video.publishedAt, locale)}
      {video.kind ? ` · ${video.kind}` : ""}
    </small>
  )
}

/**
 * The pinned video, wherever the list is paged or filtered, with a one-click
 * return to the automatic choice.
 */
function FeaturedVideo({
  video,
  onClear,
}: {
  video: AdminVideo | null
  onClear: () => Promise<void>
}) {
  const { t } = useAppSettings()
  const [busy, setBusy] = useState(false)
  if (!video)
    return (
      <div className="adm-featured is-automatic">
        <span className="adm-featured-icon">
          <Sparkles aria-hidden />
        </span>
        <div className="adm-featured-body">
          <strong>{t("admin.videos.none")}</strong>
          <small>{t("admin.videos.automatic")}</small>
        </div>
      </div>
    )
  const thumbnail = localUrl(video.thumbnailUrl)
  return (
    <div className="adm-featured">
      <div className="adm-featured-thumb">
        {thumbnail ? (
          <img src={thumbnail} alt="" loading="lazy" />
        ) : (
          <ImageOff aria-hidden />
        )}
      </div>
      <div className="adm-featured-body">
        <span className="adm-featured-label">
          <Star aria-hidden />
          {t("admin.videos.current")}
        </span>
        <strong>{video.title}</strong>
        <VideoWhen video={video} />
      </div>
      <div className="adm-actions">
        <Button
          size="sm"
          variant="ghost"
          icon={StarOff}
          busy={busy}
          onClick={async () => {
            setBusy(true)
            await onClear()
            setBusy(false)
          }}
        >
          {t("admin.videos.unfeature")}
        </Button>
        <a
          className="adm-link"
          href={video.url}
          target="_blank"
          rel="noreferrer"
        >
          {t("admin.videos.open")}
          <ExternalLink aria-hidden />
        </a>
      </div>
    </div>
  )
}

function VideoCard({
  video,
  featured,
  onFeature,
}: {
  video: AdminVideo
  featured: boolean
  onFeature: (video: AdminVideo | null) => void
}) {
  const { t } = useAppSettings()
  const thumbnail = localUrl(video.thumbnailUrl)
  return (
    <article className={`adm-video${featured ? " is-featured" : ""}`}>
      <div className="adm-video-thumb">
        {thumbnail ? (
          <img src={thumbnail} alt="" loading="lazy" />
        ) : (
          <ImageOff aria-hidden />
        )}
        {featured ? (
          <Badge tone="berry" icon={Star}>
            {t("admin.videos.featured")}
          </Badge>
        ) : null}
      </div>
      <div className="adm-video-body">
        <strong>{video.title}</strong>
        <VideoWhen video={video} />
        <div className="adm-actions">
          {featured ? (
            <Button
              size="sm"
              variant="ghost"
              icon={StarOff}
              onClick={() => onFeature(null)}
            >
              {t("admin.videos.unfeature")}
            </Button>
          ) : (
            <Button size="sm" icon={Star} onClick={() => onFeature(video)}>
              {t("admin.videos.feature")}
            </Button>
          )}
          <a
            className="adm-link"
            href={video.url}
            target="_blank"
            rel="noreferrer"
          >
            {t("admin.videos.open")}
            <ExternalLink aria-hidden />
          </a>
        </div>
      </div>
    </article>
  )
}

function VideoList() {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [status, setStatus] = useState<"" | "published" | "upcoming" | "live">(
    "",
  )
  const [page, setPage] = useState(1)
  const videos = useResource(
    () => api.videos({ status, page, pageSize: 24 }),
    [status, page],
  )
  const data = videos.data
  const feature = async (video: AdminVideo | null) => {
    const result = await act(
      () => api.setFeatured(video?.id ?? null),
      video ? "admin.notice.featured" : "admin.notice.unfeatured",
    )
    if (result)
      videos.setData(
        (current) =>
          current && {
            ...current,
            featuredVideoId: result.featuredVideoId,
            featuredVideo: result.featuredVideoId ? video : null,
          },
      )
  }
  return (
    <>
      {data ? (
        <FeaturedVideo
          video={data.featuredVideo}
          onClear={() => feature(null)}
        />
      ) : null}
      <div className="adm-toolbar">
        <Segmented
          label={t("admin.videos.filter")}
          value={status}
          onChange={(value) => {
            setStatus(value)
            setPage(1)
          }}
          options={[
            { value: "", label: t("admin.videos.all") },
            { value: "upcoming", label: t("admin.videos.upcoming") },
            { value: "live", label: t("admin.videos.live") },
            { value: "published", label: t("admin.videos.published") },
          ]}
        />
      </div>
      {!data ? (
        <Loading />
      ) : data.items.length ? (
        <>
          <div className="adm-video-grid">
            {data.items.map((video) => (
              <VideoCard
                key={video.id}
                video={video}
                featured={video.id === data.featuredVideoId}
                onFeature={feature}
              />
            ))}
          </div>
          <Pager
            page={data.page}
            totalPages={data.totalPages}
            total={data.total}
            onPage={setPage}
          />
        </>
      ) : (
        <Empty icon={ImageOff} title={t("admin.videos.empty")} />
      )}
    </>
  )
}

export function VideosView({ navigate }: ViewProps) {
  const { t } = useAppSettings()
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.videos.title")}
        description={t("admin.videos.description")}
      />
      <QuickFetch source="youtube" navigate={navigate} />
      <Panel>
        <VideoList />
      </Panel>
    </div>
  )
}
