import { useState, type SubmitEvent } from "react"
import {
  Check,
  ListChecks,
  Play,
  RefreshCw,
  RotateCcw,
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
  Facts,
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
  stepIcons,
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

function stepResult(run: Run, step: string): unknown {
  const results = run.result?.results || run.run?.counts || {}
  return results[step === "schedule" ? "schedules" : step] ?? results[step]
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
    // The step's own source name repeats the card's heading.
    if (key === "source" && typeof item === "string") continue
    if (key === "error" && typeof item === "string") out.error = item
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

function MetricValue({ value, t }: { value: Scalar; t: Translator }) {
  if (typeof value === "boolean")
    return <>{t(value ? "admin.metric.yes" : "admin.metric.no")}</>
  if (typeof value === "number") return <>{value.toLocaleString()}</>
  return <>{value}</>
}

function Metrics({ items }: { items: [string, unknown][] }) {
  const { t } = useAppSettings()
  const scalars = items.filter((entry): entry is [string, Scalar] =>
    isScalar(entry[1]),
  )
  if (!scalars.length) return null
  return (
    <dl className="adm-metrics">
      {scalars.map(([key, value]) => {
        const tone = metricTone(key, value)
        return (
          <div key={key} className={tone ? `adm-tone-${tone}` : undefined}>
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

function WindowLine({ run, step }: { run: Run; step: string }) {
  const { t } = useAppSettings()
  const window = run.fetchWindows?.[step]
  if (!window) return null
  return (
    <small className="adm-muted">
      {t(`admin.fetch.mode.${window.mode}`)}
      {window.days
        ? ` · ${t("admin.fetch.daysCount", { count: window.days })}`
        : ""}
      {window.max
        ? ` · ${t("admin.fetch.maxCount", { count: window.max })}`
        : ""}
      {window.count
        ? ` · ${t(step === "youtube" ? "admin.fetch.videosCount" : "admin.fetch.postsCount", { count: window.count })}`
        : ""}
      {window.startDate ? ` · ${window.startDate} – ${window.endDate}` : ""}
    </small>
  )
}

/** One step of a run: its outcome, error in words and what it counted. */
function StepCard({ run, step }: { run: Run; step: string }) {
  const { t } = useAppSettings()
  const progress = run.progress[step] || "pending"
  const Icon = stepIcons[step] || Sparkles
  const result = splitResult(stepResult(run, step))
  const empty =
    !result.error &&
    !result.scalars.length &&
    !result.lists.length &&
    !result.groups.length &&
    !result.records.length
  return (
    <li className={cn("adm-step-card", `adm-tone-${statusTone(progress)}`)}>
      <header>
        <span className="adm-step-card-icon">
          <Icon aria-hidden />
        </span>
        <strong>{t(`admin.step.${step}`)}</strong>
        <StatusBadge status={progress} />
        <WindowLine run={run} step={step} />
      </header>
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
          {key === "errors" ? (
            <span className="adm-chip-row">
              {items.map((item, index) => (
                <code key={index} title={explainCode(String(item), t) || ""}>
                  {String(item)}
                </code>
              ))}
            </span>
          ) : (
            <span className="adm-chip-row">
              {items.map((item, index) => (
                <span key={index} className="adm-metric-chip">
                  {String(item)}
                </span>
              ))}
            </span>
          )}
        </div>
      ))}
      {result.records.map(([key, records]) => (
        <Records key={key} name={key} records={records} />
      ))}
      {empty && !["pending", "running", "queued"].includes(progress) ? (
        <p className="adm-muted">{t("admin.runs.noCounts")}</p>
      ) : null}
    </li>
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

function RunDetail({ run, onChanged }: { run: Run; onChanged: () => void }) {
  const { t, locale } = useAppSettings()
  const { api, act, refreshOverview } = useAdmin()
  const active = run.status === "queued" || run.status === "running"
  return (
    <div className="adm-run-detail">
      <RunProblems run={run} />
      <Facts
        items={[
          [t("admin.runs.created"), formatDateTime(run.createdAt, locale)],
          [
            t("admin.runs.started"),
            formatDateTime(run.startedAt, locale) || null,
          ],
          [
            t("admin.runs.finished"),
            formatDateTime(run.finishedAt, locale) || null,
          ],
          [
            t("admin.runs.duration"),
            run.startedAt
              ? formatDuration(run.startedAt, run.finishedAt)
              : null,
          ],
        ]}
      />
      <ul className="adm-step-cards">
        {run.steps.map((step) => (
          <StepCard key={step} run={run} step={step} />
        ))}
      </ul>
      {active || run.status === "failed" ? (
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
                  await act(
                    () => api.retryRun(run.id),
                    "admin.notice.runQueued",
                  )
                ) {
                  onChanged()
                  void refreshOverview().catch(() => {})
                }
              }}
            >
              {t("admin.runs.retry")}
            </Button>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

function HistoryPanel() {
  const { t, locale } = useAppSettings()
  const { api, overview } = useAdmin()
  const [page, setPage] = useState(1)
  const [open, setOpen] = useState("")
  const activeId = overview?.activeRun?.id
  const runs = useResource(() => api.runs(page), [page, activeId])
  const latest = overview?.recentRuns?.[0]
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
      ) : runs.data.items.length ? (
        <>
          <div className="adm-run-list">
            {runs.data.items.map((listed) => {
              // The overview poll carries fresher progress for the active run.
              const run =
                (listed.id === activeId && overview?.activeRun) || listed
              return (
                <div
                  key={run.id}
                  className={cn("adm-run-item", open === run.id && "is-open")}
                >
                  <RunLine
                    run={run}
                    selected={open === run.id}
                    onOpen={() => setOpen(open === run.id ? "" : run.id)}
                  />
                  {open === run.id ? (
                    <RunDetail run={run} onChanged={() => void runs.reload()} />
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
        </>
      ) : (
        <Empty title={t("admin.overview.noRunsYet")}>
          {t("admin.overview.noRunsHint")}
        </Empty>
      )}
    </Panel>
  )
}

export function RunsView(_: ViewProps) {
  const { t } = useAppSettings()
  const { start, overview } = useAdmin()
  const [picking, setPicking] = useState(false)
  const busy = Boolean(overview?.activeRun)
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.runs.title")}
        description={t("admin.runs.description")}
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
      <HistoryPanel />
      {picking ? <StepRunDialog onClose={() => setPicking(false)} /> : null}
    </div>
  )
}
