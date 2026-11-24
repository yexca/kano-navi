import { useEffect, useState } from "react"
import { Save, ScanText } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import type { Extraction } from "@/admin/types"
import {
  Badge,
  Button,
  FieldGroup,
  Loading,
  PageHeader,
  Panel,
  Segmented,
  TagInput,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { ModelSetup } from "@/admin/views/llm"

const keywordLimit = 30
const keywordLength = 80

type DetectionMode = "keyword" | "llm"
type Stages = Pick<
  Extraction,
  "keywordEnabled" | "visionEnabled" | "messageEnabled" | "messageAllPosts"
>

const detectionModes: DetectionMode[] = ["keyword", "llm"]

/**
 * The stage flags behind each mode. Keyword match reads keyword posts as
 * schedule boards and other dated posts as messages; the LLM mode sends every
 * post with text or images through the message route.
 */
const modeStages: Record<DetectionMode, Stages> = {
  keyword: {
    keywordEnabled: true,
    visionEnabled: true,
    messageEnabled: true,
    messageAllPosts: false,
  },
  llm: {
    keywordEnabled: false,
    visionEnabled: false,
    messageEnabled: true,
    messageAllPosts: true,
  },
}

/** The mode whose flags the settings carry, or null for any other mix. */
function detectionMode(stages: Stages) {
  return (
    detectionModes.find((mode) =>
      (Object.keys(modeStages[mode]) as (keyof Stages)[]).every(
        (key) => stages[key] === modeStages[mode][key],
      ),
    ) ?? null
  )
}

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
  const mode = detectionMode(draft)
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
      <FieldGroup
        label={t("admin.detection.mode")}
        hint={mode ? undefined : t("admin.detection.modeHint.custom")}
      >
        <Segmented<DetectionMode | "">
          label={t("admin.detection.mode")}
          value={mode ?? ""}
          onChange={(next) =>
            next && setDraft({ ...draft, ...modeStages[next] })
          }
          options={detectionModes.map((value) => ({
            value,
            label: t(`admin.detection.mode.${value}`),
          }))}
        />
      </FieldGroup>
      {mode === "keyword" ? (
        <FieldGroup
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
        </FieldGroup>
      ) : null}
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

export function DetectionView({ tab, navigate }: ViewProps) {
  const { t } = useAppSettings()
  return (
    <div className="adm-view">
      <PageHeader title={t("admin.detection.title")} />
      <ExtractionPanel />
      <ModelSetup tab={tab} navigate={navigate} />
    </div>
  )
}
