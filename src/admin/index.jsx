import { useCallback, useEffect, useState } from "react"
import {
  Bot,
  CalendarClock,
  Check,
  ChevronRight,
  Clapperboard,
  KeyRound,
  LoaderCircle,
  LockKeyhole,
  LogOut,
  ScanSearch,
  TriangleAlert,
  Workflow,
  X,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { PreferenceControls } from "@/components/preference-controls"
import { adminMessage, formatAdminError, renderAdminMessage } from "@/admin/api"
import { Btn } from "@/admin/components/primitives"
import { useAdminData } from "@/admin/use-admin-data"
import { ContentView } from "@/admin/views/content-view"
import { DetectionView } from "@/admin/views/detection-view"
import { ProvidersView } from "@/admin/views/providers-view"
import { SchedulesView } from "@/admin/views/schedules-view"
import { WorkflowView, stepMeta } from "@/admin/views/workflow-view"
import { cn } from "@/lib/utils"
import "./admin.css"

// Views are registered here; adding a module means one entry and one file.
const views = [
  { id: "workflow", group: "operate", icon: Workflow, component: WorkflowView },
  {
    id: "schedules",
    group: "operate",
    icon: CalendarClock,
    component: SchedulesView,
  },
  {
    id: "detection",
    group: "automation",
    icon: ScanSearch,
    component: DetectionView,
  },
  { id: "providers", group: "automation", icon: Bot, component: ProvidersView },
  {
    id: "content",
    group: "content",
    icon: Clapperboard,
    component: ContentView,
  },
]
const viewGroups = ["operate", "automation", "content"]

function viewFromHash() {
  const id = window.location.hash.replace(/^#/u, "")
  return views.some((view) => view.id === id) ? id : "workflow"
}

function Login({ onLogin }) {
  const { t } = useAppSettings()
  const [password, setPassword] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(null)

  const submit = async (event) => {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await onLogin(password)
    } catch (loginError) {
      setError(
        loginError.status === 429
          ? adminMessage("errors.tooManyAttempts")
          : loginError.status === 401 ||
              loginError.code === "invalid_credentials"
            ? adminMessage("errors.invalidCredentials")
            : formatAdminError(loginError),
      )
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="adm-login-shell">
      <div className="adm-login-toolbar">
        <PreferenceControls />
      </div>
      <form className="adm-login" onSubmit={submit}>
        <span className="adm-brand-mark is-large" aria-hidden="true">
          K
        </span>
        <p className="adm-eyebrow">{t("admin.brand.kanoStatusBoard")}</p>
        <h1>{t("admin.auth.title")}</h1>
        <label className="adm-field">
          <span className="adm-field-label">{t("admin.auth.password")}</span>
          <div className="adm-input-group">
            <LockKeyhole aria-hidden="true" />
            <input
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              required
              autoFocus
            />
          </div>
        </label>
        {error ? (
          <p className="adm-inline-error">{renderAdminMessage(error, t)}</p>
        ) : null}
        <Btn
          type="submit"
          variant="primary"
          size="lg"
          icon={KeyRound}
          busy={busy}
        >
          {t("admin.auth.login")}
        </Btn>
      </form>
    </main>
  )
}

function Toasts({ toasts, onDismiss, t }) {
  return (
    <div className="adm-toasts" aria-live="polite">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          className={cn("adm-toast", `is-${toast.tone}`)}
          role={toast.tone === "danger" ? "alert" : "status"}
        >
          {toast.tone === "danger" ? (
            <TriangleAlert aria-hidden="true" />
          ) : (
            <Check aria-hidden="true" />
          )}
          <span>{renderAdminMessage(toast.message, t)}</span>
          <button
            type="button"
            onClick={() => onDismiss(toast.id)}
            aria-label={t("admin.action.closeNotice")}
          >
            <X aria-hidden="true" />
          </button>
        </div>
      ))}
    </div>
  )
}

function ActiveJobPill({ job, t }) {
  if (!job) return null
  const steps = job.steps || []
  const running = steps.find((step) => job.progress?.[step] === "running")
  const done = steps.filter((step) =>
    ["completed", "partial", "failed", "skipped"].includes(
      job.progress?.[step],
    ),
  ).length
  const Icon = running ? stepMeta[running]?.icon : null
  return (
    <span className="adm-job-pill" role="status">
      <LoaderCircle className="adm-spin" aria-hidden="true" />
      <span>
        {job.kind === "workflow"
          ? job.workflowName || t("admin.workflow.kind.workflow")
          : job.kind === "scan"
            ? t("admin.workflow.kind.scan")
            : t("admin.workflow.kind.sync")}
      </span>
      {steps.length ? (
        <small>
          {Icon ? <Icon aria-hidden="true" /> : null}
          {done}/{steps.length}
        </small>
      ) : null}
    </span>
  )
}

