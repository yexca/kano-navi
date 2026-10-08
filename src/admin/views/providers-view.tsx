import { useCallback, useEffect, useMemo, useState } from "react"
import {
  Bot,
  CircleCheck,
  CloudDownload,
  KeyRound,
  Link2,
  PlugZap,
  Plus,
  Route,
  Save,
  Search,
  Server,
  Star,
  Trash2,
  TriangleAlert,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { adminMessage, formatAdminError, jsonBody, request } from "@/admin/api"
import { formatRelative, paletteIndex } from "@/admin/format"
import {
  inferenceEndpoint,
  modelTags,
  modelsEndpoint,
  providerPresets,
  providerSlug,
  scheduleRoutes,
} from "@/admin/llm"
import {
  Btn,
  Card,
  CardHeader,
  Dialog,
  EmptyState,
  Field,
  IconBtn,
  PageHeader,
  Switch,
  Tag,
} from "@/admin/components/primitives"
import { cn } from "@/lib/utils"
import { providerIcon } from "@/admin/provider-icons"

const selectedProviderKey = "kano-admin-provider"
const discoverRenderLimit = 300

function readSelectedProvider() {
  try {
    return window.localStorage.getItem(selectedProviderKey) || ""
  } catch {
    return ""
  }
}

function ProviderAvatar({ provider }: { provider: any }) {
  const icon = providerIcon(provider)
  const initial = String(provider?.name || provider?.id || "?")
    .trim()
    .slice(0, 1)
    .toUpperCase()
  return (
    <span
      className={cn(
        "adm-avatar",
        `adm-avatar-${paletteIndex(provider?.id || provider?.name)}`,
      )}
      aria-hidden="true"
    >
      {icon ? (
        <span
          className="adm-provider-logo"
          style={{ maskImage: `url("${icon}")` }}
        />
      ) : (
        initial
      )}
    </span>
  )
}

function providerState(provider, draftApiKey = "") {
  if (!provider.enabled) return "disabled"
  if (String(draftApiKey).trim()) return "pending"
  if (!provider.apiKeyConfigured) return "keyMissing"
  if (provider.lastStatus === "failed") return "failed"
  if (provider.lastStatus === "success") return "ok"
  return "unchecked"
}

function ModelTagToggles({
  tags,
  onToggle,
  disabled,
  t,
}: {
  tags: any
  onToggle: any
  disabled?: boolean
  t: any
}) {
  return (
    <div
      className="adm-tag-toggles"
      role="group"
      aria-label={t("admin.model.tags")}
    >
      {modelTags.map((tag) => {
        const Icon = tag.icon
        const active = tags.includes(tag.id)
        return (
          <button
            key={tag.id}
            type="button"
            className={cn(
              "adm-tag",
              `adm-tone-${tag.tone}`,
              !active && "is-inactive",
            )}
            aria-pressed={active}
            onClick={() => onToggle(tag.id)}
            disabled={disabled}
            title={t(`admin.model.tag.${tag.id}Hint`)}
          >
            <Icon aria-hidden="true" />
            {t(`admin.model.tag.${tag.id}`)}
          </button>
        )
      })}
    </div>
  )
}

function StaticTags({ tags, t }: { tags: any; t: any }) {
  if (!tags?.length)
    return (
      <span className="adm-muted adm-small">{t("admin.model.noTags")}</span>
    )
  return (
    <span className="adm-tag-row">
      {modelTags
        .filter((tag) => tags.includes(tag.id))
        .map((tag) => {
          const Icon = tag.icon
          return (
            <Tag key={tag.id} tone={tag.tone}>
              <Icon aria-hidden="true" />
              {t(`admin.model.tag.${tag.id}`)}
            </Tag>
          )
        })}
    </span>
  )
}

function NewProviderDialog({
  providers,
  onClose,
  onCreated,
  onError,
  t,
}: {
  providers: any
  onClose: any
  onCreated: any
  onError: any
  t: any
}) {
  const [preset, setPreset] = useState(providerPresets[0])
  const [form, setForm] = useState({
    name: providerPresets[0].name,
    baseUrl: providerPresets[0].baseUrl,
    protocol: providerPresets[0].protocol,
    apiKey: "",
  })
  const [busy, setBusy] = useState(false)

  const choose = (next) => {
    setPreset(next)
    setForm((current) => ({
      ...current,
      name: next.name || current.name,
      baseUrl: next.baseUrl,
      protocol: next.protocol,
    }))
  }

  const submit = async (event) => {
    event.preventDefault()
    setBusy(true)
    try {
      const id = providerSlug(
        form.name,
        providers.map((provider) => provider.id),
      )
      const payload = await request("/providers", {
        method: "POST",
        body: jsonBody({
          id,
          name: form.name,
          baseUrl: form.baseUrl,
          protocol: form.protocol,
          enabled: true,
          ...(form.apiKey.trim() ? { apiKey: form.apiKey.trim() } : {}),
        }),
      })
      await onCreated(payload.provider, Boolean(form.apiKey.trim()))
    } catch (error) {
      onError(formatAdminError(error))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      title={t("admin.provider.newTitle")}
      description={t("admin.provider.newDescription")}
      onClose={onClose}
      closeLabel={t("admin.action.close")}
      size="lg"
    >
      <form className="adm-form" onSubmit={submit}>
        <fieldset className="adm-fieldset is-wide">
          <legend>{t("admin.provider.preset")}</legend>
          <div className="adm-preset-grid">
            {providerPresets.map((item) => (
              <button
                key={item.id}
                type="button"
                className={cn(
                  "adm-preset",
                  preset.id === item.id && "is-active",
                )}
                aria-pressed={preset.id === item.id}
                onClick={() => choose(item)}
              >
                <ProviderAvatar
                  provider={{ ...item, name: item.name || "+" }}
                />
                <span>{item.name || t("admin.provider.custom")}</span>
              </button>
            ))}
          </div>
        </fieldset>
        <Field label={t("admin.provider.name")}>
          <input
            value={form.name}
            onChange={(event) =>
              setForm((current) => ({ ...current, name: event.target.value }))
            }
            maxLength={120}
            required
            data-autofocus
          />
        </Field>
        <Field label={t("admin.provider.protocol")}>
          <select
            value={form.protocol}
            onChange={(event) =>
              setForm((current) => ({
                ...current,
                protocol: event.target.value,
              }))
            }
          >
            <option value="openai-chat-completions">
              {t("admin.provider.protocol.chat")}
            </option>
            <option value="openai-responses">
              {t("admin.provider.protocol.responses")}
            </option>
            <option value="anthropic-messages">
              {t("admin.provider.protocol.anthropic")}
            </option>
          </select>
        </Field>
        <Field
          label={t("admin.provider.baseUrl")}
          hint={
            form.baseUrl
              ? t("admin.provider.endpointPreview", {
                  models: modelsEndpoint(form.baseUrl),
                })
              : t("admin.provider.baseUrlHint")
          }
          wide
        >
          <input
            type="url"
            value={form.baseUrl}
            onChange={(event) =>
              setForm((current) => ({
                ...current,
                baseUrl: event.target.value,
              }))
            }
            required
            placeholder="https://api.example.invalid"
          />
        </Field>
        <Field
          label={t("admin.provider.apiKey")}
          hint={t("admin.provider.apiKeyHint")}
          wide
        >
          <input
            type="password"
            value={form.apiKey}
            onChange={(event) =>
              setForm((current) => ({ ...current, apiKey: event.target.value }))
            }
            autoComplete="new-password"
            placeholder={t("admin.provider.apiKeyPlaceholder")}
          />
        </Field>
        <div className="adm-form-actions is-wide">
          <Btn variant="outline" onClick={onClose}>
            {t("admin.action.cancel")}
          </Btn>
          <Btn type="submit" variant="primary" icon={Plus} busy={busy}>
            {t("admin.provider.create")}
          </Btn>
        </div>
      </form>
    </Dialog>
  )
}

function DiscoverDialog({
  provider,
  onClose,
  onAdded,
  onError,
  t,
}: {
  provider: any
  onClose: any
  onAdded: any
  onError: any
  t: any
}) {
  const [state, setState] = useState({ loading: true, models: [], error: null })
  const [query, setQuery] = useState("")
  const [hideAdded, setHideAdded] = useState(true)
  const [selected, setSelected] = useState(() => new Set())
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let active = true
    request(`/providers/${encodeURIComponent(provider.id)}/models/discover`, {
      method: "POST",
    })
      .then((payload) => {
        if (active)
          setState({ loading: false, models: payload.models, error: null })
      })
      .catch((error) => {
        if (active)
          setState({
            loading: false,
            models: [],
            error: formatAdminError(error),
          })
      })
    return () => {
      active = false
    }
  }, [provider.id])

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase()
    return state.models.filter(
      (model) =>
        (!hideAdded || !model.added) &&
        (!needle ||
          model.id.toLowerCase().includes(needle) ||
          String(model.ownedBy || "")
            .toLowerCase()
            .includes(needle)),
    )
  }, [hideAdded, query, state.models])
  const visible = filtered.slice(0, discoverRenderLimit)
  const selectable = visible.filter((model) => !model.added)
  const allVisibleSelected =
    selectable.length > 0 && selectable.every((model) => selected.has(model.id))

  const toggle = (id) =>
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const toggleVisible = () =>
    setSelected((current) => {
      const next = new Set(current)
      for (const model of selectable) {
        if (allVisibleSelected) next.delete(model.id)
        else next.add(model.id)
      }
      return next
    })

  const add = async () => {
    setBusy(true)
    try {
      const chosen = state.models.filter((model) => selected.has(model.id))
      await request(`/providers/${encodeURIComponent(provider.id)}/models`, {
        method: "POST",
        body: jsonBody({
          origin: "remote",
          models: chosen.map((model) => ({
            id: model.id,
            ...(model.name ? { name: model.name } : {}),
            ...(model.ownedBy ? { ownedBy: model.ownedBy } : {}),
          })),
        }),
      })
      await onAdded(chosen.length)
    } catch (error) {
      onError(formatAdminError(error))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      title={t("admin.discover.title", { name: provider.name })}
      description={t("admin.discover.description")}
      onClose={onClose}
      closeLabel={t("admin.action.close")}
      size="lg"
      footer={
        <>
          <span className="adm-muted adm-small">
            {t("admin.discover.selected", { count: selected.size })}
          </span>
          <Btn variant="outline" onClick={onClose}>
            {t("admin.action.cancel")}
          </Btn>
          <Btn
            variant="primary"
            icon={Plus}
            busy={busy}
            disabled={!selected.size}
            onClick={add}
          >
            {t("admin.discover.add", { count: selected.size })}
          </Btn>
        </>
      }
    >
      {state.loading ? (
        <div className="adm-loading-block">
          <CloudDownload className="adm-pulse" aria-hidden="true" />
          {t("admin.discover.loading")}
        </div>
      ) : state.error ? (
        <p className="adm-callout is-danger">
          <TriangleAlert aria-hidden="true" />
          {state.error.key ? t(state.error.key) : state.error.text}
        </p>
      ) : (
        <div className="adm-discover">
          <div className="adm-discover-toolbar">
            <label className="adm-search">
              <Search aria-hidden="true" />
              <input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder={t("admin.discover.search", {
                  count: state.models.length,
                })}
                data-autofocus
              />
            </label>
            <label className="adm-check-line">
              <input
                type="checkbox"
                checked={hideAdded}
                onChange={(event) => setHideAdded(event.target.checked)}
              />
              {t("admin.discover.hideAdded")}
            </label>
            <Btn
              variant="ghost"
              size="sm"
              onClick={toggleVisible}
              disabled={!selectable.length}
            >
              {allVisibleSelected
                ? t("admin.discover.clearVisible")
                : t("admin.discover.selectVisible")}
            </Btn>
          </div>
          {visible.length ? (
            <ul className="adm-discover-list">
              {visible.map((model) => (
                <li key={model.id} className={model.added ? "is-added" : ""}>
                  <label>
                    <input
                      type="checkbox"
                      checked={model.added || selected.has(model.id)}
                      disabled={model.added}
                      onChange={() => toggle(model.id)}
                    />
                    <span className="adm-discover-copy">
                      <code>{model.id}</code>
                      {model.ownedBy ? <small>{model.ownedBy}</small> : null}
                    </span>
                    <StaticTags
                      tags={model.tags || model.suggestedTags}
                      t={t}
                    />
                    {model.added ? (
                      <Tag tone="leaf">{t("admin.discover.added")}</Tag>
                    ) : null}
                  </label>
                </li>
              ))}
            </ul>
          ) : (
            <EmptyState icon={Search} title={t("admin.discover.none")} />
          )}
          {filtered.length > visible.length ? (
            <p className="adm-muted adm-small">
              {t("admin.discover.truncated", {
                shown: visible.length,
                total: filtered.length,
              })}
            </p>
          ) : null}
        </div>
      )}
    </Dialog>
  )
}

