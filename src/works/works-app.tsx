import type { ComponentType, CSSProperties } from "react"
import type { Translator } from "@/types"
import { useCallback, useEffect, useRef, useState } from "react"
import { ArrowUpRight, Info, RotateCw } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { mediaSrcSet } from "@/lib/media"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ExternalLink } from "@/site/links"
import { REPOSITORY_URL } from "@/site/repository"
import { MediaDialog, stepIndex } from "@/site/media-dialog"
import {
  Placeholder,
  SiteFooter,
  SiteHeader,
  SkipLink,
} from "@/site/site-shell"
import {
  OFFICIAL_DISCOGRAPHY,
  fullDate,
  mediaUrl,
  personalReleases,
  worksSummary,
} from "./content"
import {
  PersonalView,
  ReleaseSheet,
  Sleeve,
  frontCover,
  type OpenCovers,
} from "./personal-view"
import type { PersonalRelease, WorksCover } from "./types"
import { CoverDetails, WORKS_COVER_SIZES, WorksCoverImage } from "./works-media"
import "./works.css"

type Section = "personal" | "participation" | "uploads"
const SECTIONS: Section[] = ["personal", "participation", "uploads"]

type SectionProps = {
  onOpenCovers: OpenCovers
}

/** The two longer sections load their code and data when first shown. */
const sectionLoaders: Record<
  Exclude<Section, "personal">,
  () => Promise<ComponentType<SectionProps>>
> = {
  participation: () =>
    import("./participation-view").then((module) => module.ParticipationView),
  uploads: () => import("./uploads-view").then((module) => module.UploadsView),
}

const loadedSections = new Map<Section, ComponentType<SectionProps>>()

function useSection(section: Exclude<Section, "personal">, wanted: boolean) {
  const [state, setState] = useState<{
    View: ComponentType<SectionProps> | null
    failed: boolean
  }>(() => ({ View: loadedSections.get(section) ?? null, failed: false }))
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    if (!wanted || state.View) return undefined
    let current = true
    sectionLoaders[section]().then(
      (View) => {
        loadedSections.set(section, View)
        if (current) setState({ View, failed: false })
      },
      () => {
        if (current) setState({ View: null, failed: true })
      },
    )
    return () => {
      current = false
    }
  }, [section, wanted, state.View, attempt])
  const retry = useCallback(() => {
    setState({ View: null, failed: false })
    setAttempt((value) => value + 1)
  }, [])
  return { ...state, retry }
}

function sectionFromUrl(): Section {
  const value = new URLSearchParams(window.location.search).get("view")
  return SECTIONS.includes(value as Section) ? (value as Section) : "personal"
}

/** A release another page links to with `?release=<id>`, opened on load. */
function releaseFromUrl() {
  const id = new URLSearchParams(window.location.search).get("release")
  return personalReleases.some((release) => release.id === id) ? id : null
}

/** The acoustic cover trilogy, in album order. */
const TRILOGY = ["one", "two", "three"]

/**
 * The scene has two sides, like a record: side A keeps album order, side B
 * leads with two. The figures stay in album order in the document, so
 * flipping moves them on screen without reordering them.
 */
type Side = "a" | "b"
const SIDES: Record<Side, string[]> = {
  a: ["one", "two", "three"],
  b: ["two", "one", "three"],
}

/** This device remembers the side last played. */
const SIDE_KEY = "kano-works-trilogy-side"

function readSide(): Side {
  try {
    return window.localStorage.getItem(SIDE_KEY) === "b" ? "b" : "a"
  } catch {
    return "a"
  }
}

/**
 * Flipping sides: one and two sink into the ground with their discs and
 * labels, the ground and its flowers shift, and the two rise in their new
 * places. Each part moves on its own layer, so the discs stay behind the
 * hills and the figures in front, and only opacity and translate change,
 * which the browser can animate without repainting the figures.
 */
const SINK_MS = 260
const RISE_MS = 700
const RISE_DELAY_MS = 160
const SINK_DEPTH = "22px"
const FLIPPING_PARTS =
  ":is(.is-one, .is-two) :is(.works-figure-sun, .works-figure img, .works-figure-label)"

/** A part sunk out of sight; a disc stays centred on its own left edge. */
function sunk(part: HTMLElement): Keyframe {
  const x = part.classList.contains("works-figure-sun") ? "-50%" : "0"
  return { opacity: 0, translate: `${x} ${SINK_DEPTH}` }
}

