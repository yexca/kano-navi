import { useEffect, useState, type ReactNode, type SubmitEvent } from "react"
import {
  CalendarClock,
  ChevronRight,
  Download,
  History,
  Info,
  KeyRound,
  Play,
  Radio,
  RadioTower,
  RotateCw,
  Save,
  ShieldAlert,
  Tv,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import {
  formatDateTime,
  formatInterval,
  formatRelative,
  japanToday,
} from "@/admin/format"
import type {
  FetchWindow,
  LiveProbeStatus,
  PushStatus,
  ScheduleSource,
  SourceSchedule,
  SourceSync,
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
  StatusBadge,
  Switch,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { BilibiliSource } from "@/admin/views/bilibili-parts"
import { stepIcons } from "@/admin/views/run-parts"
import { cn } from "@/lib/utils"
import type { Translator } from "@/types"

/**
 * How past windows are read: the source API with its credential, the public
 * X timeline without it, or not at all.
 */
function WindowBadge({ state }: { state: Sources["x"] | Sources["youtube"] }) {
  const { t } = useAppSettings()
  if (state.historyAvailable) {
    return (
      <Badge tone="leaf" icon={KeyRound}>
        {t("admin.sources.historyOn")}
      </Badge>
    )
  }
  if (state.windows.includes("range")) {
    return (
      <Badge tone="leaf" icon={History}>
        {t("admin.sources.historyTimeline")}
      </Badge>
    )
  }
  return <Badge icon={KeyRound}>{t("admin.sources.historyOff")}</Badge>
}

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
  const modes = (["recent", "before", "range", "count"] as const).map(
    (value) => ({ value, label: t(`admin.fetch.mode.${value}`) }),
  )
  const hint =
    mode === "recent" && windows.includes("range")
      ? "admin.fetch.hint.recentAll"
      : mode === "count" && source === "youtube"
        ? "admin.fetch.hint.count.youtube"
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
        <Field
          label={t(
            source === "youtube"
              ? "admin.fetch.count.youtube"
              : "admin.fetch.count",
          )}
          hint={t("admin.fetch.countHint")}
        >
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

const intervals = [15, 30, 60, 120, 180, 360, 720, 1440, 2880, 10080]

const skipFlags: Record<ScheduleSource, string> = {
  x: "SKIP_X",
  youtube: "SKIP_YOUTUBE",
  bilibili: "SKIP_BILIBILI",
}

const sameSync = (a: SourceSync | null, b: SourceSync | null) =>
  JSON.stringify(a) === JSON.stringify(b)

/** Why a source's timer is not running, or null while it runs. */
function timerBlock(
  schedule: SourceSchedule,
  timersOff: boolean,
  t: Translator,
) {
  if (schedule.skipped)
    return t("admin.timer.skipped", { flag: skipFlags[schedule.source] })
  if (timersOff) return t("admin.timer.schedulerOff")
  return null
}

/**
 * A source at a glance: whether its timer runs and when, how the last run
 * went, the timer switch and Run now. Selecting the card shows the source's
 * settings below.
 */
function SourceCard({
  schedule,
  lastSuccess,
  chips,
  selected,
  onSelect,
  onChange,
}: {
  schedule: SourceSchedule
  lastSuccess: string | null | undefined
  chips?: ReactNode
  selected: boolean
  onSelect: () => void
  onChange: (schedule: SourceSchedule) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act, start, overview } = useAdmin()
  const [busy, setBusy] = useState("")
  const source = schedule.source
  const Icon = stepIcons[source] || Tv
  const timersOff = overview ? !overview.scheduler.enabled : false
  const blocked = timerBlock(schedule, timersOff, t)
  const running = schedule.enabled && !blocked
  const toggle = async (enabled: boolean) => {
    setBusy("toggle")
    const next = await act(
      () => api.updateSourceSchedule(source, { enabled }),
      "admin.notice.scheduleSaved",
    )
    setBusy("")
    if (next) onChange(next)
  }
  const runNow = async () => {
    setBusy("run")
    await start({ kind: "source", source })
    setBusy("")
  }
  const when = (value: string | null | undefined) =>
    value ? (
      <time dateTime={value} title={formatDateTime(value, locale)}>
        {formatRelative(value, locale)}
      </time>
    ) : (
      "—"
    )
  return (
    <article
      className={cn(
        "adm-source-card",
        selected && "is-selected",
        schedule.skipped && "is-skipped",
      )}
    >
      <button
        type="button"
        className="adm-source-card-head"
        aria-pressed={selected}
        onClick={onSelect}
      >
        <span className="adm-source-icon">
          <Icon aria-hidden />
        </span>
        <span className="adm-source-name">
          <strong>{t(`admin.step.${source}`)}</strong>
          <small>
            {running
              ? t("admin.timer.on", {
                  interval: formatInterval(schedule.intervalMinutes, t),
                })
              : schedule.enabled && blocked
                ? t("admin.sources.timerBlocked")
                : t("admin.sources.timerOff")}
          </small>
        </span>
        <ChevronRight className="adm-source-card-chevron" aria-hidden />
      </button>
      <dl className="adm-source-facts">
        <div>
          <dt>{t("admin.timer.nextRun")}</dt>
          <dd>{running ? when(schedule.nextRunAt) : "—"}</dd>
        </div>
        <div>
          <dt>{t("admin.timer.lastRun")}</dt>
          <dd>
            {schedule.lastRun ? (
              <>
                <StatusBadge status={schedule.lastRun.status} />
                {when(schedule.lastRun.at)}
              </>
            ) : (
              t("admin.timer.never")
            )}
          </dd>
        </div>
        <div>
          <dt>{t("admin.sources.lastSuccess")}</dt>
          <dd>{when(lastSuccess)}</dd>
        </div>
      </dl>
      {chips ? <span className="adm-chip-row">{chips}</span> : null}
      {blocked ? (
        <p className="adm-source-note">
          <Info aria-hidden />
          {blocked}
        </p>
      ) : null}
      <footer>
        <Switch
          checked={schedule.enabled}
          disabled={schedule.skipped || busy !== ""}
          onChange={toggle}
          label={t("admin.timer.enable")}
        />
        <Button
          size="sm"
          icon={Play}
          busy={busy === "run"}
          disabled={
            schedule.skipped || busy !== "" || Boolean(overview?.activeRun)
          }
          onClick={runNow}
          title={t("admin.timer.runNowHint")}
        >
          {t("admin.timer.runNow")}
        </Button>
      </footer>
    </article>
  )
}

/** A source's timer settings: its interval and, for X and YouTube, how much
 * each run reads. Saving keeps the timer on or off as it is. */
function TimerPanel({
  schedule,
  onChange,
}: {
  schedule: SourceSchedule
  onChange: (schedule: SourceSchedule) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [minutes, setMinutes] = useState(schedule.intervalMinutes)
  const [sync, setSync] = useState(schedule.sync)
  const [busy, setBusy] = useState(false)
  const syncKey = JSON.stringify(schedule.sync)
  useEffect(
    () => setMinutes(schedule.intervalMinutes),
    [schedule.intervalMinutes],
  )
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => setSync(schedule.sync), [syncKey])
  const source = schedule.source
  const options = [...new Set([...intervals, minutes])]
    .filter(
      (value) =>
        value >= schedule.minIntervalMinutes &&
        value <= schedule.maxIntervalMinutes,
    )
    .sort((a, b) => a - b)
  if (!options.includes(schedule.minIntervalMinutes))
    options.unshift(schedule.minIntervalMinutes)
  const limit = sync ? (sync.mode === "count" ? sync.count : sync.max) : 0
  const dirty =
    minutes !== schedule.intervalMinutes || !sameSync(sync, schedule.sync)
  return (
    <Panel
      level={3}
      icon={CalendarClock}
      title={t("admin.timer.title")}
      description={t(`admin.timer.description.${source}`, {
        floor: formatInterval(schedule.minIntervalMinutes, t),
      })}
    >
      <form
        className="adm-form"
        onSubmit={async (event) => {
          event.preventDefault()
          setBusy(true)
          const next = await act(
            () =>
              api.updateSourceSchedule(source, {
                intervalMinutes: minutes,
                ...(sync ? { sync } : {}),
              }),
            "admin.notice.scheduleSaved",
          )
          setBusy(false)
          if (next) onChange(next)
        }}
      >
        <Field label={t("admin.timer.interval")}>
          <select
            value={minutes}
            onChange={(e) => setMinutes(Number(e.target.value))}
          >
            {options.map((value) => (
              <option key={value} value={value}>
                {formatInterval(value, t)}
              </option>
            ))}
          </select>
        </Field>
        {sync ? (
          <>
            <Segmented
              label={t("admin.timer.sync")}
              value={sync.mode}
              onChange={(mode) =>
                setSync(
                  mode === "count"
                    ? { mode: "count", count: limit }
                    : { mode: "incremental", max: limit },
                )
              }
              options={[
                {
                  value: "incremental",
                  label: t("admin.fetch.mode.incremental"),
                },
                { value: "count", label: t("admin.fetch.mode.count") },
              ]}
            />
            <p className="adm-field-hint">
              {t(`admin.timer.syncHint.${sync.mode}`)}
            </p>
            <Field
              label={t(`admin.timer.syncLimit.${sync.mode}.${source}`)}
              hint={t(`admin.timer.syncLimitHint.${source}`)}
            >
              <input
                type="number"
                min={1}
                max={200}
                value={limit}
                onChange={(e) => {
                  const value = Number(e.target.value)
                  setSync(
                    sync.mode === "count"
                      ? { mode: "count", count: value }
                      : { mode: "incremental", max: value },
                  )
                }}
                required
              />
            </Field>
          </>
        ) : null}
        <div className="adm-form-actions">
          {dirty ? (
            <span className="adm-muted">{t("admin.models.unsaved")}</span>
          ) : null}
          <Button
            type="submit"
            size="sm"
            variant="primary"
            icon={Save}
            busy={busy}
            disabled={!dirty}
          >
            {t("admin.action.save")}
          </Button>
        </div>
      </form>
    </Panel>
  )
}

function FetchPanel({
  source,
  sources,
}: {
  source: "x" | "youtube"
  sources: Sources
}) {
  const { t, locale } = useAppSettings()
  const { overview } = useAdmin()
  const state = sources[source]
  const oldest =
    source === "x"
      ? sources.x.accounts.map(
          (account) => [account.account, account.oldest] as const,
        )
      : [[t("admin.sources.channel"), sources.youtube.oldest] as const]
  return (
    <Panel
      level={3}
      icon={Download}
      title={t("admin.fetch.title")}
      description={t("admin.fetch.description")}
      actions={<WindowBadge state={state} />}
    >
      <Facts
        items={[
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
      level={3}
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
      level={3}
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

const sourceIds: ScheduleSource[] = ["x", "youtube", "bilibili"]

export function SourcesView({ tab, navigate }: ViewProps) {
  const { t, locale } = useAppSettings()
  const { api, overview } = useAdmin()
  const sources = useResource(() => api.sources(), [])
  const data = sources.data
  const selected = sourceIds.includes(tab as ScheduleSource)
    ? (tab as ScheduleSource)
    : "x"
  const setSchedule = (next: SourceSchedule) =>
    data &&
    sources.setData({
      ...data,
      schedules: data.schedules.map((entry) =>
        entry.source === next.source ? next : entry,
      ),
    })
  const schedule = (source: ScheduleSource) =>
    data?.schedules.find((entry) => entry.source === source)
  const blockedUntil =
    data?.bilibili.blockedUntil &&
    Date.parse(data.bilibili.blockedUntil) > Date.now()
      ? data.bilibili.blockedUntil
      : null
  const chips = (source: ScheduleSource) => {
    if (!data) return null
    return source === "bilibili" && blockedUntil ? (
      <Badge tone="danger" icon={ShieldAlert}>
        {t("admin.bilibili.blockedUntil", {
          time: formatDateTime(blockedUntil, locale),
        })}
      </Badge>
    ) : null
  }
  const current = schedule(selected)
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
          <div className="adm-source-cards">
            {sourceIds.map((source) => {
              const found = schedule(source)
              return found ? (
                <SourceCard
                  key={source}
                  schedule={found}
                  lastSuccess={
                    source === "bilibili"
                      ? data.bilibili.lastSuccessAt
                      : overview?.sources[source].lastSuccessAt
                  }
                  chips={chips(source)}
                  selected={selected === source}
                  onSelect={() => navigate("sources", source)}
                  onChange={setSchedule}
                />
              ) : null
            })}
          </div>
          <section
            className="adm-source-detail"
            aria-label={t("admin.sources.settingsFor", {
              source: t(`admin.step.${selected}`),
            })}
          >
            <header className="adm-source-header">
              <h2>
                {t("admin.sources.settingsFor", {
                  source: t(`admin.step.${selected}`),
                })}
              </h2>
              <p>{t(`admin.sources.${selected}.hint`)}</p>
            </header>
            <div className="adm-grid is-two">
              {current ? (
                <TimerPanel schedule={current} onChange={setSchedule} />
              ) : null}
              {selected === "bilibili" ? (
                <BilibiliSource
                  status={data.bilibili}
                  onAdded={() => void sources.reload()}
                />
              ) : (
                <FetchPanel source={selected} sources={data} />
              )}
              {selected === "youtube" ? (
                <>
                  <PushPanel
                    push={data.youtube.push}
                    onChange={(push) =>
                      sources.setData({
                        ...data,
                        youtube: { ...data.youtube, push },
                      })
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
                </>
              ) : null}
            </div>
          </section>
        </>
      )}
    </div>
  )
}
