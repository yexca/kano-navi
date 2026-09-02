import { useCallback, useEffect, useState } from "react"
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
import { useAppSettings } from "@/app-settings"
import { PreferenceControls } from "@/components/preference-controls"
import "./admin.css"

const emptyEvent = {
  title: "",
  detail: "",
  startsOn: "",
  startTime: "",
  endTime: "",
  timePrecision: "exact",
  status: "",
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

const adminIntlLocales = {
  "zh-CN": "zh-CN",
  ja: "ja-JP",
  en: "en-US",
}
const adminDateTimeFormatters = new Map()

function formatAdminDateTime(value, locale) {
  if (!value) return ""
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ""
  if (!adminDateTimeFormatters.has(locale)) {
    adminDateTimeFormatters.set(
      locale,
      new Intl.DateTimeFormat(adminIntlLocales[locale] || adminIntlLocales.en, {
        timeZone: "Asia/Tokyo",
        dateStyle: "medium",
        timeStyle: "short",
      }),
    )
  }
  return adminDateTimeFormatters.get(locale).format(date)
}

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
    error.code = payload.code || payload.error || "request_failed"
    throw error
  }
  return payload
}

const errorMessageKeys = {
  admin_auth_required: "errors.authRequired",
  invalid_credentials: "errors.invalidCredentials",
  too_many_attempts: "errors.tooManyAttempts",
  csrf_origin_mismatch: "errors.csrf",
  event_not_found: "errors.notFound",
  llm_provider_not_found: "errors.notFound",
  profile_media_not_found: "errors.notFound",
  profile_media_not_ready: "errors.notFound",
  sync_job_not_found: "errors.notFound",
  sync_jobs_unavailable: "errors.syncUnavailable",
}

function adminMessage(key, values = {}) {
  return { key, values }
}

function renderAdminMessage(message, t) {
  if (!message) return ""
  if (typeof message === "string") return message
  if (message.key) return t(message.key, message.values)
  return message.text || ""
}

function formatAdminError(error) {
  const key = errorMessageKeys[error?.code]
  if (key) return adminMessage(key)
  if (error?.status === 401) return adminMessage("errors.authRequired")
  if (error?.status >= 500) return adminMessage("errors.requestFailed")
  if (!error?.message || /^API \d+$/u.test(error.message)) {
    return adminMessage("errors.requestFailed")
  }
  if (error?.name === "TypeError" && /fetch|network/i.test(error.message)) {
    return adminMessage("errors.network")
  }
  if (/[぀-ヿ㐀-鿿]/u.test(error.message)) {
    return adminMessage("errors.requestFailed")
  }
  return { text: error.message }
}

function Login({ onLogin }) {
  const { t } = useAppSettings()
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
          ? adminMessage("admin.auth.tooManyAttempts")
          : loginError.status === 401 ||
              loginError.code === "invalid_credentials"
            ? adminMessage("admin.auth.invalidPassword")
            : formatAdminError(loginError),
      )
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <main className="admin-login-shell">
      <div className="admin-login-toolbar">
        <PreferenceControls />
      </div>
      <form className="admin-login" onSubmit={submit}>
        <span className="admin-login-icon" aria-hidden="true">
          <LockKeyhole />
        </span>
        <p className="admin-kicker">{t("admin.brand.kanoStatusBoard")}</p>
        <h1>{t("admin.auth.title")}</h1>
        <label htmlFor="admin-password">{t("admin.auth.password")}</label>
        <input
          id="admin-password"
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          required
          autoFocus
        />
        {error ? (
          <p className="admin-form-error">{renderAdminMessage(error, t)}</p>
        ) : null}
        <Button type="submit" disabled={isSubmitting}>
          {isSubmitting ? (
            <LoaderCircle className="admin-spin" />
          ) : (
            <KeyRound />
          )}
          {t("admin.auth.login")}
        </Button>
      </form>
    </main>
  )
}

