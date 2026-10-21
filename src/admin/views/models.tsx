import { useEffect, useMemo, useRef, useState } from "react"
import {
  ArrowDown,
  ArrowUp,
  Bot,
  Check,
  Globe,
  KeyRound,
  ListOrdered,
  ListPlus,
  Pencil,
  Plug,
  Plus,
  RefreshCw,
  Route,
  Save,
  Search,
  ScanText,
  SlidersHorizontal,
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

// --- queue ------------------------------------------------------------------

function QueueEditor({
  catalog,
  onSaved,
}: {
  catalog: LlmCatalog
  onSaved: (catalog: LlmCatalog) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [targets, setTargets] = useState<RouteTarget[]>(catalog.queue)
  const [choice, setChoice] = useState("")
  const [busy, setBusy] = useState(false)
  useEffect(() => setTargets(catalog.queue), [catalog.queue])
  const dirty = JSON.stringify(targets) !== JSON.stringify(catalog.queue)
  const described = targets.map((target) =>
    describeTarget(target, catalog.providers, catalog.models),
  )
  const usable = described.filter((item) => !item.issue)
  // Each stage picks the first usable model with the capability it needs.
  const missing = (["text", "image"] as const).filter(
    (needs) =>
      usable.length > 0 &&
      !usable.some((item) => item.capabilities.includes(needs)),
  )
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
      () => api.updateQueue(targets),
      "admin.notice.queueSaved",
    )
    setBusy(false)
    if (result) onSaved({ ...catalog, queue: result.queue })
  }
  return (
    <div className="adm-route">
      {targets.length ? (
        <ol className="adm-route-targets">
          {described.map((item, index) => (
            <li
              key={`${targets[index].providerId}:${targets[index].modelId}`}
              className={cn(item.issue && "has-issue")}
            >
              <span className="adm-route-rank">{index + 1}</span>
              {item.provider ? <ProviderMark provider={item.provider} /> : null}
              <span className="adm-route-model">
                <strong>{item.modelId}</strong>
                <small>
                  {item.provider?.name || targets[index].providerId}
                </small>
              </span>
              <TagChips tags={item.tags} />
              {item.issue ? (
                <Badge tone="danger" icon={TriangleAlert}>
                  {t(`admin.route.issue.${item.issue}`)}
                </Badge>
              ) : null}
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
                  disabled={index === targets.length - 1}
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
            </li>
          ))}
        </ol>
      ) : (
        <Empty title={t("admin.route.empty")}>
          {t("admin.route.emptyHint")}
        </Empty>
      )}
      {missing.map((needs) => (
        <p key={needs} className="adm-inline-error">
          <TriangleAlert aria-hidden />
          {t(`admin.route.needs.${needs}`)}
        </p>
      ))}
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
    </div>
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
  timeoutSeconds: number
  maxRetries: number
  enabled: boolean
}

function presetHost(baseUrl: string) {
  try {
    return new URL(baseUrl).host
  } catch {
    return ""
  }
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
  const form = useRef<HTMLFormElement>(null)
  const [preset, setPreset] = useState(provider ? "" : "openai")
  const [draft, setDraft] = useState<ProviderDraft>(() => ({
    id: provider?.id || "",
    name: provider?.name || "OpenAI",
    baseUrl: provider?.baseUrl || providerPresets[0].baseUrl,
    protocol: provider?.protocol || providerPresets[0].protocol,
    apiKey: "",
    clearKey: false,
    timeoutSeconds: Math.round((provider?.timeoutMs || 30000) / 1000),
    maxRetries: provider?.maxRetries ?? 2,
    enabled: provider?.enabled ?? true,
  }))
  // The last stored row. Fetching models saves a new provider first, so
  // later saves in the same dialog update that row instead of adding another.
  const [stored, setStored] = useState<Provider | null>(provider)
  const [persisted, setPersisted] = useState(false)
  const [remote, setRemote] = useState<DiscoveredModel[] | null>(null)
  const [picked, setPicked] = useState<string[]>([])
  // Models chosen for the extraction queue; each is called by its own ID.
  const [queued, setQueued] = useState<string[]>([])
  const [filter, setFilter] = useState("")
  const [busy, setBusy] = useState("")
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
  const persist = async (notice?: string) => {
    const body: Record<string, any> = {
      name: draft.name,
      baseUrl: draft.baseUrl,
      protocol: draft.protocol,
      timeoutMs: draft.timeoutSeconds * 1000,
      maxRetries: draft.maxRetries,
      enabled: draft.enabled,
    }
    // A key is sent only when typed or explicitly cleared; it is never read back.
    if (draft.apiKey.trim()) body.apiKey = draft.apiKey.trim()
    else if (draft.clearKey) body.apiKey = null
    const result = await act(
      () =>
        stored
          ? api.updateProvider(stored.id, body)
          : api.createProvider({
              ...body,
              id:
                draft.id ||
                providerSlug(
                  draft.name,
                  catalog.providers.map((item) => item.id),
                ),
            }),
      notice,
    )
    if (!result) return null
    setStored(result.provider)
    setPersisted(true)
    set({ apiKey: "", clearKey: false })
    return result.provider
  }
  const keyState = draft.apiKey.trim()
    ? "keyReady"
    : stored?.apiKeyConfigured && !draft.clearKey
      ? "keySet"
      : "keyMissing"
  const hasKey = keyState !== "keyMissing"
  const fetchModels = async () => {
    if (!form.current?.reportValidity()) return
    setBusy("discover")
    const saved = await persist()
    const result = saved
      ? await act(() => api.discoverModels(saved.id))
      : undefined
    setBusy("")
    if (!result) return
    setStored(result.provider)
    setRemote(result.models)
    setPicked([])
    setQueued([])
  }
  const save = async (event) => {
    event.preventDefault()
    setBusy("save")
    const saved = await persist("admin.notice.providerSaved")
    const chosen = (remote || []).filter((model) => picked.includes(model.id))
    const added =
      saved && chosen.length
        ? await act(
            () =>
              api.addModels(
                saved.id,
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
        : true
    const queuedNow = new Set(
      catalog.queue
        .filter((target) => target.providerId === saved?.id)
        .map((target) => target.modelId),
    )
    const joining = queued.filter((id) => !queuedNow.has(id))
    const lined =
      saved && added && joining.length
        ? await act(
            () =>
              api.updateQueue([
                ...catalog.queue,
                ...joining.map((modelId) => ({
                  providerId: saved.id,
                  modelId,
                })),
              ]),
            "admin.notice.queueSaved",
          )
        : true
    setBusy("")
    if (saved && added && lined) onClose(true)
  }
  const inQueue = (id: string) =>
    catalog.queue.some(
      (target) => target.providerId === stored?.id && target.modelId === id,
    )
  const shownRemote = useMemo(
    () =>
      (remote || []).filter((model) =>
        model.id.toLowerCase().includes(filter.trim().toLowerCase()),
      ),
    [remote, filter],
  )
  const without = (list: string[], id: string) =>
    list.filter((item) => item !== id)
  const toggle = (id: string, on: boolean) => {
    setPicked((current) => (on ? [...current, id] : without(current, id)))
    // A model that is not added cannot be queued.
    if (!on) setQueued((current) => without(current, id))
  }
  const toggleQueue = (model: DiscoveredModel) => {
    const on = !queued.includes(model.id)
    setQueued((current) =>
      on ? [...current, model.id] : without(current, model.id),
    )
    if (on && !model.added && !picked.includes(model.id))
      setPicked((current) => [...current, model.id])
  }
  const endpoint = inferenceEndpoint(draft.baseUrl, draft.protocol)
  const identity = {
    id: stored?.id || preset || draft.name,
    name: draft.name || "?",
    baseUrl: draft.baseUrl,
  }
  return (
    <Modal
      open
      className="adm-provider-modal"
      title={provider ? t("admin.provider.edit") : t("admin.provider.create")}
      onClose={() => onClose(persisted)}
    >
      <form ref={form} className="adm-provider-form" onSubmit={save}>
        <aside className="adm-provider-side">
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
                    disabled={Boolean(stored)}
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
          <div className="adm-provider-identity">
            <ProviderMark provider={identity} />
            <div>
              <strong>{draft.name || t("admin.provider.unnamed")}</strong>
              <small>{presetHost(draft.baseUrl) || "—"}</small>
            </div>
            <span className="adm-chip-row">
              <Badge tone={hasKey ? "leaf" : "danger"} icon={KeyRound}>
                {t(`admin.provider.${keyState}`)}
              </Badge>
              {stored ? (
                <Badge>
                  {t("admin.provider.modelCount", {
                    count: catalog.models.filter(
                      (model) => model.providerId === stored.id,
                    ).length,
                  })}
                </Badge>
              ) : null}
            </span>
          </div>
        </aside>
        <div className="adm-provider-main">
          <section className="adm-form-section">
            <h3>
              <Plug aria-hidden />
              {t("admin.provider.connection")}
            </h3>
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
                endpoint
                  ? t("admin.provider.endpoint", { endpoint })
                  : undefined
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
                stored?.apiKeyConfigured
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
                  onChange={(e) =>
                    set({ apiKey: e.target.value, clearKey: false })
                  }
                  maxLength={500}
                />
              </span>
            </Field>
            {stored?.apiKeyConfigured ? (
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
          </section>
          <section className="adm-form-section">
            <header className="adm-section-header">
              <h3>
                <Bot aria-hidden />
                {t("admin.provider.models")}
              </h3>
              <Button
                size="sm"
                icon={remote ? RefreshCw : Search}
                busy={busy === "discover"}
                disabled={!hasKey || Boolean(busy)}
                onClick={fetchModels}
              >
                {t(
                  remote ? "admin.provider.refetch" : "admin.provider.discover",
                )}
              </Button>
            </header>
            <p className="adm-field-hint">
              {hasKey
                ? t("admin.provider.discoverSaves")
                : t("admin.provider.discoverEnterKey")}
            </p>
            {remote ? (
              <div className="adm-discovered">
                <span className="adm-input-icon">
                  <Search aria-hidden />
                  <input
                    type="search"
                    value={filter}
                    onChange={(e) => setFilter(e.target.value)}
                    placeholder={t("admin.provider.filterRemote")}
                    aria-label={t("admin.provider.filterRemote")}
                  />
                </span>
                <ul className="adm-discovered-list">
                  {shownRemote.slice(0, 200).map((model) => {
                    const lined = inQueue(model.id)
                    const queuing = lined || queued.includes(model.id)
                    return (
                      <li key={model.id} className={cn(queuing && "is-queued")}>
                        <label className="adm-check">
                          <input
                            type="checkbox"
                            checked={model.added || picked.includes(model.id)}
                            disabled={model.added}
                            onChange={(e) => toggle(model.id, e.target.checked)}
                          />
                          <span className="adm-model-name">
                            <strong>{model.id}</strong>
                            <small>
                              {[
                                model.ownedBy,
                                model.added
                                  ? t("admin.provider.inCatalog")
                                  : "",
                              ]
                                .filter(Boolean)
                                .join(" · ")}
                            </small>
                          </span>
                        </label>
                        <TagChips tags={model.tags || model.suggestedTags} />
                        <button
                          type="button"
                          className="adm-queue-pick"
                          aria-pressed={queuing}
                          disabled={lined}
                          title={t("admin.provider.queueHint")}
                          onClick={() => toggleQueue(model)}
                        >
                          <ListOrdered aria-hidden />
                          <span>
                            {lined
                              ? t("admin.provider.inQueue")
                              : t("admin.provider.queue")}
                          </span>
                        </button>
                      </li>
                    )
                  })}
                </ul>
                <p className="adm-discovered-summary">
                  {shownRemote.length
                    ? t("admin.provider.discoveredCount", {
                        count: remote.length,
                        picked: picked.length,
                        queued: queued.length,
                      })
                    : t("admin.provider.noMatch")}
                </p>
              </div>
            ) : null}
          </section>
          <section className="adm-form-section">
            <h3>
              <SlidersHorizontal aria-hidden />
              {t("admin.provider.requests")}
            </h3>
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
            <Switch
              checked={draft.enabled}
              onChange={(enabled) => set({ enabled })}
              label={t("admin.provider.enabled")}
            />
          </section>
          <div className="adm-form-actions adm-provider-actions">
            {picked.length || queued.length ? (
              <span className="adm-muted">
                {t("admin.provider.onSave", {
                  added: picked.length,
                  queued: queued.length,
                })}
              </span>
            ) : null}
            <Button onClick={() => onClose(persisted)}>
              {t("admin.action.cancel")}
            </Button>
            <Button
              type="submit"
              variant="primary"
              icon={Save}
              busy={busy === "save"}
              disabled={Boolean(busy)}
            >
              {t("admin.action.save")}
            </Button>
          </div>
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
  const queuedIds = new Set(
    catalog.queue
      .filter((target) => target.providerId === provider.id)
      .map((target) => target.modelId),
  )
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
                    <button
                      type="button"
                      className="adm-queue-pick"
                      aria-pressed={queuedIds.has(model.id)}
                      title={t("admin.provider.queueHint")}
                      disabled={Boolean(busy)}
                      onClick={() =>
                        run(
                          `queue:${model.id}`,
                          () =>
                            api.updateQueue(
                              queuedIds.has(model.id)
                                ? catalog.queue.filter(
                                    (target) =>
                                      target.providerId !== provider.id ||
                                      target.modelId !== model.id,
                                  )
                                : [
                                    ...catalog.queue,
                                    {
                                      providerId: provider.id,
                                      modelId: model.id,
                                    },
                                  ],
                            ),
                          "admin.notice.queueSaved",
                        )
                      }
                    >
                      <ListOrdered aria-hidden />
                      <span>
                        {queuedIds.has(model.id)
                          ? t("admin.provider.inQueue")
                          : t("admin.provider.queue")}
                      </span>
                    </button>
                    <IconButton
                      icon={Plug}
                      label={t("admin.provider.testModel")}
                      busy={busy === `test:${model.id}`}
                      disabled={!provider.apiKeyConfigured}
                      onClick={() =>
                        run(
                          `test:${model.id}`,
                          () => api.testProvider(provider.id, model.id),
                          "admin.notice.providerOk",
                        )
                      }
                    />
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
  const queued = catalog.queue.filter(
    (target) => target.providerId === provider.id,
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
      </span>
      <dl className="adm-facts is-compact">
        <div>
          <dt>{t("admin.provider.models")}</dt>
          <dd>{count}</dd>
        </div>
        <div>
          <dt>{t("admin.provider.queued")}</dt>
          <dd>{queued}</dd>
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
          <QueueEditor catalog={data} onSaved={catalog.setData} />
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
