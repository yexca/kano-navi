import type { Translator } from "@/types"
import { ArrowUpRight } from "lucide-react"

import { BilibiliLogo, ExternalLink, YouTubeLogo } from "@/site/links"
import type { VideoLink } from "../video-groups"

const platformNames: Record<string, string> = {
  youtube: "YouTube",
  bilibili: "bilibili",
}

function PlatformMark({ source }: { source: string }) {
  if (source === "youtube") return <YouTubeLogo aria-hidden="true" />
  if (source === "bilibili") return <BilibiliLogo aria-hidden="true" />
  return <ArrowUpRight aria-hidden="true" />
}

/**
 * One link per platform, in `videoLinks` order: the card's own link first,
 * then the other uploads of the same work, such as its bilibili copy.
 */
export function PlatformLinks({
  links,
  title,
  variant,
  t,
}: {
  links: VideoLink[]
  title: string
  variant: "pill" | "chip"
  t: Translator
}) {
  return (
    <span className="platform-links">
      {links.map((link, index) => {
        const name = platformNames[link.source] || link.source
        const className =
          variant === "pill"
            ? `pill-button${index === 0 ? " is-solid" : ""}`
            : "platform-chip"
        return (
          <ExternalLink
            key={link.url}
            className={className}
            href={link.url}
            aria-label={`${t("videos.watchOn", { platform: name })}: ${title}`}
          >
            <PlatformMark source={link.source} />
            {name}
          </ExternalLink>
        )
      })}
    </span>
  )
}

/**
 * A cover's mark for a work on more than one platform. It only signals; the
 * links follow the title, so a tap on the cover still opens the card's link.
 */
export function PlatformBadge({ links }: { links: VideoLink[] }) {
  if (links.length < 2) return null
  return (
    <span className="platform-badge" aria-hidden="true">
      {links.map((link) => (
        <PlatformMark key={link.url} source={link.source} />
      ))}
    </span>
  )
}
