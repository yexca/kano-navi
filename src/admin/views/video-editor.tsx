import {
  useEffect,
  useId,
  useState,
  type ReactNode,
  type SubmitEvent,
} from "react"
import {
  Check,
  CornerDownLeft,
  ExternalLink,
  History,
  ImageOff,
  PencilLine,
  RefreshCw,
  RotateCcw,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { japanStamp } from "@/admin/format"
import type {
  AdminVideo,
  VideoCorrection,
  VideoHistory,
  VideoThumbnail,
} from "@/admin/types"
import { Badge, Button, Loading, Modal } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { useForcedRead } from "@/admin/views/bilibili-parts"
import { MergeSection } from "@/admin/views/merge-parts"
import type { VideoAvailability } from "@/types"

/** The forms an operator may set; an empty choice follows the platform. */
export const videoKinds = ["stream", "premiere", "upload"]
/** A Bilibili upload is an upload or a live replay; it has no premieres. */
const bilibiliKinds = ["stream", "upload"]

/** The content tags; an empty choice follows the schedule or inference. */
export const videoTags = ["game", "talk", "song"]

/** Who can watch a video, as an operator may set it over the checks. */
const videoAvailabilities: VideoAvailability[] = [
  "public",
  "unlisted",
  "members",
  "private",
  "removed",
]

/** A Works choice: empty follows the rules, otherwise include or exclude. */
const workChoice = (work: boolean | null) =>
  work === null ? "" : work ? "include" : "exclude"

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
        {checked ? (
          <span className="adm-thumb-check" aria-hidden>
            <Check />
          </span>
        ) : null}
      </span>
      <small title={label}>{label}</small>
    </label>
  )
}

/** The reset to the platform value, shown while a correction is set. */
function CorrectionReset({
  corrected,
  onReset,
}: {
  corrected: boolean
  onReset: () => void
}) {
  const { t } = useAppSettings()
  if (!corrected) return null
  return (
    <button
      type="button"
      className="adm-correction-reset"
      aria-label={t("admin.videos.resetField")}
      title={t("admin.videos.resetField")}
      onClick={onReset}
    >
      <RotateCcw aria-hidden />
    </button>
  )
}

/**
 * A corrected field: its control is tinted and the reset sits beside its
 * label rather than inside it, so the label names only the control.
 */
function CorrectionField({
  id,
  label,
  corrected,
  onReset,
  children,
}: {
  id: string
  label: string
  corrected: boolean
  onReset: () => void
  children: ReactNode
}) {
  return (
    <div className={`adm-field${corrected ? " is-corrected" : ""}`}>
      <div className="adm-correction-head">
        <label className="adm-field-label" htmlFor={id}>
          {label}
        </label>
        <CorrectionReset corrected={corrected} onReset={onReset} />
      </div>
      {children}
    </div>
  )
}

