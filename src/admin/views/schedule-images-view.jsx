import { useState } from "react"
import {
  Check,
  ExternalLink,
  Image as ImageIcon,
  RefreshCw,
  X,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import {
  Btn,
  Card,
  EmptyState,
  PageHeader,
  Tag,
} from "@/admin/components/primitives"

const statusTone = {
  schedule: "leaf",
  not_schedule: "danger",
  uncertain: "honey",
  pending: "neutral",
  running: "sky",
  failed: "danger",
  skipped: "neutral",
  unreviewed: "neutral",
}

// Skip reasons written by the verifier; model-written reasons stay verbatim.
const reasonCodes = new Set([
  "source_not_board",
  "media_pending",
  "missing_api_key",
  "no_compatible_provider",
])

export function ScheduleImagesView({ data }) {
  const { t } = useAppSettings()
  const {
    scheduleAssets = [],
    startJob,
    activeJob,
    reviewScheduleAsset,
    fail,
  } = data
  const [reasons, setReasons] = useState({})
  const [busyId, setBusyId] = useState(null)

  const review = async (asset, status) => {
    const reason = String(reasons[asset.id] || "").trim()
    if (status !== "unreviewed" && !reason) return
    setBusyId(`${asset.id}:${status}`)
    try {
      await reviewScheduleAsset(asset.id, status, reason)
    } catch (error) {
      fail(error)
    } finally {
      setBusyId(null)
    }
  }

  return (
    <section className="adm-view" aria-labelledby="adm-schedule-images-title">
      <PageHeader
        id="adm-schedule-images-title"
        title={t("admin.scheduleImages.title")}
        description={t("admin.scheduleImages.description")}
        actions={
          <Btn
            variant="outline"
            icon={RefreshCw}
            disabled={Boolean(activeJob)}
            onClick={() => startJob("/scan/automatic")}
          >
            {t("admin.scheduleImages.runVerification")}
          </Btn>
        }
      />
      <Card className="adm-schedule-assets-card">
        <div className="adm-table-meta">
          <span>
            {t("admin.scheduleImages.total", { count: scheduleAssets.length })}
          </span>
          <span>{t("admin.scheduleImages.approvalHint")}</span>
        </div>
        {scheduleAssets.length ? (
          <ul className="adm-schedule-assets-list">
            {scheduleAssets.map((asset) => {
              const llmLabel = t(
                `admin.scheduleImages.status.${asset.llmStatus}`,
              )
              const manualLabel = t(
                `admin.scheduleImages.manual.${asset.manualStatus}`,
              )
              const effectiveLabel = t(
                `admin.scheduleImages.status.${asset.effectiveStatus}`,
              )
              const hasReason = Boolean(String(reasons[asset.id] || "").trim())
              return (
                <li key={asset.id} className="adm-schedule-asset-row">
                  <div className="adm-schedule-asset-preview">
                    {asset.previewUrl ? (
                      <img
                        src={asset.previewUrl}
                        alt={asset.alt || t("admin.scheduleImages.previewAlt")}
                        loading="lazy"
                      />
                    ) : (
                      <ImageIcon aria-hidden="true" />
                    )}
                  </div>
                  <div className="adm-schedule-asset-copy">
                    <div className="adm-schedule-asset-heading">
                      <strong>{asset.weekStart || asset.id}</strong>
                      <Tag
                        tone={statusTone[asset.effectiveStatus] || "neutral"}
                      >
                        {t("admin.scheduleImages.effective", {
                          status: effectiveLabel,
                        })}
                      </Tag>
                      <Tag tone={asset.approved ? "leaf" : "neutral"}>
                        {t(
                          asset.approved
                            ? "admin.scheduleImages.public"
                            : "admin.scheduleImages.hidden",
                        )}
                      </Tag>
                    </div>
                    {!asset.sourceMatchesBoard ? (
                      <p className="adm-schedule-asset-reason">
                        {t("admin.scheduleImages.sourceNotBoard")}
                      </p>
                    ) : null}
                    <p>
                      {t("admin.scheduleImages.llmLabel", {
                        status: llmLabel,
                        confidence:
                          asset.llmConfidence == null
                            ? "—"
                            : `${Math.round(asset.llmConfidence * 100)}%`,
                      })}
                    </p>
                    <p>
                      {t("admin.scheduleImages.manualLabel", {
                        status: manualLabel,
                      })}
                    </p>
                    {asset.llmReason ? (
                      <p className="adm-schedule-asset-reason">
                        {reasonCodes.has(asset.llmReason)
                          ? t(`admin.scheduleImages.reason.${asset.llmReason}`)
                          : asset.llmReason}
                      </p>
                    ) : null}
                    {asset.llmEvidence ? (
                      <p className="adm-schedule-asset-evidence">
                        {asset.llmEvidence}
                      </p>
                    ) : null}
                    <div className="adm-schedule-asset-links">
                      {asset.sourceUrl ? (
                        <a
                          href={asset.sourceUrl}
                          target="_blank"
                          rel="noreferrer noopener"
                        >
                          {t("admin.scheduleImages.openSource")}
                          <ExternalLink aria-hidden="true" />
                        </a>
                      ) : null}
                      {asset.llmModel ? <code>{asset.llmModel}</code> : null}
                    </div>
                    <label className="adm-schedule-asset-reason-field">
                      <span>{t("admin.scheduleImages.manualReason")}</span>
                      <input
                        value={reasons[asset.id] || ""}
                        onChange={(event) =>
                          setReasons((current) => ({
                            ...current,
                            [asset.id]: event.target.value,
                          }))
                        }
                        placeholder={t(
                          "admin.scheduleImages.manualReasonPlaceholder",
                        )}
                      />
                    </label>
                    <div className="adm-schedule-asset-actions">
                      <Btn
                        size="sm"
                        icon={Check}
                        busy={busyId === `${asset.id}:schedule`}
                        disabled={Boolean(activeJob) || !hasReason}
                        onClick={() => review(asset, "schedule")}
                      >
                        {t("admin.scheduleImages.markSchedule")}
                      </Btn>
                      <Btn
                        variant="outline"
                        size="sm"
                        icon={X}
                        busy={busyId === `${asset.id}:not_schedule`}
                        disabled={Boolean(activeJob) || !hasReason}
                        onClick={() => review(asset, "not_schedule")}
                      >
                        {t("admin.scheduleImages.markNotSchedule")}
                      </Btn>
                      {asset.manualStatus !== "unreviewed" ? (
                        <Btn
                          variant="ghost"
                          size="sm"
                          disabled={Boolean(activeJob)}
                          onClick={() => review(asset, "unreviewed")}
                        >
                          {t("admin.scheduleImages.clearManual")}
                        </Btn>
                      ) : null}
                    </div>
                  </div>
                </li>
              )
            })}
          </ul>
        ) : (
          <EmptyState
            icon={ImageIcon}
            title={t("admin.scheduleImages.empty")}
          />
        )}
      </Card>
    </section>
  )
}