function useTrilogySide() {
  const [side, setSide] = useState<Side>(readSide)
  const stage = useRef<HTMLUListElement>(null)
  const busy = useRef(false)
  const flip = async () => {
    if (busy.current) return
    const next: Side = side === "a" ? "b" : "a"
    try {
      window.localStorage.setItem(SIDE_KEY, next)
    } catch {
      // The side still applies until the page closes.
    }
    const still = window.matchMedia("(prefers-reduced-motion: reduce)").matches
    const parts =
      stage.current && !still
        ? [...stage.current.querySelectorAll<HTMLElement>(FLIPPING_PARTS)]
        : []
    if (!parts.length) {
      setSide(next)
      return
    }
    busy.current = true
    const sinking = parts.map((part) =>
      part.animate([sunk(part)], {
        duration: SINK_MS,
        easing: "ease-in",
        fill: "forwards",
      }),
    )
    await Promise.all(sinking.map((animation) => animation.finished)).catch(
      () => undefined,
    )
    setSide(next)
    // The rise holds each part sunk until it starts, so nothing shows in the
    // old place between the two animations.
    const rising = parts.map((part) =>
      part.animate([{ ...sunk(part), offset: 0 }], {
        duration: RISE_MS,
        delay: RISE_DELAY_MS,
        easing: "cubic-bezier(0.2, 0.7, 0.2, 1)",
        fill: "backwards",
      }),
    )
    sinking.forEach((animation) => animation.cancel())
    await Promise.all(rising.map((animation) => animation.finished)).catch(
      () => undefined,
    )
    busy.current = false
  }
  return { side, stage, flip }
}

const featured = TRILOGY.map((title) =>
  personalReleases.find((release) => release.title === title),
).filter((release) => release !== undefined)

/** Each album's own colour, taken from its cover: scarf, stripes, dress. */
const FEATURED_TONES: Record<string, string> = {
  one: "#c8432f",
  two: "#e0aa2b",
  three: "#4862bd",
}

/** The trilogy stands as the figures cut from its covers when all have one. */
const figuresReady =
  featured.length === TRILOGY.length &&
  featured.every((release) => release.cutout)

/**
 * A figure cut from an album's cover, standing before a disc in the album's
 * colour. Without its image it falls back to the album's sleeve.
 */
function FeaturedFigure({
  release,
  t,
}: {
  release: PersonalRelease
  t: Translator
}) {
  const [failed, setFailed] = useState(false)
  const cutout = release.cutout
  if (!cutout || failed)
    return (
      <Sleeve
        cover={frontCover(release)}
        disc={release.disc}
        t={t}
        sizes={WORKS_COVER_SIZES.trio}
        eager
        className="is-trio"
      />
    )
  const url = mediaUrl(cutout.media)
  return (
    <span className="works-figure">
      <span className="works-figure-sun" aria-hidden="true" />
      <img
        src={url}
        srcSet={mediaSrcSet(url, cutout.width)}
        sizes={WORKS_COVER_SIZES.figure}
        alt=""
        width={cutout.width}
        height={cutout.height}
        decoding="async"
        onError={() => setFailed(true)}
      />
      <span className="works-figure-label" aria-hidden="true">
        <b>{String(TRILOGY.indexOf(release.title) + 1).padStart(2, "0")}</b>
        {release.title}
      </span>
    </span>
  )
}

type Point = [number, number]
type Curve = { start: Point; segments: [Point, Point, Point][] }

/**
 * The ground the figures stand and sit on, one per side. On side A one
 * stands at the left and two sits lowest in front of the rise; on side B two
 * sits lowest at the left, one stands in the middle and three on the rise.
 * Both curves use the same commands, so the ground morphs between them, and
 * each figure's base in styles/hero.css sits on its side's curve.
 */
const GROUND: Record<Side, Curve> = {
  b: {
    start: [0, 900],
    segments: [
      [
        [50, 922],
        [120, 928],
        [260, 928],
      ],
      [
        [400, 928],
        [470, 924],
        [530, 902],
      ],
      [
        [570, 888],
        [610, 880],
        [700, 878],
      ],
      [
        [790, 876],
        [840, 860],
        [930, 858],
      ],
      [
        [1030, 850],
        [1110, 862],
        [1160, 872],
      ],
    ],
  },
  a: {
    start: [0, 900],
    segments: [
      [
        [90, 884],
        [150, 876],
        [230, 878],
      ],
      [
        [295, 880],
        [345, 895.5],
        [397.5, 910.75],
      ],
      [
        [450, 926],
        [505, 941],
        [580, 942],
      ],
      [
        [730, 944],
        [800, 868],
        [930, 858],
      ],
      [
        [1030, 850],
        [1110, 862],
        [1160, 872],
      ],
    ],
  },
}

