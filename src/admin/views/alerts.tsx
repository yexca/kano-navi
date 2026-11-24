import { useEffect, useState } from "react"
import {
  Activity,
  BellRing,
  Check,
  HeartPulse,
  History,
  ListChecks,
  Save,
  Siren,
  Undo2,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { formatRelative, formatTime } from "@/admin/format"
import type {
  AlertChannel,
  AlertDelivery,
  AlertHeartbeat,
  AlertIncident,
  AlertRule,
  AlertRuleSetting,
  AlertSettings,
  AlertSeverity,
} from "@/admin/types"
import {
  Badge,
  Button,
  Empty,
  Field,
  IconButton,
  Loading,
  PageHeader,
  Panel,
  Switch,
  type Tone,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { testSummary, ChannelsPanel } from "@/admin/views/alert-channels"
import { cn } from "@/lib/utils"

const severities: AlertSeverity[] = ["info", "warning", "critical"]

const severityTone: Record<AlertSeverity, Tone> = {
  info: "sky",
  warning: "warning",
  critical: "danger",
}

const stateTone: Record<AlertSettings["state"], Tone> = {
  on: "success",
  development: "honey",
  off: "neutral",
}

const deliveryTone: Record<AlertDelivery["status"], Tone> = {
  pending: "running",
  sent: "success",
  failed: "danger",
  dropped: "neutral",
}

function RuleRow({
  rule,
  save,
}: {
  rule: AlertRule
  save: (setting: AlertRuleSetting) => Promise<boolean>
}) {
  const { t } = useAppSettings()
  const setting = rule.setting
  const [threshold, setThreshold] = useState(String(setting.threshold))
  useEffect(() => setThreshold(String(setting.threshold)), [setting.threshold])
  const commitThreshold = () => {
    const value = Number(threshold)
    // An empty box is not 0: the server would read 0 as the default.
    if (
      !threshold.trim() ||
      !Number.isInteger(value) ||
      value === setting.threshold
    ) {
      setThreshold(String(setting.threshold))
      return
    }
    void save({ ...setting, threshold: value }).then((saved) => {
      if (!saved) setThreshold(String(setting.threshold))
    })
  }
  return (
    <li className={cn("adm-alert-rule", !setting.enabled && "is-off")}>
      <Switch
        checked={setting.enabled}
        onChange={(enabled) => void save({ ...setting, enabled })}
        label={t(`admin.alerts.rule.${rule.code}`)}
      />
      {setting.enabled ? (
        <div className="adm-alert-rule-fields">
          {rule.param ? (
            <label className="adm-alert-inline">
              <input
                type="number"
                inputMode="numeric"
                min={rule.min}
                max={rule.max}
                value={threshold}
                onChange={(event) => setThreshold(event.target.value)}
                onBlur={commitThreshold}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.preventDefault()
                    commitThreshold()
                  }
                }}
              />
              <span>{t(`admin.alerts.param.${rule.param}`)}</span>
            </label>
          ) : null}
          <label className="adm-alert-inline">
            <select
              value={setting.severity}
              aria-label={t("admin.alerts.severity")}
              onChange={(event) =>
                void save({
                  ...setting,
                  severity: event.target.value as AlertSeverity,
                })
              }
            >
              {severities.map((severity) => (
                <option key={severity} value={severity}>
                  {t(`admin.alerts.severity.${severity}`)}
                </option>
              ))}
            </select>
          </label>
          <Switch
            checked={setting.notifyResolved}
            onChange={(notifyResolved) =>
              void save({ ...setting, notifyResolved })
            }
            label={t("admin.alerts.notifyResolved")}
          />
        </div>
      ) : null}
    </li>
  )
}

function RulesPanel({
  rules,
  save,
}: {
  rules: AlertRule[]
  save: (code: string, setting: AlertRuleSetting) => Promise<boolean>
}) {
  const { t } = useAppSettings()
  return (
    <Panel icon={ListChecks} title={t("admin.alerts.rules")}>
      <ul className="adm-alert-rules">
        {rules.map((rule) => (
          <RuleRow
            key={rule.code}
            rule={rule}
            save={(setting) => save(rule.code, setting)}
          />
        ))}
      </ul>
    </Panel>
  )
}

