import { useEffect, useState, type SubmitEvent } from "react"
import {
  Download,
  KeyRound,
  Radio,
  RadioTower,
  RotateCw,
  Save,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatDateTime, formatRelative, japanToday } from "@/admin/format"
import type {
  FetchWindow,
  LiveProbeStatus,
  PushStatus,
  Sources,
} from "@/admin/types"
import {
  Badge,
  Button,
  ConfirmButton,
  Facts,
  Field,
  Loading,
  PageHeader,
  Panel,
  Segmented,
  Switch,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { WindowBadge } from "@/admin/views/fetch-parts"
import { stepIcons } from "@/admin/views/run-parts"

function FetchForm({
  source,
  windows,
  hasRecords,
  disabled,
}: {
  source: "x" | "youtube"
  windows: FetchWindow["mode"][]
  hasRecords: boolean
  disabled: boolean
}) {
  const { t } = useAppSettings()
  const { start } = useAdmin()
  const [mode, setMode] = useState<FetchWindow["mode"]>("recent")
  const [days, setDays] = useState(source === "x" ? 7 : 14)
  const [count, setCount] = useState(50)
  const [startDate, setStartDate] = useState("")
  const [endDate, setEndDate] = useState(japanToday())
  const [busy, setBusy] = useState(false)
  const unavailable = !windows.includes(mode)
  // Only the X timeline lists posts by count.
  const modes = (
    source === "x"
      ? (["recent", "before", "range", "count"] as const)
      : (["recent", "before", "range"] as const)
  ).map((value) => ({ value, label: t(`admin.fetch.mode.${value}`) }))
  const hint =
    mode === "recent" && windows.includes("range")
      ? "admin.fetch.hint.recentAll"
      : `admin.fetch.hint.${mode}`
  const submit = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    await start({
      kind: "fetch",
      source,
      window:
        mode === "range"
          ? { mode, startDate, endDate }
          : mode === "count"
            ? { mode, count }
            : { mode, days },
    })
    setBusy(false)
  }
  return (
    <form className="adm-form" onSubmit={submit}>
      <Segmented
        label={t("admin.fetch.mode")}
        value={mode}
        onChange={setMode}
        options={modes}
      />
      <p className="adm-field-hint">{t(hint)}</p>
      {mode === "count" ? (
        <Field label={t("admin.fetch.count")} hint={t("admin.fetch.countHint")}>
          <input
            type="number"
            min={1}
            max={200}
            value={count}
            onChange={(e) => setCount(Number(e.target.value))}
            required
          />
        </Field>
      ) : mode === "range" ? (
        <div className="adm-form-row">
          <Field label={t("admin.fetch.start")}>
            <input
              type="date"
              value={startDate}
              max={endDate}
              onChange={(e) => setStartDate(e.target.value)}
              required
            />
          </Field>
          <Field label={t("admin.fetch.end")}>
            <input
              type="date"
              value={endDate}
              min={startDate}
              onChange={(e) => setEndDate(e.target.value)}
              required
            />
          </Field>
        </div>
      ) : (
        <Field
          label={t(
            mode === "before" ? "admin.fetch.backfillDays" : "admin.fetch.days",
          )}
          hint={t("admin.fetch.daysHint")}
        >
          <input
            type="number"
            min={1}
            max={365}
            value={days}
            onChange={(e) => setDays(Number(e.target.value))}
            required
          />
        </Field>
      )}
      {unavailable ? (
        <p className="adm-inline-error">
          <KeyRound aria-hidden />
          {t(`admin.fetch.needsKey.${source}`)}
        </p>
      ) : null}
      {mode === "before" && !hasRecords ? (
        <p className="adm-inline-error">{t("admin.fetch.noRecords")}</p>
      ) : null}
      <div className="adm-form-actions">
        <Button
          type="submit"
          variant="primary"
          icon={Download}
          busy={busy}
          disabled={
            disabled || unavailable || (mode === "before" && !hasRecords)
          }
        >
          {t("admin.fetch.run")}
        </Button>
      </div>
    </form>
  )
}

