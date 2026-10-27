import {
  ArrowRight,
  CalendarClock,
  CheckCircle2,
  CircleSlash,
  FileWarning,
  ImageOff,
  RefreshCw,
  Sparkles,
  Square,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatDateTime, formatRelative } from "@/admin/format"
import type { SourceHealth } from "@/admin/types"
import {
  Badge,
  Button,
  ConfirmButton,
  Empty,
  Loading,
  PageHeader,
  Panel,
  StatusBadge,
} from "@/admin/ui"
import { useAdmin } from "@/admin/use-admin-data"
import { RunLine, RunSteps, runTitle, stepIcons } from "@/admin/views/run-parts"

function SourceRow({
  id,
  health,
}: {
  id: "x" | "youtube"
  health: SourceHealth
}) {
  const { t, locale } = useAppSettings()
  const Icon = stepIcons[id]
  return (
    <li className="adm-source-row">
      <span className="adm-source-icon" aria-hidden>
        <Icon />
      </span>
      <span className="adm-source-copy">
        <strong>{t(`admin.step.${id}`)}</strong>
        <small>
          {health.lastSuccessAt
            ? t("admin.overview.lastSuccess", {
                time: formatRelative(health.lastSuccessAt, locale),
              })
            : t("admin.overview.neverSucceeded")}
        </small>
      </span>
      <span className="adm-source-state">
        {health.skipped ? (
          <Badge tone="neutral" icon={CircleSlash}>
            {t("admin.overview.skipped")}
          </Badge>
        ) : health.lastStep ? (
          <StatusBadge status={health.lastStep.status} />
        ) : (
          <Badge>{t("admin.overview.noRuns")}</Badge>
        )}
        {health.push?.enabled ? (
          <Badge tone={health.push.verified ? "leaf" : "honey"}>
            {t(
              health.push.verified
                ? "admin.overview.pushVerified"
                : "admin.overview.pushPending",
            )}
          </Badge>
        ) : null}
      </span>
    </li>
  )
}

export function OverviewView({ navigate }: ViewProps) {
  const { t, locale } = useAppSettings()
  const { overview, start, act, api, refreshOverview } = useAdmin()
  if (!overview) return <Loading />

  const { review, counts, activeRun, timers } = overview
  const attention = [
    {
      key: "images",
      count: review.pendingImages,
      icon: ImageOff,
      tone: "honey",
    },
    {
      key: "cancellations",
      count: review.suspectedCancellations,
      icon: CalendarClock,
      tone: "berry",
    },
    {
      key: "posts",
      count: review.failedPosts,
      icon: FileWarning,
      tone: "danger",
    },
  ].filter((item) => item.count > 0)

  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.overview.title")}
        description={t("admin.overview.description")}
        actions={
          <>
            <Button
              icon={Sparkles}
              disabled={Boolean(activeRun)}
              onClick={() => start({ kind: "scan" })}
            >
              {t("admin.action.scan")}
            </Button>
            <Button
              variant="primary"
              icon={RefreshCw}
              disabled={Boolean(activeRun)}
              onClick={() => start({ kind: "sync" })}
            >
              {t("admin.action.syncAll")}
            </Button>
          </>
        }
      />

      <Panel
        title={t("admin.overview.attention")}
        description={t("admin.overview.attentionHint")}
      >
        {attention.length ? (
          <div className="adm-attention">
            {attention.map((item) => {
              const Icon = item.icon
              return (
                <button
                  key={item.key}
                  type="button"
                  className={`adm-attention-item adm-tone-${item.tone}`}
                  onClick={() => navigate("review", item.key)}
                >
                  <Icon aria-hidden />
                  <span>
                    <strong>{item.count}</strong>
                    {t(`admin.overview.attention.${item.key}`, {
                      count: item.count,
                    })}
                  </span>
                  <ArrowRight aria-hidden />
                </button>
              )
            })}
          </div>
        ) : (
          <Empty icon={CheckCircle2} title={t("admin.overview.allClear")}>
            {t("admin.overview.allClearHint")}
          </Empty>
        )}
        {review.reprocessRequested ? (
          <p className="adm-note">
            {t("admin.overview.reprocessQueued", {
              count: review.reprocessRequested,
            })}
          </p>
        ) : null}
      </Panel>

      <div className="adm-grid is-two">
        <Panel
          title={t("admin.overview.now")}
          actions={
            activeRun ? (
              <ConfirmButton
                size="sm"
                icon={Square}
                onConfirm={async () => {
                  await act(
                    () => api.cancelRun(activeRun.id),
                    "admin.notice.cancelRequested",
                  )
                  void refreshOverview().catch(() => {})
                }}
              >
                {t("admin.action.cancel")}
              </ConfirmButton>
            ) : null
          }
        >
          {activeRun ? (
            <div className="adm-now">
              <p className="adm-now-title">
                <StatusBadge status={activeRun.status} />
                <strong>{runTitle(activeRun, t)}</strong>
              </p>
              <RunSteps run={activeRun} />
              <p className="adm-muted">
                {t("admin.overview.startedAt", {
                  time: formatDateTime(
                    activeRun.startedAt || activeRun.createdAt,
                    locale,
                  ),
                })}
              </p>
            </div>
          ) : (
            <div className="adm-now is-idle">
              <p className="adm-now-title">
                <Badge tone="neutral">{t("admin.overview.idle")}</Badge>
              </p>
              <p className="adm-muted">
                {timers.scheduled && timers.nextRunAt
                  ? t("admin.overview.nextRun", {
                      time: formatDateTime(timers.nextRunAt, locale),
                      relative: formatRelative(timers.nextRunAt, locale),
                    })
                  : t("admin.overview.noSchedule")}
              </p>
              <Button
                size="sm"
                variant="ghost"
                icon={CalendarClock}
                onClick={() => navigate("sources")}
              >
                {t("admin.overview.openTimers")}
              </Button>
            </div>
          )}
        </Panel>

        <Panel
          title={t("admin.overview.sources")}
          actions={
            <Button
              size="sm"
              variant="ghost"
              onClick={() => navigate("sources")}
            >
              {t("admin.overview.manageSources")}
            </Button>
          }
        >
          <ul className="adm-source-list">
            <SourceRow id="x" health={overview.sources.x} />
            <SourceRow id="youtube" health={overview.sources.youtube} />
          </ul>
        </Panel>
      </div>

      <div className="adm-stats">
        {(["upcomingEvents", "events", "posts", "videos"] as const).map(
          (key) => (
            <div className="adm-stat" key={key}>
              <strong>{counts[key]}</strong>
              <span>{t(`admin.overview.count.${key}`)}</span>
            </div>
          ),
        )}
      </div>

      <Panel
        title={t("admin.overview.recent")}
        actions={
          <Button size="sm" variant="ghost" onClick={() => navigate("runs")}>
            {t("admin.overview.allRuns")}
          </Button>
        }
      >
        {overview.recentRuns.length ? (
          <div className="adm-run-list">
            {overview.recentRuns.map((run) => (
              <RunLine key={run.id} run={run} />
            ))}
          </div>
        ) : (
          <Empty title={t("admin.overview.noRunsYet")}>
            {t("admin.overview.noRunsHint")}
          </Empty>
        )}
      </Panel>
    </div>
  )
}
