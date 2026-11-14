import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ComponentType,
  type SubmitEvent,
} from "react"
import {
  CalendarDays,
  Clapperboard,
  ExternalLink,
  FileText,
  Globe,
  KeyRound,
  LayoutDashboard,
  ListChecks,
  ListOrdered,
  LockKeyhole,
  LogOut,
  Network,
  Newspaper,
  Radio,
  RefreshCw,
  ScanText,
  ShieldCheck,
  UserRound,
  Workflow,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { PreferenceControls } from "@/components/preference-controls"
import { errorMessage, renderMessage, type Message } from "@/admin/api"
import type { Overview } from "@/admin/types"
import { Badge, Button, Spinner, Toasts } from "@/admin/ui"
import { AdminContext, useAdminData } from "@/admin/use-admin-data"
import { DetectionView } from "@/admin/views/detection"
import { ExternalPostsView } from "@/admin/views/external-posts"
import { OverviewView } from "@/admin/views/overview"
import { PostsView } from "@/admin/views/posts"
import { ProfileView } from "@/admin/views/profile"
import { ProxyView } from "@/admin/views/proxy"
import { QueueView } from "@/admin/views/queue"
import { ReviewView } from "@/admin/views/review"
import { RunsView } from "@/admin/views/runs"
import { ScheduleView } from "@/admin/views/schedule"
import { SiteView } from "@/admin/views/site"
import { SourcesView } from "@/admin/views/sources"
import { runTitle } from "@/admin/views/run-parts"
import { VideosView } from "@/admin/views/videos"
import { cn } from "@/lib/utils"
import "./admin.css"

export interface ViewProps {
  tab: string
  navigate: (view: string, tab?: string) => void
}

interface ViewEntry {
  id: string
  group: "today" | "data" | "system"
  icon: ComponentType<{ "aria-hidden"?: boolean }>
  component: ComponentType<ViewProps>
  badge?: (overview: Overview) => number
}

// Views are registered here; adding a section means one entry and one file.
const views: ViewEntry[] = [
  {
    id: "overview",
    group: "today",
    icon: LayoutDashboard,
    component: OverviewView,
  },
  {
    id: "review",
    group: "today",
    icon: ListChecks,
    component: ReviewView,
    badge: ({ review }) =>
      review.pendingImages +
      review.suspectedCancellations +
      review.uncertainEvents +
      review.videoMerges +
      review.bilibiliForms +
      review.failedPosts,
  },
  {
    id: "schedule",
    group: "data",
    icon: CalendarDays,
    component: ScheduleView,
  },
  { id: "posts", group: "data", icon: FileText, component: PostsView },
  {
    id: "external",
    group: "data",
    icon: Newspaper,
    component: ExternalPostsView,
  },
  { id: "videos", group: "data", icon: Clapperboard, component: VideosView },
  { id: "profile", group: "data", icon: UserRound, component: ProfileView },
  { id: "runs", group: "system", icon: Workflow, component: RunsView },
  { id: "queue", group: "system", icon: ListOrdered, component: QueueView },
  { id: "sources", group: "system", icon: Radio, component: SourcesView },
  {
    id: "detection",
    group: "system",
    icon: ScanText,
    component: DetectionView,
  },
  { id: "proxy", group: "system", icon: Network, component: ProxyView },
  { id: "site", group: "system", icon: Globe, component: SiteView },
]
const groups = ["today", "data", "system"] as const

/** The public header's default portrait. */
const BRAND_AVATAR = "/assets/kano-avatar.jpg"

// Links saved before Content was split into separate views.
const legacyContent: Record<string, string> = {
  posts: "posts",
  videos: "videos",
  profile: "profile",
}

function readHash() {
  // The tab is everything after the view, so it may hold several parts.
  const [view = "", ...parts] = window.location.hash
    .replace(/^#/u, "")
    .split("/")
  const tab = parts.join("/")
  if (view === "content")
    return { view: legacyContent[tab] || "posts", tab: "" }
  // Model links open the Detection page, which holds the model setup.
  if (view === "models") return { view: "detection", tab: "" }
  if (view === "llm") return { view: "detection", tab }
  return views.some((entry) => entry.id === view)
    ? { view, tab }
    : { view: "overview", tab: "" }
}

function Login({
  requiresCode,
  onLogin,
}: {
  requiresCode: boolean
  onLogin: (password: string, code?: string) => Promise<void>
}) {
  const { t } = useAppSettings()
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<Message | null>(null)

  const submit = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await onLogin(password, requiresCode ? code : undefined)
    } catch (failure) {
      const next = errorMessage(failure)
      setError(
        requiresCode &&
          "key" in next &&
          next.key === "admin.error.invalidCredentials"
          ? { key: "admin.error.invalidCredentialsOrCode" }
          : next,
      )
      // A code is accepted once, so a failed one is never retried as is.
      if (requiresCode) setCode("")
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="adm-login">
      <div className="adm-login-tools">
        <PreferenceControls content={false} />
      </div>
      <form onSubmit={submit}>
        <span className="adm-mark is-large" aria-hidden>
          K
        </span>
        <p className="adm-eyebrow">{t("admin.brand.board")}</p>
        <h1>{t("admin.login.title")}</h1>
        <label className="adm-field">
          <span className="adm-field-label">{t("admin.login.password")}</span>
          <span className="adm-input-icon">
            <LockKeyhole aria-hidden />
            <input
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              required
              autoFocus
            />
          </span>
        </label>
        {requiresCode ? (
          <label className="adm-field">
            <span className="adm-field-label">{t("admin.login.code")}</span>
            <span className="adm-input-icon">
              <ShieldCheck aria-hidden />
              <input
                type="text"
                name="one-time-code"
                autoComplete="one-time-code"
                inputMode="numeric"
                pattern="[0-9 ]{6,7}"
                maxLength={7}
                value={code}
                onChange={(event) => setCode(event.target.value)}
                required
              />
            </span>
          </label>
        ) : null}
        {error ? (
          <p className="adm-inline-error" role="alert">
            {renderMessage(error, t)}
          </p>
        ) : null}
        <Button type="submit" variant="primary" icon={KeyRound} busy={busy}>
          {t("admin.login.submit")}
        </Button>
      </form>
    </main>
  )
}

/**
 * The console could not read the session, so it does not know whether login
 * needs the one-time code. It shows why and reads the session again.
 */
function SessionUnavailable({
  error,
  onRetry,
}: {
  error: Message
  onRetry: () => void
}) {
  const { t } = useAppSettings()
  return (
    <main className="adm-login">
      <div className="adm-login-tools">
        <PreferenceControls content={false} />
      </div>
      <form
        onSubmit={(event) => {
          event.preventDefault()
          onRetry()
        }}
      >
        <span className="adm-mark is-large" aria-hidden>
          K
        </span>
        <p className="adm-eyebrow">{t("admin.brand.board")}</p>
        <h1>{t("admin.login.title")}</h1>
        <p className="adm-inline-error" role="alert">
          {renderMessage(error, t)}
        </p>
        <Button type="submit" variant="primary" icon={RefreshCw}>
          {t("admin.login.retry")}
        </Button>
      </form>
    </main>
  )
}

function ActiveRun({
  overview,
  onOpen,
}: {
  overview: Overview | null
  onOpen: () => void
}) {
  const { t } = useAppSettings()
  const run = overview?.activeRun
  if (!run) return null
  const done = run.steps.filter((step) =>
    ["completed", "partial", "failed", "skipped", "cancelled"].includes(
      run.progress[step],
    ),
  ).length
  const current = run.steps.find((step) => run.progress[step] === "running")
  return (
    <button type="button" className="adm-run-pill" onClick={onOpen}>
      <Spinner />
      <span>
        {runTitle(run, t)}
        {current ? ` · ${t(`admin.step.${current}`)}` : ""}
      </span>
      <small>
        {done}/{run.steps.length}
      </small>
    </button>
  )
}

export function AdminApp() {
  const { t } = useAppSettings()
  const data = useAdminData()
  const [location, setLocation] = useState(readHash)
  const navRef = useRef<HTMLElement>(null)

  useEffect(() => {
    document.title = t("admin.meta.title")
    let robots = document.querySelector('meta[name="robots"]')
    const previous = robots?.getAttribute("content") ?? null
    const created = !robots
    if (!robots) {
      robots = document.createElement("meta")
      robots.setAttribute("name", "robots")
      document.head.append(robots)
    }
    robots.setAttribute("content", "noindex, nofollow")
    return () => {
      if (created) robots.remove()
      else if (previous == null) robots.removeAttribute("content")
      else robots.setAttribute("content", previous)
    }
  }, [t])

  useEffect(() => {
    const onHashChange = () => setLocation(readHash())
    window.addEventListener("hashchange", onHashChange)
    return () => window.removeEventListener("hashchange", onHashChange)
  }, [])

  // On narrow screens the nav is a scrolling strip; keep the current view in it.
  const signedIn = Boolean(data.session?.authenticated)
  useEffect(() => {
    const nav = navRef.current
    const item = nav?.querySelector(".adm-nav-item.is-active")
    if (!nav || !item || nav.scrollWidth <= nav.clientWidth) return
    const box = nav.getBoundingClientRect()
    const rect = item.getBoundingClientRect()
    if (rect.left < box.left) nav.scrollLeft -= box.left - rect.left + 16
    else if (rect.right > box.right)
      nav.scrollLeft += rect.right - box.right + 16
  }, [location.view, signedIn])

  const navigate = useCallback((view: string, tab = "") => {
    if (!views.some((entry) => entry.id === view)) return
    const hash = `#${view}${tab ? `/${tab}` : ""}`
    // A tab inside the same view keeps the scroll position.
    const sameView = readHash().view === view
    if (window.location.hash !== hash) window.history.pushState(null, "", hash)
    setLocation({ view, tab })
    if (!sameView) window.scrollTo({ top: 0 })
  }, [])

  if (!data.session && data.sessionError) {
    return (
      <>
        <SessionUnavailable
          error={data.sessionError}
          onRetry={data.retrySession}
        />
        <Toasts toasts={data.toasts} onDismiss={data.dismissToast} />
      </>
    )
  }
  if (!data.session) {
    return (
      <main className="adm-boot" role="status">
        <Spinner />
        {t("admin.loading")}
        <Toasts toasts={data.toasts} onDismiss={data.dismissToast} />
      </main>
    )
  }
  if (!data.session.authenticated) {
    return (
      <>
        <Login
          requiresCode={Boolean(data.session.requiresCode)}
          onLogin={data.login}
        />
        <Toasts toasts={data.toasts} onDismiss={data.dismissToast} />
      </>
    )
  }

  const current = views.find((entry) => entry.id === location.view) || views[0]
  const View = current.component
  const overview = data.overview
  const scheduler = overview?.scheduler

  return (
    <AdminContext.Provider value={data}>
      <div className="adm-shell">
        <aside className="adm-sidebar">
          <a
            className="adm-brand"
            href="#overview"
            onClick={(event) => {
              event.preventDefault()
              navigate("overview")
            }}
          >
            <img className="adm-brand-avatar" src={BRAND_AVATAR} alt="" />
            <span>
              <strong>KANO</strong>
              <small>{t("admin.brand.console")}</small>
            </span>
          </a>
          <nav
            ref={navRef}
            className="adm-nav"
            aria-label={t("admin.nav.label")}
          >
            {groups.map((group) => (
              <div className="adm-nav-group" key={group}>
                <p>{t(`admin.nav.group.${group}`)}</p>
                {views
                  .filter((entry) => entry.group === group)
                  .map((entry) => {
                    const Icon = entry.icon
                    const badge = overview && entry.badge?.(overview)
                    const active = current.id === entry.id
                    return (
                      <a
                        key={entry.id}
                        href={`#${entry.id}`}
                        className={cn("adm-nav-item", active && "is-active")}
                        aria-current={active ? "page" : undefined}
                        onClick={(event) => {
                          event.preventDefault()
                          navigate(entry.id)
                        }}
                      >
                        <Icon aria-hidden />
                        <span>{t(`admin.nav.${entry.id}`)}</span>
                        {badge ? <em>{badge}</em> : null}
                        {entry.id === "runs" && overview?.activeRun ? (
                          <i className="adm-live-dot" aria-hidden />
                        ) : null}
                      </a>
                    )
                  })}
              </div>
            ))}
          </nav>
          <div className="adm-sidebar-foot">
            <p>
              <i
                className={cn(
                  "adm-dot",
                  scheduler?.running ? "is-on" : "is-off",
                )}
                aria-hidden
              />
              {scheduler?.running
                ? t("admin.sidebar.schedulerOn", {
                    count: overview?.timers.scheduled ?? 0,
                  })
                : t("admin.sidebar.schedulerOff")}
            </p>
            <a href="/">
              {t("admin.sidebar.public")}
              <ExternalLink aria-hidden />
            </a>
          </div>
        </aside>
        <div className="adm-body">
          <header className="adm-topbar">
            <p className="adm-crumb">
              {t(`admin.nav.group.${current.group}`)}
              <span aria-hidden>/</span>
              <strong>{t(`admin.nav.${current.id}`)}</strong>
            </p>
            <div className="adm-topbar-tools">
              <ActiveRun overview={overview} onOpen={() => navigate("runs")} />
              <Badge
                tone={data.session.mode === "production" ? "leaf" : "honey"}
              >
                {t(`admin.mode.${data.session.mode}`)}
              </Badge>
              <PreferenceControls content={false} />
              {data.session.requiresPassword ? (
                <Button
                  variant="ghost"
                  size="sm"
                  icon={LogOut}
                  onClick={data.logout}
                >
                  {t("admin.action.logout")}
                </Button>
              ) : null}
            </div>
          </header>
          <main className="adm-main" id="admin-main">
            <View key={current.id} tab={location.tab} navigate={navigate} />
          </main>
        </div>
        <Toasts toasts={data.toasts} onDismiss={data.dismissToast} />
      </div>
    </AdminContext.Provider>
  )
}