function EventEditor({ event, onClose, onSaved }) {
  const { t } = useAppSettings()
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
      setError(formatAdminError(saveError))
    } finally {
      setIsSaving(false)
    }
  }

  return (
    <section className="admin-editor" aria-labelledby="event-editor-title">
      <div className="admin-editor-header">
        <div>
          <p className="admin-kicker">{t("admin.event.kicker")}</p>
          <h2 id="event-editor-title">
            {t(editing ? "admin.event.editTitle" : "admin.event.newTitle")}
          </h2>
        </div>
        <Button
          variant="ghost"
          size="icon"
          onClick={onClose}
          aria-label={t("admin.action.close")}
        >
          <X />
        </Button>
      </div>
      <form className="admin-event-form" onSubmit={submit}>
        <label className="admin-field admin-field-wide">
          <span>{t("admin.event.title")}</span>
          <input
            value={form.title}
            onChange={setField("title")}
            maxLength={240}
            required
          />
        </label>
        <label className="admin-field admin-field-wide">
          <span>{t("admin.event.detail")}</span>
          <textarea
            value={form.detail}
            onChange={setField("detail")}
            maxLength={500}
            rows={3}
          />
        </label>
        <label className="admin-field">
          <span>{t("admin.event.date")}</span>
          <input
            type="date"
            value={form.startsOn}
            onChange={setField("startsOn")}
            required
          />
        </label>
        <label className="admin-field">
          <span>{t("admin.event.startTime")}</span>
          <input
            type="time"
            value={form.startTime}
            onChange={setField("startTime")}
          />
        </label>
        <label className="admin-field">
          <span>{t("admin.event.endTime")}</span>
          <input
            type="time"
            value={form.endTime}
            onChange={setField("endTime")}
          />
        </label>
        <label className="admin-field">
          <span>{t("admin.event.timePrecision")}</span>
          <select
            value={form.startTime ? form.timePrecision : "unknown"}
            onChange={setField("timePrecision")}
            disabled={!form.startTime}
          >
            <option value="exact">{t("admin.event.precision.exact")}</option>
            <option value="approximate">
              {t("admin.event.precision.approximate")}
            </option>
            <option value="unknown">
              {t("admin.event.precision.unknown")}
            </option>
          </select>
        </label>
        <label className="admin-field">
          <span>{t("admin.event.type")}</span>
          <select value={form.eventType} onChange={setField("eventType")}>
            <option value="event">{t("admin.event.type.event")}</option>
            <option value="stream">{t("admin.event.type.stream")}</option>
            <option value="member">{t("admin.event.type.member")}</option>
            <option value="release">{t("admin.event.type.release")}</option>
            <option value="appearance">
              {t("admin.event.type.appearance")}
            </option>
          </select>
        </label>
        <label className="admin-field">
          <span>{t("admin.event.status")}</span>
          <input
            value={form.status}
            onChange={setField("status")}
            maxLength={80}
          />
        </label>
        <label className="admin-field admin-field-wide">
          <span>{t("admin.event.publicUrl")}</span>
          <input
            type="url"
            value={form.url}
            onChange={setField("url")}
            maxLength={2000}
          />
        </label>
        {error ? (
          <p className="admin-form-error admin-field-wide">
            {renderAdminMessage(error, t)}
          </p>
        ) : null}
        <div className="admin-form-actions admin-field-wide">
          <Button type="button" variant="outline" onClick={onClose}>
            {t("admin.action.cancel")}
          </Button>
          <Button type="submit" disabled={isSaving}>
            {isSaving ? <LoaderCircle className="admin-spin" /> : <Save />}
            {t("admin.event.saveAndConfirm")}
          </Button>
        </div>
      </form>
    </section>
  )
}

function formatEventTime(event, t) {
  if (!event.startsAt) return t("admin.event.timeUnknown")
  const parts = japanParts(event.startsAt)
  return parts
    ? `${parts.hour}:${parts.minute} ${t("common.timezoneShort")}`
    : t("admin.event.timeUnknown")
}

const profileSlotKeys = {
  avatar: "admin.profile.avatar",
  banner: "admin.profile.banner",
}

function profileSlotLabel(slot, t) {
  return t(profileSlotKeys[slot] || "admin.profile.avatar")
}

function profileSourceLabel(source, t) {
  if (source === "youtube") return t("admin.profile.source.youtube")
  if (source === "upload") return t("admin.profile.source.upload")
  return t("admin.profile.source.x")
}