const HILLS: Curve[] = [
  {
    start: [0, 600],
    segments: [
      [
        [120, 540],
        [250, 548],
        [360, 590],
      ],
      [
        [470, 630],
        [580, 560],
        [700, 550],
      ],
      [
        [820, 540],
        [930, 610],
        [1040, 580],
      ],
      [
        [1100, 565],
        [1140, 550],
        [1160, 548],
      ],
    ],
  },
  {
    start: [0, 760],
    segments: [
      [
        [120, 700],
        [250, 700],
        [360, 740],
      ],
      [
        [470, 778],
        [580, 720],
        [700, 705],
      ],
      [
        [820, 690],
        [920, 750],
        [1030, 738],
      ],
      [
        [1090, 730],
        [1130, 712],
        [1160, 706],
      ],
    ],
  },
]

const curvePath = ({ start, segments }: Curve) =>
  `M${start.join(" ")}` +
  segments
    .map((points) => ` C${points.map((p) => p.join(" ")).join(" ")}`)
    .join("")

const filled = (curve: Curve) => `${curvePath(curve)} V1000 H0 Z`

/** A point a fraction t along one cubic segment of a curve. */
function pointOn({ start, segments }: Curve, segment: number, t: number) {
  const from = segment ? segments[segment - 1][2] : start
  const [c1, c2, to] = segments[segment]
  const u = 1 - t
  const axis = (i: 0 | 1) =>
    u * u * u * from[i] +
    3 * u * u * t * c1[i] +
    3 * u * t * t * c2[i] +
    t * t * t * to[i]
  return [axis(0), axis(1)] as Point
}

/** Grass along the ground's crest: three blades every few steps. */
const grass = (ground: Curve) =>
  ground.segments
    .flatMap((_, segment) =>
      [0.08, 0.3, 0.52, 0.74, 0.94].map((t, i) => {
        const [x, y] = pointOn(ground, segment, t)
        const lean = (segment + i) % 2 ? 1 : -1
        return `M${x} ${y + 2}l${-5 * lean} -15M${x + 4} ${y + 2}l${2 * lean} -20M${x + 8} ${y + 2}l${7 * lean} -13`
      }),
    )
    .join("")

type Flower = readonly [segment: number, t: number, title: string]

/**
 * Small flowers in the three albums' colours, along the crest. Each side has
 * as many, so a flower drifts to its place on the other side.
 */
const FLOWER_SPOTS: Record<Side, readonly Flower[]> = {
  b: [
    [0, 0.15, "one"],
    [0, 0.75, "three"],
    [1, 0.6, "two"],
    [2, 0.5, "three"],
    [3, 0.3, "one"],
    [3, 0.85, "two"],
    [4, 0.55, "one"],
  ],
  a: [
    [0, 0.2, "one"],
    [0, 0.7, "three"],
    [1, 0.9, "two"],
    [2, 0.7, "one"],
    [3, 0.25, "three"],
    [3, 0.8, "two"],
    [4, 0.5, "one"],
  ],
}

type Land = {
  near: string
  crest: string
  grass: string
  flowers: { x: number; y: number; tone: string }[]
}

function land(side: Side): Land {
  const ground = GROUND[side]
  return {
    near: filled(ground),
    crest: curvePath(ground),
    grass: grass(ground),
    flowers: FLOWER_SPOTS[side].map(([segment, t, title]) => {
      const [x, y] = pointOn(ground, segment, t)
      return { x, y: y - 9, tone: FEATURED_TONES[title] }
    }),
  }
}

const LAND: Record<Side, Land> = { a: land("a"), b: land("b") }

/**
 * Rolling hills behind the figures, after the night hills of a fan-made MV:
 * two far ridges the discs sink behind, then the grassy ground in front.
 */
function FeaturedLandscape({ side }: { side: Side }) {
  const { near, crest, grass, flowers } = LAND[side]
  return (
    <svg
      className="works-landscape"
      viewBox="0 0 1160 1000"
      preserveAspectRatio="none"
      aria-hidden="true"
      focusable="false"
    >
      <path className="is-far" d={filled(HILLS[0])} />
      <path className="is-mid" d={filled(HILLS[1])} />
      <path className="is-near" d={near} />
      <path className="is-crest" d={crest} />
      <path className="is-grass" d={grass} />
      {flowers.map((flower, index) => (
        <g key={index} className="is-flower">
          <circle cx={flower.x} cy={flower.y} r="6.5" fill={flower.tone} />
          <circle cx={flower.x} cy={flower.y} r="2.2" />
        </g>
      ))}
    </svg>
  )
}

