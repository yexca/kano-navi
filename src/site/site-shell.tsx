import type { ReactNode } from "react"
import { useEffect, useState } from "react"
import { ArrowUp, ArrowUpRight, Check } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { PreferenceControls } from "@/components/preference-controls"
import { cdnUrl } from "@/lib/cdn"
import { useMainlandSite } from "@/lib/visitor-region"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { KanoSeal } from "./kano-seal"
import { ExternalLink } from "./links"
import { NoticeDialog } from "./site-notice"
import "./site.css"

export type PublicPage = "board" | "history" | "works" | "lineblog" | "about"

const pages: { id: PublicPage; href: string; labelKey: string }[] = [
  { id: "board", href: "/", labelKey: "nav.now" },
  { id: "history", href: "/journey", labelKey: "history.eyebrow" },
  { id: "works", href: "/works", labelKey: "works.label" },
  { id: "lineblog", href: "/lineblog", labelKey: "lineblog.label" },
  { id: "about", href: "/about", labelKey: "about.label" },
]

/** The build's avatar, on the advertised CDN base when there is one. */
function defaultAvatar() {
  return cdnUrl("/assets/kano-avatar.jpg")
}

function japanClock(now: number) {
  return new Intl.DateTimeFormat("en", {
    timeZone: "Asia/Tokyo",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).format(now)
}

/** Minute-aligned Japan clock: every time on the site is Asia/Tokyo. */
function JapanClock() {
  const { t } = useAppSettings()
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    let timer: number
    const tick = () => {
      setNow(Date.now())
      timer = window.setTimeout(tick, 60000 - (Date.now() % 60000) + 50)
    }
    timer = window.setTimeout(tick, 60000 - (Date.now() % 60000) + 50)
    document.addEventListener("visibilitychange", tick)
    return () => {
      window.clearTimeout(timer)
      document.removeEventListener("visibilitychange", tick)
    }
  }, [])
  return (
    <span className="site-clock" title={t("header.japanTime")}>
      <span className="site-clock-zone">JST</span>
      <time dateTime={new Date(now).toISOString()}>{japanClock(now)}</time>
    </span>
  )
}

function useScrolled() {
  const [scrolled, setScrolled] = useState(false)
  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 4)
    onScroll()
    window.addEventListener("scroll", onScroll, { passive: true })
    return () => window.removeEventListener("scroll", onScroll)
  }, [])
  return scrolled
}

/** Narrow screens fold the page links into the seal button. */
function PageMenu({ page }: { page: PublicPage }) {
  const { t } = useAppSettings()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          className="site-menu-button"
          aria-label={t("header.pages")}
        >
          <KanoSeal />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="site-page-menu">
        {pages.map((item) => (
          <DropdownMenuItem asChild key={item.id}>
            <a
              href={item.href}
              aria-current={item.id === page ? "page" : undefined}
            >
              {t(item.labelKey)}
              {item.id === page ? <Check aria-hidden="true" /> : null}
            </a>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function SiteHeader({
  page,
  avatarUrl,
  status,
}: {
  page: PublicPage
  avatarUrl?: string | null
  /** Page-specific status (the board's snapshot state). */
  status?: ReactNode
}) {
  const { t } = useAppSettings()
  const scrolled = useScrolled()
  return (
    <header className={`site-header${scrolled ? " is-scrolled" : ""}`}>
      <div className="site-header-inner">
        <a
          className="site-brand"
          href="/"
          aria-label={t(page === "board" ? "header.backToTop" : "history.back")}
        >
          <img src={avatarUrl || defaultAvatar()} alt="" />
          <span className="site-brand-copy">
            <strong>{t("site.name")}</strong>
            <small>{t("header.unofficial")}</small>
          </span>
        </a>
        <nav className="site-pages" aria-label={t("header.pages")}>
          {pages.map((item) => (
            <a
              key={item.id}
              href={item.href}
              aria-current={item.id === page ? "page" : undefined}
            >
              {t(item.labelKey)}
            </a>
          ))}
        </nav>
        <div className="site-header-end">
          {status}
          <JapanClock />
          <PreferenceControls />
          <PageMenu page={page} />
        </div>
      </div>
    </header>
  )
}

/** The mainland China site stays reachable after its notice is dismissed. */
function MainlandSiteLink() {
  const { t } = useAppSettings()
  const site = useMainlandSite()
  if (!site) return null
  return (
    <p className="site-footer-mirror">
      <ExternalLink href={site.mirrorUrl}>
        {t("footer.mainlandSite")}
        <ArrowUpRight aria-hidden="true" />
      </ExternalLink>
    </p>
  )
}

/**
 * The shared footer: one line saying the site is fan-made and unaffiliated,
 * Notes opening the site's notice in a dialog, and links to About and the
 * top. The header holds the other pages.
 */
export function SiteFooter({ topHref = "#top" }: { topHref?: string }) {
  const { locale, t } = useAppSettings()
  const [open, setOpen] = useState(false)
  const about = pages.find((item) => item.id === "about")!
  return (
    <footer className="site-footer">
      <div className="site-footer-inner">
        <div className="site-footer-copy">
          <p className="site-footer-lead">
            <strong>{t("footer.fanMade")}</strong>
            <span className="site-footer-sep" aria-hidden="true">
              {" · "}
            </span>
            <span>{t("footer.unaffiliated")}</span>
          </p>
          {locale === "zh-CN" && <MainlandSiteLink />}
        </div>
        <nav className="site-footer-links" aria-label={t("header.pages")}>
          <button
            type="button"
            className="site-footer-notes"
            aria-haspopup="dialog"
            onClick={() => setOpen(true)}
          >
            {t("footer.notes")}
          </button>
          <a href={about.href}>{t(about.labelKey)}</a>
          <a href={topHref}>
            <ArrowUp aria-hidden="true" />
            {t("footer.backToTop")}
          </a>
        </nav>
      </div>
      {open ? <NoticeDialog onClose={() => setOpen(false)} /> : null}
    </footer>
  )
}

export function SkipLink({
  href,
  children,
}: {
  href: string
  children: ReactNode
}) {
  return (
    <a className="skip-link" href={href}>
      {children}
    </a>
  )
}

/** Section title with a small mono kicker and optional actions. */
export function SectionTitle({
  id,
  kicker,
  title,
  children,
}: {
  id: string
  kicker?: ReactNode
  title: ReactNode
  children?: ReactNode
}) {
  return (
    <div className="section-title">
      <div className="section-title-copy">
        {kicker ? <p className="kicker">{kicker}</p> : null}
        <h2 id={id}>{title}</h2>
      </div>
      {children ? (
        <div className="section-title-actions">{children}</div>
      ) : null}
    </div>
  )
}

export function EmptyNote({
  title,
  hint,
  children,
}: {
  title: ReactNode
  hint?: ReactNode
  children?: ReactNode
}) {
  return (
    <div className="empty-note">
      <p className="empty-note-title">{title}</p>
      {hint ? <p className="empty-note-hint">{hint}</p> : null}
      {children}
    </div>
  )
}

export function Placeholder({ lines = 3 }: { lines?: number }) {
  return (
    <div className="placeholder" aria-hidden="true">
      {Array.from({ length: lines }, (_, index) => (
        <span key={index} style={{ width: `${94 - index * 13}%` }} />
      ))}
    </div>
  )
}
