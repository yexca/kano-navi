import type { Dashboard, Locale, Profile, Translator } from "@/types"
import {
  ArrowUpRight,
  BadgeCheck,
  Play,
  Radio,
  RefreshCw,
  Sparkles,
} from "lucide-react"

import { ExternalLink, XLogo, YouTubeLogo } from "@/site/links"
import {
  countdownParts,
  eventIsManual,
  eventTypeKey,
  formatJapanDay,
  formatJapanTime,
  formatRelative,
  formatVisitorTime,
  videoKindLabel,
  visitorIsInJapan,
} from "../format"
import { pickCompanion, pickSpotlight } from "../spotlight"

export interface SyncState {
  tone: "leaf" | "honey" | "danger" | "muted"
  label: string
}

function japanDateParts(value: string, locale: Locale) {
  const date = new Date(value)
  const pick = (options: Intl.DateTimeFormatOptions) =>
    new Intl.DateTimeFormat(locale === "ja" ? "ja-JP" : locale, {
      timeZone: "Asia/Tokyo",
      ...options,
    }).format(date)
  return {
    day: pick({ day: "numeric" }).replace(/\D/gu, ""),
    month: pick({ month: "short" }),
    weekday: pick({ weekday: "short" }),
  }
}

function dateOnlyParts(key: string, locale: Locale) {
  const [year, month, day] = key.split("-").map(Number)
  const date = new Date(Date.UTC(year, month - 1, day, 3))
  return japanDateParts(date.toISOString(), locale)
}

function Countdown({
  time,
  now,
  t,
}: {
  time: string
  now: number
  t: Translator
}) {
  const parts = countdownParts(time, now)
  if (!parts || parts.past) return null
  const cells = parts.days
    ? [
        [parts.days, t("countdown.unitDays")],
        [parts.hours, t("countdown.unitHours")],
      ]
    : [
        [parts.hours, t("countdown.unitHours")],
        [parts.minutes, t("countdown.unitMinutes")],
      ]
  return (
    <div className="ticket-countdown">
      <span className="ticket-countdown-label">{t("spotlight.startsIn")}</span>
      <span className="ticket-countdown-cells">
        {cells.map(([value, unit]) => (
          <span key={unit} className="ticket-countdown-cell">
            <strong>{String(value).padStart(2, "0")}</strong>
            <small>{unit}</small>
          </span>
        ))}
      </span>
    </div>
  )
}

type SpotlightItem = NonNullable<ReturnType<typeof pickSpotlight>>
type Companion = NonNullable<ReturnType<typeof pickCompanion>>

function TicketHead({
  item,
  isLive,
  t,
}: {
  item: SpotlightItem
  isLive: boolean
  t: Translator
}) {
  const typeKey = item.event ? eventTypeKey(item.event) : null
  const tag = item.video
    ? videoKindLabel(item.video, t)
    : typeKey
      ? t(typeKey)
      : item.detail || t("schedule.publicEvent")
  const manual = item.event ? eventIsManual(item.event) : false
  return (
    <span className="ticket-head">
      {isLive ? (
        <span className="ticket-onair">
          <Radio aria-hidden="true" />
          {t("spotlight.onAir")}
        </span>
      ) : (
        <span className="kicker">
          {t(item.mode === "next" ? "spotlight.next" : "spotlight.latest")}
        </span>
      )}
      <span className="tag tone-berry">{tag}</span>
      {item.event ? (
        <span className={`ticket-provenance${manual ? " is-manual" : ""}`}>
          {manual ? (
            <BadgeCheck aria-hidden="true" />
          ) : (
            <Sparkles aria-hidden="true" />
          )}
          {t(manual ? "schedule.sourceManual" : "schedule.sourceAutomatic")}
        </span>
      ) : null}
    </span>
  )
}

function TicketWhen({
  item,
  locale,
  now,
  t,
}: {
  item: SpotlightItem
  locale: Locale
  now: number
  t: Translator
}) {
  if (item.mode !== "next") {
    return (
      <span className="ticket-when">
        <small>{formatRelative(item.time, locale, now)}</small>
      </span>
    )
  }
  if (!item.time) {
    return (
      <span className="ticket-when">
        <strong>
          {item.dateOnly} · {t("common.pendingConfirmation")}
        </strong>
      </span>
    )
  }
  return (
    <span className="ticket-when">
      <strong>
        <span className="sr-only">{formatJapanDay(item.time, locale)} </span>
        {formatJapanTime(item.time)} {t("common.timezoneShort")}
      </strong>
      {!visitorIsInJapan() ? (
        <small>
          {t("spotlight.localTime", {
            time: formatVisitorTime(item.time, locale),
          })}
        </small>
      ) : null}
    </span>
  )
}

