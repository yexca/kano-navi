import type { Dashboard, Locale, Translator } from "@/types"
import {
  ArrowRight,
  ArrowUpRight,
  Globe,
  Hash,
  House,
  ListMusic,
  Radio,
  Sparkles,
} from "lucide-react"

import { SectionTitle } from "@/site/site-shell"
import { BilibiliLogo, ExternalLink, XLogo, YouTubeLogo } from "@/site/links"
import { archiveLinks, hashtags } from "../content"
import { localizeSeedText } from "../seed-copy"

function resourceIcon(url = "") {
  if (/\/\/(?:www\.)?(?:x|twitter)\.com\//u.test(url)) return XLogo
  if (/youtube\.com/u.test(url)) return YouTubeLogo
  if (/bilibili\.com/u.test(url)) return BilibiliLogo
  if (/twitcasting\.tv/u.test(url)) return Radio
  if (/nicovideo\.jp/u.test(url)) return ListMusic
  if (/milpr\.com/u.test(url)) return Sparkles
  if (/amebaownd\.com/u.test(url)) return House
  return Globe
}

function hostOf(url: string) {
  try {
    return new URL(url).hostname.replace(/^www\./u, "")
  } catch {
    return ""
  }
}

export function LinkDirectory({
  timeline,
  resources,
  locale,
  t,
}: {
  timeline: Dashboard["timeline"]
  resources: Dashboard["resources"]
  locale: Locale
  t: Translator
}) {
  return (
    <section className="section links" id="links" aria-labelledby="links-title">
      <SectionTitle
        id="links-title"
        kicker={t("directory.kicker")}
        title={t("directory.title")}
      />
      <div className="links-layout">
        <ul className="link-list">
          {resources.map((resource) => {
            const Icon = resourceIcon(resource.url)
            return (
              <li key={resource.id}>
                <ExternalLink className="link-row" href={resource.url}>
                  <span className="link-row-icon" aria-hidden="true">
                    <Icon />
                  </span>
                  <span className="link-row-copy">
                    <strong>{localizeSeedText(resource.title, locale)}</strong>
                    <small>
                      {localizeSeedText(resource.detail, locale) ||
                        hostOf(resource.url)}
                    </small>
                  </span>
                  <ArrowUpRight className="link-row-arrow" aria-hidden="true" />
                </ExternalLink>
              </li>
            )
          })}
        </ul>

        <aside className="journey-card" aria-labelledby="journey-title">
          <p className="kicker">{t("archive.timeline")}</p>
          <h3 id="journey-title">{t("archive.subtitle")}</h3>
          <ol className="journey-mini">
            {timeline.map((item) => (
              <li key={item.id}>
                <span className="journey-mini-year">{item.year}</span>
                <span className="journey-mini-copy">
                  <strong>{localizeSeedText(item.title, locale)}</strong>
                  {item.detail ? (
                    <small>{localizeSeedText(item.detail, locale)}</small>
                  ) : null}
                </span>
              </li>
            ))}
          </ol>
          <div className="journey-card-actions">
            <a className="pill-button is-solid" href="/history">
              {t("archive.fullHistory")}
              <ArrowRight aria-hidden="true" />
            </a>
            <ExternalLink className="text-link" href={archiveLinks.wikipedia}>
              {t("archive.wiki")}
              <ArrowUpRight aria-hidden="true" />
            </ExternalLink>
            <ExternalLink className="text-link" href={archiveLinks.mylist}>
              {t("archive.listenFromStart")}
              <ArrowUpRight aria-hidden="true" />
            </ExternalLink>
          </div>
        </aside>
      </div>

      <div className="hashtags">
        <span className="hashtags-label">
          <Hash aria-hidden="true" />
          {t("directory.tags")}
        </span>
        {hashtags.map(({ tag, url }) => (
          <ExternalLink key={tag} className="hashtag" href={url}>
            #{tag}
          </ExternalLink>
        ))}
      </div>
    </section>
  )
}
