import { createRoot } from "react-dom/client"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import {
  ArrowRight,
  ArrowUpRight,
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  Clock3,
  Heart,
  Image as ImageIcon,
  Languages,
  LoaderCircle,
  MessageCircle,
  Moon,
  Play,
  RefreshCw,
  Repeat2,
  Sun,
  WifiOff,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { createTranslator, detectLocale, localeOptions } from "@/i18n"
import "./index.css"

const JAPAN_TIME_ZONE = "Asia/Tokyo"
const postWindowDays = 3

const fallbackProfile = {
  displayName: "鹿乃まほろ",
  romanizedName: "Kano Mahoro",
  bio: "歌手 / Virtual Artist / みんなの毎日を、まほろばに。",
  avatarUrl: "/assets/kano-avatar.jpg",
  bannerUrl: "/assets/kano-banner.jpg",
  xUrl: "https://x.com/kano_2525",
  youtubeUrl: "https://www.youtube.com/channel/UCShXNLMXCfstmWKH_q86B8w",
}

const emptyDashboard = {
  profile: fallbackProfile,
  posts: [],
  events: [],
  videos: [],
  focus: null,
  timeline: [],
  resources: [],
  assets: [],
  meta: { fetchedAt: null, lastSync: null, postWindowDays },
}

const intlLocales = {
  "zh-CN": "zh-CN",
  ja: "ja-JP",
  en: "en-US",
}

const dateKeyFormatter = new Intl.DateTimeFormat("en-CA", {
  timeZone: JAPAN_TIME_ZONE,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
})

const dateTimeFormatters = new Map()
const weekRangeFormatters = new Map()
const calendarDateFormatters = new Map()

function getFormatter(cache, locale, options) {
  if (!cache.has(locale)) {
    cache.set(
      locale,
      new Intl.DateTimeFormat(intlLocales[locale] || intlLocales.en, options),
    )
  }
  return cache.get(locale)
}

function pad(value) {
  return String(value).padStart(2, "0")
}

function dateKey(value) {
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return ""
  const parts = dateKeyFormatter.formatToParts(date).reduce((result, part) => {
    result[part.type] = part.value
    return result
  }, {})
  return `${parts.year}-${parts.month}-${parts.day}`
}

function localDateKey(date) {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

function japanToday() {
  const [year, month, day] = dateKey(new Date()).split("-").map(Number)
  return new Date(year, month - 1, day)
}

function addDays(date, amount) {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate() + amount)
}

function startOfWeek(date) {
  const daysSinceMonday = (date.getDay() + 6) % 7
  return addDays(date, -daysSinceMonday)
}

function formatWeekLabel(weekStart, locale) {
  const weekEnd = addDays(weekStart, 6)
  const formatter = getFormatter(weekRangeFormatters, locale, {
    year: "numeric",
    month: "short",
    day: "numeric",
  })
  return typeof formatter.formatRange === "function"
    ? formatter.formatRange(weekStart, weekEnd)
    : `${formatter.format(weekStart)} – ${formatter.format(weekEnd)}`
}

function formatCalendarDate(date, locale) {
  return getFormatter(calendarDateFormatters, locale, {
    year: "numeric",
    month: "long",
    day: "numeric",
  }).format(date)
}

function formatDateTime(value, locale) {
  if (!value) return "—"
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return "—"
  const parts = getFormatter(dateTimeFormatters, locale, {
    timeZone: JAPAN_TIME_ZONE,
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  })
    .formatToParts(date)
    .reduce((result, part) => {
      result[part.type] = part.value
      return result
    }, {})
  return `${parts.month}.${parts.day} · ${parts.hour}:${parts.minute}`
}

function formatEventDate(value, locale, t) {
  if (!value) return { date: "—", time: t("common.pendingConfirmation") }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return { date: "—", time: t("common.pendingConfirmation") }
  }
  const parts = getFormatter(dateTimeFormatters, locale, {
    timeZone: JAPAN_TIME_ZONE,
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  })
    .formatToParts(date)
    .reduce((result, part) => {
      result[part.type] = part.value
      return result
    }, {})
  return {
    date: `${parts.month}.${parts.day}`,
    time: `${parts.hour}:${parts.minute} JST`,
  }
}

