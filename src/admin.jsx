import { useCallback, useEffect, useMemo, useState } from "react"
import {
  CalendarClock,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronUp,
  Download,
  Gauge,
  Image as ImageIcon,
  KeyRound,
  LoaderCircle,
  LockKeyhole,
  LogOut,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Save,
  ScanSearch,
  Settings2,
  Search,
  Trash2,
  Upload,
  X,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import "./admin.css"

const emptyEvent = {
  title: "",
  detail: "",
  startsOn: "",
  startTime: "",
  endTime: "",
  timePrecision: "exact",
  status: "待确认",
  eventType: "event",
  url: "",
}

const japanDateTime = new Intl.DateTimeFormat("en-CA", {
  timeZone: "Asia/Tokyo",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
})

function japanParts(value) {
  if (!value) return null
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null
  return japanDateTime.formatToParts(date).reduce((result, part) => {
    result[part.type] = part.value
    return result
  }, {})
}

function eventToForm(event) {
  const start = japanParts(event.startsAt)
  const end = japanParts(event.endsAt)
  return {
    title: event.title || "",
    detail: event.detail || "",
    startsOn: event.startsOn || "",
    startTime: start ? `${start.hour}:${start.minute}` : "",
    endTime: end ? `${end.hour}:${end.minute}` : "",
    timePrecision: event.timePrecision || (start ? "exact" : "unknown"),
    status: event.status || "",
    eventType: event.eventType || "event",
    url: event.url || "",
  }
}

function eventPayload(form) {
  const startsAt = form.startTime
    ? new Date(`${form.startsOn}T${form.startTime}:00+09:00`)
    : null
  let endsAt = form.endTime
    ? new Date(`${form.startsOn}T${form.endTime}:00+09:00`)
    : null
  if (startsAt && endsAt && endsAt < startsAt) {
    endsAt = new Date(endsAt.getTime() + 24 * 60 * 60 * 1000)
  }
  return {
    ...form,
    startsAt: startsAt?.toISOString() || null,
    endsAt: endsAt?.toISOString() || null,
    timePrecision: startsAt ? form.timePrecision : "unknown",
  }
}

async function request(path, options = {}) {
  const response = await fetch(`/api/admin${path}`, {
    credentials: "same-origin",
    ...options,
    headers: {
      accept: "application/json",
      ...(options.body ? { "content-type": "application/json" } : {}),
      ...(options.headers || {}),
    },
  })
  if (response.status === 204) return null
  const payload = await response.json().catch(() => ({}))
  if (!response.ok) {
    const error = new Error(
      payload.message || payload.error || `API ${response.status}`,
    )
    error.status = response.status
    throw error
  }
  return payload
}

function Login({ onLogin }) {
  const [password, setPassword] = useState("")
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState("")

  const submit = async (event) => {
    event.preventDefault()
    setIsSubmitting(true)
    setError("")
    try {
      await onLogin(password)
    } catch (loginError) {
      setError(
        loginError.status === 429
          ? "尝试次数过多，请稍后再试。"
          : "密码不正确。",
      )
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <main className="admin-login-shell">
      <form className="admin-login" onSubmit={submit}>
        <span className="admin-login-icon" aria-hidden="true">
          <LockKeyhole />
        </span>
        <p className="admin-kicker">KANO STATUS BOARD</p>
        <h1>管理界面</h1>
        <label htmlFor="admin-password">访问密码</label>
        <input
          id="admin-password"
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          required
          autoFocus
        />
        {error ? <p className="admin-form-error">{error}</p> : null}
        <Button type="submit" disabled={isSubmitting}>
          {isSubmitting ? (
            <LoaderCircle className="admin-spin" />
          ) : (
            <KeyRound />
          )}
          登录
        </Button>
      </form>
    </main>
  )
}

function EventEditor({ event, onClose, onSaved }) {
  const [form, setForm] = useState(() =>
    event ? eventToForm(event) : { ...emptyEvent },
  )
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState("")
  const editing = Boolean(event)

  const setField = (name) => (inputEvent) => {
    setForm((current) => ({ ...current, [name]: inputEvent.target.value }))
  }

  const submit = async (submitEvent) => {
    submitEvent.preventDefault()
    setIsSaving(true)
    setError("")
    try {
      const result = await request(
        editing ? `/events/${event.id}` : "/events",
        {
          method: editing ? "PUT" : "POST",
          body: JSON.stringify(eventPayload(form)),
        },
      )
      onSaved(result.event)
    } catch (saveError) {
      setError(saveError.message)
    } finally {
      setIsSaving(false)
    }
  }

  return (
    <section className="admin-editor" aria-labelledby="event-editor-title">
      <div className="admin-editor-header">
        <div>
          <p className="admin-kicker">SCHEDULE EDITOR</p>
          <h2 id="event-editor-title">{editing ? "编辑日程" : "新增日程"}</h2>
        </div>
        <Button
          variant="ghost"
          size="icon"
          onClick={onClose}
          aria-label="关闭编辑器"
        >
          <X />
        </Button>
      </div>
      <form className="admin-event-form" onSubmit={submit}>
        <label className="admin-field admin-field-wide">
          <span>标题</span>
          <input
            value={form.title}
            onChange={setField("title")}
            maxLength={240}
            required
          />
        </label>
        <label className="admin-field admin-field-wide">
          <span>详情</span>
          <textarea
            value={form.detail}
            onChange={setField("detail")}
            maxLength={500}
            rows={3}
          />
        </label>
        <label className="admin-field">
          <span>日期</span>
          <input
            type="date"
            value={form.startsOn}
            onChange={setField("startsOn")}
            required
          />
        </label>
        <label className="admin-field">
          <span>开始时间</span>
          <input
            type="time"
            value={form.startTime}
            onChange={setField("startTime")}
          />
        </label>
        <label className="admin-field">
          <span>结束时间</span>
          <input
            type="time"
            value={form.endTime}
            onChange={setField("endTime")}
          />
        </label>
        <label className="admin-field">
          <span>时间精度</span>
          <select
            value={form.startTime ? form.timePrecision : "unknown"}
            onChange={setField("timePrecision")}
            disabled={!form.startTime}
          >
            <option value="exact">准确</option>
            <option value="approximate">大约</option>
            <option value="unknown">未知</option>
          </select>
        </label>
        <label className="admin-field">
          <span>类型</span>
          <select value={form.eventType} onChange={setField("eventType")}>
            <option value="event">活动</option>
            <option value="stream">直播</option>
            <option value="member">会员限定</option>
            <option value="release">发布</option>
            <option value="appearance">出演</option>
          </select>
        </label>
        <label className="admin-field">
          <span>状态</span>
          <input
            value={form.status}
            onChange={setField("status")}
            maxLength={80}
          />
        </label>
        <label className="admin-field admin-field-wide">
          <span>公开链接</span>
          <input
            type="url"
            value={form.url}
            onChange={setField("url")}
            maxLength={2000}
          />
        </label>
        {error ? (
          <p className="admin-form-error admin-field-wide">{error}</p>
        ) : null}
        <div className="admin-form-actions admin-field-wide">
          <Button type="button" variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button type="submit" disabled={isSaving}>
            {isSaving ? <LoaderCircle className="admin-spin" /> : <Save />}
            保存并人工确认
          </Button>
        </div>
      </form>
    </section>
  )
}

function formatEventTime(event) {
  if (!event.startsAt) return "时间未定"
  const parts = japanParts(event.startsAt)
  return parts ? `${parts.hour}:${parts.minute} JST` : "时间未定"
}

const profileSlotLabels = {
  avatar: "头像",
  banner: "横幅",
}

function profileSourceLabel(source) {
  if (source === "youtube") return "YouTube"
  if (source === "upload") return "本地上传"
  return "X"
}

function ProfileMediaManager({ value, onChange, onNotice, onError }) {
  const [slot, setSlot] = useState("avatar")
  const [source, setSource] = useState("x")
  const [sourceUrl, setSourceUrl] = useState("")
  const [busyKey, setBusyKey] = useState("")

  const items = value?.items || []
  const active = value?.active || {}

  const applyPayload = (payload) => {
    if (payload?.items) onChange(payload)
  }

  const discover = async (targetSlot, targetSource) => {
    const key = `discover-${targetSlot}-${targetSource}`
    setBusyKey(key)
    onError("")
    try {
      const payload = await request("/profile-media/discover", {
        method: "POST",
        body: JSON.stringify({ slot: targetSlot, source: targetSource }),
      })
      applyPayload(payload)
      onNotice(
        `${profileSourceLabel(targetSource)} ${profileSlotLabels[targetSlot]} 候选已更新`,
      )
    } catch (discoverError) {
      onError(discoverError.message)
    } finally {
      setBusyKey("")
    }
  }

  const addUrl = async (event) => {
    event.preventDefault()
    if (!sourceUrl.trim()) return
    setBusyKey("add-url")
    onError("")
    try {
      const payload = await request("/profile-media", {
        method: "POST",
        body: JSON.stringify({ slot, source, sourceUrl }),
      })
      applyPayload(payload)
      setSourceUrl("")
      onNotice("图片候选已添加")
    } catch (addError) {
      onError(addError.message)
    } finally {
      setBusyKey("")
    }
  }

  const upload = async (event, targetSlot) => {
    const file = event.target.files?.[0]
    event.target.value = ""
    if (!file) return
    setBusyKey(`upload-${targetSlot}`)
    onError("")
    try {
      const response = await fetch(
        `/api/admin/profile-media/upload/${targetSlot}`,
        {
          method: "POST",
          credentials: "same-origin",
          headers: {
            accept: "application/json",
            "content-type": file.type || "application/octet-stream",
          },
          body: file,
        },
      )
      const payload = await response.json().catch(() => ({}))
      if (!response.ok)
        throw new Error(
          payload.message || payload.error || `API ${response.status}`,
        )
      applyPayload(payload)
      setSlot(targetSlot)
      onNotice(`${profileSlotLabels[targetSlot]}候选已上传，请选择启用`)
    } catch (uploadError) {
      onError(uploadError.message)
    } finally {
      setBusyKey("")
    }
  }

  const mutateItem = async (item, action) => {
    const key = `${action}-${item.id}`
    setBusyKey(key)
    onError("")
    try {
      const payload = await request(
        `/profile-media/${encodeURIComponent(item.id)}/${action}`,
        {
          method: "POST",
        },
      )
      applyPayload(payload)
      onNotice(
        action === "select"
          ? `${profileSlotLabels[item.slot]}已切换`
          : "候选图片已下载",
      )
    } catch (mutateError) {
      onError(mutateError.message)
    } finally {
      setBusyKey("")
    }
  }

  return (
    <section
      className="admin-profile-media"
      aria-labelledby="profile-media-title"
    >
      <div className="admin-section-heading">
        <span className="admin-section-icon">
          <ImageIcon />
        </span>
        <div>
          <p className="admin-kicker">PROFILE MEDIA</p>
          <h2 id="profile-media-title">头像与横幅</h2>
        </div>
      </div>
      <p className="admin-profile-help">
        只在这里手动发现或上传，普通同步不会自动替换当前素材。
      </p>
      <div className="admin-profile-slots">
        {Object.keys(profileSlotLabels).map((targetSlot) => {
          const current = active[targetSlot]
          const slotItems = items.filter((item) => item.slot === targetSlot)
          return (
            <article className="admin-profile-slot" key={targetSlot}>
              <div
                className={`admin-profile-current admin-profile-current-${targetSlot}`}
              >
                {current?.publicUrl ? (
                  <img src={current.publicUrl} alt="" />
                ) : (
                  <span aria-hidden="true">
                    <ImageIcon />
                  </span>
                )}
                <div>
                  <strong>{profileSlotLabels[targetSlot]}</strong>
                  <small>
                    {current
                      ? `${profileSourceLabel(current.source)} · 当前使用`
                      : "尚未选择"}
                  </small>
                </div>
              </div>
              <div className="admin-profile-actions">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => discover(targetSlot, "x")}
                  disabled={busyKey !== ""}
                >
                  {busyKey === `discover-${targetSlot}-x` ? (
                    <LoaderCircle className="admin-spin" />
                  ) : (
                    <Search />
                  )}
                  发现 X
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => discover(targetSlot, "youtube")}
                  disabled={busyKey !== ""}
                >
                  {busyKey === `discover-${targetSlot}-youtube` ? (
                    <LoaderCircle className="admin-spin" />
                  ) : (
                    <Search />
                  )}
                  发现 YouTube
                </Button>
                <label className="admin-upload-button">
                  <Upload />
                  上传
                  <input
                    type="file"
                    accept="image/avif,image/gif,image/jpeg,image/png,image/webp"
                    onChange={(event) => upload(event, targetSlot)}
                    disabled={busyKey !== ""}
                  />
                </label>
              </div>
              <form className="admin-profile-url-form" onSubmit={addUrl}>
                <select
                  value={slot === targetSlot ? source : "x"}
                  onChange={(event) => {
                    setSlot(targetSlot)
                    setSource(event.target.value)
                  }}
                  aria-label={`${profileSlotLabels[targetSlot]}来源`}
                >
                  <option value="x">X 图片地址</option>
                  <option value="youtube">YouTube 图片地址</option>
                </select>
                <input
                  type="url"
                  value={slot === targetSlot ? sourceUrl : ""}
                  onFocus={() => setSlot(targetSlot)}
                  onChange={(event) => {
                    setSlot(targetSlot)
                    setSourceUrl(event.target.value)
                  }}
                  placeholder="手动粘贴图片地址"
                  aria-label={`${profileSlotLabels[targetSlot]}图片地址`}
                />
                <Button
                  type="submit"
                  size="icon"
                  variant="outline"
                  disabled={busyKey !== ""}
                  aria-label="添加图片地址"
                >
                  <Plus />
                </Button>
              </form>
              <div className="admin-profile-candidates">
                {slotItems.map((item) => (
                  <div className="admin-profile-candidate" key={item.id}>
                    <div className="admin-profile-candidate-preview">
                      {item.previewUrl ? (
                        <img src={item.previewUrl} alt="" />
                      ) : (
                        <ImageIcon />
                      )}
                    </div>
                    <div className="admin-profile-candidate-copy">
                      <strong>{profileSourceLabel(item.source)}</strong>
                      <small>
                        {item.status === "ready"
                          ? "已下载"
                          : item.status === "failed"
                            ? "下载失败"
                            : "待下载"}
                      </small>
                    </div>
                    <div className="admin-profile-candidate-actions">
                      {item.status !== "ready" ? (
                        <Button
                          type="button"
                          size="icon"
                          variant="ghost"
                          onClick={() => mutateItem(item, "download")}
                          disabled={busyKey !== ""}
                          aria-label="下载候选"
                          title="下载候选"
                        >
                          {busyKey === `download-${item.id}` ? (
                            <LoaderCircle className="admin-spin" />
                          ) : (
                            <Download />
                          )}
                        </Button>
                      ) : null}
                      {item.status === "ready" && !item.isActive ? (
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          onClick={() => mutateItem(item, "select")}
                          disabled={busyKey !== ""}
                        >
                          <Check /> 选择
                        </Button>
                      ) : item.isActive ? (
                        <Badge variant="mint">当前使用</Badge>
                      ) : null}
                    </div>
                  </div>
                ))}
                {!slotItems.length ? (
                  <p className="admin-profile-empty">暂无候选</p>
                ) : null}
              </div>
            </article>
          )
        })}
      </div>
      <p className="admin-profile-footnote">
        候选按来源下载到 <code>data/x</code> 或 <code>data/youtube</code>
        ；本地上传和选择后的头像、横幅写入 <code>data/avatar</code>
        ，选择操作会原子替换对应槽位。
      </p>
    </section>
  )
}

