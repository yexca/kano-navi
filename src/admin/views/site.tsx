import { useEffect, useState } from "react"
import { Globe, ImageDown, Images, RefreshCw, Save } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { japanStamp } from "@/admin/format"
import type {
  VariantGroupStatus,
  VariantStatus,
  VisitorRegionSettings,
} from "@/admin/types"
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

/** The groups in the order the server lists them, shown before it answers. */
const variantGroups: VariantGroupStatus["group"][] = [
  "lineblog",
  "works",
  "journey",
  "media",
]

/**
 * One group's ready originals against all of them, with what is left; a
 * group not read yet keeps its place with dashes.
 */
function VariantMeter({
  name,
  group,
}: {
  name: VariantGroupStatus["group"]
  group: VariantGroupStatus | undefined
}) {
  const { t } = useAppSettings()
  const missing = group ? group.originals - group.ready : 0
  const share = group && group.originals > 0 ? group.ready / group.originals : 0
  return (
    <li className={`adm-meter${group ? "" : " is-pending"}`}>
      <span className="adm-meter-label">
        {t(`admin.site.variantsGroup.${name}`)}
      </span>
      <span className="adm-meter-value">
        <strong>{group ? group.ready : "—"}</strong>
        {group ? (
          <small>
            {t("admin.site.variantsOf", { count: group.originals })}
          </small>
        ) : null}
      </span>
      <span className="adm-meter-bar" aria-hidden>
        <i style={{ width: `${share * 100}%` }} />
      </span>
      <span className="adm-meter-label">
        {!group
          ? " "
          : missing > 0
            ? t("admin.site.variantsMissing", { count: missing })
            : t("admin.site.variantsComplete")}
      </span>
    </li>
  )
}

/**
 * The smaller image copies: how complete each group is and a run that makes
 * the missing ones. The panel renders at once and fills in when the status
 * arrives. The page reads the status when it opens and after every run,
 * never on a timer; the server answers its last listing until a run
 * finishes, and Check again lists anew.
 */
function VariantsPanel({
  status,
  loading,
  onChange,
}: {
  status: VariantStatus | undefined
  loading: boolean
  onChange: (status: VariantStatus) => void
}) {
  const { t } = useAppSettings()
  const { api, act, start, overview } = useAdmin()
  const [busy, setBusy] = useState(false)
  const [checking, setChecking] = useState(false)
  const check = async () => {
    setChecking(true)
    const next = await act(() => api.variantStatus(true))
    setChecking(false)
    if (next) onChange(next)
  }
  const active = overview?.activeRun
  const running = active?.kind === "variants"
  const missing = (status?.groups ?? []).reduce(
    (sum, group) => sum + group.originals - group.ready,
    0,
  )
  const make = async () => {
    setBusy(true)
    await start({ kind: "variants" })
    setBusy(false)
  }
  return (
    <Panel
      icon={Images}
      title={t("admin.site.variants")}
      actions={
        <>
          <Button
            size="sm"
            variant="ghost"
            icon={RefreshCw}
            busy={checking || (loading && !status)}
            onClick={() => void check()}
          >
            {t("admin.site.variantsRefresh")}
          </Button>
          <Button
            size="sm"
            variant="primary"
            icon={ImageDown}
            busy={busy || running}
            disabled={missing === 0 || Boolean(active)}
            onClick={() => void make()}
          >
            {running
              ? t("admin.site.variantsRunning")
              : t("admin.site.variantsMake")}
          </Button>
        </>
      }
    >
      <ul className="adm-meters" aria-busy={!status}>
        {variantGroups.map((name) => (
          <VariantMeter
            key={name}
            name={name}
            group={status?.groups.find((group) => group.group === name)}
          />
        ))}
      </ul>
      <p className="adm-field-hint">
        {status
          ? t("admin.site.variantsChecked", {
              time: japanStamp(status.checkedAt),
            })
          : loading
            ? t("admin.loading")
            : null}{" "}
        {t("admin.site.variantsBudget")}
      </p>
    </Panel>
  )
}

/** The Site page: the image copies the pages are served and, last as the
 * least used, the mainland China site suggestion. */
export function SiteView() {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const region = useResource(() => api.visitorRegion(), [])
  const variants = useResource(() => api.variantStatus(), [])
  return (
    <div className="adm-view">
      <PageHeader title={t("admin.site.title")} />
      <VariantsPanel
        status={variants.data}
        loading={variants.loading}
        onChange={(next) => variants.setData(next)}
      />
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
