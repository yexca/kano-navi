import type { ReactNode } from "react"
import { useEffect, useId, useRef, useState } from "react"
import {
  AppWindow,
  ArrowUpRight,
  CodeXml,
  Scale,
  Smartphone,
} from "lucide-react"
import type { LucideIcon } from "lucide-react"

import { version } from "../../package.json"
import { useAppSettings } from "@/app-settings"
import { ExternalLink } from "@/site/links"
import { LICENSE_URL, REPOSITORY_URL } from "@/site/repository"
import { SiteNotice } from "@/site/site-notice"
import { usePageMeta } from "@/site/page-meta"
import { SiteFooter, SiteHeader, SkipLink } from "@/site/site-shell"
import "./about.css"

const AUTHOR = "yexca"
/**
 * GitHub accounts, named as on GitHub. Avatars load from GitHub's avatar
 * CDN, which answers anonymous cross-site image requests;
 * github.com/<login>.png may not.
 */
const ZHFUWE = "ZhFuwe"
const contributors: { login: string; name?: string }[] = [
  { login: "kanomahoro1224", name: "Lu Ke" },
  { login: ZHFUWE },
]
const LINEBLOG_RECOVERY_URL =
  "https://github.com/ZhFuwe/Kano_LINE_Blog_Recovery"
const ZHFUWE_URL = "https://github.com/ZhFuwe"
const LUKEKE_URL = "https://space.bilibili.com/426276698"

type App = {
  key: string
  icon: LucideIcon
  /** A proper name, or `about.name.<key>` when it reads differently by language. */
  maker: { name: string } | { key: string }
  /** Message keys of the platforms. */
  platforms: string[]
  /** The platforms whose builds are unsigned. */
  unsigned: string[]
  /** The lowest system version each platform's build runs on, where known. */
  minimum?: Record<string, string>
  link: { href: string; label: "download" | "repository" }
}

/** The apps that show the board, by Kano Navi or by others. */
const apps: App[] = [
  {
    key: "treasure",
    icon: Smartphone,
    maker: { key: "lukeke" },
    platforms: ["android", "harmonyos", "windows", "ios"],
    unsigned: ["ios"],
    minimum: { harmonyos: "5", ios: "13" },
    link: { href: "https://dl.xiaolubao.com", label: "download" },
  },
  {
    key: "apple",
    icon: AppWindow,
    maker: { name: AUTHOR },
    platforms: ["ios", "ipados", "macos"],
    unsigned: ["ios", "ipados", "macos"],
    minimum: { ios: "26", ipados: "26", macos: "26" },
    link: {
      href: "https://github.com/yexca/kano-navi-apple",
      label: "repository",
    },
  },
]

const technologyGroups = [
  {
    key: "ai",
    mono: true,
    items: ["Claude Opus 5.5", "Gemini 4 Hydrogen", "GPT-6.2-Sol"],
    more: ["GPT-6.1-Sol", "Gemini 4 Carbon", "GPT-6-Astra", "GPT-5.6-Sol"],
  },
  {
    key: "frontend",
    items: [
      "React",
      "TypeScript",
      "Vite",
      "Tailwind CSS",
      "Radix UI",
      "lucide-react",
    ],
  },
  {
    key: "backend",
    items: ["Go", "net/http", "SQLite", "modernc.org/sqlite", "yt-dlp", "MCP"],
  },
  { key: "delivery", items: ["Docker", "Docker Compose", "GitHub Actions"] },
]

/** One part of the page under a small label, as wide as the page. */
function Section({
  id,
  title,
  children,
}: {
  id: string
  title: ReactNode
  children?: ReactNode
}) {
  return (
    <section
      className={`about-section about-section-${id}`}
      aria-labelledby={`about-${id}-title`}
    >
      <h2 className="kicker" id={`about-${id}-title`}>
        {title}
      </h2>
      {children}
    </section>
  )
}

