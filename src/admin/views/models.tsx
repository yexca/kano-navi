import { useEffect, useMemo, useState } from "react"
import {
  ArrowDown,
  ArrowUp,
  Bot,
  KeyRound,
  ListPlus,
  Pencil,
  Plug,
  Plus,
  Route,
  Save,
  Search,
  ScanText,
  Trash2,
  TriangleAlert,
  X,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatRelative } from "@/admin/format"
import {
  describeTarget,
  inferenceEndpoint,
  modelTags,
  providerPresets,
  providerSlug,
} from "@/admin/llm"
import { providerIcon } from "@/admin/provider-icons"
import type {
  DiscoveredModel,
  Extraction,
  LlmCatalog,
  Provider,
  RouteId,
  RouteTarget,
} from "@/admin/types"
import {
  Badge,
  Button,
  ConfirmButton,
  Empty,
  Field,
  IconButton,
  Loading,
  Modal,
  PageHeader,
  Panel,
  StatusBadge,
  Switch,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { cn } from "@/lib/utils"

const routes: { id: RouteId; needs: "image" | "text" }[] = [
  { id: "schedule_board", needs: "image" },
  { id: "schedule_message", needs: "text" },
  { id: "schedule_vision", needs: "image" },
]

function ProviderMark({ provider }: { provider: Partial<Provider> }) {
  const icon = providerIcon(provider)
  return (
    <span className="adm-provider-mark" aria-hidden>
      {icon ? (
        <img src={icon} alt="" />
      ) : (
        (provider.name || provider.id || "?").slice(0, 1).toUpperCase()
      )}
    </span>
  )
}

function TagChips({ tags }: { tags: string[] }) {
  const { t } = useAppSettings()
  return (
    <span className="adm-chip-row">
      {modelTags
        .filter((tag) => tags.includes(tag.id))
        .map((tag) => (
          <Badge key={tag.id} tone={tag.tone as any} icon={tag.icon}>
            {t(`admin.tag.${tag.id}`)}
          </Badge>
        ))}
    </span>
  )
}

// --- extraction -------------------------------------------------------------

function ExtractionPanel() {
  const { t } = useAppSettings()
  const { api, act, start, overview } = useAdmin()
  const extraction = useResource(() => api.extraction(), [])
  const [draft, setDraft] = useState<Extraction | null>(null)
  const [keywords, setKeywords] = useState("")
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!extraction.data) return
    setDraft(extraction.data)
    setKeywords(extraction.data.keywords.join("\n"))
  }, [extraction.data])
  if (!draft || !extraction.data)
    return (
      <Panel title={t("admin.models.extraction")}>
        <Loading />
      </Panel>
    )
  const parsed = keywords
    .split(/[\n,，]/u)
    .map((item) => item.trim())
    .filter(Boolean)
  const dirty =
    draft.keywordEnabled !== extraction.data.keywordEnabled ||
    draft.visionEnabled !== extraction.data.visionEnabled ||
    draft.messageEnabled !== extraction.data.messageEnabled ||
    draft.messageAllPosts !== extraction.data.messageAllPosts ||
    parsed.join("\n") !== extraction.data.keywords.join("\n")
  const save = async () => {
    setBusy(true)
    const next = await act(
      () =>
        api.updateExtraction({
          keywordEnabled: draft.keywordEnabled,
          visionEnabled: draft.visionEnabled,
          messageEnabled: draft.messageEnabled,
          messageAllPosts: draft.messageAllPosts,
          keywords: parsed,
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
          <Badge tone={extraction.data.enabled ? "leaf" : "neutral"}>
            {t(
              extraction.data.enabled
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
        <Switch
          checked={draft.keywordEnabled}
          onChange={(keywordEnabled) => setDraft({ ...draft, keywordEnabled })}
          label={t("admin.models.stage.keyword")}
          description={t("admin.models.stage.keywordHint")}
        />
        <Switch
          checked={draft.visionEnabled}
          onChange={(visionEnabled) => setDraft({ ...draft, visionEnabled })}
          label={t("admin.models.stage.vision")}
          description={
            draft.visionEnabled && !draft.keywordEnabled
              ? t("admin.models.stage.visionNeedsKeywords")
              : t("admin.models.stage.visionHint")
          }
        />
        <Switch
          checked={draft.messageEnabled}
          onChange={(messageEnabled) => setDraft({ ...draft, messageEnabled })}
          label={t("admin.models.stage.message")}
          description={t("admin.models.stage.messageHint")}
        />
      </div>
      <Switch
        checked={draft.messageAllPosts}
        disabled={!draft.messageEnabled}
        onChange={(messageAllPosts) => setDraft({ ...draft, messageAllPosts })}
        label={t("admin.models.stage.allPosts")}
        description={t("admin.models.stage.allPostsHint")}
      />
      <Field
        label={t("admin.models.keywords")}
        hint={t("admin.models.keywordsHint", { count: parsed.length })}
      >
        <textarea
          rows={4}
          value={keywords}
          onChange={(e) => setKeywords(e.target.value)}
          placeholder={t("admin.models.keywordsPlaceholder")}
        />
      </Field>
      <div className="adm-form-actions">
        {dirty ? (
          <span className="adm-muted">{t("admin.models.unsaved")}</span>
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

// --- routes -----------------------------------------------------------------

function RouteEditor({
  route,
  needs,
  catalog,
  onSaved,
}: {
  route: RouteId
  needs: "image" | "text"
  catalog: LlmCatalog
  onSaved: (catalog: LlmCatalog) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const state = catalog.routes[route]
  const [targets, setTargets] = useState<RouteTarget[]>(state.configured)
  const [choice, setChoice] = useState("")
  const [busy, setBusy] = useState(false)
  useEffect(() => setTargets(state.configured), [state.configured])
  const dirty = JSON.stringify(targets) !== JSON.stringify(state.configured)
  const shown = targets.length || dirty ? targets : state.targets
  const fallback = !targets.length && !dirty && state.targets.length > 0
  const described = shown.map((target) =>
    describeTarget(target, catalog.providers, catalog.models),
  )
  const usable = described.filter((item) => !item.issue)
  const missingCapability =
    usable.length > 0 &&
    !usable.some((item) => item.capabilities.includes(needs))
  const candidates = catalog.models.filter(
    (model) =>
      model.enabled &&
      !targets.some(
        (target) =>
          target.providerId === model.providerId && target.modelId === model.id,
      ),
  )
  const move = (index: number, delta: number) =>
    setTargets((current) => {
      const next = [...current]
      const [item] = next.splice(index, 1)
      next.splice(index + delta, 0, item)
      return next
    })
  const save = async () => {
    setBusy(true)
    const result = await act(
      () => api.updateRoute(route, targets),
      "admin.notice.routeSaved",
    )
    setBusy(false)
    if (result) onSaved({ ...catalog, routes: result.routes })
  }
  return (
    <article className="adm-route">
      <header>
        <div>
          <h3>{t(`admin.route.${route}`)}</h3>
          <p className="adm-muted">{t(`admin.route.${route}.hint`)}</p>
        </div>
        <Badge tone={needs === "image" ? "berry" : "sky"}>
          {t(`admin.tag.${needs}`)}
        </Badge>
      </header>
      {fallback ? (
        <p className="adm-note">{t("admin.route.usesFallback")}</p>
      ) : null}
      {shown.length ? (
        <ol className="adm-route-targets">
          {described.map((item, index) => (
            <li
              key={`${shown[index].providerId}:${shown[index].modelId}`}
              className={cn(
                item.issue && "has-issue",
                fallback && "is-fallback",
              )}
            >
              <span className="adm-route-rank">{index + 1}</span>
              {item.provider ? <ProviderMark provider={item.provider} /> : null}
              <span className="adm-route-model">
                <strong>{item.modelId || t("admin.route.noDefault")}</strong>
                <small>
                  {item.provider?.name || shown[index].providerId}
                  {item.followsDefault
                    ? ` · ${t("admin.route.followsDefault")}`
                    : ""}
                </small>
              </span>
              <TagChips tags={item.tags} />
              {item.issue ? (
                <Badge tone="danger" icon={TriangleAlert}>
                  {t(`admin.route.issue.${item.issue}`)}
                </Badge>
              ) : null}
              {!fallback ? (
                <span className="adm-row-actions">
                  <IconButton
                    icon={ArrowUp}
                    label={t("admin.route.up")}
                    disabled={index === 0}
                    onClick={() => move(index, -1)}
                  />
                  <IconButton
                    icon={ArrowDown}
                    label={t("admin.route.down")}
                    disabled={index === shown.length - 1}
                    onClick={() => move(index, 1)}
                  />
                  <IconButton
                    icon={X}
                    label={t("admin.route.remove")}
                    onClick={() =>
                      setTargets(targets.filter((_, at) => at !== index))
                    }
                  />
                </span>
              ) : null}
            </li>
          ))}
        </ol>
      ) : (
        <Empty title={t("admin.route.empty")}>
          {t("admin.route.emptyHint")}
        </Empty>
      )}
      {missingCapability ? (
        <p className="adm-inline-error">
          <TriangleAlert aria-hidden />
          {t(`admin.route.needs.${needs}`)}
        </p>
      ) : null}
      <div className="adm-route-add">
        <select
          value={choice}
          onChange={(e) => setChoice(e.target.value)}
          aria-label={t("admin.route.addLabel")}
        >
          <option value="">
            {candidates.length
              ? t("admin.route.choose")
              : t("admin.route.noModels")}
          </option>
          {catalog.providers.map((provider) => {
            const models = candidates.filter(
              (model) => model.providerId === provider.id,
            )
            return models.length ? (
              <optgroup key={provider.id} label={provider.name}>
                {models.map((model) => (
                  <option
                    key={model.id}
                    value={`${provider.id}\u0000${model.id}`}
                  >
                    {model.name || model.id}
                    {model.tags.length
                      ? ` (${model.tags.map((tag) => t(`admin.tag.${tag}`)).join(", ")})`
                      : ""}
                  </option>
                ))}
              </optgroup>
            ) : null
          })}
        </select>
        <Button
          size="sm"
          icon={Plus}
          disabled={!choice}
          onClick={() => {
            const [providerId, modelId] = choice.split("\u0000")
            setTargets([...targets, { providerId, modelId }])
            setChoice("")
          }}
        >
          {t("admin.route.add")}
        </Button>
        <Button
          size="sm"
          variant="primary"
          icon={Save}
          busy={busy}
          disabled={!dirty}
          onClick={save}
        >
          {t("admin.action.save")}
        </Button>
      </div>
    </article>
  )
}

// --- providers --------------------------------------------------------------

interface ProviderDraft {
  id: string
  name: string
  baseUrl: string
  protocol: string
  apiKey: string
  clearKey: boolean
  model: string
  capabilities: string[]
  timeoutSeconds: number
  maxRetries: number
  enabled: boolean
}

function ProviderDialog({
  provider,
  catalog,
  onClose,
}: {
  provider: Provider | null
  catalog: LlmCatalog
  onClose: (saved?: boolean) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [preset, setPreset] = useState(provider ? "" : "openai")
  const [draft, setDraft] = useState<ProviderDraft>(() => ({
    id: provider?.id || "",
    name: provider?.name || "OpenAI",
    baseUrl: provider?.baseUrl || providerPresets[0].baseUrl,
    protocol: provider?.protocol || providerPresets[0].protocol,
    apiKey: "",
    clearKey: false,
    model: provider?.model || "",
    capabilities: provider?.capabilities || ["text", "image"],
    timeoutSeconds: Math.round((provider?.timeoutMs || 30000) / 1000),
    maxRetries: provider?.maxRetries ?? 2,
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
  const save = async (event) => {
    event.preventDefault()
    setBusy(true)
    const body: Record<string, any> = {
      name: draft.name,
      baseUrl: draft.baseUrl,
      protocol: draft.protocol,
      model: draft.model,
      capabilities: draft.capabilities,
      timeoutMs: draft.timeoutSeconds * 1000,
      maxRetries: draft.maxRetries,
      enabled: draft.enabled,
    }
    // A key is sent only when typed or explicitly cleared; it is never read back.
    if (draft.apiKey.trim()) body.apiKey = draft.apiKey.trim()
    else if (draft.clearKey) body.apiKey = null
    const result = await act(
      () =>
        provider
          ? api.updateProvider(provider.id, body)
          : api.createProvider({
              ...body,
              id:
                draft.id ||
                providerSlug(
                  draft.name,
                  catalog.providers.map((item) => item.id),
                ),
            }),
      "admin.notice.providerSaved",
    )
    setBusy(false)
    if (result) onClose(true)
  }
  const endpoint = inferenceEndpoint(draft.baseUrl, draft.protocol)
  return (
    <Modal
      open
      wide
      title={provider ? t("admin.provider.edit") : t("admin.provider.create")}
      onClose={() => onClose()}
    >
      <form className="adm-form" onSubmit={save}>
        {!provider ? (
          <fieldset className="adm-fieldset">
            <legend>{t("admin.provider.preset")}</legend>
            <div className="adm-preset-grid">
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
                  {item.name || t("admin.provider.custom")}
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
          <input
            type="url"
            value={draft.baseUrl}
            onChange={(e) => set({ baseUrl: e.target.value })}
            required
            placeholder="https://"
          />
        </Field>
        <Field
          label={t("admin.provider.apiKey")}
          hint={
            provider?.apiKeyConfigured
              ? t("admin.provider.apiKeyKeep")
              : t("admin.provider.apiKeyHint")
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
        <div className="adm-form-row is-three">
          <Field
            label={t("admin.provider.defaultModel")}
            hint={t("admin.provider.defaultModelHint")}
          >
            <input
              value={draft.model}
              onChange={(e) => set({ model: e.target.value })}
              maxLength={128}
            />
          </Field>
          <Field label={t("admin.provider.timeout")}>
            <input
              type="number"
              min={1}
              max={120}
              value={draft.timeoutSeconds}
              onChange={(e) => set({ timeoutSeconds: Number(e.target.value) })}
            />
          </Field>
          <Field label={t("admin.provider.retries")}>
            <input
              type="number"
              min={0}
              max={2}
              value={draft.maxRetries}
              onChange={(e) => set({ maxRetries: Number(e.target.value) })}
            />
          </Field>
        </div>
        <fieldset className="adm-fieldset is-inline">
          <legend>{t("admin.provider.capabilities")}</legend>
          {["text", "image"].map((capability) => (
            <label key={capability} className="adm-check">
              <input
                type="checkbox"
                checked={draft.capabilities.includes(capability)}
                onChange={(e) =>
                  set({
                    capabilities: e.target.checked
                      ? [...draft.capabilities, capability]
                      : draft.capabilities.filter(
                          (item) => item !== capability,
                        ),
                  })
                }
              />
              {t(`admin.tag.${capability}`)}
            </label>
          ))}
        </fieldset>
        <Switch
          checked={draft.enabled}
          onChange={(enabled) => set({ enabled })}
          label={t("admin.provider.enabled")}
        />
        <div className="adm-form-actions">
          <Button onClick={() => onClose()}>{t("admin.action.cancel")}</Button>
          <Button type="submit" variant="primary" busy={busy}>
            {t("admin.action.save")}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

function ModelsDialog({
  provider,
  catalog,
  onChanged,
  onClose,
}: {
  provider: Provider
  catalog: LlmCatalog
  onChanged: () => void
  onClose: () => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const models = catalog.models.filter(
    (model) => model.providerId === provider.id,
  )
  const [remote, setRemote] = useState<DiscoveredModel[] | null>(null)
  const [picked, setPicked] = useState<string[]>([])
  const [filter, setFilter] = useState("")
  const [manual, setManual] = useState("")
  const [busy, setBusy] = useState("")
  const run = async (
    key: string,
    work: () => Promise<unknown>,
    notice?: string,
  ) => {
    setBusy(key)
    const result = await act(work, notice)
    setBusy("")
    if (result !== undefined) onChanged()
    return result
  }
  const shownRemote = useMemo(
    () =>
      (remote || []).filter(
        (model) =>
          !model.added && model.id.toLowerCase().includes(filter.toLowerCase()),
      ),
    [remote, filter],
  )
  return (
    <Modal
      open
      wide
      title={t("admin.provider.modelsTitle", { name: provider.name })}
      onClose={onClose}
    >
      <div className="adm-form">
        <section className="adm-model-section">
          <h3>{t("admin.provider.catalog", { count: models.length })}</h3>
          {models.length ? (
            <ul className="adm-model-list">
              {models.map((model) => (
                <li
                  key={model.id}
                  className={cn(!model.enabled && "is-disabled")}
                >
                  <span className="adm-model-name">
                    <strong>{model.id}</strong>
                    <small>
                      {model.name && model.name !== model.id
                        ? `${model.name} · `
                        : ""}
                      {t(`admin.provider.origin.${model.origin}`)}
                      {provider.model === model.id
                        ? ` · ${t("admin.provider.isDefault")}`
                        : ""}
                    </small>
                  </span>
                  <span className="adm-tag-toggles">
                    {modelTags.map((tag) => {
                      const on = model.tags.includes(tag.id)
                      return (
                        <button
                          key={tag.id}
                          type="button"
                          aria-pressed={on}
                          className={cn(
                            "adm-tag-toggle",
                            `adm-tone-${tag.tone}`,
                            on && "is-on",
                          )}
                          title={t(`admin.tag.${tag.id}`)}
                          onClick={() =>
                            run(`tag:${model.id}`, () =>
                              api.updateModel(provider.id, {
                                id: model.id,
                                tags: on
                                  ? model.tags.filter((item) => item !== tag.id)
                                  : [...model.tags, tag.id],
                              }),
                            )
                          }
                        >
                          <tag.icon aria-hidden />
                          <span className="adm-visually-hidden">
                            {t(`admin.tag.${tag.id}`)}
                          </span>
                        </button>
                      )
                    })}
                  </span>
                  <span className="adm-row-actions">
                    <label
                      className="adm-check"
                      title={t("admin.provider.modelEnabled")}
                    >
                      <input
                        type="checkbox"
                        checked={model.enabled}
                        onChange={(e) =>
                          run(`on:${model.id}`, () =>
                            api.updateModel(provider.id, {
                              id: model.id,
                              enabled: e.target.checked,
                            }),
                          )
                        }
                      />
                      <span className="adm-visually-hidden">
                        {t("admin.provider.modelEnabled")}
                      </span>
                    </label>
                    {provider.model !== model.id ? (
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() =>
                          run(
                            `default:${model.id}`,
                            () =>
                              api.updateProvider(provider.id, {
                                model: model.id,
                              }),
                            "admin.notice.providerSaved",
                          )
                        }
                      >
                        {t("admin.provider.makeDefault")}
                      </Button>
                    ) : null}
                    <IconButton
                      icon={Trash2}
                      label={t("admin.provider.removeModel")}
                      busy={busy === `rm:${model.id}`}
                      onClick={() =>
                        run(
                          `rm:${model.id}`,
                          () => api.removeModels(provider.id, [model.id]),
                          "admin.notice.modelsRemoved",
                        )
                      }
                    />
                  </span>
                </li>
              ))}
            </ul>
          ) : (
            <Empty title={t("admin.provider.noModels")}>
              {t("admin.provider.noModelsHint")}
            </Empty>
          )}
          <form
            className="adm-inline-form"
            onSubmit={async (event) => {
              event.preventDefault()
              if (
                await run(
                  "manual",
                  () => api.addModels(provider.id, [{ id: manual.trim() }]),
                  "admin.notice.modelsAdded",
                )
              )
                setManual("")
            }}
          >
            <Field label={t("admin.provider.addManual")}>
              <input
                value={manual}
                onChange={(e) => setManual(e.target.value)}
                maxLength={128}
                required
              />
            </Field>
            <Button
              type="submit"
              size="sm"
              icon={Plus}
              busy={busy === "manual"}
            >
              {t("admin.provider.add")}
            </Button>
          </form>
        </section>
        <section className="adm-model-section">
          <header className="adm-section-header">
            <h3>{t("admin.provider.remote")}</h3>
            <Button
              size="sm"
              icon={Search}
              busy={busy === "discover"}
              disabled={!provider.apiKeyConfigured}
              onClick={async () => {
                setBusy("discover")
                const result = await act(() => api.discoverModels(provider.id))
                setBusy("")
                if (result) {
                  setRemote(result.models)
                  setPicked([])
                  onChanged()
                }
              }}
            >
              {t("admin.provider.discover")}
            </Button>
          </header>
          {!provider.apiKeyConfigured ? (
            <p className="adm-muted">{t("admin.provider.discoverNeedsKey")}</p>
          ) : null}
          {remote ? (
            <>
              <input
                type="search"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder={t("admin.provider.filterRemote")}
                aria-label={t("admin.provider.filterRemote")}
              />
              <ul className="adm-remote-list">
                {shownRemote.slice(0, 200).map((model) => (
                  <li key={model.id}>
                    <label className="adm-check">
                      <input
                        type="checkbox"
                        checked={picked.includes(model.id)}
                        onChange={(e) =>
                          setPicked(
                            e.target.checked
                              ? [...picked, model.id]
                              : picked.filter((id) => id !== model.id),
                          )
                        }
                      />
                      <span>
                        <strong>{model.id}</strong>
                        {model.ownedBy ? (
                          <small> · {model.ownedBy}</small>
                        ) : null}
                      </span>
                    </label>
                    <TagChips tags={model.suggestedTags} />
                  </li>
                ))}
              </ul>
              {shownRemote.length === 0 ? (
                <p className="adm-muted">{t("admin.provider.allAdded")}</p>
              ) : null}
              <div className="adm-form-actions">
                <Button
                  size="sm"
                  variant="primary"
                  icon={ListPlus}
                  disabled={!picked.length}
                  busy={busy === "add"}
                  onClick={async () => {
                    const chosen = (remote || []).filter((model) =>
                      picked.includes(model.id),
                    )
                    const result = await run(
                      "add",
                      () =>
                        api.addModels(
                          provider.id,
                          chosen.map((model) => ({
                            id: model.id,
                            name: model.name,
                            ownedBy: model.ownedBy,
                            tags: model.suggestedTags,
                          })),
                          "remote",
                        ),
                      "admin.notice.modelsAdded",
                    )
                    if (result) {
                      setRemote((current) =>
                        (current || []).map((model) =>
                          picked.includes(model.id)
                            ? { ...model, added: true }
                            : model,
                        ),
                      )
                      setPicked([])
                    }
                  }}
                >
                  {t("admin.provider.addSelected", { count: picked.length })}
                </Button>
              </div>
            </>
          ) : null}
        </section>
      </div>
    </Modal>
  )
}

function ProviderCard({
  provider,
  catalog,
  onEdit,
  onModels,
  onChanged,
}: {
  provider: Provider
  catalog: LlmCatalog
  onEdit: () => void
  onModels: () => void
  onChanged: () => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const [testing, setTesting] = useState(false)
  const count = catalog.models.filter(
    (model) => model.providerId === provider.id,
  ).length
  return (
    <article className={cn("adm-provider", !provider.enabled && "is-disabled")}>
      <header>
        <ProviderMark provider={provider} />
        <div>
          <h3>{provider.name}</h3>
          <small>{provider.baseUrl}</small>
        </div>
      </header>
      <span className="adm-chip-row">
        {!provider.enabled ? (
          <Badge>{t("admin.provider.disabled")}</Badge>
        ) : null}
        <Badge
          tone={provider.apiKeyConfigured ? "leaf" : "danger"}
          icon={KeyRound}
        >
          {t(
            provider.apiKeyConfigured
              ? "admin.provider.keySet"
              : "admin.provider.keyMissing",
          )}
        </Badge>
        <TagChips tags={provider.capabilities} />
      </span>
      <dl className="adm-facts is-compact">
        <div>
          <dt>{t("admin.provider.defaultModel")}</dt>
          <dd>{provider.model || "—"}</dd>
        </div>
        <div>
          <dt>{t("admin.provider.models")}</dt>
          <dd>{count}</dd>
        </div>
        <div>
          <dt>{t("admin.provider.lastCheck")}</dt>
          <dd>
            {provider.lastStatus ? (
              <StatusBadge
                status={
                  provider.lastStatus === "success" ? "completed" : "failed"
                }
              />
            ) : (
              "—"
            )}
            {provider.lastCheckedAt ? (
              <small> {formatRelative(provider.lastCheckedAt, locale)}</small>
            ) : null}
          </dd>
        </div>
      </dl>
      {provider.lastError ? (
        <p className="adm-error-text">{provider.lastError}</p>
      ) : null}
      <div className="adm-actions">
        <Button
          size="sm"
          icon={Plug}
          busy={testing}
          disabled={!provider.apiKeyConfigured}
          onClick={async () => {
            setTesting(true)
            await act(
              () => api.testProvider(provider.id),
              "admin.notice.providerOk",
            )
            setTesting(false)
            // The outcome is recorded on the provider either way.
            onChanged()
          }}
        >
          {t("admin.provider.test")}
        </Button>
        <Button size="sm" icon={Bot} onClick={onModels}>
          {t("admin.provider.manageModels")}
        </Button>
        <IconButton
          icon={Pencil}
          label={t("admin.provider.edit")}
          onClick={onEdit}
        />
        <ConfirmButton
          size="sm"
          variant="ghost"
          icon={Trash2}
          aria-label={t("admin.provider.delete")}
          onConfirm={async () => {
            const result = await act(
              () => api.deleteProvider(provider.id),
              "admin.notice.providerDeleted",
            )
            if (result !== undefined) onChanged()
          }}
        />
      </div>
    </article>
  )
}

export function ModelsView(_: ViewProps) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const catalog = useResource(() => api.llm(), [])
  const [editing, setEditing] = useState<Provider | null | undefined>(undefined)
  const [models, setModels] = useState("")
  const data = catalog.data
  const modelsFor = data?.providers.find((provider) => provider.id === models)
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.models.title")}
        description={t("admin.models.description")}
      />
      <ExtractionPanel />
      <Panel
        icon={Route}
        title={t("admin.models.routes")}
        description={t("admin.models.routesHint")}
      >
        {!data ? (
          <Loading />
        ) : (
          <div className="adm-route-grid">
            {routes.map((route) => (
              <RouteEditor
                key={route.id}
                route={route.id}
                needs={route.needs}
                catalog={data}
                onSaved={catalog.setData}
              />
            ))}
          </div>
        )}
      </Panel>
      <Panel
        icon={Bot}
        title={t("admin.models.providers")}
        description={t("admin.models.providersHint")}
        actions={
          <Button
            size="sm"
            icon={Plus}
            disabled={!data}
            onClick={() => setEditing(null)}
          >
            {t("admin.provider.create")}
          </Button>
        }
      >
        {!data ? (
          <Loading />
        ) : data.providers.length ? (
          <div className="adm-provider-grid">
            {data.providers.map((provider) => (
              <ProviderCard
                key={provider.id}
                provider={provider}
                catalog={data}
                onEdit={() => setEditing(provider)}
                onModels={() => setModels(provider.id)}
                onChanged={() => void catalog.reload()}
              />
            ))}
          </div>
        ) : (
          <Empty icon={Bot} title={t("admin.models.noProviders")}>
            {t("admin.models.noProvidersHint")}
          </Empty>
        )}
      </Panel>
      {editing !== undefined && data ? (
        <ProviderDialog
          provider={editing}
          catalog={data}
          onClose={(saved) => {
            setEditing(undefined)
            if (saved) void catalog.reload()
          }}
        />
      ) : null}
      {modelsFor && data ? (
        <ModelsDialog
          provider={modelsFor}
          catalog={data}
          onChanged={() => void catalog.reload()}
          onClose={() => setModels("")}
        />
      ) : null}
    </div>
  )
}
