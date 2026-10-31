import {
  useEffect,
  useId,
  useState,
  type ComponentType,
  type ReactNode,
  type SubmitEvent,
} from "react"
import {
  ArrowDown,
  ArrowUp,
  CalendarCheck,
  CalendarClock,
  ChevronDown,
  CircleHelp,
  Download,
  Gauge,
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
  SlidersHorizontal,
  Tv,
  Undo2,
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
  SourceChainUpdate,
  SourceSchedule,
  SourceSync,
  Sources,
  SyncSettings,
  YTDLPStatus,
} from "@/admin/types"
import {
  Badge,
  Button,
  Callout,
  ConfirmButton,
  Facts,
  Field,
  FieldGroup,
  IconButton,
  Loading,
  Modal,
  PageHeader,
  Panel,
  Segmented,
  StatusBadge,
  Switch,
  type Tone,
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
import { SyncSettingsForm } from "@/admin/views/sync-settings"
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

type TimerState = "locked" | "running" | "blocked" | "off"

/** Whether a source's timer runs in this process. */
function timerState(schedule: SourceSchedule, timersOff: boolean): TimerState {
  if (schedule.skipped) return "locked"
  if (!schedule.enabled) return "off"
  return timersOff ? "blocked" : "running"
}

const timerTones: Record<TimerState, Tone> = {
  locked: "neutral",
  running: "leaf",
  blocked: "honey",
  off: "neutral",
}

/** A timer state as its tab and summary name it. */
function timerLabel(
  schedule: SourceSchedule,
  state: TimerState,
  t: ReturnType<typeof useAppSettings>["t"],
) {
  return state === "locked"
    ? t("admin.sources.locked")
    : state === "running"
      ? t("admin.timer.on", {
          interval: formatInterval(schedule.intervalMinutes, t),
        })
      : state === "blocked"
        ? t("admin.sources.timerBlocked")
        : t("admin.sources.timerOff")
}

/** A source's tab: its name and a dot for whether its timer runs here. */
function SourceTab({
  schedule,
  selected,
  onSelect,
}: {
  schedule: SourceSchedule
  selected: boolean
  onSelect: () => void
}) {
  const { t } = useAppSettings()
  const { overview } = useAdmin()
  const source = schedule.source
  const Icon = stepIcons[source] || Tv
  const state = timerState(
    schedule,
    overview ? !overview.scheduler.enabled : false,
  )
  return (
    <button
      type="button"
      role="tab"
      aria-selected={selected}
      className={cn(selected && "is-active", state === "locked" && "is-locked")}
      onClick={onSelect}
      title={timerLabel(schedule, state, t)}
    >
      <span className="adm-source-icon">
        <Icon aria-hidden />
      </span>
      <strong>{t(`admin.step.${source}`)}</strong>
      {state === "locked" ? (
        <Lock aria-hidden className="adm-source-lock" />
      ) : (
        <i className={cn("adm-source-dot", `is-${state}`)} aria-hidden />
      )}
      <span className="adm-visually-hidden">
        {timerLabel(schedule, state, t)}
      </span>
    </button>
  )
}

/**
 * The selected source at a glance: what it reads, whether its timer runs,
 * when it runs next, how the last run went and its last successful read,
 * with the timer switch and Run now. The switch saves at once and keeps any
 * unsaved timed-read edits below.
 */
function SourceSummary({
  schedule,
  lastSuccess,
  chips,
  onChange,
}: {
  schedule: SourceSchedule
  lastSuccess: string | null | undefined
  chips?: ReactNode
  onChange: (schedule: SourceSchedule) => void
}) {
  const { t } = useAppSettings()
  const { api, act, start, overview } = useAdmin()
  const [busy, setBusy] = useState("")
  const id = useId()
  const source = schedule.source
  const Icon = stepIcons[source] || Tv
  const locked = schedule.skipped
  const state = timerState(
    schedule,
    overview ? !overview.scheduler.enabled : false,
  )
  const toggle = async (enabled: boolean) => {
    setBusy("toggle")
    const saved = await act(
      () => api.updateSourceSchedule(source, { enabled }),
      "admin.notice.scheduleSaved",
    )
    setBusy("")
    if (saved) onChange(saved)
  }
  const runNow = async () => {
    setBusy("run")
    await start({ kind: "source", source })
    setBusy("")
  }
  return (
    <section className="adm-panel adm-source-summary" aria-labelledby={id}>
      <header className="adm-source-summary-head">
        <span className="adm-source-icon">
          <Icon aria-hidden />
        </span>
        <div className="adm-source-summary-title">
          <h2 id={id}>{t(`admin.step.${source}`)}</h2>
          <p>{t(`admin.sources.${source}.hint`)}</p>
        </div>
        <div className="adm-source-summary-actions">
          <Switch
            checked={schedule.enabled}
            disabled={locked || busy !== ""}
            onChange={toggle}
            label={t("admin.timer.enable")}
          />
          <Button
            size="sm"
            icon={Play}
            busy={busy === "run"}
            disabled={locked || busy !== "" || Boolean(overview?.activeRun)}
            onClick={runNow}
            title={t("admin.timer.runNowHint")}
          >
            {t("admin.timer.runNow")}
          </Button>
        </div>
      </header>
      {locked ? (
        <p className="adm-source-note">
          <Lock aria-hidden />
          {t("admin.timer.skipped", { flag: skipFlags[source] })}
        </p>
      ) : null}
      <dl className="adm-source-stats">
        <div>
          <dt>{t("admin.sources.timer")}</dt>
          <dd>
            <Badge tone={timerTones[state]}>
              {state === "locked" ? (
                <Lock aria-hidden />
              ) : (
                <i
                  className={cn("adm-source-dot", `is-${state}`)}
                  aria-hidden
                />
              )}
              {timerLabel(schedule, state, t)}
            </Badge>
            {chips}
          </dd>
        </div>
        <div>
          <dt>{t("admin.timer.nextRun")}</dt>
          <dd>
            {state === "running" ? <When value={schedule.nextRunAt} /> : "—"}
          </dd>
        </div>
        <div>
          <dt>{t("admin.timer.lastRun")}</dt>
          <dd>
            {schedule.lastRun ? (
              <>
                <StatusBadge status={schedule.lastRun.status} />
                <When value={schedule.lastRun.at} />
              </>
            ) : (
              t("admin.timer.never")
            )}
          </dd>
        </div>
        <div>
          <dt>{t("admin.sources.lastSuccess")}</dt>
          <dd>
            <When value={lastSuccess} />
          </dd>
        </div>
      </dl>
    </section>
  )
}

/** A question-mark button that opens a short explanation in a dialog. */
function HelpButton({
  title,
  children,
}: {
  title: string
  children: ReactNode
}) {
  const { t } = useAppSettings()
  const [open, setOpen] = useState(false)
  return (
    <>
      <IconButton
        icon={CircleHelp}
        label={t("admin.help.about", { topic: title })}
        className="adm-help-button"
        onClick={() => setOpen(true)}
      />
      <Modal open={open} title={title} onClose={() => setOpen(false)}>
        <div className="adm-help">{children}</div>
      </Modal>
    </>
  )
}

/**
 * A field whose explanation, when it has one, sits behind a question mark
 * beside its label. The label is its own element, so pressing the question
 * mark never focuses the control.
 */
function HelpField({
  label,
  help,
  hint,
  children,
}: {
  label: string
  help?: ReactNode
  hint?: ReactNode
  children: (id: string) => ReactNode
}) {
  const id = useId()
  return (
    <div className="adm-field">
      <span className="adm-field-label adm-label-help">
        <label htmlFor={id}>{label}</label>
        {help ? <HelpButton title={label}>{help}</HelpButton> : null}
      </span>
      {children(id)}
      {hint ? <span className="adm-field-hint">{hint}</span> : null}
    </div>
  )
}

/**
 * A folded section: a header row that opens onto its settings. Controls in
 * the header, such as a quick switch or a status badge, work while it is
 * closed, and pressing anywhere else on the header opens or closes it. The
 * body stays mounted, so unsaved edits survive folding.
 */
function Fold({
  icon: FoldIcon,
  title,
  level = 3,
  help,
  hint,
  controls,
  className,
  children,
}: {
  icon: LucideIcon | ComponentType<{ "aria-hidden"?: boolean }>
  title: string
  /** 4 for a fold inside a panel, such as a confirmation step. */
  level?: 3 | 4
  help?: ReactNode
  hint?: ReactNode
  controls?: ReactNode
  className?: string
  children: ReactNode
}) {
  const [open, setOpen] = useState(false)
  const id = useId()
  const Heading = level === 4 ? "h4" : "h3"
  return (
    <section
      className={cn("adm-fold", open && "is-open", className)}
      aria-labelledby={`${id}-title`}
    >
      <header
        className="adm-fold-head"
        onClick={(event) => {
          // The title button, the help and the controls handle their own.
          if (
            !(event.target as Element).closest(
              "button, a, input, label, select",
            )
          )
            setOpen(!open)
        }}
      >
        <span className="adm-fold-mark">
          <FoldIcon aria-hidden />
        </span>
        <span className="adm-fold-title">
          <span className="adm-fold-name">
            <Heading id={`${id}-title`}>
              <button
                type="button"
                aria-expanded={open}
                aria-controls={`${id}-body`}
                onClick={() => setOpen(!open)}
              >
                {title}
              </button>
            </Heading>
            {help ? <HelpButton title={title}>{help}</HelpButton> : null}
          </span>
          {hint ? <small>{hint}</small> : null}
        </span>
        {controls ? (
          <span className="adm-fold-controls">{controls}</span>
        ) : null}
        <ChevronDown aria-hidden className="adm-fold-chevron" />
      </header>
      <div id={`${id}-body`} className="adm-fold-body" hidden={!open}>
        {children}
      </div>
    </section>
  )
}

/** A count against its limit: the number, the limit and a bar. */
function Meter({
  label,
  value,
  of,
  unit,
}: {
  label: string
  value: number
  of: number
  unit: string
}) {
  const share = of > 0 ? Math.min(value / of, 1) : value > 0 ? 1 : 0
  return (
    <li className={cn("adm-meter", share >= 1 && "is-full")}>
      <span className="adm-meter-label">{label}</span>
      <span className="adm-meter-value">
        <strong>{value}</strong>
        <small>
          / {of} {unit}
        </small>
      </span>
      <span className="adm-meter-bar" aria-hidden>
        <i style={{ width: `${share * 100}%` }} />
      </span>
    </li>
  )
}

/** The timed-read settings the operator edits before one Save. */
interface TimedDraft {
  minutes: number
  sync: SourceSync | null
  methods: ChainMethod[]
}

const draftOf = (schedule: SourceSchedule): TimedDraft => ({
  minutes: schedule.intervalMinutes,
  sync: schedule.sync,
  methods: schedule.chain.methods,
})

const methodsKey = (methods: ChainMethod[]) =>
  methods.map(({ id, enabled }) => `${id}:${enabled}`).join(",")

/** What a Save sends: only the fields the draft changed. */
function timedChanges(draft: TimedDraft, saved: TimedDraft) {
  const chain: SourceChainUpdate = {}
  if (methodsKey(draft.methods) !== methodsKey(saved.methods))
    chain.methods = draft.methods.map(({ id, enabled }) => ({ id, enabled }))
  return {
    ...(draft.minutes !== saved.minutes
      ? { intervalMinutes: draft.minutes }
      : {}),
    ...(draft.sync && !sameSync(draft.sync, saved.sync)
      ? { sync: draft.sync }
      : {}),
    ...(Object.keys(chain).length ? { chain } : {}),
  }
}

/**
 * How the timer reads a source, saved together: the interval and, for X and
 * YouTube, how much each run reads; and the method chain (ADR-0004) that
 * timed reads try from the top. A method this process cannot run is greyed
 * with the variable it needs, a source SKIP_* disables keeps its chain
 * locked, and Bilibili's one method is read-only.
 */
function TimedReadsPanel({
  schedule,
  onChange,
}: {
  schedule: SourceSchedule
  onChange: (schedule: SourceSchedule) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const saved = draftOf(schedule)
  const savedKey = JSON.stringify(saved)
  const [draft, setDraft] = useState(saved)
  const [busy, setBusy] = useState(false)
  // A saved change elsewhere, such as the timer switch, keeps the edits.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => setDraft(saved), [savedKey])
  const set = (patch: Partial<TimedDraft>) =>
    setDraft((current) => ({ ...current, ...patch }))
  const source = schedule.source
  const chain = schedule.chain
  const methods = draft.methods
  const label = (id: string) => t(`admin.chain.method.${source}.${id}`)
  const changes = timedChanges(draft, saved)
  const dirty = Object.keys(changes).length > 0
  const sync = draft.sync
  const limit = sync ? (sync.mode === "count" ? sync.count : sync.max) : 0
  const options = [...new Set([...intervals, draft.minutes])]
    .filter(
      (value) =>
        value >= schedule.minIntervalMinutes &&
        value <= schedule.maxIntervalMinutes,
    )
    .sort((a, b) => a - b)
  if (!options.includes(schedule.minIntervalMinutes))
    options.unshift(schedule.minIntervalMinutes)
  const enabledCount = methods.filter((method) => method.enabled).length
  const move = (index: number, by: number) => {
    const next = [...methods]
    const [method] = next.splice(index, 1)
    next.splice(index + by, 0, method)
    set({ methods: next })
  }
  const submit = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!dirty) return
    setBusy(true)
    const next = await act(
      () => api.updateSourceSchedule(source, changes),
      "admin.notice.scheduleSaved",
    )
    setBusy(false)
    if (next) onChange(next)
  }
  return (
    <Panel level={3} icon={CalendarClock} title={t("admin.sources.timedReads")}>
      <form className="adm-form adm-timed" onSubmit={submit}>
        <div className="adm-timed-grid">
          <Field
            label={t("admin.timer.interval")}
            hint={
              source === "bilibili"
                ? t("admin.timer.floor", {
                    floor: formatInterval(schedule.minIntervalMinutes, t),
                  })
                : undefined
            }
          >
            <select
              value={draft.minutes}
              onChange={(e) => set({ minutes: Number(e.target.value) })}
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
              <FieldGroup label={t("admin.timer.sync")}>
                <Segmented
                  label={t("admin.timer.sync")}
                  value={sync.mode}
                  onChange={(mode) =>
                    set({
                      sync:
                        mode === "count"
                          ? { mode: "count", count: limit }
                          : { mode: "incremental", max: limit },
                    })
                  }
                  options={[
                    {
                      value: "incremental",
                      label: t("admin.fetch.mode.incremental"),
                    },
                    { value: "count", label: t("admin.fetch.mode.count") },
                  ]}
                />
              </FieldGroup>
              <HelpField
                label={t(`admin.timer.syncLimit.${sync.mode}.${source}`)}
                help={
                  source === "youtube" ? (
                    <p>{t("admin.timer.syncLimitHint.youtube")}</p>
                  ) : null
                }
                hint={
                  source === "youtube"
                    ? undefined
                    : t(`admin.timer.syncLimitHint.${source}`)
                }
              >
                {(id) => (
                  <input
                    id={id}
                    type="number"
                    min={1}
                    max={200}
                    value={limit}
                    onChange={(e) => {
                      const value = Number(e.target.value)
                      set({
                        sync:
                          sync.mode === "count"
                            ? { mode: "count", count: value }
                            : { mode: "incremental", max: value },
                      })
                    }}
                    required
                  />
                )}
              </HelpField>
            </>
          ) : null}
        </div>
        <fieldset
          className="adm-timed-section adm-chain"
          disabled={schedule.skipped}
        >
          <legend>
            {t("admin.chain.title")}
            <HelpButton title={t("admin.chain.title")}>
              <p>{t("admin.chain.description")}</p>
            </HelpButton>
          </legend>
          {chain.fixed ? (
            <p className="adm-field-hint">{t("admin.chain.fixed")}</p>
          ) : null}
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
                        set({
                          methods: methods.map((entry) =>
                            entry.id === method.id
                              ? { ...entry, enabled }
                              : entry,
                          ),
                        })
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
          {source === "youtube" &&
          !methods.some((method) => method.id === "api" && method.enabled) ? (
            <p className="adm-field-hint">{t("admin.chain.youtubeAPIOff")}</p>
          ) : null}
        </fieldset>
        <div
          className={cn(
            "adm-form-actions adm-timed-actions",
            dirty && "is-dirty",
          )}
        >
          {dirty ? (
            <span className="adm-muted">{t("admin.models.unsaved")}</span>
          ) : null}
          <Button
            size="sm"
            variant="ghost"
            icon={Undo2}
            disabled={!dirty || busy}
            onClick={() => setDraft(saved)}
          >
            {t("admin.action.discard")}
          </Button>
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
 * X's API limits, folded: the gate of timeline failures that opens the X API
 * for an account and the daily cap of posts it may read, saved together,
 * with today's usage and each account's failures as meters against them.
 * The header shows the API's place in the saved chain and today's usage.
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
  const savedGate = chain.apiGate ?? 0
  const savedCap = chain.apiDailyCap ?? 1
  const [gate, setGate] = useState(savedGate)
  const [cap, setCap] = useState(savedCap)
  const [busy, setBusy] = useState(false)
  useEffect(() => setGate(savedGate), [savedGate])
  useEffect(() => setCap(savedCap), [savedCap])
  const rank = chain.methods.findIndex((method) => method.id === "api")
  const method = chain.methods[rank]
  if (!method) return null
  const failures = chain.timelineFailures || {}
  const streaks = [...new Set([...accounts, ...Object.keys(failures)])]
  const update: SourceChainUpdate = {
    ...(gate !== savedGate ? { apiGate: gate } : {}),
    ...(cap !== savedCap ? { apiDailyCap: cap } : {}),
  }
  const dirty = Object.keys(update).length > 0
  const submit = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!dirty) return
    setBusy(true)
    const next = await act(
      () => api.updateSourceSchedule(schedule.source, { chain: update }),
      "admin.notice.scheduleSaved",
    )
    setBusy(false)
    if (next) onChange(next)
  }
  return (
    <Fold
      icon={KeyRound}
      title={t("admin.sources.xapi")}
      className="adm-panel"
      help={
        <>
          <p>{t("admin.xapi.description")}</p>
          <dl className="adm-help-terms">
            <dt>{t("admin.chain.apiGate")}</dt>
            <dd>{t("admin.chain.apiGateHint")}</dd>
            <dt>{t("admin.chain.apiDailyCap")}</dt>
            <dd>{t("admin.chain.apiDailyCapHint")}</dd>
          </dl>
        </>
      }
      hint={t("admin.xapi.usageHint", {
        posts: chain.apiUsage?.posts ?? 0,
        cap: savedCap,
      })}
      controls={
        !method.available ? null : method.enabled ? (
          <Badge tone="leaf">
            {t("admin.xapi.inChain", { rank: rank + 1 })}
          </Badge>
        ) : (
          <Badge>{t("admin.xapi.offInChain")}</Badge>
        )
      }
    >
      <form className="adm-form" onSubmit={submit}>
        <fieldset className="adm-fold-fields" disabled={schedule.skipped}>
          <div className="adm-form-row is-top">
            <Field label={t("admin.chain.apiGate")}>
              <input
                type="number"
                min={0}
                max={100}
                value={gate}
                onChange={(e) => setGate(Number(e.target.value))}
                required
              />
            </Field>
            <Field label={t("admin.chain.apiDailyCap")}>
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
        </fieldset>
        <ul className="adm-meters">
          <Meter
            label={t("admin.chain.usage")}
            value={chain.apiUsage?.posts ?? 0}
            of={savedCap}
            unit={t("admin.chain.usageUnit")}
          />
          {streaks.map((account) => (
            <Meter
              key={account}
              label={`@${account}`}
              value={failures[account] ?? 0}
              of={savedGate}
              unit={t("admin.chain.streakUnit")}
            />
          ))}
        </ul>
        <div className="adm-form-actions">
          <Button
            type="submit"
            size="sm"
            icon={Save}
            busy={busy}
            disabled={!dirty || busy}
          >
            {t("admin.action.save")}
          </Button>
        </div>
      </form>
    </Fold>
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
      <FieldGroup
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
      </FieldGroup>
      {method.available ? null : (
        <p className="adm-inline-error">
          <KeyRound aria-hidden />
          {method.requires
            ? t("admin.fetch.needs", { variable: method.requires })
            : t("admin.fetch.unavailable")}
        </p>
      )}
      <FieldGroup
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
      </FieldGroup>
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
          />
        ) : null}
        <Switch
          checked={translate}
          onChange={setTranslate}
          label={t("admin.fetch.option.translate")}
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

