import { useEffect, useState, type SubmitEvent } from "react"
import {
  ArrowDown,
  ArrowUp,
  CalendarClock,
  KeyRound,
  Save,
  Undo2,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { formatInterval } from "@/admin/format"
import type {
  ChainMethod,
  SourceChainUpdate,
  SourceSchedule,
  SourceSync,
} from "@/admin/types"
import {
  Badge,
  Button,
  Field,
  FieldGroup,
  Panel,
  Segmented,
  Switch,
} from "@/admin/ui"
import { useAdmin } from "@/admin/use-admin-data"
import { cn } from "@/lib/utils"
import { Fold } from "@/admin/views/source-parts"

const intervals = [15, 30, 60, 120, 180, 360, 720, 1440, 2880, 10080]

const sameSync = (a: SourceSync | null, b: SourceSync | null) =>
  JSON.stringify(a) === JSON.stringify(b)

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
 * YouTube, how much each run reads; and the method chain that
 * timed reads try from the top. A method this process cannot run is greyed
 * with the variable it needs, a source SKIP_* disables keeps its chain
 * locked, and a timer with one fixed method shows it read-only.
 */
export function TimedReadsPanel({
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
              source === "shop"
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
              {/* Related accounts read incrementally only. */}
              {source === "x_related" ? null : (
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
              )}
              <Field
                label={t(`admin.timer.syncLimit.${sync.mode}.${source}`)}
                hint={t("admin.timer.syncLimitHint")}
              >
                <input
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
              </Field>
            </>
          ) : null}
        </div>
        <fieldset
          className="adm-timed-section adm-chain"
          disabled={schedule.skipped}
        >
          <legend>{t("admin.chain.title")}</legend>
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
export function XAPIPanel({
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
