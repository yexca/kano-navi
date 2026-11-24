import { useId, useState, type ReactNode } from "react"
import {
  Download,
  Gauge,
  Info,
  ListEnd,
  Lock,
  Play,
  RotateCw,
  ShieldAlert,
  SlidersHorizontal,
  Tv,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { RelatedAccountsPanel } from "@/admin/views/related-accounts"
import { formatInterval, formatRelative, japanStamp } from "@/admin/format"
import type {
  FetchSource,
  ScheduleSource,
  SourceSchedule,
  SyncSettings,
  YTDLPStatus,
} from "@/admin/types"
import {
  Badge,
  Button,
  Callout,
  Facts,
  Loading,
  PageHeader,
  StatusBadge,
  Switch,
  type Tone,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import {
  BilibiliAddDynamic,
  BilibiliAddUpload,
  BilibiliLoginBadge,
  BilibiliLoginNote,
} from "@/admin/views/bilibili-parts"
import { stepIcons } from "@/admin/views/run-parts"
import { ShopStatusPanel, WebBotAuthPanel } from "@/admin/views/shop-parts"
import { SyncSettingsForm } from "@/admin/views/sync-settings"
import { cn } from "@/lib/utils"
import { Fold } from "@/admin/views/source-parts"
import { TimedReadsPanel, XAPIPanel } from "@/admin/views/source-timed"
import {
  RelatedFetchPanel,
  ShopFetchPanel,
  FetchPanel,
  YoutubeAddVideo,
} from "@/admin/views/source-fetch"
import { ConfirmationPanel, BilibiliLivePanel } from "@/admin/views/source-live"

const skipFlags: Record<ScheduleSource, string> = {
  x: "SKIP_X",
  x_related: "SKIP_X",
  youtube: "SKIP_YOUTUBE",
  bilibili: "SKIP_BILIBILI",
  bilibili_dynamics: "SKIP_BILIBILI",
  shop: "SKIP_SHOP",
}

/** Both Bilibili timers: bounded by BILIBILI_MIN_INTERVAL_MINUTES and
 * sharing one captcha backoff. */
const isBilibili = (
  source: ScheduleSource,
): source is "bilibili" | "bilibili_dynamics" =>
  source === "bilibili" || source === "bilibili_dynamics"

/** The sources with listing methods and read settings. */
const isListing = (source: ScheduleSource): source is "x" | "youtube" =>
  source === "x" || source === "youtube"

/** The sources whose manual fetch form names a method and a selection. */
const isFetchable = (source: ScheduleSource): source is FetchSource =>
  isListing(source) || isBilibili(source)

/** A relative time with its full date as a tooltip, or a dash. */
function When({ value }: { value: string | null | undefined }) {
  const { locale } = useAppSettings()
  return value ? (
    <time dateTime={value} title={japanStamp(value)}>
      {formatRelative(value, locale)}
    </time>
  ) : (
    <>—</>
  )
}

type TimerState = "locked" | "running" | "blocked" | "off"

/** Whether a source's timer runs in this process. */
function timerState(schedule: SourceSchedule, timersOff: boolean): TimerState {
  if (schedule.skipped) return "locked"
  if (!schedule.enabled) return "off"
  return timersOff ? "blocked" : "running"
}

const timerTones: Record<TimerState, Tone> = {
  locked: "neutral",
  running: "leaf",
  blocked: "honey",
  off: "neutral",
}

/** A timer state as its tab and summary name it. */
function timerLabel(
  schedule: SourceSchedule,
  state: TimerState,
  t: ReturnType<typeof useAppSettings>["t"],
) {
  return state === "locked"
    ? t("admin.sources.locked")
    : state === "running"
      ? t("admin.timer.on", {
          interval: formatInterval(schedule.intervalMinutes, t),
        })
      : state === "blocked"
        ? t("admin.sources.timerBlocked")
        : t("admin.sources.timerOff")
}

/** A source's tab: its name and a dot for whether its timer runs here. */
function SourceTab({
  schedule,
  selected,
  onSelect,
}: {
  schedule: SourceSchedule
  selected: boolean
  onSelect: () => void
}) {
  const { t } = useAppSettings()
  const { overview } = useAdmin()
  const source = schedule.source
  const Icon = stepIcons[source] || Tv
  const state = timerState(
    schedule,
    overview ? !overview.scheduler.enabled : false,
  )
  return (
    <button
      type="button"
      role="tab"
      aria-selected={selected}
      className={cn(selected && "is-active", state === "locked" && "is-locked")}
      onClick={onSelect}
      title={timerLabel(schedule, state, t)}
    >
      <span className="adm-source-icon">
        <Icon aria-hidden />
      </span>
      <strong>{t(`admin.step.${source}`)}</strong>
      {state === "locked" ? (
        <Lock aria-hidden className="adm-source-lock" />
      ) : (
        <i className={cn("adm-source-dot", `is-${state}`)} aria-hidden />
      )}
      <span className="adm-visually-hidden">
        {timerLabel(schedule, state, t)}
      </span>
    </button>
  )
}

/**
 * The selected source at a glance: what it reads, whether its timer runs,
 * when it runs next, how the last run went and its last successful read,
 * with the timer switch and Run now. The switch saves at once and keeps any
 * unsaved timed-read edits below.
 */
function SourceSummary({
  schedule,
  lastSuccess,
  chips,
  notes,
  onChange,
}: {
  schedule: SourceSchedule
  lastSuccess: string | null | undefined
  chips?: ReactNode
  /** Notes under the locked note, such as a missing login. */
  notes?: ReactNode
  onChange: (schedule: SourceSchedule) => void
}) {
  const { t } = useAppSettings()
  const { api, act, start, overview } = useAdmin()
  const [busy, setBusy] = useState("")
  const id = useId()
  const source = schedule.source
  const Icon = stepIcons[source] || Tv
  const locked = schedule.skipped
  const state = timerState(
    schedule,
    overview ? !overview.scheduler.enabled : false,
  )
  const toggle = async (enabled: boolean) => {
    setBusy("toggle")
    const saved = await act(
      () => api.updateSourceSchedule(source, { enabled }),
      "admin.notice.scheduleSaved",
    )
    setBusy("")
    if (saved) onChange(saved)
  }
  // A running timer is started itself, as on the overview queue: its run
  // plans the next one, and while another run holds the queue it runs next.
  // A source whose timer is off runs its steps once.
  const timed = state === "running"
  const queued = Boolean(overview?.activeRun)
  const runNow = async () => {
    setBusy("run")
    if (timed) {
      const saved = await act(
        () => api.startSourceSchedule(source),
        queued ? "admin.notice.timerNext" : "admin.notice.timerStarted",
      )
      if (saved) onChange(saved)
    } else await start({ kind: "source", source })
    setBusy("")
  }
  return (
    <section className="adm-panel adm-source-summary" aria-labelledby={id}>
      <header className="adm-source-summary-head">
        <span className="adm-source-icon">
          <Icon aria-hidden />
        </span>
        <div className="adm-source-summary-title">
          <h2 id={id}>{t(`admin.step.${source}`)}</h2>
        </div>
        <div className="adm-source-summary-actions">
          <Switch
            checked={schedule.enabled}
            disabled={locked || busy !== ""}
            onChange={toggle}
            label={t("admin.timer.enable")}
          />
          <Button
            size="sm"
            icon={timed && queued ? ListEnd : Play}
            busy={busy === "run"}
            disabled={locked || busy !== "" || (!timed && queued)}
            onClick={runNow}
          >
            {t(timed && queued ? "admin.queue.runNext" : "admin.timer.runNow")}
          </Button>
        </div>
      </header>
      {locked ? (
        <p className="adm-source-note">
          <Lock aria-hidden />
          {t("admin.timer.skipped", { flag: skipFlags[source] })}
        </p>
      ) : null}
      {notes}
      <dl className="adm-source-stats">
        <div>
          <dt>{t("admin.sources.timer")}</dt>
          <dd>
            <Badge tone={timerTones[state]}>
              {state === "locked" ? (
                <Lock aria-hidden />
              ) : (
                <i
                  className={cn("adm-source-dot", `is-${state}`)}
                  aria-hidden
                />
              )}
              {timerLabel(schedule, state, t)}
            </Badge>
            {chips}
          </dd>
        </div>
        <div>
          <dt>{t("admin.timer.nextRun")}</dt>
          <dd>
            {state === "running" ? <When value={schedule.nextRunAt} /> : "—"}
          </dd>
        </div>
        <div>
          <dt>{t("admin.timer.lastRun")}</dt>
          <dd>
            {schedule.lastRun ? (
              <>
                <StatusBadge status={schedule.lastRun.status} />
                <When value={schedule.lastRun.at} />
              </>
            ) : (
              t("admin.timer.never")
            )}
          </dd>
        </div>
        <div>
          <dt>{t("admin.sources.lastSuccess")}</dt>
          <dd>
            <When value={lastSuccess} />
          </dd>
        </div>
      </dl>
    </section>
  )
}

function YTDLPPanel({
  status,
  onChange,
}: {
  status: YTDLPStatus | undefined
  onChange: (status: YTDLPStatus) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [busy, setBusy] = useState(false)
  const available = status?.available === true
  const update = async () => {
    setBusy(true)
    const next = await act(() => api.updateYTDLP(), "admin.notice.ytdlpUpdated")
    setBusy(false)
    if (next) onChange(next)
  }
  return (
    <Fold
      icon={Download}
      title={t("admin.ytdlp.title")}
      className="adm-panel"
      hint={status?.version || undefined}
      controls={
        status ? (
          <Badge tone={available ? "leaf" : "neutral"}>
            {available
              ? t("admin.ytdlp.available")
              : t("admin.ytdlp.unavailable")}
          </Badge>
        ) : null
      }
    >
      <div className="adm-form">
        <Facts
          items={[
            [
              t("admin.ytdlp.status"),
              available
                ? t("admin.ytdlp.available")
                : t("admin.ytdlp.unavailable"),
            ],
            [t("admin.ytdlp.version"), status?.version || null],
          ]}
        />
        <div className="adm-form-actions">
          <Button
            size="sm"
            icon={RotateCw}
            busy={busy}
            disabled={!available}
            onClick={update}
          >
            {t("admin.ytdlp.update")}
          </Button>
        </div>
      </div>
    </Fold>
  )
}

/**
 * One section of a source's read limits, folded: the bounds of its timed
 * reads, or of the manual fetches that X and YouTube share, one field per
 * row beside the manual fetch. Both sections edit the page's one read of the
 * settings.
 */
function LimitsPanel({
  source,
  section,
  settings,
  onSaved,
}: {
  source: "x" | "youtube"
  section: "timed" | "manual"
  settings: SyncSettings | undefined
  onSaved: (settings: SyncSettings) => void
}) {
  const { t } = useAppSettings()
  return (
    <Fold
      icon={section === "timed" ? Gauge : SlidersHorizontal}
      title={t(`admin.sources.limits.${section}`)}
      className="adm-panel"
      hint={section === "manual" ? t("admin.syncSettings.shared") : undefined}
    >
      {settings ? (
        <SyncSettingsForm
          group={source}
          section={section}
          data={settings}
          onSaved={onSaved}
          rows={section === "manual"}
        />
      ) : (
        <Loading />
      )}
    </Fold>
  )
}

const sourceIds: ScheduleSource[] = [
  "x",
  "x_related",
  "youtube",
  "bilibili",
  "bilibili_dynamics",
  "shop",
]

/**
 * The Sources page: a tab per source, then the selected source's summary
 * (timer state, runs, the timer switch and Run now). Below it the timed-read
 * settings fill the main column, followed by X's API limits or YouTube's
 * progressive confirmation, the timed-read limits and yt-dlp, each folded,
 * the related X accounts beside their timer, or the uploads' folded Bilibili
 * live room; a Bilibili summary shows the login cookie and the backoff. The manual actions sit beside them: the manual
 * fetch, a related account's fetch or the shop's read, then one YouTube
 * video, Bilibili upload or dynamic added by link, then the fetches' folded
 * limits for X and YouTube. On a narrower screen the side column follows
 * the main one.
 */
export function SourcesView({ tab, navigate }: ViewProps) {
  const { t } = useAppSettings()
  const { api, overview } = useAdmin()
  const sources = useResource(() => api.sources(), [])
  const ytdlp = useResource(() => api.ytdlp(), [])
  // One read for both limit sections: a second read of the same path would
  // supersede the first.
  const limits = useResource(() => api.syncSettings(), [])
  const data = sources.data
  const selected = sourceIds.includes(tab as ScheduleSource)
    ? (tab as ScheduleSource)
    : "x"
  // The related accounts' editor and fetch share one read, made on their
  // tab only.
  const relatedTab = selected === "x_related"
  const related = useResource(
    () => (relatedTab ? api.relatedAccounts() : Promise.resolve(undefined)),
    [relatedTab],
  )
  const timersOff = overview ? !overview.scheduler.enabled : false
  const setSchedule = (next: SourceSchedule) =>
    data &&
    sources.setData({
      ...data,
      schedules: data.schedules.map((entry) =>
        entry.source === next.source ? next : entry,
      ),
    })
  const schedule = (source: ScheduleSource) =>
    data?.schedules.find((entry) => entry.source === source)
  const blockedUntil =
    data?.bilibili.blockedUntil &&
    Date.parse(data.bilibili.blockedUntil) > Date.now()
      ? data.bilibili.blockedUntil
      : null
  const current = schedule(selected)
  return (
    <div className="adm-view">
      <PageHeader title={t("admin.sources.title")} />
      {!data ? (
        <Loading />
      ) : (
        <>
          {timersOff ? (
            <Callout tone="honey" icon={Info}>
              <p>{t("admin.timer.schedulerOff")}</p>
            </Callout>
          ) : null}
          <div
            className="adm-tabs is-bare adm-source-tabs"
            role="tablist"
            aria-label={t("admin.sources.title")}
          >
            {sourceIds.map((source) => {
              const found = schedule(source)
              return found ? (
                <SourceTab
                  key={source}
                  schedule={found}
                  selected={selected === source}
                  onSelect={() => navigate("sources", source)}
                />
              ) : null
            })}
          </div>
          {current ? (
            <div
              role="tabpanel"
              className="adm-source-page"
              aria-label={t(`admin.step.${selected}`)}
            >
              <SourceSummary
                key={selected}
                schedule={current}
                lastSuccess={
                  selected === "bilibili"
                    ? data.bilibili.lastSuccessAt
                    : selected === "bilibili_dynamics"
                      ? data.bilibili.dynamics?.lastSuccessAt
                      : overview?.sources[selected].lastSuccessAt
                }
                chips={
                  isBilibili(selected) ? (
                    <>
                      <BilibiliLoginBadge status={data.bilibili} />
                      {blockedUntil ? (
                        <Badge tone="danger" icon={ShieldAlert}>
                          {t("admin.bilibili.blockedUntil", {
                            time: japanStamp(blockedUntil),
                          })}
                        </Badge>
                      ) : null}
                    </>
                  ) : null
                }
                notes={
                  isBilibili(selected) ? (
                    <BilibiliLoginNote
                      status={data.bilibili}
                      dynamics={selected === "bilibili_dynamics"}
                    />
                  ) : null
                }
                onChange={setSchedule}
              />
              <div className="adm-sources-layout">
                <div className="adm-source-main">
                  <TimedReadsPanel
                    key={selected}
                    schedule={current}
                    onChange={setSchedule}
                  />
                  {selected === "x" ? (
                    <XAPIPanel
                      schedule={current}
                      accounts={data.x.accounts.map((entry) => entry.account)}
                      onChange={setSchedule}
                    />
                  ) : null}
                  {selected === "x_related" ? (
                    <RelatedAccountsPanel
                      related={related.data}
                      onSaved={related.setData}
                      navigate={navigate}
                    />
                  ) : null}
                  {selected === "youtube" ? (
                    <ConfirmationPanel
                      youtube={data.youtube}
                      onChange={(youtube) =>
                        sources.setData({ ...data, youtube })
                      }
                    />
                  ) : null}
                  {selected === "shop" ? (
                    <ShopStatusPanel shop={data.shop} />
                  ) : null}
                  {selected === "shop" ? (
                    <WebBotAuthPanel
                      status={data.shop.webBotAuth}
                      onChange={(webBotAuth) => {
                        sources.setData({
                          ...data,
                          shop: { ...data.shop, webBotAuth },
                        })
                        // The chain's signed method follows the key.
                        void sources.reload()
                      }}
                    />
                  ) : null}
                  {selected === "bilibili" &&
                  data.youtube.liveProbe.bilibili ? (
                    <BilibiliLivePanel
                      probe={data.youtube.liveProbe.bilibili}
                      live={data.bilibili.live}
                      onChange={(liveProbe) =>
                        sources.setData({
                          ...data,
                          youtube: { ...data.youtube, liveProbe },
                        })
                      }
                    />
                  ) : null}
                  {isListing(selected) ? (
                    <LimitsPanel
                      key={`${selected}-timed`}
                      source={selected}
                      section="timed"
                      settings={limits.data}
                      onSaved={limits.setData}
                    />
                  ) : null}
                  {selected === "youtube" ? (
                    <YTDLPPanel
                      status={ytdlp.data}
                      onChange={(next) => ytdlp.setData(next)}
                    />
                  ) : null}
                </div>
                <div className="adm-source-aside">
                  {selected === "x_related" ? (
                    <RelatedFetchPanel
                      related={related.data}
                      skipped={current.skipped}
                      onOpenRuns={() => navigate("runs")}
                    />
                  ) : selected === "shop" ? (
                    <ShopFetchPanel
                      sources={data}
                      onOpenRuns={() => navigate("runs")}
                    />
                  ) : isFetchable(selected) ? (
                    <FetchPanel
                      key={selected}
                      source={selected}
                      sources={data}
                      onOpenRuns={() => navigate("runs")}
                    />
                  ) : null}
                  {selected === "youtube" ? (
                    <YoutubeAddVideo
                      youtube={data.youtube}
                      onAdded={() => void sources.reload()}
                    />
                  ) : null}
                  {selected === "bilibili" ? (
                    <BilibiliAddUpload
                      status={data.bilibili}
                      onAdded={() => void sources.reload()}
                    />
                  ) : null}
                  {selected === "bilibili_dynamics" ? (
                    <BilibiliAddDynamic
                      status={data.bilibili}
                      onAdded={() => void sources.reload()}
                    />
                  ) : null}
                  {isListing(selected) ? (
                    <LimitsPanel
                      key={`${selected}-manual`}
                      source={selected}
                      section="manual"
                      settings={limits.data}
                      onSaved={limits.setData}
                    />
                  ) : null}
                </div>
              </div>
            </div>
          ) : null}
        </>
      )}
    </div>
  )
}
