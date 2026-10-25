import { useState } from "react"
import {
  ArrowRight,
  CalendarRange,
  Download,
  History,
  KeyRound,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { japanToday } from "@/admin/format"
import type { FetchWindow, Sources } from "@/admin/types"
import { Badge, Button, Field, Panel } from "@/admin/ui"
import { useAdmin } from "@/admin/use-admin-data"

const presets = [1, 3, 7, 30] as const
/** Newest posts per account; the server accepts 1 to 200. */
const counts = [20, 50, 100, 200] as const

/**
 * How past windows are read: the source API with its credential, the public
 * X timeline without it, or not at all.
 */
export function WindowBadge({
  state,
}: {
  state: Sources["x"] | Sources["youtube"]
}) {
  const { t } = useAppSettings()
  if (state.historyAvailable) {
    return (
      <Badge tone="leaf" icon={KeyRound}>
        {t("admin.sources.historyOn")}
      </Badge>
    )
  }
  if (state.windows.includes("range")) {
    return (
      <Badge tone="leaf" icon={History}>
        {t("admin.sources.historyTimeline")}
      </Badge>
    )
  }
  return <Badge icon={KeyRound}>{t("admin.sources.historyOff")}</Badge>
}

/** A Japan date `days` before `date`, both `YYYY-MM-DD`. */
function shiftDate(date: string, days: number) {
  const value = new Date(`${date}T00:00:00Z`)
  value.setUTCDate(value.getUTCDate() - days)
  return value.toISOString().slice(0, 10)
}

/**
 * One-click fetch windows for one source. It starts the same `fetch` run as
 * the Sources page; the lists reload when the run finishes. The page loads
 * `sources` once: reads are keyed by path, so a second load here would cancel
 * the page's own.
 */
export function QuickFetch({
  source,
  sources,
  navigate,
}: {
  source: "x" | "youtube"
  sources: Sources | undefined
  navigate: ViewProps["navigate"]
}) {
  const { t } = useAppSettings()
  const { start, overview } = useAdmin()
  const today = japanToday()
  const [startDate, setStartDate] = useState(() => shiftDate(today, 6))
  const [endDate, setEndDate] = useState(today)
  const [busy, setBusy] = useState("")
  const state = sources?.[source]
  const windows = state?.windows ?? []
  const rangeAvailable = windows.includes("range")
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
            <WindowBadge state={state} />
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
          <p className="adm-field-hint">
            {t(
              rangeAvailable
                ? "admin.fetch.hint.recentAll"
                : "admin.fetch.hint.recent",
            )}
          </p>
        </div>
        {windows.includes("count") ? (
          <div className="adm-quick-group">
            <span className="adm-field-label">
              {t("admin.quickFetch.latest")}
            </span>
            <div className="adm-actions">
              {counts.map((count) => (
                <Button
                  key={count}
                  size="sm"
                  busy={busy === `count-${count}`}
                  disabled={disabled}
                  onClick={() =>
                    run(`count-${count}`, { mode: "count", count })
                  }
                >
                  {t("admin.fetch.postsCount", { count })}
                </Button>
              ))}
            </div>
            <p className="adm-field-hint">{t("admin.fetch.hint.count")}</p>
          </div>
        ) : null}
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
              disabled={disabled || !rangeAvailable}
            >
              {t("admin.quickFetch.range")}
            </Button>
          </div>
          {state && !rangeAvailable ? (
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
