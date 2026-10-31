import type { ComponentType, SVGProps } from "react"
import {
  Check,
  CircleDashed,
  Combine,
  ImageDown,
  Languages,
  RadioTower,
  Sparkles,
  TriangleAlert,
  X,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import {
  formatDateTime,
  formatDuration,
  formatRelative,
  statusTone,
} from "@/admin/format"
import type { Run, RunWindow } from "@/admin/types"
import type { Translator } from "@/types"
import { StatusBadge, Spinner } from "@/admin/ui"
import { cn } from "@/lib/utils"
import { BilibiliLogo, XLogo, YouTubeLogo } from "@/site/links"

// lucide-react 1.x has no brand icons. The board's logos take lucide's
// default 24px size so unstyled icon slots render them the same way.
function XIcon(props: SVGProps<SVGSVGElement>) {
  return <XLogo width={24} height={24} {...props} />
}

function YouTubeIcon(props: SVGProps<SVGSVGElement>) {
  return <YouTubeLogo width={24} height={24} {...props} />
}

export function BilibiliIcon(props: SVGProps<SVGSVGElement>) {
  return <BilibiliLogo width={24} height={24} {...props} />
}

/** Lucide icons and the platform logos, used as `<Icon aria-hidden />`. */
type StepIcon = ComponentType<{ className?: string; "aria-hidden"?: boolean }>

export const stepIcons: Record<string, StepIcon> = {
  x: XIcon,
  youtube: YouTubeIcon,
  bilibili: BilibiliIcon,
  media: ImageDown,
  schedule: Sparkles,
  translate: Languages,
  reconcile: Combine,
  live: RadioTower,
}

/**
 * One icon per step, in execution order, coloured by its outcome. The
 * compact form drops the names and keeps them for tooltips and readers.
 */
export function RunSteps({ run, compact }: { run: Run; compact?: boolean }) {
  const { t } = useAppSettings()
  return (
    <ol className={cn("adm-steps", compact && "is-compact")}>
      {run.steps.map((step) => {
        const Icon = stepIcons[step] || Sparkles
        const progress = run.progress[step] || "pending"
        const name = t(`admin.step.${step}`)
        return (
          <li
            key={step}
            className={cn(`adm-tone-${statusTone(progress)}`)}
            title={`${name} · ${t(`admin.status.${progress}`)}`}
          >
            {progress === "running" ? <Spinner /> : <Icon aria-hidden />}
            <span className={compact ? "adm-visually-hidden" : undefined}>
              {name}
            </span>
            <small className="adm-visually-hidden">
              {t(`admin.status.${progress}`)}
            </small>
          </li>
        )
      })}
    </ol>
  )
}

/** The outcome as a round icon, for scanning a list of runs. */
export function RunMark({ status }: { status: string }) {
  const tone = statusTone(status)
  const Icon =
    tone === "success"
      ? Check
      : tone === "danger"
        ? X
        : tone === "warning"
          ? TriangleAlert
          : CircleDashed
  return (
    <span className={cn("adm-run-mark", `adm-tone-${tone}`)} aria-hidden>
      {tone === "running" ? <Spinner /> : <Icon />}
    </span>
  )
}

/**
 * Plain words for an error code stored on a run, a step or an attempt, or
 * null when the console has none; the code itself is always shown too.
 */
export function explainCode(code: string, t: Translator) {
  const key = `admin.code.${code}`
  const text = t(key)
  return text === key ? null : text
}

/** A count's label: a known name, or the stored key in words. */
export function metricLabel(key: string, t: Translator) {
  const name = `admin.metric.${key}`
  const text = t(name)
  if (text !== name) return text
  const words = key
    .replace(/([a-z0-9])([A-Z])/gu, "$1 $2")
    .replace(/[_-]+/gu, " ")
    .toLowerCase()
  return words.charAt(0).toUpperCase() + words.slice(1)
}

/** Steps a run may hold and still read as a fetch of its sources. */
const fetchSteps = ["x", "youtube", "bilibili", "media", "reconcile"]

/** The sources a run fetched through a named method. */
function fetchedSources(run: Run) {
  return Object.entries(run.fetchWindows || {})
    .filter(([, window]) => window.method)
    .map(([source]) => source)
}

/** A step's stored result: the finished run's counts or the history row's. */
export function stepResult(run: Run, step: string): unknown {
  const results = run.result?.results || run.run?.counts || {}
  return results[step === "schedule" ? "schedules" : step] ?? results[step]
}

/**
 * A run as plain text for diagnosing it elsewhere, such as with a coding
 * agent: a summary with each step's outcome and error code, then the whole
 * stored run. It keeps codes and English labels rather than the console's
 * words so the text matches the source.
 */
export function runDiagnostics(run: Run) {
  const lines = [
    `Kano Navi run ${run.id}`,
    [
      `Kind: ${run.kind}`,
      `outcome: ${runOutcome(run)}`,
      `triggered by: ${run.triggeredBy}`,
      ...(run.sourceSchedule ? [`source: ${run.sourceSchedule}`] : []),
    ].join("; "),
    `Queued ${run.createdAt}; started ${run.startedAt ?? "-"}; finished ${run.finishedAt ?? "-"}`,
  ]
  if (run.error) lines.push(`Error: ${run.error}`)
  if (run.run?.message && run.run.message !== run.error)
    lines.push(`Message: ${run.run.message}`)
  lines.push("Steps:")
  for (const step of run.steps) {
    const result = stepResult(run, step) as
      { error?: unknown; errors?: unknown } | undefined
    const codes = [
      result?.error,
      ...(Array.isArray(result?.errors) ? result.errors : []),
    ].filter((code): code is string => typeof code === "string")
    const suffix = codes.length ? ` (${[...new Set(codes)].join(", ")})` : ""
    lines.push(`- ${step}: ${run.progress[step] || "pending"}${suffix}`)
  }
  lines.push("", "Stored run:", "```json", JSON.stringify(run, null, 2), "```")
  return lines.join("\n")
}

/**
 * A run's source window in words: the method, the mode and its count, days
 * or dates.
 */
export function windowText(window: RunWindow, step: string, t: Translator) {
  const parts = []
  if (window.method)
    parts.push(t(`admin.fetch.method.${step}.${window.method}`))
  parts.push(t(`admin.fetch.mode.${window.mode}`))
  if (window.days)
    parts.push(t("admin.fetch.daysCount", { count: window.days }))
  if (window.max) parts.push(t("admin.fetch.maxCount", { count: window.max }))
  if (window.count) {
    const key =
      step === "youtube" ? "admin.fetch.videosCount" : "admin.fetch.postsCount"
    parts.push(t(key, { count: window.count }))
  }
  if (window.startDate) parts.push(`${window.startDate} – ${window.endDate}`)
  if (window.refresh) parts.push(t("admin.fetch.option.refresh"))
  return parts.join(" · ")
}

export function runTitle(run: Run, t: Translator) {
  if (run.sourceSchedule) {
    const source = t(`admin.step.${run.sourceSchedule}`)
    return run.triggeredBy === "source-schedule"
      ? t("admin.run.sourceTitle", { source })
      : t("admin.run.sourceRunNow", { source })
  }
  // A stored run of a saved workflow (see ADR-0003).
  if (run.workflowId)
    return t("admin.run.retiredWorkflow", { id: run.workflowId })
  // A manual fetch names its method, and may go on to extract and translate;
  // a run frozen before that is a fetch when it holds only source steps.
  const fetched = fetchedSources(run)
  if (
    run.kind === "workflow" &&
    run.fetchWindows &&
    (fetched.length > 0 || run.steps.every((step) => fetchSteps.includes(step)))
  ) {
    const sources = fetched.length ? fetched : Object.keys(run.fetchWindows)
    return t("admin.run.fetchTitle", {
      sources: sources.map((source) => t(`admin.step.${source}`)).join(" + "),
    })
  }
  return t(`admin.run.kind.${run.kind}`)
}

export function runOutcome(run: Run) {
  if (run.status !== "completed") return run.status
  return run.result?.status === "partial" ? "partial" : "completed"
}

const triggers = [
  "admin",
  "scheduler",
  "source-schedule",
  "websub",
  "live-probe",
  "mcp",
]

export function RunLine({
  run,
  onOpen,
  selected,
  href,
  onFollow,
}: {
  run: Run
  onOpen?: () => void
  selected?: boolean
  /** A link to the run elsewhere; a plain click calls onFollow instead. */
  href?: string
  onFollow?: () => void
}) {
  const { t, locale } = useAppSettings()
  const outcome = runOutcome(run)
  const content = (
    <>
      <RunMark status={outcome} />
      <span className="adm-run-line-main">
        <strong>{runTitle(run, t)}</strong>
        <small>
          {t(
            `admin.run.by.${triggers.includes(run.triggeredBy) ? run.triggeredBy : "other"}`,
            {
              source: run.triggeredBy,
            },
          )}
          {" · "}
          <time
            dateTime={run.createdAt}
            title={formatDateTime(run.createdAt, locale)}
          >
            {formatRelative(run.createdAt, locale)}
          </time>
          {run.startedAt
            ? ` · ${formatDuration(run.startedAt, run.finishedAt)}`
            : ""}
        </small>
      </span>
      <RunSteps run={run} compact />
      <StatusBadge status={outcome} />
    </>
  )
  if (href)
    return (
      <a
        className="adm-run-line"
        href={href}
        onClick={(event) => {
          // Modified clicks keep the browser's own new-tab behaviour.
          if (
            !onFollow ||
            event.button !== 0 ||
            event.metaKey ||
            event.ctrlKey ||
            event.shiftKey ||
            event.altKey
          )
            return
          event.preventDefault()
          onFollow()
        }}
      >
        {content}
      </a>
    )
  return onOpen ? (
    <button
      type="button"
      className={cn("adm-run-line", selected && "is-selected")}
      onClick={onOpen}
      aria-expanded={selected}
    >
      {content}
    </button>
  ) : (
    <div className="adm-run-line">{content}</div>
  )
}
