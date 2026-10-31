import { useEffect, useState } from "react"
import { Gauge, Save } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { SyncSettingField, SyncSettings } from "@/admin/types"
import { Button, Field, Loading, Panel } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"

/** The fields each panel edits, in display order. */
export const syncSettingGroups = {
  x: [
    "xMaxStatusRequests",
    "xRefreshKnown",
    "xBootstrapDays",
    "xDiscoveryLimit",
    "fetchMaxPages",
    "fetchTimeoutMs",
    "fetchRequestDelayMs",
  ],
  youtube: [
    "youtubeMaxDetailRequests",
    "fetchMaxPages",
    "fetchTimeoutMs",
    "fetchRequestDelayMs",
  ],
  llm: ["llmModelAttempts", "translationScanLimit", "translationLookbackDays"],
} satisfies Record<string, SyncSettingField[]>

/** The fetch bounds X and YouTube share; their hint says so. */
const sharedFields = new Set<SyncSettingField>([
  "fetchMaxPages",
  "fetchTimeoutMs",
  "fetchRequestDelayMs",
])

/**
 * The run bounds of one group: whole numbers within each field's range,
 * saved together. The next run reads them; a running one keeps its values.
 */
export function SyncSettingsPanel({
  group,
  level = 3,
}: {
  group: keyof typeof syncSettingGroups
  /** 2 on a page of top-level panels, such as the LLM page. */
  level?: 2 | 3
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const resource = useResource(() => api.syncSettings(), [])
  const data = resource.data
  const [draft, setDraft] = useState<Partial<Record<SyncSettingField, string>>>(
    {},
  )
  const [busy, setBusy] = useState(false)
  useEffect(() => setDraft({}), [data])
  const fields: SyncSettingField[] = syncSettingGroups[group]
  const value = (field: SyncSettingField) =>
    draft[field] ?? String(data?.settings[field] ?? "")
  const changed = fields.filter(
    (field) =>
      draft[field] !== undefined &&
      Number(draft[field]) !== data?.settings[field],
  )
  const save = async () => {
    setBusy(true)
    const body = Object.fromEntries(
      changed.map((field) => [field, Number(draft[field])]),
    )
    const next = await act(
      () => api.updateSyncSettings(body),
      "admin.notice.syncSettingsSaved",
    )
    setBusy(false)
    if (next) resource.setData(next as SyncSettings)
  }
  return (
    <Panel
      level={level}
      icon={Gauge}
      title={t(`admin.syncSettings.${group}.title`)}
    >
      {!data ? (
        <Loading />
      ) : (
        <form
          className="adm-form"
          onSubmit={(event) => {
            event.preventDefault()
            void save()
          }}
        >
          {/* Three fields share one row rather than leaving one alone. */}
          <div
            className={`adm-form-row is-top${fields.length === 3 ? " is-three" : ""}`}
          >
            {fields.map((field) => {
              const spec = data.fields.find((entry) => entry.field === field)
              return spec ? (
                <Field
                  key={field}
                  label={t(`admin.syncSettings.field.${field}`)}
                  hint={[
                    t(`admin.syncSettings.hint.${field}`),
                    t("admin.syncSettings.range", {
                      min: spec.min,
                      max: spec.max,
                      default: spec.default,
                    }),
                    sharedFields.has(field) && group !== "llm"
                      ? t("admin.syncSettings.shared")
                      : null,
                  ]
                    .filter(Boolean)
                    .join(" ")}
                >
                  <input
                    type="number"
                    inputMode="numeric"
                    min={spec.min}
                    max={spec.max}
                    step={1}
                    required
                    value={value(field)}
                    onChange={(event) =>
                      setDraft((current) => ({
                        ...current,
                        [field]: event.target.value,
                      }))
                    }
                  />
                </Field>
              ) : null
            })}
          </div>
          <div className="adm-form-actions">
            <Button
              type="submit"
              size="sm"
              icon={Save}
              busy={busy}
              disabled={!changed.length || busy}
            >
              {t("admin.action.save")}
            </Button>
          </div>
        </form>
      )}
    </Panel>
  )
}
