import {
  ArrowRight,
  CalendarClock,
  CalendarMinus,
  CalendarSearch,
  CheckCircle2,
  CircleSlash,
  Combine,
  FileWarning,
  ImageOff,
  Languages,
  ListEnd,
  ListOrdered,
  Play,
  RefreshCw,
  Sparkles,
  Square,
} from "lucide-react"
import { useState } from "react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatRelative, japanStamp } from "@/admin/format"
import type {
  Backlog,
  QueueEntry,
  ScheduleSource,
  SourceHealth,
} from "@/admin/types"
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
  id: ScheduleSource
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

/**
 * One timer in line for the queue: when it runs and, for a source timer,
 * Start now. While a run holds the queue the button reads Run next, and a
 * started timer waits behind the timers due before it.
 */
function QueueRow({ entry, busy }: { entry: QueueEntry; busy: boolean }) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const [pending, setPending] = useState(false)
  const Icon = stepIcons[entry.kind === "source" ? entry.source : "live"]
  const startNow = async (source: ScheduleSource) => {
    setPending(true)
    await act(
      () => api.startSourceSchedule(source),
      busy ? "admin.notice.timerNext" : "admin.notice.timerStarted",
    )
    setPending(false)
  }
  return (
    <li className="adm-source-row">
      <span className="adm-source-icon" aria-hidden>
        <Icon />
      </span>
      <span className="adm-source-copy">
        <strong>
          {t(
            entry.kind === "source"
              ? `admin.step.${entry.source}`
              : "admin.step.live",
          )}
        </strong>
        <small>
          {entry.due ? (
            t(busy ? "admin.queue.waiting" : "admin.queue.starting")
          ) : (
            <time
              dateTime={entry.nextRunAt}
              title={japanStamp(entry.nextRunAt)}
            >
              {t("admin.queue.at", {
                relative: formatRelative(entry.nextRunAt, locale),
              })}
            </time>
          )}
        </small>
      </span>
      <span className="adm-source-state">
        {entry.due ? (
          <Badge tone="honey">{t("admin.queue.due")}</Badge>
        ) : entry.kind === "source" ? (
          <Button
            size="sm"
            icon={busy ? ListEnd : Play}
            busy={pending}
            disabled={pending}
            onClick={() => startNow(entry.source)}
          >
            {t(busy ? "admin.queue.runNext" : "admin.queue.startNow")}
          </Button>
        ) : null}
      </span>
    </li>
  )
}

/**
 * The operator requests waiting for a run, each with the run that takes it
 * and a link to the list of what waits. Nothing shows while none waits.
 */
function BacklogList({
  backlog,
  navigate,
}: {
  backlog: Backlog
  navigate: ViewProps["navigate"]
}) {
  const { t } = useAppSettings()
  const items = [
    {
      key: "extraction",
      count: backlog.extraction,
      icon: Sparkles,
      open: () => navigate("posts", "queued-extraction"),
    },
    {
      key: "postRefresh",
      count: backlog.postRefresh,
      icon: stepIcons.x,
      open: () => navigate("posts", "queued-refresh"),
    },
    {
      key: "videoRefresh",
      count: backlog.videoRefresh,
      icon: stepIcons.youtube,
      open: () => navigate("videos", "queued"),
    },
    {
      key: "postTranslation",
      count: backlog.postTranslation,
      icon: Languages,
      open: () => navigate("posts", "queued-translation"),
    },
    {
      key: "videoTranslation",
      count: backlog.videoTranslation,
      icon: Languages,
      open: () => navigate("videos", "queued"),
    },
  ].filter((item) => item.count > 0)
  if (!items.length) return null
  return (
    <div className="adm-backlog">
      <h3 id="adm-backlog-title">{t("admin.backlog.title")}</h3>
      <div
        className="adm-queue"
        role="list"
        aria-labelledby="adm-backlog-title"
      >
        {items.map((item) => {
          const Icon = item.icon
          return (
            <button
              key={item.key}
              type="button"
              role="listitem"
              className="adm-source-row adm-backlog-row"
              onClick={item.open}
            >
              <span className="adm-source-icon" aria-hidden>
                <Icon />
              </span>
              <span className="adm-source-copy">
                <strong>
                  {t(`admin.backlog.${item.key}`, { count: item.count })}
                </strong>
                <small>{t(`admin.backlog.${item.key}.when`)}</small>
              </span>
              <ArrowRight aria-hidden />
            </button>
          )
        })}
      </div>
    </div>
  )
}