function ProfileMediaManager({ value, onChange, onNotice, onError }) {
  const { t } = useAppSettings()
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
        adminMessage("admin.profile.candidateUpdated", {
          source: profileSourceLabel(targetSource, t),
          slot: profileSlotLabel(targetSlot, t),
        }),
      )
    } catch (discoverError) {
      onError(formatAdminError(discoverError))
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
      onNotice(adminMessage("admin.profile.candidateAdded"))
    } catch (addError) {
      onError(formatAdminError(addError))
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
        throw Object.assign(
          new Error(
            payload.message || payload.error || `API ${response.status}`,
          ),
          { code: payload.code || payload.error, status: response.status },
        )
      applyPayload(payload)
      setSlot(targetSlot)
      onNotice(
        adminMessage("admin.profile.candidateUploaded", {
          slot: profileSlotLabel(targetSlot, t),
        }),
      )
    } catch (uploadError) {
      onError(formatAdminError(uploadError))
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
          ? adminMessage("admin.profile.switched", {
              slot: profileSlotLabel(item.slot, t),
            })
          : adminMessage("admin.profile.downloaded"),
      )
    } catch (mutateError) {
      onError(formatAdminError(mutateError))
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
          <p className="admin-kicker">{t("admin.profile.kicker")}</p>
          <h2 id="profile-media-title">{t("admin.profile.title")}</h2>
        </div>
      </div>
      <p className="admin-profile-help">{t("admin.profile.help")}</p>
      <div className="admin-profile-slots">
        {Object.keys(profileSlotKeys).map((targetSlot) => {
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
                  <strong>{profileSlotLabel(targetSlot, t)}</strong>
                  <small>
                    {current
                      ? `${profileSourceLabel(current.source, t)} · ${t("admin.profile.current")}`
                      : t("admin.profile.notSelected")}
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
                  {t("admin.profile.discover")} {t("admin.profile.source.x")}
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
                  {t("admin.profile.discover")}{" "}
                  {t("admin.profile.source.youtube")}
                </Button>
                <label className="admin-upload-button">
                  <Upload />
                  {t("admin.action.upload")}
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
                  aria-label={t("admin.profile.sourceLabel", {
                    slot: profileSlotLabel(targetSlot, t),
                  })}
                >
                  <option value="x">{t("admin.profile.imageUrl.x")}</option>
                  <option value="youtube">
                    {t("admin.profile.imageUrl.youtube")}
                  </option>
                </select>
                <input
                  type="url"
                  value={slot === targetSlot ? sourceUrl : ""}
                  onFocus={() => setSlot(targetSlot)}
                  onChange={(event) => {
                    setSlot(targetSlot)
                    setSourceUrl(event.target.value)
                  }}
                  placeholder={t("admin.profile.imageUrl.placeholder")}
                  aria-label={t("admin.profile.imageUrl.aria", {
                    slot: profileSlotLabel(targetSlot, t),
                  })}
                />
                <Button
                  type="submit"
                  size="icon"
                  variant="outline"
                  disabled={busyKey !== ""}
                  aria-label={t("admin.action.addImageUrl")}
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
                      <strong>{profileSourceLabel(item.source, t)}</strong>
                      <small>
                        {item.status === "ready"
                          ? t("admin.profile.status.ready")
                          : item.status === "failed"
                            ? t("admin.profile.status.failed")
                            : t("admin.profile.status.pending")}
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
                          aria-label={t("admin.action.download")}
                          title={t("admin.action.download")}
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
                          <Check /> {t("admin.action.select")}
                        </Button>
                      ) : item.isActive ? (
                        <Badge variant="mint">
                          {t("admin.profile.current")}
                        </Badge>
                      ) : null}
                    </div>
                  </div>
                ))}
                {!slotItems.length ? (
                  <p className="admin-profile-empty">
                    {t("admin.profile.noCandidates")}
                  </p>
                ) : null}
              </div>
            </article>
          )
        })}
      </div>
      <p className="admin-profile-footnote">{t("admin.profile.footnote")}</p>
    </section>
  )
}

