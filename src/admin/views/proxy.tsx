import { useEffect, useState } from "react"
import {
  Activity,
  ArrowDown,
  ArrowUp,
  KeyRound,
  ListOrdered,
  Pencil,
  Plus,
  Route,
  Save,
  Snowflake,
  TriangleAlert,
  X,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { ApiError } from "@/admin/api"
import { formatRelative } from "@/admin/format"
import type {
  OutboundProxy,
  ProxyArea,
  ProxyInput,
  ProxyRoute,
  ProxyScheme,
  ProxySettings,
} from "@/admin/types"
import {
  Badge,
  Button,
  ConfirmButton,
  Empty,
  Field,
  FieldGroup,
  IconButton,
  Loading,
  Modal,
  PageHeader,
  Panel,
  Switch,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { cn } from "@/lib/utils"

const schemes: ProxyScheme[] = ["http", "https", "socks5", "socks5h"]

/** A save keeps every stored password unless the proxy is the one edited. */
function inputOf(proxy: OutboundProxy): ProxyInput {
  return {
    id: proxy.id,
    name: proxy.name,
    scheme: proxy.scheme,
    host: proxy.host,
    port: proxy.port,
    username: proxy.username,
    enabled: proxy.enabled,
  }
}

function endpoint(proxy: Pick<OutboundProxy, "scheme" | "host" | "port">) {
  const host = proxy.host.includes(":") ? `[${proxy.host}]` : proxy.host
  return `${proxy.scheme}://${host}:${proxy.port}`
}

interface ProxyDraft {
  id?: string
  name: string
  scheme: ProxyScheme
  host: string
  port: string
  username: string
  password: string
  /** Clears the stored password instead of keeping it. */
  clearPassword: boolean
  enabled: boolean
}

const emptyDraft: ProxyDraft = {
  name: "",
  scheme: "http",
  host: "",
  port: "",
  username: "",
  password: "",
  clearPassword: false,
  enabled: true,
}

/** Adds or edits one proxy. An empty password keeps the stored one. */
function ProxyEditor({
  proxy,
  open,
  onClose,
  onSave,
}: {
  proxy: OutboundProxy | null
  open: boolean
  onClose: () => void
  onSave: (input: ProxyInput) => Promise<boolean>
}) {
  const { t } = useAppSettings()
  const [draft, setDraft] = useState<ProxyDraft>(emptyDraft)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!open) return
    setDraft(
      proxy
        ? {
            ...emptyDraft,
            id: proxy.id,
            name: proxy.name,
            scheme: proxy.scheme,
            host: proxy.host,
            port: String(proxy.port),
            username: proxy.username,
            enabled: proxy.enabled,
          }
        : emptyDraft,
    )
  }, [open, proxy])
  const set = (patch: Partial<ProxyDraft>) =>
    setDraft((current) => ({ ...current, ...patch }))
  const stored = Boolean(proxy?.hasPassword)
  const submit = async () => {
    const input: ProxyInput = {
      id: draft.id,
      name: draft.name,
      scheme: draft.scheme,
      host: draft.host.trim(),
      port: Number(draft.port),
      username: draft.username,
      enabled: draft.enabled,
    }
    if (draft.password) input.password = draft.password
    else if (draft.clearPassword || !draft.username) input.password = ""
    setBusy(true)
    const saved = await onSave(input)
    setBusy(false)
    if (saved) onClose()
  }
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={t(proxy ? "admin.proxy.editTitle" : "admin.proxy.addTitle")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("admin.action.cancel")}
          </Button>
          <Button
            type="submit"
            form="adm-proxy-form"
            variant="primary"
            icon={Save}
            busy={busy}
            disabled={!draft.host.trim() || !draft.port}
          >
            {t("admin.action.save")}
          </Button>
        </>
      }
    >
      <form
        id="adm-proxy-form"
        className="adm-form"
        onSubmit={(event) => {
          event.preventDefault()
          void submit()
        }}
      >
        <Field label={t("admin.proxy.name")}>
          <input
            value={draft.name}
            maxLength={64}
            onChange={(event) => set({ name: event.target.value })}
          />
        </Field>
        <div className="adm-form-row is-three adm-proxy-endpoint">
          <Field label={t("admin.proxy.scheme")}>
            <select
              value={draft.scheme}
              onChange={(event) =>
                set({ scheme: event.target.value as ProxyScheme })
              }
            >
              {schemes.map((scheme) => (
                <option key={scheme} value={scheme}>
                  {scheme}
                </option>
              ))}
            </select>
          </Field>
          <Field label={t("admin.proxy.host")}>
            <input
              value={draft.host}
              required
              autoComplete="off"
              spellCheck={false}
              placeholder="127.0.0.1"
              onChange={(event) => set({ host: event.target.value })}
            />
          </Field>
          <Field label={t("admin.proxy.port")}>
            <input
              value={draft.port}
              required
              inputMode="numeric"
              type="number"
              min={1}
              max={65535}
              placeholder="7890"
              onChange={(event) => set({ port: event.target.value })}
            />
          </Field>
        </div>
        <div className="adm-form-row">
          <Field label={t("admin.proxy.username")}>
            <input
              value={draft.username}
              autoComplete="off"
              spellCheck={false}
              onChange={(event) => set({ username: event.target.value })}
            />
          </Field>
          <Field
            label={t("admin.proxy.password")}
            hint={
              stored && !draft.clearPassword
                ? t(
                    proxy?.passwordUnreadable
                      ? "admin.proxy.passwordUnreadableHint"
                      : "admin.proxy.passwordKeepHint",
                  )
                : undefined
            }
          >
            <input
              type="password"
              value={draft.password}
              autoComplete="new-password"
              disabled={!draft.username}
              placeholder={
                stored && !draft.clearPassword ? "••••••••" : undefined
              }
              onChange={(event) =>
                set({ password: event.target.value, clearPassword: false })
              }
            />
          </Field>
        </div>
        {stored && draft.username ? (
          <Switch
            checked={draft.clearPassword}
            onChange={(clearPassword) => set({ clearPassword, password: "" })}
            label={t("admin.proxy.clearPassword")}
          />
        ) : null}
        <Switch
          checked={draft.enabled}
          onChange={(enabled) => set({ enabled })}
          label={t("admin.proxy.enabled")}
        />
      </form>
    </Modal>
  )
}

