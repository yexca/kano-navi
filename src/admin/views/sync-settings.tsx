import { useEffect, useState, type ReactNode } from "react"
import { Gauge, Save } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { SyncSettingField, SyncSettings } from "@/admin/types"
import { Button, Field, Loading, Panel } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { cn } from "@/lib/utils"

/** A titled part of a group: the runs its fields bound. */
interface SyncSettingSection {
  /** Names the section under admin.syncSettings.section; a group with one
   * section shows no title. */
  id: "timed" | "manual" | "llm"
  fields: SyncSettingField[]
}

/** The sections and fields each panel edits, in display order. */
export const syncSettingGroups = {
  x: [
    {
      id: "timed",
      fields: [
        "xMaxStatusRequests",
        "xRefreshKnown",
        "xBootstrapDays",
        "xDiscoveryLimit",
      ],
    },
    {
      id: "manual",
      fields: ["fetchMaxPages", "fetchTimeoutMs", "fetchRequestDelayMs"],
    },
  ],
  youtube: [
    { id: "timed", fields: ["youtubeMaxDetailRequests"] },
    {
      id: "manual",
      fields: ["fetchMaxPages", "fetchTimeoutMs", "fetchRequestDelayMs"],
    },
  ],
  llm: [
    {
      id: "llm",
      fields: [
        "llmModelAttempts",
        "translationScanLimit",
        "translationLookbackDays",
      ],
    },
  ],
} satisfies Record<string, SyncSettingSection[]>

/** The fetch bounds X and YouTube share; their hint says so. */
const sharedFields = new Set<SyncSettingField>([
  "fetchMaxPages",
  "fetchTimeoutMs",
  "fetchRequestDelayMs",
])

/**
 * The run bounds of one group: whole numbers within each field's range,
 * saved together. The next run reads them; a running one keeps its values.
 * A group with several sections titles each, such as X's timed reads and
 * manual fetches.
 */
export function SyncSettingsPanel({
  group,
  level = 3,
  bare = false,
}: {
  group: keyof typeof syncSettingGroups
  /** 2 on a page of top-level panels, such as the LLM page. */
  level?: 2 | 3
  /** Without the panel's own title, inside a section that names it. */
  bare?: boolean
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
  const sections: SyncSettingSection[] = syncSettingGroups[group]
  const fields = sections.flatMap((section) => section.fields)
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
  const row = (sectionFields: SyncSettingField[]): ReactNode =>
    data ? (
      // Three fields share one row rather than leaving one alone.
      <div
        className={cn(
          "adm-form-row is-top",
          sectionFields.length === 3 && "is-three",
        )}
      >
        {sectionFields.map((field) => {
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
    ) : null
  return (
    <Panel
      level={level}
      icon={bare ? undefined : Gauge}
      title={bare ? undefined : t(`admin.syncSettings.${group}.title`)}
      className={bare ? "is-bare" : undefined}
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
          {sections.length === 1
            ? row(sections[0].fields)
            : sections.map((section) => (
                <fieldset key={section.id} className="adm-sync-section">
                  <legend>
                    {t(`admin.syncSettings.section.${section.id}`)}
                  </legend>
                  {row(section.fields)}
                </fieldset>
              ))}
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
