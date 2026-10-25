import { useEffect, useState } from "react"
import { ExternalLink, History, ImageOff, RefreshCw } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { formatDateTime } from "@/admin/format"
import type {
  AdminVideo,
  VideoCorrection,
  VideoHistory,
  VideoThumbnail,
} from "@/admin/types"
import { Button, Facts, Field, Loading, Modal } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"

/** The kinds the board labels; an empty choice follows the platform. */
const videoKinds = ["VIDEO", "MUSIC", "LIVE ARCHIVE", "GAMEPLAY", "TALK"]

const pinKey = (file: { mediaId: string; sha256: string } | null) =>
  file ? `${file.mediaId}/${file.sha256}` : ""

function duration(seconds: number | null) {
  if (!seconds) return null
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = String(seconds % 60).padStart(2, "0")
  return h ? `${h}:${String(m).padStart(2, "0")}:${s}` : `${m}:${s}`
}

function ThumbnailChoice({
  file,
  url,
  label,
  checked,
  onSelect,
}: {
  file: string
  url: string | null
  label: string
  checked: boolean
  onSelect: (file: string) => void
}) {
  return (
    <label className={`adm-thumb-choice${checked ? " is-selected" : ""}`}>
      <input
        type="radio"
        name="video-thumbnail"
        value={file}
        checked={checked}
        onChange={() => onSelect(file)}
      />
      <span className="adm-video-thumb">
        {url ? (
          <img src={url} alt="" loading="lazy" />
        ) : (
          <ImageOff aria-hidden />
        )}
      </span>
      <small>{label}</small>
    </label>
  )
}

/**
 * Corrections an operator layers over a video's platform title, kind and
 * thumbnail, with what the platform replaced and a refresh of its page.
 */