/** The ordered proxy queue: every routed request tries these top to bottom
 * until one answers. */
function QueuePanel({
  settings,
  save,
  onTested,
}: {
  settings: ProxySettings
  save: (proxies: ProxyInput[], success?: string) => Promise<boolean>
  /** Takes the tested proxy's updated health. */
  onTested: (proxy: OutboundProxy) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, notify, fail } = useAdmin()
  const [editing, setEditing] = useState<OutboundProxy | null>(null)
  const [adding, setAdding] = useState(false)
  const [testArea, setTestArea] = useState<ProxyArea>(
    settings.testAreas[0] ?? "youtube",
  )
  const [testing, setTesting] = useState<string | null>(null)
  const [results, setResults] = useState<Record<string, string>>({})
  const proxies = settings.proxies
  const inputs = proxies.map(inputOf)
  const move = (from: number, to: number) => {
    const next = [...inputs]
    const [item] = next.splice(from, 1)
    next.splice(to, 0, item)
    void save(next)
  }
  const test = async (proxy: OutboundProxy) => {
    setTesting(proxy.id)
    try {
      const result = await api.testProxy(proxy.id, testArea)
      const summary = result.ok
        ? t("admin.proxy.testOk", {
            status: result.status ?? 0,
            ms: result.latencyMs,
          })
        : t(`admin.proxy.testError.${result.error ?? "failed"}`)
      setResults((current) => ({ ...current, [proxy.id]: summary }))
      if (result.ok)
        notify("admin.notice.proxyTestOk", {
          name: proxy.name || endpoint(proxy),
          ms: result.latencyMs,
        })
      else fail(new ApiError(0, `proxy_test_${result.error ?? "failed"}`, ""))
      onTested(result.proxy)
    } catch (error) {
      fail(error)
    } finally {
      setTesting(null)
    }
  }
  const editorProxy = adding ? null : editing
  return (
    <Panel
      icon={ListOrdered}
      title={t("admin.proxy.queue")}
      actions={
        <Button
          size="sm"
          variant="primary"
          icon={Plus}
          disabled={proxies.length >= 16}
          onClick={() => setAdding(true)}
        >
          {t("admin.proxy.add")}
        </Button>
      }
    >
      {proxies.length ? (
        <>
          <ol className="adm-queue-list adm-proxy-list">
            {proxies.map((proxy, index) => {
              const health = proxy.health
              const issue = proxy.passwordUnreadable
              return (
                <li
                  key={proxy.id}
                  className={cn(
                    "adm-proxy-row",
                    issue && "has-issue",
                    !proxy.enabled && "is-off",
                  )}
                >
                  <span className="adm-route-rank">{index + 1}</span>
                  <span className="adm-route-model">
                    <strong>{proxy.name || endpoint(proxy)}</strong>
                    <small className="adm-proxy-endpoint-text">
                      {endpoint(proxy)}
                      {proxy.username ? ` · ${proxy.username}` : ""}
                    </small>
                    <span className="adm-chip-row adm-queue-tags">
                      {!proxy.enabled ? (
                        <Badge>{t("admin.proxy.disabled")}</Badge>
                      ) : null}
                      {issue ? (
                        <Badge tone="danger" icon={TriangleAlert}>
                          {t("admin.proxy.passwordUnreadable")}
                        </Badge>
                      ) : proxy.hasPassword ? (
                        <Badge icon={KeyRound}>
                          {t("admin.proxy.hasPassword")}
                        </Badge>
                      ) : null}
                      {health.coolingUntil ? (
                        <Badge tone="warning" icon={Snowflake}>
                          {t("admin.proxy.cooling", {
                            time: formatRelative(health.coolingUntil, locale),
                          })}
                        </Badge>
                      ) : null}
                      {health.lastSuccessAt ? (
                        <Badge tone="success">
                          {t("admin.proxy.lastSuccess", {
                            time: formatRelative(health.lastSuccessAt, locale),
                          })}
                        </Badge>
                      ) : null}
                      {health.lastFailureAt ? (
                        <Badge tone="danger">
                          {t("admin.proxy.lastFailure", {
                            time: formatRelative(health.lastFailureAt, locale),
                          })}
                        </Badge>
                      ) : null}
                      {results[proxy.id] ? (
                        <span className="adm-muted">{results[proxy.id]}</span>
                      ) : null}
                    </span>
                  </span>
                  <span className="adm-row-actions">
                    <IconButton
                      icon={Activity}
                      label={t("admin.proxy.test")}
                      busy={testing === proxy.id}
                      disabled={issue || testing !== null}
                      onClick={() => void test(proxy)}
                    />
                    <IconButton
                      icon={ArrowUp}
                      label={t("admin.route.up")}
                      disabled={index === 0}
                      onClick={() => move(index, index - 1)}
                    />
                    <IconButton
                      icon={ArrowDown}
                      label={t("admin.route.down")}
                      disabled={index === proxies.length - 1}
                      onClick={() => move(index, index + 1)}
                    />
                    <IconButton
                      icon={Pencil}
                      label={t("admin.proxy.edit")}
                      onClick={() => setEditing(proxy)}
                    />
                    <ConfirmButton
                      size="sm"
                      variant="ghost"
                      icon={X}
                      aria-label={t("admin.proxy.remove")}
                      title={t("admin.proxy.remove")}
                      onConfirm={() =>
                        void save(
                          inputs.filter((_, at) => at !== index),
                          "admin.notice.proxyRemoved",
                        )
                      }
                    />
                  </span>
                </li>
              )
            })}
          </ol>
          {settings.testAreas.length ? (
            <div className="adm-queue-foot">
              <label className="adm-proxy-test-target">
                <span>{t("admin.proxy.testTarget")}</span>
                <select
                  value={testArea}
                  onChange={(event) =>
                    setTestArea(event.target.value as ProxyArea)
                  }
                >
                  {settings.testAreas.map((area) => (
                    <option key={area} value={area}>
                      {t(`admin.proxy.area.${area}`)}
                    </option>
                  ))}
                </select>
              </label>
            </div>
          ) : null}
        </>
      ) : (
        <Empty title={t("admin.proxy.empty")}>
          {t("admin.proxy.emptyHint")}
        </Empty>
      )}
      <ProxyEditor
        open={adding || editing !== null}
        proxy={editorProxy}
        onClose={() => {
          setAdding(false)
          setEditing(null)
        }}
        onSave={(input) =>
          save(
            editorProxy
              ? inputs.map((item) => (item.id === input.id ? input : item))
              : [...inputs, input],
            "admin.notice.proxySaved",
          )
        }
      />
    </Panel>
  )
}

