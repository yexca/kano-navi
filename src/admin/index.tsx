import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ComponentType,
  type SubmitEvent,
} from "react"
import {
  Bot,
  CalendarDays,
  Clapperboard,
  ExternalLink,
  FileText,
  KeyRound,
  LayoutDashboard,
  ListChecks,
  LockKeyhole,
  LogOut,
  Radio,
  ScanText,
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
import { LlmView } from "@/admin/views/llm"
import { OverviewView } from "@/admin/views/overview"
import { PostsView } from "@/admin/views/posts"
import { ProfileView } from "@/admin/views/profile"
import { ReviewView } from "@/admin/views/review"
import { RunsView } from "@/admin/views/runs"
import { ScheduleView } from "@/admin/views/schedule"
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
      review.pendingImages + review.suspectedCancellations + review.failedPosts,
  },
  {
    id: "schedule",
    group: "data",
    icon: CalendarDays,
    component: ScheduleView,
  },
  { id: "posts", group: "data", icon: FileText, component: PostsView },
  { id: "videos", group: "data", icon: Clapperboard, component: VideosView },
  { id: "profile", group: "data", icon: UserRound, component: ProfileView },
  { id: "runs", group: "system", icon: Workflow, component: RunsView },
  { id: "sources", group: "system", icon: Radio, component: SourcesView },
  {
    id: "detection",
    group: "system",
    icon: ScanText,
    component: DetectionView,
  },
  { id: "llm", group: "system", icon: Bot, component: LlmView },
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
  const [view = "", tab = ""] = window.location.hash
    .replace(/^#/u, "")
    .split("/")
  if (view === "content")
    return { view: legacyContent[tab] || "posts", tab: "" }
  // Models & detection became two views; old links open the model setup.
  if (view === "models") return { view: "llm", tab: "" }
  return views.some((entry) => entry.id === view)
    ? { view, tab }
    : { view: "overview", tab: "" }
}

function Login({ onLogin }: { onLogin: (password: string) => Promise<void> }) {
  const { t } = useAppSettings()
  const [password, setPassword] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<Message | null>(null)

  const submit = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await onLogin(password)
    } catch (failure) {
      setError(errorMessage(failure))
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="adm-login">
      <div className="adm-login-tools">
        <PreferenceControls />
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
        <Login onLogin={data.login} />
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
              <PreferenceControls />
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