export function VideoEditor({
  videoId,
  onClose,
}: {
  videoId: string
  onClose: (changed?: AdminVideo) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const history = useResource<VideoHistory>(
    () => api.videoHistory(videoId),
    [videoId],
  )
  const [draft, setDraft] = useState<{
    title: string
    kind: string
    thumbnail: string
  } | null>(null)
  const [busy, setBusy] = useState<"" | "save" | "refresh">("")
  // A refresh request changes the listed row even when nothing is saved.
  const [refreshed, setRefreshed] = useState<AdminVideo | undefined>()
  const close = () => onClose(refreshed)
  const data = history.data
  // A background reload keeps what the operator has typed.
  useEffect(() => {
    if (data && !draft)
      setDraft({
        title: data.video.manualTitle ?? "",
        kind: data.video.manualKind ?? "",
        thumbnail: pinKey(data.video.manualThumbnail),
      })
  }, [data, draft])

  const save = async (submit) => {
    submit.preventDefault()
    if (!draft || !data) return
    const file = data.thumbnails.find(
      (item) => pinKey(item) === draft.thumbnail,
    )
    const correction: VideoCorrection = {
      title: draft.title.trim() || null,
      kind: draft.kind || null,
      thumbnail: file ? { mediaId: file.mediaId, sha256: file.sha256 } : null,
    }
    setBusy("save")
    const result = await act(
      () => api.correctVideo(videoId, correction),
      "admin.notice.videoSaved",
    )
    setBusy("")
    if (result) onClose(result.video)
  }
  // A YouTube video waits for the next YouTube step; a Bilibili upload is
  // read at once, so its history may already have changed.
  const bilibili = data?.video.source === "bilibili"
  const refresh = async () => {
    setBusy("refresh")
    const result = await act(
      () => api.refreshVideo(videoId),
      bilibili ? "admin.notice.bilibiliRead" : "admin.notice.videoRefresh",
    )
    setBusy("")
    if (!result) return
    setRefreshed(result.video)
    if (bilibili) void history.reload()
    else
      history.setData(
        (current) => current && { ...current, video: result.video },
      )
  }

  const video = data?.video
  const thumbnailLabel = (item: VideoThumbnail) =>
    item.current
      ? t("admin.videos.thumbnail.current")
      : t("admin.videos.thumbnail.earlier", {
          time: formatDateTime(item.fetchedAt, locale),
        })
  const current = data?.thumbnails.find((item) => item.current)
  return (
    <Modal open wide title={t("admin.videos.editTitle")} onClose={close}>
      {!data || !video || !draft ? (
        <Loading />
      ) : (
        <form className="adm-form" onSubmit={save}>
          <p className="adm-note">{t("admin.videos.correctionHint")}</p>
          <Facts
            items={[
              [t("admin.videos.platformTitle"), video.sourceTitle],
              [
                t("admin.videos.lastChecked"),
                video.checkedAt
                  ? formatDateTime(video.checkedAt, locale)
                  : t("admin.videos.neverChecked"),
              ],
              [t("admin.videos.duration"), duration(video.durationSeconds)],
              [
                t("admin.videos.refreshState"),
                video.refreshRequestedAt
                  ? t("admin.videos.refreshPending", {
                      time: formatDateTime(video.refreshRequestedAt, locale),
                    })
                  : bilibili
                    ? t("admin.videos.refreshBilibili")
                    : t("admin.videos.refreshIdle"),
              ],
            ]}
          />
          <Field
            label={t("admin.videos.field.title")}
            hint={t("admin.videos.field.titleHint")}
          >
            <input
              value={draft.title}
              placeholder={video.sourceTitle}
              maxLength={300}
              onChange={(e) => setDraft({ ...draft, title: e.target.value })}
            />
          </Field>
          <Field label={t("admin.videos.field.kind")}>
            <select
              value={draft.kind}
              onChange={(e) => setDraft({ ...draft, kind: e.target.value })}
            >
              <option value="">
                {t("admin.videos.kind.platform", {
                  kind: video.sourceKind || "—",
                })}
              </option>
              {videoKinds.map((kind) => (
                <option key={kind} value={kind}>
                  {kind}
                </option>
              ))}
            </select>
          </Field>
          <fieldset className="adm-field adm-thumb-picker">
            <legend className="adm-field-label">
              {t("admin.videos.field.thumbnail")}
            </legend>
            <div className="adm-thumb-choices">
              <ThumbnailChoice
                file=""
                url={current?.url ?? null}
                label={t("admin.videos.thumbnail.platform")}
                checked={draft.thumbnail === ""}
                onSelect={(thumbnail) => setDraft({ ...draft, thumbnail })}
              />
              {data.thumbnails.map((item) => (
                <ThumbnailChoice
                  key={pinKey(item)}
                  file={pinKey(item)}
                  url={item.url}
                  label={thumbnailLabel(item)}
                  checked={draft.thumbnail === pinKey(item)}
                  onSelect={(thumbnail) => setDraft({ ...draft, thumbnail })}
                />
              ))}
            </div>
            <span className="adm-field-hint">
              {t("admin.videos.thumbnail.hint")}
            </span>
          </fieldset>
          <section className="adm-video-history">
            <h3>
              <History aria-hidden />
              {t("admin.videos.history")}
            </h3>
            {data.revisions.length ? (
              <ol>
                {data.revisions.map((revision) => (
                  <li key={revision.id}>
                    <span className="adm-video-thumb">
                      {revision.thumbnailUrl ? (
                        <img
                          src={revision.thumbnailUrl}
                          alt=""
                          loading="lazy"
                        />
                      ) : (
                        <ImageOff aria-hidden />
                      )}
                    </span>
                    <span className="adm-video-history-body">
                      <strong>{revision.title}</strong>
                      <small>
                        {t(`admin.videos.revision.${revision.origin}`, {
                          time: formatDateTime(revision.replacedAt, locale),
                        })}
                      </small>
                    </span>
                    {revision.title !== video.sourceTitle ? (
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() =>
                          setDraft({ ...draft, title: revision.title })
                        }
                      >
                        {t("admin.videos.useTitle")}
                      </Button>
                    ) : null}
                  </li>
                ))}
              </ol>
            ) : (
              <p className="adm-muted">{t("admin.videos.historyEmpty")}</p>
            )}
          </section>
          <div className="adm-form-actions adm-video-editor-actions">
            {video.source === "youtube" || bilibili ? (
              <Button
                icon={RefreshCw}
                busy={busy === "refresh"}
                onClick={refresh}
              >
                {bilibili
                  ? t("admin.videos.refreshBilibiliButton")
                  : t("admin.videos.refresh")}
              </Button>
            ) : null}
            <a
              className="adm-link"
              href={video.url}
              target="_blank"
              rel="noreferrer"
            >
              {t("admin.videos.open")}
              <ExternalLink aria-hidden />
            </a>
            <span className="adm-spacer" />
            <Button onClick={close}>{t("admin.action.cancel")}</Button>
            <Button type="submit" variant="primary" busy={busy === "save"}>
              {t("admin.action.save")}
            </Button>
          </div>
        </form>
      )}
    </Modal>
  )
}
