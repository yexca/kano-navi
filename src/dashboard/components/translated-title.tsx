import type { Locale, Translator, Video } from "@/types"

import {
  pickTranslation,
  TranslationMark,
  TranslationNote,
  useTranslatedText,
} from "@/components/translated-text"
import { cleanVideoTitle } from "../format"

/**
 * A video title inside a link (a ticket, a row): the reader's translation,
 * marked as one. A button cannot go inside a link, so the source wording sits
 * in the mark's tooltip. `title` is the source title as the card shows it.
 */
export function TranslatedTitle({
  className,
  title,
  video,
  locale,
  t,
}: {
  className: string
  title: string
  video?: Video | null
  locale: Locale
  t: Translator
}) {
  const translation = pickTranslation(video?.translations, locale)
  if (!translation) return <span className={className}>{title}</span>
  return (
    <span className={className}>
      <span lang={locale}>{cleanVideoTitle(translation.text)}</span>
      <TranslationMark translation={translation} original={title} t={t} />
    </span>
  )
}

/**
 * A video title outside any link: the reader's translation as a heading,
 * followed by its origin and a Show original button.
 */
export function TranslatedHeading({
  title,
  video,
  locale,
  t,
}: {
  title: string
  video: Video
  locale: Locale
  t: Translator
}) {
  const state = useTranslatedText(
    title,
    video.translations,
    video.sourceLanguage,
    locale,
  )
  return (
    <>
      <h3 lang={state.lang}>{cleanVideoTitle(state.text)}</h3>
      <TranslationNote state={state} t={t} />
    </>
  )
}
