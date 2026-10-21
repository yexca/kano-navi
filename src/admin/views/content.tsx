import { useRef, useState } from "react"
import {
  Check,
  Download,
  ExternalLink,
  FileText,
  ImageOff,
  Link2,
  Search,
  Star,
  StarOff,
  Upload,
  UserRound,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatDateTime } from "@/admin/format"
import type { AdminVideo, ProfileMedia, ProfileMediaState } from "@/admin/types"
import {
  Badge,
  Button,
  Empty,
  Field,
  Loading,
  PageHeader,
  Pager,
  Panel,
  Segmented,
  StatusBadge,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { PostRow } from "@/admin/views/review"

const tabs = ["posts", "videos", "profile"] as const
type Tab = (typeof tabs)[number]

function PostsTab() {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const [filters, setFilters] = useState({
    search: "",
    source: "",
    llm: "",
    media: "",
  })
  const [search, setSearch] = useState("")
  const [page, setPage] = useState(1)
  const posts = useResource(
    () => api.posts({ ...filters, page, pageSize: 30 }),
    [filters, page],
  )
  const update = (patch: Partial<typeof filters>) => {
    setFilters((current) => ({ ...current, ...patch }))
    setPage(1)
  }
  return (
    <>
      <form
        className="adm-filters"
        onSubmit={(event) => {
          event.preventDefault()
          update({ search })
        }}
      >
        <label className="adm-search">
          <Search aria-hidden />
          <input
            type="search"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t("admin.posts.search")}
            aria-label={t("admin.posts.search")}
          />
        </label>
        <select
          value={filters.source}
          onChange={(e) => update({ source: e.target.value })}
          aria-label={t("admin.posts.source")}
        >
          <option value="">{t("admin.posts.anySource")}</option>
          <option value="x">{t("admin.step.x")}</option>
          <option value="youtube">{t("admin.step.youtube")}</option>
        </select>
        <select
          value={filters.llm}
          onChange={(e) => update({ llm: e.target.value })}
          aria-label={t("admin.posts.llm")}
        >
          <option value="">{t("admin.posts.anyLlm")}</option>
          {[
            "never",
            "pending",
            "processed",
            "uncertain",
            "failed",
            "skipped",
          ].map((status) => (
            <option key={status} value={status}>
              {t(`admin.status.${status}`)}
            </option>
          ))}
        </select>
        <select
          value={filters.media}
          onChange={(e) => update({ media: e.target.value })}
          aria-label={t("admin.posts.media")}
        >
          <option value="">{t("admin.posts.anyMedia")}</option>
          <option value="present">{t("admin.posts.withMedia")}</option>
          <option value="missing">{t("admin.posts.withoutMedia")}</option>
        </select>
      </form>
      {!posts.data ? (
        <Loading />
      ) : posts.data.items.length ? (
        <>
          <div className="adm-post-list">
            {posts.data.items.map((post) => (
              <PostRow
                key={post.id}
                post={post}
                onDone={() => void posts.reload()}
              />
            ))}
          </div>
          <Pager
            page={posts.data.page}
            totalPages={posts.data.totalPages}
            total={posts.data.total}
            onPage={setPage}
          />
        </>
      ) : (
        <Empty icon={FileText} title={t("admin.posts.empty")} />
      )}
    </>
  )
}

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