function LegacyAdminApp() {
  const [session, setSession] = useState(null)
  const [config, setConfig] = useState(null)
  const [events, setEvents] = useState([])
  const [videos, setVideos] = useState([])
  const [profileMedia, setProfileMedia] = useState({
    items: [],
    active: { avatar: null, banner: null },
  })
  const [model, setModel] = useState("")
  const [scheduleEnabled, setScheduleEnabled] = useState(true)
  const [scheduleKeywords, setScheduleKeywords] = useState("")
  const [featuredVideoId, setFeaturedVideoId] = useState("")
  const [editor, setEditor] = useState(null)
  const [isLoading, setIsLoading] = useState(true)
  const [notice, setNotice] = useState("")
  const [error, setError] = useState("")

  useEffect(() => {
    document.documentElement.lang = "zh-CN"
    document.title = "管理界面 / Kano status board"
    let robots = document.querySelector('meta[name="robots"]')
    const previousContent = robots?.getAttribute("content") ?? null
    const created = !robots
    if (!robots) {
      robots = document.createElement("meta")
      robots.setAttribute("name", "robots")
      document.head.append(robots)
    }
    robots.setAttribute("content", "noindex, nofollow")
    return () => {
      if (created) robots.remove()
      else if (previousContent == null) robots.removeAttribute("content")
      else robots.setAttribute("content", previousContent)
    }
  }, [])

  const loadData = useCallback(async () => {
    const [configPayload, eventsPayload, videosPayload, profileMediaPayload] =
      await Promise.all([
        request("/config"),
        request("/events"),
        request("/videos"),
        request("/profile-media"),
      ])
    setConfig(configPayload)
    setModel(configPayload.llmModel)
    setScheduleEnabled(configPayload.scheduleExtractionEnabled !== false)
    setScheduleKeywords((configPayload.scheduleKeywords || []).join("\n"))
    setFeaturedVideoId(configPayload.featuredVideoId || "")
    setEvents(eventsPayload.events)
    setVideos(videosPayload.videos || [])
    setProfileMedia(profileMediaPayload)
  }, [])

  useEffect(() => {
    let active = true
    request("/session")
      .then(async (payload) => {
        if (!active) return
        setSession(payload)
        if (payload.authenticated) await loadData()
      })
      .catch((loadError) => active && setError(loadError.message))
      .finally(() => active && setIsLoading(false))
    return () => {
      active = false
    }
  }, [loadData])

  const login = async (password) => {
    const payload = await request("/login", {
      method: "POST",
      body: JSON.stringify({ password }),
    })
    setSession(payload)
    await loadData()
  }

  const logout = async () => {
    setError("")
    try {
      await request("/logout", { method: "POST" })
      setSession((current) => ({ ...current, authenticated: false }))
      setConfig(null)
      setEvents([])
      setVideos([])
      setProfileMedia({ items: [], active: { avatar: null, banner: null } })
    } catch (logoutError) {
      setError(logoutError.message)
    }
  }

  const saveConfig = async (event) => {
    event.preventDefault()
    setError("")
    try {
      const payload = await request("/config", {
        method: "PUT",
        body: JSON.stringify({
          llmModel: model,
          scheduleExtractionEnabled: scheduleEnabled,
          scheduleKeywords,
          featuredVideoId: featuredVideoId || null,
        }),
      })
      setConfig(payload)
      setScheduleEnabled(payload.scheduleExtractionEnabled !== false)
      setScheduleKeywords((payload.scheduleKeywords || []).join("\n"))
      setFeaturedVideoId(payload.featuredVideoId || "")
      setNotice("设置已保存")
    } catch (saveError) {
      setError(saveError.message)
    }
  }

  const replaceEvent = (saved) => {
    setEvents((current) => {
      const remaining = current.filter((item) => item.id !== saved.id)
      return [saved, ...remaining].sort((a, b) =>
        `${b.startsOn}${b.startsAt || ""}`.localeCompare(
          `${a.startsOn}${a.startsAt || ""}`,
        ),
      )
    })
    setEditor(null)
    setNotice("日程已保存并锁定为人工确认")
  }

  const confirmEvent = async (event) => {
    setError("")
    try {
      const payload = await request(`/events/${event.id}/confirm`, {
        method: "POST",
      })
      replaceEvent(payload.event)
    } catch (confirmError) {
      setError(confirmError.message)
    }
  }

  const removeEvent = async (event) => {
    if (
      !window.confirm(
        `删除“${event.title}”？该记录会保留为人工锁定的删除标记。`,
      )
    ) {
      return
    }
    setError("")
    try {
      await request(`/events/${event.id}`, { method: "DELETE" })
      setEvents((current) => current.filter((item) => item.id !== event.id))
      setNotice("日程已删除并保留人工锁")
    } catch (removeError) {
      setError(removeError.message)
    }
  }

  const counts = useMemo(
    () => ({
      total: events.length,
      manual: events.filter((event) => event.provenance === "manual").length,
      automatic: events.filter((event) => event.provenance !== "manual").length,
    }),
    [events],
  )

  if (isLoading || !session) {
    return (
      <main className="admin-loading">
        <LoaderCircle className="admin-spin" />
        正在读取管理状态
      </main>
    )
  }
  if (!session.authenticated) return <Login onLogin={login} />

  return (
    <div className="admin-shell">
      <header className="admin-topbar">
        <div>
          <p className="admin-kicker">KANO STATUS BOARD</p>
          <h1>管理界面</h1>
        </div>
        <div className="admin-topbar-actions">
          <Badge variant={session.mode === "production" ? "coral" : "mint"}>
            {session.mode === "production" ? "生产模式" : "开发模式"}
          </Badge>
          {session.requiresPassword ? (
            <Button variant="outline" size="sm" onClick={logout}>
              <LogOut /> 退出
            </Button>
          ) : null}
        </div>
      </header>

      {notice ? (
        <div className="admin-notice" role="status">
          <Check /> {notice}
          <button
            type="button"
            onClick={() => setNotice("")}
            aria-label="关闭提示"
          >
            <X />
          </button>
        </div>
      ) : null}
      {error ? (
        <div className="admin-error" role="alert">
          {error}
        </div>
      ) : null}

      <main className="admin-main">
        <section
          className="admin-settings"
          aria-labelledby="llm-settings-title"
        >
          <div className="admin-section-heading">
            <span className="admin-section-icon">
              <Settings2 />
            </span>
            <div>
              <p className="admin-kicker">OPENAI</p>
              <h2 id="llm-settings-title">内容设置</h2>
            </div>
          </div>
          <form className="admin-config-form" onSubmit={saveConfig}>
            <label className="admin-field admin-field-wide">
              <span>模型名称</span>
              <input
                value={model}
                onChange={(event) => setModel(event.target.value)}
                required
              />
            </label>
            <label className="admin-field admin-field-wide admin-toggle-field">
              <span>启用日程自动识别</span>
              <input
                type="checkbox"
                checked={scheduleEnabled}
                onChange={(event) => setScheduleEnabled(event.target.checked)}
              />
            </label>
            <label className="admin-field admin-field-wide">
              <span>日程关键词</span>
              <textarea
                value={scheduleKeywords}
                onChange={(event) => setScheduleKeywords(event.target.value)}
                rows={3}
                placeholder="每行一个关键词"
              />
            </label>
            <label className="admin-field admin-field-wide">
              <span>Featured video</span>
              <select
                value={featuredVideoId}
                onChange={(event) => setFeaturedVideoId(event.target.value)}
              >
                <option value="">不设置</option>
                {videos.map((video) => (
                  <option value={video.id} key={video.id}>
                    {video.title}
                  </option>
                ))}
              </select>
            </label>
            <div className="admin-form-actions admin-field-wide">
              <Button type="submit">
                <Save /> 保存设置
              </Button>
            </div>
          </form>
          <div
            className={`admin-key-status ${config?.openAiKeyConfigured ? "is-ready" : ""}`}
          >
            <KeyRound />
            {config?.openAiKeyConfigured
              ? "OPENAI_API_KEY 已由服务端环境提供"
              : "OPENAI_API_KEY 尚未配置，自动识别会安全跳过"}
          </div>
        </section>

        <ProfileMediaManager
          value={profileMedia}
          onChange={setProfileMedia}
          onNotice={setNotice}
          onError={setError}
        />

        <section
          className="admin-schedules"
          aria-labelledby="schedule-admin-title"
        >
          <div className="admin-schedule-header">
            <div className="admin-section-heading">
              <span className="admin-section-icon">
                <CalendarClock />
              </span>
              <div>
                <p className="admin-kicker">SCHEDULES</p>
                <h2 id="schedule-admin-title">日程管理</h2>
              </div>
            </div>
            <Button onClick={() => setEditor({ type: "new" })}>
              <Plus /> 新增日程
            </Button>
          </div>
          <div className="admin-counts" aria-label="日程统计">
            <span>
              全部 <strong>{counts.total}</strong>
            </span>
            <span>
              人工确认 <strong>{counts.manual}</strong>
            </span>
            <span>
              自动识别 <strong>{counts.automatic}</strong>
            </span>
          </div>

          {editor ? (
            <EventEditor
              event={editor.type === "edit" ? editor.event : null}
              onClose={() => setEditor(null)}
              onSaved={replaceEvent}
            />
          ) : null}

          <div className="admin-event-list">
            {events.map((event) => (
              <article className="admin-event-row" key={event.id}>
                <div className="admin-event-date">
                  <strong>{event.startsOn}</strong>
                  <span>{formatEventTime(event)}</span>
                </div>
                <div className="admin-event-copy">
                  <div>
                    <h3>{event.title}</h3>
                    <Badge
                      className="admin-source-badge"
                      variant={
                        event.provenance === "manual" ? "mint" : "butter"
                      }
                    >
                      {event.provenance === "manual" ? "人工确认" : "自动识别"}
                    </Badge>
                  </div>
                  <p>{event.detail || event.status || "暂无详情"}</p>
                </div>
                <div className="admin-event-actions">
                  {event.provenance !== "manual" ? (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => confirmEvent(event)}
                    >
                      <Check /> 确认
                    </Button>
                  ) : null}
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() => setEditor({ type: "edit", event })}
                    aria-label={`编辑 ${event.title}`}
                    title="编辑"
                  >
                    <Pencil />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() => removeEvent(event)}
                    aria-label={`删除 ${event.title}`}
                    title="删除"
                  >
                    <Trash2 />
                  </Button>
                </div>
              </article>
            ))}
            {!events.length ? (
              <div className="admin-empty">当前没有日程记录</div>
            ) : null}
          </div>
        </section>
      </main>
    </div>
  )
}