/** A step of YouTube's progressive confirmation without settings, its
 * explanation behind a question mark beside its title. */
function Stage({
  icon: StageIcon,
  title,
  help,
  badge,
  children,
}: {
  icon: LucideIcon
  title: string
  help: ReactNode
  badge: ReactNode
  children: ReactNode
}) {
  const id = useId()
  return (
    <li className="adm-confirm-step">
      <section aria-labelledby={id}>
        <header className="adm-confirm-head">
          <span className="adm-confirm-mark">
            <StageIcon aria-hidden />
          </span>
          <div className="adm-confirm-title">
            <h4 id={id}>{title}</h4>
            <HelpButton title={title}>{help}</HelpButton>
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
    <li className="adm-confirm-step">
      <Fold
        icon={Radio}
        level={4}
        title={t("admin.push.title")}
        help={
          <>
            <p>{t("admin.push.description")}</p>
            <p>{t("admin.push.enableHint")}</p>
          </>
        }
        controls={
          <>
            {push.enabled ? (
              <Badge tone={push.verified ? "leaf" : "honey"}>
                {t(
                  push.verified
                    ? "admin.overview.pushVerified"
                    : "admin.overview.pushPending",
                )}
              </Badge>
            ) : null}
            <Switch
              compact
              checked={push.enabled}
              disabled={!push.available || busy !== ""}
              onChange={(enabled) =>
                save({ enabled, callbackUrl: callback || null })
              }
              label={t("admin.push.enable")}
            />
          </>
        }
      >
        <div className="adm-form">
          {push.available ? null : (
            <p className="adm-field-hint">{t("admin.push.unavailable")}</p>
          )}
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
      </Fold>
    </li>
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
    <li className="adm-confirm-step">
      <Fold
        icon={RadioTower}
        level={4}
        title={t("admin.liveProbe.title")}
        help={
          <>
            <p>{t("admin.liveProbe.description", { max: probe.maxVideos })}</p>
            <p>{t("admin.liveProbe.enableHint")}</p>
            <dl className="adm-help-terms">
              <dt>{t("admin.liveProbe.interval")}</dt>
              <dd>
                {t("admin.liveProbe.intervalHint", {
                  min: probe.minIntervalMinutes,
                  max: probe.maxIntervalMinutes,
                })}
              </dd>
            </dl>
          </>
        }
        controls={
          <>
            {probe.enabled && !running ? (
              <Badge tone="honey">{t("admin.liveProbe.notRunning")}</Badge>
            ) : null}
            <Switch
              compact
              checked={probe.enabled}
              disabled={!probe.available || busy !== ""}
              onChange={(enabled) => save({ enabled })}
              label={t("admin.liveProbe.enable")}
            />
          </>
        }
      >
        <form
          className="adm-form"
          onSubmit={(event) => {
            event.preventDefault()
            save({ intervalMinutes: minutes })
          }}
        >
          {probe.available ? null : (
            <p className="adm-field-hint">{t("admin.liveProbe.unavailable")}</p>
          )}
          <Field
            label={t("admin.liveProbe.interval")}
            hint={t("admin.liveProbe.intervalRange", {
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
      </Fold>
    </li>
  )
}

/**
 * YouTube's progressive confirmation: push queues a read as a video appears,
 * the live-status probe follows streams around their start, and follow-up
 * checks reread finished videos. Push and the probe each fold their settings
 * and figures under a header with their state and a quick switch. The
 * follow-ups have no setting; they ride on every YouTube read.
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
      title={
        <>
          {t("admin.confirm.title")}
          <HelpButton title={t("admin.confirm.title")}>
            <p>{t("admin.confirm.description")}</p>
          </HelpButton>
        </>
      }
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
          help={<p>{t("admin.followUp.description")}</p>}
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

function YTDLPPanel({
  status,
  onChange,
}: {
  status: YTDLPStatus | undefined
  onChange: (status: YTDLPStatus) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [busy, setBusy] = useState(false)
  const available = status?.available === true
  const update = async () => {
    setBusy(true)
    const next = await act(() => api.updateYTDLP(), "admin.notice.ytdlpUpdated")
    setBusy(false)
    if (next) onChange(next)
  }
  return (
    <Fold
      icon={Download}
      title={t("admin.ytdlp.title")}
      className="adm-panel"
      help={<p>{t("admin.ytdlp.description")}</p>}
      hint={status?.version || undefined}
      controls={
        status ? (
          <Badge tone={available ? "leaf" : "neutral"}>
            {available
              ? t("admin.ytdlp.available")
              : t("admin.ytdlp.unavailable")}
          </Badge>
        ) : null
      }
    >
      <div className="adm-form">
        <Facts
          items={[
            [
              t("admin.ytdlp.status"),
              available
                ? t("admin.ytdlp.available")
                : t("admin.ytdlp.unavailable"),
            ],
            [t("admin.ytdlp.version"), status?.version || null],
          ]}
        />
        <div className="adm-form-actions">
          <Button
            size="sm"
            icon={RotateCw}
            busy={busy}
            disabled={!available}
            onClick={update}
          >
            {t("admin.ytdlp.update")}
          </Button>
        </div>
      </div>
    </Fold>
  )
}

/**
 * One section of a source's read limits, folded: the bounds of its timed
 * reads, or of the manual fetches that X and YouTube share, one field per
 * row beside the manual fetch. Both sections edit the page's one read of the
 * settings.
 */
function LimitsPanel({
  source,
  section,
  settings,
  onSaved,
}: {
  source: "x" | "youtube"
  section: "timed" | "manual"
  settings: SyncSettings | undefined
  onSaved: (settings: SyncSettings) => void
}) {
  const { t } = useAppSettings()
  return (
    <Fold
      icon={section === "timed" ? Gauge : SlidersHorizontal}
      title={t(`admin.sources.limits.${section}`)}
      className="adm-panel"
      hint={section === "manual" ? t("admin.syncSettings.shared") : undefined}
    >
      {settings ? (
        <SyncSettingsForm
          group={source}
          section={section}
          data={settings}
          onSaved={onSaved}
          rows={section === "manual"}
        />
      ) : (
        <Loading />
      )}
    </Fold>
  )
}

const sourceIds: ScheduleSource[] = ["x", "youtube", "bilibili"]

/**
 * The Sources page: a tab per source, then the selected source's summary
 * (timer state, runs, the timer switch and Run now). Below it the timed-read
 * settings fill the main column, followed by X's API limits or YouTube's
 * progressive confirmation, the timed-read limits and yt-dlp, each folded;
 * the manual fetch and its folded limits, or Bilibili's account and
 * single-upload form, sit beside them. On a narrower screen the side column
 * follows the main one.
 */
export function SourcesView({ tab, navigate }: ViewProps) {
  const { t, locale } = useAppSettings()
  const { api, overview } = useAdmin()
  const sources = useResource(() => api.sources(), [])
  const ytdlp = useResource(() => api.ytdlp(), [])
  // One read for both limit sections: a second read of the same path would
  // supersede the first.
  const limits = useResource(() => api.syncSettings(), [])
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
  const current = schedule(selected)
  return (
    <div className="adm-view">
      <PageHeader title={t("admin.sources.title")} />
      {!data ? (
        <Loading />
      ) : (
        <>
          {timersOff ? (
            <Callout tone="honey" icon={Info}>
              <p>{t("admin.timer.schedulerOff")}</p>
            </Callout>
          ) : null}
          <div
            className="adm-tabs is-bare adm-source-tabs"
            role="tablist"
            aria-label={t("admin.sources.title")}
          >
            {sourceIds.map((source) => {
              const found = schedule(source)
              return found ? (
                <SourceTab
                  key={source}
                  schedule={found}
                  selected={selected === source}
                  onSelect={() => navigate("sources", source)}
                />
              ) : null
            })}
          </div>
          {current ? (
            <div
              role="tabpanel"
              className="adm-source-page"
              aria-label={t(`admin.step.${selected}`)}
            >
              <SourceSummary
                key={selected}
                schedule={current}
                lastSuccess={
                  selected === "bilibili"
                    ? data.bilibili.lastSuccessAt
                    : overview?.sources[selected].lastSuccessAt
                }
                chips={
                  selected === "bilibili" && blockedUntil ? (
                    <Badge tone="danger" icon={ShieldAlert}>
                      {t("admin.bilibili.blockedUntil", {
                        time: formatDateTime(blockedUntil, locale),
                      })}
                    </Badge>
                  ) : null
                }
                onChange={setSchedule}
              />
              <div className="adm-sources-layout">
                <div className="adm-source-main">
                  <TimedReadsPanel
                    key={selected}
                    schedule={current}
                    onChange={setSchedule}
                  />
                  {selected === "x" ? (
                    <XAPIPanel
                      schedule={current}
                      accounts={data.x.accounts.map((entry) => entry.account)}
                      onChange={setSchedule}
                    />
                  ) : null}
                  {selected === "youtube" ? (
                    <ConfirmationPanel
                      youtube={data.youtube}
                      onChange={(youtube) =>
                        sources.setData({ ...data, youtube })
                      }
                    />
                  ) : null}
                  {selected === "bilibili" ? null : (
                    <LimitsPanel
                      key={`${selected}-timed`}
                      source={selected}
                      section="timed"
                      settings={limits.data}
                      onSaved={limits.setData}
                    />
                  )}
                  {selected === "youtube" ? (
                    <YTDLPPanel
                      status={ytdlp.data}
                      onChange={(next) => ytdlp.setData(next)}
                    />
                  ) : null}
                </div>
                <div className="adm-source-aside">
                  {selected === "bilibili" ? (
                    <BilibiliSource
                      status={data.bilibili}
                      onAdded={() => void sources.reload()}
                    />
                  ) : (
                    <>
                      <FetchPanel
                        key={selected}
                        source={selected}
                        sources={data}
                        onOpenRuns={() => navigate("runs")}
                      />
                      <LimitsPanel
                        key={`${selected}-manual`}
                        source={selected}
                        section="manual"
                        settings={limits.data}
                        onSaved={limits.setData}
                      />
                    </>
                  )}
                </div>
              </div>
            </div>
          ) : null}
        </>
      )}
    </div>
  )
}
