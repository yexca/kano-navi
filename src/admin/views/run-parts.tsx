import type { ComponentType, SVGProps } from "react"
import {
  AtSign,
  Check,
  CircleDashed,
  Combine,
  ImageDown,
  Languages,
  RadioTower,
  Sparkles,
  Tv,
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
import type { Run } from "@/admin/types"
import type { Translator } from "@/types"
import { StatusBadge, Spinner } from "@/admin/ui"
import { cn } from "@/lib/utils"
import { YouTubeLogo } from "@/site/links"

// lucide-react 1.x has no brand icons. The board's logo takes lucide's
// default 24px size so unstyled icon slots render it the same way.
function YouTubeIcon(props: SVGProps<SVGSVGElement>) {
  return <YouTubeLogo width={24} height={24} {...props} />
}

/** Lucide icons and the YouTube logo, used as `<Icon aria-hidden />`. */
type StepIcon = ComponentType<{ className?: string; "aria-hidden"?: boolean }>

export const stepIcons: Record<string, StepIcon> = {
  x: AtSign,
  youtube: YouTubeIcon,
  bilibili: Tv,
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
  if (
    run.kind === "workflow" &&
    run.fetchWindows &&
    run.steps.every((step) => fetchSteps.includes(step))
  ) {
    const sources = Object.keys(run.fetchWindows)
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
}: {
  run: Run
  onOpen?: () => void
  selected?: boolean
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