/** Which parts of the server's outbound requests use the queue, and with
 * which of its proxies. */
function AreasPanel({
  settings,
  saveRoutes,
}: {
  settings: ProxySettings
  saveRoutes: (
    routes: Partial<Record<ProxyArea, ProxyRoute>>,
    directFallback?: boolean,
  ) => Promise<boolean>
}) {
  const { t } = useAppSettings()
  const none = settings.proxies.length === 0
  const toggleProxy = (area: ProxyArea, id: string) => {
    const route = settings.routes[area]
    const known = new Set(settings.proxies.map((proxy) => proxy.id))
    const current = route.proxyIds.filter((value) => known.has(value))
    const selected = current.includes(id)
      ? current.filter((value) => value !== id)
      : [...current, id]
    void saveRoutes({ [area]: { ...route, proxyIds: selected } })
  }
  return (
    <Panel icon={Route} title={t("admin.proxy.areas")}>
      <div className="adm-proxy-areas">
        {settings.areas.map((area) => {
          const route = settings.routes[area]
          // A routed area whose selection has no proxy in use fails, or
          // connects directly with the fallback.
          const usable = settings.proxies.some(
            (proxy) =>
              proxy.enabled &&
              !proxy.passwordUnreadable &&
              (!route.proxyIds.length || route.proxyIds.includes(proxy.id)),
          )
          return (
            <section key={area} className="adm-proxy-area">
              <Switch
                checked={route.enabled}
                disabled={none && !route.enabled}
                onChange={(enabled) =>
                  void saveRoutes({ [area]: { ...route, enabled } })
                }
                label={t(`admin.proxy.area.${area}`)}
              />
              {route.enabled &&
              (settings.proxies.length > 1 || route.proxyIds.length > 0) ? (
                <FieldGroup
                  label={t("admin.proxy.useProxies")}
                  hint={t(
                    route.proxyIds.length
                      ? "admin.proxy.selectionHint"
                      : "admin.proxy.allHint",
                  )}
                >
                  <span className="adm-chip-row">
                    {settings.proxies.map((proxy) => {
                      const on = route.proxyIds.includes(proxy.id)
                      return (
                        <button
                          key={proxy.id}
                          type="button"
                          className={cn(
                            "adm-tag-toggle adm-tone-sky",
                            on && "is-on",
                          )}
                          aria-pressed={on}
                          onClick={() => toggleProxy(area, proxy.id)}
                        >
                          {proxy.name || endpoint(proxy)}
                        </button>
                      )
                    })}
                  </span>
                </FieldGroup>
              ) : null}
              {route.enabled && !usable ? (
                <p className="adm-inline-error">
                  <TriangleAlert aria-hidden />
                  {t(
                    settings.directFallback
                      ? "admin.proxy.noUsableDirect"
                      : "admin.proxy.noUsable",
                  )}
                </p>
              ) : null}
            </section>
          )
        })}
      </div>
      <Switch
        checked={settings.directFallback}
        onChange={(directFallback) => void saveRoutes({}, directFallback)}
        label={t("admin.proxy.directFallback")}
      />
    </Panel>
  )
}

