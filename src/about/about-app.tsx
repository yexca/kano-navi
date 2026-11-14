import type { ReactNode } from "react"
import { useEffect } from "react"
import {
  ArrowRight,
  ArrowUpRight,
  CodeXml,
  Globe,
  Scale,
  Smartphone,
} from "lucide-react"
import type { LucideIcon } from "lucide-react"

import { version } from "../../package.json"
import { useAppSettings } from "@/app-settings"
import { ExternalLink } from "@/site/links"
import { LICENSE_URL, REPOSITORY_URL } from "@/site/repository"
import { SiteNotice } from "@/site/site-notice"
import { SiteFooter, SiteHeader, SkipLink } from "@/site/site-shell"
import "./about.css"

const AUTHOR = "yexca"
const CONTRIBUTORS_URL = `${REPOSITORY_URL}/graphs/contributors`
/**
 * GitHub logins. Avatars load from GitHub's avatar CDN, which answers
 * anonymous cross-site image requests; github.com/<login>.png may not.
 */
const contributors = ["kanomahoro1224"]
const LINEBLOG_RECOVERY_URL =
  "https://github.com/ZhFuwe/Kano_LINE_Blog_Recovery"
const ZHFUWE = "ZhFuwe"
const ZHFUWE_URL = "https://github.com/ZhFuwe"
const MAINLAND_PROVIDER = "鹿可可"
const MAINLAND_PROVIDER_URL = "https://space.bilibili.com/426276698"

type Place = {
  key: string
  /** A proper name shown as written; otherwise `about.place.<key>`. */
  name?: string
  /** The language of `name`, so the browser picks matching glyphs. */
  nameLang?: string
  icon: LucideIcon
  maker: string
  /** Message keys of the platform labels. */
  platforms: string[]
  /** Omitted while the place is still in development. */
  link?: { href: string; label: "download" | "visit" }
}

/** Other apps and sites that show the board, by Kano Navi or by others. */
const places: Place[] = [
  {
    key: "xiaolubao",
    name: "小鹿包们的宝物",
    nameLang: "zh-CN",
    icon: Smartphone,
    maker: MAINLAND_PROVIDER,
    platforms: ["android", "ios", "harmonyos"],
    link: { href: "https://dl.xiaolubao.com", label: "download" },
  },
  {
    key: "ios",
    icon: Smartphone,
    maker: AUTHOR,
    platforms: ["iphone", "ios26"],
  },
  {
    key: "mainland",
    icon: Globe,
    maker: MAINLAND_PROVIDER,
    platforms: ["web"],
    link: { href: "https://xiaolubao.com", label: "visit" },
  },
]