const adminTabs = [
  { id: "overview", label: "概览", icon: Gauge },
  { id: "schedules", label: "日程管理", icon: CalendarClock },
  { id: "scan", label: "扫描设置", icon: ScanSearch },
  { id: "providers", label: "模型提供商", icon: Settings2 },
  { id: "media", label: "头像与横幅", icon: ImageIcon },
]

const emptyProvider = {
  id: "",
  name: "",
  protocol: "openai-responses",
  baseUrl: "https://api.openai.com/v1",
  model: "gpt-4o-mini",
  enabled: true,
  visionCapable: true,
  timeoutMs: 30000,
  maxRetries: 0,
  apiKey: "",
  clearApiKey: false,
}

function syncStatusLabel(status) {
  if (status === "running" || status === "queued") return "进行中"
  if (status === "completed" || status === "success") return "已完成"
  if (status === "partial") return "部分完成"
  if (status === "failed") return "失败"
  return status || "未知"
}

function AdminMetric({ label, value, note, tone = "neutral" }) {
  return (
    <div className={`admin-metric admin-metric-${tone}`}>
      <span>{label}</span>
      <strong>{value}</strong>
      {note ? <small>{note}</small> : null}
    </div>
  )
}

function OverviewPanel({
  eventsPage,
  providers,
  config,
  latestRun,
  jobs,
  onStartSync,
  onReload,
  isBusy,
}) {
  const events = eventsPage?.items || eventsPage?.events || []
  const manualCount = events.filter(
    (event) => event.provenance === "manual",
  ).length
  const activeJob = jobs.find(
    (job) => job.status === "running" || job.status === "queued",
  )
  return (
    <section className="admin-view" aria-labelledby="admin-overview-title">
      <div className="admin-view-heading">
        <div>
          <p className="admin-kicker">WORKSPACE</p>
          <h2 id="admin-overview-title">概览</h2>
          <p>查看快照状态、自动化任务和需要人工处理的日程。</p>
        </div>
        <div className="admin-heading-actions">
          <Button variant="outline" onClick={onReload} disabled={isBusy}>
            <RefreshCw /> 刷新状态
          </Button>
          <Button onClick={onStartSync} disabled={Boolean(activeJob) || isBusy}>
            {isBusy ? <LoaderCircle className="admin-spin" /> : <Play />}
            开始同步
          </Button>
        </div>
      </div>
      <div className="admin-metrics-grid">
        <AdminMetric
          label="日程总数"
          value={eventsPage?.total ?? events.length}
          note="当前筛选结果之外的总记录"
          tone="coral"
        />
        <AdminMetric
          label="人工锁定"
          value={manualCount}
          note="本页已确认或编辑"
          tone="mint"
        />
        <AdminMetric
          label="视觉提供商"
          value={
            providers.filter(
              (provider) => provider.enabled && provider.visionCapable,
            ).length
          }
          note={`${providers.length} 个已配置`}
          tone="butter"
        />
        <AdminMetric
          label="扫描阶段"
          value={config?.scheduleVisionEnabled ? "已启用" : "已暂停"}
          note={
            config?.scheduleKeywordEnabled
              ? "关键词候选识别开启"
              : "关键词候选识别暂停"
          }
          tone="sky"
        />
      </div>
      <div className="admin-overview-columns">
        <section className="admin-panel admin-panel-flat">
          <div className="admin-panel-heading">
            <div>
              <p className="admin-kicker">SYNC ACTIVITY</p>
              <h3>同步活动</h3>
            </div>
            {activeJob ? (
              <Badge variant="butter">
                {syncStatusLabel(activeJob.status)}
              </Badge>
            ) : null}
          </div>
          {latestRun ? (
            <div className="admin-activity-row">
              <div>
                <strong>{latestRun.message || "最近一次同步"}</strong>
                <small>
                  {latestRun.triggeredBy === "mcp"
                    ? "MCP 控制"
                    : "管理界面 / 定时任务"}
                  {latestRun.finishedAt ? ` · ${latestRun.finishedAt}` : ""}
                </small>
              </div>
              <Badge
                variant={
                  latestRun.status === "success"
                    ? "mint"
                    : latestRun.status === "failed"
                      ? "coral"
                      : "butter"
                }
              >
                {syncStatusLabel(latestRun.status)}
              </Badge>
            </div>
          ) : (
            <p className="admin-empty-inline">还没有同步记录</p>
          )}
          {activeJob ? (
            <div className="admin-activity-row admin-activity-row-muted">
              <div>
                <strong>
                  {activeJob.kind === "scan" ? "自动日程扫描" : "完整同步"}
                </strong>
                <small>{activeJob.id}</small>
              </div>
              <span className="admin-live-dot">运行中</span>
            </div>
          ) : null}
        </section>
        <section className="admin-panel admin-panel-flat">
          <div className="admin-panel-heading">
            <div>
              <p className="admin-kicker">OPERATOR NOTES</p>
              <h3>操作边界</h3>
            </div>
            <CheckCircle2 className="admin-panel-check" />
          </div>
          <ul className="admin-boundary-list">
            <li>自动识别只会写入未锁定的候选日程。</li>
            <li>确认、编辑、删除和素材选择仍需真人访问本页。</li>
            <li>MCP 刷新只推进页面 revision，不等于外部同步。</li>
          </ul>
        </section>
      </div>
    </section>
  )
}