function ManualModelDialog({
  provider,
  onClose,
  onAdded,
  onError,
  t,
}: {
  provider: any
  onClose: any
  onAdded: any
  onError: any
  t: any
}) {
  const [id, setId] = useState("")
  const [tags, setTags] = useState(["text"])
  const [busy, setBusy] = useState(false)
  const submit = async (event) => {
    event.preventDefault()
    setBusy(true)
    try {
      await request(`/providers/${encodeURIComponent(provider.id)}/models`, {
        method: "POST",
        body: jsonBody({ models: [{ id: id.trim(), tags }] }),
      })
      await onAdded(1)
    } catch (error) {
      onError(formatAdminError(error))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog
      title={t("admin.model.manualTitle")}
      description={t("admin.model.manualDescription")}
      onClose={onClose}
      closeLabel={t("admin.action.close")}
    >
      <form className="adm-form" onSubmit={submit}>
        <Field label={t("admin.model.id")} wide>
          <input
            value={id}
            onChange={(event) => setId(event.target.value)}
            maxLength={200}
            required
            data-autofocus
            placeholder="vendor/model-name"
            spellCheck={false}
          />
        </Field>
        <div className="is-wide">
          <span className="adm-field-label">{t("admin.model.tags")}</span>
          <ModelTagToggles
            tags={tags}
            onToggle={(tag) =>
              setTags((current) =>
                current.includes(tag)
                  ? current.filter((item) => item !== tag)
                  : [...current, tag],
              )
            }
            t={t}
          />
        </div>
        <div className="adm-form-actions is-wide">
          <Btn variant="outline" onClick={onClose}>
            {t("admin.action.cancel")}
          </Btn>
          <Btn type="submit" variant="primary" icon={Plus} busy={busy}>
            {t("admin.model.add")}
          </Btn>
        </div>
      </form>
    </Dialog>
  )
}

function ConnectionCard({
  provider,
  models,
  onSaved,
  onError,
  onNotice,
  t,
  locale,
}: {
  provider: any
  models: any
  onSaved: any
  onError: any
  onNotice: any
  t: any
  locale: any
}) {
  const [form, setForm] = useState(() => ({
    name: provider.name,
    baseUrl: provider.baseUrl,
    protocol: provider.protocol,
    timeoutMs: provider.timeoutMs,
    apiKey: "",
    clearApiKey: false,
  }))
  const [busy, setBusy] = useState("")
  const [testModel, setTestModel] = useState(provider.model || "")

  useEffect(() => {
    setForm({
      name: provider.name,
      baseUrl: provider.baseUrl,
      protocol: provider.protocol,
      timeoutMs: provider.timeoutMs,
      apiKey: "",
      clearApiKey: false,
    })
    setTestModel(provider.model || "")
  }, [
    provider.id,
    provider.name,
    provider.baseUrl,
    provider.protocol,
    provider.timeoutMs,
    provider.model,
  ])

  const setField = (name) => (event) =>
    setForm((current) => ({
      ...current,
      [name]:
        event.target.type === "checkbox"
          ? event.target.checked
          : event.target.value,
    }))

  const dirty =
    form.name !== provider.name ||
    form.baseUrl !== provider.baseUrl ||
    form.protocol !== provider.protocol ||
    Number(form.timeoutMs) !== Number(provider.timeoutMs) ||
    Boolean(form.apiKey.trim()) ||
    form.clearApiKey

  const save = async (event) => {
    event?.preventDefault()
    setBusy("save")
    try {
      const body: {
        name: string
        baseUrl: string
        protocol: string
        timeoutMs: number
        apiKey?: string | null
      } = {
        name: form.name,
        baseUrl: form.baseUrl,
        protocol: form.protocol,
        timeoutMs: Number(form.timeoutMs),
      }
      if (form.apiKey.trim()) body.apiKey = form.apiKey.trim()
      else if (form.clearApiKey) body.apiKey = null
      await request(`/providers/${encodeURIComponent(provider.id)}`, {
        method: "PUT",
        body: jsonBody(body),
      })
      await onSaved()
      onNotice(adminMessage("admin.provider.saved"))
    } catch (error) {
      onError(formatAdminError(error))
    } finally {
      setBusy("")
    }
  }

  const test = async () => {
    setBusy("test")
    try {
      const payload = await request(
        `/providers/${encodeURIComponent(provider.id)}/test`,
        {
          method: "POST",
          body: jsonBody(testModel ? { model: testModel } : {}),
        },
      )
      await onSaved()
      onNotice(
        payload.method === "models"
          ? adminMessage("admin.provider.testModels", {
              count: payload.remoteModelCount,
            })
          : adminMessage("admin.provider.testSuccess", {
              model: payload.model,
            }),
      )
    } catch (error) {
      await onSaved().catch(() => {})
      onError(formatAdminError(error))
    } finally {
      setBusy("")
    }
  }

  const status = providerState(provider, form.apiKey)

  return (
    <Card>
      <CardHeader
        icon={PlugZap}
        title={t("admin.provider.connection")}
        description={t("admin.provider.connectionDescription")}
        actions={
          <span className={cn("adm-health", `is-${status}`)}>
            {status === "ok" ? (
              <CircleCheck aria-hidden="true" />
            ) : ["keyMissing", "pending"].includes(status) ? (
              <KeyRound aria-hidden="true" />
            ) : status === "failed" ? (
              <TriangleAlert aria-hidden="true" />
            ) : null}
            {t(`admin.provider.health.${status}`)}
            {provider.lastCheckedAt && status !== "pending"
              ? ` · ${formatRelative(provider.lastCheckedAt, locale)}`
              : ""}
          </span>
        }
      />
      <form className="adm-form" onSubmit={save}>
        <Field label={t("admin.provider.name")}>
          <input
            value={form.name}
            onChange={setField("name")}
            maxLength={120}
            required
          />
        </Field>
        <Field label={t("admin.provider.protocol")}>
          <select value={form.protocol} onChange={setField("protocol")}>
            <option value="openai-chat-completions">
              {t("admin.provider.protocol.chat")}
            </option>
            <option value="openai-responses">
              {t("admin.provider.protocol.responses")}
            </option>
            <option value="anthropic-messages">
              {t("admin.provider.protocol.anthropic")}
            </option>
          </select>
        </Field>
        <Field
          label={t("admin.provider.apiKey")}
          hint={
            provider.apiKeyConfigured
              ? t("admin.provider.apiKeyKeep")
              : t("admin.provider.apiKeyHint")
          }
          wide
        >
          <div className="adm-input-group">
            <KeyRound aria-hidden="true" />
            <input
              type="password"
              value={form.apiKey}
              onChange={setField("apiKey")}
              autoComplete="new-password"
              placeholder={
                provider.apiKeyConfigured
                  ? t("admin.provider.apiKeySaved")
                  : t("admin.provider.apiKeyPlaceholder")
              }
            />
            {form.apiKey.trim() ? (
              <Tag tone="sky">{t("admin.provider.keyPending")}</Tag>
            ) : provider.apiKeyConfigured ? (
              <Tag tone="leaf">{t("admin.provider.keyStored")}</Tag>
            ) : (
              <Tag tone="honey">{t("admin.provider.health.keyMissing")}</Tag>
            )}
          </div>
        </Field>
        {provider.apiKeyConfigured ? (
          <label className="adm-check-line is-wide">
            <input
              type="checkbox"
              checked={form.clearApiKey}
              onChange={setField("clearApiKey")}
            />
            {t("admin.provider.clearApiKey")}
          </label>
        ) : null}
        <Field
          label={t("admin.provider.baseUrl")}
          hint={t("admin.provider.endpointDetail", {
            models: modelsEndpoint(form.baseUrl),
            inference: inferenceEndpoint(form.baseUrl, form.protocol),
          })}
          wide
        >
          <div className="adm-input-group">
            <Link2 aria-hidden="true" />
            <input
              type="url"
              value={form.baseUrl}
              onChange={setField("baseUrl")}
              required
              spellCheck={false}
            />
          </div>
        </Field>
        <Field label={t("admin.provider.timeout")}>
          <input
            type="number"
            min="1000"
            max="120000"
            step="1000"
            value={form.timeoutMs}
            onChange={setField("timeoutMs")}
          />
        </Field>
        <Field label={t("admin.provider.testModel")}>
          <select
            value={testModel}
            onChange={(event) => setTestModel(event.target.value)}
          >
            <option value="">{t("admin.provider.testListOnly")}</option>
            {models.map((model) => (
              <option key={model.id} value={model.id}>
                {model.id}
              </option>
            ))}
          </select>
        </Field>
        <div className="adm-form-actions is-wide">
          <Btn
            variant="outline"
            icon={PlugZap}
            busy={busy === "test"}
            disabled={busy !== "" || !provider.apiKeyConfigured || dirty}
            onClick={test}
            title={dirty ? t("admin.provider.saveFirst") : undefined}
          >
            {t("admin.provider.test")}
          </Btn>
          <Btn
            type="submit"
            variant="primary"
            icon={Save}
            busy={busy === "save"}
            disabled={busy !== "" || !dirty}
          >
            {t("admin.action.save")}
          </Btn>
        </div>
      </form>
    </Card>
  )
}

function routeUsage(model, provider, config) {
  const routes = []
  for (const route of scheduleRoutes) {
    const targets = config?.configuredRouteTargets?.[route.id] || []
    if (
      targets.some(
        (target) =>
          target.providerId === provider.id &&
          (target.modelId === model.id ||
            (!target.modelId && provider.model === model.id)),
      )
    )
      routes.push(route)
  }
  return routes
}

function ModelsCard({
  provider,
  models,
  config,
  autoOpen,
  onAutoOpened,
  onChanged,
  onError,
  onNotice,
  onNavigate,
  t,
}: {
  provider: any
  models: any
  config: any
  autoOpen: any
  onAutoOpened: any
  onChanged: any
  onError: any
  onNotice: any
  onNavigate: any
  t: any
}) {
  const [query, setQuery] = useState("")
  const [dialog, setDialog] = useState(null)
  const [busy, setBusy] = useState("")

  // A provider created with a key opens model discovery right away.
  useEffect(() => {
    if (!autoOpen) return
    if (provider.apiKeyConfigured) setDialog("discover")
    onAutoOpened()
  }, [autoOpen, onAutoOpened, provider.apiKeyConfigured])

  const filtered = models.filter((model) =>
    model.id.toLowerCase().includes(query.trim().toLowerCase()),
  )

  const mutate = async (key, path, options, notice?) => {
    setBusy(key)
    try {
      await request(path, options)
      await onChanged()
      if (notice) onNotice(notice)
    } catch (error) {
      onError(formatAdminError(error))
    } finally {
      setBusy("")
    }
  }

  const base = `/providers/${encodeURIComponent(provider.id)}`
  const toggleTag = (model, tag) => {
    const tags = model.tags.includes(tag)
      ? model.tags.filter((item) => item !== tag)
      : [...model.tags, tag]
    mutate(`tag-${model.id}`, `${base}/models`, {
      method: "PUT",
      body: jsonBody({ id: model.id, tags }),
    })
  }

  const added = async (count) => {
    setDialog(null)
    await onChanged()
    onNotice(adminMessage("admin.model.addedNotice", { count }))
  }

  return (
    <Card>
      <CardHeader
        icon={Bot}
        title={t("admin.model.title", { count: models.length })}
        description={t("admin.model.description")}
        actions={
          <>
            <Btn
              variant="outline"
              size="sm"
              icon={Plus}
              onClick={() => setDialog("manual")}
            >
              {t("admin.model.manual")}
            </Btn>
            <Btn
              variant="primary"
              size="sm"
              icon={CloudDownload}
              onClick={() => setDialog("discover")}
              disabled={!provider.apiKeyConfigured}
              title={
                provider.apiKeyConfigured
                  ? undefined
                  : t("admin.provider.health.keyMissing")
              }
            >
              {t("admin.model.fetch")}
            </Btn>
          </>
        }
      />
      {models.length > 6 ? (
        <label className="adm-search adm-models-search">
          <Search aria-hidden="true" />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t("admin.model.search")}
          />
        </label>
      ) : null}
      {filtered.length ? (
        <ul className="adm-model-list">
          {filtered.map((model) => {
            const isDefault = provider.model === model.id
            const usage = routeUsage(model, provider, config)
            const canDefault = model.capabilities.length > 0
            return (
              <li
                key={model.id}
                className={cn("adm-model-row", !model.enabled && "is-disabled")}
              >
                <div className="adm-model-main">
                  <button
                    type="button"
                    className={cn("adm-star", isDefault && "is-on")}
                    aria-pressed={isDefault}
                    aria-label={t("admin.model.default")}
                    title={
                      canDefault
                        ? t("admin.model.defaultHint")
                        : t("admin.model.defaultNeedsCapability")
                    }
                    disabled={isDefault || !canDefault || busy !== ""}
                    onClick={() =>
                      mutate(
                        `default-${model.id}`,
                        base,
                        {
                          method: "PUT",
                          body: jsonBody({
                            model: model.id,
                            capabilities: model.capabilities,
                          }),
                        },
                        adminMessage("admin.model.defaultSet", {
                          model: model.id,
                        }),
                      )
                    }
                  >
                    <Star aria-hidden="true" />
                  </button>
                  <div className="adm-model-copy">
                    <code>{model.id}</code>
                    <small>
                      {[
                        model.name,
                        model.ownedBy,
                        t(`admin.model.origin.${model.origin}`),
                      ]
                        .filter(Boolean)
                        .join(" · ")}
                    </small>
                  </div>
                  <div className="adm-model-actions">
                    {usage.length ? (
                      <span
                        className="adm-usage"
                        title={usage
                          .map((route) => t(route.labelKey))
                          .join(", ")}
                      >
                        <Route aria-hidden="true" />
                        {usage.length}
                      </span>
                    ) : null}
                    <Switch
                      size="sm"
                      checked={model.enabled}
                      label={t("admin.model.enabled")}
                      disabled={busy !== ""}
                      onChange={(enabled) =>
                        mutate(`enable-${model.id}`, `${base}/models`, {
                          method: "PUT",
                          body: jsonBody({ id: model.id, enabled }),
                        })
                      }
                    />
                    <IconBtn
                      label={t("admin.model.remove", { model: model.id })}
                      icon={Trash2}
                      busy={busy === `remove-${model.id}`}
                      disabled={busy !== ""}
                      onClick={() => {
                        if (
                          window.confirm(
                            t("admin.model.removeConfirm", { model: model.id }),
                          )
                        )
                          mutate(
                            `remove-${model.id}`,
                            `${base}/models/remove`,
                            {
                              method: "POST",
                              body: jsonBody({ ids: [model.id] }),
                            },
                            adminMessage("admin.model.removed"),
                          )
                      }}
                    />
                  </div>
                </div>
                <ModelTagToggles
                  tags={model.tags}
                  onToggle={(tag) => toggleTag(model, tag)}
                  disabled={busy !== ""}
                  t={t}
                />
              </li>
            )
          })}
        </ul>
      ) : (
        <EmptyState
          icon={CloudDownload}
          title={
            models.length ? t("admin.model.noMatch") : t("admin.model.empty")
          }
        >
          {models.length ? null : t("admin.model.emptyHint")}
        </EmptyState>
      )}
      <p className="adm-card-foot">
        <Route aria-hidden="true" />
        {t("admin.model.routeHint")}
        <button
          type="button"
          className="adm-link"
          onClick={() => onNavigate("detection")}
        >
          {t("admin.model.routeLink")}
        </button>
      </p>
      {dialog === "discover" ? (
        <DiscoverDialog
          provider={provider}
          onClose={() => setDialog(null)}
          onAdded={added}
          onError={onError}
          t={t}
        />
      ) : null}
      {dialog === "manual" ? (
        <ManualModelDialog
          provider={provider}
          onClose={() => setDialog(null)}
          onAdded={added}
          onError={onError}
          t={t}
        />
      ) : null}
    </Card>
  )
}

