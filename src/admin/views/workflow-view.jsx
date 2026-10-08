import { useEffect, useMemo, useState } from "react"
import {
  ArrowRight,
  AtSign,
  Bot,
  CalendarClock,
  Check,
  CircleAlert,
  CircleCheck,
  CircleDashed,
  CircleMinus,
  CircleX,
  Clock3,
  History,
  ImageDown,
  LoaderCircle,
  LockKeyhole,
  Pencil,
  Play,
  Plus,
  Sparkles,
  Trash2,
  TriangleAlert,
  Workflow,
  Youtube,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import {
  SourceLookbackEditor,
  SourceLookbackFields,
} from "@/admin/components/source-fetch-card"
import { adminMessage, jsonBody, request } from "@/admin/api"
import {
  formatDateTime,
  formatDuration,
  formatInterval,
  formatRelative,
  statusLabel,
  statusTone,
} from "@/admin/format"
import { describeTarget } from "@/admin/llm"
import {
  Btn,
  Card,
  CardHeader,
  Dialog,
  EmptyState,
  Field,
  IconBtn,
  Metric,
  PageHeader,
  StatusPill,
  Switch,
  Tag,
} from "@/admin/components/primitives"
import { cn } from "@/lib/utils"

export const stepMeta = {
  x: { icon: AtSign, tone: "ink" },
  youtube: { icon: Youtube, tone: "berry" },
  media: { icon: ImageDown, tone: "sky" },
  schedule: { icon: Sparkles, tone: "lavender" },
}

const intervalOptions = [15, 30, 60, 120, 180, 360, 720, 1440]
const selectedWorkflowKey = "kano-admin-workflow"

function readSelectedWorkflow() {
  try {
    return window.localStorage.getItem(selectedWorkflowKey) || ""
  } catch {
    return ""
  }
}

function rememberSelectedWorkflow(id) {
  try {
    window.localStorage.setItem(selectedWorkflowKey, id)
  } catch {
    // Remembering the tab is a convenience only.
  }
}

function usableTargets(config, route) {
  const targets = config?.routeTargets?.[route] || []
  return targets.filter(
    (target) =>
      !describeTarget(target, config?.providers || [], config?.models || [])
        .issue,
  )
}

/** Readiness warnings for a step, derived from saved configuration. */
function stepWarnings(stepId, { config, skipped }) {
  const warnings = []
  if (skipped.includes(stepId)) warnings.push("admin.workflow.warn.envSkipped")
  if (stepId === "schedule" && config) {
    const boardOn =
      config.scheduleKeywordEnabled && config.scheduleVisionEnabled
    const messageOn = config.scheduleMessageEnabled
    if (!boardOn && !messageOn)
      warnings.push("admin.workflow.warn.detectionOff")
    else {
      const ready =
        (boardOn && usableTargets(config, "schedule_board").length) ||
        (messageOn && usableTargets(config, "schedule_message").length)
      if (!ready) warnings.push("admin.workflow.warn.noModel")
    }
  }
  return warnings
}

const progressIcons = {
  pending: CircleDashed,
  running: LoaderCircle,
  completed: CircleCheck,
  partial: CircleAlert,
  failed: CircleX,
  skipped: CircleMinus,
  cancelled: CircleMinus,
}

function jobTitle(job, t) {
  if (job.kind === "workflow")
    return job.workflowName || t("admin.workflow.kind.workflow")
  if (job.kind === "scan") return t("admin.workflow.kind.scan")
  return t("admin.workflow.kind.sync")
}

function triggerLabel(triggeredBy, t) {
  const keys = {
    admin: "admin.trigger.admin",
    scheduler: "admin.trigger.scheduler",
    mcp: "admin.trigger.mcp",
    cli: "admin.trigger.cli",
  }
  return t(keys[triggeredBy] || "admin.trigger.other")
}

function JobProgress({ job, steps, t, locale }) {
  if (!job) {
    return (
      <EmptyState icon={Workflow} title={t("admin.workflow.noJob")}>
        {t("admin.workflow.noJobHint")}
      </EmptyState>
    )
  }
  const progress =
    job.progress ||
    (job.kind === "scan"
      ? { schedule: job.status === "completed" ? "completed" : job.status }
      : null)
  const jobSteps = job.steps || (job.kind === "scan" ? ["schedule"] : null)
  const active = job.status === "running" || job.status === "queued"
  return (
    <div className="adm-job">
      <div className="adm-job-head">
        <div>
          <strong>{jobTitle(job, t)}</strong>
          <small>
            {triggerLabel(job.triggeredBy, t)} ·{" "}
            {formatDateTime(job.startedAt || job.createdAt, locale)} ·{" "}
            {formatDuration(job.startedAt || job.createdAt, job.finishedAt)}
          </small>
        </div>
        <StatusPill
          status={
            job.status === "completed" && job.result?.status
              ? job.result.status
              : job.status
          }
          t={t}
        />
      </div>
      {jobSteps ? (
        <ol className="adm-pipeline" aria-label={t("admin.workflow.progress")}>
          {jobSteps.map((stepId) => {
            const status =
              progress?.[stepId] || (active ? "pending" : "skipped")
            const Icon = progressIcons[status] || CircleDashed
            const meta = stepMeta[stepId]
            const StepIcon = meta?.icon || Workflow
            return (
              <li
                key={stepId}
                className={cn("adm-pipeline-step", `is-${statusTone(status)}`)}
              >
                <span className="adm-pipeline-icon" aria-hidden="true">
                  <StepIcon />
                </span>
                <span className="adm-pipeline-copy">
                  <strong>{t(`admin.step.${stepId}`)}</strong>
                  <small>
                    <Icon
                      className={status === "running" ? "adm-spin" : undefined}
                      aria-hidden="true"
                    />
                    {statusLabel(status, t)}
                  </small>
                </span>
              </li>
            )
          })}
        </ol>
      ) : (
        <p className="adm-muted">
          {t("admin.workflow.allSteps", { count: steps.length })}
        </p>
      )}
      {job.error ? <p className="adm-inline-error">{job.error}</p> : null}
    </div>
  )
}

function runMessage(run, t) {
  const message = String(run?.message || "").trim()
  const known = {
    同步完成: "admin.run.syncComplete",
    "部分数据源不可用或存在警告，保留已有快照": "admin.run.syncPartial",
    "同步异常终止，保留已有快照": "admin.run.syncFailed",
    自动扫描完成: "admin.run.scanComplete",
    "自动扫描完成，但部分候选失败": "admin.run.scanPartial",
    "自动扫描异常终止，保留已有快照": "admin.run.scanFailed",
  }
  if (known[message]) return t(known[message])
  if (!message || /[぀-ヿ㐀-鿿]/u.test(message))
    return t(
      run?.source === "schedule"
        ? "admin.run.scanRecorded"
        : "admin.run.syncRecorded",
    )
  return message
}

function runCounts(run, t) {
  const counts = run?.counts || {}
  const parts = []
  if (counts.x && !counts.x.error && !counts.x.skipped)
    parts.push(t("admin.run.countX", { count: counts.x.count ?? 0 }))
  if (counts.youtube && !counts.youtube.error && !counts.youtube.skipped)
    parts.push(
      t("admin.run.countYoutube", { count: counts.youtube.count ?? 0 }),
    )
  if (counts.media && !counts.media.skipped)
    parts.push(t("admin.run.countMedia", { count: counts.media.ready ?? 0 }))
  const schedules = run?.source === "schedule" ? counts : counts.schedules
  if (schedules && !schedules.skipped && schedules.attempted != null)
    parts.push(
      t("admin.run.countSchedules", { count: schedules.attempted ?? 0 }),
    )
  return parts
}

function RunHistory({ runs, t, locale }) {
  if (!runs.length) {
    return (
      <EmptyState icon={History} title={t("admin.run.empty")}>
        {t("admin.run.emptyHint")}
      </EmptyState>
    )
  }
  return (
    <ol className="adm-timeline">
      {runs.map((run) => {
        const counts = runCounts(run, t)
        return (
          <li key={run.id} className={`is-${statusTone(run.status)}`}>
            <span className="adm-timeline-dot" aria-hidden="true" />
            <div className="adm-timeline-body">
              <div className="adm-timeline-row">
                <strong>{runMessage(run, t)}</strong>
                <time dateTime={run.finishedAt || run.startedAt}>
                  {formatRelative(run.finishedAt || run.startedAt, locale)}
                </time>
              </div>
              <small>
                {t(
                  run.source === "workflow"
                    ? "admin.workflow.kind.workflow"
                    : run.source === "schedule"
                      ? "admin.workflow.kind.scan"
                      : "admin.workflow.kind.sync",
                )}{" "}
                · {triggerLabel(run.triggeredBy, t)}
                {counts.length ? ` · ${counts.join(" · ")}` : ""}
              </small>
            </div>
          </li>
        )
      })}
    </ol>
  )
}

function WorkflowDialog({ steps, onClose, onCreate, t }) {
  const [name, setName] = useState("")
  const [selected, setSelected] = useState(steps.map((step) => step.id))
  const [scheduleEnabled, setScheduleEnabled] = useState(false)
  const [intervalMinutes, setIntervalMinutes] = useState(60)
  const [sourceLookbackDays, setSourceLookbackDays] = useState({
    x: 7,
    youtube: 14,
  })
  const [busy, setBusy] = useState(false)
  const submit = async (event) => {
    event.preventDefault()
    if (!selected.length) return
    setBusy(true)
    try {
      await onCreate({
        name,
        steps: selected,
        scheduleEnabled,
        intervalMinutes,
        sourceLookbackDays: Object.fromEntries(
          Object.entries(sourceLookbackDays).map(([source, days]) => [
            source,
            Number(days),
          ]),
        ),
      })
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog
      title={t("admin.workflow.newTitle")}
      description={t("admin.workflow.newDescription")}
      onClose={onClose}
      closeLabel={t("admin.action.close")}
    >
      <form className="adm-form" onSubmit={submit}>
        <Field label={t("admin.workflow.name")} wide>
          <input
            value={name}
            onChange={(event) => setName(event.target.value)}
            maxLength={80}
            required
            data-autofocus
            placeholder={t("admin.workflow.namePlaceholder")}
          />
        </Field>
        <fieldset className="adm-fieldset is-wide">
          <legend>{t("admin.workflow.modules")}</legend>
          <div className="adm-check-grid">
            {steps.map((step) => {
              const Icon = stepMeta[step.id]?.icon || Workflow
              const checked = selected.includes(step.id)
              return (
                <label
                  key={step.id}
                  className={cn("adm-check-card", checked && "is-checked")}
                >
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() =>
                      setSelected((current) =>
                        checked
                          ? current.filter((id) => id !== step.id)
                          : [...current, step.id],
                      )
                    }
                  />
                  <Icon aria-hidden="true" />
                  <span>{t(`admin.step.${step.id}`)}</span>
                </label>
              )
            })}
          </div>
        </fieldset>
        <div className="is-wide">
          <SourceLookbackFields
            value={sourceLookbackDays}
            sources={selected}
            onChange={setSourceLookbackDays}
          />
        </div>
        <div className="adm-inline-row is-wide">
          <Switch
            checked={scheduleEnabled}
            onChange={setScheduleEnabled}
            label={t("admin.workflow.schedule")}
          />
          <span>{t("admin.workflow.schedule")}</span>
          <select
            value={intervalMinutes}
            onChange={(event) => setIntervalMinutes(Number(event.target.value))}
            disabled={!scheduleEnabled}
            aria-label={t("admin.workflow.interval")}
          >
            {intervalOptions.map((minutes) => (
              <option key={minutes} value={minutes}>
                {formatInterval(minutes, t)}
              </option>
            ))}
          </select>
        </div>
        <div className="adm-form-actions is-wide">
          <Btn variant="outline" onClick={onClose}>
            {t("admin.action.cancel")}
          </Btn>
          <Btn
            type="submit"
            variant="primary"
            busy={busy}
            icon={Plus}
            disabled={!selected.length || !name.trim()}
          >
            {t("admin.workflow.create")}
          </Btn>
        </div>
      </form>
    </Dialog>
  )
}

export function WorkflowView({ data, onNavigate }) {
  const { locale, t } = useAppSettings()
  const {
    config,
    workflowState,
    setWorkflowState,
    jobs,
    runs,
    activeJob,
    eventsPage,
    notify,
    fail,
    startJob,
  } = data
  const workflows = workflowState?.workflows || []
  const steps = workflowState?.steps || []
  const skipped = workflowState?.skippedByEnvironment || []
  const [selectedId, setSelectedId] = useState(readSelectedWorkflow)
  const [busy, setBusy] = useState("")
  const [creating, setCreating] = useState(false)
  const [renaming, setRenaming] = useState(null)

  const selected =
    workflows.find((workflow) => workflow.id === selectedId) || workflows[0]

  useEffect(() => {
    if (selected?.id) rememberSelectedWorkflow(selected.id)
  }, [selected?.id])

  const latestJob = activeJob || jobs[0] || null
  const models = config?.models || []
  const usableModelCount = models.filter((model) => {
    const provider = config?.providers?.find(
      (item) => item.id === model.providerId,
    )
    return model.enabled && provider?.enabled && provider?.apiKeyConfigured
  }).length
  const lastRun = runs.find((run) => run.status !== "running") || null

  const metrics = useMemo(() => {
    const events = eventsPage?.items || []
    return {
      total: eventsPage?.total ?? 0,
      manual: events.filter((event) => event.provenance === "manual").length,
    }
  }, [eventsPage])

  const save = async (workflow, patch, key = "save") => {
    setBusy(`${key}-${workflow.id}`)
    try {
      const payload = await request(
        `/workflows/${encodeURIComponent(workflow.id)}`,
        { method: "PUT", body: jsonBody(patch) },
      )
      setWorkflowState((current) => ({ ...current, ...payload }))
      return payload.workflow
    } catch (error) {
      fail(error)
      return null
    } finally {
      setBusy("")
    }
  }

  const toggleStep = (workflow, stepId) => {
    const next = workflow.steps.includes(stepId)
      ? workflow.steps.filter((id) => id !== stepId)
      : [...workflow.steps, stepId]
    if (!next.length) {
      fail(adminMessage("admin.workflow.keepOneStep"))
      return
    }
    save(workflow, { steps: next }, `step-${stepId}`)
  }

  const run = async (workflow) => {
    setBusy(`run-${workflow.id}`)
    try {
      await startJob(`/workflows/${encodeURIComponent(workflow.id)}/run`)
    } finally {
      setBusy("")
    }
  }

  const create = async (input) => {
    try {
      const payload = await request("/workflows", {
        method: "POST",
        body: jsonBody(input),
      })
      setWorkflowState((current) => ({ ...current, ...payload }))
      setSelectedId(payload.workflow.id)
      setCreating(false)
      notify(adminMessage("admin.workflow.created"))
    } catch (error) {
      fail(error)
    }
  }

  const remove = async (workflow) => {
    if (
      !window.confirm(
        t("admin.workflow.deleteConfirm", { name: workflow.name }),
      )
    )
      return
    try {
      const payload = await request(
        `/workflows/${encodeURIComponent(workflow.id)}`,
        { method: "DELETE" },
      )
      setWorkflowState((current) => ({ ...current, ...payload }))
      setSelectedId("")
      notify(adminMessage("admin.workflow.deleted"))
    } catch (error) {
      fail(error)
    }
  }

  const submitRename = async (event) => {
    event.preventDefault()
    if (!renaming?.name.trim()) return
    const saved = await save(selected, { name: renaming.name }, "rename")
    if (saved) setRenaming(null)
  }

  const scheduler = workflowState?.scheduler
  const isRunning = Boolean(activeJob)

  return (
    <section className="adm-view" aria-labelledby="adm-workflow-title">
      <PageHeader
        id="adm-workflow-title"
        title={t("admin.workflow.title")}
        description={t("admin.workflow.description")}
      />

      <div className="adm-metrics">
        <Metric
          label={t("admin.metric.schedules")}
          value={metrics.total}
          note={t("admin.metric.schedulesNote", { count: metrics.manual })}
          icon={CalendarClock}
          tone="berry"
        />
        <Metric
          label={t("admin.metric.models")}
          value={usableModelCount}
          note={t("admin.metric.modelsNote", {
            count: config?.providers?.length || 0,
          })}
          icon={Bot}
          tone="lavender"
        />
        <Metric
          label={t("admin.metric.scheduled")}
          value={
            workflows.filter((workflow) => workflow.scheduleEnabled).length
          }
          note={t("admin.metric.scheduledNote", { count: workflows.length })}
          icon={Clock3}
          tone="sky"
        />
        <Metric
          label={t("admin.metric.lastSync")}
          value={
            lastRun
              ? formatRelative(lastRun.finishedAt || lastRun.startedAt, locale)
              : "—"
          }
          note={lastRun ? statusLabel(lastRun.status, t) : t("admin.run.empty")}
          icon={History}
          tone={lastRun ? statusTone(lastRun.status) : "neutral"}
        />
      </div>

      <Card className="adm-runner">
        <div
          className="adm-runner-tabs"
          role="tablist"
          aria-label={t("admin.workflow.list")}
        >
          {workflows.map((workflow) => (
            <button
              key={workflow.id}
              type="button"
              role="tab"
              aria-selected={workflow.id === selected?.id}
              className={cn(
                "adm-runner-tab",
                workflow.id === selected?.id && "is-active",
              )}
              onClick={() => {
                setSelectedId(workflow.id)
                setRenaming(null)
              }}
            >
              <span>{workflow.name}</span>
              {workflow.scheduleEnabled ? (
                <Clock3 aria-label={t("admin.workflow.scheduled")} />
              ) : null}
            </button>
          ))}
          <button
            type="button"
            className="adm-runner-tab is-add"
            onClick={() => setCreating(true)}
          >
            <Plus aria-hidden="true" />
            {t("admin.workflow.new")}
          </button>
        </div>

        {selected ? (
          <div className="adm-runner-body" role="tabpanel">
            <div className="adm-runner-head">
              {renaming ? (
                <form className="adm-rename" onSubmit={submitRename}>
                  <input
                    value={renaming.name}
                    onChange={(event) =>
                      setRenaming({ name: event.target.value })
                    }
                    maxLength={80}
                    autoFocus
                    aria-label={t("admin.workflow.name")}
                  />
                  <IconBtn
                    type="submit"
                    label={t("admin.action.save")}
                    icon={Check}
                    busy={busy === `rename-${selected.id}`}
                  />
                </form>
              ) : (
                <div className="adm-runner-name">
                  <h3>{selected.name}</h3>
                  <IconBtn
                    label={t("admin.workflow.rename")}
                    icon={Pencil}
                    onClick={() => setRenaming({ name: selected.name })}
                  />
                  {workflows.length > 1 ? (
                    <IconBtn
                      label={t("admin.action.delete")}
                      icon={Trash2}
                      onClick={() => remove(selected)}
                    />
                  ) : null}
                </div>
              )}
              <p className="adm-muted">{t("admin.workflow.modulesHint")}</p>
            </div>

            <ol className="adm-modules">
              {steps.map((step, index) => {
                const meta = stepMeta[step.id] || { icon: Workflow }
                const Icon = meta.icon
                const checked = selected.steps.includes(step.id)
                const warnings = stepWarnings(step.id, { config, skipped })
                return (
                  <li key={step.id} className="adm-module-slot">
                    <button
                      type="button"
                      className={cn(
                        "adm-module",
                        `adm-tone-${meta.tone}`,
                        checked && "is-on",
                      )}
                      aria-pressed={checked}
                      onClick={() => toggleStep(selected, step.id)}
                      disabled={busy !== "" || isRunning}
                    >
                      <span className="adm-module-top">
                        <span className="adm-module-icon" aria-hidden="true">
                          <Icon />
                        </span>
                        <span className="adm-module-check" aria-hidden="true">
                          {busy === `step-${step.id}-${selected.id}` ? (
                            <LoaderCircle className="adm-spin" />
                          ) : checked ? (
                            <Check />
                          ) : null}
                        </span>
                      </span>
                      <strong>{t(`admin.step.${step.id}`)}</strong>
                      <small>{t(`admin.step.${step.id}Description`)}</small>
                      <span className="adm-module-group">
                        {t(`admin.step.group.${step.group}`)}
                      </span>
                      {checked && warnings.length ? (
                        <span className="adm-module-warning">
                          <TriangleAlert aria-hidden="true" />
                          {t(warnings[0])}
                        </span>
                      ) : null}
                    </button>
                    {index < steps.length - 1 ? (
                      <ArrowRight
                        className="adm-module-arrow"
                        aria-hidden="true"
                      />
                    ) : null}
                  </li>
                )
              })}
            </ol>

            <div className="adm-runner-foot">
              <SourceLookbackEditor
                key={selected.id}
                workflow={selected}
                disabled={busy !== ""}
                onSave={(sourceLookbackDays) =>
                  save(selected, { sourceLookbackDays }, "lookback")
                }
              />
              <div className="adm-schedule-bar">
                <Switch
                  checked={selected.scheduleEnabled}
                  onChange={(value) =>
                    save(selected, { scheduleEnabled: value }, "schedule")
                  }
                  label={t("admin.workflow.schedule")}
                  disabled={busy !== ""}
                />
                <div className="adm-schedule-copy">
                  <strong>{t("admin.workflow.schedule")}</strong>
                  <small>
                    {selected.scheduleEnabled && selected.nextRunAt
                      ? t("admin.workflow.nextRun", {
                          time: formatDateTime(selected.nextRunAt, locale),
                          relative: formatRelative(selected.nextRunAt, locale),
                        })
                      : t("admin.workflow.manualOnly")}
                  </small>
                </div>
                <select
                  value={selected.intervalMinutes}
                  onChange={(event) =>
                    save(
                      selected,
                      { intervalMinutes: Number(event.target.value) },
                      "interval",
                    )
                  }
                  disabled={busy !== ""}
                  aria-label={t("admin.workflow.interval")}
                >
                  {[...new Set([...intervalOptions, selected.intervalMinutes])]
                    .sort((left, right) => left - right)
                    .map((minutes) => (
                      <option key={minutes} value={minutes}>
                        {t("admin.workflow.every", {
                          interval: formatInterval(minutes, t),
                        })}
                      </option>
                    ))}
                </select>
              </div>
              <Btn
                variant="primary"
                size="lg"
                icon={Play}
                busy={busy === `run-${selected.id}`}
                disabled={isRunning || !selected.steps.length}
                onClick={() => run(selected)}
                className="adm-run-button"
              >
                {isRunning
                  ? t("admin.workflow.running")
                  : t("admin.workflow.runNow")}
              </Btn>
            </div>
            {selected.scheduleEnabled && scheduler && !scheduler.running ? (
              <p className="adm-callout is-warning">
                <TriangleAlert aria-hidden="true" />
                {t("admin.workflow.schedulerOff")}
              </p>
            ) : null}
            {selected.lastRunAt ? (
              <p className="adm-runner-last">
                {t("admin.workflow.lastRun", {
                  time: formatRelative(selected.lastRunAt, locale),
                })}
                {selected.lastStatus ? (
                  <StatusPill status={selected.lastStatus} t={t} />
                ) : null}
              </p>
            ) : null}
          </div>
        ) : (
          <EmptyState icon={Workflow} title={t("admin.workflow.empty")}>
            <Btn icon={Plus} onClick={() => setCreating(true)}>
              {t("admin.workflow.new")}
            </Btn>
          </EmptyState>
        )}
      </Card>

      <div className="adm-grid-2">
        <Card>
          <CardHeader
            icon={Workflow}
            title={
              activeJob
                ? t("admin.workflow.liveTitle")
                : t("admin.workflow.lastJob")
            }
            description={t("admin.workflow.liveDescription")}
          />
          <JobProgress job={latestJob} steps={steps} t={t} locale={locale} />
        </Card>
        <Card>
          <CardHeader
            icon={History}
            title={t("admin.run.title")}
            description={t("admin.run.description")}
            actions={
              <Btn
                variant="ghost"
                size="sm"
                icon={Sparkles}
                onClick={() => startJob("/scan/automatic")}
                disabled={isRunning}
              >
                {t("admin.workflow.scanOnly")}
              </Btn>
            }
          />
          <RunHistory runs={runs.slice(0, 8)} t={t} locale={locale} />
        </Card>
      </div>

      <Card className="adm-guardrails">
        <CardHeader
          icon={LockKeyhole}
          title={t("admin.guard.title")}
          description={t("admin.guard.description")}
          actions={
            <Btn
              variant="outline"
              size="sm"
              onClick={() => onNavigate("providers")}
            >
              <Bot aria-hidden="true" />
              {t("admin.guard.providers")}
            </Btn>
          }
        />
        <ul className="adm-guard-list">
          <li>
            <Tag tone="leaf">01</Tag>
            {t("admin.guard.one")}
          </li>
          <li>
            <Tag tone="sky">02</Tag>
            {t("admin.guard.two")}
          </li>
          <li>
            <Tag tone="honey">03</Tag>
            {t("admin.guard.three")}
          </li>
        </ul>
      </Card>

      {creating ? (
        <WorkflowDialog
          steps={steps}
          onClose={() => setCreating(false)}
          onCreate={create}
          t={t}
        />
      ) : null}
    </section>
  )
}