function ScanSettingsPanel({
  keywordEnabled,
  visionEnabled,
  keywords,
  providerOrder,
  providers,
  videos,
  featuredVideoId,
  onChangeKeywordEnabled,
  onChangeVisionEnabled,
  onChangeKeywords,
  onChangeFeaturedVideoId,
  onSave,
  onRunScan,
  isBusy,
}) {
  return (
    <section className="admin-view" aria-labelledby="admin-scan-title">
      <div className="admin-view-heading">
        <div>
          <p className="admin-kicker">AUTOMATION PIPELINE</p>
          <h2 id="admin-scan-title">扫描设置</h2>
          <p>把候选识别和视觉抽取拆开控制，便于限流、排错和逐步上线。</p>
        </div>
        <Button
          onClick={onRunScan}
          disabled={isBusy || !keywordEnabled || !visionEnabled}
        >
          {isBusy ? <LoaderCircle className="admin-spin" /> : <ScanSearch />}
          运行自动扫描
        </Button>
      </div>
      <form className="admin-scan-grid" onSubmit={onSave}>
        <section className="admin-panel admin-stage-panel">
          <div className="admin-stage-number">01</div>
          <div className="admin-panel-heading">
            <div>
              <p className="admin-kicker">KEYWORD CANDIDATES</p>
              <h3>关键词候选识别</h3>
            </div>
            <input
              className="admin-switch"
              type="checkbox"
              checked={keywordEnabled}
              onChange={(event) => onChangeKeywordEnabled(event.target.checked)}
              aria-label="启用关键词候选识别"
            />
          </div>
          <p className="admin-panel-copy">
            先从已保存的公开动态中筛出可能包含日程的帖子，不调用模型。
          </p>
          <label className="admin-field">
            <span>候选关键词</span>
            <textarea
              value={keywords}
              onChange={(event) => onChangeKeywords(event.target.value)}
              rows={7}
              placeholder="每行一个关键词"
              disabled={!keywordEnabled}
            />
          </label>
        </section>
        <section className="admin-panel admin-stage-panel">
          <div className="admin-stage-number">02</div>
          <div className="admin-panel-heading">
            <div>
              <p className="admin-kicker">VISION EXTRACTION</p>
              <h3>多模态视觉抽取</h3>
            </div>
            <input
              className="admin-switch"
              type="checkbox"
              checked={visionEnabled}
              onChange={(event) => onChangeVisionEnabled(event.target.checked)}
              aria-label="启用多模态视觉抽取"
            />
          </div>
          <p className="admin-panel-copy">
            按 provider 优先级读取已缓存图片和正文，失败时自动尝试下一个可用
            provider。
          </p>
          <div className="admin-provider-route-summary">
            {providerOrder.length ? (
              providerOrder.map((id, index) => {
                const provider = providers.find((item) => item.id === id)
                return (
                  <div className="admin-route-row" key={id}>
                    <span>{String(index + 1).padStart(2, "0")}</span>
                    <strong>{provider?.name || id}</strong>
                    <small>{provider?.model || "未找到 provider"}</small>
                  </div>
                )
              })
            ) : (
              <p className="admin-empty-inline">尚未设置视觉 provider 顺序</p>
            )}
          </div>
          <p className="admin-stage-footnote">
            优先级与密钥在“模型提供商”标签中管理。
          </p>
        </section>
        <section className="admin-panel admin-content-settings-panel">
          <div className="admin-panel-heading">
            <div>
              <p className="admin-kicker">PUBLIC HIGHLIGHT</p>
              <h3>内容设置</h3>
            </div>
            <Play className="admin-panel-check" />
          </div>
          <p className="admin-panel-copy">
            选择公开页面“Recent videos”区域顶部展示的 Featured
            video；留空则按默认顺序展示。
          </p>
          <label className="admin-field">
            <span>Featured video</span>
            <select
              value={featuredVideoId}
              onChange={(event) => onChangeFeaturedVideoId(event.target.value)}
            >
              <option value="">不设置</option>
              {(videos || []).map((video) => (
                <option value={video.id} key={video.id}>
                  {video.title}
                </option>
              ))}
            </select>
          </label>
        </section>
        <div className="admin-form-actions admin-scan-actions">
          <Button type="submit" disabled={isBusy}>
            <Save /> 保存扫描设置
          </Button>
        </div>
      </form>
    </section>
  )
}

