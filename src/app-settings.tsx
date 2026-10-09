import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react"

import { createTranslator, localeOptions, type PageMessages } from "@/i18n"
import type { Dispatch, SetStateAction, ReactNode } from "react"
import type { Locale, Translator } from "@/types"

interface AppSettings {
  locale: Locale
  isDark: boolean
  setLocale: (locale: Locale) => void
  setIsDark: Dispatch<SetStateAction<boolean>>
  t: Translator
}

const settingsContext = createContext<AppSettings | null>(null)

function readLocale(): Locale {
  if (typeof window === "undefined") return "en"
  const savedLocale = window.localStorage.getItem("kano-locale")
  return localeOptions.some((option) => option.value === savedLocale)
    ? (savedLocale as Locale)
    : "en"
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
  const [isDark, setIsDarkState] = useState(readTheme)

  const setLocale = useCallback((nextLocale: Locale) => {
    if (localeOptions.some((option) => option.value === nextLocale)) {
      setLocaleState(nextLocale)
    }
  }, [])

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
    window.localStorage.setItem("kano-locale", locale)
  }, [locale])

  const value = useMemo(
    () => ({ isDark, locale, setIsDark, setLocale, t }),
    [isDark, locale, setIsDark, setLocale, t],
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
