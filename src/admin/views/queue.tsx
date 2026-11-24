import type { ReactNode } from "react"
import {
  ArrowUpRight,
  Clock,
  ExternalLink,
  Info,
  ListOrdered,
  RefreshCw,
  TriangleAlert,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { japanStamp } from "@/admin/format"
import type {
  RefreshQueueEntry,
  ScheduleQueueEntry,
  TranslationQueueEntry,
  YoutubeQueueEntry,
} from "@/admin/types"
import {
  Badge,
  Button,
  Callout,
  Empty,
  Facts,
  Loading,
  Panel,
  type Tone,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import type { ViewProps } from "@/admin"
import { explainCode } from "@/admin/views/run-parts"
import type { Translator } from "@/types"

const tabs = ["schedule", "youtube", "translate", "x-refresh"] as const
type Tab = (typeof tabs)[number]

const reasonTones: Record<string, Tone> = {
  requested: "berry",
  new: "sky",
  changed: "lavender",
  backoffDue: "honey",
}

/**
 * The shared head of a tab: its figures and, on the same row, a way to
 * recompute them.
 */
function TabHead({
  facts,
  loading,
  onRefresh,
}: {
  facts: [ReactNode, ReactNode][]
  loading: boolean
  onRefresh: () => void
}) {
  const { t } = useAppSettings()
  return (
    <div className="adm-qp-head">
      <Facts items={facts} />
      <Button
        size="sm"
        variant="ghost"
        icon={RefreshCw}
        busy={loading}
        onClick={onRefresh}
      >
        {t("admin.queuePreview.refresh")}
      </Button>
    </div>
  )
}

function Position({ index }: { index: number }) {
  return (
    <span className="adm-qp-position" aria-hidden>
      {index + 1}
    </span>
  )
}

function skipLabel(code: string, t: Translator) {
  const key = `admin.queuePreview.skip.${code}`
  const text = t(key)
  return text === key ? explainCode(code, t) || code : text
}

function ScheduleRow({
  entry,
  index,
  navigate,
}: {
  entry: ScheduleQueueEntry
  index: number
  navigate: ViewProps["navigate"]
}) {
  const { t } = useAppSettings()
  return (
    <li className="adm-qp-row">
      <Position index={index} />
      <div className="adm-qp-copy">
        <p className="adm-qp-tags">
          {entry.outcome === "call" ? (
            <Badge tone={reasonTones[entry.reason] || "neutral"}>
              {t(`admin.queuePreview.reason.${entry.reason}`)}
            </Badge>
          ) : (
            <Badge tone="warning" icon={TriangleAlert}>
              {t("admin.queuePreview.skipped", {
                reason: skipLabel(entry.reason, t),
              })}
            </Badge>
          )}
          <Badge>{t(`admin.queuePreview.route.${entry.route}`)}</Badge>
          <Badge>{t(`admin.queuePreview.source.${entry.source}`)}</Badge>
        </p>
        <p className="adm-qp-text">
          {entry.excerpt || t("admin.queuePreview.noText")}
        </p>
        <p className="adm-qp-meta">
          {entry.publishedAt ? (
            <span>
              {t("admin.queuePreview.published", {
                time: japanStamp(entry.publishedAt),
              })}
            </span>
          ) : null}
          {entry.lastAttemptAt ? (
            <span>
              {t("admin.queuePreview.lastAttempt", {
                time: japanStamp(entry.lastAttemptAt),
              })}
            </span>
          ) : null}
        </p>
      </div>
      <div className="adm-qp-actions">
        <Button
          size="sm"
          variant="ghost"
          icon={ArrowUpRight}
          onClick={() => navigate("posts", `find-${entry.postId}`)}
        >
          {t("admin.queuePreview.openPost")}
        </Button>
      </div>
    </li>
  )
}

function ScheduleTab({ navigate }: { navigate: ViewProps["navigate"] }) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const resource = useResource(() => api.scheduleQueue(), [])
  const queue = resource.data?.queue
  if (!queue) return resource.loading ? <Loading /> : null
  const stages = [
    queue.boardEnabled ? t("admin.queuePreview.route.board") : null,
    queue.messageEnabled ? t("admin.queuePreview.route.message") : null,
  ].filter(Boolean)
  return (
    <div className="adm-qp-tab">
      <TabHead
        facts={[
          [
            t("admin.queuePreview.schedule.stages"),
            stages.length ? stages.join(" · ") : t("admin.metric.no"),
          ],
          [
            t("admin.queuePreview.schedule.lookback"),
            queue.lookbackDays || t("admin.queuePreview.schedule.noLimit"),
          ],
          [t("admin.queuePreview.schedule.candidates"), queue.candidates],
          [t("admin.queuePreview.schedule.requested"), queue.requested],
          [t("admin.queuePreview.schedule.postsRead"), queue.postsRead],
          [
            t("admin.queuePreview.schedule.candidateMs"),
            t("admin.queuePreview.ms", { ms: queue.candidateMs }),
          ],
        ]}
        loading={resource.loading}
        onRefresh={() => void resource.reload()}
      />
      {resource.data?.skipped ? (
        <Callout tone="warning" icon={TriangleAlert}>
          {t("admin.queuePreview.stepSkipped")}
        </Callout>
      ) : null}
      {queue.disabled ? (
        <Callout tone="warning" icon={TriangleAlert}>
          {t("admin.queuePreview.schedule.disabled")}
        </Callout>
      ) : null}
      <h3 className="adm-qp-title">
        {t("admin.queuePreview.schedule.thisRun", {
          count: queue.run.length,
          limit: queue.limit,
        })}
      </h3>
      {queue.run.length ? (
        <ol className="adm-qp-list">
          {queue.run.map((entry, index) => (
            <ScheduleRow
              key={entry.postId}
              entry={entry}
              index={index}
              navigate={navigate}
            />
          ))}
        </ol>
      ) : (
        <Empty
          icon={ListOrdered}
          title={t("admin.queuePreview.schedule.none")}
        />
      )}
      <h3 className="adm-qp-title">{t("admin.queuePreview.schedule.rest")}</h3>
      <Facts
        items={[
          [t("admin.queuePreview.schedule.later"), queue.later],
          [t("admin.queuePreview.schedule.cached"), queue.cached],
          [
            t("admin.queuePreview.schedule.backoff"),
            <>
              {queue.backoff.count}
              {queue.backoff.nextAt ? (
                <span className="adm-muted">
                  {t("admin.queuePreview.schedule.nextAt", {
                    time: japanStamp(queue.backoff.nextAt),
                  })}
                </span>
              ) : null}
            </>,
          ],
          [
            t("admin.queuePreview.schedule.retryStopped"),
            queue.retryStopped.count,
          ],
        ]}
      />
    </div>
  )
}

const groupTones: Record<YoutubeQueueEntry["group"], Tone> = {
  refresh: "berry",
  queue: "sky",
  active: "leaf",
  followUp: "lavender",
}

function YoutubeTab() {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const resource = useResource(() => api.youtubeQueue(), [])
  const queue = resource.data?.queue
  if (!queue) return resource.loading ? <Loading /> : null
  return (
    <div className="adm-qp-tab">
      <TabHead
        facts={[
          [t("admin.queuePreview.youtube.watchPages"), queue.watchPages],
          [
            t("admin.queuePreview.youtube.apiVideos"),
            queue.api ? queue.apiVideos : t("admin.queuePreview.youtube.noApi"),
          ],
          [t("admin.queuePreview.youtube.group.refresh"), queue.counts.refresh],
          [t("admin.queuePreview.youtube.group.queue"), queue.counts.queue],
          [t("admin.queuePreview.youtube.group.active"), queue.counts.active],
          [
            t("admin.queuePreview.youtube.group.followUp"),
            queue.counts.followUp,
          ],
        ]}
        loading={resource.loading}
        onRefresh={() => void resource.reload()}
      />
      {queue.skipped ? (
        <Callout tone="warning" icon={TriangleAlert}>
          {t("admin.queuePreview.stepSkipped")}
        </Callout>
      ) : null}
      <Callout icon={Info}>
        {t("admin.queuePreview.youtube.discovered")}
      </Callout>
      {queue.entries.length ? (
        <ol className="adm-qp-list">
          {queue.entries.map((entry, index) => (
            <li key={entry.id} className="adm-qp-row">
              <Position index={index} />
              <div className="adm-qp-copy">
                <p className="adm-qp-tags">
                  <Badge tone={groupTones[entry.group]}>
                    {t(`admin.queuePreview.youtube.group.${entry.group}`)}
                  </Badge>
                  {entry.upcoming ? (
                    <Badge tone="running" icon={Clock}>
                      {t("admin.queuePreview.youtube.upcoming")}
                    </Badge>
                  ) : null}
                </p>
                <p className="adm-qp-text">
                  {entry.title || t("admin.queuePreview.youtube.unknown")}
                </p>
                <p className="adm-qp-meta">
                  <code>{entry.id}</code>
                  {entry.scheduledAt || entry.publishedAt ? (
                    <span>
                      {japanStamp(entry.scheduledAt || entry.publishedAt)}
                    </span>
                  ) : null}
                </p>
              </div>
              <div className="adm-qp-actions">
                <a
                  className="adm-icon-button"
                  href={entry.url}
                  target="_blank"
                  rel="noreferrer"
                  aria-label={t("admin.queuePreview.openVideo")}
                  title={t("admin.queuePreview.openVideo")}
                >
                  <ExternalLink aria-hidden />
                </a>
              </div>
            </li>
          ))}
        </ol>
      ) : (
        <Empty
          icon={ListOrdered}
          title={t("admin.queuePreview.youtube.none")}
        />
      )}
    </div>
  )
}

function TranslateRow({
  entry,
  index,
  limit,
  navigate,
}: {
  entry: TranslationQueueEntry
  index: number
  limit: number
  navigate: ViewProps["navigate"]
}) {
  const { t } = useAppSettings()
  return (
    <li className="adm-qp-row">
      <Position index={index} />
      <div className="adm-qp-copy">
        <p className="adm-qp-tags">
          {entry.requested ? (
            <Badge tone="berry">
              {t("admin.queuePreview.reason.requested")}
            </Badge>
          ) : null}
          <Badge>{t(`admin.queuePreview.owner.${entry.ownerType}`)}</Badge>
          <Badge>{t(`admin.queuePreview.source.${entry.source}`)}</Badge>
          {index >= limit ? (
            <Badge tone="neutral">
              {t("admin.queuePreview.translate.ifSkips")}
            </Badge>
          ) : null}
        </p>
        <p className="adm-qp-text">{entry.excerpt}</p>
      </div>
      <div className="adm-qp-actions">
        {entry.ownerType === "post" ? (
          <Button
            size="sm"
            variant="ghost"
            icon={ArrowUpRight}
            onClick={() => navigate("posts", `find-${entry.ownerId}`)}
          >
            {t("admin.queuePreview.openPost")}
          </Button>
        ) : (
          <code className="adm-qp-id">{entry.ownerId}</code>
        )}
      </div>
    </li>
  )
}

function TranslateTab({ navigate }: { navigate: ViewProps["navigate"] }) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const resource = useResource(() => api.translationQueue(), [])
  const queue = resource.data?.queue
  if (!queue) return resource.loading ? <Loading /> : null
  return (
    <div className="adm-qp-tab">
      <TabHead
        facts={[
          [t("admin.queuePreview.translate.limit"), queue.limit],
          [t("admin.queuePreview.translate.lookback"), queue.lookbackDays],
          [
            t("admin.queuePreview.translate.candidates"),
            queue.candidates.length,
          ],
        ]}
        loading={resource.loading}
        onRefresh={() => void resource.reload()}
      />
      {resource.data?.skipped ? (
        <Callout tone="warning" icon={TriangleAlert}>
          {t("admin.queuePreview.stepSkipped")}
        </Callout>
      ) : null}
      {queue.disabled ? (
        <Callout tone="warning" icon={TriangleAlert}>
          {t("admin.queuePreview.translate.disabled", {
            reason: skipLabel(queue.reason || "", t),
          })}
        </Callout>
      ) : null}
      {queue.candidates.length ? (
        <ol className="adm-qp-list">
          {queue.candidates.map((entry, index) => (
            <TranslateRow
              key={`${entry.ownerType}:${entry.ownerId}`}
              entry={entry}
              index={index}
              limit={queue.limit}
              navigate={navigate}
            />
          ))}
        </ol>
      ) : (
        <Empty
          icon={ListOrdered}
          title={t("admin.queuePreview.translate.none")}
        />
      )}
    </div>
  )
}