function formatNumber(value, locale) {
  const number = Number(value)
  if (!Number.isFinite(number)) return "0"
  return new Intl.NumberFormat(intlLocales[locale] || intlLocales.en, {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(number)
}

function formatSyncTime(meta, locale, t) {
  const sync = meta?.lastSync
  if (sync?.finishedAt) return formatDateTime(sync.finishedAt, locale)
  if (meta?.fetchedAt) return formatDateTime(meta.fetchedAt, locale)
  return t("sync.waiting")
}

function eventIsUpcoming(event) {
  const timestamp = Date.parse(event.startsAt || "")
  if (!Number.isNaN(timestamp)) return timestamp >= Date.now()
  return Boolean(event.isUpcoming)
}

function eventStatusClass(event) {
  if (eventIsUpcoming(event)) return "upcoming"
  if (/待|確認|确认|pending|tentative/i.test(event.status || "")) {
    return "pending"
  }
  return "done"
}

function Calendar({ selectedDate, events, locale, t, onSelect, onWeekChange }) {
  const weekStart = useMemo(() => startOfWeek(selectedDate), [selectedDate])
  const days = useMemo(
    () => Array.from({ length: 7 }, (_, index) => addDays(weekStart, index)),
    [weekStart],
  )

  const eventDates = useMemo(
    () => new Set(events.map((event) => dateKey(event.startsAt))),
    [events],
  )
  const todayKey = dateKey(new Date())
  const selectedKey = localDateKey(selectedDate)
  const weekLabel = formatWeekLabel(weekStart, locale)

  return (
    <>
      <div className="calendar-toolbar">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="week-button"
              onClick={() => onWeekChange(-1)}
              aria-label={t("calendar.previousWeek")}
            >
              <ChevronLeft className="inline-icon" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t("calendar.previousWeek")}</TooltipContent>
        </Tooltip>
        <strong>{weekLabel}</strong>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="week-button"
              onClick={() => onWeekChange(1)}
              aria-label={t("calendar.nextWeek")}
            >
              <ChevronRight className="inline-icon" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t("calendar.nextWeek")}</TooltipContent>
        </Tooltip>
      </div>
      <div className="calendar-weekdays" aria-hidden="true">
        {t("calendar.weekdays").map((weekday) => (
          <span key={weekday}>{weekday}</span>
        ))}
      </div>
      <div
        className="calendar-grid"
        role="grid"
        aria-label={t("calendar.weekLabel", {
          range: weekLabel,
        })}
      >
        {days.map((date) => {
          const key = localDateKey(date)
          const hasEvent = eventDates.has(key)
          const isToday = key === todayKey
          const isSelected = key === selectedKey
          const spokenDate = formatCalendarDate(date, locale)
          return (
            <button
              key={key}
              type="button"
              role="gridcell"
              className={`calendar-day${isToday ? " is-today" : ""}${isSelected ? " is-selected" : ""}${hasEvent ? " has-event" : ""}`}
              aria-current={isToday ? "date" : undefined}
              aria-selected={isSelected}
              aria-label={
                hasEvent
                  ? t("calendar.dayWithEvent", { date: spokenDate })
                  : spokenDate
              }
              onClick={() => onSelect(date)}
            >
              {date.getDate()}
            </button>
          )
        })}
      </div>
    </>
  )
}

function PostItem({ post, profile, locale, t }) {
  return (
    <article className="post-item">
      <div className="post-avatar">
        <img src={profile.avatarUrl} alt="" />
      </div>
      <div>
        <div className="post-meta">
          <span className={`post-type post-type-${post.type || "daily"}`}>
            {post.label || t("feed.defaultLabel")}
          </span>
          <time className="post-time" dateTime={post.publishedAt}>
            {formatDateTime(post.publishedAt, locale)}
          </time>
        </div>
        <p className="post-text">{post.text}</p>
        {post.mediaUrl ? (
          <a
            className="post-media-link"
            href={post.url}
            target="_blank"
            rel="noreferrer"
          >
            <ImageIcon className="inline-icon" />
            {t("feed.mediaAttachment")}
          </a>
        ) : null}
        <div className="post-foot">
          <span>
            <Heart className="inline-icon" /> {formatNumber(post.likes, locale)}
          </span>
          <span>
            <Repeat2 className="inline-icon" />{" "}
            {formatNumber(post.reposts, locale)}
          </span>
          <span>
            <MessageCircle className="inline-icon" />{" "}
            {formatNumber(post.replies, locale)}
          </span>
          <a
            className="post-link"
            href={post.url}
            target="_blank"
            rel="noreferrer"
          >
            {t("feed.originalPost")} <ArrowUpRight className="inline-icon" />
          </a>
        </div>
      </div>
    </article>
  )
}