function ProviderEditor({ provider, onClose, onSaved, onError }) {
  const [form, setForm] = useState(() => ({
    ...emptyProvider,
    ...(provider || {}),
  }))
  const [saving, setSaving] = useState(false)
  const editing = Boolean(provider)
  const setField = (name) => (event) =>
    setForm((current) => ({
      ...current,
      [name]:
        event.target.type === "checkbox"
          ? event.target.checked
          : event.target.value,
    }))

  const submit = async (event) => {
    event.preventDefault()
    setSaving(true)
    onError("")
    try {
      const body = { ...form }
      delete body.clearApiKey
      if (!form.apiKey && !form.clearApiKey) delete body.apiKey
      if (form.clearApiKey) body.apiKey = null
      const result = await request(
        editing
          ? `/providers/${encodeURIComponent(provider.id)}`
          : "/providers",
        {
          method: editing ? "PUT" : "POST",
          body: JSON.stringify(body),
        },
      )
      onSaved(result.provider)
    } catch (saveError) {
      onError(saveError.message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <aside className="admin-drawer" aria-labelledby="provider-editor-title">
      <div className="admin-drawer-heading">
        <div>
          <p className="admin-kicker">PROVIDER CONFIGURATION</p>
          <h3 id="provider-editor-title">
            {editing ? "编辑模型提供商" : "新增模型提供商"}
          </h3>
        </div>
        <Button
          variant="ghost"
          size="icon"
          onClick={onClose}
          aria-label="关闭提供商编辑器"
        >
          <X />
        </Button>
      </div>
      <form className="admin-provider-form" onSubmit={submit}>
        <div className="admin-provider-form-grid">
          <label className="admin-field">
            <span>标识 ID</span>
            <input
              value={form.id}
              onChange={setField("id")}
              required
              disabled={editing}
              placeholder="provider-id"
            />
          </label>
          <label className="admin-field">
            <span>显示名称</span>
            <input
              value={form.name}
              onChange={setField("name")}
              required
              placeholder="视觉主模型"
            />
          </label>
          <label className="admin-field">
            <span>协议</span>
            <select value={form.protocol} onChange={setField("protocol")}>
              <option value="openai-responses">OpenAI Responses</option>
              <option value="openai-chat-completions">
                OpenAI Chat Completions
              </option>
            </select>
          </label>
          <label className="admin-field">
            <span>Model</span>
            <input
              value={form.model}
              onChange={setField("model")}
              required
              placeholder="gpt-4o-mini"
            />
          </label>
        </div>
        <label className="admin-field">
          <span>Base URL</span>
          <input
            type="url"
            value={form.baseUrl}
            onChange={setField("baseUrl")}
            required
            placeholder="https://api.example.invalid/v1"
          />
        </label>
        <label className="admin-field">
          <span>API Key</span>
          <input
            type="password"
            value={form.apiKey}
            onChange={setField("apiKey")}
            autoComplete="new-password"
            placeholder={editing ? "留空以保留当前密钥" : "输入后加密保存"}
          />
        </label>
        {editing && form.apiKeyConfigured ? (
          <label className="admin-toggle-line">
            <input
              type="checkbox"
              checked={form.clearApiKey}
              onChange={setField("clearApiKey")}
            />
            清除已保存的 API Key
          </label>
        ) : null}
        <div className="admin-provider-form-grid admin-provider-number-grid">
          <label className="admin-field">
            <span>超时（毫秒）</span>
            <input
              type="number"
              min="1000"
              max="120000"
              step="1000"
              value={form.timeoutMs}
              onChange={setField("timeoutMs")}
            />
          </label>
          <label className="admin-field">
            <span>失败重试</span>
            <input
              type="number"
              min="0"
              max="3"
              step="1"
              value={form.maxRetries}
              onChange={setField("maxRetries")}
            />
          </label>
        </div>
        <div className="admin-provider-flags">
          <label className="admin-toggle-line">
            <input
              type="checkbox"
              checked={form.enabled}
              onChange={setField("enabled")}
            />
            启用 provider
          </label>
          <label className="admin-toggle-line">
            <input
              type="checkbox"
              checked={form.visionCapable}
              onChange={setField("visionCapable")}
            />
            支持视觉输入
          </label>
        </div>
        <div className="admin-form-actions">
          <Button type="button" variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button type="submit" disabled={saving}>
            {saving ? <LoaderCircle className="admin-spin" /> : <Save />}保存
          </Button>
        </div>
      </form>
    </aside>
  )
}

function ProviderManager({
  providers,
  providerOrder,
  onReload,
  onNotice,
  onError,
}) {
  const [editor, setEditor] = useState(null)
  const [busy, setBusy] = useState("")
  const orderedProviders = [
    ...providerOrder
      .map((id) => providers.find((provider) => provider.id === id))
      .filter(Boolean),
    ...providers.filter((provider) => !providerOrder.includes(provider.id)),
  ]

  const saveProvider = async () => {
    setEditor(null)
    await onReload()
    onNotice("模型提供商已保存")
  }

  const move = async (index, direction) => {
    const current = orderedProviders.map((provider) => provider.id)
    const target = index + direction
    if (target < 0 || target >= current.length) return
    ;[current[index], current[target]] = [current[target], current[index]]
    setBusy("order")
    onError("")
    try {
      await request("/providers/order", {
        method: "PUT",
        body: JSON.stringify({ providerOrder: current }),
      })
      await onReload()
      onNotice("视觉模型优先级已更新")
    } catch (moveError) {
      onError(moveError.message)
    } finally {
      setBusy("")
    }
  }

  const test = async (provider) => {
    setBusy(`test-${provider.id}`)
    onError("")
    try {
      await request(`/providers/${encodeURIComponent(provider.id)}/test`, {
        method: "POST",
      })
      await onReload()
      onNotice(`${provider.name} 连接测试成功`)
    } catch (testError) {
      onError(testError.message)
    } finally {
      setBusy("")
    }
  }

  const remove = async (provider) => {
    if (!window.confirm(`删除“${provider.name}”？视觉路由中的优先级也会移除。`))
      return
    setBusy(`delete-${provider.id}`)
    onError("")
    try {
      await request(`/providers/${encodeURIComponent(provider.id)}`, {
        method: "DELETE",
      })
      await onReload()
      onNotice("模型提供商已删除")
    } catch (removeError) {
      onError(removeError.message)
    } finally {
      setBusy("")
    }
  }

  return (
    <section className="admin-view" aria-labelledby="admin-provider-title">
      <div className="admin-view-heading">
        <div>
          <p className="admin-kicker">LLM ROUTING</p>
          <h2 id="admin-provider-title">模型提供商</h2>
          <p>支持多个 OpenAI-compatible endpoint，并按优先级自动故障转移。</p>
        </div>
        <Button onClick={() => setEditor({ ...emptyProvider })}>
          <Plus /> 新增 provider
        </Button>
      </div>
      <div className="admin-provider-layout">
        <section className="admin-panel admin-provider-table-panel">
          <div className="admin-panel-heading">
            <div>
              <p className="admin-kicker">VISION ROUTE</p>
              <h3>优先级顺序</h3>
            </div>
            <Badge variant="sky">schedule_vision</Badge>
          </div>
          <div className="admin-provider-table-wrap">
            <table className="admin-provider-table">
              <thead>
                <tr>
                  <th>顺序</th>
                  <th>提供商</th>
                  <th>协议 / Model</th>
                  <th>状态</th>
                  <th aria-label="操作" />
                </tr>
              </thead>
              <tbody>
                {orderedProviders.map((provider, index) => (
                  <tr key={provider.id}>
                    <td>
                      <div className="admin-order-cell">
                        <strong>{String(index + 1).padStart(2, "0")}</strong>
                        <div>
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => move(index, -1)}
                            disabled={busy !== "" || index === 0}
                            aria-label="上移"
                            title="上移"
                          >
                            <ChevronUp />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => move(index, 1)}
                            disabled={
                              busy !== "" ||
                              index === orderedProviders.length - 1
                            }
                            aria-label="下移"
                            title="下移"
                          >
                            <ChevronDown />
                          </Button>
                        </div>
                      </div>
                    </td>
                    <td>
                      <div className="admin-provider-name">
                        <strong>{provider.name}</strong>
                        <small>{provider.id}</small>
                      </div>
                    </td>
                    <td>
                      <div className="admin-provider-meta">
                        <span>
                          {provider.protocol === "openai-chat-completions"
                            ? "Chat Completions"
                            : "Responses"}
                        </span>
                        <small>{provider.model}</small>
                      </div>
                    </td>
                    <td>
                      <div className="admin-provider-statuses">
                        <Badge variant={provider.enabled ? "mint" : "neutral"}>
                          {provider.enabled ? "启用" : "停用"}
                        </Badge>
                        {provider.visionCapable ? (
                          <Badge variant="sky">视觉</Badge>
                        ) : null}
                        <small>
                          {provider.apiKeyConfigured
                            ? "密钥已配置"
                            : "缺少密钥"}
                        </small>
                        {provider.lastStatus ? (
                          <small>
                            {provider.lastStatus === "success"
                              ? "最近成功"
                              : "最近失败"}
                          </small>
                        ) : null}
                      </div>
                    </td>
                    <td>
                      <div className="admin-table-actions">
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() =>
                            setEditor({
                              ...provider,
                              apiKey: "",
                              clearApiKey: false,
                            })
                          }
                          aria-label={`编辑 ${provider.name}`}
                          title="编辑"
                        >
                          <Pencil />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => test(provider)}
                          disabled={busy !== "" || !provider.apiKeyConfigured}
                          aria-label={`测试 ${provider.name}`}
                          title="测试连接"
                        >
                          {busy === `test-${provider.id}` ? (
                            <LoaderCircle className="admin-spin" />
                          ) : (
                            <RefreshCw />
                          )}
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => remove(provider)}
                          disabled={busy !== ""}
                          aria-label={`删除 ${provider.name}`}
                          title="删除"
                        >
                          <Trash2 />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {!orderedProviders.length ? (
              <div className="admin-empty">还没有配置 provider</div>
            ) : null}
          </div>
        </section>
        {editor ? (
          <ProviderEditor
            provider={editor.id ? editor : null}
            onClose={() => setEditor(null)}
            onSaved={saveProvider}
            onError={onError}
          />
        ) : null}
      </div>
      <p className="admin-security-note">
        <KeyRound /> API Key
        只在服务端以加密密文保存，列表和日志不会回显明文。生产环境要求 HTTPS
        Base URL。
      </p>
    </section>
  )
}

