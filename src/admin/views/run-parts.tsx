import { AtSign, ImageDown, Sparkles, Youtube } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import {
  formatDateTime,
  formatDuration,
  formatRelative,
  statusTone,
} from "@/admin/format"
import type { Run } from "@/admin/types"
import { StatusBadge, Spinner } from "@/admin/ui"
import { cn } from "@/lib/utils"

export const stepIcons = {
  x: AtSign,
  youtube: Youtube,
  media: ImageDown,
  schedule: Sparkles,
}

/** One chip per step, in execution order, coloured by its outcome. */
export function RunSteps({ run }: { run: Run }) {
  const { t } = useAppSettings()
  return (
    <ol className="adm-steps">
      {run.steps.map((step) => {
        const Icon = stepIcons[step] || Sparkles
        const progress = run.progress[step] || "pending"
        return (
          <li
            key={step}
            className={cn(`adm-tone-${statusTone(progress)}`)}
            title={`${t(`admin.step.${step}`)} · ${t(`admin.status.${progress}`)}`}
          >
            {progress === "running" ? <Spinner /> : <Icon aria-hidden />}
            <span>{t(`admin.step.${step}`)}</span>
            <small className="adm-visually-hidden">
              {t(`admin.status.${progress}`)}
            </small>
          </li>
        )
      })}
    </ol>
  )
}

export function runTitle(run: Run, t) {
  if (run.workflowName) return run.workflowName
  if (run.kind === "workflow" && run.fetchWindows) {
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

const triggers = ["admin", "scheduler", "websub", "mcp"]

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
  const content = (
    <>
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
      <RunSteps run={run} />
      <StatusBadge status={runOutcome(run)} />
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