/**
 * Corrections an operator layers over a video's platform title, kind,
 * content tag and thumbnail, with what the platform replaced, a refresh of its page and the
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
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const history = useResource<VideoHistory>(
    () => api.videoHistory(videoId),
    [videoId],
  )
  const [draft, setDraft] = useState<{
    title: string
    kind: string
    tag: string
    thumbnail: string
    format: string
    availability: string
    work: string
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
        tag: data.video.manualTag ?? "",
        thumbnail: pinKey(data.video.manualThumbnail),
        format: formatChoice(data.video.manualIsShort),
        availability: data.video.manualAvailability ?? "",
        work: workChoice(data.video.manualWork),
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
      tag: draft.tag || null,
      thumbnail: file ? { mediaId: file.mediaId, sha256: file.sha256 } : null,
      isShort: draft.format ? draft.format === "short" : null,
      availability: (draft.availability || null) as VideoAvailability | null,
      work: draft.work ? draft.work === "include" : null,
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
  // A Bilibili read the captcha backoff refused can be sent once anyway.
  const forced = useForcedRead()
  const refresh = async () => {
    setBusy("refresh")
    await forced.read(
      (force) => api.refreshVideo(videoId, force),
      bilibili ? "admin.notice.bilibiliRead" : "admin.notice.videoRefresh",
      (result) => {
        setRefreshed(result.video)
        if (bilibili) void history.reload()
        else
          history.setData(
            (current) => current && { ...current, video: result.video },
          )
      },
    )
    setBusy("")
  }

  const video = data?.video
  const thumbnailLabel = (item: VideoThumbnail) =>
    item.current
      ? t("admin.videos.thumbnail.current")
      : t("admin.videos.thumbnail.earlier", {
          time: japanStamp(item.fetchedAt),
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
  // Without a video tag, the schedule entry's label and then the title
  // inference's decide, as on the board.
  const autoTag = video?.scheduleTag ?? video?.inferredTag ?? null
  const boardTag = draft?.tag || autoTag
  const boardShort = draft?.format
    ? draft.format === "short"
    : Boolean(video?.sourceIsShort)
  const corrected = draft
    ? [
        draft.title.trim(),
        draft.kind,
        draft.tag,
        draft.thumbnail,
        draft.format,
        draft.availability,
        draft.work,
      ].filter(Boolean).length
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
                variant="ghost"
                icon={RefreshCw}
                busy={busy === "refresh"}
                onClick={refresh}
              >
                {bilibili
                  ? t("admin.videos.refreshBilibiliButton")
                  : t("admin.videos.refresh")}
              </Button>
            ) : null}
            {bilibili ? forced.offer : null}
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
          <div className="adm-video-editor-columns">
            <div className="adm-video-editor-main">
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
                  <strong className="adm-video-editor-id-title">
                    {draft.title.trim() || video.sourceTitle}
                  </strong>
                  <div className="adm-chip-row">
                    <Badge tone={bilibili ? "sky" : "berry"}>
                      {t(`admin.videos.source.${video.source}`)}
                    </Badge>
                    <Badge>{boardKind}</Badge>
                    {boardTag ? <Badge tone="leaf">{boardTag}</Badge> : null}
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
                  <small className="adm-video-editor-id-meta">
                    {t("admin.videos.lastChecked")}{" "}
                    {video.checkedAt
                      ? japanStamp(video.checkedAt)
                      : t("admin.videos.neverChecked")}
                    {video.refreshRequestedAt
                      ? ` · ${t("admin.videos.refreshPending", {
                          time: japanStamp(video.refreshRequestedAt),
                        })}`
                      : null}
                  </small>
                </div>
              </section>
              <section className="adm-video-editor-fields adm-form">
                <CorrectionField
                  id={`${fieldId}-title`}
                  label={t("admin.videos.field.title")}
                  corrected={Boolean(draft.title.trim())}
                  onReset={() => setDraft({ ...draft, title: "" })}
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
                <div className="adm-form-row">
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
                      {(video.source === "bilibili"
                        ? bilibiliKinds
                        : videoKinds
                      ).map((kind) => (
                        <option key={kind} value={kind}>
                          {kind}
                        </option>
                      ))}
                    </select>
                  </CorrectionField>
                  <CorrectionField
                    id={`${fieldId}-tag`}
                    label={t("admin.videos.field.tag")}
                    corrected={Boolean(draft.tag)}
                    onReset={() => setDraft({ ...draft, tag: "" })}
                  >
                    <select
                      id={`${fieldId}-tag`}
                      value={draft.tag}
                      onChange={(e) =>
                        setDraft({ ...draft, tag: e.target.value })
                      }
                    >
                      <option value="">
                        {t("admin.videos.tag.auto", {
                          tag: autoTag || t("admin.videos.tag.none"),
                        })}
                      </option>
                      {videoTags.map((tag) => (
                        <option key={tag} value={tag}>
                          {tag}
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
                  <CorrectionField
                    id={`${fieldId}-availability`}
                    label={t("admin.videos.field.availability")}
                    corrected={Boolean(draft.availability)}
                    onReset={() => setDraft({ ...draft, availability: "" })}
                  >
                    <select
                      id={`${fieldId}-availability`}
                      value={draft.availability}
                      onChange={(e) =>
                        setDraft({ ...draft, availability: e.target.value })
                      }
                    >
                      <option value="">
                        {t("admin.videos.availability.checked", {
                          availability: t(
                            `admin.videos.availability.${video.sourceAvailability ?? "unknown"}`,
                          ),
                        })}
                      </option>
                      {videoAvailabilities.map((value) => (
                        <option key={value} value={value}>
                          {t(`admin.videos.availability.${value}`)}
                        </option>
                      ))}
                    </select>
                  </CorrectionField>
                  {/* Only videos after the Works collection date are listed. */}
                  <CorrectionField
                    id={`${fieldId}-work`}
                    label={t("admin.videos.field.work")}
                    corrected={Boolean(draft.work)}
                    onReset={() => setDraft({ ...draft, work: "" })}
                  >
                    <select
                      id={`${fieldId}-work`}
                      value={draft.work}
                      onChange={(e) =>
                        setDraft({ ...draft, work: e.target.value })
                      }
                    >
                      <option value="">{t("admin.videos.work.auto")}</option>
                      <option value="include">
                        {t("admin.videos.work.include")}
                      </option>
                      <option value="exclude">
                        {t("admin.videos.work.exclude")}
                      </option>
                    </select>
                  </CorrectionField>
                </div>
                {/* With no stored file there is nothing to pin. */}
                {data.thumbnails.length ? (
                  <fieldset className="adm-field adm-thumb-picker">
                    <legend className="adm-correction-head">
                      <span className="adm-field-label">
                        {t("admin.videos.field.thumbnail")}
                      </span>
                      <CorrectionReset
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
                        onSelect={(thumbnail) =>
                          setDraft({ ...draft, thumbnail })
                        }
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
                  </fieldset>
                ) : null}
              </section>
            </div>
            <aside className="adm-video-editor-side">
              <MergeSection
                video={video}
                onChanged={() => {
                  void history.reload()
                  onMerged?.()
                }}
              />
              {data.revisions.length ? (
                <section className="adm-video-history">
                  <h3 className="adm-video-editor-heading">
                    <History aria-hidden />
                    {t("admin.videos.history")}
                    <em>{data.revisions.length}</em>
                  </h3>
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
                          <strong title={revision.title}>
                            {revision.title}
                          </strong>
                          <small>
                            {t(`admin.videos.revision.${revision.origin}`, {
                              time: japanStamp(revision.replacedAt),
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
                </section>
              ) : null}
            </aside>
          </div>
        </form>
      )}
    </Modal>
  )
}