function SourcePanel({
  source,
  sources,
}: {
  source: "x" | "youtube"
  sources: Sources
}) {
  const { t, locale } = useAppSettings()
  const { overview } = useAdmin()
  const state = sources[source]
  const Icon = stepIcons[source]
  const health = overview?.sources[source]
  const oldest =
    source === "x"
      ? sources.x.accounts.map(
          (account) => [account.account, account.oldest] as const,
        )
      : [[t("admin.sources.channel"), sources.youtube.oldest] as const]
  return (
    <Panel
      icon={Icon}
      title={t(`admin.step.${source}`)}
      description={t(`admin.sources.${source}.hint`)}
      actions={
        <span className="adm-chip-row">
          {state.skipped ? <Badge>{t("admin.overview.skipped")}</Badge> : null}
          <WindowBadge state={state} />
        </span>
      }
    >
      <Facts
        items={[
          [
            t("admin.sources.lastSuccess"),
            health?.lastSuccessAt
              ? `${formatDateTime(health.lastSuccessAt, locale)} · ${formatRelative(health.lastSuccessAt, locale)}`
              : null,
          ],
          ...oldest.map(
            ([label, time]) =>
              [
                t("admin.sources.oldest", { label }),
                time ? formatDateTime(time, locale) : t("admin.sources.none"),
              ] as [string, string],
          ),
        ]}
      />
      <FetchForm
        source={source}
        windows={state.windows}
        hasRecords={oldest.some(([, time]) => Boolean(time))}
        disabled={state.skipped || Boolean(overview?.activeRun)}
      />
    </Panel>
  )
}