/** A message whose values are links, each to its person's or project's page. */
function CreditText({
  message,
  links,
}: {
  message: string
  links: Record<string, { text: string; href: string }>
}) {
  const { t } = useAppSettings()
  const markers = Object.fromEntries(
    Object.keys(links).map((param) => [param, `__LINK_${param}__`]),
  )
  // Splitting on a captured marker leaves each link's name at an odd index.
  const parts = t(message, markers).split(/__LINK_(\w+)__/u)
  return (
    <p>
      {parts.map((part, index) =>
        index % 2 && links[part] ? (
          <ExternalLink
            key={index}
            className="about-inline-link"
            href={links[part].href}
          >
            {links[part].text}
          </ExternalLink>
        ) : (
          part
        ),
      )}
    </p>
  )
}

/** A platform chip, with the lowest system version after the name. */
function PlatformTag({ app, platform }: { app: App; platform: string }) {
  const { t } = useAppSettings()
  const minimum = app.minimum?.[platform]
  return (
    <li className="tag">
      {t(`about.platform.${platform}`)}
      {minimum ? (
        <span className="about-app-minimum">{` ${minimum}+`}</span>
      ) : null}
    </li>
  )
}

function AppCard({ app }: { app: App }) {
  const { t } = useAppSettings()
  const { icon: Icon, link } = app
  const maker =
    "name" in app.maker ? app.maker.name : t(`about.name.${app.maker.key}`)
  const signed = app.platforms.filter((key) => !app.unsigned.includes(key))
  const unsigned = app.platforms.filter((key) => app.unsigned.includes(key))
  return (
    <li className="about-app">
      <span className="about-app-icon" aria-hidden="true">
        <Icon />
      </span>
      <div className="about-app-name">
        <h3>{t(`about.app.${app.key}`)}</h3>
        <p>{t("about.by", { author: maker })}</p>
      </div>
      <ExternalLink className="pill-button about-app-action" href={link.href}>
        {link.label === "repository" ? <CodeXml aria-hidden="true" /> : null}
        {t(`about.${link.label}`)}
        <ArrowUpRight aria-hidden="true" />
      </ExternalLink>
      <ul className="about-app-platforms" aria-label={t("about.platforms")}>
        {signed.map((key) => (
          <PlatformTag key={key} app={app} platform={key} />
        ))}
        {unsigned.length ? (
          <li className="about-app-unsigned">
            <ul>
              {unsigned.map((key) => (
                <PlatformTag key={key} app={app} platform={key} />
              ))}
            </ul>
            <span>{t("about.unsigned")}</span>
          </li>
        ) : null}
      </ul>
    </li>
  )
}

function Card({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <li className="about-card">
      <h3>{label}</h3>
      {children}
    </li>
  )
}

/**
 * The rest of a list behind "+N": a button that opens them in a small
 * panel, which Escape, a click elsewhere or leaving it with the keyboard
 * closes.
 */
function MoreItems({ items, label }: { items: string[]; label: string }) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLSpanElement>(null)
  const button = useRef<HTMLButtonElement>(null)
  const id = useId()
  useEffect(() => {
    if (!open) return
    const outside = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return
      setOpen(false)
      button.current?.focus()
    }
    document.addEventListener("pointerdown", outside)
    document.addEventListener("keydown", escape)
    return () => {
      document.removeEventListener("pointerdown", outside)
      document.removeEventListener("keydown", escape)
    }
  }, [open])
  return (
    <span
      className="about-more"
      ref={root}
      onBlur={(event) => {
        if (!root.current?.contains(event.relatedTarget as Node | null))
          setOpen(false)
      }}
    >
      <button
        ref={button}
        type="button"
        className="about-more-button"
        aria-expanded={open}
        aria-controls={id}
        aria-label={label}
        onClick={() => setOpen((value) => !value)}
      >
        +{items.length}
      </button>
      <ul id={id} className="about-more-panel" hidden={!open}>
        {items.map((item) => (
          <li key={item}>{item}</li>
        ))}
      </ul>
    </span>
  )
}

