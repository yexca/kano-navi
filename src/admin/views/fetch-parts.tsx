import { useState } from "react"
import { ArrowRight, CalendarRange, Download, KeyRound } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { japanToday } from "@/admin/format"
import type { FetchWindow } from "@/admin/types"
import { Badge, Button, Field, Panel } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"

const presets = [1, 3, 7, 30] as const

/** A Japan date `days` before `date`, both `YYYY-MM-DD`. */
function shiftDate(date: string, days: number) {
  const value = new Date(`${date}T00:00:00Z`)
  value.setUTCDate(value.getUTCDate() - days)
  return value.toISOString().slice(0, 10)
}

/**
 * One-click fetch windows for one source. It starts the same `fetch` run as
 * the Sources page; the lists reload when the run finishes.
 */
export function QuickFetch({
  source,
  navigate,
}: {
  source: "x" | "youtube"
  navigate: ViewProps["navigate"]
}) {
  const { t } = useAppSettings()
  const { api, start, overview } = useAdmin()
  const sources = useResource(() => api.sources(), [])
  const today = japanToday()
  const [startDate, setStartDate] = useState(() => shiftDate(today, 6))
  const [endDate, setEndDate] = useState(today)
  const [busy, setBusy] = useState("")
  const state = sources.data?.[source]
  const historyAvailable = Boolean(state?.historyAvailable)
  const running = Boolean(overview?.activeRun)
  const disabled = !state || state.skipped || running || busy !== ""
  const run = async (key: string, window: FetchWindow) => {
    setBusy(key)
    await start({ kind: "fetch", source, window })
    setBusy("")
  }
  return (
    <Panel
      icon={Download}
      title={t(`admin.quickFetch.title.${source}`)}
      description={t(`admin.quickFetch.description.${source}`)}
      actions={
        state ? (
          <span className="adm-chip-row">
            {state.skipped ? (
              <Badge>{t("admin.overview.skipped")}</Badge>
            ) : null}
            <Badge tone={historyAvailable ? "leaf" : "neutral"} icon={KeyRound}>
              {t(
                historyAvailable
                  ? "admin.sources.historyOn"
                  : "admin.sources.historyOff",
              )}
            </Badge>
          </span>
        ) : null
      }
    >
      <div className="adm-quick-fetch">
        <div className="adm-quick-group">
          <span className="adm-field-label">
            {t("admin.quickFetch.recent")}
          </span>
          <div className="adm-actions">
            {presets.map((days) => (
              <Button
                key={days}
                size="sm"
                busy={busy === `recent-${days}`}
                disabled={disabled}
                onClick={() => run(`recent-${days}`, { mode: "recent", days })}
              >
                {t(`admin.quickFetch.preset.${days}`)}
              </Button>
            ))}
          </div>
          <p className="adm-field-hint">{t("admin.fetch.hint.recent")}</p>
        </div>
        <form
          className="adm-quick-group"
          onSubmit={(event) => {
            event.preventDefault()
            void run("range", { mode: "range", startDate, endDate })
          }}
        >
          <span className="adm-field-label">{t("admin.fetch.mode.range")}</span>
          <div className="adm-quick-range">
            <Field label={t("admin.fetch.start")}>
              <input
                type="date"
                value={startDate}
                max={endDate}
                onChange={(e) => setStartDate(e.target.value)}
                required
              />
            </Field>
            <Field label={t("admin.fetch.end")}>
              <input
                type="date"
                value={endDate}
                min={startDate}
                max={today}
                onChange={(e) => setEndDate(e.target.value)}
                required
              />
            </Field>
            <Button
              type="submit"
              size="sm"
              variant="primary"
              icon={CalendarRange}
              busy={busy === "range"}
              disabled={disabled || !historyAvailable}
            >
              {t("admin.quickFetch.range")}
            </Button>
          </div>
          {state && !historyAvailable ? (
            <p className="adm-inline-error">
              <KeyRound aria-hidden />
              {t(`admin.fetch.needsKey.${source}`)}
            </p>
          ) : (
            <p className="adm-field-hint">{t("admin.fetch.hint.range")}</p>
          )}
        </form>
      </div>
      <div className="adm-quick-foot">
        <p className="adm-muted">
          {running ? t("admin.quickFetch.running") : t("admin.quickFetch.more")}
        </p>
        <Button
          size="sm"
          variant="ghost"
          icon={ArrowRight}
          onClick={() => navigate(running ? "runs" : "sources")}
        >
          {t(running ? "admin.quickFetch.openRuns" : "admin.nav.sources")}
        </Button>
      </div>
    </Panel>
  )
}