export function AdminApp() {
  const { t } = useAppSettings()
  const data = useAdminData()
  const [activeView, setActiveView] = useState(viewFromHash)

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

  useEffect(() => {
    const onHashChange = () => setActiveView(viewFromHash())
    window.addEventListener("hashchange", onHashChange)
    return () => window.removeEventListener("hashchange", onHashChange)
  }, [])

  const navigate = useCallback((id) => {
    if (!views.some((view) => view.id === id)) return
    if (window.location.hash !== `#${id}`) window.location.hash = id
    setActiveView(id)
    window.scrollTo({ top: 0 })
  }, [])

  if (data.isLoading || !data.session) {
    return (
      <main className="adm-loading">
        <LoaderCircle className="adm-spin" aria-hidden="true" />
        {t("admin.loading")}
        <Toasts toasts={data.toasts} onDismiss={data.dismissToast} t={t} />
      </main>
    )
  }
  if (!data.session.authenticated) {
    return (
      <>
        <Login onLogin={data.login} />
        <Toasts toasts={data.toasts} onDismiss={data.dismissToast} t={t} />
      </>
    )
  }

  const current = views.find((view) => view.id === activeView) || views[0]
  const View = current.component
  const scheduler = data.workflowState?.scheduler
  const scheduledCount = (data.workflowState?.workflows || []).filter(
    (workflow) => workflow.scheduleEnabled,
  ).length

  return (
    <div className="adm-shell">
      <aside className="adm-sidebar">
        <a
          className="adm-brand"
          href="#workflow"
          onClick={() => navigate("workflow")}
        >
          <span className="adm-brand-mark" aria-hidden="true">
            K
          </span>
          <span>
            <strong>KANO</strong>
            <small>{t("admin.brand.console")}</small>
          </span>
        </a>
        <nav className="adm-nav" aria-label={t("admin.nav.aria")}>
          {viewGroups.map((group) => (
            <div className="adm-nav-group" key={group}>
              <p>{t(`admin.nav.group.${group}`)}</p>
              {views
                .filter((view) => view.group === group)
                .map((view) => {
                  const Icon = view.icon
                  const badge =
                    view.id === "schedules"
                      ? data.eventsPage?.total
                      : view.id === "providers"
                        ? data.config?.providers?.length
                        : null
                  return (
                    <a
                      key={view.id}
                      href={`#${view.id}`}
                      className={cn(
                        "adm-nav-item",
                        activeView === view.id && "is-active",
                      )}
                      aria-current={activeView === view.id ? "page" : undefined}
                      onClick={(event) => {
                        event.preventDefault()
                        navigate(view.id)
                      }}
                    >
                      <Icon aria-hidden="true" />
                      <span>{t(`admin.nav.${view.id}`)}</span>
                      {badge ? <em>{badge}</em> : null}
                    </a>
                  )
                })}
            </div>
          ))}
        </nav>
        <div className="adm-sidebar-foot">
          <div className="adm-sidebar-status">
            <span
              className={cn(
                "adm-dot",
                scheduler?.running ? "is-ok" : "is-unchecked",
              )}
              aria-hidden="true"
            />
            <span>
              {scheduler?.running
                ? t("admin.sidebar.schedulerOn", { count: scheduledCount })
                : t("admin.sidebar.schedulerOff")}
            </span>
          </div>
          <a href="/" className="adm-back-link">
            {t("admin.sidebar.backToPublic")}
            <ChevronRight aria-hidden="true" />
          </a>
        </div>
      </aside>
      <div className="adm-body">
        <header className="adm-topbar">
          <h1 className="adm-crumbs">
            <span>{t("admin.brand.console")}</span>
            <ChevronRight aria-hidden="true" />
            <span>{t(`admin.nav.group.${current.group}`)}</span>
            <ChevronRight aria-hidden="true" />
            <strong>{t(`admin.nav.${current.id}`)}</strong>
          </h1>
          <div className="adm-topbar-actions">
            <ActiveJobPill job={data.activeJob} t={t} />
            <span
              className={cn(
                "adm-mode",
                data.session.mode === "production"
                  ? "is-production"
                  : "is-development",
              )}
            >
              {data.session.mode === "production"
                ? t("admin.mode.production")
                : t("admin.mode.development")}
            </span>
            <PreferenceControls />
            {data.session.requiresPassword ? (
              <Btn
                variant="outline"
                size="sm"
                icon={LogOut}
                onClick={data.logout}
              >
                {t("admin.action.logout")}
              </Btn>
            ) : null}
          </div>
        </header>
        <main className="adm-main">
          {data.config ? (
            <View data={data} onNavigate={navigate} />
          ) : (
            <div className="adm-loading-block">
              <LoaderCircle className="adm-spin" aria-hidden="true" />
              {t("admin.loading")}
            </div>
          )}
        </main>
      </div>
      <Toasts toasts={data.toasts} onDismiss={data.dismissToast} t={t} />
    </div>
  )
}
