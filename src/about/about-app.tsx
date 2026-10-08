import { useEffect } from "react"
import { ArrowLeft, ArrowUpRight, CodeXml, Scale } from "lucide-react"

import { version } from "../../package.json"
import { useAppSettings } from "@/app-settings"
import { PreferenceControls } from "@/components/preference-controls"
import { ExternalLink } from "@/dashboard/components/primitives"
import { fallbackProfile } from "@/dashboard/content"
import "@/dashboard/dashboard.css"
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
  {
    key: "backend",
    items: ["Node.js", "Express", "SQLite", "better-sqlite3"],
  },
  {
    key: "delivery",
    items: ["Docker", "Docker Compose", "GitHub Actions"],
  },
]

function AboutSection({
  number,
  title,
  children,
}: {
  number: any
  title?: React.ReactNode
  children?: React.ReactNode
}) {
  const id = `about-section-${number}`
  return (
    <section className="about-section" aria-labelledby={id}>
      <h2 id={id}>
        <span className="about-section-number" aria-hidden="true">
          {String(number).padStart(2, "0")}
        </span>
        {title}
      </h2>
      <div className="about-section-body">{children}</div>
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
    <div className="board about-page">
      <a className="skip-link" href="#about-content">
        {t("about.skip")}
      </a>
      <header className="site-header is-scrolled">
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
            <a
              className="soft-button about-back"
              href="/"
              aria-label={t("history.back")}
            >
              <ArrowLeft aria-hidden="true" />
              <span>{t("history.back")}</span>
            </a>
            <PreferenceControls className="header-preferences" />
          </div>
        </div>
      </header>

      <main className="board-main about-main" id="about-content">
        <section className="panel about-identity" aria-labelledby="about-title">
          <div className="about-cover" aria-hidden="true">
            <img
              src={fallbackProfile.avatarUrl}
              alt=""
              width="160"
              height="160"
            />
            <span>🍓🦌</span>
          </div>
          <div className="about-identity-copy">
            <p className="eyebrow">{t("about.label")}</p>
            <h1 id="about-title">Kano Navi</h1>
            <p className="about-tagline">{t("about.tagline")}</p>
            <div className="about-edition">
              <span>v{version}</span>
              <span>{t("about.by", { author: AUTHOR })}</span>
            </div>
          </div>
          <nav
            className="about-project-links"
            aria-label={t("about.projectLinks")}
          >
            <ExternalLink href={REPOSITORY_URL}>
              <CodeXml aria-hidden="true" />
              <span>{t("about.sourceCode")}</span>
              <ArrowUpRight aria-hidden="true" />
            </ExternalLink>
            <ExternalLink href={LICENSE_URL}>
              <Scale aria-hidden="true" />
              <span>{t("about.license")}</span>
              <small>AGPL-3.0</small>
              <ArrowUpRight aria-hidden="true" />
            </ExternalLink>
          </nav>
        </section>

        <div className="about-notes">
          <p className="about-lead">{t("about.intro")}</p>
          <AboutSection number={1} title={t("about.overview")}>
            <p>{t("about.overviewOne")}</p>
            <p>{t("about.overviewTwo")}</p>
            <a className="soft-button" href="/history">
              {t("archive.fullHistory")}
              <ArrowUpRight aria-hidden="true" />
            </a>
          </AboutSection>
          <AboutSection number={2} title={t("about.builtWithAi")}>
            <p>{t("about.aiCredit", { author: AUTHOR })}</p>
            <ul className="about-models" aria-label={t("about.models")}>
              {developmentModels.map((model) => (
                <li key={model}>{model}</li>
              ))}
            </ul>
          </AboutSection>
          <AboutSection number={3} title={t("about.technologies")}>
            <dl className="about-technologies">
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
          </AboutSection>
          <AboutSection number={4} title={t("about.notice")}>
            <p>{t("about.unofficial")}</p>
            <p>{t("about.accuracy")}</p>
            <p>{t("about.rights")}</p>
            <p>
              {t("about.licenseText")}{" "}
              <ExternalLink className="about-text-link" href={LICENSE_URL}>
                {t("about.fullLicense")}
                <ArrowUpRight aria-hidden="true" />
              </ExternalLink>
            </p>
          </AboutSection>
        </div>
      </main>

      <footer className="site-footer">
        <div className="site-footer-brand">
          <span aria-hidden="true">🍓🦌</span>
          <strong>{t("footer.fanMade")}</strong>
        </div>
        <p>{t("footer.disclaimer")}</p>
        <a href="/" className="soft-button">
          <ArrowLeft aria-hidden="true" />
          {t("history.back")}
        </a>
      </footer>
    </div>
  )
}