export function OverviewView({ navigate }: ViewProps) {
  const { t } = useAppSettings()
  const { overview, start, act, api, refreshOverview } = useAdmin()
  if (!overview) return <Loading />

  const { review, counts, activeRun, scheduler } = overview
  const queue = overview.queue || []
  // Each item opens its Review queue tab; the schedule's three share one.
  const attention = [
    {
      key: "images",
      tab: "schedule",
      count: review.pendingImages,
      icon: ImageOff,
      tone: "honey",
    },
    {
      key: "cancellations",
      tab: "schedule",
      count: review.suspectedCancellations,
      icon: CalendarClock,
      tone: "berry",
    },
    {
      key: "unlisted",
      tab: "schedule",
      count: review.unlistedEvents,
      icon: CalendarMinus,
      tone: "berry",
    },
    {
      key: "uncertain",
      tab: "schedule",
      count: review.uncertainEvents,
      icon: CalendarSearch,
      tone: "sky",
    },
    {
      key: "merges",
      tab: "merges",
      count: review.videoMerges,
      icon: Combine,
      tone: "lavender",
    },
    {
      key: "forms",
      tab: "forms",
      count: review.bilibiliForms,
      icon: Combine,
      tone: "honey",
    },
    {
      key: "posts",
      tab: "posts",
      count: review.failedPosts,
      icon: FileWarning,
      tone: "danger",
    },
  ].filter((item) => item.count > 0)

  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.overview.title")}
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

      <Panel title={t("admin.overview.attention")}>
        {attention.length ? (
          <div className="adm-attention">
            {attention.map((item) => {
              const Icon = item.icon
              return (
                <button
                  key={item.key}
                  type="button"
                  className={`adm-attention-item adm-tone-${item.tone}`}
                  onClick={() => navigate("review", item.tab)}
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
      </Panel>

      <div className="adm-grid is-two">
        <Panel
          title={t("admin.overview.queue")}
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
          <div className="adm-now">
            {activeRun ? (
              <>
                <p className="adm-now-title">
                  <StatusBadge status={activeRun.status} />
                  <strong>{runTitle(activeRun, t)}</strong>
                </p>
                <RunSteps run={activeRun} />
                <p className="adm-muted">
                  {t("admin.overview.startedAt", {
                    time: japanStamp(
                      activeRun.startedAt || activeRun.createdAt,
                    ),
                  })}
                </p>
              </>
            ) : (
              <p className="adm-now-title">
                <Badge tone="neutral">{t("admin.overview.idle")}</Badge>
              </p>
            )}
            {!scheduler.enabled ? (
              <p className="adm-muted">{t("admin.timer.schedulerOff")}</p>
            ) : queue.length ? (
              <ol className="adm-queue" aria-label={t("admin.queue.label")}>
                {queue.map((entry) => (
                  <QueueRow
                    key={entry.kind === "source" ? entry.source : entry.kind}
                    entry={entry}
                    busy={Boolean(activeRun)}
                  />
                ))}
              </ol>
            ) : (
              <p className="adm-muted">{t("admin.overview.noSchedule")}</p>
            )}
            {overview.backlog ? (
              <BacklogList backlog={overview.backlog} navigate={navigate} />
            ) : null}
            <div className="adm-now-links">
              <Button
                size="sm"
                variant="ghost"
                icon={CalendarClock}
                onClick={() => navigate("sources")}
              >
                {t("admin.overview.openTimers")}
              </Button>
              <Button
                size="sm"
                variant="ghost"
                icon={ListOrdered}
                onClick={() => navigate("runs", "next")}
              >
                {t("admin.overview.openQueue")}
              </Button>
            </div>
          </div>
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
            {overview.sources.x_related ? (
              <SourceRow id="x_related" health={overview.sources.x_related} />
            ) : null}
            <SourceRow id="youtube" health={overview.sources.youtube} />
            <SourceRow id="bilibili" health={overview.sources.bilibili} />
            {overview.sources.bilibili_dynamics ? (
              <SourceRow
                id="bilibili_dynamics"
                health={overview.sources.bilibili_dynamics}
              />
            ) : null}
            {overview.sources.shop ? (
              <SourceRow id="shop" health={overview.sources.shop} />
            ) : null}
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
              <RunLine
                key={run.id}
                run={run}
                href={`#runs/${encodeURIComponent(run.id)}`}
                onFollow={() => navigate("runs", run.id)}
              />
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