function HeartbeatPanel({
  heartbeat,
  save,
}: {
  heartbeat: AlertHeartbeat
  save: (value: {
    enabled: boolean
    intervalMinutes: number
    url?: string
  }) => Promise<boolean>
}) {
  const { t, locale } = useAppSettings()
  const { api, notify, fail } = useAdmin()
  const [url, setUrl] = useState("")
  const [interval, setMinutes] = useState(String(heartbeat.intervalMinutes))
  const [busy, setBusy] = useState(false)
  const [testing, setTesting] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(
    null,
  )
  useEffect(
    () => setMinutes(String(heartbeat.intervalMinutes)),
    [heartbeat.intervalMinutes],
  )
  const minutes = Number(interval)
  // An empty box is not 0: the server would read 0 as the default.
  const minutesValid = Boolean(interval.trim()) && Number.isInteger(minutes)
  const changed =
    (Boolean(url) && minutesValid) ||
    (minutesValid && minutes !== heartbeat.intervalMinutes)
  const submit = async () => {
    setBusy(true)
    const saved = await save({
      enabled: heartbeat.enabled,
      intervalMinutes: minutes,
      ...(url ? { url } : {}),
    })
    setBusy(false)
    if (saved) setUrl("")
  }
  const test = async () => {
    setTesting(true)
    try {
      const next = await api.testAlertHeartbeat()
      setResult({ ok: next.ok, text: testSummary(next, t) })
      if (next.ok) notify("admin.notice.alertHeartbeatSent")
    } catch (error) {
      fail(error)
    } finally {
      setTesting(false)
    }
  }
  return (
    <Panel
      icon={HeartPulse}
      title={t("admin.alerts.heartbeat")}
      actions={
        <Button
          size="sm"
          variant="ghost"
          icon={Activity}
          busy={testing}
          disabled={!heartbeat.hasUrl || heartbeat.urlUnreadable}
          onClick={() => void test()}
        >
          {t("admin.alerts.test")}
        </Button>
      }
    >
      <div className="adm-form">
        <Switch
          checked={heartbeat.enabled}
          disabled={!heartbeat.hasUrl && !heartbeat.enabled}
          onChange={(enabled) =>
            void save({ enabled, intervalMinutes: heartbeat.intervalMinutes })
          }
          label={t("admin.alerts.heartbeatEnabled")}
        />
        <div className="adm-form-row">
          <Field
            label={t("admin.alerts.heartbeatUrl")}
            hint={
              heartbeat.urlUnreadable
                ? t("admin.alerts.secretUnreadableHint")
                : heartbeat.hasUrl
                  ? t("admin.alerts.secretKeepHint")
                  : undefined
            }
          >
            <input
              type="password"
              value={url}
              autoComplete="off"
              placeholder={heartbeat.hasUrl ? "••••••••" : "https://"}
              onChange={(event) => setUrl(event.target.value)}
            />
          </Field>
          <Field label={t("admin.alerts.heartbeatInterval")}>
            <input
              type="number"
              inputMode="numeric"
              min={1}
              max={60}
              value={interval}
              onChange={(event) => setMinutes(event.target.value)}
            />
          </Field>
        </div>
        <div className="adm-alert-foot">
          <span className="adm-chip-row">
            {heartbeat.lastOkAt ? (
              <Badge tone="success">
                {t("admin.alerts.lastOk", {
                  time: formatRelative(heartbeat.lastOkAt, locale),
                })}
              </Badge>
            ) : null}
            {heartbeat.lastFailureAt ? (
              <Badge tone="danger">
                {t("admin.alerts.lastFailure", {
                  time: formatRelative(heartbeat.lastFailureAt, locale),
                })}
                {heartbeat.lastError
                  ? ` · ${t(`admin.alerts.error.${heartbeat.lastError}`)}`
                  : ""}
              </Badge>
            ) : null}
            {result ? (
              <Badge tone={result.ok ? "success" : "danger"}>
                {result.text}
              </Badge>
            ) : null}
          </span>
          <Button
            size="sm"
            variant="primary"
            icon={Save}
            busy={busy}
            disabled={!changed}
            onClick={() => void submit()}
          >
            {t("admin.action.save")}
          </Button>
        </div>
      </div>
    </Panel>
  )
}

function subjectLabel(
  subject: string | null,
  t: (key: string, values?: Record<string, any>) => string,
) {
  if (!subject) return ""
  const key = `admin.alerts.subject.${subject}`
  const label = t(key)
  return label === key ? subject : label
}