/**
 * Flips the trilogy to its other side: a small two-sided record turns over
 * while one and two sink and rise in their places on that side.
 */
function SideSwitch({
  side,
  onFlip,
  t,
}: {
  side: Side
  onFlip: () => void
  t: Translator
}) {
  const other: Side = side === "a" ? "b" : "a"
  const action = t("works.trilogyFlip", {
    side: other.toUpperCase(),
    order: SIDES[other].join(", "),
  })
  return (
    <button
      type="button"
      className="works-side"
      data-side={side}
      aria-label={action}
      title={action}
      onClick={onFlip}
    >
      <span aria-hidden="true">
        {t("works.trilogySide", { side: side.toUpperCase() })}
      </span>
      <span className="works-side-disc" aria-hidden="true">
        <span className="is-a">A</span>
        <span className="is-b">B</span>
      </span>
    </button>
  )
}

function LazySection({
  section,
  active,
  onOpenCovers,
}: {
  section: Exclude<Section, "personal">
  active: boolean
  onOpenCovers: OpenCovers
}) {
  const { t } = useAppSettings()
  const { View, failed, retry } = useSection(section, active)
  if (View) return <View onOpenCovers={onOpenCovers} />
  if (failed)
    return (
      <div className="works-load-failed" role="alert">
        <p>{t("works.loadFailed")}</p>
        <button type="button" className="works-chip" onClick={retry}>
          <RotateCw aria-hidden="true" />
          {t("works.retry")}
        </button>
      </div>
    )
  return (
    <div className="works-loading" role="status">
      <span className="sr-only">{t("works.loading")}</span>
      <Placeholder lines={4} />
    </div>
  )
}

