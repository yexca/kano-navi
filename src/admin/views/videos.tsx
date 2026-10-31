import { useState } from "react"
import {
  Clock3,
  Download,
  ExternalLink,
  History,
  ImageOff,
  Pencil,
  Search,
  Sparkles,
  Star,
  StarOff,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatDateTime } from "@/admin/format"
import type { AdminVideo } from "@/admin/types"
import {
  Badge,
  Button,
  Empty,
  IconButton,
  Loading,
  PageHeader,
  Pager,
  Panel,
  Segmented,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { MergeLine } from "@/admin/views/merge-parts"
import {
  TranslationBacklog,
  TranslationCell,
} from "@/admin/views/translation-parts"
import { stepIcons } from "@/admin/views/run-parts"
import { formatLength, VideoEditor } from "@/admin/views/video-editor"

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
  onEdit,
}: {
  video: AdminVideo | null
  onClear: () => Promise<void>
  onEdit: (video: AdminVideo) => void
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
          icon={Pencil}
          onClick={() => onEdit(video)}
        >
          {t("admin.videos.edit")}
        </Button>
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
function VideoMarks({
  video,
  featured,
}: {
  video: AdminVideo
  featured: boolean
}) {
  const { t } = useAppSettings()
  const corrected =
    video.manualTitle !== null ||
    video.manualKind !== null ||
    video.manualThumbnail !== null ||
    video.manualIsShort !== null
  if (!featured && !corrected && !video.revisions && !video.refreshRequestedAt)
    return null
  return (
    <span className="adm-chip-row">
      {featured ? (
        <Badge tone="berry" icon={Star}>
          {t("admin.videos.featured")}
        </Badge>
      ) : null}
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

/** The platform, when, kind, length and format on one line. */
function VideoMeta({ video }: { video: AdminVideo }) {
  const { t, locale } = useAppSettings()
  const PlatformIcon = stepIcons[video.source]
  const length = formatLength(video.durationSeconds)
  return (
    <span className="adm-video-meta">
      {PlatformIcon ? (
        <span
          className="adm-video-platform"
          title={t(`admin.videos.source.${video.source}`)}
        >
          <PlatformIcon aria-hidden />
          <span className="adm-visually-hidden">
            {t(`admin.videos.source.${video.source}`)}
          </span>
        </span>
      ) : null}
      <time>
        {video.isUpcoming
          ? t("admin.videos.scheduled", {
              time: formatDateTime(video.scheduledAt, locale),
            })
          : formatDateTime(video.publishedAt, locale)}
      </time>
      {video.kind ? <span>{video.kind}</span> : null}
      {length ? <span>{length}</span> : null}
      {video.isShort ? <span>{t("admin.videos.format.short")}</span> : null}
    </span>
  )
}

function VideoRow({
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
    <li
      className={`adm-video-row${featured ? " is-featured" : ""}${video.mergedInto ? " is-merged" : ""}`}
    >
      {/* A pointer shortcut to the dialog; the title is the keyboard path. */}
      <span
        className="adm-video-thumb"
        aria-hidden
        onClick={() => onEdit(video)}
      >
        {thumbnail ? (
          <img src={thumbnail} alt="" loading="lazy" />
        ) : (
          <ImageOff />
        )}
      </span>
      <div className="adm-video-main">
        <button
          type="button"
          className="adm-video-title"
          onClick={() => onEdit(video)}
          title={
            video.manualTitle !== null && video.manualTitle !== undefined
              ? video.sourceTitle
              : undefined
          }
        >
          {video.title}
        </button>
        <VideoMeta video={video} />
        <VideoMarks video={video} featured={featured} />
        <MergeLine video={video} onChanged={onMerged} onMerge={onEdit} />
      </div>
      <div className="adm-video-translation">
        <TranslationCell
          owner="video"
          id={video.id}
          original={video.title}
          translation={video.translation}
          onChanged={onTranslated}
        />
      </div>
      <div className="adm-row-actions adm-video-row-actions">
        {featured ? (
          <IconButton
            icon={StarOff}
            label={t("admin.videos.unfeature")}
            className="is-on"
            onClick={() => onFeature(null)}
          />
        ) : bilibili ? null : (
          <IconButton
            icon={Star}
            label={t("admin.videos.feature")}
            onClick={() => onFeature(video)}
          />
        )}
        <IconButton
          icon={Pencil}
          label={t("admin.videos.edit")}
          onClick={() => onEdit(video)}
        />
        <a
          className="adm-icon-button"
          href={video.url}
          target="_blank"
          rel="noreferrer"
          aria-label={t("admin.videos.open")}
          title={t("admin.videos.open")}
        >
          <ExternalLink aria-hidden />
        </a>
      </div>
    </li>
  )
}

type VideoView = "all" | "upcoming" | "bilibili" | "translation"

/**
 * Each tab is an operator task. Uploads merged into another video show on
 * that video, so only the Bilibili tab lists them as rows of their own.
 */
const viewFilters: Record<VideoView, Record<string, string>> = {
  all: { merge: "visible" },
  upcoming: { status: "upcoming", merge: "visible" },
  bilibili: { source: "bilibili" },
  translation: { translation: "incomplete" },
}

function VideoList() {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [view, setView] = useState<VideoView>("all")
  const [search, setSearch] = useState("")
  const [query, setQuery] = useState("")
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState<string | null>(null)
  // A merge or undo changes two rows, so the page reloads.
  const [merges, setMerges] = useState(0)
  const videos = useResource(
    () =>
      api.videos({
        ...viewFilters[view],
        search: query,
        page,
        pageSize: 30,
      }),
    [view, query, page, merges],
  )
  const submitSearch = () => {
    if (search === query) return
    setQuery(search)
    setPage(1)
  }
  const merged = () => setMerges((value) => value + 1)
  const data = videos.data
  // A saved correction replaces its row, and the pinned copy, in place.
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
          onEdit={(video) => setEditing(video.id)}
        />
      ) : null}
      <Panel>
        <div className="adm-list-tools">
          <Segmented
            label={t("admin.videos.view")}
            value={view}
            options={(
              ["all", "upcoming", "bilibili", "translation"] as const
            ).map((value) => ({
              value,
              label: t(`admin.videos.view.${value}`),
            }))}
            onChange={(value) => {
              setView(value)
              setPage(1)
            }}
          />
          <form
            className="adm-search"
            role="search"
            onSubmit={(event) => {
              event.preventDefault()
              submitSearch()
            }}
          >
            <Search aria-hidden />
            <input
              type="search"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              onBlur={submitSearch}
              placeholder={t("admin.videos.search")}
              aria-label={t("admin.videos.search")}
            />
          </form>
        </div>
        <div className="adm-list-bar">
          <span className="adm-muted">
            {data ? t("admin.pager.total", { total: data.total }) : null}
          </span>
          <TranslationBacklog
            owner="video"
            onQueued={() => void videos.reload()}
          />
        </div>
        {!data ? (
          <Loading />
        ) : data.items.length ? (
          <>
            <ul className="adm-video-list">
              {data.items.map((video) => (
                <VideoRow
                  key={video.id}
                  video={video}
                  featured={video.id === data.featuredVideoId}
                  onFeature={feature}
                  onEdit={(chosen) => setEditing(chosen.id)}
                  onTranslated={() => void videos.reload()}
                  onMerged={merged}
                />
              ))}
            </ul>
            <Pager
              page={data.page}
              totalPages={data.totalPages}
              total={data.total}
              onPage={setPage}
            />
          </>
        ) : (
          <Empty
            icon={ImageOff}
            title={
              query || view !== "all"
                ? t("admin.videos.noMatch")
                : t("admin.videos.empty")
            }
          />
        )}
      </Panel>
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
      <VideoList />
    </div>
  )
}
