import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react"

import { createTranslator, localeOptions, type PageMessages } from "@/i18n"
import { syncFeedLinks } from "@/lib/feeds"
import type { Dispatch, SetStateAction, ReactNode } from "react"
import type { Locale, Translator } from "@/types"

/** The language of synced titles and posts: a locale, or the interface's. */
export type ContentLanguage = Locale | "interface"

interface AppSettings {
  locale: Locale
  /** The content language the reader picked. */
  contentLanguage: ContentLanguage
  /** The locale whose translations of titles and posts show. */
  contentLocale: Locale
  isDark: boolean
  setLocale: (locale: Locale) => void
  setContentLanguage: (language: ContentLanguage) => void
  setIsDark: Dispatch<SetStateAction<boolean>>
  t: Translator
}

const settingsContext = createContext<AppSettings | null>(null)

function isLocale(value: unknown): value is Locale {
  return localeOptions.some((option) => option.value === value)
}

function readLocale(): Locale {
  if (typeof window === "undefined") return "en"
  const savedLocale = window.localStorage.getItem("kano-locale")
  return isLocale(savedLocale) ? savedLocale : "en"
}

function readContentLanguage(): ContentLanguage {
  if (typeof window === "undefined") return "interface"
  const saved = window.localStorage.getItem("kano-content-locale")
  return isLocale(saved) ? saved : "interface"
}

function readTheme() {
  if (typeof window === "undefined") return false
  const savedTheme = window.localStorage.getItem("kano-theme")
  if (savedTheme === "dark" || savedTheme === "light") {
    return savedTheme === "dark"
  }
  return window.matchMedia?.("(prefers-color-scheme: dark)")?.matches ?? false
}

export function AppSettingsProvider({ children }: { children?: ReactNode }) {
  const [locale, setLocaleState] = useState(readLocale)
  const [contentLanguage, setContentLanguageState] =
    useState(readContentLanguage)
  const [isDark, setIsDarkState] = useState(readTheme)

  const setLocale = useCallback((nextLocale: Locale) => {
    if (isLocale(nextLocale)) setLocaleState(nextLocale)
  }, [])

  const setContentLanguage = useCallback((next: ContentLanguage) => {
    if (next === "interface" || isLocale(next)) setContentLanguageState(next)
  }, [])

  const contentLocale =
    contentLanguage === "interface" ? locale : contentLanguage

  const setIsDark = useCallback((nextValue: SetStateAction<boolean>) => {
    setIsDarkState((current) =>
      typeof nextValue === "function" ? nextValue(current) : Boolean(nextValue),
    )
  }, [])

  const t = useMemo(() => createTranslator(locale), [locale])

  useEffect(() => {
    document.documentElement.classList.toggle("dark", isDark)
    window.localStorage.setItem("kano-theme", isDark ? "dark" : "light")
  }, [isDark])

  useEffect(() => {
    document.documentElement.lang = locale
    syncFeedLinks(locale)
    window.localStorage.setItem("kano-locale", locale)
  }, [locale])

  useEffect(() => {
    window.localStorage.setItem("kano-content-locale", contentLanguage)
  }, [contentLanguage])

  const value = useMemo(
    () => ({
      contentLanguage,
      contentLocale,
      isDark,
      locale,
      setContentLanguage,
      setIsDark,
      setLocale,
      t,
    }),
    [
      contentLanguage,
      contentLocale,
      isDark,
      locale,
      setContentLanguage,
      setIsDark,
      setLocale,
      t,
    ],
  )

  return (
    <settingsContext.Provider value={value}>
      {children}
    </settingsContext.Provider>
  )
}

export function useAppSettings() {
  const settings = useContext(settingsContext)
  if (!settings) {
    throw new Error("useAppSettings must be used inside AppSettingsProvider")
  }
  return settings
}

/** Page copy travels with its lazy chunk; preferences remain in one provider. */
export function PageMessagesProvider({
  messages,
  children,
}: {
  messages: PageMessages
  children: ReactNode
}) {
  const settings = useAppSettings()
  const t = useMemo(
    () => createTranslator(settings.locale, messages),
    [settings.locale, messages],
  )
  return (
    <settingsContext.Provider value={{ ...settings, t }}>
      {children}
    </settingsContext.Provider>
  )
}