function DateBlock({ item, locale }: { item: SpotlightItem; locale: Locale }) {
  const parts = item.time
    ? japanDateParts(item.time, locale)
    : item.dateOnly
      ? dateOnlyParts(item.dateOnly, locale)
      : null
  if (!parts) return null
  return (
    <span className="ticket-date" aria-hidden="true">
      <small>{parts.month}</small>
      <strong>{parts.day}</strong>
      <small>{parts.weekday}</small>
    </span>
  )
}

/** A stream or video with artwork: the picture leads, the stub follows. */
function FullTicket({
  item,
  isLive,
  locale,
  now,
  t,
}: {
  item: SpotlightItem
  isLive: boolean
  locale: Locale
  now: number
  t: Translator
}) {
  const Wrapper = item.url ? ExternalLink : "div"
  return (
    <Wrapper
      className={`ticket is-${isLive ? "live" : item.mode}${item.thumbnailUrl ? " has-art" : ""}`}
      href={item.url || undefined}
    >
      <span className="ticket-art" aria-hidden="true">
        {item.thumbnailUrl ? (
          <img src={item.thumbnailUrl} alt="" />
        ) : (
          <span className="ticket-art-fallback" />
        )}
        {item.link === "watch" ? (
          <span className="ticket-play">
            <Play fill="currentColor" />
          </span>
        ) : null}
      </span>
      <span className="ticket-stub">
        <TicketHead item={item} isLive={isLive} t={t} />
        <span className="ticket-body">
          {item.mode === "next" ? (
            <DateBlock item={item} locale={locale} />
          ) : null}
          <span className="ticket-copy">
            <span className="ticket-title">{item.title}</span>
            <TicketWhen item={item} locale={locale} now={now} t={t} />
          </span>
        </span>
        {item.mode === "next" && item.time && !isLive ? (
          <Countdown time={item.time} now={now} t={t} />
        ) : null}
        {isLive ? (
          <span className="ticket-note">{t("spotlight.started")}</span>
        ) : null}
        {item.url ? (
          <span className="ticket-open">
            {t("spotlight.open")}
            <ArrowUpRight aria-hidden="true" />
          </span>
        ) : null}
      </span>
    </Wrapper>
  )
}

/** A schedule entry without artwork: the date is the picture. */
function CompactTicket({
  item,
  isLive,
  locale,
  now,
  t,
}: {
  item: SpotlightItem
  isLive: boolean
  locale: Locale
  now: number
  t: Translator
}) {
  const Wrapper = item.url ? ExternalLink : "div"
  return (
    <Wrapper
      className={`ticket is-compact is-${isLive ? "live" : item.mode}`}
      href={item.url || undefined}
    >
      <span className="ticket-side">
        <DateBlock item={item} locale={locale} />
      </span>
      <span className="ticket-stub">
        <TicketHead item={item} isLive={isLive} t={t} />
        <span className="ticket-copy">
          <span className="ticket-title">{item.title}</span>
          <TicketWhen item={item} locale={locale} now={now} t={t} />
        </span>
        {item.time && !isLive ? (
          <Countdown time={item.time} now={now} t={t} />
        ) : null}
        {isLive ? (
          <span className="ticket-note">{t("spotlight.started")}</span>
        ) : null}
        {item.url ? (
          <span className="ticket-link">
            {item.link === "announcement" ? (
              <XLogo aria-hidden="true" />
            ) : item.link === "watch" ? (
              <Play fill="currentColor" aria-hidden="true" />
            ) : null}
            {t(
              item.link === "announcement"
                ? "spotlight.announcement"
                : item.link === "watch"
                  ? "spotlight.watch"
                  : "spotlight.open",
            )}
            <ArrowUpRight aria-hidden="true" />
          </span>
        ) : null}
      </span>
    </Wrapper>
  )
}

/** The latest video under a compact ticket. */
function LatestCard({
  companion,
  locale,
  now,
  t,
}: {
  companion: Companion
  locale: Locale
  now: number
  t: Translator
}) {
  return (
    <ExternalLink className="latest-card" href={companion.url}>
      <span className="latest-card-art" aria-hidden="true">
        {companion.thumbnailUrl ? (
          <img src={companion.thumbnailUrl} alt="" loading="lazy" />
        ) : (
          <span className="ticket-art-fallback" />
        )}
        <span className="latest-card-play">
          <Play fill="currentColor" />
        </span>
      </span>
      <span className="latest-card-copy">
        <span className="ticket-head">
          <span className="kicker">{t("spotlight.latest")}</span>
          <span className="tag">{videoKindLabel(companion.video, t)}</span>
        </span>
        <span className="latest-card-title">{companion.title}</span>
        {companion.time ? (
          <small>{formatRelative(companion.time, locale, now)}</small>
        ) : null}
      </span>
      <ArrowUpRight className="latest-card-open" aria-hidden="true" />
    </ExternalLink>
  )
}

