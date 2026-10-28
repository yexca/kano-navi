import { useEffect, useMemo, useRef, useState, type SubmitEvent } from "react"
import {
  ArrowDown,
  ArrowUp,
  Bot,
  Check,
  Globe,
  GripVertical,
  KeyRound,
  Languages,
  ListPlus,
  Pencil,
  Plug,
  Plus,
  RefreshCw,
  Route,
  Save,
  Search,
  Trash2,
  TriangleAlert,
  X,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatRelative } from "@/admin/format"
import { createLatestWins } from "@/admin/latest-wins"
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
  LlmCatalog,
  Model,
  Provider,
  QueueRoute,
  RouteTarget,
} from "@/admin/types"
import {
  Badge,
  Button,
  Callout,
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
  type Tone,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { cn } from "@/lib/utils"

export function ProviderMark({ provider }: { provider: Partial<Provider> }) {
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

function TagChips({ tags, routing }: { tags: string[]; routing?: boolean }) {
  const { t } = useAppSettings()
  return (
    <span className="adm-chip-row">
      {modelTags
        .filter((tag) => tags.includes(tag.id) && (!routing || tag.routing))
        .map((tag) => (
          <Badge key={tag.id} tone={tag.tone as Tone} icon={tag.icon}>
            {t(`admin.tag.${tag.id}`)}
          </Badge>
        ))}
    </span>
  )
}

const queueKey = (route: QueueRoute) =>
  route === "translate" ? "translationQueue" : "queue"

const queueOf = (catalog: LlmCatalog, route: QueueRoute) =>
  (route === "translate" ? catalog.translationQueue : catalog.queue) || []

const sameTarget = (target: RouteTarget, providerId: string, id: string) =>
  target.providerId === providerId && target.modelId === id

/** The first queued model a stage can use: usable and carrying its tag. */
export function firstUsable(
  catalog: LlmCatalog,
  targets: RouteTarget[],
  needs: "text" | "image",
) {
  return (
    targets
      .map((target) =>
        describeTarget(target, catalog.providers, catalog.models),
      )
      .find((item) => !item.issue && item.capabilities.includes(needs)) || null
  )
}

// --- queues -----------------------------------------------------------------

/** A searchable list of enabled catalog models, opened from a button. */
function ModelPicker({
  catalog,
  targets,
  needsText,
  onPick,
}: {
  catalog: LlmCatalog
  targets: RouteTarget[]
  needsText: boolean
  onPick: (target: RouteTarget) => void
}) {
  const { t } = useAppSettings()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return undefined
    const onDown = (event: MouseEvent) => {
      if (!box.current?.contains(event.target as Node)) setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false)
    }
    document.addEventListener("mousedown", onDown)
    document.addEventListener("keydown", onKey)
    return () => {
      document.removeEventListener("mousedown", onDown)
      document.removeEventListener("keydown", onKey)
    }
  }, [open])
  const needle = query.trim().toLowerCase()
  const candidates = catalog.models.filter(
    (model) =>
      model.enabled &&
      (!needsText || model.capabilities.includes("text")) &&
      !targets.some((target) =>
        sameTarget(target, model.providerId, model.id),
      ) &&
      `${model.id} ${model.name || ""}`.toLowerCase().includes(needle),
  )
  return (
    <div className="adm-picker" ref={box}>
      <Button
        size="sm"
        icon={Plus}
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        {t("admin.route.addModel")}
      </Button>
      {open ? (
        <div className="adm-picker-menu">
          <span className="adm-input-icon">
            <Search aria-hidden />
            <input
              type="search"
              autoFocus
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t("admin.provider.filterRemote")}
              aria-label={t("admin.provider.filterRemote")}
            />
          </span>
          {candidates.length ? (
            <ul>
              {catalog.providers.map((provider) => {
                const models = candidates.filter(
                  (model) => model.providerId === provider.id,
                )
                return models.length ? (
                  <li key={provider.id}>
                    <p className="adm-picker-group">
                      <ProviderMark provider={provider} />
                      {provider.name}
                    </p>
                    {models.map((model) => (
                      <button
                        key={model.id}
                        type="button"
                        onClick={() => {
                          onPick({ providerId: provider.id, modelId: model.id })
                          setOpen(false)
                          setQuery("")
                        }}
                      >
                        <span>{model.name || model.id}</span>
                        <TagChips tags={model.tags} routing />
                      </button>
                    ))}
                  </li>
                ) : null
              })}
            </ul>
          ) : (
            <p className="adm-muted adm-picker-empty">
              {t("admin.route.noModels")}
            </p>
          )}
        </div>
      ) : null}
    </div>
  )
}

