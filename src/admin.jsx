import { useCallback, useEffect, useMemo, useState } from "react"
import {
  CalendarClock,
  Check,
  Download,
  Image as ImageIcon,
  KeyRound,
  LoaderCircle,
  LockKeyhole,
  LogOut,
  Pencil,
  Plus,
  Save,
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

export function AdminApp() {
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
