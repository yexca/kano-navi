import {
  useEffect,
  useId,
  useState,
  type ReactNode,
  type SubmitEvent,
} from "react"
import {
  CalendarClock,
  Download,
  History,
  KeyRound,
  Play,
  Radio,
  RadioTower,
  RotateCw,
  Save,
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

/** A source's own timer: on or off, its interval and, for X and YouTube, how
 * much each run reads. Run now submits the same chain once without moving
 * the next timed run. */
function SchedulePanel({
  schedule,
  facts,
  onChange,
}: {
  schedule: SourceSchedule
  facts: [ReactNode, ReactNode][]
  onChange: (schedule: SourceSchedule) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act, start, overview } = useAdmin()
  const [minutes, setMinutes] = useState(schedule.intervalMinutes)
  const [sync, setSync] = useState(schedule.sync)
  const [busy, setBusy] = useState("")
  const syncKey = JSON.stringify(schedule.sync)
  useEffect(
    () => setMinutes(schedule.intervalMinutes),
    [schedule.intervalMinutes],
  )
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => setSync(schedule.sync), [syncKey])
  const source = schedule.source
  const timersOff = overview ? !overview.scheduler.enabled : false
  const running = schedule.enabled && !schedule.skipped && !timersOff
  const save = async (body: Parameters<typeof api.updateSourceSchedule>[1]) => {
    setBusy(body.enabled === undefined ? "save" : "toggle")
    const next = await act(
      () => api.updateSourceSchedule(source, body),
      "admin.notice.scheduleSaved",
    )
    setBusy("")
    if (next) onChange(next)
  }
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
  const runNow = async () => {
    setBusy("run")
    await start({ kind: "source", source })
    setBusy("")
  }
  const when = (value: string | null | undefined) =>
    value
      ? `${formatDateTime(value, locale)} · ${formatRelative(value, locale)}`
      : null
  return (
    <Panel
      level={3}
      icon={CalendarClock}
      title={t("admin.timer.title")}
      description={t(`admin.timer.description.${source}`, {
        floor: formatInterval(schedule.minIntervalMinutes, t),
      })}
      actions={
        running ? (
          <Badge tone="leaf">
            {t("admin.timer.on", {
              interval: formatInterval(schedule.intervalMinutes, t),
            })}
          </Badge>
        ) : (
          <Badge>{t("admin.push.off")}</Badge>
        )
      }
    >
      <form
        className="adm-form"
        onSubmit={(event) => {
          event.preventDefault()
          save({ intervalMinutes: minutes, ...(sync ? { sync } : {}) })
        }}
      >
        <Switch
          checked={schedule.enabled}
          disabled={schedule.skipped || busy !== ""}
          onChange={(enabled) => save({ enabled })}
          label={t("admin.timer.enable")}
          description={
            schedule.skipped
              ? t("admin.timer.skipped", { flag: skipFlags[source] })
              : timersOff
                ? t("admin.timer.schedulerOff")
                : t("admin.timer.enableHint")
          }
        />
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
          <div className="adm-form-row is-top">
            <Field
              label={t("admin.timer.sync")}
              hint={t(`admin.timer.syncHint.${sync.mode}`)}
            >
              <select
                value={sync.mode}
                onChange={(e) =>
                  setSync(
                    e.target.value === "count"
                      ? { mode: "count", count: limit }
                      : { mode: "incremental", max: limit },
                  )
                }
              >
                <option value="incremental">
                  {t("admin.fetch.mode.incremental")}
                </option>
                <option value="count">{t("admin.fetch.mode.count")}</option>
              </select>
            </Field>
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
          </div>
        ) : null}
        <div className="adm-form-actions">
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
          <Button
            type="submit"
            size="sm"
            icon={Save}
            busy={busy === "save"}
            disabled={!dirty || busy !== ""}
          >
            {t("admin.action.save")}
          </Button>
        </div>
        <Facts
          items={[
            [
              t("admin.timer.nextRun"),
              running ? when(schedule.nextRunAt) : null,
            ],
            [
              t("admin.timer.lastRun"),
              schedule.lastRun ? (
                <span className="adm-chip-row">
                  <StatusBadge status={schedule.lastRun.status} />
                  <time
                    dateTime={schedule.lastRun.at}
                    title={formatDateTime(schedule.lastRun.at, locale)}
                  >
                    {formatRelative(schedule.lastRun.at, locale)}
                  </time>
                </span>
              ) : (
                t("admin.timer.never")
              ),
            ],
            ...facts,
          ]}
        />
      </form>
    </Panel>
  )
}