const adminTabs = [
  { id: "overview", labelKey: "admin.nav.overview", icon: Gauge },
  { id: "schedules", labelKey: "admin.nav.schedules", icon: CalendarClock },
  { id: "scan", labelKey: "admin.nav.scan", icon: ScanSearch },
  { id: "providers", labelKey: "admin.nav.providers", icon: Settings2 },
  { id: "media", labelKey: "admin.nav.media", icon: ImageIcon },
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

function syncStatusLabel(status, t) {
  const normalized = String(status || "")
    .trim()
    .toLowerCase()
  if (["running", "queued", "进行中", "运行中"].includes(normalized))
    return t("admin.status.running")
  if (["completed", "success", "已完成", "成功"].includes(normalized)) {
    return t("admin.status.completed")
  }
  if (["partial", "部分完成"].includes(normalized)) {
    return t("admin.status.partial")
  }
  if (["failed", "failure", "失败"].includes(normalized)) {
    return t("admin.status.failed")
  }
  return t("admin.status.unknown")
}

function syncRunMessage(run, t) {
  const message = String(run?.message || "").trim()
  if (!message) return t("admin.overview.latestSync")
  if (message === "同步完成" || message === "Sync completed") {
    return t("admin.overview.syncComplete")
  }
  if (
    message.includes("部分数据源不可用") ||
    /some sources were unavailable/i.test(message)
  ) {
    return t("admin.overview.syncPartial")
  }
  if (
    message === "同步异常终止，保留已有快照" ||
    /sync stopped unexpectedly/i.test(message)
  ) {
    return t("admin.overview.syncFailed")
  }
  if (message === "自动扫描完成" || message === "Automatic scan completed") {
    return t("admin.overview.scanComplete")
  }
  if (
    message.includes("自动扫描完成，但部分候选失败") ||
    /automatic scan completed with some candidate failures/i.test(message)
  ) {
    return t("admin.overview.scanPartial")
  }
  if (
    message === "自动扫描异常终止，保留已有快照" ||
    /automatic scan stopped unexpectedly/i.test(message)
  ) {
    return t("admin.overview.scanFailed")
  }
  if (/[぀-ヿ㐀-鿿]/u.test(message)) {
    return t(
      ["schedule", "scan"].includes(String(run?.source || "").toLowerCase())
        ? "admin.overview.unknownScan"
        : "admin.overview.unknownSync",
    )
  }
  return message
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
  const { locale, t } = useAppSettings()
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
          <p className="admin-kicker">{t("admin.overview.kicker")}</p>
          <h2 id="admin-overview-title">{t("admin.overview.title")}</h2>
          <p>{t("admin.overview.description")}</p>
        </div>
        <div className="admin-heading-actions">
          <Button variant="outline" onClick={onReload} disabled={isBusy}>
            <RefreshCw /> {t("admin.overview.refresh")}
          </Button>
          <Button onClick={onStartSync} disabled={Boolean(activeJob) || isBusy}>
            {isBusy ? <LoaderCircle className="admin-spin" /> : <Play />}
            {t("admin.overview.startSync")}
          </Button>
        </div>
      </div>
      <div className="admin-metrics-grid">
        <AdminMetric
          label={t("admin.overview.totalSchedules")}
          value={eventsPage?.total ?? events.length}
          note={t("admin.overview.totalSchedulesNote")}
          tone="coral"
        />
        <AdminMetric
          label={t("admin.overview.manualLocked")}
          value={manualCount}
          note={t("admin.overview.manualLockedNote")}
          tone="mint"
        />
        <AdminMetric
          label={t("admin.overview.visionProviders")}
          value={
            providers.filter(
              (provider) => provider.enabled && provider.visionCapable,
            ).length
          }
          note={t("admin.overview.configured", { count: providers.length })}
          tone="butter"
        />
        <AdminMetric
          label={t("admin.overview.scanStage")}
          value={t(
            config?.scheduleVisionEnabled
              ? "admin.overview.enabled"
              : "admin.overview.paused",
          )}
          note={
            config?.scheduleKeywordEnabled
              ? t("admin.overview.keywordEnabled")
              : t("admin.overview.keywordPaused")
          }
          tone="sky"
        />
      </div>
      <div className="admin-overview-columns">
        <section className="admin-panel admin-panel-flat">
          <div className="admin-panel-heading">
            <div>
              <p className="admin-kicker">{t("admin.overview.syncKicker")}</p>
              <h3>{t("admin.overview.syncTitle")}</h3>
            </div>
            {activeJob ? (
              <Badge variant="butter">
                {syncStatusLabel(activeJob.status, t)}
              </Badge>
            ) : null}
          </div>
          {latestRun ? (
            <div className="admin-activity-row">
              <div>
                <strong>{syncRunMessage(latestRun, t)}</strong>
                <small>
                  {latestRun.triggeredBy === "mcp"
                    ? t("admin.overview.triggeredMcp")
                    : t("admin.overview.triggeredScheduled")}
                  {latestRun.finishedAt
                    ? ` · ${formatAdminDateTime(latestRun.finishedAt, locale)}`
                    : ""}
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
                {syncStatusLabel(latestRun.status, t)}
              </Badge>
            </div>
          ) : (
            <p className="admin-empty-inline">{t("admin.overview.noSync")}</p>
          )}
          {activeJob ? (
            <div className="admin-activity-row admin-activity-row-muted">
              <div>
                <strong>
                  {activeJob.kind === "scan"
                    ? t("admin.overview.autoScan")
                    : t("admin.overview.fullSync")}
                </strong>
                <small>{activeJob.id}</small>
              </div>
              <span className="admin-live-dot">
                {t("admin.overview.running")}
              </span>
            </div>
          ) : null}
        </section>
        <section className="admin-panel admin-panel-flat">
          <div className="admin-panel-heading">
            <div>
              <p className="admin-kicker">{t("admin.overview.notesKicker")}</p>
              <h3>{t("admin.overview.notesTitle")}</h3>
            </div>
            <CheckCircle2 className="admin-panel-check" />
          </div>
          <ul className="admin-boundary-list">
            <li>{t("admin.overview.boundaryOne")}</li>
            <li>{t("admin.overview.boundaryTwo")}</li>
            <li>{t("admin.overview.boundaryThree")}</li>
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
  const { t } = useAppSettings()
  return (
    <section className="admin-view" aria-labelledby="admin-scan-title">
      <div className="admin-view-heading">
        <div>
          <p className="admin-kicker">{t("admin.scan.kicker")}</p>
          <h2 id="admin-scan-title">{t("admin.scan.title")}</h2>
          <p>{t("admin.scan.description")}</p>
        </div>
        <Button
          onClick={onRunScan}
          disabled={isBusy || !keywordEnabled || !visionEnabled}
        >
          {isBusy ? <LoaderCircle className="admin-spin" /> : <ScanSearch />}
          {t("admin.scan.run")}
        </Button>
      </div>
      <form className="admin-scan-grid" onSubmit={onSave}>
        <section className="admin-panel admin-stage-panel">
          <div className="admin-stage-number">01</div>
          <div className="admin-panel-heading">
            <div>
              <p className="admin-kicker">{t("admin.scan.keywordKicker")}</p>
              <h3>{t("admin.scan.keywordTitle")}</h3>
            </div>
            <input
              className="admin-switch"
              type="checkbox"
              checked={keywordEnabled}
              onChange={(event) => onChangeKeywordEnabled(event.target.checked)}
              aria-label={t("admin.scan.keywordAria")}
            />
          </div>
          <p className="admin-panel-copy">{t("admin.scan.keywordCopy")}</p>
          <label className="admin-field">
            <span>{t("admin.scan.keywordLabel")}</span>
            <textarea
              value={keywords}
              onChange={(event) => onChangeKeywords(event.target.value)}
              rows={7}
              placeholder={t("admin.scan.keywordPlaceholder")}
              disabled={!keywordEnabled}
            />
          </label>
        </section>
        <section className="admin-panel admin-stage-panel">
          <div className="admin-stage-number">02</div>
          <div className="admin-panel-heading">
            <div>
              <p className="admin-kicker">{t("admin.scan.visionKicker")}</p>
              <h3>{t("admin.scan.visionTitle")}</h3>
            </div>
            <input
              className="admin-switch"
              type="checkbox"
              checked={visionEnabled}
              onChange={(event) => onChangeVisionEnabled(event.target.checked)}
              aria-label={t("admin.scan.visionAria")}
            />
          </div>
          <p className="admin-panel-copy">{t("admin.scan.visionCopy")}</p>
          <div className="admin-provider-route-summary">
            {providerOrder.length ? (
              providerOrder.map((id, index) => {
                const provider = providers.find((item) => item.id === id)
                return (
                  <div className="admin-route-row" key={id}>
                    <span>{String(index + 1).padStart(2, "0")}</span>
                    <strong>{provider?.name || id}</strong>
                    <small>
                      {provider?.model || t("admin.scan.providerMissing")}
                    </small>
                  </div>
                )
              })
            ) : (
              <p className="admin-empty-inline">
                {t("admin.scan.noProviderOrder")}
              </p>
            )}
          </div>
          <p className="admin-stage-footnote">{t("admin.scan.providerNote")}</p>
        </section>
        <section className="admin-panel admin-content-settings-panel">
          <div className="admin-panel-heading">
            <div>
              <p className="admin-kicker">{t("admin.scan.highlightKicker")}</p>
              <h3>{t("admin.scan.contentTitle")}</h3>
            </div>
            <Play className="admin-panel-check" />
          </div>
          <p className="admin-panel-copy">{t("admin.scan.contentCopy")}</p>
          <label className="admin-field">
            <span>{t("admin.scan.featuredVideo")}</span>
            <select
              value={featuredVideoId}
              onChange={(event) => onChangeFeaturedVideoId(event.target.value)}
            >
              <option value="">{t("admin.scan.notSet")}</option>
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
            <Save /> {t("admin.scan.save")}
          </Button>
        </div>
      </form>
    </section>
  )
}

function ProviderEditor({ provider, onClose, onSaved, onError }) {
  const { t } = useAppSettings()
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
      onError(formatAdminError(saveError))
    } finally {
      setSaving(false)
    }
  }

  return (
    <aside className="admin-drawer" aria-labelledby="provider-editor-title">
      <div className="admin-drawer-heading">
        <div>
          <p className="admin-kicker">{t("admin.provider.kicker")}</p>
          <h3 id="provider-editor-title">
            {t(
              editing ? "admin.provider.editTitle" : "admin.provider.newTitle",
            )}
          </h3>
        </div>
        <Button
          variant="ghost"
          size="icon"
          onClick={onClose}
          aria-label={t("admin.provider.closeEditor")}
        >
          <X />
        </Button>
      </div>
      <form className="admin-provider-form" onSubmit={submit}>
        <div className="admin-provider-form-grid">
          <label className="admin-field">
            <span>{t("admin.provider.id")}</span>
            <input
              value={form.id}
              onChange={setField("id")}
              required
              disabled={editing}
              placeholder="provider-id"
            />
          </label>
          <label className="admin-field">
            <span>{t("admin.provider.name")}</span>
            <input
              value={form.name}
              onChange={setField("name")}
              required
              placeholder={t("admin.provider.name")}
            />
          </label>
          <label className="admin-field">
            <span>{t("admin.provider.protocol")}</span>
            <select value={form.protocol} onChange={setField("protocol")}>
              <option value="openai-responses">
                {t("admin.provider.protocol.responsesOption")}
              </option>
              <option value="openai-chat-completions">
                {t("admin.provider.protocol.chatCompletionsOption")}
              </option>
            </select>
          </label>
          <label className="admin-field">
            <span>{t("admin.provider.model")}</span>
            <input
              value={form.model}
              onChange={setField("model")}
              required
              placeholder="gpt-4o-mini"
            />
          </label>
        </div>
        <label className="admin-field">
          <span>{t("admin.provider.baseUrl")}</span>
          <input
            type="url"
            value={form.baseUrl}
            onChange={setField("baseUrl")}
            required
            placeholder="https://api.example.invalid/v1"
          />
        </label>
        <label className="admin-field">
          <span>{t("admin.provider.apiKey")}</span>
          <input
            type="password"
            value={form.apiKey}
            onChange={setField("apiKey")}
            autoComplete="new-password"
            placeholder={t(
              editing
                ? "admin.provider.apiKeyKeep"
                : "admin.provider.apiKeySave",
            )}
          />
        </label>
        {editing && form.apiKeyConfigured ? (
          <label className="admin-toggle-line">
            <input
              type="checkbox"
              checked={form.clearApiKey}
              onChange={setField("clearApiKey")}
            />
            {t("admin.provider.clearApiKey")}
          </label>
        ) : null}
        <div className="admin-provider-form-grid admin-provider-number-grid">
          <label className="admin-field">
            <span>{t("admin.provider.timeout")}</span>
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
            <span>{t("admin.provider.retries")}</span>
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
            {t("admin.provider.enable")}
          </label>
          <label className="admin-toggle-line">
            <input
              type="checkbox"
              checked={form.visionCapable}
              onChange={setField("visionCapable")}
            />
            {t("admin.provider.vision")}
          </label>
        </div>
        <div className="admin-form-actions">
          <Button type="button" variant="outline" onClick={onClose}>
            {t("admin.action.cancel")}
          </Button>
          <Button type="submit" disabled={saving}>
            {saving ? <LoaderCircle className="admin-spin" /> : <Save />}
            {t("admin.action.save")}
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
  const { t } = useAppSettings()
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
    onNotice(adminMessage("admin.provider.saved"))
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
      onNotice(adminMessage("admin.provider.orderUpdated"))
    } catch (moveError) {
      onError(formatAdminError(moveError))
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
      onNotice(
        adminMessage("admin.provider.testSuccess", { name: provider.name }),
      )
    } catch (testError) {
      onError(formatAdminError(testError))
    } finally {
      setBusy("")
    }
  }

  const remove = async (provider) => {
    if (
      !window.confirm(
        t("admin.provider.deleteConfirm", { name: provider.name }),
      )
    )
      return
    setBusy(`delete-${provider.id}`)
    onError("")
    try {
      await request(`/providers/${encodeURIComponent(provider.id)}`, {
        method: "DELETE",
      })
      await onReload()
      onNotice(adminMessage("admin.provider.deleted"))
    } catch (removeError) {
      onError(formatAdminError(removeError))
    } finally {
      setBusy("")
    }
  }

  return (
    <section className="admin-view" aria-labelledby="admin-provider-title">
      <div className="admin-view-heading">
        <div>
          <p className="admin-kicker">{t("admin.provider.routingKicker")}</p>
          <h2 id="admin-provider-title">{t("admin.provider.title")}</h2>
          <p>{t("admin.provider.description")}</p>
        </div>
        <Button onClick={() => setEditor({ ...emptyProvider })}>
          <Plus /> {t("admin.provider.new")}
        </Button>
      </div>
      <div className="admin-provider-layout">
        <section className="admin-panel admin-provider-table-panel">
          <div className="admin-panel-heading">
            <div>
              <p className="admin-kicker">{t("admin.provider.routeKicker")}</p>
              <h3>{t("admin.provider.orderTitle")}</h3>
            </div>
            <Badge variant="sky">{t("admin.provider.routeName")}</Badge>
          </div>
          <div className="admin-provider-table-wrap">
            <table className="admin-provider-table">
              <thead>
                <tr>
                  <th>{t("admin.provider.order")}</th>
                  <th>{t("admin.provider.provider")}</th>
                  <th>{t("admin.provider.protocolModel")}</th>
                  <th>{t("admin.provider.state")}</th>
                  <th aria-label={t("admin.provider.actions")} />
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
                            aria-label={t("admin.action.moveUp")}
                            title={t("admin.action.moveUp")}
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
                            aria-label={t("admin.action.moveDown")}
                            title={t("admin.action.moveDown")}
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
                            ? t("admin.provider.chatCompletions")
                            : t("admin.provider.responses")}
                        </span>
                        <small>{provider.model}</small>
                      </div>
                    </td>
                    <td>
                      <div className="admin-provider-statuses">
                        <Badge variant={provider.enabled ? "mint" : "neutral"}>
                          {provider.enabled
                            ? t("admin.provider.enabled")
                            : t("admin.provider.disabled")}
                        </Badge>
                        {provider.visionCapable ? (
                          <Badge variant="sky">
                            {t("admin.provider.visionBadge")}
                          </Badge>
                        ) : null}
                        <small>
                          {provider.apiKeyConfigured
                            ? t("admin.provider.keyConfigured")
                            : t("admin.provider.keyMissing")}
                        </small>
                        {provider.lastStatus ? (
                          <small>
                            {provider.lastStatus === "success"
                              ? t("admin.provider.lastSuccess")
                              : t("admin.provider.lastFailure")}
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
                          aria-label={t("admin.schedule.editLabel", {
                            title: provider.name,
                          })}
                          title={t("admin.action.edit")}
                        >
                          <Pencil />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => test(provider)}
                          disabled={busy !== "" || !provider.apiKeyConfigured}
                          aria-label={`${t("admin.action.test")} ${provider.name}`}
                          title={t("admin.action.test")}
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
                          aria-label={t("admin.schedule.deleteLabel", {
                            title: provider.name,
                          })}
                          title={t("admin.action.delete")}
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
              <div className="admin-empty">{t("admin.provider.empty")}</div>
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
        <KeyRound /> {t("admin.provider.securityNote")}
      </p>
    </section>
  )
}