function VideoItem({ video, locale }) {
  const isUpcoming = Boolean(video.isUpcoming)
  return (
    <a
      className={`media-item${isUpcoming ? " is-upcoming" : ""}`}
      href={video.url}
      target="_blank"
      rel="noreferrer"
    >
      <div className="media-thumb">
        {video.thumbnailUrl ? (
          <img src={video.thumbnailUrl} alt="" loading="lazy" />
        ) : (
          <span className="media-placeholder" aria-hidden="true">
            <ImageIcon />
          </span>
        )}
        <span className="play-badge">
          {isUpcoming ? (
            <Clock3 size={12} />
          ) : (
            <Play size={11} fill="currentColor" />
          )}
        </span>
      </div>
      <div className="media-meta">
        <span>{video.kind || (isUpcoming ? "UPCOMING LIVE" : "VIDEO")}</span>
        <time>
          {formatDateTime(video.scheduledAt || video.publishedAt, locale)}
        </time>
      </div>
      <h3>{video.title}</h3>
    </a>
  )
}

function ResourceLink({ resource }) {
  return (
    <a
      className={`resource-link resource-link-${resource.tone || "ink"}`}
      href={resource.url}
      target="_blank"
      rel="noreferrer"
    >
      <span className="resource-icon">{resource.icon || "↗"}</span>
      <span>
        <strong>{resource.title}</strong>
        <small>{resource.detail}</small>
      </span>
      <ArrowUpRight className="inline-icon" />
    </a>
  )
}

function LoadingPanel({ t }) {
  return (
    <div className="loading-panel" role="status" aria-live="polite">
      <LoaderCircle className="loading-icon" />
      <span>{t("common.loading")}</span>
    </div>
  )
}

function getInitialLocale() {
  const savedLocale = window.localStorage.getItem("kano-locale")
  if (localeOptions.some((option) => option.value === savedLocale)) {
    return savedLocale
  }

  const browserLanguages = window.navigator.languages?.length
    ? window.navigator.languages
    : [window.navigator.language]
  return detectLocale(browserLanguages)
}

