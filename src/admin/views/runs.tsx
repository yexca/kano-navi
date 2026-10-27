import { useState, type SubmitEvent } from "react"
import {
  ListChecks,
  Play,
  RefreshCw,
  RotateCcw,
  Sparkles,
  Square,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatDateTime } from "@/admin/format"
import type { Run } from "@/admin/types"
import {
  Button,
  ConfirmButton,
  Empty,
  Facts,
  Loading,
  PageHeader,
  Pager,
  Panel,
  StatusBadge,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { RunLine, stepIcons } from "@/admin/views/run-parts"

/** Pick steps from the catalog and run them once; nothing is saved. Sources
 * also run on their own timers on the Sources page (ADR-0003). */
function StepRunPanel() {
  const { t } = useAppSettings()
  const { api, start, overview } = useAdmin()
  const catalog = useResource(() => api.stepCatalog(), [])
  const skipped = catalog.data?.skippedSteps || []
  const [steps, setSteps] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const submit = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    if (await start({ kind: "workflow", steps })) setSteps([])
    setBusy(false)
  }
  return (
    <Panel
      title={t("admin.runs.stepRun")}
      description={t("admin.runs.stepRunHint")}
      icon={ListChecks}
    >
      {!catalog.data ? (
        <Loading />
      ) : (
        <form className="adm-form" onSubmit={submit}>
          <fieldset className="adm-fieldset">
            <legend>{t("admin.runs.field.steps")}</legend>
            <div className="adm-step-picker">
              {catalog.data.steps.map((step) => {
                const Icon = stepIcons[step.id]
                const checked = steps.includes(step.id)
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
                        {skipped.includes(step.id)
                          ? t("admin.runs.skippedStep")
                          : t(`admin.step.${step.id}.hint`)}
                      </small>
                    </span>
                  </label>
                )
              })}
            </div>
          </fieldset>
          <div className="adm-form-actions">
            <Button
              type="submit"
              variant="primary"
              icon={Play}
              busy={busy}
              disabled={!steps.length || Boolean(overview?.activeRun)}
            >
              {t("admin.runs.runOnce")}
            </Button>
          </div>
        </form>
      )}
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
    .map(([key, item]): [string, unknown] => [key, flat(item)])
    .filter(([, item]) => item != null && typeof item !== "object")
    .map(([key, item]) => `${key}: ${item}`)
    .join(" · ")
}

/** Lists of plain values, such as error codes, read as one comma list. */
function flat(item: unknown): unknown {
  if (!Array.isArray(item) || item.length === 0) return item
  return item.every((entry) => entry == null || typeof entry !== "object")
    ? item.join(", ")
    : item
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
                {run.fetchWindows[step].max
                  ? ` · ${t("admin.fetch.maxCount", { count: run.fetchWindows[step].max })}`
                  : ""}
                {run.fetchWindows[step].count
                  ? ` · ${t(step === "youtube" ? "admin.fetch.videosCount" : "admin.fetch.postsCount", { count: run.fetchWindows[step].count })}`
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
              const run =
                (listed.id === activeId && overview?.activeRun) || listed
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
      <StepRunPanel />
      <HistoryPanel />
    </div>
  )
}
