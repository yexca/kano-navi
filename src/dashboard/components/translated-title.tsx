import type { Translations, Translator, Video } from "@/types"

import {
  TranslationToggle,
  useTranslatedText,
} from "@/components/translated-text"
import { ExternalLink } from "@/site/links"
import { cn } from "@/lib/utils"
import { cleanVideoTitle } from "../format"

/**
 * A video or schedule title on a card (a ticket, a row): the reader's
 * translation, followed by the translation switch. The title is the
 * card's link (see `followCardLink`), because the switch cannot go inside
 * one. `title` is the source title as the card shows it; `translations` are
 * its video's or event's.
 */
export function TranslatedTitle({
  className,
  title,
  translations,
  href,
  t,
}: {
  className: string
  title: string
  translations?: Translations | null
  href?: string | null
  t: Translator
}) {
  const state = useTranslatedText(title, translations, null)
  const text = state.shown ? cleanVideoTitle(state.text) : title
  const wording = href ? (
    <ExternalLink
      className="card-link translated-title-text"
      href={href}
      lang={state.lang}
    >
      {text}
    </ExternalLink>
  ) : (
    <span className="translated-title-text" lang={state.lang}>
      {text}
    </span>
  )
  return (
    <span className={cn(className, state.available && "translated-title")}>
      {wording}
      <TranslationToggle state={state} original={title} t={t} />
    </span>
  )
}

/**
 * A video title outside any link: the reader's translation as a heading,
 * with the same switch after its last word.
 */
export function TranslatedHeading({
  title,
  video,
  t,
}: {
  title: string
  video: Video
  t: Translator
}) {
  const state = useTranslatedText(
    title,
    video.translations,
    video.sourceLanguage,
  )
  return (
    <h3 className="translated-heading">
      <span lang={state.lang}>{cleanVideoTitle(state.text)}</span>
      <TranslationToggle state={state} original={title} t={t} />
    </h3>
  )
}
