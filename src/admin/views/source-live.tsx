import { useEffect, useId, useState, type ReactNode } from "react"
import {
  CalendarCheck,
  CircleStop,
  ListChecks,
  Play,
  Radio,
  RadioTower,
  RotateCw,
  Save,
  type LucideIcon,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { formatInterval, formatRelative, japanStamp } from "@/admin/format"
import type { LiveProbeStatus, PushStatus, Sources } from "@/admin/types"
import {
  Badge,
  Button,
  ConfirmButton,
  Facts,
  Field,
  Panel,
  Switch,
} from "@/admin/ui"
import { useAdmin } from "@/admin/use-admin-data"
import { LiveRoomFacts, LiveRoomState } from "@/admin/views/bilibili-parts"
import { Fold } from "@/admin/views/source-parts"

/** When a finished YouTube video is reread, in minutes after its anchor
 * (ytsource.FollowUpOffsets). */
const followUpOffsets = [60, 1440, 4320, 10080]

/** A step of YouTube's progressive confirmation without settings. */
function Stage({
  icon: StageIcon,
  title,
  badge,
  children,
}: {
  icon: LucideIcon
  title: string
  badge: ReactNode
  children: ReactNode
}) {
  const id = useId()
  return (
    <li className="adm-confirm-step">
      <section aria-labelledby={id}>
        <header className="adm-confirm-head">
          <span className="adm-confirm-mark">
            <StageIcon aria-hidden />
          </span>
          <div className="adm-confirm-title">
            <h4 id={id}>{title}</h4>
          </div>
          {badge}
        </header>
        {children}
      </section>
    </li>
  )
}

/** The deployment address a stored callback URL starts with. */
function callbackOrigin(url: string | null | undefined, path: string) {
  if (!url) return ""
  return url.endsWith(path) ? url.slice(0, -path.length) : url
}

/**
 * The callback URL for an entered deployment address: HTTPS unless another
 * scheme is given, without trailing slashes, ending in the callback path.
 * Nothing entered clears it.
 */
function callbackUrl(input: string, path: string) {
  let origin = input.trim().replace(/\/+$/u, "")
  if (!origin) return null
  if (origin.endsWith(path)) origin = origin.slice(0, -path.length)
  if (!/^[a-z][a-z0-9+.-]*:\/\//iu.test(origin)) origin = `https://${origin}`
  return `${origin}${path}`
}

function PushStage({
  push,
  onChange,
}: {
  push: PushStatus
  onChange: (push: PushStatus) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const path = push.callbackPath
  const saved = callbackOrigin(push.callbackUrl, path)
  // The field holds the deployment address; the callback path is added on
  // save.
  const [callback, setCallback] = useState(saved)
  const [busy, setBusy] = useState("")
  useEffect(() => setCallback(saved), [saved])
  const url = callbackUrl(callback, path)
  const save = async (body: {
    enabled?: boolean
    callbackUrl?: string | null
  }) => {
    setBusy(body.enabled === undefined ? "save" : "toggle")
    const next = await act(() => api.updatePush(body), "admin.notice.pushSaved")
    setBusy("")
    if (next) onChange(next)
  }
  return (
    <li className="adm-confirm-step">
      <Fold
        icon={Radio}
        level={4}
        title={t("admin.push.title")}
        controls={
          <>
            {push.enabled ? (
              <Badge tone={push.verified ? "leaf" : "honey"}>
                {t(
                  push.verified
                    ? "admin.overview.pushVerified"
                    : "admin.overview.pushPending",
                )}
              </Badge>
            ) : null}
            <Switch
              compact
              checked={push.enabled}
              disabled={!push.available || busy !== ""}
              onChange={(enabled) => save({ enabled, callbackUrl: url })}
              label={t("admin.push.enable")}
            />
          </>
        }
      >
        <div className="adm-form">
          {push.available ? null : (
            <p className="adm-field-hint">{t("admin.push.unavailable")}</p>
          )}
          <Field label={t("admin.push.callback")}>
            <input
              type="text"
              inputMode="url"
              value={callback}
              onChange={(e) => setCallback(e.target.value)}
              placeholder={t("admin.push.callbackPlaceholder")}
              spellCheck={false}
              autoCapitalize="off"
            />
          </Field>
          <div className="adm-form-actions">
            <ConfirmButton
              size="sm"
              variant="ghost"
              icon={RotateCw}
              disabled={!push.secretConfigured}
              onConfirm={async () => {
                const next = await act(
                  () => api.rotatePushSecret(),
                  "admin.notice.secretRotated",
                )
                if (next) onChange(next)
              }}
            >
              {t("admin.push.rotate")}
            </ConfirmButton>
            <Button
              size="sm"
              icon={Save}
              busy={busy === "save"}
              disabled={url === (push.callbackUrl || null)}
              onClick={() => save({ callbackUrl: url })}
            >
              {t("admin.action.save")}
            </Button>
          </div>
          <Facts
            items={[
              [
                t("admin.push.secret"),
                t(
                  push.secretConfigured
                    ? "admin.push.secretSet"
                    : "admin.push.secretUnset",
                ),
              ],
              [t("admin.push.lease"), japanStamp(push.leaseExpiresAt) || null],
              [t("admin.push.renewal"), japanStamp(push.nextRenewalAt) || null],
              [
                t("admin.push.notified"),
                japanStamp(push.lastNotificationAt) || null,
              ],
              [
                t("admin.push.triggered"),
                japanStamp(push.lastTriggeredAt) || null,
              ],
              [t("admin.push.lastError"), push.lastError],
            ]}
          />
        </div>
      </Fold>
    </li>
  )
}

/**
 * The Bilibili live-room check on the Bilibili tab, folded like YouTube's
 * probe: the room's state and the switch in the header, then the interval
 * saved through the live-probe settings, the stream events it follows and
 * the room's last check. It runs in the live-probe step on its own interval.
 * The board shows a Bilibili stream On air from this check.
 */
export function BilibiliLivePanel({
  probe,
  live,
  onChange,
}: {
  probe: NonNullable<LiveProbeStatus["bilibili"]>
  live: Sources["bilibili"]["live"]
  onChange: (probe: LiveProbeStatus) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const [minutes, setMinutes] = useState(probe.intervalMinutes)
  const [busy, setBusy] = useState("")
  useEffect(() => setMinutes(probe.intervalMinutes), [probe.intervalMinutes])
  const save = async (body: {
    bilibiliEnabled?: boolean
    bilibiliIntervalMinutes?: number
  }) => {
    setBusy(body.bilibiliEnabled === undefined ? "save" : "toggle")
    const next = await act(
      () => api.updateLiveProbe(body),
      "admin.notice.liveProbeSaved",
    )
    setBusy("")
    if (next) onChange(next)
  }
  return (
    <Fold
      icon={RadioTower}
      title={t("admin.liveProbe.bilibili.title")}
      className="adm-panel"
      controls={
        <>
          {live ? <LiveRoomState live={live} /> : null}
          {probe.enabled && !probe.available ? (
            <Badge tone="honey">{t("admin.liveProbe.notRunning")}</Badge>
          ) : null}
          <Switch
            compact
            checked={probe.enabled}
            disabled={!probe.available || busy !== ""}
            onChange={(bilibiliEnabled) => save({ bilibiliEnabled })}
            label={t("admin.liveProbe.bilibili.enable")}
          />
        </>
      }
    >
      <form
        className="adm-form"
        onSubmit={(event) => {
          event.preventDefault()
          save({ bilibiliIntervalMinutes: minutes })
        }}
      >
        <Field
          label={t("admin.liveProbe.interval")}
          hint={t("admin.liveProbe.intervalRange", {
            min: probe.minIntervalMinutes,
            max: probe.maxIntervalMinutes,
          })}
        >
          <input
            type="number"
            min={probe.minIntervalMinutes}
            max={probe.maxIntervalMinutes}
            step={1}
            value={minutes}
            onChange={(e) => setMinutes(Number(e.target.value))}
            required
          />
        </Field>
        <div className="adm-form-actions">
          <Button
            type="submit"
            size="sm"
            icon={Save}
            busy={busy === "save"}
            disabled={minutes === probe.intervalMinutes || busy !== ""}
          >
            {t("admin.action.save")}
          </Button>
        </div>
        <Facts
          items={[
            [
              t("admin.liveProbe.bilibili.nextCheck"),
              probe.nextCheckAt
                ? `${japanStamp(probe.nextCheckAt)} · ${formatRelative(probe.nextCheckAt, locale)}`
                : null,
            ],
          ]}
        />
      </form>
      {probe.targets.length ? (
        <ul className="adm-live-videos">
          {probe.targets.map((target) => (
            <li key={target.eventId}>
              <span className="adm-live-videos-name">
                <strong>{target.title}</strong>
                <small>
                  {t(`admin.liveProbe.phase.${target.phase}`)}
                  {" · "}
                  {japanStamp(target.startsAt)}
                  {" · "}
                  {Date.parse(target.dueAt) > Date.now()
                    ? t("admin.liveProbe.due", {
                        time: formatRelative(target.dueAt, locale),
                      })
                    : t("admin.liveProbe.dueNow")}
                </small>
              </span>
            </li>
          ))}
        </ul>
      ) : null}
      {live ? (
        <section className="adm-bilibili-part">
          <h4>{t("admin.bilibili.live.title")}</h4>
          <LiveRoomFacts live={live} />
        </section>
      ) : null}
    </Fold>
  )
}

function LiveProbeStage({
  probe,
  onChange,
}: {
  probe: LiveProbeStatus
  onChange: (probe: LiveProbeStatus) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const [minutes, setMinutes] = useState(probe.intervalMinutes)
  const [busy, setBusy] = useState("")
  useEffect(() => setMinutes(probe.intervalMinutes), [probe.intervalMinutes])
  const save = async (body: {
    enabled?: boolean
    intervalMinutes?: number
  }) => {
    setBusy(body.enabled === undefined ? "save" : "toggle")
    const next = await act(
      () => api.updateLiveProbe(body),
      "admin.notice.liveProbeSaved",
    )
    setBusy("")
    if (next) onChange(next)
  }
  const setStopped = async (id: string, stopped: boolean) => {
    setBusy(`video:${id}`)
    const next = await act(
      () => api.stopLiveProbeVideo(id, stopped),
      stopped
        ? "admin.notice.liveProbeStopped"
        : "admin.notice.liveProbeResumed",
    )
    setBusy("")
    if (next) onChange(next)
  }
  const running = probe.enabled && probe.available
  return (
    <li className="adm-confirm-step">
      <Fold
        icon={RadioTower}
        level={4}
        title={t("admin.liveProbe.title")}
        controls={
          <>
            {probe.enabled && !running ? (
              <Badge tone="honey">{t("admin.liveProbe.notRunning")}</Badge>
            ) : null}
            <Switch
              compact
              checked={probe.enabled}
              disabled={!probe.available || busy !== ""}
              onChange={(enabled) => save({ enabled })}
              label={t("admin.liveProbe.enable")}
            />
          </>
        }
      >
        <form
          className="adm-form"
          onSubmit={(event) => {
            event.preventDefault()
            save({ intervalMinutes: minutes })
          }}
        >
          <Field
            label={t("admin.liveProbe.interval")}
            hint={t("admin.liveProbe.intervalRange", {
              min: probe.minIntervalMinutes,
              max: probe.maxIntervalMinutes,
            })}
          >
            <input
              type="number"
              min={probe.minIntervalMinutes}
              max={probe.maxIntervalMinutes}
              step={1}
              value={minutes}
              onChange={(e) => setMinutes(Number(e.target.value))}
              required
            />
          </Field>
          <div className="adm-form-actions">
            <Button
              type="submit"
              size="sm"
              icon={Save}
              busy={busy === "save"}
              disabled={minutes === probe.intervalMinutes || busy !== ""}
            >
              {t("admin.action.save")}
            </Button>
          </div>
          <Facts
            items={[
              [
                t("admin.liveProbe.lastRun"),
                probe.lastRunAt
                  ? `${japanStamp(probe.lastRunAt)} · ${formatRelative(probe.lastRunAt, locale)}`
                  : null,
              ],
              [
                t("admin.liveProbe.nextRun"),
                probe.nextRunAt
                  ? `${japanStamp(probe.nextRunAt)} · ${formatRelative(probe.nextRunAt, locale)}`
                  : null,
              ],
              [
                t("admin.liveProbe.active"),
                t("admin.liveProbe.activeCount", {
                  count: probe.activeVideos,
                  max: probe.maxVideos,
                }),
              ],
            ]}
          />
        </form>
        {probe.videos.length || probe.stopped.length ? (
          <ul className="adm-live-videos">
            {probe.videos.map((video) => (
              <li key={video.id}>
                <span className="adm-live-videos-name">
                  <a href={video.url} target="_blank" rel="noreferrer">
                    {video.title}
                  </a>
                  <small>
                    {t(`admin.liveProbe.phase.${video.phase}`)}
                    {" · "}
                    {Date.parse(video.dueAt) > Date.now()
                      ? t("admin.liveProbe.due", {
                          time: formatRelative(video.dueAt, locale),
                        })
                      : t("admin.liveProbe.dueNow")}
                  </small>
                </span>
                <Button
                  size="sm"
                  variant="ghost"
                  icon={CircleStop}
                  busy={busy === `video:${video.id}`}
                  disabled={busy !== ""}
                  onClick={() => setStopped(video.id, true)}
                >
                  {t("admin.liveProbe.stop")}
                </Button>
              </li>
            ))}
            {probe.stopped.map((video) => (
              <li key={video.id} className="is-stopped">
                <span className="adm-live-videos-name">
                  <a href={video.url} target="_blank" rel="noreferrer">
                    {video.title}
                  </a>
                  <small>
                    {t(`admin.liveProbe.stopped.${video.reason}`)}
                    {" · "}
                    {japanStamp(video.stoppedAt)}
                  </small>
                </span>
                <Button
                  size="sm"
                  variant="ghost"
                  icon={Play}
                  busy={busy === `video:${video.id}`}
                  disabled={busy !== ""}
                  onClick={() => setStopped(video.id, false)}
                >
                  {t("admin.liveProbe.resume")}
                </Button>
              </li>
            ))}
          </ul>
        ) : null}
      </Fold>
    </li>
  )
}

/**
 * YouTube's progressive confirmation: push queues a read as a video appears,
 * the live-status probe follows streams around their start, and follow-up
 * checks reread finished videos. Push and the probe each fold their settings
 * and figures under a header with their state and a quick switch. The
 * follow-ups have no setting; they ride on every YouTube read.
 */
export function ConfirmationPanel({
  youtube,
  onChange,
}: {
  youtube: Sources["youtube"]
  onChange: (youtube: Sources["youtube"]) => void
}) {
  const { t } = useAppSettings()
  return (
    <Panel level={3} icon={ListChecks} title={t("admin.confirm.title")}>
      <ol className="adm-confirm-steps">
        <PushStage
          push={youtube.push}
          onChange={(push) => onChange({ ...youtube, push })}
        />
        <LiveProbeStage
          probe={youtube.liveProbe}
          onChange={(liveProbe) => onChange({ ...youtube, liveProbe })}
        />
        <Stage
          icon={CalendarCheck}
          title={t("admin.followUp.title")}
          badge={<Badge>{t("admin.followUp.automatic")}</Badge>}
        >
          <ul
            className="adm-chip-row adm-follow-ups"
            aria-label={t("admin.followUp.offsets")}
          >
            {followUpOffsets.map((minutes) => (
              <li key={minutes}>
                <Badge tone="sky">{formatInterval(minutes, t)}</Badge>
              </li>
            ))}
          </ul>
        </Stage>
      </ol>
    </Panel>
  )
}