function IncidentsPanel({
  incidents,
  acknowledge,
}: {
  incidents: AlertIncident[]
  acknowledge: (incident: AlertIncident, on: boolean) => Promise<void>
}) {
  const { t, locale } = useAppSettings()
  const [busy, setBusy] = useState<number | null>(null)
  const toggle = async (incident: AlertIncident) => {
    setBusy(incident.id)
    try {
      await acknowledge(incident, !incident.acknowledgedAt)
    } finally {
      setBusy(null)
    }
  }
  return (
    <Panel icon={Siren} title={t("admin.alerts.open")}>
      <ul className="adm-alert-list">
        {incidents.map((incident) => (
          <li
            key={incident.id}
            className={cn("adm-alert-row", incident.acknowledgedAt && "is-off")}
          >
            <span className="adm-alert-main">
              <strong>
                {t(`admin.alerts.rule.${incident.rule}`)} ·{" "}
                {subjectLabel(incident.subject, t)}
              </strong>
              <small>
                {t("admin.alerts.openedAt", {
                  time: formatRelative(incident.openedAt, locale),
                })}
                {incident.acknowledgedAt
                  ? ` · ${t("admin.alerts.acknowledgedAt", {
                      time: formatRelative(incident.acknowledgedAt, locale),
                    })}`
                  : null}
              </small>
            </span>
            <span className="adm-row-actions">
              <Badge tone={severityTone[incident.severity]}>
                {t(`admin.alerts.severity.${incident.severity}`)}
              </Badge>
              <IconButton
                icon={incident.acknowledgedAt ? Undo2 : Check}
                label={t(
                  incident.acknowledgedAt
                    ? "admin.alerts.unacknowledge"
                    : "admin.alerts.acknowledge",
                )}
                busy={busy === incident.id}
                disabled={busy !== null}
                onClick={() => void toggle(incident)}
              />
            </span>
          </li>
        ))}
      </ul>
    </Panel>
  )
}

function DeliveriesPanel({
  deliveries,
  channels,
}: {
  deliveries: AlertDelivery[] | undefined
  channels: AlertChannel[]
}) {
  const { t, locale } = useAppSettings()
  const names = new Map(
    channels.map((channel) => [
      channel.id,
      channel.name || t(`admin.alerts.channelType.${channel.type}`),
    ]),
  )
  return (
    <Panel icon={History} title={t("admin.alerts.deliveries")}>
      {!deliveries ? (
        <Loading />
      ) : deliveries.length ? (
        <ul className="adm-alert-list is-compact">
          {deliveries.map((delivery) => (
            <li key={delivery.id} className="adm-alert-row">
              <span className="adm-alert-main">
                <strong>
                  {delivery.rule
                    ? `${t(`admin.alerts.rule.${delivery.rule}`)} · ${subjectLabel(delivery.subject, t)}`
                    : t("admin.alerts.kind.test")}
                </strong>
                <small>
                  {formatTime(delivery.createdAt, locale)} ·{" "}
                  {names.get(delivery.channelId) ??
                    t("admin.alerts.removedChannel")}
                  {delivery.rule
                    ? ` · ${t(`admin.alerts.kind.${delivery.kind}`)}`
                    : ""}
                  {delivery.attempts > 1
                    ? ` · ${t("admin.alerts.attempts", { count: delivery.attempts })}`
                    : ""}
                </small>
              </span>
              <Badge tone={deliveryTone[delivery.status]}>
                {t(`admin.alerts.status.${delivery.status}`)}
                {delivery.lastError
                  ? ` · ${t(`admin.alerts.error.${delivery.lastError}`)}`
                  : ""}
              </Badge>
            </li>
          ))}
        </ul>
      ) : (
        <Empty title={t("admin.alerts.noDeliveries")} />
      )}
    </Panel>
  )
}

/** The Alerts page: where problems are delivered, which rules raise them,
 * the heartbeat, open incidents and the delivery history. */
export function AlertsView() {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const resource = useResource(() => api.alertSettings(), [])
  const history = useResource(() => api.alertDeliveries(), [])
  const settings = resource.data
  const apply = async (
    body: Parameters<typeof api.updateAlertSettings>[0],
    success?: string,
  ) => {
    const next = await act(() => api.updateAlertSettings(body), success)
    if (next) resource.setData(next)
    return Boolean(next)
  }
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.alerts.title")}
        actions={
          settings ? (
            <Badge tone={stateTone[settings.state]} icon={BellRing}>
              {t(`admin.alerts.state.${settings.state}`)}
            </Badge>
          ) : null
        }
      />
      {!settings ? (
        <Loading />
      ) : (
        <div className="adm-alerts-grid">
          <div className="adm-alerts-column">
            {settings.incidents.length ? (
              <IncidentsPanel
                incidents={settings.incidents}
                acknowledge={async (incident, on) => {
                  const next = await act(() =>
                    api.acknowledgeAlertIncident(incident.id, on),
                  )
                  if (next) resource.setData(next)
                }}
              />
            ) : null}
            <ChannelsPanel
              settings={settings}
              save={(channels, success) => apply({ channels }, success)}
              onTested={() => void history.reload()}
            />
            <HeartbeatPanel
              heartbeat={settings.heartbeat}
              save={(heartbeat) =>
                apply({ heartbeat }, "admin.notice.alertHeartbeatSaved")
              }
            />
          </div>
          <div className="adm-alerts-column">
            <RulesPanel
              rules={settings.rules}
              save={(code, setting) =>
                apply(
                  { rules: { [code]: setting } },
                  "admin.notice.alertRuleSaved",
                )
              }
            />
            <DeliveriesPanel
              deliveries={history.data?.deliveries}
              channels={settings.channels}
            />
          </div>
        </div>
      )}
    </div>
  )
}
