import { useEffect, useState } from "react"
import { Download } from "lucide-react"
import { useAppSettings } from "@/app-settings"
import { formatDateTime, statusLabel } from "@/admin/format"
import {
  Btn,
  Card,
  CardHeader,
  Field,
  Tag,
} from "@/admin/components/primitives"

export function SourceFetchCard({ source, data }) {
  const { t, locale } = useAppSettings()
  const [mode, setMode] = useState("recent")
  const [days, setDays] = useState(source === "x" ? 7 : 14)
  const [startDate, setStartDate] = useState("")
  const [endDate, setEndDate] = useState("")
  const [busy, setBusy] = useState(false)
  const status = data.sourceFetch?.[source]
  const oldest =
    source === "x"
      ? status?.accounts
          ?.map((account) => account.oldest)
          .filter(Boolean)
          .sort()[0]
      : status?.oldest
  const historyBlocked = mode !== "recent" && !status?.historyAvailable
  const lastJob = data.jobs?.find((job) => job.fetchWindows?.[source])
  const result = lastJob?.result?.results?.[source]
  const warnings = [
    ...new Set([
      ...(result?.errors || []),
      ...(result?.accounts || []).flatMap((account) =>
        (account.errors || []).map((error) => `${account.handle}: ${error}`),
      ),
    ]),
  ]
  const fetchRecords = async (event) => {
    event.preventDefault()
    setBusy(true)
    try {
      await data.startJob(`/sources/${source}/fetch`, {
        window:
          mode === "range"
            ? { mode, startDate, endDate }
            : { mode, days: Number(days) },
      })
    } finally {
      setBusy(false)
    }
  }
  return (
    <Card>
      <CardHeader
        icon={Download}
        title={t("admin.fetch.title", {
          source: source === "x" ? "X" : "YouTube",
        })}
        description={t("admin.fetch.description")}
      />
      <form className="adm-form" onSubmit={fetchRecords}>
        <Field label={t("admin.fetch.mode")}>
          <select
            value={mode}
            onChange={(event) => setMode(event.target.value)}
          >
            {["recent", "before", "range"].map((value) => (
              <option key={value} value={value}>
                {t(`admin.fetch.${value}`)}
              </option>
            ))}
          </select>
        </Field>
        {mode === "range" ? (
          <>
            <Field label={t("admin.fetch.start")}>
              <input
                type="date"
                required
                value={startDate}
                max={endDate || undefined}
                onChange={(event) => setStartDate(event.target.value)}
              />
            </Field>
            <Field label={t("admin.fetch.end")}>
              <input
                type="date"
                required
                value={endDate}
                min={startDate || undefined}
                onChange={(event) => setEndDate(event.target.value)}
              />
            </Field>
          </>
        ) : (
          <Field
            label={t(
              mode === "before"
                ? "admin.fetch.backfillDays"
                : "admin.fetch.days",
            )}
          >
            <input
              type="number"
              min="1"
              max="365"
              step="1"
              required
              value={days}
              onChange={(event) => setDays(event.target.value)}
            />
          </Field>
        )}
        <p className="adm-muted is-wide">
          {t("admin.fetch.oldest", {
            time: oldest
              ? formatDateTime(oldest, locale)
              : t("admin.fetch.noRecords"),
          })}
        </p>
        <p className="adm-muted is-wide">{t("admin.fetch.timezone")}</p>
        {mode === "before" ? (
          <p className="adm-muted is-wide">
            {t(
              oldest
                ? source === "x"
                  ? "admin.fetch.beforeHint"
                  : "admin.fetch.youtubeBeforeHint"
                : "admin.fetch.emptyHint",
            )}
          </p>
        ) : null}
        {!status?.historyAvailable ? (
          <p className="adm-muted is-wide">
            {t(
              source === "x"
                ? "admin.fetch.xKeyHint"
                : "admin.fetch.youtubeKeyHint",
            )}
          </p>
        ) : null}
        <div className="adm-form-actions is-wide">
          <Btn
            type="submit"
            variant="primary"
            icon={Download}
            busy={busy}
            disabled={
              Boolean(data.activeJob) ||
              !status ||
              historyBlocked ||
              (mode === "before" && !oldest)
            }
          >
            {t("admin.fetch.run")}
          </Btn>
        </div>
      </form>
      {lastJob ? (
        <div className="adm-fetch-result">
          <Tag>{statusLabel(lastJob.result?.status || lastJob.status, t)}</Tag>
          {result ? (
            <>
              <span>
                {t("admin.fetch.result", { count: result.count || 0 })}
              </span>
              {warnings.length ? (
                <p className="adm-muted">{t("admin.fetch.partialHint")}</p>
              ) : null}
              {warnings.length ? (
                <ul className="adm-muted">
                  {warnings.map((warning) => (
                    <li key={warning}>{warning}</li>
                  ))}
                </ul>
              ) : null}
              {result.window ? (
                <p className="adm-muted">
                  {formatDateTime(result.window.startTime, locale)} —{" "}
                  {formatDateTime(result.window.endTime, locale)}
                </p>
              ) : null}
              {(result.accounts || [])
                .filter((account) => account.window)
                .map((account) => (
                  <p className="adm-muted" key={account.handle}>
                    @{account.handle}:{" "}
                    {formatDateTime(account.window.startTime, locale)} —{" "}
                    {formatDateTime(account.window.endTime, locale)}
                  </p>
                ))}
              {result.error ? (
                <p className="adm-muted">{result.error}</p>
              ) : null}
            </>
          ) : lastJob.error ? (
            <p className="adm-muted">{lastJob.error}</p>
          ) : null}
        </div>
      ) : null}
    </Card>
  )
}

export function SourceLookbackFields({
  value,
  sources,
  onChange,
  disabled = false,
}) {
  const { t } = useAppSettings()
  return (
    <div className="adm-form adm-lookback-fields">
      {["x", "youtube"]
        .filter((source) => sources.includes(source))
        .map((source) => (
          <Field
            key={source}
            label={t("admin.fetch.workflowDays", {
              source: source === "x" ? "X" : "YouTube",
            })}
          >
            <input
              type="number"
              min="1"
              max="365"
              step="1"
              required
              disabled={disabled}
              value={value?.[source] ?? (source === "x" ? 7 : 14)}
              onChange={(event) =>
                onChange({ ...value, [source]: event.target.value })
              }
            />
          </Field>
        ))}
    </div>
  )
}

export function SourceLookbackEditor({ workflow, onSave, disabled }) {
  const { t } = useAppSettings()
  const [value, setValue] = useState(workflow.sourceLookbackDays)
  useEffect(
    () => setValue(workflow.sourceLookbackDays),
    [
      workflow.id,
      workflow.sourceLookbackDays.x,
      workflow.sourceLookbackDays.youtube,
    ],
  )
  if (!workflow.steps.some((source) => ["x", "youtube"].includes(source)))
    return null
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault()
        onSave(
          Object.fromEntries(
            Object.entries(value).map(([source, days]) => [
              source,
              Number(days),
            ]),
          ),
        )
      }}
    >
      <p className="adm-muted">{t("admin.fetch.workflowHint")}</p>
      <SourceLookbackFields
        value={value}
        sources={workflow.steps}
        onChange={setValue}
        disabled={disabled}
      />
      <Btn
        type="submit"
        variant="outline"
        disabled={
          disabled ||
          (Number(value.x) === workflow.sourceLookbackDays.x &&
            Number(value.youtube) === workflow.sourceLookbackDays.youtube)
        }
      >
        {t("admin.action.save")}
      </Btn>
    </form>
  )
}