/** The Proxies page: forward proxies for the server's own outbound requests
 * (sources, media and model providers), never for visitors. */
export function ProxyView() {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const resource = useResource(() => api.proxySettings(), [])
  const settings = resource.data
  const apply = async (
    body: Parameters<typeof api.updateProxySettings>[0],
    success?: string,
  ) => {
    const next = await act(() => api.updateProxySettings(body), success)
    if (next) resource.setData(next)
    return Boolean(next)
  }
  return (
    <div className="adm-view">
      <PageHeader title={t("admin.proxy.title")} />
      {!settings ? (
        <Loading />
      ) : (
        <div className="adm-proxy-grid">
          <QueuePanel
            settings={settings}
            save={(proxies, success) => apply({ proxies }, success)}
            onTested={(proxy) =>
              resource.setData({
                ...settings,
                proxies: settings.proxies.map((item) =>
                  item.id === proxy.id ? proxy : item,
                ),
              })
            }
          />
          <AreasPanel
            settings={settings}
            saveRoutes={(routes, directFallback) =>
              apply(
                {
                  routes: { ...settings.routes, ...routes },
                  ...(directFallback === undefined ? {} : { directFallback }),
                },
                "admin.notice.proxyRoutesSaved",
              )
            }
          />
        </div>
      )}
    </div>
  )
}
