import { useEffect, useState } from "react"
import {
  Clapperboard,
  ImageDown,
  ImageOff,
  ListMusic,
  RefreshCw,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { japanStamp } from "@/admin/format"
import type {
  WorksCoverStatus,
  WorksCoverVideo,
  WorksRecentItem,
  WorksRecentReport,
} from "@/admin/types"
import { Badge, Button, PageHeader, Panel, type Tone } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"

/** The counts of the covers card, in the order they are shown. */
const coverCounts = ["covered", "due", "waiting", "uncovered"] as const

const coverTones: Record<string, Tone> = {
  due: "sky",
  waiting: "honey",
  uncovered: "danger",
  gone: "neutral",
}

/** Where an operator opens a video to check it by hand. */
function videoPage(video: WorksCoverVideo) {
  switch (video.platform) {
    case "youtube":
      return `https://www.youtube.com/watch?v=${video.id}`
    case "niconico":
      return `https://www.nicovideo.jp/watch/${video.id}`
    case "bilibili":
      return `https://www.bilibili.com/video/${video.id}`
  }
}

/**
 * The song titles of the packaged uploads, read the first time the list
 * opens; until then an upload is named by its ID.
 */
function useUploadSongs(open: boolean) {
  const [songs, setSongs] = useState<Map<string, string>>()
  useEffect(() => {
    if (!open || songs) return
    let live = true
    import("@/works/uploads.json")
      .then((module) => {
        if (live)
          setSongs(
            new Map(module.default.map((upload) => [upload.id, upload.song])),
          )
      })
      .catch(() => {})
    return () => {
      live = false
    }
  }, [open, songs])
  return songs
}

/**
 * The Works page's upload covers: how many uploads have one, which still
 * lack one and why, and a media step that looks the due ones up and
 * downloads them. The status is read from the snapshot when the page opens
 * and after every change; only the media step reads the platforms.
 */
function WorksCoversPanel({
  status,
  loading,
  onCheck,
}: {
  status: WorksCoverStatus | undefined
  loading: boolean
  onCheck: () => Promise<unknown>
}) {
  const { t } = useAppSettings()
  const { start, overview } = useAdmin()
  const [busy, setBusy] = useState(false)
  const [checking, setChecking] = useState(false)
  const [open, setOpen] = useState(false)
  const songs = useUploadSongs(open)
  const active = overview?.activeRun
  const running =
    active?.kind === "workflow" &&
    active.steps.length === 1 &&
    active.steps[0] === "media"
  const uploads = status?.uploads ?? []
  const check = async () => {
    setChecking(true)
    await onCheck()
    setChecking(false)
  }
  const fill = async () => {
    setBusy(true)
    await start({ kind: "workflow", steps: ["media"] })
    setBusy(false)
  }
  return (
    <Panel
      icon={Clapperboard}
      title={t("admin.site.worksCovers")}
      actions={
        <>
          <Button
            size="sm"
            variant="ghost"
            icon={RefreshCw}
            busy={checking || (loading && !status)}
            onClick={() => void check()}
          >
            {t("admin.site.worksCoversRefresh")}
          </Button>
          <Button
            size="sm"
            variant="primary"
            icon={ImageDown}
            busy={busy || running}
            disabled={
              !status || status.skipped || !status.due || Boolean(active)
            }
            onClick={() => void fill()}
          >
            {running
              ? t("admin.site.worksCoversRunning")
              : t("admin.site.worksCoversFill")}
          </Button>
        </>
      }
    >
      <ul className="adm-meters" aria-busy={!status}>
        {coverCounts.map((name) => {
          const value = status?.[name]
          const share = status?.total ? (value ?? 0) / status.total : 0
          return (
            <li
              key={name}
              className={`adm-meter${status ? "" : " is-pending"}`}
            >
              <span className="adm-meter-label">
                {t(`admin.site.worksCoversCount.${name}`)}
              </span>
              <span className="adm-meter-value">
                <strong>{value ?? "—"}</strong>
                {status ? (
                  <small>
                    {t("admin.site.worksCoversOf", { count: status.total })}
                  </small>
                ) : null}
              </span>
              <span className="adm-meter-bar" aria-hidden>
                <i style={{ width: `${share * 100}%` }} />
              </span>
            </li>
          )
        })}
      </ul>
      {uploads.length ? (
        <details
          className="adm-disclosure"
          onToggle={(event) => setOpen(event.currentTarget.open)}
        >
          <summary>
            {t("admin.site.worksCoversList", { count: uploads.length })}
          </summary>
          <ol className="adm-works-covers">
            {uploads.map((upload) => (
              <li key={upload.id}>
                <div className="adm-chip-row">
                  <time dateTime={upload.date}>{upload.date}</time>
                  <strong>{songs?.get(upload.id) ?? upload.id}</strong>
                  <Badge tone={coverTones[upload.state]}>
                    {t(`admin.site.worksCoversState.${upload.state}`)}
                  </Badge>
                </div>
                <ul>
                  {upload.videos.map((video) => (
                    <li
                      key={`${video.platform}:${video.id}`}
                      className="adm-chip-row"
                    >
                      <span className="adm-muted">
                        {t(
                          `admin.site.worksCoversPlatform.${video.members ? "members" : video.platform}`,
                        )}
                      </span>
                      <a
                        href={videoPage(video)}
                        target="_blank"
                        rel="noreferrer"
                      >
                        <code>{video.id}</code>
                      </a>
                      <Badge tone={coverTones[video.state]}>
                        {t(`admin.site.worksCoversVideo.${video.state}`)}
                      </Badge>
                      {video.code ? <code>{video.code}</code> : null}
                      {video.retryAt ? (
                        <span className="adm-muted">
                          {t("admin.site.worksCoversRetry", {
                            time: japanStamp(video.retryAt),
                          })}
                        </span>
                      ) : null}
                    </li>
                  ))}
                </ul>
              </li>
            ))}
          </ol>
        </details>
      ) : null}
      <p className="adm-field-hint">
        {status
          ? t("admin.site.worksCoversChecked", {
              time: japanStamp(status.checkedAt),
            })
          : loading
            ? t("admin.loading")
            : null}{" "}
        {status?.skipped
          ? t("admin.site.worksCoversSkipped")
          : t("admin.site.worksCoversBudget")}
      </p>
    </Panel>
  )
}

const recentTones: Record<string, Tone> = {
  auto: "sky",
  operator: "leaf",
  excluded: "neutral",
}

/**
 * The Works page's uploads after the collection date: the videos it lists,
 * by their tag, form and format (auto) or by the operator's decision, and
 * those the operator keeps off. Exclude takes an automatic one off; Follow
 * the rules clears a decision. A video the rules miss is included from its
 * video editor.
 */
function WorksRecentPanel({
  report,
  loading,
  onChange,
}: {
  report: WorksRecentReport | undefined
  loading: boolean
  onChange: () => Promise<unknown>
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [busy, setBusy] = useState("")
  const decide = async (item: WorksRecentItem, work: boolean | null) => {
    setBusy(item.id)
    const result = await act(
      () => api.correctVideo(item.id, { work }),
      "admin.notice.worksDecisionSaved",
    )
    if (result) await onChange()
    setBusy("")
  }
  const items = report?.items ?? []
  return (
    <Panel
      icon={ListMusic}
      title={t("admin.site.worksRecent", {
        date: report?.updatedAt ?? "—",
      })}
    >
      {items.length ? (
        <ol className="adm-works-covers adm-works-recent">
          {items.map((item) => {
            const state = item.origin ?? "excluded"
            return (
              <li key={item.id}>
                <span className="adm-merge-video">
                  <span className="adm-video-thumb">
                    {item.thumbnailUrl ? (
                      <img src={item.thumbnailUrl} alt="" loading="lazy" />
                    ) : (
                      <ImageOff aria-hidden />
                    )}
                  </span>
                  <span className="adm-merge-video-body">
                    <a
                      className="adm-link"
                      href={item.url}
                      target="_blank"
                      rel="noreferrer"
                    >
                      <strong>{item.title}</strong>
                    </a>
                    <small>
                      {[
                        japanStamp(item.scheduledAt || item.publishedAt),
                        t(`admin.site.worksCoversPlatform.${item.source}`),
                        item.tag ?? t("admin.videos.tag.none"),
                        item.kind ?? "—",
                      ].join(" · ")}
                    </small>
                  </span>
                </span>
                <div className="adm-chip-row">
                  <Badge tone={recentTones[state]}>
                    {t(`admin.site.worksRecentOrigin.${state}`)}
                  </Badge>
                  {item.work === null ? (
                    <Button
                      size="sm"
                      variant="ghost"
                      busy={busy === item.id}
                      disabled={Boolean(busy)}
                      onClick={() => void decide(item, false)}
                    >
                      {t("admin.site.worksRecentExclude")}
                    </Button>
                  ) : (
                    <Button
                      size="sm"
                      variant="ghost"
                      busy={busy === item.id}
                      disabled={Boolean(busy)}
                      onClick={() => void decide(item, null)}
                    >
                      {t("admin.site.worksRecentClear")}
                    </Button>
                  )}
                </div>
              </li>
            )
          })}
        </ol>
      ) : (
        <p className="adm-field-hint">
          {report
            ? t("admin.site.worksRecentEmpty")
            : loading
              ? t("admin.loading")
              : null}
        </p>
      )}
      {report && report.total > items.length ? (
        <p className="adm-field-hint">
          {t("admin.site.worksRecentLimit", {
            count: items.length,
            total: report.total,
          })}
        </p>
      ) : null}
    </Panel>
  )
}

/** The Works page: the upload covers and the recent uploads the public
 * Works page lists. */
export function WorksView() {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const covers = useResource(() => api.worksCovers(), [])
  const recent = useResource(() => api.worksRecent(), [])
  return (
    <div className="adm-view">
      <PageHeader title={t("admin.works.title")} />
      <WorksCoversPanel
        status={covers.data}
        loading={covers.loading}
        onCheck={covers.reload}
      />
      <WorksRecentPanel
        report={recent.data}
        loading={recent.loading}
        onChange={recent.reload}
      />
    </div>
  )
}
