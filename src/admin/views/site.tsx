import { useEffect, useState } from "react"
import { Globe, Save } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { VisitorRegionSettings } from "@/admin/types"
import { Button, Field, Loading, PageHeader, Panel, Switch } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"

/**
 * The mainland China site suggestion: whether the public pages offer the
 * mirror to visitors the proxy marks as mainland China, and the provider and
 * mirror the suggestion names, saved together.
 */
function VisitorRegionPanel({
  settings,
  onChange,
}: {
  settings: VisitorRegionSettings
  onChange: (settings: VisitorRegionSettings) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [draft, setDraft] = useState(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => setDraft(settings), [settings])
  const dirty =
    draft.enabled !== settings.enabled ||
    draft.providerName !== settings.providerName ||
    draft.providerUrl !== settings.providerUrl ||
    draft.mirrorUrl !== settings.mirrorUrl
  const set = (patch: Partial<VisitorRegionSettings>) =>
    setDraft((current) => ({ ...current, ...patch }))
  const save = async () => {
    setBusy(true)
    const next = await act(
      () => api.updateVisitorRegion(draft),
      "admin.notice.visitorRegionSaved",
    )
    setBusy(false)
    if (next) onChange(next)
  }
  return (
    <Panel icon={Globe} title={t("admin.site.visitorRegion")}>
      <form
        className="adm-form"
        onSubmit={(event) => {
          event.preventDefault()
          void save()
        }}
      >
        <Switch
          checked={draft.enabled}
          disabled={busy}
          onChange={(enabled) => set({ enabled })}
          label={t("admin.site.visitorRegionToggle")}
        />
        <div className="adm-form-row">
          <Field label={t("admin.site.visitorRegionProvider")}>
            <input
              value={draft.providerName}
              onChange={(event) => set({ providerName: event.target.value })}
              maxLength={120}
              placeholder={t("admin.site.visitorRegionProviderPlaceholder")}
            />
          </Field>
          <Field label={t("admin.site.visitorRegionProviderUrl")}>
            <input
              type="url"
              value={draft.providerUrl}
              onChange={(event) => set({ providerUrl: event.target.value })}
              placeholder="https://"
            />
          </Field>
        </div>
        <Field label={t("admin.site.visitorRegionMirrorUrl")}>
          <input
            type="url"
            value={draft.mirrorUrl}
            onChange={(event) => set({ mirrorUrl: event.target.value })}
            placeholder="https://"
          />
        </Field>
        <div className="adm-form-actions">
          <Button
            type="submit"
            size="sm"
            variant="primary"
            icon={Save}
            busy={busy}
            disabled={!dirty || busy}
          >
            {t("admin.action.save")}
          </Button>
        </div>
      </form>
    </Panel>
  )
}

/** The Site page: settings that change what visitors see on the public
 * pages. */
export function SiteView() {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const region = useResource(() => api.visitorRegion(), [])
  return (
    <div className="adm-view">
      <PageHeader title={t("admin.site.title")} />
      {!region.data ? (
        <Loading />
      ) : (
        <VisitorRegionPanel
          settings={region.data}
          onChange={(next) => region.setData(next)}
        />
      )}
    </div>
  )
}