function adminEventStatusLabel(event, t) {
  if (event.deletedAt) return t("admin.schedule.deleted")
  const explicit = String(event.statusCode || "").toLowerCase()
  const status = String(event.status || "")
    .trim()
    .toLowerCase()
  const normalized = explicit || status
  if (
    ["cancelled", "canceled", "cancel", "取消", "中止", "キャンセル"].includes(
      normalized,
    ) ||
    normalized.startsWith("cancel")
  ) {
    return t("admin.schedule.cancelled")
  }
  if (
    [
      "pending",
      "tentative",
      "unknown",
      "待确认",
      "待补充",
      "確認待ち",
      "未定",
    ].includes(normalized) ||
    event.timePrecision === "unknown"
  ) {
    return t("admin.schedule.pending")
  }
  if (
    ["upcoming", "scheduled", "已预约", "配信予定", "予約"].includes(
      normalized,
    ) ||
    (event.isUpcoming && !event.deletedAt)
  ) {
    return t("admin.schedule.upcoming")
  }
  if (
    ["completed", "success", "done", "已完成", "記録済み"].includes(normalized)
  ) {
    return t("admin.schedule.recorded")
  }
  if (["member", "会员限定", "メンバーシップ限定"].includes(normalized)) {
    return t("admin.event.type.member")
  }
  return t("admin.schedule.unknown")
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
  const { t } = useAppSettings()
  const events = pageData?.items || pageData?.events || []
  return (
    <section className="admin-view" aria-labelledby="admin-schedule-title">
      <div className="admin-view-heading">
        <div>
          <p className="admin-kicker">{t("admin.schedule.kicker")}</p>
          <h2 id="admin-schedule-title">{t("admin.schedule.title")}</h2>
          <p>{t("admin.schedule.description")}</p>
        </div>
        <Button onClick={onNew}>
          <Plus /> {t("admin.schedule.new")}
        </Button>
      </div>
      <form className="admin-schedule-filters" onSubmit={onFilterSubmit}>
        <label className="admin-field">
          <span>{t("admin.schedule.search")}</span>
          <div className="admin-input-with-icon">
            <Search />
            <input
              value={filters.search}
              onChange={(event) => onFilterChange("search", event.target.value)}
              placeholder={t("admin.schedule.searchPlaceholder")}
            />
          </div>
        </label>
        <label className="admin-field">
          <span>{t("admin.schedule.source")}</span>
          <select
            value={filters.provenance}
            onChange={(event) =>
              onFilterChange("provenance", event.target.value)
            }
          >
            <option value="">{t("admin.schedule.allSources")}</option>
            <option value="automatic">{t("admin.schedule.automatic")}</option>
            <option value="manual">{t("admin.schedule.manual")}</option>
          </select>
        </label>
        <label className="admin-field">
          <span>{t("admin.schedule.startDate")}</span>
          <input
            type="date"
            value={filters.from}
            onChange={(event) => onFilterChange("from", event.target.value)}
          />
        </label>
        <label className="admin-field">
          <span>{t("admin.schedule.endDate")}</span>
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
          {t("admin.schedule.includeDeleted")}
        </label>
        <Button type="submit" variant="outline">
          <Search />
          {t("admin.schedule.filter")}
        </Button>
      </form>
      <div className="admin-table-toolbar">
        <span>
          {t("admin.schedule.total", {
            count: pageData?.total ?? events.length,
          })}
        </span>
        <span>
          {t("admin.schedule.page", {
            page: pageData?.page || 1,
            totalPages: pageData?.totalPages || 1,
          })}
        </span>
      </div>
      <div className="admin-schedule-table-wrap">
        <table className="admin-schedule-table">
          <thead>
            <tr>
              <th>{t("admin.schedule.dateTime")}</th>
              <th>{t("admin.schedule.record")}</th>
              <th>{t("admin.schedule.source")}</th>
              <th>{t("admin.schedule.status")}</th>
              <th aria-label={t("admin.provider.actions")} />
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
                    <small>{formatEventTime(event, t)}</small>
                  </div>
                </td>
                <td>
                  <div className="admin-event-table-copy">
                    <strong>{event.title}</strong>
                    <small>
                      {event.detail || t("admin.schedule.noDetails")}
                    </small>
                  </div>
                </td>
                <td>
                  <Badge
                    variant={event.provenance === "manual" ? "mint" : "butter"}
                  >
                    {event.provenance === "manual"
                      ? t("admin.schedule.manualSource")
                      : t("admin.schedule.automaticSource")}
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
                    {adminEventStatusLabel(event, t)}
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
                        {t("admin.action.confirm")}
                      </Button>
                    ) : null}
                    {!event.deletedAt ? (
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => onEdit(event)}
                        aria-label={t("admin.schedule.editLabel", {
                          title: event.title,
                        })}
                        title={t("admin.action.edit")}
                      >
                        <Pencil />
                      </Button>
                    ) : null}
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => onDelete(event)}
                      aria-label={t("admin.schedule.deleteLabel", {
                        title: event.title,
                      })}
                      title={t("admin.action.delete")}
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
          <div className="admin-empty">{t("admin.schedule.noRecords")}</div>
        ) : null}
      </div>
      <div className="admin-pagination">
        <Button
          variant="outline"
          size="icon"
          onClick={() => onPageChange((pageData?.page || 1) - 1)}
          disabled={!pageData?.hasPrevious}
          aria-label={t("admin.schedule.pagePrevious")}
          title={t("admin.schedule.pagePrevious")}
        >
          <ChevronLeft />
        </Button>
        <span>
          {t("admin.schedule.page", {
            page: pageData?.page || 1,
            totalPages: pageData?.totalPages || 1,
          })}
        </span>
        <Button
          variant="outline"
          size="icon"
          onClick={() => onPageChange((pageData?.page || 1) + 1)}
          disabled={!pageData?.hasNext}
          aria-label={t("admin.schedule.pageNext")}
          title={t("admin.schedule.pageNext")}
        >
          <ChevronRight />
        </Button>
      </div>
      {pageData?.total ? (
        <p className="admin-table-footnote">
          {t("admin.schedule.rowsNote", { count: pageData.pageSize || 20 })}
        </p>
      ) : null}
    </section>
  )
}

