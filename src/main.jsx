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
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
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

const dateFormatter = new Intl.DateTimeFormat("zh-CN", {
  timeZone: JAPAN_TIME_ZONE,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
})

const dateTimeFormatter = new Intl.DateTimeFormat("zh-CN", {
  timeZone: JAPAN_TIME_ZONE,
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
})

function pad(value) {
  return String(value).padStart(2, "0")
}

function dateKey(value) {
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return ""
  const parts = dateFormatter.formatToParts(date).reduce((result, part) => {
    result[part.type] = part.value
    return result
  }, {})
  return `${parts.year}-${parts.month}-${parts.day}`
}

function localDateKey(date) {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

function formatMonthLabel(date) {
  return `${date.getFullYear()} 年 ${pad(date.getMonth() + 1)} 月`
}

function formatDateTime(value) {
  if (!value) return "—"
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return "—"
  const parts = dateTimeFormatter.formatToParts(date).reduce((result, part) => {
    result[part.type] = part.value
    return result
  }, {})
  return `${parts.month}.${parts.day} · ${parts.hour}:${parts.minute}`
}

function formatEventDate(value) {
  if (!value) return { date: "—", time: "待确认" }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return { date: "—", time: "待确认" }
  const parts = dateTimeFormatter.formatToParts(date).reduce((result, part) => {
    result[part.type] = part.value
    return result
  }, {})
  return { date: `${parts.month}.${parts.day}`, time: `${parts.hour}:${parts.minute} JST` }
}

function formatNumber(value) {
  const number = Number(value)
  if (!Number.isFinite(number)) return "0"
  return new Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 }).format(number)
}

function formatSyncLabel(meta) {
  return `SQLite 快照 · ${formatSyncTime(meta)}`
}

function formatSyncTime(meta) {
  const sync = meta?.lastSync
  if (sync?.finishedAt) return formatDateTime(sync.finishedAt)
  if (meta?.fetchedAt) return formatDateTime(meta.fetchedAt)
  return "等待同步"
}

function sameMonth(a, b) {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth()
}

function eventIsUpcoming(event) {
  const timestamp = Date.parse(event.startsAt || "")
  if (!Number.isNaN(timestamp)) return timestamp >= Date.now()
  return Boolean(event.isUpcoming)
}

function eventStatusClass(event) {
  if (eventIsUpcoming(event)) return "upcoming"
  if (/待|确认/i.test(event.status || "")) return "pending"
  return "done"
}