function SchedulePanel({
  pageData,
  filters,
  onFilterChange,
  onFilterSubmit,
  onPageChange,
  onNew,
  onEdit,
  onConfirm,
  onDelete,
}) {
  const events = pageData?.items || pageData?.events || []
  return (
    <section className="admin-view" aria-labelledby="admin-schedule-title">
      <div className="admin-view-heading">
        <div>
          <p className="admin-kicker">RECORDS</p>
          <h2 id="admin-schedule-title">日程管理</h2>
          <p>分页浏览自动识别结果；人工确认、编辑和删除会写入永久锁。</p>
        </div>
        <Button onClick={onNew}>
          <Plus /> 新增日程
        </Button>
      </div>
      <form className="admin-schedule-filters" onSubmit={onFilterSubmit}>
        <label className="admin-field">
          <span>搜索</span>
          <div className="admin-input-with-icon">
            <Search />
            <input
              value={filters.search}
              onChange={(event) => onFilterChange("search", event.target.value)}
              placeholder="标题、详情或状态"
            />
          </div>
        </label>
        <label className="admin-field">
          <span>来源</span>
          <select
            value={filters.provenance}
            onChange={(event) =>
              onFilterChange("provenance", event.target.value)
            }
          >
            <option value="">全部来源</option>
            <option value="automatic">自动识别</option>
            <option value="manual">人工锁定</option>
          </select>
        </label>
        <label className="admin-field">
          <span>开始日期</span>
          <input
            type="date"
            value={filters.from}
            onChange={(event) => onFilterChange("from", event.target.value)}
          />
        </label>
        <label className="admin-field">
          <span>结束日期</span>
          <input
            type="date"
            value={filters.to}
            onChange={(event) => onFilterChange("to", event.target.value)}
          />
        </label>
        <label className="admin-toggle-line admin-deleted-toggle">
          <input
            type="checkbox"
            checked={filters.includeDeleted}
            onChange={(event) =>
              onFilterChange("includeDeleted", event.target.checked)
            }
          />
          显示删除标记
        </label>
        <Button type="submit" variant="outline">
          <Search />
          筛选
        </Button>
      </form>
      <div className="admin-table-toolbar">
        <span>
          共 <strong>{pageData?.total ?? events.length}</strong> 条
        </span>
        <span>
          第 {pageData?.page || 1} / {pageData?.totalPages || 1} 页
        </span>
      </div>
      <div className="admin-schedule-table-wrap">
        <table className="admin-schedule-table">
          <thead>
            <tr>
              <th>日期 / 时间</th>
              <th>日程</th>
              <th>来源</th>
              <th>状态</th>
              <th aria-label="操作" />
            </tr>
          </thead>
          <tbody>
            {events.map((event) => (
              <tr
                key={event.id}
                className={event.deletedAt ? "is-deleted" : ""}
              >
                <td>
                  <div className="admin-date-cell">
                    <strong>{event.startsOn}</strong>
                    <small>{formatEventTime(event)}</small>
                  </div>
                </td>
                <td>
                  <div className="admin-event-table-copy">
                    <strong>{event.title}</strong>
                    <small>{event.detail || "暂无详情"}</small>
                  </div>
                </td>
                <td>
                  <Badge
                    variant={event.provenance === "manual" ? "mint" : "butter"}
                  >
                    {event.provenance === "manual" ? "人工锁定" : "自动识别"}
                  </Badge>
                  <small className="admin-source-text">{event.source}</small>
                </td>
                <td>
                  <span
                    className={
                      event.deletedAt
                        ? "admin-status-text is-danger"
                        : "admin-status-text"
                    }
                  >
                    {event.deletedAt ? "已删除" : event.status || "待确认"}
                  </span>
                </td>
                <td>
                  <div className="admin-table-actions">
                    {event.provenance !== "manual" && !event.deletedAt ? (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => onConfirm(event)}
                      >
                        <Check />
                        确认
                      </Button>
                    ) : null}
                    {!event.deletedAt ? (
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => onEdit(event)}
                        aria-label={`编辑 ${event.title}`}
                        title="编辑"
                      >
                        <Pencil />
                      </Button>
                    ) : null}
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => onDelete(event)}
                      aria-label={`删除 ${event.title}`}
                      title="删除"
                    >
                      <Trash2 />
                    </Button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {!events.length ? (
          <div className="admin-empty">当前筛选没有日程记录</div>
        ) : null}
      </div>
      <div className="admin-pagination">
        <Button
          variant="outline"
          size="icon"
          onClick={() => onPageChange((pageData?.page || 1) - 1)}
          disabled={!pageData?.hasPrevious}
          aria-label="上一页"
          title="上一页"
        >
          <ChevronLeft />
        </Button>
        <span>第 {pageData?.page || 1} 页</span>
        <Button
          variant="outline"
          size="icon"
          onClick={() => onPageChange((pageData?.page || 1) + 1)}
          disabled={!pageData?.hasNext}
          aria-label="下一页"
          title="下一页"
        >
          <ChevronRight />
        </Button>
      </div>
      {pageData?.total ? (
        <p className="admin-table-footnote">
          每页显示 {pageData.pageSize || 20} 条，服务端只返回当前页。
        </p>
      ) : null}
    </section>
  )
}

