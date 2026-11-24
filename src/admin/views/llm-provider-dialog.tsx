import { useState, type SubmitEvent } from "react"
import { Check, Globe, KeyRound, Save } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { inferenceEndpoint, providerPresets, providerSlug } from "@/admin/llm"
import type { LlmCatalog, Provider } from "@/admin/types"
import { Button, Field, Modal, Switch } from "@/admin/ui"
import { useAdmin } from "@/admin/use-admin-data"
import { cn } from "@/lib/utils"
import { ProviderMark } from "@/admin/views/llm-parts"

// --- providers --------------------------------------------------------------

interface ProviderDraft {
  name: string
  baseUrl: string
  protocol: string
  apiKey: string
  clearKey: boolean
  timeoutSeconds: number
  enabled: boolean
}

function presetHost(baseUrl: string) {
  try {
    return new URL(baseUrl).host
  } catch {
    return ""
  }
}

/** A provider's connection: create one from a preset, or edit a stored one. */
export function ProviderDialog({
  provider,
  catalog,
  onClose,
  onSaved,
}: {
  provider: Provider | null
  catalog: LlmCatalog
  onClose: () => void
  onSaved: (provider: Provider, created: boolean) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [preset, setPreset] = useState(provider ? "" : "openai")
  const [draft, setDraft] = useState<ProviderDraft>(() => ({
    name: provider?.name || providerPresets[0].name,
    baseUrl: provider?.baseUrl || providerPresets[0].baseUrl,
    protocol: provider?.protocol || providerPresets[0].protocol,
    apiKey: "",
    clearKey: false,
    timeoutSeconds: Math.round((provider?.timeoutMs || 30000) / 1000),
    enabled: provider?.enabled ?? true,
  }))
  const [busy, setBusy] = useState(false)
  const set = (patch: Partial<ProviderDraft>) =>
    setDraft((current) => ({ ...current, ...patch }))
  const choosePreset = (id: string) => {
    const chosen = providerPresets.find((item) => item.id === id)
    setPreset(id)
    if (chosen)
      set({
        name: chosen.name,
        baseUrl: chosen.baseUrl,
        protocol: chosen.protocol,
      })
  }
  const save = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    const body: Record<string, any> = {
      name: draft.name,
      baseUrl: draft.baseUrl,
      protocol: draft.protocol,
      timeoutMs: draft.timeoutSeconds * 1000,
      enabled: draft.enabled,
    }
    // A key is sent only when typed or explicitly cleared; it is never read back.
    if (draft.apiKey.trim()) body.apiKey = draft.apiKey.trim()
    else if (draft.clearKey) body.apiKey = null
    setBusy(true)
    const result = await act(
      () =>
        provider
          ? api.updateProvider(provider.id, body)
          : api.createProvider({
              ...body,
              id: providerSlug(
                draft.name,
                catalog.providers.map((item) => item.id),
              ),
            }),
      "admin.notice.providerSaved",
    )
    setBusy(false)
    if (result) onSaved(result.provider, !provider)
  }
  const endpoint = inferenceEndpoint(draft.baseUrl, draft.protocol)
  return (
    <Modal
      open
      wide
      title={provider ? t("admin.provider.edit") : t("admin.provider.create")}
      onClose={onClose}
    >
      <form className="adm-form" onSubmit={save}>
        {!provider ? (
          <fieldset className="adm-fieldset">
            <legend>{t("admin.provider.preset")}</legend>
            <div className="adm-preset-list">
              {providerPresets.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  className={cn(
                    "adm-preset",
                    preset === item.id && "is-active",
                  )}
                  aria-pressed={preset === item.id}
                  onClick={() => choosePreset(item.id)}
                >
                  <ProviderMark
                    provider={{
                      id: item.id,
                      name: item.name || "?",
                      baseUrl: item.baseUrl,
                    }}
                  />
                  <span className="adm-preset-text">
                    <strong>{item.name || t("admin.provider.custom")}</strong>
                    <small>
                      {presetHost(item.baseUrl) ||
                        t("admin.provider.customHint")}
                    </small>
                  </span>
                  {preset === item.id ? (
                    <Check className="adm-preset-check" aria-hidden />
                  ) : null}
                </button>
              ))}
            </div>
          </fieldset>
        ) : null}
        <div className="adm-form-row">
          <Field label={t("admin.provider.name")}>
            <input
              value={draft.name}
              onChange={(e) => set({ name: e.target.value })}
              maxLength={120}
              required
            />
          </Field>
          <Field label={t("admin.provider.protocol")}>
            <select
              value={draft.protocol}
              onChange={(e) => set({ protocol: e.target.value })}
            >
              <option value="openai-responses">
                {t("admin.provider.protocol.responses")}
              </option>
              <option value="openai-chat-completions">
                {t("admin.provider.protocol.chat")}
              </option>
              <option value="anthropic-messages">
                {t("admin.provider.protocol.anthropic")}
              </option>
            </select>
          </Field>
        </div>
        <Field
          label={t("admin.provider.baseUrl")}
          hint={
            endpoint ? t("admin.provider.endpoint", { endpoint }) : undefined
          }
        >
          <span className="adm-input-icon">
            <Globe aria-hidden />
            <input
              type="url"
              value={draft.baseUrl}
              onChange={(e) => set({ baseUrl: e.target.value })}
              required
              placeholder="https://"
            />
          </span>
        </Field>
        <Field
          label={t("admin.provider.apiKey")}
          hint={
            provider?.apiKeyConfigured
              ? t("admin.provider.apiKeyKeep")
              : undefined
          }
        >
          <span className="adm-input-icon">
            <KeyRound aria-hidden />
            <input
              type="password"
              autoComplete="off"
              value={draft.apiKey}
              onChange={(e) => set({ apiKey: e.target.value, clearKey: false })}
              maxLength={500}
            />
          </span>
        </Field>
        {provider?.apiKeyConfigured ? (
          <label className="adm-check">
            <input
              type="checkbox"
              checked={draft.clearKey}
              disabled={Boolean(draft.apiKey)}
              onChange={(e) => set({ clearKey: e.target.checked })}
            />
            {t("admin.provider.clearKey")}
          </label>
        ) : null}
        <details className="adm-disclosure">
          <summary>{t("admin.provider.requests")}</summary>
          <div className="adm-form-row">
            <Field label={t("admin.provider.timeout")}>
              <input
                type="number"
                min={1}
                max={120}
                value={draft.timeoutSeconds}
                onChange={(e) =>
                  set({ timeoutSeconds: Number(e.target.value) })
                }
              />
            </Field>
          </div>
        </details>
        <Switch
          checked={draft.enabled}
          onChange={(enabled) => set({ enabled })}
          label={t("admin.provider.enabled")}
        />
        <div className="adm-form-actions">
          <Button onClick={onClose}>{t("admin.action.cancel")}</Button>
          <Button type="submit" variant="primary" icon={Save} busy={busy}>
            {provider ? t("admin.action.save") : t("admin.provider.create")}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
