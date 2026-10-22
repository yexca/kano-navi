import { useEffect, useState, type SubmitEvent } from "react"
import { Download, KeyRound, Radio, RotateCw, Save } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatDateTime, formatRelative, japanToday } from "@/admin/format"
import type { FetchWindow, PushStatus, Sources } from "@/admin/types"
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
import { stepIcons } from "@/admin/views/run-parts"

function FetchForm({
  source,
  historyAvailable,
  hasRecords,
  disabled,
}: {
  source: "x" | "youtube"
  historyAvailable: boolean
  hasRecords: boolean
  disabled: boolean
}) {
  const { t } = useAppSettings()
  const { start } = useAdmin()
  const [mode, setMode] = useState<FetchWindow["mode"]>("recent")
  const [days, setDays] = useState(source === "x" ? 7 : 14)
  const [startDate, setStartDate] = useState("")
  const [endDate, setEndDate] = useState(japanToday())
  const [busy, setBusy] = useState(false)
  const past = mode !== "recent"
  const submit = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    await start({
      kind: "fetch",
      source,
      window: mode === "range" ? { mode, startDate, endDate } : { mode, days },
    })
    setBusy(false)
  }
  return (
    <form className="adm-form" onSubmit={submit}>
      <Segmented
        label={t("admin.fetch.mode")}
        value={mode}
        onChange={setMode}
        options={(["recent", "before", "range"] as const).map((value) => ({
          value,
          label: t(`admin.fetch.mode.${value}`),
        }))}
      />
      <p className="adm-field-hint">{t(`admin.fetch.hint.${mode}`)}</p>
      {mode === "range" ? (
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
      {past && !historyAvailable ? (
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
            disabled ||
            (past && !historyAvailable) ||
            (mode === "before" && !hasRecords)
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
          <Badge
            tone={state.historyAvailable ? "leaf" : "neutral"}
            icon={KeyRound}
          >
            {t(
              state.historyAvailable
                ? "admin.sources.historyOn"
                : "admin.sources.historyOff",
            )}
          </Badge>
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
        historyAvailable={state.historyAvailable}
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
          <PushPanel
            push={data.youtube.push}
            onChange={(push) =>
              sources.setData({ ...data, youtube: { ...data.youtube, push } })
            }
          />
        </>
      )}
    </div>
  )
}
