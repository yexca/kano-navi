import { useEffect, useState } from "react"
import { ArrowRight, Bot, Save, ScanText, TriangleAlert } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import type { Extraction } from "@/admin/types"
import {
  Badge,
  Button,
  Field,
  Loading,
  PageHeader,
  Panel,
  Switch,
  TagInput,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { firstUsable, ProviderMark } from "@/admin/views/llm"

const keywordLimit = 30
const keywordLength = 80

function ExtractionPanel() {
  const { t } = useAppSettings()
  const { api, act, start, overview } = useAdmin()
  const extraction = useResource(() => api.extraction(), [])
  const [draft, setDraft] = useState<Extraction | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (extraction.data) setDraft(extraction.data)
  }, [extraction.data])
  if (!draft || !extraction.data)
    return (
      <Panel title={t("admin.models.extraction")}>
        <Loading />
      </Panel>
    )
  const saved = extraction.data
  const dirty =
    draft.keywordEnabled !== saved.keywordEnabled ||
    draft.visionEnabled !== saved.visionEnabled ||
    draft.messageEnabled !== saved.messageEnabled ||
    draft.messageAllPosts !== saved.messageAllPosts ||
    draft.keywords.join("\n") !== saved.keywords.join("\n")
  const save = async () => {
    setBusy(true)
    const next = await act(
      () =>
        api.updateExtraction({
          keywordEnabled: draft.keywordEnabled,
          visionEnabled: draft.visionEnabled,
          messageEnabled: draft.messageEnabled,
          messageAllPosts: draft.messageAllPosts,
          keywords: draft.keywords,
        }),
      "admin.notice.extractionSaved",
    )
    setBusy(false)
    if (next) extraction.setData(next)
  }
  return (
    <Panel
      icon={ScanText}
      title={t("admin.models.extraction")}
      description={t("admin.models.extractionHint")}
      actions={
        <>
          <Badge tone={saved.enabled ? "leaf" : "neutral"}>
            {t(
              saved.enabled
                ? "admin.models.extractionOn"
                : "admin.models.extractionOff",
            )}
          </Badge>
          <Button
            size="sm"
            variant="ghost"
            disabled={Boolean(overview?.activeRun)}
            onClick={() => start({ kind: "scan" })}
          >
            {t("admin.action.scan")}
          </Button>
        </>
      }
    >
      <div className="adm-stage-grid">
        <div className="adm-stage">
          <Switch
            checked={draft.keywordEnabled}
            onChange={(keywordEnabled) =>
              setDraft({ ...draft, keywordEnabled })
            }
            label={t("admin.models.stage.keyword")}
            description={t("admin.models.stage.keywordHint")}
          />
        </div>
        <div className="adm-stage">
          <Switch
            checked={draft.visionEnabled}
            onChange={(visionEnabled) => setDraft({ ...draft, visionEnabled })}
            label={t("admin.models.stage.vision")}
            description={t("admin.models.stage.visionHint")}
          />
          {draft.visionEnabled && !draft.keywordEnabled ? (
            <p className="adm-inline-error">
              <TriangleAlert aria-hidden />
              {t("admin.models.stage.visionNeedsKeywords")}
            </p>
          ) : null}
        </div>
        <div className="adm-stage">
          <Switch
            checked={draft.messageEnabled}
            onChange={(messageEnabled) =>
              setDraft({ ...draft, messageEnabled })
            }
            label={t("admin.models.stage.message")}
            description={t("admin.models.stage.messageHint")}
          />
          <div className="adm-stage-sub">
            <Switch
              checked={draft.messageAllPosts}
              disabled={!draft.messageEnabled}
              onChange={(messageAllPosts) =>
                setDraft({ ...draft, messageAllPosts })
              }
              label={t("admin.models.stage.allPosts")}
              description={t("admin.models.stage.allPostsHint")}
            />
          </div>
        </div>
      </div>
      <Field
        label={t("admin.models.keywords")}
        hint={t("admin.models.keywordsHint", {
          count: draft.keywords.length,
          max: keywordLimit,
          length: keywordLength,
        })}
      >
        <TagInput
          value={draft.keywords}
          onChange={(keywords) => setDraft({ ...draft, keywords })}
          label={t("admin.models.keywords")}
          placeholder={t("admin.models.keywordsPlaceholder")}
          max={keywordLimit}
          maxLength={keywordLength}
        />
      </Field>
      <div className="adm-form-actions">
        {dirty ? (
          <>
            <span className="adm-muted">{t("admin.models.unsaved")}</span>
            <Button size="sm" variant="ghost" onClick={() => setDraft(saved)}>
              {t("admin.action.discard")}
            </Button>
          </>
        ) : null}
        <Button
          variant="primary"
          size="sm"
          icon={Save}
          busy={busy}
          disabled={!dirty}
          onClick={save}
        >
          {t("admin.action.save")}
        </Button>
      </div>
    </Panel>
  )
}

/** Which model each model stage calls first; the queue itself is on the LLM page. */
function ModelSummary({ onOpen }: { onOpen: () => void }) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const catalog = useResource(() => api.llm(), [])
  const data = catalog.data
  const stages = [
    { key: "text", needs: "text" as const },
    { key: "image", needs: "image" as const },
  ]
  return (
    <Panel
      icon={Bot}
      title={t("admin.detection.models")}
      description={t("admin.detection.modelsHint")}
      actions={
        <Button size="sm" icon={ArrowRight} onClick={onOpen}>
          {t("admin.detection.editQueue")}
        </Button>
      }
    >
      {!data ? (
        <Loading />
      ) : (
        <ul className="adm-stage-models">
          {stages.map((stage) => {
            const first = firstUsable(data, data.queue, stage.needs)
            return (
              <li key={stage.key}>
                <span>{t(`admin.detection.stage.${stage.key}`)}</span>
                {first ? (
                  <span className="adm-stage-model">
                    {first.provider ? (
                      <ProviderMark provider={first.provider} />
                    ) : null}
                    <strong>{first.model?.name || first.modelId}</strong>
                  </span>
                ) : (
                  <Badge tone="warning" icon={TriangleAlert}>
                    {t("admin.detection.noModel")}
                  </Badge>
                )}
              </li>
            )
          })}
        </ul>
      )}
    </Panel>
  )
}

export function DetectionView({ navigate }: ViewProps) {
  const { t } = useAppSettings()
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.detection.title")}
        description={t("admin.detection.description")}
      />
      <ExtractionPanel />
      <ModelSummary onOpen={() => navigate("llm")} />
    </div>
  )
}
