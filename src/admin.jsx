import { useCallback, useEffect, useMemo, useState } from "react"
import {
  CalendarClock,
  Check,
  KeyRound,
  LoaderCircle,
  LockKeyhole,
  LogOut,
  Pencil,
  Plus,
  Save,
  Settings2,
  Trash2,
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

export function AdminApp() {
  const [session, setSession] = useState(null)
  const [config, setConfig] = useState(null)
  const [events, setEvents] = useState([])
  const [model, setModel] = useState("")
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
    const [configPayload, eventsPayload] = await Promise.all([
      request("/config"),
      request("/events"),
    ])
    setConfig(configPayload)
    setModel(configPayload.llmModel)
    setEvents(eventsPayload.events)
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
    } catch (logoutError) {
      setError(logoutError.message)
    }
  }

  const saveModel = async (event) => {
    event.preventDefault()
    setError("")
    try {
      const payload = await request("/config", {
        method: "PUT",
        body: JSON.stringify({ llmModel: model }),
      })
      setConfig(payload)
      setNotice("模型设置已保存")
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
              <h2 id="llm-settings-title">日程识别模型</h2>
            </div>
          </div>
          <form className="admin-model-form" onSubmit={saveModel}>
            <label>
              <span>模型名称</span>
              <input
                value={model}
                onChange={(event) => setModel(event.target.value)}
                required
              />
            </label>
            <Button type="submit">
              <Save /> 保存模型
            </Button>
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
