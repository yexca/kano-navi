import {
  useEffect,
  useId,
  useState,
  type ReactNode,
  type SubmitEvent,
} from "react"
import {
  CornerDownLeft,
  ExternalLink,
  History,
  ImageOff,
  PencilLine,
  RefreshCw,
  RotateCcw,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { formatDateTime } from "@/admin/format"
import type {
  AdminVideo,
  VideoCorrection,
  VideoHistory,
  VideoThumbnail,
} from "@/admin/types"
import { Badge, Button, Facts, Loading, Modal } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { MergeSection } from "@/admin/views/merge-parts"

/** The kinds the board labels; an empty choice follows the platform. */
export const videoKinds = ["VIDEO", "MUSIC", "LIVE ARCHIVE", "GAMEPLAY", "TALK"]

/** A format choice: empty follows the feed, otherwise Short or not. */
const formatChoice = (isShort: boolean | null) =>
  isShort === null ? "" : isShort ? "short" : "regular"

const formatLabel = (isShort: boolean | null) =>
  isShort === null
    ? "admin.videos.format.unknown"
    : isShort
      ? "admin.videos.format.short"
      : "admin.videos.format.regular"

const pinKey = (file: { mediaId: string; sha256: string } | null) =>
  file ? `${file.mediaId}/${file.sha256}` : ""

/** A video's length as h:mm:ss or m:ss; null while unknown. */
export function formatLength(seconds: number | null) {
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

/** A correction's state: marked and resettable while it is set. */
function CorrectionMark({
  corrected,
  onReset,
}: {
  corrected: boolean
  onReset: () => void
}) {
  const { t } = useAppSettings()
  if (!corrected) return null
  return (
    <>
      <Badge tone="honey">{t("admin.videos.corrected")}</Badge>
      <button type="button" className="adm-correction-reset" onClick={onReset}>
        <RotateCcw aria-hidden />
        {t("admin.videos.resetField")}
      </button>
    </>
  )
}

/**
 * A corrected field. The reset sits beside its label rather than inside it,
 * so the label names only the control.
 */
function CorrectionField({
  id,
  label,
  hint,
  corrected,
  onReset,
  children,
}: {
  id: string
  label: string
  hint?: string
  corrected: boolean
  onReset: () => void
  children: ReactNode
}) {
  return (
    <div className="adm-field">
      <div className="adm-correction-head">
        <label className="adm-field-label" htmlFor={id}>
          {label}
        </label>
        <CorrectionMark corrected={corrected} onReset={onReset} />
      </div>
      {children}
      {hint ? <span className="adm-field-hint">{hint}</span> : null}
    </div>
  )
}

/**
 * Corrections an operator layers over a video's platform title, kind and
 * thumbnail, with what the platform replaced, a refresh of its page and the
 * merge of a Bilibili upload with its YouTube copy.
 */
export function VideoEditor({
  videoId,
  onClose,
  onMerged,
}: {
  videoId: string
  onClose: (changed?: AdminVideo) => void
  /** A merge or undo changed this video and another one. */
  onMerged?: () => void
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
    format: string
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
        format: formatChoice(data.video.manualIsShort),
      })
  }, [data, draft])

  const save = async (submit: SubmitEvent<HTMLFormElement>) => {
    submit.preventDefault()
    if (!draft || !data) return
    const file = data.thumbnails.find(
      (item) => pinKey(item) === draft.thumbnail,
    )
    const correction: VideoCorrection = {
      title: draft.title.trim() || null,
      kind: draft.kind || null,
      thumbnail: file ? { mediaId: file.mediaId, sha256: file.sha256 } : null,
      isShort: draft.format ? draft.format === "short" : null,
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
  // The preview follows the thumbnail choice before it is saved.
  const chosen = draft?.thumbnail
    ? data?.thumbnails.find((item) => pinKey(item) === draft.thumbnail)?.url
    : (current?.url ?? data?.video.thumbnailUrl)
  const preview = chosen ?? null
  const fieldId = useId()
  // The header previews the board's row as the draft would leave it.
  const boardKind = draft?.kind || video?.sourceKind || "—"
  const boardShort = draft?.format
    ? draft.format === "short"
    : Boolean(video?.sourceIsShort)
  const corrected = draft
    ? [draft.title.trim(), draft.kind, draft.thumbnail, draft.format].filter(
        Boolean,
      ).length
    : 0
  return (
    <Modal
      open
      wide
      className="adm-video-editor"
      title={t("admin.videos.editTitle")}
      onClose={close}
      footer={
        video ? (
          <>
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
            <Button
              type="submit"
              form="adm-video-form"
              variant="primary"
              busy={busy === "save"}
              disabled={!draft}
            >
              {t("admin.action.save")}
            </Button>
          </>
        ) : null
      }
    >
      {!data || !video || !draft ? (
        <Loading />
      ) : (
        <form
          id="adm-video-form"
          className="adm-video-editor-form"
          onSubmit={save}
        >
          <section
            className="adm-video-editor-id"
            aria-label={t("admin.videos.boardPreview")}
          >
            <span className="adm-video-thumb">
              {preview ? (
                <img src={preview} alt="" />
              ) : (
                <ImageOff aria-hidden />
              )}
              {formatLength(video.durationSeconds) ? (
                <span className="adm-video-length">
                  {formatLength(video.durationSeconds)}
                </span>
              ) : null}
            </span>
            <div className="adm-video-editor-id-body">
              <div className="adm-chip-row">
                <Badge tone={bilibili ? "sky" : "berry"}>
                  {t(`admin.videos.source.${video.source}`)}
                </Badge>
                <Badge>{boardKind}</Badge>
                {boardShort ? (
                  <Badge tone="lavender">
                    {t("admin.videos.format.short")}
                  </Badge>
                ) : null}
                {corrected ? (
                  <Badge tone="honey" icon={PencilLine}>
                    {t("admin.videos.correctedCount", { count: corrected })}
                  </Badge>
                ) : null}
              </div>
              <strong className="adm-video-editor-id-title">
                {draft.title.trim() || video.sourceTitle}
              </strong>
              {draft.title.trim() ? (
                <small className="adm-video-editor-id-source">
                  {t("admin.videos.platformTitle")} · {video.sourceTitle}
                </small>
              ) : null}
              <Facts
                items={[
                  [
                    t("admin.videos.lastChecked"),
                    video.checkedAt
                      ? formatDateTime(video.checkedAt, locale)
                      : t("admin.videos.neverChecked"),
                  ],
                  [
                    t("admin.videos.refreshState"),
                    video.refreshRequestedAt
                      ? t("admin.videos.refreshPending", {
                          time: formatDateTime(
                            video.refreshRequestedAt,
                            locale,
                          ),
                        })
                      : bilibili
                        ? t("admin.videos.refreshBilibili")
                        : t("admin.videos.refreshIdle"),
                  ],
                ]}
              />
            </div>
          </section>
          <div className="adm-video-editor-columns">
            <section className="adm-video-editor-fields adm-form">
              <h3 className="adm-video-editor-heading">
                <PencilLine aria-hidden />
                {t("admin.videos.corrections")}
              </h3>
              <p className="adm-muted">{t("admin.videos.correctionHint")}</p>
              <CorrectionField
                id={`${fieldId}-title`}
                label={t("admin.videos.field.title")}
                corrected={Boolean(draft.title.trim())}
                onReset={() => setDraft({ ...draft, title: "" })}
                hint={t("admin.videos.field.titleHint")}
              >
                <input
                  id={`${fieldId}-title`}
                  value={draft.title}
                  placeholder={video.sourceTitle}
                  maxLength={300}
                  onChange={(e) =>
                    setDraft({ ...draft, title: e.target.value })
                  }
                />
              </CorrectionField>
              <div className="adm-form-row is-top">
                <CorrectionField
                  id={`${fieldId}-kind`}
                  label={t("admin.videos.field.kind")}
                  corrected={Boolean(draft.kind)}
                  onReset={() => setDraft({ ...draft, kind: "" })}
                >
                  <select
                    id={`${fieldId}-kind`}
                    value={draft.kind}
                    onChange={(e) =>
                      setDraft({ ...draft, kind: e.target.value })
                    }
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
                </CorrectionField>
                {video.source === "youtube" ? (
                  <CorrectionField
                    id={`${fieldId}-format`}
                    label={t("admin.videos.field.format")}
                    corrected={Boolean(draft.format)}
                    onReset={() => setDraft({ ...draft, format: "" })}
                    hint={t("admin.videos.format.hint")}
                  >
                    <select
                      id={`${fieldId}-format`}
                      value={draft.format}
                      onChange={(e) =>
                        setDraft({ ...draft, format: e.target.value })
                      }
                    >
                      <option value="">
                        {t("admin.videos.format.platform", {
                          format: t(formatLabel(video.sourceIsShort)),
                        })}
                      </option>
                      <option value="short">
                        {t("admin.videos.format.short")}
                      </option>
                      <option value="regular">
                        {t("admin.videos.format.regular")}
                      </option>
                    </select>
                  </CorrectionField>
                ) : null}
              </div>
              <fieldset className="adm-field adm-thumb-picker">
                <legend className="adm-correction-head">
                  <span className="adm-field-label">
                    {t("admin.videos.field.thumbnail")}
                  </span>
                  <CorrectionMark
                    corrected={Boolean(draft.thumbnail)}
                    onReset={() => setDraft({ ...draft, thumbnail: "" })}
                  />
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
                      onSelect={(thumbnail) =>
                        setDraft({ ...draft, thumbnail })
                      }
                    />
                  ))}
                </div>
                <span className="adm-field-hint">
                  {t("admin.videos.thumbnail.hint")}
                </span>
              </fieldset>
            </section>
            <aside className="adm-video-editor-side">
              <MergeSection
                video={video}
                onChanged={() => {
                  void history.reload()
                  onMerged?.()
                }}
              />
              <section className="adm-video-history">
                <h3 className="adm-video-editor-heading">
                  <History aria-hidden />
                  {t("admin.videos.history")}
                  {data.revisions.length ? (
                    <em>{data.revisions.length}</em>
                  ) : null}
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
                          {revision.title !== video.sourceTitle ? (
                            <Button
                              size="sm"
                              variant="ghost"
                              icon={CornerDownLeft}
                              onClick={() =>
                                setDraft({ ...draft, title: revision.title })
                              }
                            >
                              {t("admin.videos.useTitle")}
                            </Button>
                          ) : null}
                        </span>
                      </li>
                    ))}
                  </ol>
                ) : (
                  <p className="adm-muted">{t("admin.videos.historyEmpty")}</p>
                )}
              </section>
            </aside>
          </div>
        </form>
      )}
    </Modal>
  )
}
