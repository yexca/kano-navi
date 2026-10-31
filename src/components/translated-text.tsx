import { useState } from "react"
import { Languages } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { cn } from "@/lib/utils"
import type { Locale, Translation, Translations, Translator } from "@/types"

/** The lang attribute of wording in a detected source language. */
const sourceLangs: Record<string, string> = {
  ja: "ja",
  en: "en",
  zh: "zh",
  ko: "ko",
}

const platforms = new Set(["youtube", "bilibili"])

/**
 * The translation a reader of the content locale sees instead of the source
 * wording, or null: Japanese content and untranslated wording keep the source.
 */
export function pickTranslation(
  translations: Translations | undefined | null,
  locale: Locale,
): Translation | null {
  if (locale === "ja") return null
  const translation = translations?.[locale]
  return translation?.text ? translation : null
}

/** Names where a translation came from: AI, a platform or the editor. */
export function translationOriginLabel(
  translation: Translation,
  t: Translator,
): string {
  if (translation.origin === "machine") return t("translation.machine")
  if (translation.origin === "manual") return t("translation.manual")
  const platform = platforms.has(translation.platform || "")
    ? translation.platform
    : "other"
  return t(`translation.platform.${platform}`)
}

export interface TranslatedTextState {
  /** The wording to show. */
  text: string
  /** The translation shown, or null while the source shows. */
  shown: Translation | null
  /** The translation this reader can switch to, shown or not. */
  available: Translation | null
  /** The lang attribute of the wording shown. */
  lang: string | undefined
  toggle: () => void
}

/**
 * Shows the translation in the reader's content language by default and lets
 * them switch back.
 */
export function useTranslatedText(
  original: string,
  translations: Translations | undefined | null,
  sourceLanguage: string | undefined | null,
): TranslatedTextState {
  const { contentLocale } = useAppSettings()
  const [showOriginal, setShowOriginal] = useState(false)
  const available = pickTranslation(translations, contentLocale)
  const shown = available && !showOriginal ? available : null
  return {
    text: shown ? shown.text : original,
    shown,
    available,
    lang: shown ? contentLocale : sourceLangs[sourceLanguage || ""],
    toggle: () => setShowOriginal((value) => !value),
  }
}

/**
 * The switch after translated wording: a translate icon and "AI", lit while
 * the translation shows. Selecting it shows the source wording and turns it
 * off; selecting it again brings the translation back. Its tooltip and
 * accessible name spell out where the translation came from (AI, a platform
 * or the editor), and a title's tooltip previews the source wording.
 */
export function TranslationToggle({
  state,
  original,
  t,
  className,
}: {
  state: TranslatedTextState
  /** Short source wording, such as a title, to preview in the tooltip. */
  original?: string
  t: Translator
  className?: string
}) {
  const { available, shown } = state
  if (!available) return null
  const origin = translationOriginLabel(available, t)
  const hint = [
    origin,
    available.origin === "machine" ? t("translation.machineHint") : null,
    shown && original
      ? t("translation.originalIs", { text: original })
      : t(shown ? "translation.showOriginal" : "translation.showTranslation"),
  ]
    .filter(Boolean)
    .join(" · ")
  return (
    <button
      type="button"
      className={cn("translation-mark", shown && "is-active", className)}
      title={hint}
      aria-label={origin}
      aria-pressed={Boolean(shown)}
      onClick={state.toggle}
    >
      <Languages aria-hidden="true" />
      {t("translation.machineShort")}
    </button>
  )
}
