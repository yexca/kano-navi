import type { ReactNode } from "react"
import { useEffect, useState } from "react"
import { ArrowUp } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { PreferenceControls } from "@/components/preference-controls"
import "./site.css"

export type PublicPage = "board" | "history" | "about"

const pages: { id: PublicPage; href: string; labelKey: string }[] = [
  { id: "board", href: "/", labelKey: "nav.now" },
  { id: "history", href: "/history", labelKey: "history.eyebrow" },
  { id: "about", href: "/about", labelKey: "about.label" },
]

const DEFAULT_AVATAR = "/assets/kano-avatar.jpg"

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
          <img src={avatarUrl || DEFAULT_AVATAR} alt="" />
          <span className="site-brand-copy">
            <strong>Kano Navi</strong>
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
        </div>
      </div>
    </header>
  )
}

export function SiteFooter({
  disclaimer,
  topHref = "#top",
}: {
  disclaimer: string
  topHref?: string
}) {
  const { t } = useAppSettings()
  return (
    <footer className="site-footer">
      <div className="site-footer-inner">
        <div className="site-footer-mark" aria-hidden="true">
          <span>🍓</span>
          <span>🦌</span>
        </div>
        <div className="site-footer-copy">
          <strong>{t("footer.fanMade")}</strong>
          <p>{disclaimer}</p>
        </div>
        <nav className="site-footer-links" aria-label={t("header.pages")}>
          {pages.map((item) => (
            <a key={item.id} href={item.href}>
              {t(item.labelKey)}
            </a>
          ))}
          <a href={topHref} className="site-footer-top">
            <ArrowUp aria-hidden="true" />
            {t("footer.backToTop")}
          </a>
        </nav>
      </div>
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
  note,
  children,
}: {
  id: string
  kicker?: ReactNode
  title: ReactNode
  note?: ReactNode
  children?: ReactNode
}) {
  return (
    <div className="section-title">
      <div className="section-title-copy">
        {kicker ? <p className="kicker">{kicker}</p> : null}
        <h2 id={id}>{title}</h2>
        {note ? <p className="section-title-note">{note}</p> : null}
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
