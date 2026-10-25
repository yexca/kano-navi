import type { ReactNode } from "react"
import { useEffect } from "react"
import { ArrowRight, ArrowUpRight, CodeXml, Scale } from "lucide-react"

import { version } from "../../package.json"
import { useAppSettings } from "@/app-settings"
import { ExternalLink } from "@/site/links"
import { SiteFooter, SiteHeader, SkipLink } from "@/site/site-shell"
import "./about.css"

const AUTHOR = "yexca"
const REPOSITORY_URL = "https://github.com/yexca/kano-navi"
const LICENSE_URL = `${REPOSITORY_URL}/blob/main/LICENSE`
const developmentModels = ["GPT-5.6-Sol", "Claude Opus 5.5", "GPT-6.1-Sol"]
const technologyGroups = [
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
  { key: "backend", items: ["Go", "net/http", "SQLite", "modernc.org/sqlite"] },
  { key: "delivery", items: ["Docker", "Docker Compose", "GitHub Actions"] },
]

function Chapter({
  number,
  title,
  children,
}: {
  number: number
  title: ReactNode
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
      <div className="chapter-body">{children}</div>
    </section>
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
          <p className="kicker">{t("about.label")}</p>
          <h1 id="about-title">
            {t("site.name")}
            <span aria-hidden="true">🍓🦌</span>
          </h1>
          <p className="about-intro">{t("about.intro")}</p>
          <p className="about-edition">
            <span className="tag">Kano Navi v{version}</span>
            <span className="tag">{t("about.tagline")}</span>
            <span className="tag">{t("about.by", { author: AUTHOR })}</span>
          </p>
          <nav className="about-links" aria-label={t("about.projectLinks")}>
            <ExternalLink
              className="pill-button is-solid"
              href={REPOSITORY_URL}
            >
              <CodeXml aria-hidden="true" />
              {t("about.sourceCode")}
              <ArrowUpRight aria-hidden="true" />
            </ExternalLink>
            <ExternalLink className="pill-button" href={LICENSE_URL}>
              <Scale aria-hidden="true" />
              {t("about.license")} · AGPL-3.0
              <ArrowUpRight aria-hidden="true" />
            </ExternalLink>
          </nav>
        </section>

        <div className="chapters">
          <Chapter number={1} title={t("about.overview")}>
            <p>{t("about.overviewOne")}</p>
            <p>{t("about.overviewTwo")}</p>
            <a className="text-link" href="/history">
              {t("archive.fullHistory")}
              <ArrowRight aria-hidden="true" />
            </a>
          </Chapter>
          <Chapter number={2} title={t("about.builtWithAi")}>
            <p>{t("about.aiCredit", { author: AUTHOR })}</p>
            <ul className="about-models" aria-label={t("about.models")}>
              {developmentModels.map((model) => (
                <li key={model}>{model}</li>
              ))}
            </ul>
          </Chapter>
          <Chapter number={3} title={t("about.technologies")}>
            <dl className="about-stack">
              {technologyGroups.map(({ key, items }) => (
                <div key={key}>
                  <dt>{t(`about.group.${key}`)}</dt>
                  <dd>
                    <ul>
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
            <p>{t("about.unofficial")}</p>
            <p>{t("about.accuracy")}</p>
            <p>{t("about.rights")}</p>
            <p>
              {t("about.licenseText")}{" "}
              <ExternalLink className="text-link" href={LICENSE_URL}>
                {t("about.fullLicense")}
                <ArrowUpRight aria-hidden="true" />
              </ExternalLink>
            </p>
          </Chapter>
        </div>
      </main>

      <SiteFooter disclaimer={t("footer.disclaimer")} />
    </div>
  )
}
