import type { PointerEvent } from "react"
import { useRef, useState } from "react"
import { ArrowUpRight, ChevronDown } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { cn } from "@/lib/utils"
import { mediaVariantUrl } from "@/lib/media"
import { ExternalLink } from "@/site/links"
import { archiveMonths, mediaUrl, posts } from "./content"
import { designOptions, officialSites, type LineblogDesign } from "./designs"
import { type Translation, MenuLabel } from "./translation"

export function monthLabel(
  t: ReturnType<typeof useAppSettings>["t"],
  month: string,
) {
  return t("lineblog.month", {
    year: month.slice(0, 4),
    month: month.slice(5, 7),
  })
}

/** The months a design or official site was in use, from Japan dates. */
function designPeriod(
  t: ReturnType<typeof useAppSettings>["t"],
  since: string,
  until?: string,
) {
  const month = (date: string) => date.slice(0, 7).replace("-", "/")
  // The first design dates from before the blog's first post.
  const oldest = posts.at(-1)?.published.slice(0, 10) ?? since
  const from = month(since < oldest ? oldest : since)
  const to = until ? month(until) : ""
  // A design shown for part of one month reads as that month.
  if (to === from) return from
  return t("lineblog.designPeriod", { from, to })
}

/**
 * Picks the blog's header art and profile photo: Auto follows the articles
 * shown, as the blog did, and a period keeps that design on every view.
 */
export function DesignPicker({
  choice,
  design,
  onChoose,
}: {
  choice: string | null
  design: LineblogDesign
  onChoose: (id: string | null) => void
}) {
  const { t } = useAppSettings()
  const chosen = designOptions.find((option) => option.id === choice)
  const current = chosen
    ? designPeriod(t, chosen.id, chosen.until)
    : t("lineblog.designAuto")
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          className={cn("lb-design-button", chosen && "is-chosen")}
          aria-label={`${t("lineblog.design")}: ${current}`}
          title={t("lineblog.design")}
        >
          <img
            src={mediaVariantUrl(mediaUrl(design.profile.media), 320)}
            alt=""
            width={20}
            height={20}
            decoding="async"
          />
          <span>{current}</span>
          <ChevronDown aria-hidden="true" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        className="lb-design-menu overflow-y-auto"
      >
        <DropdownMenuLabel>{t("lineblog.design")}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuRadioGroup
          value={choice ?? "auto"}
          onValueChange={(value) => onChoose(value === "auto" ? null : value)}
        >
          <DropdownMenuRadioItem value="auto" className="lb-design-item">
            <span className="lb-design-copy">
              <strong>{t("lineblog.designAuto")}</strong>
              <small>{t("lineblog.designAutoHint")}</small>
            </span>
          </DropdownMenuRadioItem>
          {designOptions.map((option) => (
            <DropdownMenuRadioItem
              key={option.id}
              value={option.id}
              className="lb-design-item"
            >
              <img
                className="lb-design-photo"
                src={mediaVariantUrl(
                  mediaUrl(option.design.profile.media),
                  320,
                )}
                alt=""
                width={32}
                height={32}
                loading="lazy"
                decoding="async"
              />
              <span
                className="lb-design-header"
                style={{
                  backgroundImage: `url("${mediaVariantUrl(mediaUrl(option.design.header.media), 640)}")`,
                }}
                aria-hidden="true"
              />
              <span className="lb-design-copy">
                <strong>{designPeriod(t, option.id, option.until)}</strong>
              </span>
              <span
                className="lb-design-accent"
                style={{ background: option.design.accent }}
                aria-hidden="true"
              />
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/**
 * A pull-down that a touch opens only once it ends as a tap, so a swipe or a
 * page scroll that starts on its trigger leaves it shut. A mouse opens it on
 * press as usual. A tap on the open pull-down's trigger closes it, though
 * pressing it already dismissed the list.
 */
function useTapMenu() {
  const [open, setOpen] = useState(false)
  const touch = useRef<{ open: boolean } | null>(null)
  return {
    menu: { open, onOpenChange: setOpen },
    trigger: {
      onPointerDown: (event: PointerEvent<HTMLElement>) => {
        touch.current = event.pointerType === "mouse" ? null : { open }
        if (touch.current) event.preventDefault()
      },
      onClick: () => {
        if (touch.current) setOpen(!touch.current.open)
      },
    },
  }
}

/**
 * The menu bar's 鹿乃オフィシャルサイト opens the official sites the blog
 * linked over the years, each with the months it was linked.
 */
export function OfficialSiteMenu({
  translation,
}: {
  translation: Translation | null
}) {
  const { t } = useAppSettings()
  const { menu, trigger } = useTapMenu()
  return (
    <DropdownMenu modal={false} {...menu}>
      <DropdownMenuTrigger asChild {...trigger}>
        <button type="button" className="lb-site-button">
          <span lang="ja">
            <MenuLabel
              label="鹿乃オフィシャルサイト"
              item="site"
              translation={translation}
            />
          </span>
          <ChevronDown aria-hidden="true" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="start"
        collisionPadding={16}
        className="lb-site-menu"
      >
        <DropdownMenuLabel>{t("lineblog.officialSites")}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {officialSites.map((site) => (
          <DropdownMenuItem key={site.url} asChild className="lb-site-item">
            <ExternalLink href={site.url}>
              <span className="lb-design-copy">
                <strong>{site.url.replace(/^https?:\/\//u, "")}</strong>
                <small>{designPeriod(t, site.since, site.until)}</small>
              </span>
              <ArrowUpRight aria-hidden="true" />
            </ExternalLink>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** The template's monthly archive: a pull-down of every month. */
export function MonthSelect({
  current,
  onChoose,
}: {
  current: string | null
  onChoose: (month: string) => void
}) {
  const { t } = useAppSettings()
  const { menu, trigger } = useTapMenu()
  return (
    <section className="lbl-months">
      <DropdownMenu modal={false} {...menu}>
        <DropdownMenuTrigger asChild {...trigger}>
          <button
            type="button"
            className="lbl-months-button"
            aria-label={
              current
                ? `${t("lineblog.monthArchive")}: ${monthLabel(t, current)}`
                : t("lineblog.monthArchive")
            }
          >
            {current ? monthLabel(t, current) : t("lineblog.monthArchive")}
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="start"
          className="lbl-months-menu overflow-y-auto"
        >
          <DropdownMenuRadioGroup
            // The menu opens at the month shown, once it has its size.
            ref={(group) => {
              if (!group) return
              requestAnimationFrame(() => {
                const menu = group.closest<HTMLElement>(".lbl-months-menu")
                const item = group.querySelector<HTMLElement>(
                  '[data-state="checked"]',
                )
                if (menu && item)
                  menu.scrollTop =
                    item.offsetTop - (menu.clientHeight - item.offsetHeight) / 2
              })
            }}
            value={current ?? ""}
            onValueChange={(month) => {
              if (month !== current) onChoose(month)
            }}
          >
            {archiveMonths.map(({ year, months }) => (
              <div key={year} role="group" aria-label={year}>
                <DropdownMenuLabel className="lbl-months-year">
                  {year}
                </DropdownMenuLabel>
                {months.map(({ month, count }) => (
                  <DropdownMenuRadioItem
                    key={month}
                    value={month}
                    className="lbl-months-item"
                  >
                    <span>{monthLabel(t, month)}</span>
                    <small aria-hidden="true">{count}</small>
                    <span className="sr-only">
                      {t("lineblog.monthCount", { count })}
                    </span>
                  </DropdownMenuRadioItem>
                ))}
              </div>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </section>
  )
}