function AdminAppModern() {
  const [session, setSession] = useState(null)
  const [config, setConfig] = useState(null)
  const [eventsPage, setEventsPage] = useState({
    items: [],
    events: [],
    page: 1,
    pageSize: 20,
    total: 0,
    totalPages: 1,
  })
  const [eventFilters, setEventFilters] = useState({
    search: "",
    provenance: "",
    from: "",
    to: "",
    includeDeleted: false,
  })
  const [videos, setVideos] = useState([])
  const [profileMedia, setProfileMedia] = useState({
    items: [],
    active: { avatar: null, banner: null },
  })
  const [providers, setProviders] = useState([])
  const [providerOrder, setProviderOrder] = useState([])
  const [keywordEnabled, setKeywordEnabled] = useState(true)
  const [visionEnabled, setVisionEnabled] = useState(true)
  const [scheduleKeywords, setScheduleKeywords] = useState("")
  const [featuredVideoId, setFeaturedVideoId] = useState("")
  const [activeTab, setActiveTab] = useState("overview")
  const [editor, setEditor] = useState(null)
  const [jobs, setJobs] = useState([])
  const [runs, setRuns] = useState([])
  const [isLoading, setIsLoading] = useState(true)
  const [isBusy, setIsBusy] = useState(false)
  const [notice, setNotice] = useState("")
  const [error, setError] = useState("")

  useEffect(() => {
    document.documentElement.lang = "zh-CN"
    document.title = "管理界面 / Kano status board"
    let robots = document.querySelector('meta[name="robots"]')
    const previousContent = robots?.getAttribute("content") ?? null
    const created = !robots
    if (!robots) {
      robots = document.createElement("meta")
      robots.setAttribute("name", "robots")
      document.head.append(robots)
    }
    robots.setAttribute("content", "noindex, nofollow")
    return () => {
      if (created) robots.remove()
      else if (previousContent == null) robots.removeAttribute("content")
      else robots.setAttribute("content", previousContent)
    }
  }, [])

  const loadEventsPage = useCallback(async (query = {}) => {
    const params = new URLSearchParams({
      page: String(query.page || 1),
      pageSize: String(query.pageSize || 20),
    })
    if (query.search) params.set("search", query.search)
    if (query.provenance) params.set("provenance", query.provenance)
    if (query.from) params.set("from", query.from)
    if (query.to) params.set("to", query.to)
    if (query.includeDeleted) params.set("includeDeleted", "1")
    const payload = await request(`/events?${params.toString()}`)
    setEventsPage(payload)
    return payload
  }, [])

  const loadSync = useCallback(async () => {
    const [jobPayload, runPayload] = await Promise.all([
      request("/sync/jobs?limit=8"),
      request("/sync/runs?limit=8"),
    ])
    setJobs(jobPayload.jobs || [])
    setRuns(runPayload.runs || [])
  }, [])

  const loadData = useCallback(async () => {
    const [
      configPayload,
      eventsPayload,
      videosPayload,
      mediaPayload,
      jobPayload,
      runPayload,
    ] = await Promise.all([
      request("/config"),
      request("/events?page=1&pageSize=20"),
      request("/videos"),
      request("/profile-media"),
      request("/sync/jobs?limit=8"),
      request("/sync/runs?limit=8"),
    ])
    setConfig(configPayload)
    setKeywordEnabled(configPayload.scheduleKeywordEnabled !== false)
    setVisionEnabled(configPayload.scheduleVisionEnabled !== false)
    setScheduleKeywords((configPayload.scheduleKeywords || []).join("\n"))
    setFeaturedVideoId(configPayload.featuredVideoId || "")
    setProviders(configPayload.providers || [])
    setProviderOrder(configPayload.providerOrder || [])
    setEventsPage(eventsPayload)
    setEventFilters({
      search: "",
      provenance: "",
      from: "",
      to: "",
      includeDeleted: false,
    })
    setVideos(videosPayload.videos || [])
    setProfileMedia(mediaPayload)
    setJobs(jobPayload.jobs || [])
    setRuns(runPayload.runs || [])
  }, [])

  useEffect(() => {
    let active = true
    request("/session")
      .then(async (payload) => {
        if (!active) return
        setSession(payload)
        if (payload.authenticated) await loadData()
      })
      .catch((loadError) => active && setError(loadError.message))
      .finally(() => active && setIsLoading(false))
    return () => {
      active = false
    }
  }, [loadData])

  useEffect(() => {
    if (!session?.authenticated) return undefined
    const timer = window.setInterval(() => {
      loadSync().catch((syncError) => setError(syncError.message))
    }, 5000)
    return () => window.clearInterval(timer)
  }, [session?.authenticated, loadSync])

  const login = async (password) => {
    const payload = await request("/login", {
      method: "POST",
      body: JSON.stringify({ password }),
    })
    setSession(payload)
    await loadData()
  }

  const logout = async () => {
    setError("")
    try {
      await request("/logout", { method: "POST" })
      setSession((current) => ({ ...current, authenticated: false }))
      setConfig(null)
    } catch (logoutError) {
      setError(logoutError.message)
    }
  }

  const saveConfig = async (event) => {
    event.preventDefault()
    setIsBusy(true)
    setError("")
    try {
      const payload = await request("/config", {
        method: "PUT",
        body: JSON.stringify({
          scheduleKeywordEnabled: keywordEnabled,
          scheduleVisionEnabled: visionEnabled,
          scheduleKeywords,
          featuredVideoId: featuredVideoId || null,
        }),
      })
      setConfig(payload)
      setProviders(payload.providers || [])
      setProviderOrder(payload.providerOrder || [])
      setNotice("扫描设置已保存")
    } catch (saveError) {
      setError(saveError.message)
    } finally {
      setIsBusy(false)
    }
  }

  const refreshProviderData = async () => {
    const payload = await request("/config")
    setConfig(payload)
    setProviders(payload.providers || [])
    setProviderOrder(payload.providerOrder || [])
  }

  const startJob = async (kind) => {
    setIsBusy(true)
    setError("")
    try {
      const payload = await request(
        kind === "scan" ? "/scan/automatic" : "/sync",
        { method: "POST" },
      )
      await loadSync()
      setNotice(payload.accepted ? "任务已加入队列" : "已有任务正在运行")
    } catch (jobError) {
      setError(jobError.message)
    } finally {
      setIsBusy(false)
    }
  }

  const replaceEvent = async () => {
    setEditor(null)
    await loadEventsPage({ page: eventsPage.page || 1, ...eventFilters })
    setNotice("日程已保存并锁定为人工确认")
  }

  const confirmEvent = async (event) => {
    setError("")
    try {
      await request(`/events/${encodeURIComponent(event.id)}/confirm`, {
        method: "POST",
      })
      await loadEventsPage({ page: eventsPage.page || 1, ...eventFilters })
      setNotice("日程已人工确认")
    } catch (confirmError) {
      setError(confirmError.message)
    }
  }

  const removeEvent = async (event) => {
    if (
      !window.confirm(
        `删除“${event.title}”？该记录会保留为人工锁定的删除标记。`,
      )
    )
      return
    setError("")
    try {
      await request(`/events/${encodeURIComponent(event.id)}`, {
        method: "DELETE",
      })
      await loadEventsPage({ page: eventsPage.page || 1, ...eventFilters })
      setNotice("日程已删除并保留人工锁")
    } catch (removeError) {
      setError(removeError.message)
    }
  }

  const applyFilters = async (event) => {
    event.preventDefault()
    await loadEventsPage({ page: 1, ...eventFilters })
  }

  const changePage = async (page) => {
    await loadEventsPage({ page, ...eventFilters })
  }

  if (isLoading || !session)
    return (
      <main className="admin-loading">
        <LoaderCircle className="admin-spin" />
        正在读取管理状态
      </main>
    )
  if (!session.authenticated) return <Login onLogin={login} />

  const activeJob = jobs.find(
    (job) => job.status === "running" || job.status === "queued",
  )
  const latestRun = runs[0] || null

  return (
    <div className="admin-console">
      <aside className="admin-sidebar">
        <div className="admin-brand">
          <span className="admin-brand-mark">K</span>
          <div>
            <strong>KANO</strong>
            <small>STATUS BOARD</small>
          </div>
        </div>
        <nav className="admin-nav" aria-label="管理标签">
          <p className="admin-nav-label">WORKSPACE</p>
          {adminTabs.map((tab) => {
            const Icon = tab.icon
            return (
              <button
                type="button"
                key={tab.id}
                className={`admin-nav-item${activeTab === tab.id ? " is-active" : ""}`}
                onClick={() => setActiveTab(tab.id)}
              >
                <Icon />
                <span>{tab.label}</span>
                {tab.id === "schedules" && eventsPage.total ? (
                  <em>{eventsPage.total}</em>
                ) : null}
              </button>
            )
          })}
        </nav>
        <div className="admin-sidebar-footer">
          <div className="admin-sidebar-status">
            <span className="admin-status-dot" />
            服务在线
          </div>
          <a href="/" className="admin-back-link">
            返回公开页面 <ChevronRight />
          </a>
        </div>
      </aside>
      <div className="admin-console-body">
        <header className="admin-console-topbar">
          <div>
            <p className="admin-kicker">CONTROL CENTER</p>
            <h1>
              {adminTabs.find((tab) => tab.id === activeTab)?.label || "概览"}
            </h1>
          </div>
          <div className="admin-topbar-actions">
            {activeJob ? (
              <Badge variant="butter">
                <span className="admin-pulse-dot" />
                {activeJob.kind === "scan" ? "自动扫描中" : "同步中"}
              </Badge>
            ) : null}
            <Badge variant={session.mode === "production" ? "coral" : "mint"}>
              {session.mode === "production" ? "生产模式" : "开发模式"}
            </Badge>
            {session.requiresPassword ? (
              <Button variant="outline" size="sm" onClick={logout}>
                <LogOut />
                退出
              </Button>
            ) : null}
          </div>
        </header>
        {notice ? (
          <div className="admin-notice" role="status">
            <Check />
            {notice}
            <button
              type="button"
              onClick={() => setNotice("")}
              aria-label="关闭提示"
            >
              <X />
            </button>
          </div>
        ) : null}
        {error ? (
          <div className="admin-error" role="alert">
            {error}
          </div>
        ) : null}
        <main className="admin-console-main">
          {activeTab === "overview" ? (
            <OverviewPanel
              eventsPage={eventsPage}
              providers={providers}
              config={config}
              latestRun={latestRun}
              jobs={jobs}
              onStartSync={() => startJob("sync")}
              onReload={async () => {
                await loadData()
                setNotice("状态已刷新")
              }}
              isBusy={isBusy}
            />
          ) : null}
          {activeTab === "schedules" ? (
            <SchedulePanel
              pageData={eventsPage}
              filters={eventFilters}
              onFilterChange={(name, value) =>
                setEventFilters((current) => ({ ...current, [name]: value }))
              }
              onFilterSubmit={applyFilters}
              onPageChange={changePage}
              onNew={() => setEditor({ type: "new" })}
              onEdit={(event) => setEditor({ type: "edit", event })}
              onConfirm={confirmEvent}
              onDelete={removeEvent}
            />
          ) : null}
          {activeTab === "scan" ? (
            <ScanSettingsPanel
              keywordEnabled={keywordEnabled}
              visionEnabled={visionEnabled}
              keywords={scheduleKeywords}
              providerOrder={providerOrder}
              providers={providers}
              videos={videos}
              featuredVideoId={featuredVideoId}
              onChangeKeywordEnabled={setKeywordEnabled}
              onChangeVisionEnabled={setVisionEnabled}
              onChangeKeywords={setScheduleKeywords}
              onChangeFeaturedVideoId={setFeaturedVideoId}
              onSave={saveConfig}
              onRunScan={() => startJob("scan")}
              isBusy={isBusy || Boolean(activeJob)}
            />
          ) : null}
          {activeTab === "providers" ? (
            <ProviderManager
              providers={providers}
              providerOrder={providerOrder}
              onReload={refreshProviderData}
              onNotice={setNotice}
              onError={setError}
            />
          ) : null}
          {activeTab === "media" ? (
            <ProfileMediaManager
              value={profileMedia}
              onChange={setProfileMedia}
              onNotice={setNotice}
              onError={setError}
            />
          ) : null}
        </main>
        {editor ? (
          <EventEditor
            event={editor.type === "edit" ? editor.event : null}
            onClose={() => setEditor(null)}
            onSaved={replaceEvent}
          />
        ) : null}
      </div>
    </div>
  )
}

export function AdminApp() {
  return <AdminAppModern />
}