/**
 * One ordered queue: the schedule queue every extraction stage walks, or the
 * separate translation queue, whose models need the text tag. Every change
 * is saved at once.
 */
function QueueEditor({
  catalog,
  route,
  onSave,
}: {
  catalog: LlmCatalog
  route: QueueRoute
  onSave: (route: QueueRoute, targets: RouteTarget[]) => Promise<boolean>
}) {
  const { t } = useAppSettings()
  const translating = route === "translate"
  const targets = queueOf(catalog, route)
  const [dragging, setDragging] = useState<number | null>(null)
  const [over, setOver] = useState<number | null>(null)
  const described = targets.map((target) =>
    describeTarget(target, catalog.providers, catalog.models),
  )
  // Each stage calls the first usable model with the tag it needs.
  const stages = (
    translating ? (["text"] as const) : (["text", "image"] as const)
  ).map((needs) => ({
    needs,
    index: described.findIndex(
      (item) => !item.issue && item.capabilities.includes(needs),
    ),
  }))
  const save = (next: RouteTarget[]) => void onSave(route, next)
  const move = (from: number, to: number) => {
    if (from === to) return
    const next = [...targets]
    const [item] = next.splice(from, 1)
    next.splice(to, 0, item)
    save(next)
  }
  return (
    <div className="adm-queue">
      {targets.length ? (
        <ol className="adm-queue-list">
          {described.map((item, index) => {
            const target = targets[index]
            const roles = stages.filter((stage) => stage.index === index)
            return (
              <li
                key={`${target.providerId}:${target.modelId}`}
                className={cn(
                  item.issue && "has-issue",
                  roles.length && "is-used",
                  dragging === index && "is-dragging",
                  over === index && dragging !== index && "is-over",
                )}
                draggable
                onDragStart={(event) => {
                  event.dataTransfer.effectAllowed = "move"
                  setDragging(index)
                }}
                onDragOver={(event) => {
                  if (dragging === null) return
                  event.preventDefault()
                  setOver(index)
                }}
                onDrop={(event) => {
                  event.preventDefault()
                  if (dragging !== null) move(dragging, index)
                  setDragging(null)
                  setOver(null)
                }}
                onDragEnd={() => {
                  setDragging(null)
                  setOver(null)
                }}
              >
                <GripVertical className="adm-queue-grip" aria-hidden />
                <span className="adm-route-rank">{index + 1}</span>
                {item.provider ? (
                  <ProviderMark provider={item.provider} />
                ) : null}
                <span className="adm-route-model">
                  <strong>{item.model?.name || item.modelId}</strong>
                  <small>{item.provider?.name || target.providerId}</small>
                  <span className="adm-chip-row adm-queue-tags">
                    {item.issue ? (
                      <Badge tone="danger" icon={TriangleAlert}>
                        {t(`admin.route.issue.${item.issue}`)}
                      </Badge>
                    ) : roles.length ? (
                      roles.map((role) => (
                        <Badge key={role.needs} tone="leaf" icon={Check}>
                          {t(
                            translating
                              ? "admin.route.usedFor.translate"
                              : `admin.route.usedFor.${role.needs}`,
                          )}
                        </Badge>
                      ))
                    ) : (
                      <span className="adm-muted">
                        {t("admin.route.fallback")}
                      </span>
                    )}
                  </span>
                </span>
                <span className="adm-row-actions">
                  <IconButton
                    icon={ArrowUp}
                    label={t("admin.route.up")}
                    disabled={index === 0}
                    onClick={() => move(index, index - 1)}
                  />
                  <IconButton
                    icon={ArrowDown}
                    label={t("admin.route.down")}
                    disabled={index === targets.length - 1}
                    onClick={() => move(index, index + 1)}
                  />
                  <IconButton
                    icon={X}
                    label={t("admin.route.remove")}
                    onClick={() =>
                      save(targets.filter((_, at) => at !== index))
                    }
                  />
                </span>
              </li>
            )
          })}
        </ol>
      ) : (
        <Empty title={t("admin.route.empty")}>
          {t(
            translating
              ? "admin.route.translationEmptyHint"
              : "admin.route.emptyHint",
          )}
        </Empty>
      )}
      {targets.length
        ? stages
            .filter((stage) => stage.index < 0)
            .map((stage) => (
              <p key={stage.needs} className="adm-inline-error">
                <TriangleAlert aria-hidden />
                {t(
                  translating
                    ? "admin.route.translationNeedsText"
                    : `admin.route.needs.${stage.needs}`,
                )}
              </p>
            ))
        : null}
      <div className="adm-queue-foot">
        <ModelPicker
          catalog={catalog}
          targets={targets}
          needsText={translating}
          onPick={(target) => save([...targets, target])}
        />
        {targets.length > 1 ? (
          <span className="adm-muted">{t("admin.route.dragHint")}</span>
        ) : null}
      </div>
    </div>
  )
}