const technologyGroups = [
  {
    key: "ai",
    mono: true,
    items: ["Claude Opus 5.5", "GPT-6-Astra", "GPT-6.1-Sol", "GPT-5.6-Sol"],
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

function Chapter({
  number,
  title,
  wide = false,
  children,
}: {
  number: number
  title: ReactNode
  wide?: boolean
  children?: ReactNode
}) {
  const id = `about-chapter-${number}`
  return (
    <section className="chapter" aria-labelledby={id}>
      <h2 id={id}>
        <span className="chapter-number" aria-hidden="true">
          {String(number).padStart(2, "0")}
        </span>
        {title}
      </h2>
      <div className={wide ? "chapter-body is-wide" : "chapter-body"}>
        {children}
      </div>
    </section>
  )
}

/** A message with one of its values as a link to that person's page. */
function CreditText({
  message,
  param,
  name,
  href,
}: {
  message: string
  param: string
  name: string
  href: string
}) {
  const { t } = useAppSettings()
  const marker = "__CREDIT_NAME__"
  const [before, after = ""] = t(message, { [param]: marker }).split(marker)
  return (
    <p>
      {before}
      <ExternalLink className="about-inline-link" href={href}>
        {name}
      </ExternalLink>
      {after}
    </p>
  )
}

function PlaceRow({ place }: { place: Place }) {
  const { t } = useAppSettings()
  const { icon: Icon, link } = place
  return (
    <li className="about-place">
      <span className="about-place-icon" aria-hidden="true">
        <Icon />
      </span>
      <div className="about-place-name">
        <h3 lang={place.nameLang}>
          {place.name ?? t(`about.place.${place.key}`)}
        </h3>
        <p>{t("about.by", { author: place.maker })}</p>
      </div>
      <ul className="about-place-platforms" aria-label={t("about.platforms")}>
        {place.platforms.map((platform) => (
          <li key={platform} className="tag">
            {t(`about.platform.${platform}`)}
          </li>
        ))}
      </ul>
      <div className="about-place-action">
        {link ? (
          <ExternalLink className="pill-button" href={link.href}>
            {t(`about.${link.label}`)}
            <ArrowUpRight aria-hidden="true" />
          </ExternalLink>
        ) : (
          <span className="about-place-status">{t("about.inDevelopment")}</span>
        )}
      </div>
    </li>
  )
}

function CreditItem({
  label,
  children,
}: {
  label: ReactNode
  children: ReactNode
}) {
  return (
    <li className="about-credit">
      <h3>{label}</h3>
      <div>{children}</div>
    </li>
  )
}

export function AboutApp() {
  const { t } = useAppSettings()

  useEffect(() => {
    document.title = t("about.metaTitle")
    document
      .querySelector('meta[name="description"]')
      ?.setAttribute("content", t("about.metaDescription"))
  }, [t])

  return (
    <div className="site about" id="top">
      <SkipLink href="#about-content">{t("about.skip")}</SkipLink>
      <SiteHeader page="about" />

      <main className="site-main" id="about-content">
        <section className="about-hero" aria-labelledby="about-title">
          <p className="kicker">{t("about.kicker")}</p>
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

        <div className="chapters">
          <Chapter number={1} title={t("about.places")} wide>
            <ul className="about-places">
              {places.map((place) => (
                <PlaceRow key={place.key} place={place} />
              ))}
            </ul>
          </Chapter>

          <Chapter number={2} title={t("about.credits")} wide>
            <ul className="about-credits">
              <CreditItem label={t("about.contributors")}>
                <p>{t("about.contributorsText")}</p>
                <ul
                  className="about-contributors"
                  aria-label={t("about.contributorList")}
                >
                  {contributors.map((login) => (
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
                        <span>{login}</span>
                      </ExternalLink>
                    </li>
                  ))}
                </ul>
                <ExternalLink className="text-link" href={CONTRIBUTORS_URL}>
                  {t("about.contributorsAll")}
                  <ArrowUpRight aria-hidden="true" />
                </ExternalLink>
              </CreditItem>
              <CreditItem label="LINE BLOG">
                <CreditText
                  message="about.creditsLineblog"
                  param="name"
                  name={ZHFUWE}
                  href={ZHFUWE_URL}
                />
                <ExternalLink
                  className="text-link"
                  href={LINEBLOG_RECOVERY_URL}
                >
                  {t("about.creditsRecovery")}
                  <ArrowUpRight aria-hidden="true" />
                </ExternalLink>
              </CreditItem>
              <CreditItem label={t("works.label")}>
                <CreditText
                  message="about.creditsWorks"
                  param="name"
                  name={ZHFUWE}
                  href={ZHFUWE_URL}
                />
                <a className="text-link" href="/works">
                  {t("about.creditsWorksPage")}
                  <ArrowRight aria-hidden="true" />
                </a>
              </CreditItem>
              <CreditItem label={t("about.place.mainland")}>
                <CreditText
                  message="about.creditsMainland"
                  param="provider"
                  name={MAINLAND_PROVIDER}
                  href={MAINLAND_PROVIDER_URL}
                />
              </CreditItem>
            </ul>
          </Chapter>

          <Chapter number={3} title={t("about.builtWith")} wide>
            <p>{t("about.aiCredit", { author: AUTHOR })}</p>
            <dl className="about-stack">
              {technologyGroups.map(({ key, items, mono }) => (
                <div key={key}>
                  <dt>{t(`about.group.${key}`)}</dt>
                  <dd>
                    <ul className={mono ? "is-mono" : undefined}>
                      {items.map((item) => (
                        <li key={item}>{item}</li>
                      ))}
                    </ul>
                  </dd>
                </div>
              ))}
            </dl>
          </Chapter>

          <Chapter number={4} title={t("about.notice")}>
            <SiteNotice />
          </Chapter>
        </div>
      </main>

      <SiteFooter />
    </div>
  )
}
