import type { Locale, Translator, Profile, Dashboard } from "@/types"
import {
  CalendarClock,
  Clapperboard,
  Database,
  MessageCircle,
  Play,
  Radio,
  RefreshCw,
} from "lucide-react"

import {
  cleanVideoTitle,
  countdownParts,
  eventTypeKey,
  formatJapanDay,
  formatJapanTime,
  formatRelative,
  formatVisitorTime,
  videoKindLabel,
  visitorIsInJapan,
} from "../format"
import { ExternalLink, PillLink, XLogo, YouTubeLogo } from "./primitives"

function pickSpotlight(summary, now) {
  const candidates = []
  if (summary?.nextStream?.scheduledAt) {
    const video = summary.nextStream
    candidates.push({
      mode: "next",
      time: video.scheduledAt,
      title: cleanVideoTitle(video.title),
      url: video.url,
      thumbnailUrl: video.thumbnailUrl,
      video,
    })
  }
  if (summary?.nextEvent) {
    const event = summary.nextEvent
    const duplicate = candidates.some(
      (candidate) => event.url && candidate.url === event.url,
    )
    if (!duplicate) {
      candidates.push({
        mode: "next",
        time: event.startsAt || null,
        dateOnly: event.startsAt ? null : event.startsOn,
        title: cleanVideoTitle(event.title),
        detail: event.detail,
        url: event.url,
        event,
      })
    }
  }
  const upcoming = candidates
    .filter((candidate) => {
      const time = Date.parse(candidate.time || "")
      // A stream that started up to three hours ago is probably still live.
      return Number.isNaN(time) || time > now - 3 * 3600 * 1000
    })
    .sort((a, b) =>
      String(a.time || a.dateOnly).localeCompare(String(b.time || b.dateOnly)),
    )
  if (upcoming[0]) return upcoming[0]

  const video = summary?.latestVideo
  if (video) {
    return {
      mode: "latest",
      time: video.publishedAt || video.scheduledAt,
      title: cleanVideoTitle(video.title),
      url: video.url,
      thumbnailUrl: video.thumbnailUrl,
      video,
    }
  }
  return null
}

function Countdown({
  time,
  now,
  t,
}: {
  time: any
  now: number
  t: Translator
}) {
  const parts = countdownParts(time, now)
  if (!parts) return null
  if (parts.past) {
    return (
      <p className="countdown is-live">
        <Radio aria-hidden="true" />
        {t("spotlight.started")}
      </p>
    )
  }
  const units = []
  if (parts.days) units.push(t("countdown.days", { count: parts.days }))
  if (parts.days || parts.hours)
    units.push(t("countdown.hours", { count: parts.hours }))
  if (!parts.days) units.push(t("countdown.minutes", { count: parts.minutes }))
  return (
    <p className="countdown">
      <span className="countdown-label">{t("spotlight.startsIn")}</span>
      <span className="countdown-value">{units.join(" ")}</span>
    </p>
  )
}

function Spotlight({
  summary,
  isLoading,
  locale,
  now,
  t,
}: {
  summary: Dashboard["summary"]
  isLoading: boolean
  locale: Locale
  now: number
  t: Translator
}) {
  const item = pickSpotlight(summary, now)
  if (isLoading) {
    return <div className="spotlight is-loading" aria-hidden="true" />
  }
  if (!item) {
    return (
      <div className="spotlight is-empty">
        <p className="spotlight-label">
          <CalendarClock aria-hidden="true" />
          {t("spotlight.next")}
        </p>
        <p className="spotlight-title">{t("spotlight.none")}</p>
        <p className="spotlight-meta">{t("spotlight.noneHint")}</p>
      </div>
    )
  }

  const Container = item.url ? ExternalLink : "div"
  const typeKey = item.event ? eventTypeKey(item.event) : null
  const tag = item.video
    ? videoKindLabel(item.video, t)
    : typeKey
      ? t(typeKey)
      : item.detail || t("schedule.publicEvent")

  return (
    <Container
      className={`spotlight is-${item.mode}${item.thumbnailUrl ? " has-thumb" : ""}`}
      href={item.url || undefined}
    >
      {item.thumbnailUrl ? (
        <span className="spotlight-thumb">
          <img src={item.thumbnailUrl} alt="" loading="lazy" />
          <span className="spotlight-play" aria-hidden="true">
            <Play fill="currentColor" />
          </span>
        </span>
      ) : null}
      <span className="spotlight-body">
        <span className="spotlight-label">
          {item.mode === "next" ? (
            <CalendarClock aria-hidden="true" />
          ) : (
            <Clapperboard aria-hidden="true" />
          )}
          {t(item.mode === "next" ? "spotlight.next" : "spotlight.latest")}
          <span className="spotlight-tag">{tag}</span>
        </span>
        <span className="spotlight-title">{item.title}</span>
        {item.mode === "next" ? (
          <>
            <span className="spotlight-when">
              {item.time ? (
                <>
                  <strong>{formatJapanDay(item.time, locale)}</strong>
                  <span>
                    {formatJapanTime(item.time)} {t("common.timezoneShort")}
                  </span>
                </>
              ) : (
                <strong>
                  {item.dateOnly} · {t("common.pendingConfirmation")}
                </strong>
              )}
            </span>
            {item.time && !visitorIsInJapan() ? (
              <span className="spotlight-meta">
                {t("spotlight.localTime", {
                  time: formatVisitorTime(item.time, locale),
                })}
              </span>
            ) : null}
            {item.time ? <Countdown time={item.time} now={now} t={t} /> : null}
          </>
        ) : (
          <span className="spotlight-meta">
            {formatRelative(item.time, locale, now)}
          </span>
        )}
      </span>
    </Container>
  )
}

