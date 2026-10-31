import {
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type SubmitEvent,
} from "react"
import {
  Archive,
  Check,
  ChevronDown,
  ChevronsDownUp,
  ChevronsUpDown,
  ClipboardCopy,
  ListChecks,
  Play,
  RefreshCw,
  RotateCcw,
  Save,
  Sparkles,
  Square,
  TriangleAlert,
  Workflow,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import {
  formatDateTime,
  formatDuration,
  formatRelative,
  statusTone,
} from "@/admin/format"
import type { Run } from "@/admin/types"
import {
  Button,
  Callout,
  ConfirmButton,
  Empty,
  Field,
  Loading,
  Modal,
  PageHeader,
  Pager,
  Panel,
  StatusBadge,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import {
  explainCode,
  metricLabel,
  RunLine,
  RunMark,
  runDiagnostics,
  runOutcome,
  runTitle,
  stepIcons,
  stepResult,
  windowText,
} from "@/admin/views/run-parts"
import type { Translator } from "@/types"
import { cn } from "@/lib/utils"

/** Pick steps from the catalog and run them once; nothing is saved. Sources
 * also run on their own timers on the Sources page (ADR-0003). */
function StepRunDialog({ onClose }: { onClose: () => void }) {
  const { t } = useAppSettings()
  const { api, start, overview } = useAdmin()
  const catalog = useResource(() => api.stepCatalog(), [])
  const skipped = catalog.data?.skippedSteps || []
  const [steps, setSteps] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const submit = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    const run = await start({ kind: "workflow", steps })
    setBusy(false)
    if (run) onClose()
  }
  return (
    <Modal open wide title={t("admin.runs.stepRun")} onClose={onClose}>
      {!catalog.data ? (
        <Loading />
      ) : (
        <form className="adm-form" onSubmit={submit}>
          <p className="adm-muted">{t("admin.runs.stepRunHint")}</p>
          {(["source", "process"] as const).map((group) => (
            <fieldset key={group} className="adm-fieldset">
              <legend>{t(`admin.runs.group.${group}`)}</legend>
              <div className="adm-step-picker">
                {catalog
                  .data!.steps.filter((step) => step.group === group)
                  .map((step) => {
                    const Icon = stepIcons[step.id] || Sparkles
                    const off = skipped.includes(step.id)
                    const checked = steps.includes(step.id)
                    return (
                      <label
                        key={step.id}
                        className={cn(
                          checked && "is-checked",
                          off && "is-disabled",
                        )}
                      >
                        <input
                          type="checkbox"
                          checked={checked}
                          disabled={off}
                          onChange={(event) =>
                            setSteps((current) =>
                              event.target.checked
                                ? [...current, step.id]
                                : current.filter((id) => id !== step.id),
                            )
                          }
                        />
                        <span className="adm-step-pick-icon">
                          <Icon aria-hidden />
                        </span>
                        <span>
                          <strong>{t(`admin.step.${step.id}`)}</strong>
                          <small>
                            {off
                              ? t("admin.runs.skippedStep")
                              : t(`admin.step.${step.id}.hint`)}
                          </small>
                        </span>
                        {checked ? (
                          <Check className="adm-step-pick-check" aria-hidden />
                        ) : null}
                      </label>
                    )
                  })}
              </div>
            </fieldset>
          ))}
          <div className="adm-form-actions">
            {overview?.activeRun ? (
              <span className="adm-muted">{t("admin.error.busy")}</span>
            ) : null}
            <Button onClick={onClose}>{t("admin.action.cancel")}</Button>
            <Button
              type="submit"
              variant="primary"
              icon={Play}
              busy={busy}
              disabled={!steps.length || Boolean(overview?.activeRun)}
            >
              {t("admin.runs.runOnce", { count: steps.length })}
            </Button>
          </div>
        </form>
      )}
    </Modal>
  )
}

type Scalar = string | number | boolean
const isScalar = (value: unknown): value is Scalar =>
  ["string", "number", "boolean"].includes(typeof value)
const isRecord = (value: unknown): value is Record<string, unknown> =>
  Boolean(value) && typeof value === "object" && !Array.isArray(value)

/**
 * A step's stored counts, sorted into what the console can lay out: an error
 * code, plain numbers and words, lists of plain values, nested groups of
 * counts and per-attempt records.
 */
function splitResult(value: unknown) {
  const out = {
    error: null as string | null,
    skipped: false,
    scalars: [] as [string, Scalar][],
    lists: [] as [string, Scalar[]][],
    groups: [] as [string, Record<string, unknown>][],
    records: [] as [string, Record<string, unknown>[]][],
  }
  if (!isRecord(value)) {
    if (isScalar(value)) out.scalars.push(["value", value])
    return out
  }
  for (const [key, item] of Object.entries(value)) {
    if (item == null) continue
    // The step's own source name repeats the row's heading.
    if (key === "source" && typeof item === "string") continue
    if (key === "error" && typeof item === "string") out.error = item
    else if (key === "skipped" && item === true) out.skipped = true
    else if (isScalar(item)) out.scalars.push([key, item])
    else if (Array.isArray(item)) {
      if (item.every(isRecord)) {
        if (item.length) out.records.push([key, item])
      } else if (item.length) out.lists.push([key, item.filter(isScalar)])
    } else if (isRecord(item)) out.groups.push([key, item])
  }
  return out
}

// Counts whose non-zero values call for attention, or read as good news.
const badCounts = /fail|error|dropped|ambiguous|blocked/iu
const goodCounts = /^(success|ready|merged|videosMerged|changed)$/u

function metricTone(key: string, value: Scalar) {
  if (typeof value !== "number" || value === 0) return undefined
  if (badCounts.test(key)) return "danger"
  if (goodCounts.test(key)) return "success"
  return undefined
}

/** The counts that say what a step did, first in its one-line summary. */
const headlineCounts: Record<string, string[]> = {
  x: ["posts", "discovered", "timelineGaps"],
  youtube: ["videos", "updated", "discovered"],
  bilibili: ["videos", "discovered"],
  media: ["ready", "unchanged", "attempted"],
  schedule: ["success", "uncertain", "cached"],
  translate: ["success", "attempted"],
  reconcile: ["merged", "eventsCreated", "videosMerged"],
}

/**
 * Up to three non-zero counts for a step's summary: problems first, then the
 * step's own headline counts, then whatever else it counted.
 */
function headline(step: string, scalars: [string, Scalar][]) {
  const numbers = scalars.filter(
    (entry): entry is [string, number] =>
      typeof entry[1] === "number" && entry[1] !== 0,
  )
  const preferred = headlineCounts[step] || []
  const ordered = [
    ...numbers.filter(([key]) => badCounts.test(key)),
    ...preferred.flatMap((key) =>
      numbers.filter(([name]) => name === key && !badCounts.test(name)),
    ),
    ...numbers.filter(
      ([key]) => !badCounts.test(key) && !preferred.includes(key),
    ),
  ]
  return ordered.slice(0, 3)
}

function MetricValue({ value, t }: { value: Scalar; t: Translator }) {
  if (typeof value === "boolean")
    return <>{t(value ? "admin.metric.yes" : "admin.metric.no")}</>
  if (typeof value === "number") return <>{value.toLocaleString()}</>
  return <>{value}</>
}

/** Every plain count as a dense label/value list; zeros recede. */
function Metrics({ items }: { items: [string, unknown][] }) {
  const { t } = useAppSettings()
  const scalars = items.filter((entry): entry is [string, Scalar] =>
    isScalar(entry[1]),
  )
  if (!scalars.length) return null
  return (
    <dl className="adm-kv">
      {scalars.map(([key, value]) => {
        const tone = metricTone(key, value)
        return (
          <div
            key={key}
            className={cn(
              tone && `adm-tone-${tone}`,
              (value === 0 || value === false) && "is-zero",
            )}
          >
            <dt>{metricLabel(key, t)}</dt>
            <dd>
              <MetricValue value={value} t={t} />
            </dd>
          </div>
        )
      })}
    </dl>
  )
}

/** An error code in words, with the code itself for searching logs. */
function CodeLine({ code }: { code: string }) {
  const { t } = useAppSettings()
  return (
    <p className="adm-code-line">
      <TriangleAlert aria-hidden />
      <span>{explainCode(code, t) || t("admin.code.unknown")}</span>
      <code>{code}</code>
    </p>
  )
}

// Per-attempt records lead with who and what was asked, then the outcome.
const recordColumns = [
  "ownerType",
  "ownerId",
  "provider",
  "model",
  "attempt",
  "error",
  "stopReason",
  "inputTokens",
  "outputTokens",
]

/** Per-attempt records such as model calls, failed ones first by default. */
function Records({
  name,
  records,
}: {
  name: string
  records: Record<string, unknown>[]
}) {
  const { t } = useAppSettings()
  const failed = records.filter((record) => record.error)
  const [all, setAll] = useState(failed.length === 0)
  const shown = all ? records : failed
  const keys = new Set(
    records.flatMap((record) =>
      Object.keys(record).filter((key) => isScalar(record[key])),
    ),
  )
  const columns = [
    ...recordColumns.filter((key) => keys.has(key)),
    ...[...keys].filter((key) => !recordColumns.includes(key)),
  ].slice(0, 9)
  return (
    <details className="adm-records" open={failed.length > 0}>
      <summary>
        {metricLabel(name, t)}
        <span className="adm-muted">
          {t("admin.runs.records", {
            count: records.length,
            failed: failed.length,
          })}
        </span>
      </summary>
      {failed.length && failed.length < records.length ? (
        <button
          type="button"
          className="adm-filter-reset"
          onClick={() => setAll(!all)}
        >
          {t(all ? "admin.runs.recordsFailed" : "admin.runs.recordsAll")}
        </button>
      ) : null}
      <div className="adm-records-scroll">
        <table>
          <thead>
            <tr>
              {columns.map((key) => (
                <th key={key}>{metricLabel(key, t)}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {shown.map((record, index) => (
              <tr
                key={index}
                className={cn(Boolean(record.error) && "is-failed")}
              >
                {columns.map((key) => {
                  const value = record[key]
                  if (key === "error" && typeof value === "string")
                    return (
                      <td key={key} title={value}>
                        {explainCode(value, t) || <code>{value}</code>}
                      </td>
                    )
                  return (
                    <td key={key}>
                      {isScalar(value) ? (
                        <MetricValue value={value} t={t} />
                      ) : (
                        ""
                      )}
                    </td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </details>
  )
}

/** Everything a step counted, shown when its row is open. */
function StepDetails({ result }: { result: ReturnType<typeof splitResult> }) {
  const { t } = useAppSettings()
  return (
    <div className="adm-step-details">
      {result.error ? <CodeLine code={result.error} /> : null}
      <Metrics items={result.scalars} />
      {result.groups.map(([key, group]) => (
        <div key={key} className="adm-metric-group">
          <h4>{metricLabel(key, t)}</h4>
          <Metrics items={Object.entries(group)} />
          {Object.entries(group)
            .filter(([, item]) => isRecord(item) || Array.isArray(item))
            .map(([name, item]) => (
              <p key={name} className="adm-metric-list">
                <span>{metricLabel(name, t)}</span>
                {Array.isArray(item)
                  ? item.join(", ") || "—"
                  : Object.entries(item as Record<string, unknown>)
                      .map(
                        ([label, value]) =>
                          `${label}: ${Array.isArray(value) ? value.join(" → ") || "—" : String(value)}`,
                      )
                      .join(" · ")}
              </p>
            ))}
        </div>
      ))}
      {result.lists.map(([key, items]) => (
        <div key={key} className="adm-metric-list">
          <span>{metricLabel(key, t)}</span>
          <span className="adm-chip-row">
            {items.map((item, index) =>
              key === "errors" ? (
                <code key={index} title={explainCode(String(item), t) || ""}>
                  {String(item)}
                </code>
              ) : (
                <span key={index} className="adm-metric-chip">
                  {String(item)}
                </span>
              ),
            )}
          </span>
        </div>
      ))}
      {result.records.map(([key, records]) => (
        <Records key={key} name={key} records={records} />
      ))}
    </div>
  )
}

/**
 * One step of a run as one line: its outcome, its window and what it did in
 * a few counts. Opening it shows everything it counted.
 */
function StepRow({
  run,
  step,
  open,
  onToggle,
}: {
  run: Run
  step: string
  open: boolean
  onToggle: () => void
}) {
  const { t } = useAppSettings()
  const progress = run.progress[step] || "pending"
  const Icon = stepIcons[step] || Sparkles
  const result = splitResult(stepResult(run, step))
  const window = run.fetchWindows?.[step]
  const waiting = ["pending", "running", "queued"].includes(progress)
  const detailed =
    Boolean(result.error) ||
    result.scalars.length > 0 ||
    result.lists.length > 0 ||
    result.groups.length > 0 ||
    result.records.length > 0
  const counts = headline(step, result.scalars)
  const summary = result.error ? (
    <span className="adm-tone-danger">
      {explainCode(result.error, t) || t("admin.code.unknown")}
    </span>
  ) : result.skipped ? (
    <span className="adm-muted">{t("admin.runs.stepSkipped")}</span>
  ) : counts.length ? (
    counts.map(([key, value]) => {
      const tone = metricTone(key, value)
      return (
        <span key={key} className={cn(tone && `adm-tone-${tone}`)}>
          <b>{value.toLocaleString()}</b> {metricLabel(key, t)}
        </span>
      )
    })
  ) : waiting ? null : (
    <span className="adm-muted">{t("admin.runs.noChanges")}</span>
  )
  const head = (
    <>
      <span
        className={cn("adm-step-row-icon", `adm-tone-${statusTone(progress)}`)}
      >
        <Icon aria-hidden />
      </span>
      <span className="adm-step-row-name">
        <strong>{t(`admin.step.${step}`)}</strong>
        {window ? <small>{windowText(window, step, t)}</small> : null}
      </span>
      <span className="adm-step-row-summary">{summary}</span>
      <StatusBadge status={progress} />
      {detailed ? (
        <ChevronDown className="adm-step-row-chevron" aria-hidden />
      ) : (
        <span aria-hidden />
      )}
    </>
  )
  return (
    <li className={cn("adm-step-row", open && "is-open")}>
      {detailed ? (
        <button
          type="button"
          className="adm-step-row-head"
          aria-expanded={open}
          onClick={onToggle}
        >
          {head}
        </button>
      ) : (
        <div className="adm-step-row-head">{head}</div>
      )}
      {open && detailed ? <StepDetails result={result} /> : null}
    </li>
  )
}

/** Steps that open by themselves: those that failed or stopped short. */
function troubledSteps(run: Run) {
  return run.steps.filter((step) => {
    const progress = run.progress[step]
    const result = stepResult(run, step)
    return (
      progress === "failed" ||
      progress === "partial" ||
      (isRecord(result) && typeof result.error === "string")
    )
  })
}

/** The run's steps in order, each one line until opened. */
function StepList({ run }: { run: Run }) {
  const { t } = useAppSettings()
  const [open, setOpen] = useState(() => troubledSteps(run))
  const allOpen = open.length === run.steps.length
  return (
    <section className="adm-step-list">
      <header>
        <h3>{t("admin.runs.steps", { count: run.steps.length })}</h3>
        <button
          type="button"
          className="adm-filter-reset"
          onClick={() => setOpen(allOpen ? [] : [...run.steps])}
        >
          {allOpen ? (
            <ChevronsDownUp aria-hidden />
          ) : (
            <ChevronsUpDown aria-hidden />
          )}
          {t(allOpen ? "admin.runs.collapseAll" : "admin.runs.expandAll")}
        </button>
      </header>
      <ol>
        {run.steps.map((step) => (
          <StepRow
            key={step}
            run={run}
            step={step}
            open={open.includes(step)}
            onToggle={() =>
              setOpen((current) =>
                current.includes(step)
                  ? current.filter((id) => id !== step)
                  : [...current, step],
              )
            }
          />
        ))}
      </ol>
    </section>
  )
}

/**
 * Why a run failed or finished partly, step by step, before the details: an
 * error code in words, or how many of a step's items failed.
 */
function RunProblems({ run }: { run: Run }) {
  const { t } = useAppSettings()
  const problems = run.steps
    .filter((step) => ["failed", "partial"].includes(run.progress[step]))
    .map((step) => {
      const result = stepResult(run, step)
      const record = isRecord(result) ? result : {}
      const code =
        typeof record.error === "string"
          ? record.error
          : run.progress[step] === "failed"
            ? run.error
            : null
      const failed = typeof record.failed === "number" ? record.failed : 0
      const attempted =
        typeof record.attempted === "number" ? record.attempted : 0
      // A bound that ended the step with work left, such as the media step's
      // file cap, shows as its code beside the queued count.
      const remaining =
        typeof record.remaining === "number" ? record.remaining : 0
      const stopped = typeof record.stopped === "string" ? record.stopped : null
      return {
        step,
        code: code ?? stopped,
        text: code
          ? explainCode(code, t) || t("admin.code.unknown")
          : failed
            ? t("admin.runs.problem.someFailed", { failed, attempted })
            : remaining
              ? t("admin.runs.problem.remaining", { remaining })
              : t("admin.runs.problem.partial"),
      }
    })
  if (!problems.length && !run.error) return null
  const failed = run.status === "failed"
  return (
    <Callout
      tone={failed ? "danger" : "warning"}
      icon={TriangleAlert}
      title={t(
        failed
          ? "admin.runs.problem.failedTitle"
          : "admin.runs.problem.partialTitle",
      )}
    >
      <ul className="adm-problems">
        {problems.length ? (
          problems.map((problem) => (
            <li key={problem.step}>
              <strong>{t(`admin.step.${problem.step}`)}</strong>
              <span>{problem.text}</span>
              {problem.code ? <code>{problem.code}</code> : null}
            </li>
          ))
        ) : (
          <li>
            <span>{explainCode(run.error!, t) || t("admin.code.unknown")}</span>
            <code>{run.error}</code>
          </li>
        )}
      </ul>
      {/* The stored message is often the error code again. */}
      {run.run?.message &&
      run.run.message !== run.error &&
      !problems.some((problem) => problem.code === run.run?.message) ? (
        <p>{run.run.message}</p>
      ) : null}
    </Callout>
  )
}

/**
 * Copies the run as text for diagnosing it elsewhere. A browser that refuses
 * the clipboard, such as on a plain-HTTP address, shows the text to copy by
 * hand instead.
 */
function CopyDiagnostics({ run }: { run: Run }) {
  const { t } = useAppSettings()
  const { notify } = useAdmin()
  const [text, setText] = useState<string | null>(null)
  const copy = async () => {
    const value = runDiagnostics(run)
    try {
      await navigator.clipboard.writeText(value)
      notify("admin.notice.diagnosticsCopied")
    } catch {
      setText(value)
    }
  }
  return (
    <>
      <Button size="sm" icon={ClipboardCopy} onClick={() => void copy()}>
        {t("admin.runs.copyDiagnostics")}
      </Button>
      {text !== null ? (
        <Modal
          open
          wide
          title={t("admin.runs.copyDiagnostics")}
          onClose={() => setText(null)}
        >
          <div className="adm-form">
            <p className="adm-muted">{t("admin.runs.copyDiagnosticsHint")}</p>
            <textarea
              className="adm-diagnostics"
              readOnly
              rows={16}
              value={text}
              onFocus={(event) => event.currentTarget.select()}
            />
          </div>
        </Modal>
      ) : null}
    </>
  )
}

/** When the run was queued, how long it took and how long it waited. */
function RunTimes({ run }: { run: Run }) {
  const { t, locale } = useAppSettings()
  const parts = [
    t("admin.runs.queuedAt", { time: formatDateTime(run.createdAt, locale) }),
  ]
  if (run.startedAt)
    parts.push(
      run.finishedAt
        ? t("admin.runs.ranFor", {
            duration: formatDuration(run.startedAt, run.finishedAt),
          })
        : t("admin.runs.runningFor", {
            duration: formatDuration(run.startedAt, null),
          }),
    )
  if (run.finishedAt)
    parts.push(
      t("admin.runs.finishedAt", {
        time: formatDateTime(run.finishedAt, locale),
      }),
    )
  return <p className="adm-run-times">{parts.join(" · ")}</p>
}

function RunDetail({
  run,
  onChanged,
  withHeader,
}: {
  run: Run
  onChanged: () => void
  /** The side pane names the run; inline, the row above already does. */
  withHeader?: boolean
}) {
  const { t } = useAppSettings()
  const { api, act, refreshOverview } = useAdmin()
  const active = run.status === "queued" || run.status === "running"
  const outcome = runOutcome(run)
  const troubled = outcome === "failed" || outcome === "partial"
  const actions =
    active || troubled ? (
      <div className="adm-actions">
        {active ? (
          <ConfirmButton
            size="sm"
            icon={Square}
            onConfirm={async () => {
              if (
                await act(
                  () => api.cancelRun(run.id),
                  "admin.notice.cancelRequested",
                )
              ) {
                onChanged()
                void refreshOverview().catch(() => {})
              }
            }}
          >
            {t("admin.action.cancel")}
          </ConfirmButton>
        ) : null}
        {run.status === "failed" ? (
          <Button
            size="sm"
            icon={RotateCcw}
            onClick={async () => {
              if (
                await act(() => api.retryRun(run.id), "admin.notice.runQueued")
              ) {
                onChanged()
                void refreshOverview().catch(() => {})
              }
            }}
          >
            {t("admin.runs.retry")}
          </Button>
        ) : null}
        {troubled ? <CopyDiagnostics run={run} /> : null}
      </div>
    ) : null
  return (
    <div className="adm-run-detail">
      {withHeader ? (
        <header className="adm-run-detail-head">
          <RunMark status={outcome} />
          <div>
            <h3>{runTitle(run, t)}</h3>
            <RunTimes run={run} />
          </div>
          <StatusBadge status={outcome} />
        </header>
      ) : (
        <RunTimes run={run} />
      )}
      {actions}
      <RunProblems run={run} />
      <StepList key={run.id} run={run} />
    </div>
  )
}

// Wide enough for the history list and the open run side by side.
const splitQuery = "(min-width: 1200px)"

function useSplit() {
  return useSyncExternalStore(
    (onChange) => {
      const media = window.matchMedia(splitQuery)
      media.addEventListener("change", onChange)
      return () => media.removeEventListener("change", onChange)
    },
    () => window.matchMedia(splitQuery).matches,
    () => false,
  )
}

/** The run history; `open` is the run named in the address (#runs/<id>). */
function HistoryPanel({
  open,
  onOpen,
}: {
  open: string
  onOpen: (id: string) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, overview } = useAdmin()
  const split = useSplit()
  const [page, setPage] = useState(1)
  const activeId = overview?.activeRun?.id
  const runs = useResource(() => api.runs(page), [page, activeId])
  const latest = overview?.recentRuns?.[0]
  // A linked run that is not on this page, such as one newer runs pushed
  // off it, is read on its own and listed first; a deleted one is ignored.
  const missing = Boolean(
    open && runs.data && !runs.data.items.some((run) => run.id === open),
  )
  const linked = useResource(
    () =>
      missing
        ? api.run(open).then(
            (found) => found.run,
            () => null,
          )
        : Promise.resolve(null),
    [open, missing],
  )
  const listed = runs.data?.items || []
  // The overview poll carries fresher progress for the active run.
  const items = (
    missing && linked.data?.id === open ? [linked.data, ...listed] : listed
  ).map((run) => (run.id === activeId && overview?.activeRun) || run)
  // A run opened from elsewhere, such as the overview, scrolls into view
  // once it is listed; one chosen here stays where it was clicked.
  const clicked = useRef("")
  const scrolled = useRef("")
  const shown = items.some((run) => run.id === open)
  useEffect(() => {
    if (
      !open ||
      !shown ||
      open === clicked.current ||
      open === scrolled.current
    )
      return
    scrolled.current = open
    document.getElementById(`run-${open}`)?.scrollIntoView({ block: "start" })
  }, [open, shown])
  const choose = (id: string) => {
    clicked.current = id
    onOpen(id)
  }
  // Side by side, the newest run is open until another is chosen.
  const selected = split
    ? items.find((run) => run.id === open) || items[0]
    : items.find((run) => run.id === open)
  return (
    <Panel
      icon={Workflow}
      title={t("admin.runs.history")}
      description={t("admin.runs.historyHint")}
      actions={
        latest?.finishedAt ? (
          <span className="adm-muted">
            {t("admin.runs.lastFinished", {
              time: formatRelative(latest.finishedAt, locale),
            })}
          </span>
        ) : null
      }
    >
      {!runs.data ? (
        <Loading />
      ) : items.length ? (
        <div className={cn("adm-runs", split && "is-split")}>
          <div className="adm-runs-list">
            <div className="adm-run-list">
              {items.map((run) => {
                const isOpen = selected?.id === run.id
                return (
                  <div
                    key={run.id}
                    id={`run-${run.id}`}
                    className={cn("adm-run-item", isOpen && "is-open")}
                  >
                    <RunLine
                      run={run}
                      selected={isOpen}
                      onOpen={() => choose(!split && isOpen ? "" : run.id)}
                    />
                    {!split && isOpen ? (
                      <RunDetail
                        run={run}
                        onChanged={() => void runs.reload()}
                      />
                    ) : null}
                  </div>
                )
              })}
            </div>
            <Pager
              page={runs.data.page}
              totalPages={runs.data.totalPages}
              total={runs.data.total}
              onPage={setPage}
            />
          </div>
          {split && selected ? (
            <aside className="adm-runs-pane" aria-label={runTitle(selected, t)}>
              <RunDetail
                key={selected.id}
                run={selected}
                withHeader
                onChanged={() => void runs.reload()}
              />
            </aside>
          ) : null}
        </div>
      ) : (
        <Empty title={t("admin.overview.noRunsYet")}>
          {t("admin.overview.noRunsHint")}
        </Empty>
      )}
    </Panel>
  )
}

/** How long finished runs stay in the history before the daily cleanup. */
function RetentionPanel() {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const retention = useResource(() => api.runRetention(), [])
  const [draft, setDraft] = useState<{ succeeded: number; failed: number }>()
  const [busy, setBusy] = useState(false)
  const data = retention.data
  useEffect(() => {
    if (data)
      setDraft({ succeeded: data.succeededDays, failed: data.failedDays })
  }, [data])
  const save = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!draft) return
    setBusy(true)
    const next = await act(
      () =>
        api.updateRunRetention({
          succeededDays: draft.succeeded,
          failedDays: draft.failed,
        }),
      "admin.notice.retentionSaved",
    )
    setBusy(false)
    if (next) retention.setData(next)
  }
  const days = (value: number) =>
    value === 0
      ? t("admin.retention.forever")
      : t("admin.retention.days", { count: value })
  return (
    <Panel icon={Archive} title={t("admin.retention.title")}>
      {!data || !draft ? (
        <Loading />
      ) : (
        <form className="adm-form adm-retention" onSubmit={save}>
          <div className="adm-retention-fields">
            <Field
              label={t("admin.retention.succeeded")}
              hint={days(draft.succeeded)}
            >
              <input
                type="number"
                min={0}
                max={data.maxDays}
                step={1}
                value={draft.succeeded}
                onChange={(e) =>
                  setDraft({ ...draft, succeeded: Number(e.target.value) })
                }
                required
              />
            </Field>
            <Field
              label={t("admin.retention.failed")}
              hint={days(draft.failed)}
            >
              <input
                type="number"
                min={0}
                max={data.maxDays}
                step={1}
                value={draft.failed}
                onChange={(e) =>
                  setDraft({ ...draft, failed: Number(e.target.value) })
                }
                required
              />
            </Field>
          </div>
          <div className="adm-retention-foot">
            <p className="adm-muted">
              {!data.pruner.available
                ? t("admin.retention.unavailable")
                : data.pruner.lastPrunedAt
                  ? t(
                      data.pruner.lastFailed
                        ? "admin.retention.lastFailed"
                        : "admin.retention.last",
                      {
                        time: formatRelative(data.pruner.lastPrunedAt, locale),
                        count: data.pruner.lastDeleted ?? 0,
                      },
                    )
                  : t("admin.retention.pending")}
            </p>
            <Button
              type="submit"
              size="sm"
              icon={Save}
              busy={busy}
              disabled={
                busy ||
                (draft.succeeded === data.succeededDays &&
                  draft.failed === data.failedDays)
              }
            >
              {t("admin.action.save")}
            </Button>
          </div>
        </form>
      )}
    </Panel>
  )
}

export function RunsView({ tab, navigate }: ViewProps) {
  const { t } = useAppSettings()
  const { start, overview } = useAdmin()
  const [picking, setPicking] = useState(false)
  const busy = Boolean(overview?.activeRun)
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.runs.title")}
        actions={
          <>
            <Button icon={ListChecks} onClick={() => setPicking(true)}>
              {t("admin.runs.stepRun")}
            </Button>
            <Button
              icon={Sparkles}
              disabled={busy}
              onClick={() => start({ kind: "scan" })}
            >
              {t("admin.action.scan")}
            </Button>
            <Button
              variant="primary"
              icon={RefreshCw}
              disabled={busy}
              onClick={() => start({ kind: "sync" })}
            >
              {t("admin.action.syncAll")}
            </Button>
          </>
        }
      />
      <HistoryPanel open={tab} onOpen={(id) => navigate("runs", id)} />
      <RetentionPanel />
      {picking ? <StepRunDialog onClose={() => setPicking(false)} /> : null}
    </div>
  )
}
