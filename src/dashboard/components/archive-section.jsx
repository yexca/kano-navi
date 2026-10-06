import {
  ArrowRight,
  ArrowUpRight,
  Globe,
  Hash,
  House,
  ListMusic,
  Radio,
  Sparkles,
  Tv,
} from "lucide-react"

import { archiveLinks, hashtags } from "../content"
import { ExternalLink, SectionHeading, XLogo, YouTubeLogo } from "./primitives"

function resourceIcon(url = "") {
  if (/\/\/(?:www\.)?(?:x|twitter)\.com\//u.test(url)) return XLogo
  if (/youtube\.com/u.test(url)) return YouTubeLogo
  if (/bilibili\.com/u.test(url)) return Tv
  if (/twitcasting\.tv/u.test(url)) return Radio
  if (/nicovideo\.jp/u.test(url)) return ListMusic
  if (/milpr\.com/u.test(url)) return Sparkles
  if (/amebaownd\.com/u.test(url)) return House
  return Globe
}

const toneAliases = {
  coral: "berry",
  blue: "sky",
  periwinkle: "lavender",
  mint: "leaf",
  yellow: "honey",
  butter: "honey",
  ink: "ink",
}

function ResourceCard({ resource }) {
  const Icon = resourceIcon(resource.url)
  const tone = toneAliases[resource.tone] || "ink"
  return (
    <ExternalLink className={`resource-card tone-${tone}`} href={resource.url}>
      <span className="resource-icon" aria-hidden="true">
        <Icon />
      </span>
      <span className="resource-copy">
        <strong>{resource.title}</strong>
        {resource.detail ? <small>{resource.detail}</small> : null}
      </span>
      <ArrowUpRight className="resource-arrow" aria-hidden="true" />
    </ExternalLink>
  )
}

export function ArchiveSection({ timeline, resources, t }) {
  return (
    <section
      className="archive-section"
      id="archive"
      aria-labelledby="archive-title"
    >
      <div className="panel timeline-panel">
        <SectionHeading
          id="archive-title"
          eyebrow={t("archive.timeline")}
          title={t("archive.title")}
          subtitle={t("archive.subtitle")}
        >
          <a className="soft-button" href="/history">
            {t("archive.fullHistory")}
            <ArrowRight aria-hidden="true" />
          </a>
          <ExternalLink className="soft-button" href={archiveLinks.wikipedia}>
            {t("archive.wiki")}
            <ArrowUpRight aria-hidden="true" />
          </ExternalLink>
        </SectionHeading>
        <ol className="timeline">
          {timeline.map((item, index) => (
            <li
              key={item.id}
              className={index === 0 ? "is-current" : undefined}
            >
              <span className="timeline-year">{item.year}</span>
              <span className="timeline-copy">
                <strong>{item.title}</strong>
                <span>{item.detail}</span>
              </span>
            </li>
          ))}
        </ol>
        <ExternalLink className="listen-callout" href={archiveLinks.mylist}>
          <span className="listen-callout-icon" aria-hidden="true">
            <ListMusic />
          </span>
          <span>
            <strong>{t("archive.listenFromStart")}</strong>
            <small>{t("archive.mylist")}</small>
          </span>
          <ArrowUpRight aria-hidden="true" />
        </ExternalLink>
      </div>

      <div className="panel directory-panel">
        <SectionHeading title={t("directory.title")} />
        <div className="resource-grid">
          {resources.map((resource) => (
            <ResourceCard key={resource.id} resource={resource} />
          ))}
        </div>
        <div className="tag-row">
          <span className="tag-row-label">
            <Hash aria-hidden="true" />
            {t("directory.tags")}
          </span>
          {hashtags.map(({ tag, url }) => (
            <ExternalLink key={tag} className="tag" href={url}>
              #{tag}
            </ExternalLink>
          ))}
        </div>
      </div>
    </section>
  )
}