function App() {
  const [dashboard, setDashboard] = useState(emptyDashboard)
  const [isLoading, setIsLoading] = useState(true)
  const [isRefreshing, setIsRefreshing] = useState(false)
  const [fetchError, setFetchError] = useState(false)
  const [activeFilter, setActiveFilter] = useState("all")
  const [selectedDate, setSelectedDate] = useState(japanToday)
  const [isDark, setIsDark] = useState(() => {
    const saved = window.localStorage.getItem("kano-theme")
    return saved
      ? saved === "dark"
      : window.matchMedia("(prefers-color-scheme: dark)").matches
  })
  const [locale, setLocale] = useState(getInitialLocale)
  const [toast, setToast] = useState(null)
  const hasLoadedRef = useRef(false)
  const t = useMemo(() => createTranslator(locale), [locale])

  const loadDashboard = useCallback(async ({ announce = false } = {}) => {
    setIsRefreshing(true)
    if (!hasLoadedRef.current) setIsLoading(true)
    try {
      const response = await fetch(`/api/dashboard?days=${postWindowDays}`, {
        headers: { accept: "application/json" },
      })
      if (!response.ok) throw new Error(`API ${response.status}`)
      const payload = await response.json()
      setDashboard({
        ...emptyDashboard,
        ...payload,
        profile: payload.profile || fallbackProfile,
        meta: { ...emptyDashboard.meta, ...payload.meta },
      })
      hasLoadedRef.current = true
      setFetchError(false)
      if (announce) setToast({ key: "toast.reloaded" })
    } catch (error) {
      setFetchError(true)
      if (announce) {
        setToast({ key: "toast.failed", values: { message: error.message } })
      }
    } finally {
      setIsLoading(false)
      setIsRefreshing(false)
    }
  }, [])

  useEffect(() => {
    loadDashboard()
  }, [loadDashboard])

  useEffect(() => {
    document.documentElement.classList.toggle("dark", isDark)
    window.localStorage.setItem("kano-theme", isDark ? "dark" : "light")
  }, [isDark])

  useEffect(() => {
    document.documentElement.lang = locale
    document.title = t("meta.title")
    document
      .querySelector('meta[name="description"]')
      ?.setAttribute("content", t("meta.description"))
    window.localStorage.setItem("kano-locale", locale)
  }, [locale, t])

  useEffect(() => {
    if (!toast) return undefined
    const timer = window.setTimeout(() => setToast(null), 2600)
    return () => window.clearTimeout(timer)
  }, [toast])

  const profileData = dashboard.profile || fallbackProfile
  const profile = {
    ...fallbackProfile,
    ...profileData,
    avatarUrl: profileData.avatarUrl || fallbackProfile.avatarUrl,
    bannerUrl: profileData.bannerUrl || fallbackProfile.bannerUrl,
  }
  const posts = dashboard.posts || []
  const events = dashboard.events || []
  const videos = dashboard.videos || []
  const resources = dashboard.resources || []
  const timeline = dashboard.timeline || []
  const focus = dashboard.focus
  const scheduleAsset = dashboard.assets?.find(
    (asset) => asset.kind === "schedule" && asset.url,
  )
  const syncStatus = dashboard.meta?.lastSync?.status
  const syncWarning = Boolean(syncStatus && syncStatus !== "success")
  const syncLabel = fetchError
    ? t("sync.apiUnavailable")
    : syncWarning
      ? syncStatus === "partial"
        ? t("sync.partial")
        : t("sync.needsReview")
      : t("sync.snapshot")
  const syncTime = formatSyncTime(dashboard.meta, locale, t)

  const filterCounts = useMemo(
    () => ({
      all: posts.length,
      notice: posts.filter((post) => post.type === "notice").length,
      daily: posts.filter((post) => post.type === "daily").length,
    }),
    [posts],
  )
  const visiblePosts =
    activeFilter === "all"
      ? posts
      : posts.filter((post) => post.type === activeFilter)

  const selectedEvents = useMemo(
    () =>
      events.filter(
        (event) => dateKey(event.startsAt) === localDateKey(selectedDate),
      ),
    [events, selectedDate],
  )
  const displayEvents = useMemo(
    () =>
      [...selectedEvents]
        .sort((a, b) => {
          const upcomingDelta =
            Number(eventIsUpcoming(b)) - Number(eventIsUpcoming(a))
          if (upcomingDelta) return upcomingDelta
          return Date.parse(a.startsAt || "") - Date.parse(b.startsAt || "")
        })
        .slice(0, 6),
    [selectedEvents],
  )
  const upcomingCount = events.filter(eventIsUpcoming).length

  const handleWeekChange = (offset) => {
    setSelectedDate((current) => addDays(current, offset * 7))
  }

  const handleSelectDate = (date) => {
    setSelectedDate(date)
  }

  return (
    <TooltipProvider delayDuration={250}>
      <div className="site-shell" id="top">
        <header className="topbar">
          <a
            className="wordmark"
            href="#top"
            aria-label={t("header.backToTop")}
          >
            <img className="topbar-avatar" src={profile.avatarUrl} alt="" />
            <span className="wordmark-jp">鹿乃</span>
          </a>

          <div
            className="topbar-center"
            aria-label={`${t("header.unofficial")}. ${t("header.lastUpdated")}: ${syncTime}. ${syncLabel}`}
          >
            <span className="topbar-kicker">{t("header.unofficial")}</span>
            <span className="topbar-sync" title={syncLabel}>
              <span
                className={`sync-dot${fetchError || syncWarning ? " is-offline" : ""}`}
                aria-hidden="true"
              />
              <span className="topbar-sync-label">
                {t("header.lastUpdated")}
              </span>
              <time
                dateTime={
                  dashboard.meta?.lastSync?.finishedAt ||
                  dashboard.meta?.fetchedAt ||
                  undefined
                }
              >
                {syncTime}
              </time>
            </span>
          </div>

          <div className="topbar-actions">
            <DropdownMenu>
              <Tooltip>
                <TooltipTrigger asChild>
                  <DropdownMenuTrigger asChild>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="icon-button language-button"
                      aria-label={t("header.language")}
                    >
                      <Languages className="icon" aria-hidden="true" />
                    </Button>
                  </DropdownMenuTrigger>
                </TooltipTrigger>
                <TooltipContent>{t("header.language")}</TooltipContent>
              </Tooltip>
              <DropdownMenuContent
                align="end"
                className="language-menu-content"
              >
                <DropdownMenuLabel>{t("header.language")}</DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuRadioGroup
                  value={locale}
                  onValueChange={setLocale}
                >
                  {localeOptions.map((option) => (
                    <DropdownMenuRadioItem
                      value={option.value}
                      key={option.value}
                      className="language-menu-item"
                    >
                      <span lang={option.value}>{option.label}</span>
                    </DropdownMenuRadioItem>
                  ))}
                </DropdownMenuRadioGroup>
              </DropdownMenuContent>
            </DropdownMenu>

            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  className="icon-button theme-button"
                  onClick={() => setIsDark((value) => !value)}
                  aria-label={t(
                    isDark ? "header.switchToLight" : "header.switchToDark",
                  )}
                >
                  {isDark ? (
                    <Sun className="icon" />
                  ) : (
                    <Moon className="icon" />
                  )}
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                {t(isDark ? "header.switchToLight" : "header.switchToDark")}
              </TooltipContent>
            </Tooltip>
          </div>
        </header>

        <main>
          <section className="hero-panel" aria-label={t("header.banner")}>
            <div className="hero-visual">
              <img src={profile.bannerUrl} alt={t("header.bannerAlt")} />
              <div className="hero-visual-caption">
                <span>VIRTUAL ARTIST</span>
                <span className="caption-line" aria-hidden="true" />
                <span>KANO MAHORO</span>
              </div>
              <div className="hero-visual-index" aria-hidden="true">
                01
              </div>
            </div>
          </section>

          <section className="section-intro" aria-labelledby="overview-title">
            <div>
              <p className="overline">{t("overview.eyebrow")}</p>
              <h2 id="overview-title">{t("overview.title")}</h2>
            </div>
            <div className="section-intro-right">
              <p>
                {upcomingCount
                  ? t("overview.upcoming", { count: upcomingCount })
                  : t("overview.lead")}
              </p>
              <Button
                variant="outline"
                className={`refresh-button${isRefreshing ? " is-refreshing" : ""}`}
                onClick={() => loadDashboard({ announce: true })}
                disabled={isRefreshing}
              >
                <RefreshCw className="inline-icon" />
                <span>
                  {t(isRefreshing ? "overview.refreshing" : "overview.refresh")}
                </span>
              </Button>
            </div>
          </section>

          {fetchError ? (
            <div className="api-alert" role="status">
              <WifiOff className="inline-icon" />
              <span>{t("error.api")}</span>
              <button
                type="button"
                onClick={() => loadDashboard({ announce: true })}
              >
                {t("common.retry")}
              </button>
            </div>
          ) : null}

          <div className="dashboard-grid">
            <Card className="panel feed-panel" id="feed">
              <div className="panel-header">
                <div>
                  <p className="panel-index">01 / X FEED</p>
                  <h2>{t("feed.title")}</h2>
                </div>
                <a
                  className="header-link"
                  href={profile.xUrl}
                  target="_blank"
                  rel="noreferrer"
                >
                  @kano_2525 <ArrowUpRight className="inline-icon" />
                </a>
              </div>
              <Tabs value={activeFilter} onValueChange={setActiveFilter}>
                <TabsList
                  className="filter-tabs"
                  aria-label={t("feed.filterLabel")}
                >
                  <TabsTrigger className="filter-tab" value="all">
                    {t("feed.all")} <span>{pad(filterCounts.all)}</span>
                  </TabsTrigger>
                  <TabsTrigger className="filter-tab" value="notice">
                    {t("feed.notices")} <span>{pad(filterCounts.notice)}</span>
                  </TabsTrigger>
                  <TabsTrigger className="filter-tab" value="daily">
                    {t("feed.daily")} <span>{pad(filterCounts.daily)}</span>
                  </TabsTrigger>
                </TabsList>
              </Tabs>
              {isLoading ? (
                <LoadingPanel t={t} />
              ) : (
                <div className="feed-list">
                  {visiblePosts.length ? (
                    visiblePosts.map((post) => (
                      <PostItem
                        key={post.id}
                        post={post}
                        profile={profile}
                        locale={locale}
                        t={t}
                      />
                    ))
                  ) : (
                    <div className="empty-state">
                      {t("feed.empty", {
                        days: dashboard.meta?.postWindowDays || postWindowDays,
                      })}
                    </div>
                  )}
                </div>
              )}
              <div className="panel-footer">
                <span className="data-note">
                  <span className="tiny-dot" />
                  {t("feed.window", {
                    days: dashboard.meta?.postWindowDays || postWindowDays,
                  })}
                </span>
                <a
                  href={profile.xUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="footer-action"
                >
                  {t("feed.viewThread")} <ArrowRight className="inline-icon" />
                </a>
              </div>
            </Card>

            <Card className="panel calendar-panel" id="calendar">
              <div className="panel-header calendar-header">
                <div>
                  <p className="panel-index">02 / CALENDAR</p>
                  <h2>{t("calendar.title")}</h2>
                </div>
                <span className="calendar-status">
                  <CalendarDays className="inline-icon" />
                  {t("calendar.reservations", { count: upcomingCount })}
                </span>
              </div>
              <Calendar
                selectedDate={selectedDate}
                events={events}
                locale={locale}
                t={t}
                onSelect={handleSelectDate}
                onWeekChange={handleWeekChange}
              />
              <div className="event-list">
                {isLoading ? (
                  <LoadingPanel t={t} />
                ) : displayEvents.length ? (
                  displayEvents.map((event) => {
                    const formatted = formatEventDate(event.startsAt, locale, t)
                    const statusClass = eventStatusClass(event)
                    return (
                      <a
                        className={`event-item event-item-${statusClass}`}
                        href={event.url || "#"}
                        target={event.url ? "_blank" : undefined}
                        rel={event.url ? "noreferrer" : undefined}
                        key={event.id}
                      >
                        <span className="event-date">
                          {formatted.date}
                          <small>{formatted.time}</small>
                        </span>
                        <span className="event-copy">
                          <strong>{event.title}</strong>
                          <span>
                            {event.detail || t("calendar.publicEvent")}
                          </span>
                        </span>
                        <span className={`event-status ${statusClass}`}>
                          {eventIsUpcoming(event)
                            ? t("calendar.upcoming")
                            : t(
                                statusClass === "pending"
                                  ? "calendar.pending"
                                  : "calendar.recorded",
                              )}
                        </span>
                      </a>
                    )
                  })
                ) : (
                  <div className="event-item">
                    <span className="event-date">
                      —<small>{t("calendar.none")}</small>
                    </span>
                    <span className="event-copy">
                      <strong>{t("calendar.emptyTitle")}</strong>
                      <span>{t("calendar.emptyHint")}</span>
                    </span>
                    <span className="event-status pending">
                      {t("calendar.pending")}
                    </span>
                  </div>
                )}
              </div>
              {scheduleAsset ? (
                <a
                  className="schedule-preview"
                  href={scheduleAsset.sourceUrl || scheduleAsset.url}
                  target="_blank"
                  rel="noreferrer"
                >
                  <img
                    src={scheduleAsset.url}
                    alt={scheduleAsset.alt || t("calendar.scheduleAlt")}
                    loading="lazy"
                  />
                  <span>
                    <ImageIcon className="inline-icon" />
                    {t("calendar.viewSchedule")}{" "}
                    <ArrowUpRight className="inline-icon" />
                  </span>
                </a>
              ) : null}
              <p className="calendar-note">
                {t("calendar.notePrefix")}
                <a href={profile.xUrl} target="_blank" rel="noreferrer">
                  X
                </a>
                {t("calendar.noteBetween")}
                <a href={profile.youtubeUrl} target="_blank" rel="noreferrer">
                  YouTube
                </a>
                {t("calendar.noteSuffix")}
              </p>
            </Card>

            <Card className="panel focus-panel">
              <div className="panel-header">
                <div>
                  <p className="panel-index">03 / FOCUS</p>
                  <h2>{t("focus.title")}</h2>
                </div>
                <span className="focus-stamp">LATEST SIGNAL</span>
              </div>
              {focus ? (
                <>
                  <div className="focus-layout">
                    <div className="focus-copy">
                      <span className="date-pill">
                        {focus.dateLabel || t("focus.latestUpdate")}
                      </span>
                      <h3>{focus.title}</h3>
                      <p>{focus.description}</p>
                      <div className="focus-actions">
                        {focus.url ? (
                          <Button asChild className="primary-button">
                            <a
                              href={focus.url}
                              target="_blank"
                              rel="noreferrer"
                            >
                              {t("focus.open")}{" "}
                              <Play
                                className="inline-icon"
                                fill="currentColor"
                              />
                            </a>
                          </Button>
                        ) : null}
                        {focus.sourceUrl ? (
                          <a
                            className="quiet-link"
                            href={focus.sourceUrl}
                            target="_blank"
                            rel="noreferrer"
                          >
                            {t("focus.officialSource")}{" "}
                            <ArrowUpRight className="inline-icon" />
                          </a>
                        ) : null}
                      </div>
                    </div>
                    {focus.imageUrl ? (
                      <a
                        className="focus-media"
                        href={focus.url || focus.sourceUrl || "#"}
                        target="_blank"
                        rel="noreferrer"
                      >
                        <img src={focus.imageUrl} alt={focus.title} />
                        <div className="media-caption">
                          <span>SONA / 2026</span>
                          <span>FOCUS</span>
                        </div>
                      </a>
                    ) : null}
                  </div>
                  <div className="focus-footer">
                    <div>
                      <span className="micro-label">{t("focus.state")}</span>
                      <strong>{t("focus.active")}</strong>
                    </div>
                    <div>
                      <span className="micro-label">{t("focus.updated")}</span>
                      <strong>{formatDateTime(focus.updatedAt, locale)}</strong>
                    </div>
                    <div>
                      <span className="micro-label">{t("focus.fanTag")}</span>
                      <strong>#鹿友</strong>
                    </div>
                  </div>
                </>
              ) : (
                <div className="empty-state">{t("focus.empty")}</div>
              )}
            </Card>

            <Card className="panel media-panel" id="media">
              <div className="panel-header">
                <div>
                  <p className="panel-index">04 / WATCH &amp; LISTEN</p>
                  <h2>{t("media.title")}</h2>
                </div>
                <a
                  className="header-link"
                  href={`${profile.youtubeUrl}/videos`}
                  target="_blank"
                  rel="noreferrer"
                >
                  {t("media.openChannel")}{" "}
                  <ArrowUpRight className="inline-icon" />
                </a>
              </div>
              {isLoading ? (
                <LoadingPanel t={t} />
              ) : (
                <div className="media-grid">
                  {videos.length ? (
                    videos
                      .slice(0, 6)
                      .map((video) => (
                        <VideoItem
                          key={video.id}
                          video={video}
                          locale={locale}
                        />
                      ))
                  ) : (
                    <div className="empty-state">{t("media.empty")}</div>
                  )}
                </div>
              )}
              <div className="panel-footer media-footer">
                <span className="data-note">
                  <span className="tiny-dot" />
                  {t("media.dataNote")}
                </span>
                <a
                  href={profile.youtubeUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="footer-action"
                >
                  {t("media.subscribe")} <ArrowRight className="inline-icon" />
                </a>
              </div>
            </Card>

            <Card className="panel archive-panel">
              <div className="panel-header">
                <div>
                  <p className="panel-index">05 / ARCHIVE</p>
                  <h2>{t("archive.title")}</h2>
                </div>
                <a
                  className="header-link"
                  href="https://ja.wikipedia.org/wiki/%E9%B9%BF%E4%B9%83"
                  target="_blank"
                  rel="noreferrer"
                >
                  Wikipedia <ArrowUpRight className="inline-icon" />
                </a>
              </div>
              <div className="timeline">
                {timeline.map((item, index) => (
                  <div
                    className={`timeline-item${index === 0 ? " is-current" : ""}`}
                    key={item.id}
                  >
                    <span className="timeline-year">{item.year}</span>
                    <div>
                      <strong>{item.title}</strong>
                      <p>{item.detail}</p>
                    </div>
                  </div>
                ))}
              </div>
              <a
                className="archive-callout"
                href="https://www.nicovideo.jp/user/15078610/mylist/16997570"
                target="_blank"
                rel="noreferrer"
              >
                <span className="callout-icon">
                  <ArrowUpRight size={14} />
                </span>
                <span>
                  <strong>{t("archive.listenFromStart")}</strong>
                  <small>NicoNico / mylist</small>
                </span>
              </a>
            </Card>

            <Card className="panel links-panel" id="links">
              <div className="panel-header links-header">
                <div>
                  <p className="panel-index">06 / THE DIRECTORY</p>
                  <h2>{t("directory.title")}</h2>
                </div>
                <span className="links-note">{t("directory.note")}</span>
              </div>
              <div className="links-grid">
                {resources.map((resource) => (
                  <ResourceLink key={resource.id} resource={resource} />
                ))}
              </div>
              <div className="tag-cloud">
                <span className="tag-cloud-label">{t("directory.tags")}</span>
                <a
                  href="https://x.com/hashtag/%E9%B9%BF%E4%B9%83%E3%81%BE%E3%81%BB%E3%82%8D"
                  target="_blank"
                  rel="noreferrer"
                >
                  #鹿乃まほろ
                </a>
                <a
                  href="https://x.com/hashtag/%E9%B9%BF%E5%8F%8B"
                  target="_blank"
                  rel="noreferrer"
                >
                  #鹿友
                </a>
                <a
                  href="https://x.com/hashtag/%E3%81%BE%E3%81%BB%E3%82%8D%E3%81%9F%E3%81%84%E3%82%80"
                  target="_blank"
                  rel="noreferrer"
                >
                  #まほろたいむ
                </a>
                <a
                  href="https://x.com/hashtag/%E3%81%BE%E3%81%BB%E3%82%8D%E3%81%8F%E3%82%8A%E3%81%A3%E3%81%B7"
                  target="_blank"
                  rel="noreferrer"
                >
                  #まほろくりっぷ
                </a>
              </div>
            </Card>
          </div>
        </main>

        <footer className="site-footer">
          <div>
            <span className="footer-mark">鹿乃</span>
            <span>{t("footer.fanMade")}</span>
          </div>
          <p>{t("footer.disclaimer")}</p>
          <a href="#top">
            {t("footer.backToTop")} <ChevronRight className="inline-icon" />
          </a>
        </footer>
      </div>
      <div
        className={`toast${toast ? " is-visible" : ""}`}
        role="status"
        aria-live="polite"
      >
        {toast ? t(toast.key, toast.values) : ""}
      </div>
    </TooltipProvider>
  )
}

const rootElement = document.getElementById("root")
const reactRoot =
  globalThis.__kanoReactRoot && globalThis.__kanoRootElement === rootElement
    ? globalThis.__kanoReactRoot
    : createRoot(rootElement)
globalThis.__kanoReactRoot = reactRoot
globalThis.__kanoRootElement = rootElement
reactRoot.render(<App />)

if (import.meta.hot) {
  import.meta.hot.dispose(() => {
    reactRoot.unmount()
    globalThis.__kanoReactRoot = undefined
    globalThis.__kanoRootElement = undefined
  })
}