function PushPanel({
  push,
  onChange,
}: {
  push: PushStatus
  onChange: (push: PushStatus) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const [callback, setCallback] = useState(push.callbackUrl || "")
  const [busy, setBusy] = useState("")
  useEffect(() => setCallback(push.callbackUrl || ""), [push.callbackUrl])
  const save = async (body: {
    enabled?: boolean
    callbackUrl?: string | null
  }) => {
    setBusy(body.enabled === undefined ? "save" : "toggle")
    const next = await act(() => api.updatePush(body), "admin.notice.pushSaved")
    setBusy("")
    if (next) onChange(next)
  }
  return (
    <Panel
      icon={Radio}
      title={t("admin.push.title")}
      description={t("admin.push.description")}
      actions={
        push.enabled ? (
          <Badge tone={push.verified ? "leaf" : "honey"}>
            {t(
              push.verified
                ? "admin.overview.pushVerified"
                : "admin.overview.pushPending",
            )}
          </Badge>
        ) : (
          <Badge>{t("admin.push.off")}</Badge>
        )
      }
    >
      <div className="adm-form">
        <Switch
          checked={push.enabled}
          disabled={!push.available || busy !== ""}
          onChange={(enabled) =>
            save({ enabled, callbackUrl: callback || null })
          }
          label={t("admin.push.enable")}
          description={
            push.available
              ? t("admin.push.enableHint")
              : t("admin.push.unavailable")
          }
        />
        <Field
          label={t("admin.push.callback")}
          hint={t("admin.push.callbackHint", { path: push.callbackPath })}
        >
          <input
            type="url"
            value={callback}
            onChange={(e) => setCallback(e.target.value)}
            placeholder="https://"
          />
        </Field>
        <div className="adm-form-actions">
          <ConfirmButton
            size="sm"
            variant="ghost"
            icon={RotateCw}
            disabled={!push.secretConfigured}
            onConfirm={async () => {
              const next = await act(
                () => api.rotatePushSecret(),
                "admin.notice.secretRotated",
              )
              if (next) onChange(next)
            }}
          >
            {t("admin.push.rotate")}
          </ConfirmButton>
          <Button
            size="sm"
            icon={Save}
            busy={busy === "save"}
            disabled={callback === (push.callbackUrl || "")}
            onClick={() => save({ callbackUrl: callback || null })}
          >
            {t("admin.action.save")}
          </Button>
        </div>
        <Facts
          items={[
            [
              t("admin.push.secret"),
              t(
                push.secretConfigured
                  ? "admin.push.secretSet"
                  : "admin.push.secretUnset",
              ),
            ],
            [
              t("admin.push.lease"),
              formatDateTime(push.leaseExpiresAt, locale) || null,
            ],
            [
              t("admin.push.renewal"),
              formatDateTime(push.nextRenewalAt, locale) || null,
            ],
            [
              t("admin.push.notified"),
              formatDateTime(push.lastNotificationAt, locale) || null,
            ],
            [
              t("admin.push.triggered"),
              formatDateTime(push.lastTriggeredAt, locale) || null,
            ],
            [t("admin.push.lastError"), push.lastError],
          ]}
        />
      </div>
    </Panel>
  )
}

function LiveProbePanel({
  probe,
  onChange,
}: {
  probe: LiveProbeStatus
  onChange: (probe: LiveProbeStatus) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const [minutes, setMinutes] = useState(probe.intervalMinutes)
  const [busy, setBusy] = useState("")
  useEffect(() => setMinutes(probe.intervalMinutes), [probe.intervalMinutes])
  const save = async (body: {
    enabled?: boolean
    intervalMinutes?: number
  }) => {
    setBusy(body.enabled === undefined ? "save" : "toggle")
    const next = await act(
      () => api.updateLiveProbe(body),
      "admin.notice.liveProbeSaved",
    )
    setBusy("")
    if (next) onChange(next)
  }
  const running = probe.enabled && probe.available
  return (
    <Panel
      icon={RadioTower}
      title={t("admin.liveProbe.title")}
      description={t("admin.liveProbe.description", { max: probe.maxVideos })}
      actions={
        running ? (
          <Badge tone="leaf">{t("admin.liveProbe.on")}</Badge>
        ) : (
          <Badge>{t("admin.push.off")}</Badge>
        )
      }
    >
      <form
        className="adm-form"
        onSubmit={(event) => {
          event.preventDefault()
          save({ intervalMinutes: minutes })
        }}
      >
        <Switch
          checked={probe.enabled}
          disabled={!probe.available || busy !== ""}
          onChange={(enabled) => save({ enabled })}
          label={t("admin.liveProbe.enable")}
          description={
            probe.available
              ? t("admin.liveProbe.enableHint")
              : t("admin.liveProbe.unavailable")
          }
        />
        <Field
          label={t("admin.liveProbe.interval")}
          hint={t("admin.liveProbe.intervalHint", {
            min: probe.minIntervalMinutes,
            max: probe.maxIntervalMinutes,
          })}
        >
          <input
            type="number"
            min={probe.minIntervalMinutes}
            max={probe.maxIntervalMinutes}
            step={1}
            value={minutes}
            onChange={(e) => setMinutes(Number(e.target.value))}
            required
          />
        </Field>
        <div className="adm-form-actions">
          <Button
            type="submit"
            size="sm"
            icon={Save}
            busy={busy === "save"}
            disabled={minutes === probe.intervalMinutes || busy !== ""}
          >
            {t("admin.action.save")}
          </Button>
        </div>
        <Facts
          items={[
            [
              t("admin.liveProbe.lastRun"),
              probe.lastRunAt
                ? `${formatDateTime(probe.lastRunAt, locale)} · ${formatRelative(probe.lastRunAt, locale)}`
                : null,
            ],
            [
              t("admin.liveProbe.nextRun"),
              probe.nextRunAt
                ? `${formatDateTime(probe.nextRunAt, locale)} · ${formatRelative(probe.nextRunAt, locale)}`
                : null,
            ],
            [
              t("admin.liveProbe.active"),
              t("admin.liveProbe.activeCount", {
                count: probe.activeVideos,
                max: probe.maxVideos,
              }),
            ],
          ]}
        />
      </form>
    </Panel>
  )
}

export function SourcesView(_: ViewProps) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const sources = useResource(() => api.sources(), [])
  const data = sources.data
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.sources.title")}
        description={t("admin.sources.description")}
      />
      {!data ? (
        <Loading />
      ) : (
        <>
          <div className="adm-grid is-two">
            <SourcePanel source="x" sources={data} />
            <SourcePanel source="youtube" sources={data} />
          </div>
          <div className="adm-grid is-two">
            <PushPanel
              push={data.youtube.push}
              onChange={(push) =>
                sources.setData({ ...data, youtube: { ...data.youtube, push } })
              }
            />
            <LiveProbePanel
              probe={data.youtube.liveProbe}
              onChange={(liveProbe) =>
                sources.setData({
                  ...data,
                  youtube: { ...data.youtube, liveProbe },
                })
              }
            />
          </div>
        </>
      )}
    </div>
  )
}