const refreshTones: Record<RefreshQueueEntry["status"], Tone> = {
  next: "sky",
  later: "neutral",
  notRead: "warning",
}

function RefreshTab({ navigate }: { navigate: ViewProps["navigate"] }) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const resource = useResource(() => api.refreshQueue(), [])
  const queue = resource.data?.queue
  if (!queue) return resource.loading ? <Loading /> : null
  return (
    <div className="adm-qp-tab">
      <TabHead
        facts={[
          [t("admin.queuePreview.refresh.total"), queue.total],
          [t("admin.queuePreview.refresh.detailBudget"), queue.detailBudget],
          [
            t("admin.queuePreview.refresh.handles"),
            queue.handles.length
              ? queue.handles.map((handle) => `@${handle}`).join(", ")
              : "—",
          ],
        ]}
        loading={resource.loading}
        onRefresh={() => void resource.reload()}
      />
      {queue.skipped ? (
        <Callout tone="warning" icon={TriangleAlert}>
          {t("admin.queuePreview.stepSkipped")}
        </Callout>
      ) : null}
      {queue.total > queue.entries.length ? (
        <p className="adm-muted">
          {t("admin.queuePreview.refresh.truncated", {
            shown: queue.entries.length,
            total: queue.total,
          })}
        </p>
      ) : null}
      {queue.entries.length ? (
        <ol className="adm-qp-list">
          {queue.entries.map((entry, index) => (
            <li key={entry.postId} className="adm-qp-row">
              <Position index={index} />
              <div className="adm-qp-copy">
                <p className="adm-qp-tags">
                  <Badge tone={refreshTones[entry.status]}>
                    {t(`admin.queuePreview.refresh.status.${entry.status}`)}
                  </Badge>
                  <Badge>@{entry.accountHandle}</Badge>
                </p>
                <p className="adm-qp-text">
                  {entry.text || t("admin.queuePreview.noText")}
                </p>
                <p className="adm-qp-meta">
                  {t("admin.queuePreview.refresh.requestedAt", {
                    time: japanStamp(entry.requestedAt),
                  })}
                </p>
              </div>
              <div className="adm-qp-actions">
                <Button
                  size="sm"
                  variant="ghost"
                  icon={ArrowUpRight}
                  onClick={() => navigate("posts", `find-${entry.postId}`)}
                >
                  {t("admin.queuePreview.openPost")}
                </Button>
              </div>
            </li>
          ))}
        </ol>
      ) : (
        <Empty
          icon={ListOrdered}
          title={t("admin.queuePreview.refresh.none")}
        />
      )}
    </div>
  )
}

