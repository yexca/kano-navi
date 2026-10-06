import { useEffect, useMemo, useState } from "react"
import {
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  ArrowUpRight,
  Box,
  Building2,
  Disc3,
  Mic,
  Radio,
  Shirt,
  Sparkles,
  Star,
  Ticket,
  TrainFront,
  Users,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { PreferenceControls } from "@/components/preference-controls"
import { fallbackProfile } from "@/dashboard/content"
import { ExternalLink } from "@/dashboard/components/primitives"
import { branches, milestones } from "./milestones"
import "@/dashboard/dashboard.css"
import "./history.css"

const FIRST_YEAR = 2010

const kindIcons = {
  start: Mic,
  debut: Star,
  release: Disc3,
  character: Sparkles,
  outfit: Shirt,
  model3d: Box,
  stream: Radio,
  group: Users,
  live: Ticket,
  collab: TrainFront,
  agency: Building2,
}

const branchById = Object.fromEntries(
  branches.map((branch) => [branch.id, branch]),
)

function localized(value, locale) {
  if (!value) return null
  return value[locale] ?? value.en
}

function currentJapanYear() {
  return Number(
    new Intl.DateTimeFormat("en", {
      timeZone: "Asia/Tokyo",
      year: "numeric",
    }).format(new Date()),
  )
}

function dayLabel(date) {
  const [, month, day] = date.split("-")
  return `${month}.${day}`
}

// Keeps the stored precision: a year-only or month-only entry is never shown
// as if it had a specific day.
function formatMilestoneDate(item, locale, t) {
  const parts = item.date.split("-")
  let label
  if (parts.length === 1) {
    label = t("history.wholeYear")
  } else if (parts.length === 2) {
    label = new Intl.DateTimeFormat(locale, {
      month: "short",
      timeZone: "UTC",
    }).format(new Date(Date.UTC(Number(parts[0]), Number(parts[1]) - 1, 1)))
  } else {
    label = dayLabel(item.date)
  }
  if (item.dateEnd) label = `${label} – ${dayLabel(item.dateEnd)}`
  if (item.dateNote) label = `${label} · ${item.dateNote}`
  return label
}

function groupByYear(items) {
  const groups = []
  for (const item of items) {
    const year = item.date.slice(0, 4)
    const last = groups[groups.length - 1]
    if (last?.year === year) last.items.push(item)
    else groups.push({ year, items: [item] })
  }
  return groups
}

function useScrolled() {
  const [isScrolled, setIsScrolled] = useState(false)
  useEffect(() => {
    const onScroll = () => setIsScrolled(window.scrollY > 8)
    onScroll()
    window.addEventListener("scroll", onScroll, { passive: true })
    return () => window.removeEventListener("scroll", onScroll)
  }, [])
  return isScrolled
}

function MilestoneCard({ item, locale, t }) {
  const branch = branchById[item.branch]
  const Icon = kindIcons[item.kind] || Sparkles
  const detail = localized(item.detail, locale)
  return (
    <li
      className={`journey-item tone-${branch.tone}${
        item.highlight ? " is-highlight" : ""
      }`}
    >
      <span className="journey-dot" aria-hidden="true">
        <Icon />
      </span>
      <article className="journey-card">
        <header className="journey-card-meta">
          <time dateTime={item.date}>
            {formatMilestoneDate(item, locale, t)}
          </time>
          <span className="journey-branch">
            {localized(branch.label, locale)}
          </span>
          <span className="journey-kind">{t(`history.kind.${item.kind}`)}</span>
        </header>
        <h3>{localized(item.title, locale)}</h3>
        {detail ? <p>{detail}</p> : null}
        {item.source ? (
          <ExternalLink className="journey-source" href={item.source}>
            {t("history.source")}
            <ArrowUpRight aria-hidden="true" />
          </ExternalLink>
        ) : (
          <span className="journey-source is-muted">
            {t("history.archiveOnly")}
          </span>
        )}
      </article>
    </li>
  )
}

export function HistoryApp() {
  const { locale, t } = useAppSettings()
  const [branchFilter, setBranchFilter] = useState("all")
  const isScrolled = useScrolled()

  useEffect(() => {
    document.title = t("history.metaTitle")
    document
      .querySelector('meta[name="description"]')
      ?.setAttribute("content", t("history.metaDescription"))
  }, [t])

  const groups = useMemo(
    () =>
      groupByYear(
        branchFilter === "all"
          ? milestones
          : milestones.filter((item) => item.branch === branchFilter),
      ),
    [branchFilter],
  )
  const years = currentJapanYear() - FIRST_YEAR
  const releaseCount = milestones.filter(
    (item) => item.kind === "release",
  ).length

  return (
    <div className="board history-page">
      <a className="skip-link" href="#journey">
        {t("history.eyebrow")}
      </a>
      <header className={`site-header${isScrolled ? " is-scrolled" : ""}`}>
        <div className="site-header-inner">
          <a className="brand" href="/" aria-label={t("history.back")}>
            <img
              className="brand-avatar"
              src={fallbackProfile.avatarUrl}
              alt=""
            />
            <span className="brand-copy">
              <span className="brand-name">{fallbackProfile.displayName}</span>
              <span className="brand-tag">{t("header.unofficial")}</span>
            </span>
          </a>
          <div className="site-header-end">
            <a className="soft-button history-back" href="/">
              <ArrowLeft aria-hidden="true" />
              <span>{t("history.back")}</span>
            </a>
            <PreferenceControls className="header-preferences" />
          </div>
        </div>
      </header>

      <main className="board-main">
        <section
          className="panel history-intro"
          aria-labelledby="history-title"
        >
          <p className="eyebrow">
            {t("history.eyebrow")} · {FIRST_YEAR} → {t("history.now")}
          </p>
          <h1 id="history-title">{t("history.title")}</h1>
          <p className="history-lead">{t("history.lead")}</p>

          <dl className="history-stats">
            <div>
              <dt>{t("history.stat.yearsLabel")}</dt>
              <dd>{t("history.stat.years", { count: years })}</dd>
            </div>
            <div>
              <dt>{t("history.stat.milestones")}</dt>
              <dd>{milestones.length}</dd>
            </div>
            <div>
              <dt>{t("history.stat.releases")}</dt>
              <dd>{releaseCount}</dd>
            </div>
          </dl>

          <div
            className="history-filter"
            role="group"
            aria-label={t("history.filter")}
          >
            <button
              type="button"
              className="history-filter-chip"
              aria-pressed={branchFilter === "all"}
              onClick={() => setBranchFilter("all")}
            >
              {t("common.all")}
            </button>
            {branches.map((branch) => (
              <button
                key={branch.id}
                type="button"
                className={`history-filter-chip tone-${branch.tone}`}
                aria-pressed={branchFilter === branch.id}
                onClick={() => setBranchFilter(branch.id)}
              >
                <span className="history-filter-swatch" aria-hidden="true" />
                {localized(branch.label, locale)}
                <small>
                  {t("history.branchSince", { year: branch.since })}
                </small>
              </button>
            ))}
          </div>
        </section>

        <section
          className="journey-section"
          id="journey"
          aria-label={t("history.eyebrow")}
        >
          {groups.length ? (
            <ol className="journey">
              {groups.map((group) => (
                <li key={group.year} className="journey-year">
                  <h2 className="journey-year-label">{group.year}</h2>
                  <ol className="journey-items">
                    {group.items.map((item) => (
                      <MilestoneCard
                        key={item.id}
                        item={item}
                        locale={locale}
                        t={t}
                      />
                    ))}
                  </ol>
                </li>
              ))}
              <li className="journey-year journey-now">
                <h2 className="journey-year-label">{t("history.now")}</h2>
                <a className="journey-now-card" href="/">
                  <span>
                    <strong>{t("history.nowTitle")}</strong>
                    <small>{t("history.nowDetail")}</small>
                  </span>
                  <ArrowRight aria-hidden="true" />
                </a>
              </li>
            </ol>
          ) : (
            <p className="history-empty">{t("history.empty")}</p>
          )}
          <p className="history-note">{t("history.note")}</p>
        </section>
      </main>

      <footer className="site-footer">
        <div className="site-footer-brand">
          <span aria-hidden="true">🍓🦌</span>
          <strong>{t("footer.fanMade")}</strong>
        </div>
        <p>{t("history.disclaimer")}</p>
        <a href="#history-title" className="soft-button">
          <ArrowUp aria-hidden="true" />
          {t("footer.backToTop")}
        </a>
      </footer>
    </div>
  )
}