/** One source: its heading, then its panels side by side. */
function SourceSection({
  source,
  hint,
  chips,
  children,
}: {
  source: ScheduleSource
  hint: string
  chips?: ReactNode
  children: ReactNode
}) {
  const { t } = useAppSettings()
  const id = useId()
  const Icon = stepIcons[source] || Tv
  return (
    <section className="adm-source" aria-labelledby={id}>
      <header className="adm-source-header">
        <h2 id={id}>
          <Icon aria-hidden />
          {t(`admin.step.${source}`)}
        </h2>
        {chips ? <span className="adm-chip-row">{chips}</span> : null}
        <p>{hint}</p>
      </header>
      <div className="adm-grid is-two">{children}</div>
    </section>
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

export function SourcesView(_: ViewProps) {
  const { t, locale } = useAppSettings()
  const { api, overview } = useAdmin()
  const sources = useResource(() => api.sources(), [])
  const data = sources.data
  const when = (value: string | null | undefined) =>
    value
      ? `${formatDateTime(value, locale)} · ${formatRelative(value, locale)}`
      : null
  const schedule = (source: ScheduleSource) => {
    const found = data?.schedules.find((entry) => entry.source === source)
    return found ? (
      <SchedulePanel
        schedule={found}
        facts={
          source === "bilibili"
            ? [
                [
                  t("admin.bilibili.lastSuccess"),
                  when(data?.bilibili.lastSuccessAt),
                ],
                [
                  t("admin.bilibili.backoff"),
                  data?.bilibili.blockedUntil &&
                  Date.parse(data.bilibili.blockedUntil) > Date.now()
                    ? t("admin.bilibili.blockedUntil", {
                        time: formatDateTime(
                          data.bilibili.blockedUntil,
                          locale,
                        ),
                      })
                    : t("admin.bilibili.notBlocked"),
                ],
              ]
            : [
                [
                  t("admin.sources.lastSuccess"),
                  when(overview?.sources[source].lastSuccessAt),
                ],
              ]
        }
        onChange={(next) =>
          data &&
          sources.setData({
            ...data,
            schedules: data.schedules.map((entry) =>
              entry.source === next.source ? next : entry,
            ),
          })
        }
      />
    ) : null
  }
  const skipped = (source: ScheduleSource) =>
    data?.schedules.find((entry) => entry.source === source)?.skipped ? (
      <Badge>{t("admin.overview.skipped")}</Badge>
    ) : null
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
          <SourceSection
            source="x"
            hint={t("admin.sources.x.hint")}
            chips={skipped("x")}
          >
            {schedule("x")}
            <FetchPanel source="x" sources={data} />
          </SourceSection>
          <SourceSection
            source="youtube"
            hint={t("admin.sources.youtube.hint")}
            chips={skipped("youtube")}
          >
            {schedule("youtube")}
            <FetchPanel source="youtube" sources={data} />
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
          </SourceSection>
          <SourceSection
            source="bilibili"
            hint={t("admin.sources.bilibili.hint")}
            chips={skipped("bilibili")}
          >
            {schedule("bilibili")}
            <BilibiliSource
              status={data.bilibili}
              onAdded={() => void sources.reload()}
            />
          </SourceSection>
        </>
      )}
    </div>
  )
}
