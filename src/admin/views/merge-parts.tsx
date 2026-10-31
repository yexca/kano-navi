import { useState } from "react"
import {
  CheckCircle2,
  Combine,
  ExternalLink,
  ImageOff,
  Unlink,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { formatDateTime } from "@/admin/format"
import type {
  AdminVideo,
  VideoMergeCandidate,
  VideoMergeSuggestion,
} from "@/admin/types"
import {
  Badge,
  Button,
  ConfirmButton,
  Empty,
  Facts,
  Field,
  Loading,
  type Tone,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"

/** Only local, already cached media is shown; never a remote CDN URL. */
function localUrl(value: string | null | undefined) {
  return value && value.startsWith("/") ? value : null
}

const bvPattern = /BV[0-9A-Za-z]{10}/

/** Undoing a merge keeps the upload separate; the rule never redoes it. */
function UndoMerge({
  uploadId,
  onChanged,
}: {
  uploadId: string
  onChanged: (video: AdminVideo) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  return (
    <ConfirmButton
      size="sm"
      variant="ghost"
      icon={Unlink}
      onConfirm={async () => {
        const result = await act(
          () => api.unmergeVideo(uploadId),
          "admin.notice.videoUnmerged",
        )
        if (result) onChanged(result.video)
      }}
    >
      {t("admin.videos.merge.undo")}
    </ConfirmButton>
  )
}

/**
 * A card's merge line: the uploads merged into a YouTube video, or the video
 * a Bilibili upload is merged into, with the undo. An unmerged upload offers
 * the merge dialog.
 */
export function MergeLine({
  video,
  onChanged,
  onMerge,
}: {
  video: AdminVideo
  onChanged: () => void
  onMerge: (video: AdminVideo) => void
}) {
  const { t } = useAppSettings()
  if (video.source !== "bilibili")
    return video.alternates.length ? (
      <div className="adm-video-merge">
        {video.alternates.map((alternate) => (
          <span key={alternate.id} className="adm-video-merge-item">
            <Badge tone="lavender" icon={Combine}>
              {t("admin.videos.merge.alsoOn", {
                source: t(`admin.videos.source.${alternate.source}`),
              })}
            </Badge>
            <a
              className="adm-link"
              href={alternate.url}
              target="_blank"
              rel="noreferrer"
            >
              <span>{alternate.title}</span>
              <ExternalLink aria-hidden />
            </a>
            <UndoMerge uploadId={alternate.id} onChanged={onChanged} />
          </span>
        ))}
      </div>
    ) : null
  if (video.mergedInto)
    return (
      <div className="adm-video-merge">
        <span className="adm-video-merge-item">
          <Badge tone="lavender" icon={Combine} className="adm-merge-badge">
            <span title={video.mergeReason || undefined}>
              {t("admin.videos.merge.into", {
                title: video.mergedIntoTitle || video.mergedInto,
              })}
            </span>
          </Badge>
          <small>
            {video.mergeSource === "rule"
              ? t("admin.videos.merge.byRule")
              : t("admin.videos.merge.byOperator")}
          </small>
          <UndoMerge uploadId={video.id} onChanged={onChanged} />
        </span>
      </div>
    )
  return (
    <div className="adm-video-merge">
      <span className="adm-video-merge-item">
        {video.mergeSource === "manual" ? (
          <Badge icon={Unlink}>{t("admin.videos.merge.separate")}</Badge>
        ) : null}
        <Button
          size="sm"
          variant="ghost"
          icon={Combine}
          onClick={() => onMerge(video)}
        >
          {t("admin.videos.merge.open")}
        </Button>
      </span>
    </div>
  )
}

function MiniVideo({ video, detail }: { video: AdminVideo; detail?: string }) {
  const { locale } = useAppSettings()
  const thumbnail = localUrl(video.thumbnailUrl)
  return (
    <span className="adm-merge-video">
      <span className="adm-video-thumb">
        {thumbnail ? (
          <img src={thumbnail} alt="" loading="lazy" />
        ) : (
          <ImageOff aria-hidden />
        )}
      </span>
      <span className="adm-merge-video-body">
        <a
          className="adm-link"
          href={video.url}
          target="_blank"
          rel="noreferrer"
        >
          <strong>{video.title}</strong>
        </a>
        <small>
          {formatDateTime(video.scheduledAt || video.publishedAt, locale)}
          {detail ? ` · ${detail}` : ""}
        </small>
      </span>
    </span>
  )
}

/** How a candidate compares with the upload, in a few words. */
function useCandidateDetail() {
  const { t } = useAppSettings()
  return (candidate: VideoMergeCandidate) =>
    [
      t("admin.videos.merge.gap", { count: candidate.gapMinutes }),
      candidate.durationDelta === null
        ? t("admin.videos.merge.lengthUnknown")
        : candidate.durationDelta === 0
          ? t("admin.videos.merge.sameLength")
          : t("admin.videos.merge.lengthDelta", {
              count: candidate.durationDelta,
            }),
      candidate.titleMatch ? null : t("admin.videos.merge.titleDiffers"),
    ]
      .filter(Boolean)
      .join(" · ")
}

function Candidates({
  uploadId,
  candidates,
  onMerged,
}: {
  uploadId: string
  candidates: VideoMergeCandidate[]
  onMerged: (video: AdminVideo) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const detail = useCandidateDetail()
  const [busy, setBusy] = useState("")
  const merge = async (into: string) => {
    setBusy(into)
    const result = await act(
      () => api.mergeVideo(uploadId, into),
      "admin.notice.videoMerged",
    )
    setBusy("")
    if (result) onMerged(result.video)
  }
  return (
    <ul className="adm-merge-candidates">
      {candidates.map((candidate) => (
        <li key={candidate.youtube.id}>
          <MiniVideo video={candidate.youtube} detail={detail(candidate)} />
          <Button
            size="sm"
            icon={Combine}
            busy={busy === candidate.youtube.id}
            disabled={Boolean(busy)}
            onClick={() => merge(candidate.youtube.id)}
          >
            {t("admin.videos.merge.action")}
          </Button>
        </li>
      ))}
    </ul>
  )
}

const statusTone: Partial<Record<VideoMergeSuggestion["status"], Tone>> = {
  rule: "leaf",
  ambiguous: "honey",
  unverified: "sky",
}

/** Keeping an upload apart is durable: the rule never merges it. */
function KeepSeparate({
  uploadId,
  onChanged,
}: {
  uploadId: string
  onChanged: () => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [busy, setBusy] = useState(false)
  return (
    <Button
      size="sm"
      variant="ghost"
      icon={Unlink}
      busy={busy}
      onClick={async () => {
        setBusy(true)
        const result = await act(
          () => api.keepVideoSeparate(uploadId),
          "admin.notice.videoSeparate",
        )
        setBusy(false)
        if (result) onChanged()
      }}
    >
      {t("admin.videos.merge.keepSeparate")}
    </Button>
  )
}

/**
 * The Review queue's merge decisions: uploads the matching rule merges at
 * its next run, and those it leaves to the operator because several videos
 * match or a match is incomplete. Each merges into a candidate or stays
 * apart; either decision removes it and reads the overview's count again.
 */
export function MergeQueue() {
  const { t } = useAppSettings()
  const { api, refreshOverview } = useAdmin()
  const suggestions = useResource(() => api.videoMerges(), [])
  if (!suggestions.data) return <Loading />
  const items: VideoMergeSuggestion[] = suggestions.data.items
  const decided = () => {
    void suggestions.reload()
    void refreshOverview().catch(() => {})
  }
  if (!items.length)
    return <Empty icon={CheckCircle2} title={t("admin.review.noMerges")} />
  return (
    <div className="adm-merge-queue">
      <p className="adm-note">{t("admin.review.mergeHint")}</p>
      <ol>
        {items.map((item) => (
          <li key={item.bilibili.id} className="adm-merge-row">
            <div className="adm-merge-upload">
              <MiniVideo video={item.bilibili} />
              <div className="adm-chip-row">
                <Badge tone={statusTone[item.status] ?? "neutral"}>
                  {t(`admin.videos.merge.status.${item.status}`)}
                </Badge>
                <KeepSeparate uploadId={item.bilibili.id} onChanged={decided} />
              </div>
            </div>
            <Candidates
              uploadId={item.bilibili.id}
              candidates={item.candidates}
              onMerged={decided}
            />
          </li>
        ))}
      </ol>
    </div>
  )
}

/** A text field whose Enter merges instead of submitting the dialog form. */
function MergeInput({
  label,
  placeholder,
  busy,
  valid = (value) => value.trim() !== "",
  onMerge,
}: {
  label: string
  placeholder: string
  busy: boolean
  valid?: (value: string) => boolean
  onMerge: (value: string) => Promise<boolean>
}) {
  const { t } = useAppSettings()
  const [value, setValue] = useState("")
  const submit = async () => {
    if (valid(value) && (await onMerge(value.trim()))) setValue("")
  }
  return (
    <div className="adm-bilibili-add">
      <Field label={label} hint={t("admin.videos.merge.inputHint")}>
        <input
          value={value}
          placeholder={placeholder}
          maxLength={300}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key !== "Enter") return
            e.preventDefault()
            void submit()
          }}
        />
      </Field>
      <Button
        icon={Combine}
        busy={busy}
        disabled={!valid(value)}
        onClick={submit}
      >
        {t("admin.videos.merge.action")}
      </Button>
    </div>
  )
}

/**
 * The editor's merge section. A YouTube video lists its merged uploads and
 * takes one by BV ID; a Bilibili upload shows its target, or its candidates
 * and a field for any YouTube video.
 */
export function MergeSection({
  video,
  onChanged,
}: {
  video: AdminVideo
  onChanged: () => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const bilibili = video.source === "bilibili"
  const candidates = useResource(
    () =>
      bilibili && !video.mergedInto
        ? api.uploadMerges(video.id)
        : Promise.resolve({ items: [] as VideoMergeSuggestion[] }),
    [video.id, video.mergedInto, bilibili],
  )
  const [busy, setBusy] = useState(false)
  const merge = async (upload: string, into: string) => {
    setBusy(true)
    const result = await act(
      () => api.mergeVideo(upload, into),
      "admin.notice.videoMerged",
    )
    setBusy(false)
    if (result) onChanged()
    return Boolean(result)
  }
  const suggestion = candidates.data?.items[0]
  return (
    <section className="adm-video-merge-section">
      <h3>
        <Combine aria-hidden />
        {t("admin.videos.merge.title")}
      </h3>
      {!bilibili ? (
        <>
          {video.alternates.length ? (
            <ul className="adm-merge-candidates">
              {video.alternates.map((alternate) => (
                <li key={alternate.id}>
                  <span className="adm-merge-video-body">
                    <a
                      className="adm-link"
                      href={alternate.url}
                      target="_blank"
                      rel="noreferrer"
                    >
                      <strong>{alternate.title}</strong>
                      <ExternalLink aria-hidden />
                    </a>
                    <small>
                      {t(`admin.videos.source.${alternate.source}`)} ·{" "}
                      {alternate.mergeSource === "rule"
                        ? t("admin.videos.merge.byRule")
                        : t("admin.videos.merge.byOperator")}
                    </small>
                  </span>
                  <UndoMerge uploadId={alternate.id} onChanged={onChanged} />
                </li>
              ))}
            </ul>
          ) : (
            <>
              <p className="adm-muted">{t("admin.videos.merge.noneYoutube")}</p>
              <MergeInput
                label={t("admin.videos.merge.bilibiliInput")}
                placeholder="BV1xxxxxxxxx"
                busy={busy}
                valid={(value) => bvPattern.test(value)}
                onMerge={(value) => merge(value.match(bvPattern)![0], video.id)}
              />
            </>
          )}
        </>
      ) : video.mergedInto ? (
        <>
          <Facts
            items={[
              [
                t("admin.videos.merge.mergedInto"),
                video.mergedIntoTitle || video.mergedInto,
              ],
              [
                t("admin.videos.merge.decidedBy"),
                video.mergeSource === "rule"
                  ? t("admin.videos.merge.byRule")
                  : t("admin.videos.merge.byOperator"),
              ],
              [
                t("admin.videos.merge.since"),
                formatDateTime(video.mergedAt, locale),
              ],
            ]}
          />
          <div className="adm-actions">
            <UndoMerge uploadId={video.id} onChanged={onChanged} />
          </div>
        </>
      ) : (
        <>
          {video.mergeSource === "manual" ? (
            <p className="adm-note">{t("admin.videos.merge.separateHint")}</p>
          ) : null}
          <strong className="adm-field-label">
            {t("admin.videos.merge.candidates")}
          </strong>
          {suggestion?.candidates.length ? (
            <Candidates
              uploadId={video.id}
              candidates={suggestion.candidates}
              onMerged={onChanged}
            />
          ) : (
            <p className="adm-muted">{t("admin.videos.merge.noCandidates")}</p>
          )}
          <MergeInput
            label={t("admin.videos.merge.youtubeInput")}
            placeholder="https://www.youtube.com/watch?v=…"
            busy={busy}
            onMerge={(value) => merge(video.id, value)}
          />
        </>
      )}
    </section>
  )
}