/** The single most useful thing to know right now: what airs next. */
function NextTicket({
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
  if (isLoading) {
    return (
      <div className="ticket is-loading" aria-busy="true">
        <span className="sr-only">{t("common.loading")}</span>
      </div>
    )
  }
  const item = pickSpotlight(summary, now)
  if (!item) {
    return (
      <div className="ticket is-empty">
        <div className="ticket-stub">
          <p className="kicker">{t("spotlight.next")}</p>
          <p className="ticket-title">{t("spotlight.none")}</p>
          <p className="ticket-note">{t("spotlight.noneHint")}</p>
        </div>
      </div>
    )
  }

  const isLive = item.mode === "next" && Date.parse(item.time || "") <= now
  if (item.layout === "full") {
    return (
      <FullTicket item={item} isLive={isLive} locale={locale} now={now} t={t} />
    )
  }
  const companion = pickCompanion(summary, item)
  return (
    <div className="stage-main">
      <CompactTicket
        item={item}
        isLive={isLive}
        locale={locale}
        now={now}
        t={t}
      />
      {companion ? (
        <LatestCard companion={companion} locale={locale} now={now} t={t} />
      ) : null}
    </div>
  )
}

function ProfileCard({
  profile,
  resources,
  counts,
  postWindowDays,
  syncState,
  syncTime,
  syncIso,
  isRefreshing,
  onRefresh,
  t,
}: {
  profile: Profile
  resources: Dashboard["resources"]
  counts: { posts: number; upcomingEvents: number; videos: number }
  postWindowDays: number
  syncState: SyncState
  syncTime: string
  syncIso: string | null
  isRefreshing: boolean
  onRefresh: () => void
  t: Translator
}) {
  const bilibili = resources.find((resource) =>
    /bilibili\.com/u.test(resource.url || ""),
  )
  const handle = profile.xUrl.split("/").pop()
  return (
    <aside className="profile-card" aria-labelledby="profile-name">
      <div className="profile-banner">
        {profile.bannerUrl ? (
          <img src={profile.bannerUrl} alt={t("header.bannerAlt")} />
        ) : null}
      </div>
      <div className="profile-body">
        <img
          className="profile-avatar"
          src={profile.avatarUrl ?? undefined}
          alt=""
        />
        <p className="kicker">{t("hero.kicker")}</p>
        <h1 id="profile-name" className="profile-name">
          {profile.displayName}
          <span>{profile.romanizedName}</span>
        </h1>
        <p className="profile-catchphrase">{t("hero.catchphrase")}</p>
        <div className="profile-links">
          <ExternalLink className="pill-button" href={profile.xUrl}>
            <XLogo aria-hidden="true" />@{handle}
          </ExternalLink>
          <ExternalLink className="pill-button" href={profile.youtubeUrl}>
            <YouTubeLogo aria-hidden="true" />
            YouTube
          </ExternalLink>
          {bilibili ? (
            <ExternalLink className="pill-button" href={bilibili.url}>
              bilibili
            </ExternalLink>
          ) : null}
        </div>
        <dl className="profile-stats">
          <div>
            <dt>{t("stats.upcoming")}</dt>
            <dd>{counts.upcomingEvents}</dd>
          </div>
          <div>
            <dt>{t("stats.posts", { days: postWindowDays })}</dt>
            <dd>{counts.posts}</dd>
          </div>
          <div>
            <dt>{t("stats.videos")}</dt>
            <dd>{counts.videos}</dd>
          </div>
        </dl>
        <div className={`profile-sync tone-${syncState.tone}`}>
          <span className="sync-dot" aria-hidden="true" />
          <span className="profile-sync-copy">
            <strong>{syncState.label}</strong>
            {syncIso ? (
              <small>
                {t("header.lastUpdated")}{" "}
                <time dateTime={syncIso}>{syncTime}</time>{" "}
                {t("common.timezoneShort")}
              </small>
            ) : null}
          </span>
          <button
            type="button"
            className={`round-button${isRefreshing ? " is-spinning" : ""}`}
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
    </aside>
  )
}

export function NowStage(props: {
  profile: Profile
  summary: Dashboard["summary"]
  resources: Dashboard["resources"]
  counts: { posts: number; upcomingEvents: number; videos: number }
  postWindowDays: number
  syncState: SyncState
  syncTime: string
  syncIso: string | null
  isLoading: boolean
  isRefreshing: boolean
  onRefresh: () => void
  locale: Locale
  now: number
  t: Translator
}) {
  const { summary, isLoading, locale, now, t } = props
  return (
    <section className="stage" id="now" aria-label={t("nav.now")}>
      <NextTicket
        summary={summary}
        isLoading={isLoading}
        locale={locale}
        now={now}
        t={t}
      />
      <ProfileCard {...props} />
    </section>
  )
}
