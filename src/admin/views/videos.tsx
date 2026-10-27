import { useState } from "react"
import {
  Clock3,
  Download,
  ExternalLink,
  History,
  ImageOff,
  Pencil,
  Sparkles,
  Star,
  StarOff,
  Tv,
} from "lucide-react"

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
import { MergeLine, MergeSuggestions } from "@/admin/views/merge-parts"
import {
  TranslationBacklog,
  TranslationCell,
} from "@/admin/views/translation-parts"
import { VideoEditor } from "@/admin/views/video-editor"

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

/** Corrections, platform history and a pending refresh, when there are any. */
function VideoMarks({ video }: { video: AdminVideo }) {
  const { t } = useAppSettings()
  const corrected =
    video.manualTitle !== null ||
    video.manualKind !== null ||
    video.manualThumbnail !== null ||
    video.manualIsShort !== null
  if (!corrected && !video.revisions && !video.refreshRequestedAt) return null
  return (
    <span className="adm-video-marks">
      {corrected ? (
        <Badge tone="sky" icon={Pencil}>
          {t("admin.videos.corrected")}
        </Badge>
      ) : null}
      {video.revisions ? (
        <Badge icon={History}>
          {t("admin.videos.revisionCount", { count: video.revisions })}
        </Badge>
      ) : null}
      {video.refreshRequestedAt ? (
        <Badge tone="honey" icon={Clock3}>
          {t("admin.videos.refreshQueued")}
        </Badge>
      ) : null}
    </span>
  )
}

function VideoCard({
  video,
  featured,
  onFeature,
  onEdit,
  onTranslated,
  onMerged,
}: {
  video: AdminVideo
  featured: boolean
  onFeature: (video: AdminVideo | null) => void
  onEdit: (video: AdminVideo) => void
  onTranslated: () => void
  onMerged: () => void
}) {
  const { t } = useAppSettings()
  const thumbnail = localUrl(video.thumbnailUrl)
  // Bilibili uploads are not on the board yet, so they cannot be featured.
  const bilibili = video.source === "bilibili"
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
        ) : bilibili ? (
          <Badge tone="sky" icon={Tv}>
            {t("admin.videos.source.bilibili")}
          </Badge>
        ) : null}
      </div>
      <div className="adm-video-body">
        <strong>{video.title}</strong>
        <VideoWhen video={video} />
        <VideoMarks video={video} />
        <MergeLine video={video} onChanged={onMerged} onMerge={onEdit} />
        <TranslationCell
          owner="video"
          id={video.id}
          original={video.title}
          translation={video.translation}
          onChanged={onTranslated}
        />
        <div className="adm-actions">
          <Button
            size="sm"
            variant="ghost"
            icon={Pencil}
            onClick={() => onEdit(video)}
          >
            {t("admin.videos.edit")}
          </Button>
          {featured ? (
            <Button
              size="sm"
              variant="ghost"
              icon={StarOff}
              onClick={() => onFeature(null)}
            >
              {t("admin.videos.unfeature")}
            </Button>
          ) : bilibili ? null : (
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
  const [source, setSource] = useState<"" | "youtube" | "bilibili">("")
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState<string | null>(null)
  // A merge or undo changes two cards and the suggestions, so all reload.
  const [merges, setMerges] = useState(0)
  const videos = useResource(
    () => api.videos({ status, source, page, pageSize: 24 }),
    [status, source, page, merges],
  )
  const merged = () => setMerges((value) => value + 1)
  const data = videos.data
  // A saved correction replaces its card, and the pinned copy, in place.
  const replace = (video: AdminVideo) =>
    videos.setData(
      (current) =>
        current && {
          ...current,
          items: current.items.map((item) =>
            item.id === video.id ? video : item,
          ),
          featuredVideo:
            current.featuredVideo?.id === video.id
              ? video
              : current.featuredVideo,
        },
    )
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
      <MergeSuggestions reloadKey={merges} onMerged={merged} />
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
        <Segmented
          label={t("admin.videos.source")}
          value={source}
          onChange={(value) => {
            setSource(value)
            setPage(1)
          }}
          options={[
            { value: "", label: t("admin.videos.all") },
            { value: "youtube", label: t("admin.videos.source.youtube") },
            { value: "bilibili", label: t("admin.videos.source.bilibili") },
          ]}
        />
        <TranslationBacklog
          owner="video"
          onQueued={() => void videos.reload()}
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
                onEdit={(chosen) => setEditing(chosen.id)}
                onTranslated={() => void videos.reload()}
                onMerged={merged}
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
      {editing ? (
        <VideoEditor
          videoId={editing}
          onMerged={merged}
          onClose={(changed) => {
            setEditing(null)
            if (changed) replace(changed)
          }}
        />
      ) : null}
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
        actions={
          <Button
            size="sm"
            variant="ghost"
            icon={Download}
            onClick={() => navigate("sources")}
          >
            {t("admin.fetch.openSources")}
          </Button>
        }
      />
      <Panel>
        <VideoList />
      </Panel>
    </div>
  )
}
