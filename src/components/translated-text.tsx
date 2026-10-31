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

/** The origin label and the show-original button under translated wording. */
export function TranslationNote({
  state,
  t,
  className,
}: {
  state: TranslatedTextState
  t: Translator
  className?: string
}) {
  const { available, shown } = state
  if (!available) return null
  return (
    <p className={cn("translation-note", className)}>
      <Languages aria-hidden="true" />
      <span
        title={
          shown?.origin === "machine" ? t("translation.machineHint") : undefined
        }
      >
        {shown
          ? translationOriginLabel(shown, t)
          : t("translation.originalShown")}
      </span>
      <button type="button" onClick={state.toggle} aria-pressed={!shown}>
        {shown
          ? t("translation.showOriginal")
          : t("translation.showTranslation")}
      </button>
    </p>
  )
}

/** Block wording, such as a post's text, followed by its translation note. */
export function TranslatedText({
  original,
  translations,
  sourceLanguage,
  t,
  className,
}: {
  original: string
  translations?: Translations | null
  sourceLanguage?: string | null
  t: Translator
  className?: string
}) {
  const state = useTranslatedText(original, translations, sourceLanguage)
  return (
    <>
      <p className={className} lang={state.lang}>
        {state.text}
      </p>
      <TranslationNote state={state} t={t} />
    </>
  )
}

/**
 * A compact switch after a translated title: it names the translation (AI,
 * or Translated for an editor's or a platform's) and swaps in the source
 * wording. Its tooltip previews the source wording; screen readers hear the
 * full origin.
 */
export function TranslationToggle({
  state,
  original,
  t,
}: {
  state: TranslatedTextState
  original: string
  t: Translator
}) {
  const { available, shown } = state
  if (!available) return null
  const label = shown
    ? t(
        shown.origin === "machine"
          ? "translation.machineShort"
          : "translation.translated",
      )
    : t("translation.original")
  const spoken = shown ? translationOriginLabel(shown, t) : label
  const action = shown
    ? t("translation.showOriginal")
    : t("translation.showTranslation")
  const hint = shown
    ? `${spoken} · ${t("translation.originalIs", { text: original })}`
    : action
  return (
    <button
      type="button"
      className={cn("translation-mark", !shown && "is-original")}
      title={hint}
      aria-label={`${spoken}: ${action}`}
      onClick={state.toggle}
    >
      <Languages aria-hidden="true" />
      {label}
    </button>
  )
}