function VideosTab() {
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

function Candidate({
  item,
  onState,
}: {
  item: ProfileMedia
  onState: (state: ProfileMediaState) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const [busy, setBusy] = useState("")
  const run = async (action: "download" | "select") => {
    setBusy(action)
    const state = await act(
      () =>
        action === "download"
          ? api.downloadProfileMedia(item.id)
          : api.selectProfileMedia(item.id),
      action === "download"
        ? "admin.notice.mediaDownloaded"
        : "admin.notice.mediaSelected",
    )
    setBusy("")
    if (state) onState(state)
  }
  const ready = item.status === "ready"
  return (
    <article className={`adm-candidate${item.isActive ? " is-active" : ""}`}>
      <div className={`adm-candidate-preview is-${item.slot}`}>
        {item.previewUrl ? (
          <img src={item.previewUrl} alt="" loading="lazy" />
        ) : (
          <ImageOff aria-hidden />
        )}
      </div>
      <div className="adm-candidate-body">
        <span className="adm-chip-row">
          <Badge>{t(`admin.profile.source.${item.source}`)}</Badge>
          {item.isActive ? (
            <Badge tone="leaf" icon={Check}>
              {t("admin.profile.active")}
            </Badge>
          ) : (
            <StatusBadge
              status={
                ready
                  ? "completed"
                  : item.status === "failed"
                    ? "failed"
                    : "pending"
              }
            />
          )}
        </span>
        <small>
          {item.width && item.height ? `${item.width}×${item.height} · ` : ""}
          {formatDateTime(item.updatedAt, locale)}
        </small>
        {item.lastError ? (
          <small className="adm-error-text">{item.lastError}</small>
        ) : null}
        <div className="adm-actions">
          {item.source !== "upload" ? (
            <Button
              size="sm"
              variant="ghost"
              icon={Download}
              busy={busy === "download"}
              onClick={() => run("download")}
            >
              {ready ? t("admin.profile.refresh") : t("admin.profile.download")}
            </Button>
          ) : null}
          {!item.isActive ? (
            <Button
              size="sm"
              icon={Check}
              disabled={!ready}
              busy={busy === "select"}
              onClick={() => run("select")}
            >
              {t("admin.profile.use")}
            </Button>
          ) : null}
        </div>
      </div>
    </article>
  )
}

function SlotPanel({
  slot,
  state,
  onState,
}: {
  slot: "avatar" | "banner"
  state: ProfileMediaState
  onState: (state: ProfileMediaState) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [url, setUrl] = useState("")
  const [source, setSource] = useState<"x" | "youtube">("x")
  const [busy, setBusy] = useState("")
  const file = useRef<HTMLInputElement>(null)
  const items = state.items.filter((item) => item.slot === slot)
  const active = state.active[slot]
  const perform = async (
    key: string,
    work: () => Promise<ProfileMediaState>,
    notice: string,
  ) => {
    setBusy(key)
    const next = await act(work, notice)
    setBusy("")
    if (next) onState({ items: next.items, active: next.active })
    return next
  }
  return (
    <Panel
      icon={UserRound}
      title={t(`admin.profile.${slot}`)}
      description={t(`admin.profile.${slot}Hint`)}
    >
      <div className={`adm-profile-current is-${slot}`}>
        {active?.previewUrl ? (
          <img src={active.previewUrl} alt="" />
        ) : (
          <span>{t("admin.profile.noneActive")}</span>
        )}
      </div>
      <div className="adm-actions">
        <select
          value={source}
          onChange={(e) => setSource(e.target.value as "x" | "youtube")}
          aria-label={t("admin.profile.discoverFrom")}
        >
          <option value="x">{t("admin.profile.source.x")}</option>
          <option value="youtube">{t("admin.profile.source.youtube")}</option>
        </select>
        <Button
          size="sm"
          icon={Search}
          busy={busy === "discover"}
          onClick={() =>
            perform(
              "discover",
              () => api.discoverProfileMedia(slot, source),
              "admin.notice.mediaDiscovered",
            )
          }
        >
          {t("admin.profile.discover")}
        </Button>
        <input
          ref={file}
          type="file"
          accept="image/png,image/jpeg,image/webp,image/gif"
          hidden
          onChange={(event) => {
            const chosen = event.target.files?.[0]
            event.target.value = ""
            if (chosen)
              void perform(
                "upload",
                () => api.uploadProfileMedia(slot, chosen),
                "admin.notice.mediaUploaded",
              )
          }}
        />
        <Button
          size="sm"
          variant="ghost"
          icon={Upload}
          busy={busy === "upload"}
          onClick={() => file.current?.click()}
        >
          {t("admin.profile.upload")}
        </Button>
      </div>
      <form
        className="adm-inline-form"
        onSubmit={async (event) => {
          event.preventDefault()
          if (
            await perform(
              "add",
              () => api.addProfileMedia(slot, source, url),
              "admin.notice.mediaAdded",
            )
          )
            setUrl("")
        }}
      >
        <Field
          label={t("admin.profile.addUrl")}
          hint={t("admin.profile.addUrlHint")}
        >
          <input
            type="url"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://"
            required
          />
        </Field>
        <Button type="submit" size="sm" icon={Link2} busy={busy === "add"}>
          {t("admin.profile.add")}
        </Button>
      </form>
      {items.length ? (
        <div className="adm-candidate-grid">
          {items.map((item) => (
            <Candidate key={item.id} item={item} onState={onState} />
          ))}
        </div>
      ) : (
        <Empty title={t("admin.profile.noCandidates")} />
      )}
    </Panel>
  )
}

function ProfileTab() {
  const { api } = useAdmin()
  const media = useResource(() => api.profileMedia(), [])
  if (!media.data) return <Loading />
  return (
    <div className="adm-grid is-two">
      {(["avatar", "banner"] as const).map((slot) => (
        <SlotPanel
          key={slot}
          slot={slot}
          state={media.data}
          onState={media.setData}
        />
      ))}
    </div>
  )
}

export function ContentView({ tab, navigate }: ViewProps) {
  const { t } = useAppSettings()
  const active: Tab = (tabs as readonly string[]).includes(tab)
    ? (tab as Tab)
    : "posts"
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.content.title")}
        description={t("admin.content.description")}
      />
      <div
        className="adm-tabs is-bare"
        role="tablist"
        aria-label={t("admin.content.title")}
      >
        {tabs.map((id) => (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={active === id}
            className={active === id ? "is-active" : undefined}
            onClick={() => navigate("content", id)}
          >
            {t(`admin.content.tab.${id}`)}
          </button>
        ))}
      </div>
      {active === "posts" ? (
        <Panel>
          <PostsTab />
        </Panel>
      ) : null}
      {active === "videos" ? (
        <Panel>
          <VideosTab />
        </Panel>
      ) : null}
      {active === "profile" ? <ProfileTab /> : null}
    </div>
  )
}
