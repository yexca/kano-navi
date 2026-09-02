import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react"

import { createTranslator, localeOptions } from "@/i18n"

const settingsContext = createContext(null)

function readLocale() {
  if (typeof window === "undefined") return "en"
  const savedLocale = window.localStorage.getItem("kano-locale")
  return localeOptions.some((option) => option.value === savedLocale)
    ? savedLocale
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

export function AppSettingsProvider({ children }) {
  const [locale, setLocaleState] = useState(readLocale)
  const [isDark, setIsDarkState] = useState(readTheme)

  const setLocale = useCallback((nextLocale) => {
    if (localeOptions.some((option) => option.value === nextLocale)) {
      setLocaleState(nextLocale)
    }
  }, [])

  const setIsDark = useCallback((nextValue) => {
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