function Calendar({ month, selectedDate, events, onSelect, onMonthChange }) {
  const days = useMemo(() => {
    const year = month.getFullYear()
    const monthIndex = month.getMonth()
    const firstDay = new Date(year, monthIndex, 1)
    const startOffset = firstDay.getDay()
    const daysInMonth = new Date(year, monthIndex + 1, 0).getDate()
    const daysInPrevious = new Date(year, monthIndex, 0).getDate()
    const cells = []

    for (let index = 0; index < 42; index += 1) {
      const dayNumber = index - startOffset + 1
      let cellDate
      let isMuted = false
      if (dayNumber < 1) {
        cellDate = new Date(year, monthIndex - 1, daysInPrevious + dayNumber)
        isMuted = true
      } else if (dayNumber > daysInMonth) {
        cellDate = new Date(year, monthIndex + 1, dayNumber - daysInMonth)
        isMuted = true
      } else {
        cellDate = new Date(year, monthIndex, dayNumber)
      }
      cells.push({ date: cellDate, isMuted })
    }
    return cells
  }, [month])

  const eventDates = useMemo(() => new Set(events.map((event) => dateKey(event.startsAt))), [events])
  const todayKey = dateKey(new Date())
  const selectedKey = selectedDate ? localDateKey(selectedDate) : ""

  return (
    <>
      <div className="calendar-toolbar">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon" className="month-button" onClick={() => onMonthChange(-1)} aria-label="上一个月">
              <ChevronLeft className="inline-icon" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>上一个月</TooltipContent>
        </Tooltip>
        <strong>{formatMonthLabel(month)}</strong>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon" className="month-button" onClick={() => onMonthChange(1)} aria-label="下一个月">
              <ChevronRight className="inline-icon" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>下一个月</TooltipContent>
        </Tooltip>
      </div>
      <div className="calendar-weekdays" aria-hidden="true">
        {["日", "一", "二", "三", "四", "五", "六"].map((weekday) => <span key={weekday}>{weekday}</span>)}
      </div>
      <div className="calendar-grid" role="grid" aria-label={`${formatMonthLabel(month)}月历`}>
        {days.map(({ date, isMuted }) => {
          const key = localDateKey(date)
          const hasEvent = eventDates.has(key)
          const isToday = key === todayKey
          const isSelected = key === selectedKey
          return (
            <button
              key={key}
              type="button"
              role="gridcell"
              className={`calendar-day${isMuted ? " is-muted" : ""}${isToday ? " is-today" : ""}${isSelected ? " is-selected" : ""}${hasEvent ? " has-event" : ""}`}
              aria-label={`${date.getFullYear()}年${date.getMonth() + 1}月${date.getDate()}日${hasEvent ? "，有记录" : ""}`}
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

function PostItem({ post, profile }) {
  return (
    <article className="post-item">
      <div className="post-avatar"><img src={profile.avatarUrl} alt="" /></div>
      <div>
        <div className="post-meta">
          <span className={`post-type post-type-${post.type || "daily"}`}>{post.label || "DAILY / 近况"}</span>
          <time className="post-time" dateTime={post.publishedAt}>{formatDateTime(post.publishedAt)}</time>
        </div>
        <p className="post-text">{post.text}</p>
        {post.mediaUrl ? <a className="post-media-link" href={post.url} target="_blank" rel="noreferrer"><ImageIcon className="inline-icon" />含媒体附件</a> : null}
        <div className="post-foot">
          <span><Heart className="inline-icon" /> {formatNumber(post.likes)}</span>
          <span><Repeat2 className="inline-icon" /> {formatNumber(post.reposts)}</span>
          <span><MessageCircle className="inline-icon" /> {formatNumber(post.replies)}</span>
          <a className="post-link" href={post.url} target="_blank" rel="noreferrer">原帖 <ArrowUpRight className="inline-icon" /></a>
        </div>
      </div>
    </article>
  )
}

function VideoItem({ video }) {
  const isUpcoming = Boolean(video.isUpcoming)
  return (
    <a className={`media-item${isUpcoming ? " is-upcoming" : ""}`} href={video.url} target="_blank" rel="noreferrer">
      <div className="media-thumb">
        {video.thumbnailUrl
          ? <img src={video.thumbnailUrl} alt="" loading="lazy" />
          : <span className="media-placeholder" aria-hidden="true"><ImageIcon /></span>}
        <span className="play-badge">{isUpcoming ? <Clock3 size={12} /> : <Play size={11} fill="currentColor" />}</span>
      </div>
      <div className="media-meta"><span>{video.kind || (isUpcoming ? "UPCOMING LIVE" : "VIDEO")}</span><time>{formatDateTime(video.scheduledAt || video.publishedAt)}</time></div>
      <h3>{video.title}</h3>
    </a>
  )
}

function ResourceLink({ resource }) {
  return (
    <a className={`resource-link resource-link-${resource.tone || "ink"}`} href={resource.url} target="_blank" rel="noreferrer">
      <span className="resource-icon">{resource.icon || "↗"}</span>
      <span><strong>{resource.title}</strong><small>{resource.detail}</small></span>
      <ArrowUpRight className="inline-icon" />
    </a>
  )
}

function LoadingPanel() {
  return (
    <div className="loading-panel" role="status" aria-live="polite">
      <LoaderCircle className="loading-icon" />
      <span>正在读取本地快照…</span>
    </div>
  )
}

function App() {
  const [dashboard, setDashboard] = useState(emptyDashboard)
  const [isLoading, setIsLoading] = useState(true)
  const [isRefreshing, setIsRefreshing] = useState(false)
  const [fetchError, setFetchError] = useState("")
  const [activeFilter, setActiveFilter] = useState("all")
  const [month, setMonth] = useState(() => new Date(new Date().getFullYear(), new Date().getMonth(), 1))
  const [selectedDate, setSelectedDate] = useState(null)
  const [isDark, setIsDark] = useState(() => {
    const saved = window.localStorage.getItem("kano-theme")
    return saved ? saved === "dark" : window.matchMedia("(prefers-color-scheme: dark)").matches
  })
  const [toast, setToast] = useState("")
  const hasLoadedRef = useRef(false)

  const loadDashboard = useCallback(async ({ announce = false } = {}) => {
    setIsRefreshing(true)
    if (!hasLoadedRef.current) setIsLoading(true)
    try {
      const response = await fetch(`/api/dashboard?days=${postWindowDays}`, { headers: { accept: "application/json" } })
      if (!response.ok) throw new Error(`API ${response.status}`)
      const payload = await response.json()
      setDashboard({ ...emptyDashboard, ...payload, profile: payload.profile || fallbackProfile, meta: { ...emptyDashboard.meta, ...payload.meta } })
      hasLoadedRef.current = true
      setFetchError("")
      if (announce) setToast("已重新读取 SQLite 快照")
    } catch (error) {
      setFetchError("暂时无法连接本地 API，页面保留当前快照。")
      if (announce) setToast(`读取失败 · ${error.message}`)
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
    if (!toast) return undefined
    const timer = window.setTimeout(() => setToast(""), 2600)
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
  const scheduleAsset = dashboard.assets?.find((asset) => asset.kind === "schedule" && asset.url)
  const syncStatus = dashboard.meta?.lastSync?.status
  const syncWarning = Boolean(syncStatus && syncStatus !== "success")

  const filterCounts = useMemo(() => ({
    all: posts.length,
    notice: posts.filter((post) => post.type === "notice").length,
    daily: posts.filter((post) => post.type === "daily").length,
  }), [posts])
  const visiblePosts = activeFilter === "all" ? posts : posts.filter((post) => post.type === activeFilter)

  const monthEvents = useMemo(() => events.filter((event) => {
    const timestamp = Date.parse(event.startsAt || "")
    return !Number.isNaN(timestamp) && sameMonth(new Date(timestamp), month)
  }), [events, month])
  const selectedEvents = useMemo(() => selectedDate ? monthEvents.filter((event) => dateKey(event.startsAt) === localDateKey(selectedDate)) : [], [monthEvents, selectedDate])
  const displayEvents = useMemo(() => {
    const upcomingMonthEvents = monthEvents.filter(eventIsUpcoming)
    const source = selectedDate ? selectedEvents : (upcomingMonthEvents.length ? upcomingMonthEvents : monthEvents)
    return [...source].sort((a, b) => {
      const upcomingDelta = Number(eventIsUpcoming(b)) - Number(eventIsUpcoming(a))
      if (upcomingDelta) return upcomingDelta
      return Date.parse(a.startsAt || "") - Date.parse(b.startsAt || "")
    }).slice(0, 6)
  }, [monthEvents, selectedDate, selectedEvents])
  const upcomingCount = events.filter(eventIsUpcoming).length

  const handleMonthChange = (offset) => {
    const next = new Date(month.getFullYear(), month.getMonth() + offset, 1)
    setMonth(next)
    setSelectedDate(null)
  }

  const handleSelectDate = (date) => {
    setSelectedDate(date)
    if (!sameMonth(date, month)) setMonth(new Date(date.getFullYear(), date.getMonth(), 1))
  }

  return (
    <TooltipProvider delayDuration={250}>
      <div className="site-shell" id="top">
        <header className="topbar">
          <a className="wordmark" href="#top" aria-label="回到顶部">
            <span className="wordmark-jp">鹿乃</span>
            <span className="wordmark-en">MAHORO / STATUS BOARD</span>
          </a>

          <div className="topbar-center" aria-label="数据状态">
            <span className={`sync-dot${fetchError || syncWarning ? " is-offline" : ""}`} aria-hidden="true" />
            <span>{fetchError ? "本地 API 待连接" : syncWarning ? (syncStatus === "partial" ? "同步部分完成" : "同步需检查") : "SQLite 快照"}</span>
            <span className="topbar-divider" aria-hidden="true" />
            <time dateTime={dashboard.meta?.lastSync?.finishedAt || dashboard.meta?.fetchedAt || undefined}>{formatSyncTime(dashboard.meta)}</time>
          </div>

          <nav className="topnav" aria-label="主导航">
            <a href="#feed">近况</a>
            <a href="#calendar">日程</a>
            <a href="#media">频道</a>
            <a href="#links">资料</a>
          </nav>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="icon" className="icon-button theme-button" onClick={() => setIsDark((value) => !value)} aria-label="切换深色模式">
                {isDark ? <Moon className="icon" /> : <Sun className="icon" />}
              </Button>
            </TooltipTrigger>
            <TooltipContent>{isDark ? "切换浅色模式" : "切换深色模式"}</TooltipContent>
          </Tooltip>
        </header>

        <main>
          <section className="hero-panel" aria-labelledby="hero-title">
            <div className="hero-visual">
              <img src={profile.bannerUrl} alt="鹿乃まほろ的草莓与音乐主题视觉" />
              <div className="hero-visual-caption"><span>VIRTUAL ARTIST</span><span className="caption-line" aria-hidden="true" /><span>KANO MAHORO</span></div>
              <div className="hero-visual-index" aria-hidden="true">01</div>
            </div>

            <div className="hero-info hero-info-compact">
              <div className="identity-block">
                <div className="avatar-wrap"><img src={profile.avatarUrl} alt={`${profile.displayName}头像`} /><span className="avatar-status" title="近期有更新" aria-label="近期有更新" /></div>
                <div className="identity-copy">
                  <p className="overline">{profile.displayName} · {profile.romanizedName}</p>
                  <h1 id="hero-title">{profile.displayName}</h1>
                  <p className="hero-description">{profile.bio}</p>
                  <div className="hero-links">
                    <a href={profile.xUrl} target="_blank" rel="noreferrer">X / @kano_2525 <ArrowUpRight className="inline-icon" /></a>
                    <a href={profile.youtubeUrl} target="_blank" rel="noreferrer">YouTube <ArrowUpRight className="inline-icon" /></a>
                  </div>
                </div>
              </div>
              <div className="hero-cache-note"><span className="tiny-dot" />{formatSyncLabel(dashboard.meta)}</div>
            </div>
          </section>

          <section className="section-intro" aria-labelledby="overview-title">
            <div><p className="overline">TODAY / 一眼看懂</p><h2 id="overview-title">近况面板</h2></div>
            <div className="section-intro-right"><p>{upcomingCount ? `${upcomingCount} 个未来安排已收录` : "把最值得点开的更新放在前面。"}</p><Button variant="outline" className={`refresh-button${isRefreshing ? " is-refreshing" : ""}`} onClick={() => loadDashboard({ announce: true })} disabled={isRefreshing}><RefreshCw className="inline-icon" /><span>{isRefreshing ? "读取中" : "重新读取"}</span></Button></div>
          </section>

          {fetchError ? <div className="api-alert" role="status"><WifiOff className="inline-icon" /><span>{fetchError}</span><button type="button" onClick={() => loadDashboard({ announce: true })}>重试</button></div> : null}

          <div className="dashboard-grid">
            <Card className="panel feed-panel" id="feed">
              <div className="panel-header"><div><p className="panel-index">01 / X FEED</p><h2>最近的声音</h2></div><a className="header-link" href={profile.xUrl} target="_blank" rel="noreferrer">@kano_2525 <ArrowUpRight className="inline-icon" /></a></div>
              <Tabs value={activeFilter} onValueChange={setActiveFilter}>
                <TabsList className="filter-tabs" aria-label="X 动态筛选">
                  <TabsTrigger className="filter-tab" value="all">全部 <span>{pad(filterCounts.all)}</span></TabsTrigger>
                  <TabsTrigger className="filter-tab" value="notice">公告 <span>{pad(filterCounts.notice)}</span></TabsTrigger>
                  <TabsTrigger className="filter-tab" value="daily">日常 <span>{pad(filterCounts.daily)}</span></TabsTrigger>
                </TabsList>
              </Tabs>
              {isLoading ? <LoadingPanel /> : <div className="feed-list">{visiblePosts.length ? visiblePosts.map((post) => <PostItem key={post.id} post={post} profile={profile} />) : <div className="empty-state">最近 {dashboard.meta?.postWindowDays || postWindowDays} 天没有可显示的 X 动态。</div>}</div>}
              <div className="panel-footer"><span className="data-note"><span className="tiny-dot" />最近 {dashboard.meta?.postWindowDays || postWindowDays} 天 · API 快照</span><a href={profile.xUrl} target="_blank" rel="noreferrer" className="footer-action">去 X 看完整串文 <ArrowRight className="inline-icon" /></a></div>
            </Card>

            <Card className="panel calendar-panel" id="calendar">
              <div className="panel-header calendar-header"><div><p className="panel-index">02 / CALENDAR</p><h2>未来日程</h2></div><span className="calendar-status"><CalendarDays className="inline-icon" />{upcomingCount} 个预约</span></div>
              <Calendar month={month} selectedDate={selectedDate} events={events} onSelect={handleSelectDate} onMonthChange={handleMonthChange} />
              <div className="event-list">
                {isLoading ? <LoadingPanel /> : displayEvents.length ? displayEvents.map((event) => { const formatted = formatEventDate(event.startsAt); return <a className={`event-item event-item-${eventStatusClass(event)}`} href={event.url || "#"} target={event.url ? "_blank" : undefined} rel={event.url ? "noreferrer" : undefined} key={event.id}><span className="event-date">{formatted.date}<small>{formatted.time}</small></span><span className="event-copy"><strong>{event.title}</strong><span>{event.detail || "公开活动"}</span></span><span className={`event-status ${eventStatusClass(event)}`}>{eventIsUpcoming(event) ? "即将" : (event.status || "已记录")}</span></a> }) : <div className="event-item"><span className="event-date">—<small>暂无</small></span><span className="event-copy"><strong>这个月还没有公开日程</strong><span>请留意官方 X / YouTube</span></span><span className="event-status pending">待补充</span></div>}
              </div>
              {scheduleAsset ? <a className="schedule-preview" href={scheduleAsset.sourceUrl || scheduleAsset.url} target="_blank" rel="noreferrer"><img src={scheduleAsset.url} alt={scheduleAsset.alt || "鹿乃まほろ活动 schedule"} loading="lazy" /><span><ImageIcon className="inline-icon" />查看 X 发布的 schedule 原图 <ArrowUpRight className="inline-icon" /></span></a> : null}
              <p className="calendar-note">未来安排优先显示，公开日程以 <a href={profile.xUrl} target="_blank" rel="noreferrer">X</a> 与 <a href={profile.youtubeUrl} target="_blank" rel="noreferrer">YouTube</a> 公告为准。</p>
            </Card>

            <Card className="panel focus-panel">
              <div className="panel-header"><div><p className="panel-index">03 / FOCUS</p><h2>最近焦点</h2></div><span className="focus-stamp">LATEST SIGNAL</span></div>
              {focus ? <><div className="focus-layout"><div className="focus-copy"><span className="date-pill">{focus.dateLabel || "最近更新"}</span><h3>{focus.title}</h3><p>{focus.description}</p><div className="focus-actions">{focus.url ? <Button asChild className="primary-button"><a href={focus.url} target="_blank" rel="noreferrer">打开焦点 <Play className="inline-icon" fill="currentColor" /></a></Button> : null}{focus.sourceUrl ? <a className="quiet-link" href={focus.sourceUrl} target="_blank" rel="noreferrer">查看官方资料 <ArrowUpRight className="inline-icon" /></a> : null}</div></div>{focus.imageUrl ? <a className="focus-media" href={focus.url || focus.sourceUrl || "#"} target="_blank" rel="noreferrer"><img src={focus.imageUrl} alt={focus.title} /><div className="media-caption"><span>SONA / 2026</span><span>FOCUS</span></div></a> : null}</div><div className="focus-footer"><div><span className="micro-label">STATE</span><strong>ACTIVE</strong></div><div><span className="micro-label">UPDATED</span><strong>{formatDateTime(focus.updatedAt)}</strong></div><div><span className="micro-label">FAN TAG</span><strong>#鹿友</strong></div></div></> : <div className="empty-state">暂无焦点记录。</div>}
            </Card>

            <Card className="panel media-panel" id="media">
              <div className="panel-header"><div><p className="panel-index">04 / WATCH &amp; LISTEN</p><h2>最近上传</h2></div><a className="header-link" href={`${profile.youtubeUrl}/videos`} target="_blank" rel="noreferrer">打开频道 <ArrowUpRight className="inline-icon" /></a></div>
              {isLoading ? <LoadingPanel /> : <div className="media-grid">{videos.length ? videos.slice(0, 6).map((video) => <VideoItem key={video.id} video={video} />) : <div className="empty-state">暂无 YouTube 快照。</div>}</div>}
              <div className="panel-footer media-footer"><span className="data-note"><span className="tiny-dot" />YouTube RSS + 预约直播</span><a href={profile.youtubeUrl} target="_blank" rel="noreferrer" className="footer-action">订阅频道 <ArrowRight className="inline-icon" /></a></div>
            </Card>

            <Card className="panel archive-panel">
              <div className="panel-header"><div><p className="panel-index">05 / ARCHIVE</p><h2>时间轴</h2></div><a className="header-link" href="https://ja.wikipedia.org/wiki/%E9%B9%BF%E4%B9%83" target="_blank" rel="noreferrer">Wikipedia <ArrowUpRight className="inline-icon" /></a></div>
              <div className="timeline">{timeline.map((item, index) => <div className={`timeline-item${index === 0 ? " is-current" : ""}`} key={item.id}><span className="timeline-year">{item.year}</span><div><strong>{item.title}</strong><p>{item.detail}</p></div></div>)}</div>
              <a className="archive-callout" href="https://www.nicovideo.jp/user/15078610/mylist/16997570" target="_blank" rel="noreferrer"><span className="callout-icon"><ArrowUpRight size={14} /></span><span><strong>从最早的歌开始听</strong><small>NicoNico / mylist</small></span></a>
            </Card>

            <Card className="panel links-panel" id="links">
              <div className="panel-header links-header"><div><p className="panel-index">06 / THE DIRECTORY</p><h2>资料入口</h2></div><span className="links-note">官方与公开档案</span></div>
              <div className="links-grid">{resources.map((resource) => <ResourceLink key={resource.id} resource={resource} />)}</div>
              <div className="tag-cloud"><span className="tag-cloud-label">常用标签</span><a href="https://x.com/hashtag/%E9%B9%BF%E4%B9%83%E3%81%BE%E3%81%BB%E3%82%8D" target="_blank" rel="noreferrer">#鹿乃まほろ</a><a href="https://x.com/hashtag/%E9%B9%BF%E5%8F%8B" target="_blank" rel="noreferrer">#鹿友</a><a href="https://x.com/hashtag/%E3%81%BE%E3%81%BB%E3%82%8D%E3%81%9F%E3%81%84%E3%82%80" target="_blank" rel="noreferrer">#まほろたいむ</a><a href="https://x.com/hashtag/%E3%81%BE%E3%81%BB%E3%82%8D%E3%81%8F%E3%82%8A%E3%81%A3%E3%81%B7" target="_blank" rel="noreferrer">#まほろくりっぷ</a></div>
            </Card>
          </div>
        </main>

        <footer className="site-footer"><div><span className="footer-mark">鹿乃</span><span>fan-made status board</span></div><p>非官方整理页 · 数据由本地 SQLite 快照提供 · 信息以各平台原页面为准</p><a href="#top">回到顶部 <ChevronRight className="inline-icon" /></a></footer>
      </div>
      <div className={`toast${toast ? " is-visible" : ""}`} role="status" aria-live="polite">{toast}</div>
    </TooltipProvider>
  )
}

const rootElement = document.getElementById("root")
const reactRoot = globalThis.__kanoReactRoot && globalThis.__kanoRootElement === rootElement
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
