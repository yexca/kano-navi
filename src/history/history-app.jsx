import { useCallback, useEffect, useMemo, useState } from "react"
import {
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  ArrowUpRight,
  Box,
  Building2,
  Disc3,
  GalleryVerticalEnd,
  Image,
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { fallbackProfile } from "@/dashboard/content"
import { ExternalLink } from "@/dashboard/components/primitives"
import { branches, milestones } from "./milestones"
import {
  branchPortraits,
  historyMedia,
  localized,
  mediaByMilestone,
  sourceName,
} from "./media"
import { HistoryImage, HistoryImageViewer, ImageCard } from "./history-media"
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
  artwork: Image,
  goods: Box,
}

const branchById = Object.fromEntries(
  branches.map((branch) => [branch.id, branch]),
)

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

function MilestoneCard({ item, locale, t, onOpen }) {
  const branch = branchById[item.branch]
  const Icon = kindIcons[item.kind] || Sparkles
  const detail = localized(item.detail, locale)
  const media = mediaByMilestone[item.id] || []
  return (
    <li
      className={`journey-item tone-${branch.tone}${
        item.highlight ? " is-highlight" : ""
      }`}
    >
      <span className="journey-dot" aria-hidden="true">
        <Icon />
      </span>
      <article className={`journey-card${media.length ? " has-images" : ""}`}>
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
            {sourceName(item.source, t)}
            <ArrowUpRight aria-hidden="true" />
          </ExternalLink>
        ) : (
          <span className="journey-source is-muted">
            {t("history.archiveOnly")}
          </span>
        )}
        {item.links?.map((link) => (
          <ExternalLink
            key={link.href}
            className="journey-source journey-extra-source"
            href={link.href}
          >
            {t(link.label)}
            <ArrowUpRight aria-hidden="true" />
          </ExternalLink>
        ))}
        {media.length ? (
          <div className="journey-images">
            {media.map((image, index) => (
              <ImageCard
                key={image.id}
                item={image}
                locale={locale}
                t={t}
                compact
                onOpen={() => onOpen(media, index)}
              />
            ))}
          </div>
        ) : null}
      </article>
    </li>
  )
}