export function WorksApp() {
  const { locale, t } = useAppSettings()
  const [section, setSection] = useState<Section>(sectionFromUrl)
  const [openId, setOpenId] = useState<string | null>(releaseFromUrl)
  const [pointerTabs, setPointerTabs] = useState(false)
  const trilogy = useTrilogySide()
  const [viewer, setViewer] = useState<{
    items: WorksCover[]
    index: number
    title: string
  } | null>(null)

  useEffect(() => {
    document.title = t("works.metaTitle")
    document
      .querySelector('meta[name="description"]')
      ?.setAttribute("content", t("works.metaDescription"))
  }, [t])

  // The linked release opens once; the address keeps only the section.
  useEffect(() => {
    const url = new URL(window.location.href)
    if (!url.searchParams.has("release")) return
    url.searchParams.delete("release")
    window.history.replaceState(window.history.state, "", url.href)
  }, [])

  useEffect(() => {
    const onPop = () => setSection(sectionFromUrl())
    window.addEventListener("popstate", onPop)
    return () => window.removeEventListener("popstate", onPop)
  }, [])

  const choose = (next: string) => {
    const value = next as Section
    setSection(value)
    const url = new URL(window.location.href)
    if (value === "personal") url.searchParams.delete("view")
    else url.searchParams.set("view", value)
    window.history.replaceState(window.history.state, "", url.href)
  }

  const openCovers = useCallback<OpenCovers>((items, index, title) => {
    setViewer({ items, index, title })
  }, [])
  const closeCovers = useCallback(() => setViewer(null), [])
  const stepCovers = useCallback((offset: number) => {
    setViewer((current) =>
      current
        ? {
            ...current,
            index: stepIndex(current.index, offset, current.items.length),
          }
        : current,
    )
  }, [])

  const counts: Record<Section, number> = {
    personal: worksSummary.personal,
    participation: worksSummary.participation,
    uploads: worksSummary.uploads,
  }
  const open = personalReleases.find((release) => release.id === openId)
  // The caveat's sentence carries the feedback link where {link} stands.
  const [before, after = ""] = t("works.caveat").split("{link}")

  return (
    <div className="site works" id="top">
      <SkipLink href="#works-content">{t("works.skip")}</SkipLink>
      <SiteHeader page="works" />

      <main className="site-main" id="works-content">
        <section className="works-hero" aria-labelledby="works-title">
          <div className="works-hero-copy">
            <p className="works-eyebrow">
              <span>{t("works.kicker")}</span>
              <span>
                {worksSummary.firstYear}–{worksSummary.lastYear}
              </span>
              <ExternalLink
                className="works-official"
                href={OFFICIAL_DISCOGRAPHY}
              >
                {t("works.officialDiscography")}
                <ArrowUpRight aria-hidden="true" />
              </ExternalLink>
            </p>
            <h1 id="works-title">{t("works.title")}</h1>
            <p className="works-lead">{t("works.lead")}</p>
            <p className="works-caveat">
              <Info aria-hidden="true" />
              <span>
                {before}
                <ExternalLink
                  href={`${REPOSITORY_URL}/issues/new?template=works_correction.yml`}
                >
                  {t("works.feedback")}
                </ExternalLink>
                {after}
              </span>
            </p>
            {/* Every section is packaged by hand, so all share one date. */}
            <p className="works-updated">
              {t("works.updatedThrough", {
                date: fullDate(worksSummary.updatedAt, locale),
              })}
            </p>
          </div>
          {featured.length ? (
            <div className="works-trio">
              <div className="works-trio-scene">
                {figuresReady ? (
                  <FeaturedLandscape side={trilogy.side} />
                ) : null}
                <ul
                  ref={trilogy.stage}
                  className={`works-trio-stage is-side-${trilogy.side}${figuresReady ? " is-figures" : ""}`}
                  aria-label={t("works.featured")}
                >
                  {featured.map((release) => {
                    const place = SIDES[trilogy.side].indexOf(release.title)
                    return (
                      <li
                        key={release.id}
                        className={`is-${place} is-${release.title}`}
                        style={
                          {
                            "--tone": FEATURED_TONES[release.title],
                            "--order": place,
                          } as CSSProperties
                        }
                      >
                        <button
                          type="button"
                          className="works-trio-item"
                          aria-haspopup="dialog"
                          aria-label={`${release.title} · ${fullDate(release.date, locale)}`}
                          onClick={() => setOpenId(release.id)}
                        >
                          {figuresReady ? (
                            <FeaturedFigure release={release} t={t} />
                          ) : (
                            <Sleeve
                              cover={frontCover(release)}
                              disc={release.disc}
                              t={t}
                              sizes={WORKS_COVER_SIZES.trio}
                              eager
                              className="is-trio"
                            />
                          )}
                        </button>
                      </li>
                    )
                  })}
                </ul>
                {figuresReady ? (
                  <SideSwitch side={trilogy.side} onFlip={trilogy.flip} t={t} />
                ) : null}
              </div>
            </div>
          ) : null}
        </section>

        <Tabs value={section} className="works-sections" onValueChange={choose}>
          {/*
           * The tabs focus themselves when chosen, which the browser counts
           * as keyboard focus; a pointer choice hides that outline until the
           * next key press brings it back.
           */}
          <TabsList
            className="works-tabs"
            aria-label={t("works.sections")}
            data-pointer={pointerTabs || undefined}
            onPointerDown={() => setPointerTabs(true)}
            onKeyDown={() => setPointerTabs(false)}
          >
            {SECTIONS.map((item) => (
              <TabsTrigger key={item} value={item}>
                {t(`works.section.${item}`)}
                <sup>{counts[item]}</sup>
              </TabsTrigger>
            ))}
          </TabsList>

          <TabsContent value="personal" className="works-panel">
            <PersonalView t={t} onOpenRelease={setOpenId} />
          </TabsContent>
          <TabsContent value="participation" className="works-panel">
            <LazySection
              section="participation"
              active={section === "participation"}
              onOpenCovers={openCovers}
            />
          </TabsContent>
          <TabsContent value="uploads" className="works-panel">
            <LazySection
              section="uploads"
              active={section === "uploads"}
              onOpenCovers={openCovers}
            />
          </TabsContent>
        </Tabs>
      </main>

      <SiteFooter />
      {open ? (
        <ReleaseSheet
          key={open.id}
          release={open}
          locale={locale}
          t={t}
          onClose={() => setOpenId(null)}
          onOpenCovers={openCovers}
        />
      ) : null}
      <MediaDialog
        items={viewer?.items}
        index={viewer?.index || 0}
        label={viewer?.title || ""}
        onClose={closeCovers}
        onStep={stepCovers}
        scaleUp
        renderImage={(item) => (
          <WorksCoverImage
            key={item.media}
            cover={item}
            alt={viewer?.title || ""}
            t={t}
            eager
          />
        )}
        renderDetails={(item) => (
          <CoverDetails cover={item} title={viewer?.title || ""} t={t} />
        )}
      />
    </div>
  )
}