function StatTile({
  icon: Icon,
  label,
  value,
  detail,
  tone = "berry",
}: {
  icon?: React.ElementType
  label?: string
  value: React.ReactNode
  detail?: React.ReactNode
  tone?: string
}) {
  return (
    <div className={`stat-tile tone-${tone}`}>
      <span className="stat-icon" aria-hidden="true">
        <Icon />
      </span>
      <span className="stat-copy">
        <span className="stat-value">{value}</span>
        <span className="stat-label">{label}</span>
      </span>
      {detail ? <span className="stat-detail">{detail}</span> : null}
    </div>
  )
}

export function Hero({
  profile,
  summary,
  counts,
  postWindowDays,
  syncState,
  syncTime,
  isLoading,
  isRefreshing,
  onRefresh,
  resources,
  locale,
  now,
  t,
}: {
  profile: Profile
  summary: Dashboard["summary"]
  counts: any
  postWindowDays: any
  syncState: any
  syncTime: any
  isLoading: boolean
  isRefreshing: boolean
  onRefresh: any
  resources: Dashboard["resources"]
  locale: Locale
  now: number
  t: Translator
}) {
  const bilibili = resources.find((resource) =>
    /bilibili\.com/u.test(resource.url || ""),
  )

  return (
    <section className="hero" id="now" aria-labelledby="hero-title">
      <div className="hero-banner">
        <img src={profile.bannerUrl} alt={t("header.bannerAlt")} />
      </div>

      <div className="hero-body">
        <div className="identity">
          <div className="identity-avatar">
            <img src={profile.avatarUrl} alt="" />
            <span className="identity-badge" aria-hidden="true">
              🍓
            </span>
          </div>
          <div className="identity-copy">
            <p className="eyebrow">{t("hero.kicker")}</p>
            <h1 id="hero-title">
              {profile.displayName}
              <span className="identity-romaji">{profile.romanizedName}</span>
            </h1>
            <p className="identity-catchphrase">{t("hero.catchphrase")}</p>
            <div className="identity-links">
              <PillLink href={profile.xUrl} icon={XLogo} className="is-x">
                @{profile.xUrl.split("/").pop()}
              </PillLink>
              <PillLink
                href={profile.youtubeUrl}
                icon={YouTubeLogo}
                className="is-youtube"
              >
                YouTube
              </PillLink>
              {bilibili ? (
                <PillLink href={bilibili.url} className="is-bilibili">
                  bilibili
                </PillLink>
              ) : null}
            </div>
          </div>
        </div>

        <Spotlight
          summary={summary}
          isLoading={isLoading}
          locale={locale}
          now={now}
          t={t}
        />
      </div>

      <div className="stat-strip">
        <StatTile
          icon={CalendarClock}
          label={t("stats.upcoming")}
          value={counts.upcomingEvents}
          tone="berry"
        />
        <StatTile
          icon={MessageCircle}
          label={t("stats.posts", { days: postWindowDays })}
          value={counts.posts}
          tone="sky"
        />
        <StatTile
          icon={Clapperboard}
          label={t("stats.videos")}
          value={counts.videos}
          tone="honey"
        />
        <div className={`stat-tile stat-sync tone-${syncState.tone}`}>
          <span className="stat-icon" aria-hidden="true">
            <Database />
          </span>
          <span className="stat-copy">
            <span className="stat-value stat-value-text">
              {syncState.label}
            </span>
            <span className="stat-label">{syncTime}</span>
          </span>
          <button
            type="button"
            className={`stat-refresh${isRefreshing ? " is-spinning" : ""}`}
            onClick={onRefresh}
            disabled={isRefreshing}
            aria-label={t(
              isRefreshing ? "common.refreshing" : "common.refresh",
            )}
            title={t(isRefreshing ? "common.refreshing" : "common.refresh")}
          >
            <RefreshCw aria-hidden="true" />
          </button>
        </div>
      </div>
    </section>
  )
}
