import { useState } from "react"
import { ExternalLink, ImageOff, Star, StarOff } from "lucide-react"

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

function VideoCard({
  video,
  featured,
  onFeature,
}: {
  video: AdminVideo
  featured: boolean
  onFeature: (id: string | null) => void
}) {
  const { t, locale } = useAppSettings()
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
        <small>
          {video.isUpcoming
            ? t("admin.videos.scheduled", {
                time: formatDateTime(video.scheduledAt, locale),
              })
            : formatDateTime(video.publishedAt, locale)}
          {video.kind ? ` · ${video.kind}` : ""}
        </small>
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
            <Button size="sm" icon={Star} onClick={() => onFeature(video.id)}>
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
  const feature = async (id: string | null) => {
    const result = await act(
      () => api.setFeatured(id),
      id ? "admin.notice.featured" : "admin.notice.unfeatured",
    )
    if (result && data)
      videos.setData({ ...data, featuredVideoId: result.featuredVideoId })
  }
  return (
    <>
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
        <p className="adm-muted">
          {data?.featuredVideoId
            ? t("admin.videos.pinned")
            : t("admin.videos.automatic")}
        </p>
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