// --- providers --------------------------------------------------------------

interface ProviderDraft {
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

/** A provider's connection: create one from a preset, or edit a stored one. */
function ProviderDialog({
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
  const save = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
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
        </details>
        <Switch
          checked={draft.enabled}
          onChange={(enabled) => set({ enabled })}
          label={t("admin.provider.enabled")}
        />
        {!provider ? (
          <p className="adm-field-hint">{t("admin.provider.createNext")}</p>
        ) : null}
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

/** A provider's health in one tone: off, missing its key, failing or fine. */
function providerState(provider: Provider): { tone: Tone; key: string } {
  if (!provider.enabled) return { tone: "neutral", key: "disabled" }
  if (!provider.apiKeyConfigured) return { tone: "danger", key: "keyMissing" }
  if (provider.lastStatus && provider.lastStatus !== "success")
    return { tone: "danger", key: "failing" }
  if (provider.lastStatus === "success") return { tone: "success", key: "ok" }
  return { tone: "neutral", key: "unchecked" }
}

/** One catalog model: its tags, whether it is on, and which queues hold it. */
function ModelRow({
  provider,
  model,
  catalog,
  onChanged,
  onQueue,
}: {
  provider: Provider
  model: Model
  catalog: LlmCatalog
  onChanged: () => void
  onQueue: (route: QueueRoute, targets: RouteTarget[]) => Promise<boolean>
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
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
  }
  const queued = (route: QueueRoute) =>
    queueOf(catalog, route).some((target) =>
      sameTarget(target, provider.id, model.id),
    )
  const toggleQueue = (route: QueueRoute) => {
    const targets = queueOf(catalog, route)
    void onQueue(
      route,
      queued(route)
        ? targets.filter((target) => !sameTarget(target, provider.id, model.id))
        : [...targets, { providerId: provider.id, modelId: model.id }],
    )
  }
  const text = model.capabilities.includes("text")
  return (
    <li className={cn("adm-model-row", !model.enabled && "is-disabled")}>
      <span className="adm-model-name">
        <strong>{model.name || model.id}</strong>
        <small>
          {model.name && model.name !== model.id ? `${model.id} · ` : ""}
          {t(`admin.provider.origin.${model.origin}`)}
        </small>
      </span>
      <span className="adm-tag-toggles" aria-label={t("admin.provider.tags")}>
        {modelTags.map((tag) => {
          const on = model.tags.includes(tag.id)
          return (
            <button
              key={tag.id}
              type="button"
              aria-pressed={on}
              disabled={busy !== ""}
              className={cn(
                "adm-tag-toggle",
                `adm-tone-${tag.tone}`,
                on && "is-on",
              )}
              title={t(`admin.tag.${tag.id}`)}
              onClick={() =>
                run(`tag:${tag.id}`, () =>
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
              <span>{t(`admin.tag.${tag.id}`)}</span>
            </button>
          )
        })}
      </span>
      <span className="adm-model-queues">
        {(["schedule", "translate"] as const).map((route) => (
          <button
            key={route}
            type="button"
            className="adm-queue-pick"
            aria-pressed={queued(route)}
            disabled={
              !model.enabled ||
              (route === "translate" && !text && !queued(route))
            }
            title={t(`admin.provider.queueHint.${route}`)}
            onClick={() => toggleQueue(route)}
          >
            {route === "translate" ? (
              <Languages aria-hidden />
            ) : (
              <Route aria-hidden />
            )}
            <span>{t(`admin.provider.queueName.${route}`)}</span>
          </button>
        ))}
      </span>
      <span className="adm-row-actions">
        <Switch
          compact
          checked={model.enabled}
          disabled={busy !== ""}
          onChange={(enabled) =>
            run("enabled", () =>
              api.updateModel(provider.id, { id: model.id, enabled }),
            )
          }
          label={t("admin.provider.modelEnabled")}
        />
        <IconButton
          icon={Plug}
          label={t("admin.provider.testModel")}
          busy={busy === "test"}
          disabled={!provider.apiKeyConfigured || busy !== ""}
          onClick={() =>
            run(
              "test",
              () => api.testProvider(provider.id, model.id),
              "admin.notice.providerOk",
            )
          }
        />
        <IconButton
          icon={Trash2}
          label={t("admin.provider.removeModel")}
          busy={busy === "rm"}
          disabled={busy !== ""}
          onClick={() =>
            run(
              "rm",
              () => api.removeModels(provider.id, [model.id]),
              "admin.notice.modelsRemoved",
            )
          }
        />
      </span>
    </li>
  )
}

/** The provider's own model list, fetched on demand, and adding by ID. */
function AddModels({
  provider,
  discoverNow,
  onChanged,
}: {
  provider: Provider
  discoverNow: boolean
  onChanged: () => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [remote, setRemote] = useState<DiscoveredModel[] | null>(null)
  const [picked, setPicked] = useState<string[]>([])
  const [filter, setFilter] = useState("")
  const [manual, setManual] = useState("")
  const [busy, setBusy] = useState("")
  const discover = async () => {
    setBusy("discover")
    const result = await act(() => api.discoverModels(provider.id))
    setBusy("")
    if (!result) return
    setRemote(result.models)
    setPicked([])
    onChanged()
  }
  // A provider just created with a key lists its models straight away.
  const started = useRef(false)
  useEffect(() => {
    if (!discoverNow || started.current || !provider.apiKeyConfigured) return
    started.current = true
    void discover()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [discoverNow, provider.apiKeyConfigured])
  const shown = useMemo(
    () =>
      (remote || []).filter(
        (model) =>
          !model.added &&
          model.id.toLowerCase().includes(filter.trim().toLowerCase()),
      ),
    [remote, filter],
  )
  const add = async () => {
    const chosen = (remote || []).filter((model) => picked.includes(model.id))
    setBusy("add")
    const result = await act(
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
    setBusy("")
    if (!result) return
    setRemote((current) =>
      (current || []).map((model) =>
        picked.includes(model.id) ? { ...model, added: true } : model,
      ),
    )
    setPicked([])
    onChanged()
  }
  return (
    <section className="adm-add-models">
      <header className="adm-section-header">
        <h4>{t("admin.provider.addModels")}</h4>
        <Button
          size="sm"
          icon={remote ? RefreshCw : Search}
          busy={busy === "discover"}
          disabled={!provider.apiKeyConfigured || busy !== ""}
          onClick={discover}
        >
          {t(remote ? "admin.provider.refetch" : "admin.provider.discover")}
        </Button>
      </header>
      {!provider.apiKeyConfigured ? (
        <p className="adm-muted">{t("admin.provider.discoverNeedsKey")}</p>
      ) : null}
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
          {shown.length ? (
            <ul className="adm-discovered-list">
              {shown.slice(0, 200).map((model) => (
                <li
                  key={model.id}
                  className={cn(picked.includes(model.id) && "is-picked")}
                >
                  <label className="adm-check">
                    <input
                      type="checkbox"
                      checked={picked.includes(model.id)}
                      onChange={(e) =>
                        setPicked((current) =>
                          e.target.checked
                            ? [...current, model.id]
                            : current.filter((id) => id !== model.id),
                        )
                      }
                    />
                    <span className="adm-model-name">
                      <strong>{model.id}</strong>
                      {model.ownedBy ? <small>{model.ownedBy}</small> : null}
                    </span>
                  </label>
                  <TagChips tags={model.suggestedTags} />
                </li>
              ))}
            </ul>
          ) : (
            <p className="adm-muted">
              {filter
                ? t("admin.provider.noMatch")
                : t("admin.provider.allAdded")}
            </p>
          )}
          <div className="adm-form-actions">
            <span className="adm-muted">
              {t("admin.provider.listed", {
                count: remote.length,
                added: remote.filter((model) => model.added).length,
              })}
            </span>
            <Button
              size="sm"
              variant="primary"
              icon={ListPlus}
              disabled={!picked.length}
              busy={busy === "add"}
              onClick={add}
            >
              {t("admin.provider.addSelected", { count: picked.length })}
            </Button>
          </div>
        </div>
      ) : null}
      <form
        className="adm-inline-form"
        onSubmit={async (event) => {
          event.preventDefault()
          setBusy("manual")
          const result = await act(
            () => api.addModels(provider.id, [{ id: manual.trim() }]),
            "admin.notice.modelsAdded",
          )
          setBusy("")
          if (result) {
            setManual("")
            onChanged()
          }
        }}
      >
        <Field label={t("admin.provider.addManual")}>
          <input
            value={manual}
            onChange={(e) => setManual(e.target.value)}
            maxLength={128}
            placeholder={t("admin.provider.addManualHint")}
            required
          />
        </Field>
        <Button type="submit" size="sm" icon={Plus} busy={busy === "manual"}>
          {t("admin.provider.add")}
        </Button>
      </form>
    </section>
  )
}

function ProviderDetail({
  provider,
  catalog,
  discoverNow,
  onEdit,
  onChanged,
  onDeleted,
  onQueue,
}: {
  provider: Provider
  catalog: LlmCatalog
  discoverNow: boolean
  onEdit: () => void
  onChanged: () => void
  onDeleted: () => void
  onQueue: (route: QueueRoute, targets: RouteTarget[]) => Promise<boolean>
}) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const [testing, setTesting] = useState(false)
  const models = catalog.models.filter(
    (model) => model.providerId === provider.id,
  )
  const state = providerState(provider)
  return (
    <div className="adm-provider-detail">
      <header className="adm-provider-head">
        <ProviderMark provider={provider} />
        <div>
          <h3>{provider.name}</h3>
          <small>{provider.baseUrl}</small>
        </div>
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
          <Button size="sm" icon={Pencil} onClick={onEdit}>
            {t("admin.provider.editShort")}
          </Button>
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
              if (result !== undefined) onDeleted()
            }}
          />
        </div>
      </header>
      <div className="adm-chip-row">
        <Badge tone={state.tone}>
          {t(`admin.provider.state.${state.key}`)}
        </Badge>
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
        {provider.lastCheckedAt ? (
          <span className="adm-muted adm-provider-checked">
            {t("admin.provider.checked", {
              time: formatRelative(provider.lastCheckedAt, locale),
            })}
            {provider.lastStatus ? (
              <StatusBadge
                status={
                  provider.lastStatus === "success" ? "completed" : "failed"
                }
              />
            ) : null}
          </span>
        ) : null}
      </div>
      {provider.lastError ? (
        <Callout
          tone="danger"
          icon={TriangleAlert}
          title={t("admin.provider.lastError")}
        >
          <p className="adm-error-text">{provider.lastError}</p>
        </Callout>
      ) : null}
      <section className="adm-provider-models">
        <header className="adm-section-header">
          <h4>{t("admin.provider.catalog", { count: models.length })}</h4>
        </header>
        {models.length ? (
          <ul className="adm-model-rows">
            {models.map((model) => (
              <ModelRow
                key={model.id}
                provider={provider}
                model={model}
                catalog={catalog}
                onChanged={onChanged}
                onQueue={onQueue}
              />
            ))}
          </ul>
        ) : (
          <Empty title={t("admin.provider.noModels")}>
            {t("admin.provider.noModelsHint")}
          </Empty>
        )}
      </section>
      <AddModels
        key={provider.id}
        provider={provider}
        discoverNow={discoverNow}
        onChanged={onChanged}
      />
    </div>
  )
}

function ProviderList({
  catalog,
  selected,
  onSelect,
}: {
  catalog: LlmCatalog
  selected: string
  onSelect: (id: string) => void
}) {
  const { t } = useAppSettings()
  return (
    <ul className="adm-provider-list">
      {catalog.providers.map((provider) => {
        const state = providerState(provider)
        const count = catalog.models.filter(
          (model) => model.providerId === provider.id,
        ).length
        return (
          <li key={provider.id}>
            <button
              type="button"
              aria-pressed={selected === provider.id}
              className={cn(
                selected === provider.id && "is-selected",
                !provider.enabled && "is-disabled",
              )}
              onClick={() => onSelect(provider.id)}
            >
              <ProviderMark provider={provider} />
              <span className="adm-provider-list-name">
                <strong>{provider.name}</strong>
                <small>
                  {t("admin.provider.modelCount", { count })}
                  {" · "}
                  {t(`admin.provider.state.${state.key}`)}
                </small>
              </span>
              <i
                className={cn("adm-dot", `adm-tone-${state.tone}`)}
                aria-hidden
              />
            </button>
          </li>
        )
      })}
    </ul>
  )
}

export function LlmView({ tab, navigate }: ViewProps) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const catalog = useResource(() => api.llm(), [])
  const [editing, setEditing] = useState<Provider | null | undefined>(undefined)
  const [created, setCreated] = useState("")
  // Queue edits show at once as drafts over the last server-confirmed
  // catalog. Each route sends one save at a time, latest edit first; a
  // refusal drops the draft, which rolls the queue back to what was stored.
  const [drafts, setDrafts] = useState<
    Partial<Record<QueueRoute, RouteTarget[]>>
  >({})
  const latest = useRef({ api, act, setData: catalog.setData })
  latest.current = { api, act, setData: catalog.setData }
  const [sendQueue] = useState(() =>
    createLatestWins<QueueRoute, RouteTarget[]>({
      send: async (route, targets) =>
        (
          await latest.current.act(
            () => latest.current.api.updateQueue(targets, route),
            "admin.notice.queueSaved",
          )
        )?.queue,
      confirmed: (route, queue) =>
        latest.current.setData(
          (current) => current && { ...current, [queueKey(route)]: queue },
        ),
      settled: (route) =>
        setDrafts((current) => {
          const next = { ...current }
          delete next[route]
          return next
        }),
    }),
  )
  const saveQueue = (route: QueueRoute, targets: RouteTarget[]) => {
    setDrafts((current) => ({ ...current, [route]: targets }))
    return sendQueue(route, targets)
  }
  const data = useMemo(
    () =>
      catalog.data && {
        ...catalog.data,
        ...(drafts.schedule ? { queue: drafts.schedule } : {}),
        ...(drafts.translate ? { translationQueue: drafts.translate } : {}),
      },
    [catalog.data, drafts],
  )
  const selected =
    data?.providers.find((provider) => provider.id === tab) ||
    data?.providers[0]
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.llm.title")}
        description={t("admin.llm.description")}
      />
      <Panel
        icon={Route}
        title={t("admin.llm.queues")}
        description={t("admin.models.routesHint")}
      >
        {!data ? (
          <Loading />
        ) : (
          <div className="adm-queues">
            <section>
              <h3>
                <Route aria-hidden />
                {t("admin.llm.queue.schedule")}
              </h3>
              <QueueEditor catalog={data} route="schedule" onSave={saveQueue} />
            </section>
            <section>
              <h3>
                <Languages aria-hidden />
                {t("admin.models.translation")}
              </h3>
              <p className="adm-muted">{t("admin.models.translationHint")}</p>
              <QueueEditor
                catalog={data}
                route="translate"
                onSave={saveQueue}
              />
            </section>
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
        ) : selected ? (
          <div className="adm-providers">
            <ProviderList
              catalog={data}
              selected={selected.id}
              onSelect={(id) => navigate("llm", id)}
            />
            <ProviderDetail
              key={selected.id}
              provider={selected}
              catalog={data}
              discoverNow={created === selected.id}
              onEdit={() => setEditing(selected)}
              onChanged={() => void catalog.reload()}
              onDeleted={() => {
                navigate("llm")
                void catalog.reload()
              }}
              onQueue={saveQueue}
            />
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
          onClose={() => setEditing(undefined)}
          onSaved={(provider, isNew) => {
            setEditing(undefined)
            if (isNew) {
              setCreated(provider.id)
              navigate("llm", provider.id)
            }
            void catalog.reload()
          }}
        />
      ) : null}
    </div>
  )
}
