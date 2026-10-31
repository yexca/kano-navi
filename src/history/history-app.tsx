import type { Locale, Translator } from "@/types"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import {
  ArrowRight,
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
  type LucideIcon,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ExternalLink } from "@/site/links"
import { MediaDialog, stepIndex } from "@/site/media-dialog"
import { SiteFooter, SiteHeader, SkipLink } from "@/site/site-shell"
import { branches, milestones } from "./milestones"
import {
  branchPortraits,
  historyMedia,
  localized,
  mediaByMilestone,
  sourceName,
  type LocalizedText,
} from "./media"
import {
  GalleryTile,
  HISTORY_IMAGE_SIZES,
  HistoryImage,
  ImageDetails,
  MilestoneMedia,
  type HistoryMediaItem,
} from "./history-media"
import "./history.css"

const FIRST_YEAR = 2010
const MEDIA_KINDS = ["cover", "artwork", "model", "frame", "goods"]
// The sticky header plus breathing room: a year counts as "being read" once
// its heading passes this line.
const ACTIVE_YEAR_OFFSET = 140

const kindIcons: Record<string, LucideIcon> = {
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

interface Milestone {
  id: string
  date: string
  dateEnd?: string
  dateNote?: string
  branch: string
  kind: string
  highlight?: boolean
  title: LocalizedText
  detail?: LocalizedText
  source: string | null
  links?: { href: string; label: string }[]
}

type RailSegment =
  | { gap: false; year: string; items: Milestone[] }
  | { gap: true; from: number; to: number }

const allMilestones = milestones as Milestone[]

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

function dayLabel(date: string) {
  const [, month, day] = date.split("-")
  return `${month}.${day}`
}

// Keeps the stored precision: a year-only or month-only entry is never shown
// as if it had a specific day.
function formatMilestoneDate(item: Milestone, locale: Locale, t: Translator) {
  const parts = item.date.split("-")
  let label: string
  if (parts.length === 1) label = t("history.wholeYear")
  else if (parts.length === 2)
    label = new Intl.DateTimeFormat(locale, {
      month: "short",
      timeZone: "UTC",
    }).format(new Date(Date.UTC(Number(parts[0]), Number(parts[1]) - 1, 1)))
  else label = dayLabel(item.date)
  if (item.dateEnd) label = `${label} – ${dayLabel(item.dateEnd)}`
  if (item.dateNote) label = `${label} · ${item.dateNote}`
  return label
}

function groupByYear(items: Milestone[]) {
  const groups: { year: string; items: Milestone[] }[] = []
  for (const item of items) {
    const year = item.date.slice(0, 4)
    const last = groups[groups.length - 1]
    if (last?.year === year) last.items.push(item)
    else groups.push({ year, items: [item] })
  }
  return groups
}

const allGroups = groupByYear(allMilestones)

// One stop per year with records; consecutive empty years fold into a gap so
// years without recorded milestones stay visible without taking space.
function railSegments(lastYear: number) {
  const byYear = Object.fromEntries(
    allGroups.map((group) => [group.year, group.items]),
  )
  const segments: RailSegment[] = []
  for (let year = FIRST_YEAR; year <= lastYear; year += 1) {
    const items = byYear[String(year)]
    const last = segments[segments.length - 1]
    if (items) segments.push({ gap: false, year: String(year), items })
    else if (last?.gap) last.to = year
    else segments.push({ gap: true, from: year, to: year })
  }
  return segments
}

function useActiveAnchor(ids: string[]) {
  const [active, setActive] = useState<string | null>(null)
  const key = ids.join(" ")
  useEffect(() => {
    const list = key ? key.split(" ") : []
    let frame = 0
    const update = () => {
      frame = 0
      let current: string | null = null
      for (const id of list) {
        const element = document.getElementById(id)
        if (
          element &&
          element.getBoundingClientRect().top <= ACTIVE_YEAR_OFFSET
        )
          current = id
      }
      setActive(current)
    }
    const schedule = () => {
      if (!frame) frame = window.requestAnimationFrame(update)
    }
    update()
    window.addEventListener("scroll", schedule, { passive: true })
    window.addEventListener("resize", schedule)
    return () => {
      window.cancelAnimationFrame(frame)
      window.removeEventListener("scroll", schedule)
      window.removeEventListener("resize", schedule)
    }
  }, [key])
  return active
}

function YearRail({
  lastYear,
  branchFilter,
  activeId,
  t,
}: {
  lastYear: number
  branchFilter: string
  activeId: string | null
  t: Translator
}) {
  const listRef = useRef<HTMLOListElement>(null)
  const segments = useMemo(() => railSegments(lastYear), [lastYear])

  // Keep the active stop visible when the rail scrolls sideways on phones.
  useEffect(() => {
    const list = listRef.current
    const active = list?.querySelector<HTMLElement>('[aria-current="location"]')
    if (!list || !active || list.scrollWidth <= list.clientWidth) return
    list.scrollTo({
      left: active.offsetLeft - list.clientWidth / 2,
      behavior: "smooth",
    })
  }, [activeId])

  return (
    <nav className="year-rail" aria-label={t("history.jumpYear")}>
      <ol ref={listRef}>
        {segments.map((segment) => {
          if (segment.gap) {
            const label =
              segment.from === segment.to
                ? String(segment.from)
                : `${segment.from}–${String(segment.to).slice(2)}`
            return (
              <li key={`gap-${segment.from}`} className="year-rail-gap">
                <span title={t("history.yearEmpty", { years: label })}>
                  {label}
                </span>
              </li>
            )
          }
          const matching = segment.items.filter(
            (item) => branchFilter === "all" || item.branch === branchFilter,
          ).length
          const id = `year-${segment.year}`
          const content = (
            <>
              <span className="year-rail-year">{segment.year}</span>
              <span className="year-rail-dots" aria-hidden="true">
                {segment.items.map((item) => (
                  <i
                    key={item.id}
                    className={`tone-${branchById[item.branch].tone}${
                      branchFilter === "all" || item.branch === branchFilter
                        ? ""
                        : " is-dim"
                    }`}
                  />
                ))}
              </span>
            </>
          )
          return (
            <li key={segment.year}>
              {matching ? (
                <a
                  href={`#${id}`}
                  aria-current={activeId === id ? "location" : undefined}
                  aria-label={`${segment.year} · ${t("history.branchCount", { count: matching })}`}
                >
                  {content}
                </a>
              ) : (
                <span className="is-disabled">{content}</span>
              )}
            </li>
          )
        })}
        <li>
          <a
            href="#journey-now"
            className="year-rail-now"
            aria-current={activeId === "journey-now" ? "location" : undefined}
          >
            <span className="year-rail-year">{t("history.now")}</span>
            <span className="year-rail-dots" aria-hidden="true">
              <i className="tone-berry is-pulse" />
            </span>
          </a>
        </li>
      </ol>
    </nav>
  )
}

function MilestoneEntry({
  item,
  locale,
  t,
  onOpen,
}: {
  item: Milestone
  locale: Locale
  t: Translator
  onOpen: (media: HistoryMediaItem[], index: number) => void
}) {
  const branch = branchById[item.branch]
  const Icon = kindIcons[item.kind] || Sparkles
  const detail = localized(item.detail, locale)
  const media: HistoryMediaItem[] = mediaByMilestone[item.id] || []
  return (
    <li
      className={`milestone tone-${branch.tone}${item.highlight ? " is-highlight" : ""}${
        media.length ? " has-media" : ""
      }`}
    >
      <span className="milestone-marker" aria-hidden="true">
        <Icon />
      </span>
      <div className="milestone-body">
        <p className="milestone-meta">
          <time dateTime={item.date}>
            {formatMilestoneDate(item, locale, t)}
          </time>
          <span className="tag">{localized(branch.label, locale)}</span>
          <span className="milestone-kind">
            {t(`history.kind.${item.kind}`)}
          </span>
        </p>
        <h3>{localized(item.title, locale)}</h3>
        {detail ? <p className="milestone-detail">{detail}</p> : null}
        <p className="milestone-links">
          {item.source ? (
            <ExternalLink className="text-link" href={item.source}>
              {sourceName(item.source, t)}
              <ArrowUpRight aria-hidden="true" />
            </ExternalLink>
          ) : (
            <span className="milestone-archive">
              {t("history.archiveOnly")}
            </span>
          )}
          {item.links?.map((link) => (
            <ExternalLink
              key={link.href}
              className="text-link"
              href={link.href}
            >
              {t(link.label)}
              <ArrowUpRight aria-hidden="true" />
            </ExternalLink>
          ))}
        </p>
      </div>
      {media.length ? (
        <MilestoneMedia
          media={media}
          locale={locale}
          t={t}
          onOpen={(index) => onOpen(media, index)}
        />
      ) : null}
    </li>
  )
}

export function HistoryApp() {
  const { locale, t } = useAppSettings()
  const [branchFilter, setBranchFilter] = useState("all")
  const [mediaFilter, setMediaFilter] = useState("all")
  const [view, setView] = useState("timeline")
  const [viewer, setViewer] = useState<{
    items: HistoryMediaItem[]
    index: number
  } | null>(null)

  useEffect(() => {
    document.title = t("history.metaTitle")
    document
      .querySelector('meta[name="description"]')
      ?.setAttribute("content", t("history.metaDescription"))
  }, [t])

  const lastYear = currentJapanYear()
  const groups = useMemo(
    () =>
      branchFilter === "all"
        ? allGroups
        : groupByYear(
            allMilestones.filter((item) => item.branch === branchFilter),
          ),
    [branchFilter],
  )
  const activeId = useActiveAnchor(
    view === "timeline" && groups.length
      ? [...groups.map((group) => `year-${group.year}`), "journey-now"]
      : [],
  )
  const branchMedia = useMemo(
    () =>
      historyMedia
        .filter(
          (item) =>
            branchFilter === "all" || item.branches.includes(branchFilter),
        )
        .sort((left, right) =>
          (right.date || "").localeCompare(left.date || ""),
        ),
    [branchFilter],
  )
  const images = useMemo(
    () =>
      mediaFilter === "all"
        ? branchMedia
        : branchMedia.filter((item) => item.category === mediaFilter),
    [branchMedia, mediaFilter],
  )
  const milestoneCount = groups.reduce(
    (count, group) => count + group.items.length,
    0,
  )
  const portrait = historyMedia.find((item) => item.id === branchPortraits.sona)

  const openMedia = useCallback(
    (items: HistoryMediaItem[], index: number) => setViewer({ items, index }),
    [],
  )
  const closeMedia = useCallback(() => setViewer(null), [])
  const stepMedia = useCallback(
    (offset: number) =>
      setViewer((current) =>
        current
          ? {
              ...current,
              index: stepIndex(current.index, offset, current.items.length),
            }
          : current,
      ),
    [],
  )

  return (
    <div className="site history" id="top">
      <SkipLink href="#journey">{t("history.eyebrow")}</SkipLink>
      <SiteHeader page="history" />

      <main className="site-main">
        <section className="history-intro" aria-labelledby="history-title">
          <div className="history-intro-copy">
            <p className="kicker">
              {t("history.eyebrow")} · {FIRST_YEAR} → {t("history.now")}
            </p>
            <h1 id="history-title">{t("history.title")}</h1>
            <p className="history-lead">{t("history.lead")}</p>
            <dl className="history-figures">
              <div>
                <dt>{t("history.stat.yearsLabel")}</dt>
                <dd>
                  {t("history.stat.years", { count: lastYear - FIRST_YEAR })}
                </dd>
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
            <p className="history-official">
              <ExternalLink
                className="pill-button"
                href="https://kano-official.amebaownd.com/"
              >
                {t("history.sourceName.artistSite")}
                <ArrowUpRight aria-hidden="true" />
              </ExternalLink>
              <ExternalLink
                className="pill-button"
                href="https://milpr.com/talents/kano-mahoro"
              >
                {t("history.sourceName.agency")}
                <ArrowUpRight aria-hidden="true" />
              </ExternalLink>
            </p>
          </div>
          {portrait ? (
            <figure className="history-portrait">
              <button
                type="button"
                className="media-opener"
                onClick={() => openMedia([portrait], 0)}
                aria-label={t("history.openImage", {
                  title: localized(portrait.title, locale),
                })}
              >
                <HistoryImage
                  item={portrait}
                  locale={locale}
                  t={t}
                  eager
                  sizes={HISTORY_IMAGE_SIZES.portrait}
                />
              </button>
              <figcaption>
                <span>
                  {branchById.sona.since} ·{" "}
                  {localized(branchById.sona.label, locale)}
                </span>
                <ExternalLink className="text-link" href={portrait.sourcePage}>
                  {t("history.heroImageSource")}
                  <ArrowUpRight aria-hidden="true" />
                </ExternalLink>
              </figcaption>
            </figure>
          ) : null}
        </section>

        <section className="identities" aria-labelledby="identities-title">
          <div className="identities-head">
            <h2 id="identities-title">{t("history.identities")}</h2>
            <p>{t("history.identitiesNote")}</p>
          </div>
          <div
            className="identity-picker"
            role="group"
            aria-label={t("history.filter")}
          >
            <button
              type="button"
              className="identity-option is-all"
              aria-pressed={branchFilter === "all"}
              onClick={() => setBranchFilter("all")}
            >
              <span className="identity-swatch" aria-hidden="true">
                {branches.map((branch) => (
                  <i key={branch.id} className={`tone-${branch.tone}`} />
                ))}
              </span>
              <span className="identity-copy">
                <strong>{t("history.allIdentities")}</strong>
                <small>
                  {t("history.branchCount", { count: milestones.length })}
                </small>
              </span>
            </button>
            {branches.map((branch) => {
              const image = historyMedia.find(
                (item) => item.id === branchPortraits[branch.id],
              )
              const count = milestones.filter(
                (item) => item.branch === branch.id,
              ).length
              return (
                <button
                  key={branch.id}
                  type="button"
                  className={`identity-option tone-${branch.tone}`}
                  aria-pressed={branchFilter === branch.id}
                  onClick={() =>
                    setBranchFilter((current) =>
                      current === branch.id ? "all" : branch.id,
                    )
                  }
                >
                  <span className="identity-portrait" aria-hidden="true">
                    {image ? (
                      <HistoryImage
                        item={image}
                        locale={locale}
                        t={t}
                        decorative
                        sizes={HISTORY_IMAGE_SIZES.identity}
                      />
                    ) : null}
                  </span>
                  <span className="identity-copy">
                    <strong>{localized(branch.label, locale)}</strong>
                    <small>
                      {t("history.branchSince", { year: branch.since })} ·{" "}
                      {t("history.branchCount", { count })}
                    </small>
                  </span>
                </button>
              )
            })}
          </div>
        </section>

        <Tabs
          value={view}
          className="history-views"
          onValueChange={(next) => {
            closeMedia()
            setView(next)
          }}
        >
          <div className="history-views-bar">
            <TabsList
              className="segmented history-tabs"
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
            <p className="history-count" role="status">
              {branchFilter === "all" ? null : (
                <span className={`tag tone-${branchById[branchFilter].tone}`}>
                  {localized(branchById[branchFilter].label, locale)}
                </span>
              )}
              {t("history.selectionCount", {
                milestones: milestoneCount,
                images: view === "gallery" ? images.length : branchMedia.length,
              })}
            </p>
          </div>

          <TabsContent value="timeline" className="timeline-view">
            <YearRail
              lastYear={lastYear}
              branchFilter={branchFilter}
              activeId={activeId}
              t={t}
            />
            <div className="journey" id="journey">
              {groups.length ? (
                <ol className="journey-years">
                  {groups.map((group) => (
                    <li
                      key={group.year}
                      className="journey-year"
                      id={`year-${group.year}`}
                    >
                      <h2 className="journey-year-label">
                        <span>{group.year}</span>
                        <small>
                          {t("history.branchCount", {
                            count: group.items.length,
                          })}
                        </small>
                      </h2>
                      <ol className="milestones">
                        {group.items.map((item) => (
                          <MilestoneEntry
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
                    <h2 className="journey-year-label">
                      <span>{t("history.now")}</span>
                    </h2>
                    <a className="journey-now-link" href="/">
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
            </div>
          </TabsContent>

          <TabsContent value="gallery" className="gallery-view">
            <div className="gallery-bar">
              <p>{t("history.galleryLead")}</p>
              <div
                className="segmented"
                role="group"
                aria-label={t("history.mediaFilter")}
              >
                {["all", ...MEDIA_KINDS].map((kind) => {
                  const count =
                    kind === "all"
                      ? branchMedia.length
                      : branchMedia.filter((item) => item.category === kind)
                          .length
                  return (
                    <button
                      key={kind}
                      type="button"
                      aria-pressed={mediaFilter === kind}
                      disabled={!count && mediaFilter !== kind}
                      onClick={() => setMediaFilter(kind)}
                    >
                      {kind === "all"
                        ? t("common.all")
                        : t(`history.mediaKind.${kind}`)}
                      <small>{count}</small>
                    </button>
                  )
                })}
              </div>
            </div>
            {images.length ? (
              <div className="gallery">
                {images.map((image, index) => (
                  <GalleryTile
                    key={image.id}
                    item={image}
                    locale={locale}
                    t={t}
                    onOpen={() => openMedia(images, index)}
                  />
                ))}
              </div>
            ) : (
              <p className="history-empty">{t("history.galleryEmpty")}</p>
            )}
          </TabsContent>
        </Tabs>
        <p className="history-note">{t("history.note")}</p>
      </main>

      <SiteFooter disclaimer={t("history.disclaimer")} />
      <MediaDialog
        items={viewer?.items}
        index={viewer?.index || 0}
        label={
          (viewer && localized(viewer.items[viewer.index]?.title, locale)) || ""
        }
        onClose={closeMedia}
        onStep={stepMedia}
        renderImage={(item) => (
          <HistoryImage key={item.id} item={item} locale={locale} t={t} eager />
        )}
        renderDetails={(item) => (
          <ImageDetails item={item} locale={locale} t={t} />
        )}
      />
    </div>
  )
}
