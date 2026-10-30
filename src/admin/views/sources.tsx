import {
  useEffect,
  useId,
  useState,
  type ReactNode,
  type SubmitEvent,
} from "react"
import {
  ArrowDown,
  ArrowUp,
  CalendarCheck,
  CalendarClock,
  ChevronRight,
  Download,
  History,
  Info,
  KeyRound,
  ListChecks,
  Lock,
  Play,
  Radio,
  RadioTower,
  RotateCw,
  Save,
  ShieldAlert,
  Tv,
  type LucideIcon,
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
  ChainMethod,
  FetchItem,
  FetchMethod,
  FetchMode,
  LiveProbeStatus,
  PushStatus,
  Run,
  ScheduleSource,
  SourceChain,
  SourceSchedule,
  SourceSync,
  Sources,
} from "@/admin/types"
import {
  Badge,
  Button,
  Callout,
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
import {
  explainCode,
  metricLabel,
  RunMark,
  runOutcome,
  stepIcons,
  stepResult,
  windowText,
} from "@/admin/views/run-parts"
import { cn } from "@/lib/utils"

const intervals = [15, 30, 60, 120, 180, 360, 720, 1440, 2880, 10080]

/** When a finished YouTube video is reread, in minutes after its anchor
 * (ytsource.FollowUpOffsets). */
const followUpOffsets = [60, 1440, 4320, 10080]

const skipFlags: Record<ScheduleSource, string> = {
  x: "SKIP_X",
  youtube: "SKIP_YOUTUBE",
  bilibili: "SKIP_BILIBILI",
}

const sameSync = (a: SourceSync | null, b: SourceSync | null) =>
  JSON.stringify(a) === JSON.stringify(b)

/** A relative time with its full date as a tooltip, or a dash. */
function When({ value }: { value: string | null | undefined }) {
  const { locale } = useAppSettings()
  return value ? (
    <time dateTime={value} title={formatDateTime(value, locale)}>
      {formatRelative(value, locale)}
    </time>
  ) : (
    <>—</>
  )
}

/**
 * A source in the rail: its timer switch and its method chain (ADR-0004),
 * which timed reads try from the top. Switches and moves save at once. A
 * method this process cannot run is greyed with the variable it needs, a
 * source SKIP_* disables is locked, and Bilibili's one method is read-only.
 * Selecting the entry shows the source's settings and fetch beside it.
 */
function RailEntry({
  schedule,
  chips,
  selected,
  onSelect,
  onChange,
}: {
  schedule: SourceSchedule
  chips?: ReactNode
  selected: boolean
  onSelect: () => void
  onChange: (schedule: SourceSchedule) => void
}) {
  const { t } = useAppSettings()
  const { api, act, overview } = useAdmin()
  const [busy, setBusy] = useState(false)
  // The chain as the operator just arranged it, while the save is on its way.
  const [pending, setPending] = useState<SourceChain | null>(null)
  const source = schedule.source
  const Icon = stepIcons[source] || Tv
  const locked = schedule.skipped
  const timersOff = overview ? !overview.scheduler.enabled : false
  const running = schedule.enabled && !locked && !timersOff
  const chain = pending || schedule.chain
  const methods = chain.methods
  const label = (id: string) => t(`admin.chain.method.${source}.${id}`)
  const save = async (
    body: Parameters<typeof api.updateSourceSchedule>[1],
    next?: SourceChain,
  ) => {
    if (busy) return
    setBusy(true)
    setPending(next || null)
    const saved = await act(
      () => api.updateSourceSchedule(source, body),
      "admin.notice.scheduleSaved",
    )
    setBusy(false)
    setPending(null)
    if (saved) onChange(saved)
  }
  const saveMethods = (next: ChainMethod[]) =>
    save(
      { chain: { methods: next.map(({ id, enabled }) => ({ id, enabled })) } },
      { ...chain, methods: next },
    )
  const move = (index: number, by: number) => {
    const next = [...methods]
    const [method] = next.splice(index, 1)
    next.splice(index + by, 0, method)
    saveMethods(next)
  }
  const enabledCount = methods.filter((method) => method.enabled).length
  return (
    <li
      className={cn(
        "adm-rail-entry",
        selected && "is-selected",
        locked && "is-locked",
      )}
    >
      <button
        type="button"
        className="adm-rail-head"
        aria-pressed={selected}
        onClick={onSelect}
      >
        <span className="adm-source-icon">
          <Icon aria-hidden />
        </span>
        <span className="adm-source-name">
          <strong>{t(`admin.step.${source}`)}</strong>
          <small>
            {locked ? <Lock aria-hidden /> : null}
            {locked
              ? t("admin.sources.locked")
              : running
                ? t("admin.timer.on", {
                    interval: formatInterval(schedule.intervalMinutes, t),
                  })
                : schedule.enabled
                  ? t("admin.sources.timerBlocked")
                  : t("admin.sources.timerOff")}
          </small>
        </span>
        <ChevronRight className="adm-rail-chevron" aria-hidden />
      </button>
      <div className="adm-rail-body">
        {chips ? <span className="adm-chip-row">{chips}</span> : null}
        {locked ? (
          <p className="adm-source-note">
            <Lock aria-hidden />
            {t("admin.timer.skipped", { flag: skipFlags[source] })}
          </p>
        ) : null}
        <Switch
          checked={schedule.enabled}
          disabled={locked || busy}
          onChange={(enabled) => save({ enabled })}
          label={t("admin.timer.enable")}
        />
        <fieldset className="adm-chain" disabled={locked}>
          <legend className="adm-field-label">{t("admin.chain.title")}</legend>
          <ol className="adm-chain-list">
            {methods.map((method, index) => (
              <li
                key={method.id}
                className={cn(
                  "adm-chain-item",
                  (!method.available || !method.enabled) && "is-off",
                )}
              >
                <span className="adm-chain-rank" aria-hidden>
                  {index + 1}
                </span>
                <span className="adm-chain-name">
                  <strong>{label(method.id)}</strong>
                  {!method.available && method.requires ? (
                    <small>
                      <KeyRound aria-hidden />
                      {t("admin.chain.needs", { variable: method.requires })}
                    </small>
                  ) : null}
                </span>
                {chain.fixed ? null : (
                  <span className="adm-chain-actions">
                    <Switch
                      compact
                      checked={method.enabled}
                      // The last enabled method stays on: a chain needs one.
                      disabled={method.enabled && enabledCount === 1}
                      onChange={(enabled) =>
                        saveMethods(
                          methods.map((entry) =>
                            entry.id === method.id
                              ? { ...entry, enabled }
                              : entry,
                          ),
                        )
                      }
                      label={label(method.id)}
                    />
                    <Button
                      size="sm"
                      variant="ghost"
                      icon={ArrowUp}
                      disabled={index === 0}
                      onClick={() => move(index, -1)}
                      aria-label={t("admin.chain.up", {
                        method: label(method.id),
                      })}
                      title={t("admin.chain.up", { method: label(method.id) })}
                    />
                    <Button
                      size="sm"
                      variant="ghost"
                      icon={ArrowDown}
                      disabled={index === methods.length - 1}
                      onClick={() => move(index, 1)}
                      aria-label={t("admin.chain.down", {
                        method: label(method.id),
                      })}
                      title={t("admin.chain.down", {
                        method: label(method.id),
                      })}
                    />
                  </span>
                )}
              </li>
            ))}
          </ol>
          {chain.fixed ? (
            <p className="adm-field-hint">{t("admin.chain.fixed")}</p>
          ) : null}
          {source === "youtube" &&
          !methods.some((method) => method.id === "api" && method.enabled) ? (
            <p className="adm-field-hint">{t("admin.chain.youtubeAPIOff")}</p>
          ) : null}
        </fieldset>
      </div>
    </li>
  )
}

/** A source's timer: when it runs next, how the last run went, Run now, and
 * its interval and, for X and YouTube, how much each run reads. Saving keeps
 * the timer on or off as it is. */
function TimerPanel({
  schedule,
  lastSuccess,
  onChange,
}: {
  schedule: SourceSchedule
  lastSuccess: string | null | undefined
  onChange: (schedule: SourceSchedule) => void
}) {
  const { t } = useAppSettings()
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
  return (
    <Panel
      level={3}
      icon={CalendarClock}
      title={t("admin.timer.title")}
      description={t(`admin.timer.description.${source}`, {
        floor: formatInterval(schedule.minIntervalMinutes, t),
      })}
      actions={
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
      }
    >
      <Facts
        items={[
          [
            t("admin.timer.nextRun"),
            running ? <When value={schedule.nextRunAt} /> : "—",
          ],
          [
            t("admin.timer.lastRun"),
            schedule.lastRun ? (
              <>
                <StatusBadge status={schedule.lastRun.status} />
                <When value={schedule.lastRun.at} />
              </>
            ) : (
              t("admin.timer.never")
            ),
          ],
          [t("admin.sources.lastSuccess"), <When value={lastSuccess} />],
        ]}
      />
      <form
        className="adm-form"
        onSubmit={async (event) => {
          event.preventDefault()
          setBusy("save")
          const next = await act(
            () =>
              api.updateSourceSchedule(source, {
                intervalMinutes: minutes,
                ...(sync ? { sync } : {}),
              }),
            "admin.notice.scheduleSaved",
          )
          setBusy("")
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
            busy={busy === "save"}
            disabled={!dirty || busy !== ""}
          >
            {t("admin.action.save")}
          </Button>
        </div>
      </form>
    </Panel>
  )
}

/**
 * The X API in X's chain (ADR-0004): where the chain has it, the timeline
 * failures that open it and its daily cap, with today's usage and each
 * account's timeline failure streak.
 */
function XAPIPanel({
  schedule,
  accounts,
  onChange,
}: {
  schedule: SourceSchedule
  accounts: string[]
  onChange: (schedule: SourceSchedule) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const chain = schedule.chain
  const [gate, setGate] = useState(chain.apiGate ?? 0)
  const [cap, setCap] = useState(chain.apiDailyCap ?? 1)
  const [busy, setBusy] = useState(false)
  useEffect(() => setGate(chain.apiGate ?? 0), [chain.apiGate])
  useEffect(() => setCap(chain.apiDailyCap ?? 1), [chain.apiDailyCap])
  const rank = chain.methods.findIndex((method) => method.id === "api")
  const method = chain.methods[rank]
  const failures = chain.timelineFailures || {}
  const streaks = [...new Set([...accounts, ...Object.keys(failures)])]
  const dirty = gate !== chain.apiGate || cap !== chain.apiDailyCap
  return (
    <Panel
      level={3}
      icon={KeyRound}
      title={t("admin.chain.method.x.api")}
      description={t("admin.xapi.description")}
      actions={
        !method ? null : !method.available ? (
          <Badge tone="honey" icon={KeyRound}>
            {t("admin.chain.needs", { variable: method.requires || "" })}
          </Badge>
        ) : method.enabled ? (
          <Badge tone="leaf">
            {t("admin.xapi.inChain", { rank: rank + 1 })}
          </Badge>
        ) : (
          <Badge>{t("admin.xapi.offInChain")}</Badge>
        )
      }
    >
      <Facts
        items={[
          [
            t("admin.chain.usage"),
            t("admin.chain.usageValue", {
              posts: chain.apiUsage?.posts ?? 0,
              cap: chain.apiDailyCap ?? 0,
            }),
          ],
          ...streaks.map((account): [ReactNode, ReactNode] => [
            `${t("admin.chain.streak")} · @${account}`,
            String(failures[account] ?? 0),
          ]),
        ]}
      />
      <form
        className="adm-form"
        onSubmit={async (event) => {
          event.preventDefault()
          setBusy(true)
          const next = await act(
            () =>
              api.updateSourceSchedule("x", {
                chain: { apiGate: gate, apiDailyCap: cap },
              }),
            "admin.notice.scheduleSaved",
          )
          setBusy(false)
          if (next) onChange(next)
        }}
      >
        <div className="adm-form-row">
          <Field
            label={t("admin.chain.apiGate")}
            hint={t("admin.chain.apiGateHint")}
          >
            <input
              type="number"
              min={0}
              max={100}
              value={gate}
              onChange={(e) => setGate(Number(e.target.value))}
              required
            />
          </Field>
          <Field
            label={t("admin.chain.apiDailyCap")}
            hint={t("admin.chain.apiDailyCapHint")}
          >
            <input
              type="number"
              min={1}
              max={10000}
              value={cap}
              onChange={(e) => setCap(Number(e.target.value))}
              required
            />
          </Field>
        </div>
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
            disabled={!dirty || busy}
          >
            {t("admin.action.save")}
          </Button>
        </div>
      </form>
    </Panel>
  )
}

/**
 * A labelled segmented choice. It is a group rather than a <label>, so the
 * label text never presses the first segment.
 */
function Choice({
  label,
  hint,
  children,
}: {
  label: string
  hint: ReactNode
  children: ReactNode
}) {
  return (
    <div className="adm-field">
      <span className="adm-field-label">{label}</span>
      {children}
      <span className="adm-field-hint">{hint}</span>
    </div>
  )
}

/** The mode a method serves, keeping the current one when it can. */
function servedMode(method: FetchMethod, mode: FetchMode) {
  return method.modes.includes(mode) ? mode : method.modes[0]
}

/**
 * A manual fetch, method first: the listing method, what it reads, its count
 * or dates, and the steps that follow. The run fails with the method's code
 * when the method fails; nothing falls back.
 */
function FetchForm({
  source,
  methods,
  hasRecords,
  disabled,
  onStarted,
}: {
  source: "x" | "youtube"
  methods: FetchMethod[]
  hasRecords: boolean
  disabled: boolean
  onStarted: (run: Run) => void
}) {
  const { t } = useAppSettings()
  const { start } = useAdmin()
  const [methodId, setMethodId] = useState(
    () => (methods.find((item) => item.available) || methods[0])?.id,
  )
  const [mode, setMode] = useState<FetchMode>("count")
  const [count, setCount] = useState(source === "x" ? 50 : 15)
  const [days, setDays] = useState(30)
  const [startDate, setStartDate] = useState("")
  const [endDate, setEndDate] = useState(japanToday())
  const [extract, setExtract] = useState(false)
  const [translate, setTranslate] = useState(false)
  const [refresh, setRefresh] = useState(false)
  const [busy, setBusy] = useState(false)
  const method = methods.find((item) => item.id === methodId) || methods[0]
  if (!method) return null
  const current = servedMode(method, mode)
  const limit = Math.min(count, method.maxCount)
  const needsRecords = current === "before" && !hasRecords
  const submit = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    const run = await start({
      kind: "fetch",
      source,
      method: method.id,
      window:
        current === "count"
          ? { mode: current, count: limit }
          : current === "range"
            ? { mode: current, startDate, endDate }
            : current === "before"
              ? { mode: current, days }
              : { mode: current },
      options: {
        extract: method.extract && extract,
        translate,
        refresh: method.refresh && refresh,
      },
    })
    setBusy(false)
    if (run) onStarted(run)
  }
  return (
    <form className="adm-form" onSubmit={submit}>
      <Choice
        label={t("admin.fetch.method")}
        hint={t(`admin.fetch.methodHint.${source}.${method.id}`)}
      >
        <Segmented
          label={t("admin.fetch.method")}
          value={method.id}
          onChange={setMethodId}
          options={methods.map((item) => ({
            value: item.id,
            label: (
              <>
                {item.available ? null : <KeyRound aria-hidden />}
                {t(`admin.fetch.method.${source}.${item.id}`)}
              </>
            ),
          }))}
        />
      </Choice>
      {method.available ? null : (
        <p className="adm-inline-error">
          <KeyRound aria-hidden />
          {method.requires
            ? t("admin.fetch.needs", { variable: method.requires })
            : t("admin.fetch.unavailable")}
        </p>
      )}
      <Choice
        label={t("admin.fetch.selection")}
        hint={t(
          current === "all"
            ? `admin.fetch.modeHint.all.${source}`
            : `admin.fetch.modeHint.${current}.${source}`,
        )}
      >
        <Segmented
          label={t("admin.fetch.selection")}
          value={current}
          onChange={setMode}
          options={method.modes.map((value) => ({
            value,
            label: t(`admin.fetch.mode.${value}`),
          }))}
        />
      </Choice>
      {current === "count" ? (
        <Field
          label={t(
            source === "youtube"
              ? "admin.fetch.count.youtube"
              : "admin.fetch.count",
          )}
          hint={t("admin.fetch.countRange", { max: method.maxCount })}
        >
          <input
            type="number"
            min={1}
            max={method.maxCount}
            value={limit}
            onChange={(e) => setCount(Number(e.target.value))}
            required
          />
        </Field>
      ) : current === "range" ? (
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
      ) : current === "before" ? (
        <Field
          label={t("admin.fetch.backfillDays")}
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
      ) : null}
      {needsRecords ? (
        <p className="adm-inline-error">{t("admin.fetch.noRecords")}</p>
      ) : null}
      <fieldset className="adm-fetch-options">
        <legend>{t("admin.fetch.options")}</legend>
        {method.extract ? (
          <Switch
            checked={extract}
            onChange={setExtract}
            label={t("admin.fetch.option.extract")}
            description={t("admin.fetch.option.extractHint")}
          />
        ) : null}
        <Switch
          checked={translate}
          onChange={setTranslate}
          label={t("admin.fetch.option.translate")}
          description={t("admin.fetch.option.translateHint")}
        />
        {method.refresh ? (
          <Switch
            checked={refresh}
            onChange={setRefresh}
            label={t("admin.fetch.option.refresh")}
            description={t("admin.fetch.option.refreshHint")}
          />
        ) : null}
      </fieldset>
      <div className="adm-form-actions">
        <Button
          type="submit"
          variant="primary"
          icon={Download}
          busy={busy}
          disabled={disabled || !method.available || needsRecords}
        >
          {t("admin.fetch.run")}
        </Button>
      </div>
    </form>
  )
}

/** What a fetch step counted, as the console's summary reads it. */
interface FetchCounts {
  listed?: number
  new?: number
  stored?: number
  failed?: number
  pages?: number
  /** YouTube API fetches: videos whose details were checked, and quota units. */
  inspected?: number
  apiRequests?: number
  reachedEnd?: boolean
  items?: FetchItem[]
  errors?: string[]
  error?: string
}

/**
 * The newest manual fetch of a source: its outcome, what the method listed
 * split into new, already stored and failed records, the pages it read,
 * whether it reached the end, and the first new records.
 */
function FetchSummary({
  run,
  source,
  onOpenRuns,
}: {
  run: Run
  source: "x" | "youtube"
  onOpenRuns: () => void
}) {
  const { t, locale } = useAppSettings()
  const outcome = runOutcome(run)
  const window = run.fetchWindows?.[source]
  const counts = (stepResult(run, source) || {}) as FetchCounts
  const progress = run.progress[source] || "pending"
  const finished = progress !== "pending" && progress !== "running"
  const items = counts.items || []
  return (
    <section className="adm-fetch-summary" aria-label={t("admin.fetch.last")}>
      <header>
        <RunMark status={outcome} />
        <span className="adm-fetch-summary-title">
          <strong>{t("admin.fetch.last")}</strong>
          <small>
            {window ? `${windowText(window, source, t)} · ` : ""}
            <time
              dateTime={run.createdAt}
              title={formatDateTime(run.createdAt, locale)}
            >
              {formatRelative(run.createdAt, locale)}
            </time>
          </small>
        </span>
        <StatusBadge status={outcome} />
      </header>
      {counts.error ? (
        <p className="adm-inline-error">
          <ShieldAlert aria-hidden />
          <code>{counts.error}</code>
        </p>
      ) : null}
      {finished && counts.listed !== undefined ? (
        <>
          <Facts
            items={[
              [t("admin.metric.listed"), counts.listed],
              [t("admin.metric.new"), counts.new],
              [t("admin.metric.stored"), counts.stored],
              [t("admin.metric.failed"), counts.failed],
              [t("admin.metric.pages"), counts.pages],
              ...(counts.inspected === undefined
                ? []
                : ([
                    [t("admin.metric.inspected"), counts.inspected],
                    [t("admin.metric.apiRequests"), counts.apiRequests],
                  ] as [string, number | undefined][])),
            ]}
          />
          <p className="adm-field-hint">
            {counts.reachedEnd
              ? t("admin.fetch.reachedEnd")
              : t("admin.fetch.repeat")}
          </p>
          {items.length ? (
            <div className="adm-fetch-items">
              <span>
                {t("admin.fetch.newItems", { count: counts.new || 0 })}
              </span>
              <ul>
                {items.map((item) => (
                  <li key={item.id}>
                    <code>{item.id}</code>
                    <span>{item.title}</span>
                    {item.account ? <small>@{item.account}</small> : null}
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
          {counts.errors?.length ? (
            <div className="adm-metric-list">
              <span>{metricLabel("errors", t)}</span>
              <span className="adm-chip-row">
                {counts.errors.map((code, index) => (
                  <code key={index} title={explainCode(code, t) || ""}>
                    {code}
                  </code>
                ))}
              </span>
            </div>
          ) : null}
        </>
      ) : null}
      <div className="adm-form-actions">
        <Button size="sm" variant="ghost" icon={History} onClick={onOpenRuns}>
          {t("admin.fetch.openRuns")}
        </Button>
      </div>
    </section>
  )
}

/** The manual fetch of the selected source: the form, then the newest
 * fetch's summary. */
function FetchPanel({
  source,
  sources,
  onOpenRuns,
}: {
  source: "x" | "youtube"
  sources: Sources
  onOpenRuns: () => void
}) {
  const { t, locale } = useAppSettings()
  const { api, overview } = useAdmin()
  const state = sources[source]
  const [started, setStarted] = useState<string | null>(null)
  // The run started here, or else the newest manual fetch of this source
  // among the recent runs.
  const recent = overview?.recentRuns.find(
    (run) => run.fetchWindows?.[source]?.method,
  )
  const shown = started || recent?.id || null
  const last = useResource(
    () =>
      shown ? api.run(shown).then((data) => data.run) : Promise.resolve(null),
    [shown],
  )
  const oldest =
    source === "x"
      ? sources.x.accounts.map(
          (account) => [account.account, account.oldest] as const,
        )
      : [[t("admin.sources.channel"), sources.youtube.oldest] as const]
  return (
    <Panel
      icon={Download}
      title={t("admin.fetch.headingFor", {
        source: t(`admin.step.${source}`),
      })}
      description={t("admin.fetch.intro")}
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
        methods={state.methods}
        hasRecords={oldest.some(([, time]) => Boolean(time))}
        disabled={state.skipped || Boolean(overview?.activeRun)}
        onStarted={(run) => setStarted(run.id)}
      />
      {last.data &&
      last.data.id === shown &&
      last.data.fetchWindows?.[source] ? (
        <FetchSummary run={last.data} source={source} onOpenRuns={onOpenRuns} />
      ) : shown ? null : (
        <p className="adm-fetch-empty">{t("admin.fetch.noneYet")}</p>
      )}
    </Panel>
  )
}

/** One step of YouTube's progressive confirmation. */
function Stage({
  icon: StageIcon,
  title,
  description,
  badge,
  children,
}: {
  icon: LucideIcon
  title: ReactNode
  description: ReactNode
  badge: ReactNode
  children: ReactNode
}) {
  const id = useId()
  return (
    <li className="adm-confirm-step">
      <section aria-labelledby={id}>
        <header>
          <span className="adm-confirm-mark">
            <StageIcon aria-hidden />
          </span>
          <div>
            <h4 id={id}>{title}</h4>
            <p>{description}</p>
          </div>
          {badge}
        </header>
        {children}
      </section>
    </li>
  )
}

function PushStage({
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
    <Stage
      icon={Radio}
      title={t("admin.push.title")}
      description={t("admin.push.description")}
      badge={
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
    </Stage>
  )
}

function LiveProbeStage({
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
    <Stage
      icon={RadioTower}
      title={t("admin.liveProbe.title")}
      description={t("admin.liveProbe.description", { max: probe.maxVideos })}
      badge={
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
    </Stage>
  )
}

/**
 * YouTube's progressive confirmation: push queues a read as a video appears,
 * the live-status probe follows streams around their start, and follow-up
 * checks reread finished videos. The follow-ups have no setting; they ride
 * on every YouTube read.
 */
function ConfirmationPanel({
  youtube,
  onChange,
}: {
  youtube: Sources["youtube"]
  onChange: (youtube: Sources["youtube"]) => void
}) {
  const { t } = useAppSettings()
  return (
    <Panel
      level={3}
      icon={ListChecks}
      title={t("admin.confirm.title")}
      description={t("admin.confirm.description")}
    >
      <ol className="adm-confirm-steps">
        <PushStage
          push={youtube.push}
          onChange={(push) => onChange({ ...youtube, push })}
        />
        <LiveProbeStage
          probe={youtube.liveProbe}
          onChange={(liveProbe) => onChange({ ...youtube, liveProbe })}
        />
        <Stage
          icon={CalendarCheck}
          title={t("admin.followUp.title")}
          description={t("admin.followUp.description")}
          badge={<Badge>{t("admin.followUp.automatic")}</Badge>}
        >
          <ul
            className="adm-chip-row adm-follow-ups"
            aria-label={t("admin.followUp.offsets")}
          >
            {followUpOffsets.map((minutes) => (
              <li key={minutes}>
                <Badge tone="sky">{formatInterval(minutes, t)}</Badge>
              </li>
            ))}
          </ul>
        </Stage>
      </ol>
    </Panel>
  )
}

const sourceIds: ScheduleSource[] = ["x", "youtube", "bilibili"]

/**
 * The Sources page: a rail with each source's timer switch and method chain,
 * the selected source's settings beside it, and its manual fetch below them.
 * On a narrow screen the three stack in that order.
 */
export function SourcesView({ tab, navigate }: ViewProps) {
  const { t, locale } = useAppSettings()
  const { api, overview } = useAdmin()
  const sources = useResource(() => api.sources(), [])
  const data = sources.data
  const selected = sourceIds.includes(tab as ScheduleSource)
    ? (tab as ScheduleSource)
    : "x"
  const timersOff = overview ? !overview.scheduler.enabled : false
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
  const chips = (source: ScheduleSource) =>
    source === "bilibili" && blockedUntil ? (
      <Badge tone="danger" icon={ShieldAlert}>
        {t("admin.bilibili.blockedUntil", {
          time: formatDateTime(blockedUntil, locale),
        })}
      </Badge>
    ) : null
  const current = schedule(selected)
  const heading = t("admin.sources.settingsFor", {
    source: t(`admin.step.${selected}`),
  })
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.sources.title")}
        description={t("admin.sources.description")}
      />
      {!data ? (
        <Loading />
      ) : (
        <div className="adm-sources-layout">
          <section
            className="adm-source-rail"
            aria-label={t("admin.sources.rail")}
          >
            {timersOff ? (
              <Callout tone="honey" icon={Info}>
                <p>{t("admin.timer.schedulerOff")}</p>
              </Callout>
            ) : null}
            <ul className="adm-rail-list">
              {sourceIds.map((source) => {
                const found = schedule(source)
                return found ? (
                  <RailEntry
                    key={source}
                    schedule={found}
                    chips={chips(source)}
                    selected={selected === source}
                    onSelect={() => navigate("sources", source)}
                    onChange={setSchedule}
                  />
                ) : null
              })}
            </ul>
            <p className="adm-rail-hint">{t("admin.chain.description")}</p>
          </section>
          <section className="adm-source-detail" aria-label={heading}>
            <header className="adm-source-header">
              <h2>{heading}</h2>
              <p>{t(`admin.sources.${selected}.hint`)}</p>
            </header>
            {current ? (
              <TimerPanel
                key={selected}
                schedule={current}
                lastSuccess={
                  selected === "bilibili"
                    ? data.bilibili.lastSuccessAt
                    : overview?.sources[selected].lastSuccessAt
                }
                onChange={setSchedule}
              />
            ) : null}
            {selected === "x" && current ? (
              <XAPIPanel
                schedule={current}
                accounts={data.x.accounts.map((entry) => entry.account)}
                onChange={setSchedule}
              />
            ) : null}
            {selected === "youtube" ? (
              <ConfirmationPanel
                youtube={data.youtube}
                onChange={(youtube) => sources.setData({ ...data, youtube })}
              />
            ) : null}
          </section>
          <div className="adm-source-fetch">
            {selected === "bilibili" ? (
              <BilibiliSource
                status={data.bilibili}
                onAdded={() => void sources.reload()}
              />
            ) : (
              <FetchPanel
                key={selected}
                source={selected}
                sources={data}
                onOpenRuns={() => navigate("runs")}
              />
            )}
          </div>
        </div>
      )}
    </div>
  )
}
