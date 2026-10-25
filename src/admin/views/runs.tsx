import { useState } from "react"
import {
  CalendarClock,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  RotateCcw,
  Sparkles,
  Square,
  Trash2,
  Workflow as WorkflowIcon,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatDateTime, formatInterval, formatRelative } from "@/admin/format"
import type { Run, Workflow, WorkflowCatalog } from "@/admin/types"
import {
  Badge,
  Button,
  ConfirmButton,
  Empty,
  Facts,
  Field,
  IconButton,
  Loading,
  Modal,
  PageHeader,
  Pager,
  Panel,
  StatusBadge,
  Switch,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { RunLine, stepIcons } from "@/admin/views/run-parts"

const intervals = [15, 30, 60, 120, 180, 360, 720, 1440]

function WorkflowDialog({
  workflow,
  catalog,
  onClose,
}: {
  workflow: Workflow | null
  catalog: WorkflowCatalog
  onClose: (saved?: boolean) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [name, setName] = useState(workflow?.name || "")
  const [steps, setSteps] = useState<string[]>(
    workflow?.steps || [
      "x",
      "youtube",
      "media",
      "schedule",
      "translate",
      "reconcile",
    ],
  )
  const [scheduled, setScheduled] = useState(workflow?.scheduleEnabled ?? true)
  const [interval, setInterval] = useState(workflow?.intervalMinutes || 60)
  const [lookback, setLookback] = useState({
    x: workflow?.sourceLookbackDays?.x ?? 7,
    youtube: workflow?.sourceLookbackDays?.youtube ?? 14,
  })
  const [busy, setBusy] = useState(false)
  const options = intervals.includes(interval)
    ? intervals
    : [...intervals, interval].sort((a, b) => a - b)
  const save = async (event) => {
    event.preventDefault()
    setBusy(true)
    const body = {
      name,
      steps,
      scheduleEnabled: scheduled,
      intervalMinutes: interval,
      sourceLookbackDays: lookback,
    }
    const result = await act(
      () =>
        workflow
          ? api.updateWorkflow(workflow.id, body)
          : api.createWorkflow(body),
      "admin.notice.workflowSaved",
    )
    setBusy(false)
    if (result) onClose(true)
  }
  return (
    <Modal
      open
      title={
        workflow ? t("admin.runs.editWorkflow") : t("admin.runs.newWorkflow")
      }
      onClose={() => onClose()}
    >
      <form className="adm-form" onSubmit={save}>
        <Field label={t("admin.runs.field.name")}>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={80}
            required
            autoFocus
          />
        </Field>
        <fieldset className="adm-fieldset">
          <legend>{t("admin.runs.field.steps")}</legend>
          <div className="adm-step-picker">
            {catalog.steps.map((step) => {
              const Icon = stepIcons[step.id]
              const checked = steps.includes(step.id)
              const skipped = catalog.skippedSteps.includes(step.id)
              return (
                <label
                  key={step.id}
                  className={checked ? "is-checked" : undefined}
                >
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={(event) =>
                      setSteps((current) =>
                        event.target.checked
                          ? [...current, step.id]
                          : current.filter((id) => id !== step.id),
                      )
                    }
                  />
                  {Icon ? <Icon aria-hidden /> : null}
                  <span>
                    <strong>{t(`admin.step.${step.id}`)}</strong>
                    <small>
                      {skipped
                        ? t("admin.runs.skippedStep")
                        : t(`admin.step.${step.id}.hint`)}
                    </small>
                  </span>
                </label>
              )
            })}
          </div>
        </fieldset>
        <div className="adm-form-row">
          {(["x", "youtube"] as const).map((source) =>
            steps.includes(source) ? (
              <Field
                key={source}
                label={t("admin.runs.field.lookback", {
                  source: t(`admin.step.${source}`),
                })}
                hint={t("admin.runs.field.lookbackHint")}
              >
                <input
                  type="number"
                  min={1}
                  max={365}
                  value={lookback[source]}
                  onChange={(e) =>
                    setLookback((current) => ({
                      ...current,
                      [source]: Number(e.target.value),
                    }))
                  }
                />
              </Field>
            ) : null,
          )}
        </div>
        <Switch
          checked={scheduled}
          onChange={setScheduled}
          label={t("admin.runs.field.scheduled")}
          description={t("admin.runs.field.scheduledHint")}
        />
        {scheduled ? (
          <Field label={t("admin.runs.field.interval")}>
            <select
              value={interval}
              onChange={(e) => setInterval(Number(e.target.value))}
            >
              {options
                .filter(
                  (value) =>
                    value >= catalog.limits.minIntervalMinutes &&
                    value <= catalog.limits.maxIntervalMinutes,
                )
                .map((value) => (
                  <option key={value} value={value}>
                    {formatInterval(value, t)}
                  </option>
                ))}
            </select>
          </Field>
        ) : null}
        <div className="adm-form-actions">
          <Button onClick={() => onClose()}>{t("admin.action.cancel")}</Button>
          <Button
            type="submit"
            variant="primary"
            busy={busy}
            disabled={!steps.length}
          >
            {t("admin.action.save")}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

function WorkflowsPanel() {
  const { t, locale } = useAppSettings()
  const { api, act, start, overview } = useAdmin()
  const catalog = useResource(() => api.workflows(), [])
  const [editing, setEditing] = useState<Workflow | null | undefined>(undefined)
  const busy = Boolean(overview?.activeRun)
  return (
    <Panel
      title={t("admin.runs.workflows")}
      description={t("admin.runs.workflowsHint")}
      icon={WorkflowIcon}
      actions={
        <Button
          size="sm"
          icon={Plus}
          onClick={() => setEditing(null)}
          disabled={!catalog.data}
        >
          {t("admin.runs.newWorkflow")}
        </Button>
      }
    >
      {!catalog.data ? (
        <Loading />
      ) : catalog.data.items.length ? (
        <ul className="adm-workflow-list">
          {catalog.data.items.map((workflow) => (
            <li key={workflow.id} className="adm-workflow">
              <div className="adm-workflow-main">
                <strong>{workflow.name}</strong>
                <span className="adm-chip-row">
                  {workflow.steps.map((step) => {
                    const Icon = stepIcons[step]
                    return (
                      <Badge key={step} icon={Icon}>
                        {t(`admin.step.${step}`)}
                      </Badge>
                    )
                  })}
                </span>
              </div>
              <div className="adm-workflow-schedule">
                {workflow.scheduleEnabled ? (
                  <>
                    <Badge tone="leaf" icon={CalendarClock}>
                      {t("admin.runs.every", {
                        interval: formatInterval(workflow.intervalMinutes, t),
                      })}
                    </Badge>
                    {workflow.nextRunAt ? (
                      <small title={formatDateTime(workflow.nextRunAt, locale)}>
                        {t("admin.runs.next", {
                          relative: formatRelative(workflow.nextRunAt, locale),
                        })}
                      </small>
                    ) : null}
                  </>
                ) : (
                  <Badge>{t("admin.runs.manualOnly")}</Badge>
                )}
              </div>
              <div className="adm-workflow-last">
                {workflow.lastStatus ? (
                  <StatusBadge status={workflow.lastStatus} />
                ) : null}
                {workflow.lastRunAt ? (
                  <small>{formatRelative(workflow.lastRunAt, locale)}</small>
                ) : null}
              </div>
              <div className="adm-row-actions">
                <Button
                  size="sm"
                  icon={Play}
                  disabled={busy}
                  onClick={() =>
                    start({ kind: "workflow", workflowId: workflow.id })
                  }
                >
                  {t("admin.runs.runNow")}
                </Button>
                <IconButton
                  icon={Pencil}
                  label={t("admin.runs.editWorkflow")}
                  onClick={() => setEditing(workflow)}
                />
                <ConfirmButton
                  size="sm"
                  variant="ghost"
                  icon={Trash2}
                  aria-label={t("admin.runs.deleteWorkflow")}
                  onConfirm={async () => {
                    const result = await act(
                      () => api.deleteWorkflow(workflow.id),
                      "admin.notice.workflowDeleted",
                    )
                    if (result !== undefined) void catalog.reload()
                  }}
                />
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <Empty icon={WorkflowIcon} title={t("admin.runs.noWorkflows")}>
          {t("admin.runs.noWorkflowsHint")}
        </Empty>
      )}
      {editing !== undefined && catalog.data ? (
        <WorkflowDialog
          workflow={editing}
          catalog={catalog.data}
          onClose={(saved) => {
            setEditing(undefined)
            if (saved) void catalog.reload()
          }}
        />
      ) : null}
    </Panel>
  )
}

function stepResult(run: Run, step: string) {
  const results = run.result?.results || run.run?.counts || {}
  return results[step === "schedule" ? "schedules" : step] || results[step]
}

function describe(value: unknown): string {
  if (value == null) return ""
  if (typeof value !== "object") return String(value)
  return Object.entries(value as Record<string, unknown>)
    .filter(([, item]) => item != null && typeof item !== "object")
    .map(([key, item]) => `${key}: ${item}`)
    .join(" · ")
}

function RunDetail({ run, onChanged }: { run: Run; onChanged: () => void }) {
  const { t, locale } = useAppSettings()
  const { api, act, refreshOverview } = useAdmin()
  const active = run.status === "queued" || run.status === "running"
  return (
    <div className="adm-run-detail">
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
          [t("admin.runs.error"), run.error],
        ]}
      />
      <ul className="adm-step-results">
        {run.steps.map((step) => (
          <li key={step}>
            <strong>{t(`admin.step.${step}`)}</strong>
            <StatusBadge status={run.progress[step]} />
            {run.fetchWindows?.[step] ? (
              <small>
                {t(`admin.fetch.mode.${run.fetchWindows[step].mode}`)}
                {run.fetchWindows[step].days
                  ? ` · ${t("admin.fetch.daysCount", { count: run.fetchWindows[step].days })}`
                  : ""}
                {run.fetchWindows[step].count
                  ? ` · ${t("admin.fetch.postsCount", { count: run.fetchWindows[step].count })}`
                  : ""}
                {run.fetchWindows[step].startDate
                  ? ` · ${run.fetchWindows[step].startDate} – ${run.fetchWindows[step].endDate}`
                  : ""}
              </small>
            ) : null}
            <code>{describe(stepResult(run, step))}</code>
          </li>
        ))}
      </ul>
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
      </div>
    </div>
  )
}

function HistoryPanel() {
  const { t } = useAppSettings()
  const { api, overview } = useAdmin()
  const [page, setPage] = useState(1)
  const [open, setOpen] = useState("")
  const activeId = overview?.activeRun?.id
  const runs = useResource(() => api.runs(page), [page, activeId])
  return (
    <Panel
      title={t("admin.runs.history")}
      description={t("admin.runs.historyHint")}
    >
      {!runs.data ? (
        <Loading />
      ) : runs.data.items.length ? (
        <>
          <div className="adm-run-list">
            {runs.data.items.map((listed) => {
              // The overview poll carries fresher progress for the active run.
              const run = listed.id === activeId ? overview.activeRun : listed
              return (
                <div key={run.id}>
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
  const busy = Boolean(overview?.activeRun)
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.runs.title")}
        description={t("admin.runs.description")}
        actions={
          <>
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
      <WorkflowsPanel />
      <HistoryPanel />
    </div>
  )
}