export function AboutApp() {
  const { t } = useAppSettings()

  usePageMeta(t("about.metaTitle"), t("about.metaDescription"))

  return (
    <div className="site about" id="top">
      <SkipLink href="#about-content">{t("about.skip")}</SkipLink>
      <SiteHeader page="about" />

      <main className="site-main" id="about-content">
        <section className="about-hero" aria-labelledby="about-title">
          <h1 id="about-title">
            {t("site.name")}
            <span aria-hidden="true">🍓🦌</span>
          </h1>
          <p className="about-lead">{t("about.overviewText")}</p>
          <div className="about-meta">
            <nav className="about-links" aria-label={t("about.projectLinks")}>
              <ExternalLink
                className="pill-button is-solid"
                href={REPOSITORY_URL}
              >
                <CodeXml aria-hidden="true" />
                {t("about.sourceCode")}
                <ArrowUpRight aria-hidden="true" />
              </ExternalLink>
              <ExternalLink
                className="pill-button"
                href={LICENSE_URL}
                aria-label={`${t("about.license")}: AGPL-3.0`}
              >
                <Scale aria-hidden="true" />
                AGPL-3.0
                <ArrowUpRight aria-hidden="true" />
              </ExternalLink>
            </nav>
            <p className="about-edition">
              <span>Kano Navi v{version}</span>
              <span>{t("about.by", { author: AUTHOR })}</span>
            </p>
          </div>
        </section>

        <Section id="apps" title={t("about.apps")}>
          <ul className="about-app-list">
            {apps.map((app) => (
              <AppCard key={app.key} app={app} />
            ))}
          </ul>
        </Section>

        <Section id="credits" title={t("about.credits")}>
          <ul className="about-cards">
            <Card label={t("about.contributors")}>
              <p>{t("about.contributorsText")}</p>
              <ul
                className="about-contributors"
                aria-label={t("about.contributorList")}
              >
                {contributors.map(({ login, name }) => (
                  <li key={login}>
                    <ExternalLink href={`https://github.com/${login}`}>
                      <img
                        src={`https://avatars.githubusercontent.com/${login}?size=128`}
                        alt=""
                        referrerPolicy="no-referrer"
                        width={48}
                        height={48}
                        loading="lazy"
                        decoding="async"
                      />
                      <span>
                        {name ? (
                          <>
                            {name} <small>({login})</small>
                          </>
                        ) : (
                          login
                        )}
                      </span>
                    </ExternalLink>
                  </li>
                ))}
              </ul>
            </Card>
            <Card label="LINE BLOG">
              <CreditText
                message="about.creditsLineblog"
                links={{
                  name: { text: ZHFUWE, href: ZHFUWE_URL },
                  project: {
                    text: "Kano_LINE_Blog_Recovery",
                    href: LINEBLOG_RECOVERY_URL,
                  },
                }}
              />
            </Card>
            <Card label={t("works.label")}>
              <CreditText
                message="about.creditsWorks"
                links={{ name: { text: ZHFUWE, href: ZHFUWE_URL } }}
              />
            </Card>
            <Card label={t("about.mainland")}>
              <CreditText
                message="about.creditsMainland"
                links={{
                  provider: { text: t("about.name.lukeke"), href: LUKEKE_URL },
                }}
              />
            </Card>
          </ul>
        </Section>

        <Section id="stack" title={t("about.builtWith")}>
          <p>{t("about.aiCredit", { author: AUTHOR })}</p>
          <ul className="about-cards">
            {technologyGroups.map(({ key, items, mono, more }) => (
              <Card key={key} label={t(`about.group.${key}`)}>
                <ul className={mono ? "about-chips is-mono" : "about-chips"}>
                  {items.map((item) => (
                    <li key={item}>{item}</li>
                  ))}
                  {more?.length ? (
                    <li className="is-more">
                      <MoreItems
                        items={more}
                        label={t("about.moreModels", {
                          count: more.length,
                        })}
                      />
                    </li>
                  ) : null}
                </ul>
              </Card>
            ))}
          </ul>
        </Section>

        <Section id="notice" title={t("about.notice")}>
          <SiteNotice />
        </Section>
      </main>

      <SiteFooter />
    </div>
  )
}