function AdminAppModern() {
  const { t } = useAppSettings()
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
    document.title = t("admin.meta.title")
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
  }, [t])

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
      .catch((loadError) => active && setError(formatAdminError(loadError)))
      .finally(() => active && setIsLoading(false))
    return () => {
      active = false
    }
  }, [loadData, t])

  useEffect(() => {
    if (!session?.authenticated) return undefined
    const timer = window.setInterval(() => {
      loadSync().catch((syncError) => setError(formatAdminError(syncError)))
    }, 5000)
    return () => window.clearInterval(timer)
  }, [session?.authenticated, loadSync, t])

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
      setError(formatAdminError(logoutError))
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
      setNotice(adminMessage("admin.notice.configSaved"))
    } catch (saveError) {
      setError(formatAdminError(saveError))
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
      setNotice(
        adminMessage(
          payload.accepted
            ? "admin.notice.queued"
            : "admin.notice.alreadyRunning",
        ),
      )
    } catch (jobError) {
      setError(formatAdminError(jobError))
    } finally {
      setIsBusy(false)
    }
  }

  const replaceEvent = async () => {
    setEditor(null)
    await loadEventsPage({ page: eventsPage.page || 1, ...eventFilters })
    setNotice(adminMessage("admin.notice.scheduleSaved"))
  }

  const confirmEvent = async (event) => {
    setError("")
    try {
      await request(`/events/${encodeURIComponent(event.id)}/confirm`, {
        method: "POST",
      })
      await loadEventsPage({ page: eventsPage.page || 1, ...eventFilters })
      setNotice(adminMessage("admin.notice.scheduleConfirmed"))
    } catch (confirmError) {
      setError(formatAdminError(confirmError))
    }
  }

  const removeEvent = async (event) => {
    if (
      !window.confirm(t("admin.schedule.deleteConfirm", { title: event.title }))
    )
      return
    setError("")
    try {
      await request(`/events/${encodeURIComponent(event.id)}`, {
        method: "DELETE",
      })
      await loadEventsPage({ page: eventsPage.page || 1, ...eventFilters })
      setNotice(adminMessage("admin.notice.scheduleDeleted"))
    } catch (removeError) {
      setError(formatAdminError(removeError))
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
        {t("admin.loading")}
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
            <small>{t("admin.brand.statusBoard")}</small>
          </div>
        </div>
        <nav className="admin-nav" aria-label={t("admin.nav.aria")}>
          <p className="admin-nav-label">{t("admin.nav.workspace")}</p>
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
                <span>{t(tab.labelKey)}</span>
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
            {t("admin.sidebar.online")}
          </div>
          <a href="/" className="admin-back-link">
            {t("admin.sidebar.backToPublic")} <ChevronRight />
          </a>
        </div>
      </aside>
      <div className="admin-console-body">
        <header className="admin-console-topbar">
          <div>
            <p className="admin-kicker">{t("admin.topbar.controlCenter")}</p>
            <h1>
              {t(
                adminTabs.find((tab) => tab.id === activeTab)?.labelKey ||
                  "admin.nav.overview",
              )}
            </h1>
          </div>
          <div className="admin-topbar-actions">
            <PreferenceControls className="admin-preference-controls" />
            {activeJob ? (
              <Badge variant="butter">
                <span className="admin-pulse-dot" />
                {activeJob.kind === "scan"
                  ? t("admin.topbar.autoScanning")
                  : t("admin.topbar.syncing")}
              </Badge>
            ) : null}
            <Badge variant={session.mode === "production" ? "coral" : "mint"}>
              {session.mode === "production"
                ? t("admin.mode.production")
                : t("admin.mode.development")}
            </Badge>
            {session.requiresPassword ? (
              <Button variant="outline" size="sm" onClick={logout}>
                <LogOut />
                {t("admin.action.logout")}
              </Button>
            ) : null}
          </div>
        </header>
        {notice ? (
          <div className="admin-notice" role="status">
            <Check />
            {renderAdminMessage(notice, t)}
            <button
              type="button"
              onClick={() => setNotice(null)}
              aria-label={t("admin.action.closeNotice")}
            >
              <X />
            </button>
          </div>
        ) : null}
        {error ? (
          <div className="admin-error" role="alert">
            {renderAdminMessage(error, t)}
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
                setNotice(adminMessage("admin.notice.statusRefreshed"))
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