/**
 * What each step would work through, in order, if a run started now: the
 * Next tab of the Runs page, with the step in the address
 * (`#runs/next/<step>`). Every tab is computed when it opens or is
 * refreshed.
 */
export function QueuePanel({
  step,
  navigate,
}: {
  step: string
  navigate: ViewProps["navigate"]
}) {
  const { t } = useAppSettings()
  const active: Tab = (tabs as readonly string[]).includes(step)
    ? (step as Tab)
    : "schedule"
  return (
    <Panel>
      <div
        className="adm-tabs"
        role="tablist"
        aria-label={t("admin.queuePreview.title")}
      >
        {tabs.map((id) => (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={active === id}
            className={active === id ? "is-active" : undefined}
            onClick={() => navigate("runs", `next/${id}`)}
          >
            {t(`admin.queuePreview.tab.${id}`)}
          </button>
        ))}
      </div>
      <div role="tabpanel" className="adm-tab-panel">
        {active === "schedule" ? <ScheduleTab navigate={navigate} /> : null}
        {active === "youtube" ? <YoutubeTab /> : null}
        {active === "translate" ? <TranslateTab navigate={navigate} /> : null}
        {active === "x-refresh" ? <RefreshTab navigate={navigate} /> : null}
      </div>
    </Panel>
  )
}