export function HistoryApp() {
  const { locale, t } = useAppSettings()
  const [branchFilter, setBranchFilter] = useState("all")
  const [mediaFilter, setMediaFilter] = useState("all")
  const [view, setView] = useState("timeline")
  const [lightbox, setLightbox] = useState(null)
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
  const images = useMemo(
    () =>
      historyMedia
        .filter(
          (item) =>
            (branchFilter === "all" || item.branches.includes(branchFilter)) &&
            (mediaFilter === "all" || item.category === mediaFilter),
        )
        .sort((left, right) =>
          (right.date || "").localeCompare(left.date || ""),
        ),
    [branchFilter, mediaFilter],
  )
  const currentPortrait = historyMedia.find(
    (item) => item.id === branchPortraits.sona,
  )
  const openMedia = useCallback(
    (media, index) => setLightbox({ media, index }),
    [],
  )
  const closeMedia = useCallback(() => setLightbox(null), [])
  const stepMedia = useCallback(
    (direction) =>
      setLightbox((current) =>
        current
          ? {
              ...current,
              index:
                (current.index + direction + current.media.length) %
                current.media.length,
            }
          : current,
      ),
    [],
  )

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
          <div className="history-intro-copy">
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
                <dt>{t("history.stat.images")}</dt>
                <dd>{historyMedia.length}</dd>
              </div>
            </dl>
            <div className="history-official-links">
              <ExternalLink
                className="soft-button"
                href="https://kano-official.amebaownd.com/"
              >
                {t("history.sourceName.artistSite")}
                <ArrowUpRight aria-hidden="true" />
              </ExternalLink>
              <ExternalLink
                className="soft-button"
                href="https://milpr.com/talents/kano-mahoro"
              >
                {t("history.sourceName.agency")}
                <ArrowUpRight aria-hidden="true" />
              </ExternalLink>
            </div>
          </div>
          <figure className="history-hero-portrait tone-sky">
            <button
              type="button"
              className="history-hero-image"
              onClick={() => openMedia([currentPortrait], 0)}
              aria-label={t("history.openImage", {
                title: localized(currentPortrait.title, locale),
              })}
            >
              <HistoryImage
                item={currentPortrait}
                locale={locale}
                t={t}
                eager
              />
            </button>
            <figcaption>
              <span>2026 · ミリプロSONA</span>
              <ExternalLink href={currentPortrait.sourcePage}>
                {t("history.heroImageSource")}
                <ArrowUpRight aria-hidden="true" />
              </ExternalLink>
            </figcaption>
          </figure>
        </section>

        <section
          className="history-identities"
          aria-labelledby="history-identities-title"
        >
          <div className="history-identities-heading">
            <div>
              <h2 id="history-identities-title">{t("history.identities")}</h2>
              <p>{t("history.identitiesNote")}</p>
            </div>
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
            </div>
          </div>
          <div className="history-branch-cards">
            {branches.map((branch) => {
              const image = historyMedia.find(
                (item) => item.id === branchPortraits[branch.id],
              )
              return (
                <button
                  key={branch.id}
                  type="button"
                  className={`history-branch-card tone-${branch.tone}`}
                  aria-pressed={branchFilter === branch.id}
                  onClick={() => setBranchFilter(branch.id)}
                >
                  <span className="history-branch-image">
                    <HistoryImage
                      item={image}
                      locale={locale}
                      t={t}
                      decorative
                    />
                  </span>
                  <span className="history-branch-copy">
                    <strong>{localized(branch.label, locale)}</strong>
                    <small>
                      {t("history.branchSince", { year: branch.since })}
                    </small>
                  </span>
                </button>
              )
            })}
          </div>
        </section>

        <Tabs
          value={view}
          className="history-archive"
          onValueChange={(nextView) => {
            closeMedia()
            setView(nextView)
          }}
        >
          <div className="history-archive-toolbar">
            <TabsList
              className="history-view-tabs"
              aria-label={t("history.view")}
            >
              <TabsTrigger value="timeline">
                <GalleryVerticalEnd aria-hidden="true" />
                {t("history.timeline")}
              </TabsTrigger>
              <TabsTrigger value="gallery">
                <Image aria-hidden="true" />
                {t("history.gallery")}
              </TabsTrigger>
            </TabsList>
            <p className="history-selection-count" role="status">
              {t("history.selectionCount", {
                milestones: groups.reduce(
                  (count, group) => count + group.items.length,
                  0,
                ),
                images:
                  view === "gallery"
                    ? images.length
                    : historyMedia.filter(
                        (item) =>
                          branchFilter === "all" ||
                          item.branches.includes(branchFilter),
                      ).length,
              })}
            </p>
          </div>
          <TabsContent value="timeline">
            <nav
              className="history-year-nav"
              aria-label={t("history.jumpYear")}
            >
              {groups.map((group) => (
                <a key={group.year} href={`#year-${group.year}`}>
                  {group.year}
                </a>
              ))}
              <a href="#journey-now">{t("history.now")}</a>
            </nav>
            <section
              className="journey-section"
              id="journey"
              aria-label={t("history.eyebrow")}
            >
              {groups.length ? (
                <ol className="journey">
                  {groups.map((group) => (
                    <li
                      key={group.year}
                      className="journey-year"
                      id={`year-${group.year}`}
                    >
                      <h2 className="journey-year-label">{group.year}</h2>
                      <ol className="journey-items">
                        {group.items.map((item) => (
                          <MilestoneCard
                            key={item.id}
                            item={item}
                            locale={locale}
                            t={t}
                            onOpen={openMedia}
                          />
                        ))}
                      </ol>
                    </li>
                  ))}
                  <li className="journey-year journey-now" id="journey-now">
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
            </section>
          </TabsContent>
          <TabsContent value="gallery" className="history-gallery-panel">
            <div className="history-gallery-heading">
              <p>{t("history.galleryLead")}</p>
              <label>
                {t("history.mediaFilter")}
                <select
                  value={mediaFilter}
                  onChange={(event) => setMediaFilter(event.target.value)}
                >
                  <option value="all">{t("common.all")}</option>
                  {["cover", "artwork", "model", "frame", "goods"].map(
                    (kind) => (
                      <option value={kind} key={kind}>
                        {t(`history.mediaKind.${kind}`)}
                      </option>
                    ),
                  )}
                </select>
              </label>
            </div>
            <div className="history-gallery-grid">
              {images.map((image, index) => (
                <ImageCard
                  key={image.id}
                  item={image}
                  locale={locale}
                  t={t}
                  onOpen={() => openMedia(images, index)}
                />
              ))}
            </div>
            {!images.length ? (
              <p className="history-empty">{t("history.galleryEmpty")}</p>
            ) : null}
          </TabsContent>
        </Tabs>
        <p className="history-note">{t("history.note")}</p>
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
      <HistoryImageViewer
        media={lightbox?.media}
        index={lightbox?.index}
        locale={locale}
        t={t}
        onClose={closeMedia}
        onStep={stepMedia}
      />
    </div>
  )
}