export function ProvidersView({
  data,
  onNavigate,
}: {
  data: any
  onNavigate: any
}) {
  const { locale, t } = useAppSettings()
  const { config, loadConfig, notify, fail } = data
  const providers = config?.providers || []
  const models = config?.models || []
  const [selectedId, setSelectedId] = useState(readSelectedProvider)
  const [query, setQuery] = useState("")
  const [creating, setCreating] = useState(false)
  const [busy, setBusy] = useState("")
  const [autoDiscover, setAutoDiscover] = useState("")
  const clearAutoDiscover = useCallback(() => setAutoDiscover(""), [])

  const sortedProviders = useMemo(
    () =>
      [...providers].sort(
        (left, right) =>
          Number(right.enabled) - Number(left.enabled) ||
          String(left.name).localeCompare(String(right.name)),
      ),
    [providers],
  )
  const visibleProviders = sortedProviders.filter((provider) =>
    `${provider.name} ${provider.id} ${provider.baseUrl}`
      .toLowerCase()
      .includes(query.trim().toLowerCase()),
  )
  const selected =
    providers.find((provider) => provider.id === selectedId) ||
    sortedProviders[0] ||
    null
  const selectedModels = models.filter(
    (model) => model.providerId === selected?.id,
  )

  const select = (id) => {
    setSelectedId(id)
    try {
      window.localStorage.setItem(selectedProviderKey, id)
    } catch {
      // Remembering the selection is a convenience only.
    }
  }

  const toggleEnabled = async (provider, enabled) => {
    setBusy("enabled")
    try {
      await request(`/providers/${encodeURIComponent(provider.id)}`, {
        method: "PUT",
        body: jsonBody({ enabled }),
      })
      await loadConfig()
    } catch (error) {
      fail(error)
    } finally {
      setBusy("")
    }
  }

  const remove = async (provider) => {
    if (
      !window.confirm(
        t("admin.provider.deleteConfirm", { name: provider.name }),
      )
    )
      return
    setBusy("delete")
    try {
      await request(`/providers/${encodeURIComponent(provider.id)}`, {
        method: "DELETE",
      })
      await loadConfig()
      setSelectedId("")
      notify(adminMessage("admin.provider.deleted"))
    } catch (error) {
      fail(error)
    } finally {
      setBusy("")
    }
  }

  return (
    <section className="adm-view" aria-labelledby="adm-providers-title">
      <PageHeader
        id="adm-providers-title"
        title={t("admin.provider.title")}
        description={t("admin.provider.description")}
      />
      <div className="adm-provider-layout">
        <aside
          className="adm-provider-list"
          aria-label={t("admin.provider.list")}
        >
          <label className="adm-search">
            <Search aria-hidden="true" />
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t("admin.provider.search")}
            />
          </label>
          <ul>
            {visibleProviders.map((provider) => {
              const count = models.filter(
                (model) => model.providerId === provider.id,
              ).length
              const state = providerState(provider)
              return (
                <li key={provider.id}>
                  <button
                    type="button"
                    className={cn(
                      "adm-provider-item",
                      selected?.id === provider.id && "is-active",
                    )}
                    aria-current={selected?.id === provider.id || undefined}
                    onClick={() => select(provider.id)}
                  >
                    <ProviderAvatar provider={provider} />
                    <span className="adm-provider-item-copy">
                      <strong>{provider.name}</strong>
                      <small>{t("admin.provider.modelCount", { count })}</small>
                    </span>
                    <span
                      className={cn("adm-dot", `is-${state}`)}
                      title={t(`admin.provider.health.${state}`)}
                      aria-label={t(`admin.provider.health.${state}`)}
                    />
                  </button>
                </li>
              )
            })}
          </ul>
          {!visibleProviders.length ? (
            <p className="adm-muted adm-small">{t("admin.provider.none")}</p>
          ) : null}
          <Btn
            variant="outline"
            icon={Plus}
            className="adm-provider-add"
            onClick={() => setCreating(true)}
          >
            {t("admin.provider.add")}
          </Btn>
        </aside>

        <div className="adm-provider-detail">
          {selected ? (
            <>
              <div className="adm-provider-hero">
                <ProviderAvatar provider={selected} />
                <div>
                  <h3>{selected.name}</h3>
                  <small>
                    <Server aria-hidden="true" />
                    <code>{selected.id}</code>
                    {selected.model ? (
                      <>
                        {" · "}
                        <Star aria-hidden="true" />
                        <code>{selected.model}</code>
                      </>
                    ) : null}
                  </small>
                </div>
                <div className="adm-provider-hero-actions">
                  <span className="adm-muted adm-small">
                    {selected.enabled
                      ? t("admin.provider.enabled")
                      : t("admin.provider.disabled")}
                  </span>
                  <Switch
                    checked={selected.enabled}
                    onChange={(enabled) => toggleEnabled(selected, enabled)}
                    label={t("admin.provider.enable")}
                    disabled={busy !== ""}
                  />
                  <IconBtn
                    label={t("admin.provider.delete")}
                    icon={Trash2}
                    onClick={() => remove(selected)}
                    disabled={busy !== ""}
                  />
                </div>
              </div>
              <ConnectionCard
                key={`connection-${selected.id}`}
                provider={selected}
                models={selectedModels}
                onSaved={loadConfig}
                onError={fail}
                onNotice={notify}
                t={t}
                locale={locale}
              />
              <ModelsCard
                key={`models-${selected.id}`}
                provider={selected}
                autoOpen={autoDiscover === selected.id}
                onAutoOpened={clearAutoDiscover}
                models={selectedModels}
                config={config}
                onChanged={loadConfig}
                onError={fail}
                onNotice={notify}
                onNavigate={onNavigate}
                t={t}
              />
            </>
          ) : (
            <Card>
              <EmptyState icon={Server} title={t("admin.provider.emptyTitle")}>
                {t("admin.provider.emptyHint")}
              </EmptyState>
            </Card>
          )}
          <p className="adm-security-note">
            <KeyRound aria-hidden="true" />
            {t("admin.provider.securityNote")}
          </p>
        </div>
      </div>
      {creating ? (
        <NewProviderDialog
          providers={providers}
          onClose={() => setCreating(false)}
          onError={fail}
          onCreated={async (provider) => {
            setCreating(false)
            await loadConfig()
            select(provider.id)
            setAutoDiscover(provider.id)
            notify(
              adminMessage("admin.provider.created", { name: provider.name }),
            )
          }}
          t={t}
        />
      ) : null}
    </section>
  )
}
