import type { Translator, Profile } from "@/types"
import { useEffect, useState } from "react"

import { PreferenceControls } from "@/components/preference-controls"
import { sections } from "../content"

export function SiteHeader({
  profile,
  syncState,
  syncTime,
  syncIso,
  t,
}: {
  profile: Profile
  syncState: any
  syncTime: any
  syncIso: any
  t: Translator
}) {
  const [activeSection, setActiveSection] = useState("now")
  const [isScrolled, setIsScrolled] = useState(false)

  useEffect(() => {
    const onScroll = () => {
      setIsScrolled(window.scrollY > 8)
      // The last section can be too short to cross the observer band.
      const atBottom =
        window.innerHeight + window.scrollY >=
        document.documentElement.scrollHeight - 4
      if (atBottom) setActiveSection(sections[sections.length - 1].id)
    }
    onScroll()
    window.addEventListener("scroll", onScroll, { passive: true })
    return () => window.removeEventListener("scroll", onScroll)
  }, [])

  useEffect(() => {
    if (typeof IntersectionObserver === "undefined") return undefined
    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries
          .filter((entry) => entry.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)
        if (visible[0]) setActiveSection(visible[0].target.id)
      },
      { rootMargin: "-30% 0px -60% 0px" },
    )
    for (const { id } of sections) {
      const element = document.getElementById(id)
      if (element) observer.observe(element)
    }
    return () => observer.disconnect()
  }, [])

  return (
    <header className={`site-header${isScrolled ? " is-scrolled" : ""}`}>
      <div className="site-header-inner">
        <a className="brand" href="#now" aria-label={t("header.backToTop")}>
          <img className="brand-avatar" src={profile.avatarUrl} alt="" />
          <span className="brand-copy">
            <span className="brand-name">{profile.displayName}</span>
            <span className="brand-tag">{t("header.unofficial")}</span>
          </span>
        </a>

        <nav className="section-nav" aria-label={t("header.primaryNav")}>
          {sections.map(({ id, labelKey }) => (
            <a
              key={id}
              href={`#${id}`}
              className={activeSection === id ? "is-active" : undefined}
              aria-current={activeSection === id ? "location" : undefined}
            >
              {t(labelKey)}
            </a>
          ))}
          <a href="/about">{t("about.label")}</a>
        </nav>

        <div className="site-header-end">
          <span
            className={`sync-pill is-${syncState.tone}`}
            title={syncState.label}
          >
            <span className="sync-dot" aria-hidden="true" />
            {syncIso ? (
              <>
                <span className="sync-pill-label">
                  {t("header.lastUpdated")}
                </span>
                <time dateTime={syncIso}>{syncTime}</time>
              </>
            ) : (
              <span>{syncTime}</span>
            )}
          </span>
          <PreferenceControls className="header-preferences" />
        </div>
      </div>
    </header>
  )
}
