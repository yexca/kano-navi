import { useRef } from "react"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Button } from "@/components/ui/button"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { Languages, Moon, Sun } from "lucide-react"
import { localeOptions } from "@/i18n"
import { useAppSettings, type ContentLanguage } from "@/app-settings"
import type { Locale } from "@/types"

/**
 * The header's language menu and theme switch. The menu sets the interface
 * language and, unless `content` is off (pages without synced content), the
 * content language of titles, posts and blog translations.
 */
export function PreferenceControls({
  className = "",
  content = true,
}: {
  className?: string
  content?: boolean
}) {
  const {
    contentLanguage,
    isDark,
    locale,
    setContentLanguage,
    setIsDark,
    setLocale,
    t,
  } = useAppSettings()
  const classes = ["preference-controls", className].filter(Boolean).join(" ")
  const languageButton = useRef<HTMLButtonElement>(null)

  return (
    <TooltipProvider delayDuration={250}>
      <div className={classes}>
        <DropdownMenu>
          <Tooltip>
            <TooltipTrigger asChild>
              <DropdownMenuTrigger asChild>
                <Button
                  ref={languageButton}
                  variant="ghost"
                  size="icon"
                  className="icon-button language-button"
                  aria-label={t("header.language")}
                >
                  <Languages className="icon" aria-hidden="true" />
                </Button>
              </DropdownMenuTrigger>
            </TooltipTrigger>
            <TooltipContent>{t("header.language")}</TooltipContent>
          </Tooltip>
          <DropdownMenuContent
            align="end"
            className="language-menu-content"
            // Focus returns to the button without scrolling: browsers scroll
            // a focused element in the sticky header back into view, which
            // would move the page after every language change.
            onCloseAutoFocus={(event) => {
              event.preventDefault()
              languageButton.current?.focus({ preventScroll: true })
            }}
          >
            <DropdownMenuLabel>
              {t("header.interfaceLanguage")}
            </DropdownMenuLabel>
            <DropdownMenuRadioGroup
              value={locale}
              onValueChange={(value) => setLocale(value as Locale)}
            >
              {localeOptions.map((option) => (
                <DropdownMenuRadioItem
                  value={option.value}
                  key={option.value}
                  className="language-menu-item"
                >
                  <span lang={option.htmlLang}>{option.label}</span>
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
            {content ? (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuLabel>
                  {t("header.contentLanguage")}
                </DropdownMenuLabel>
                <DropdownMenuRadioGroup
                  value={contentLanguage}
                  onValueChange={(value) =>
                    setContentLanguage(value as ContentLanguage)
                  }
                >
                  <DropdownMenuRadioItem
                    value="interface"
                    className="language-menu-item"
                  >
                    <span>{t("header.contentSameAsInterface")}</span>
                  </DropdownMenuRadioItem>
                  {localeOptions.map((option) => (
                    <DropdownMenuRadioItem
                      value={option.value}
                      key={option.value}
                      className="language-menu-item"
                    >
                      <span lang={option.htmlLang}>{option.label}</span>
                    </DropdownMenuRadioItem>
                  ))}
                </DropdownMenuRadioGroup>
              </>
            ) : null}
          </DropdownMenuContent>
        </DropdownMenu>

        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="icon-button theme-button"
              onClick={() => setIsDark((value) => !value)}
              aria-label={t(
                isDark ? "header.switchToLight" : "header.switchToDark",
              )}
            >
              {isDark ? (
                <Sun className="icon" aria-hidden="true" />
              ) : (
                <Moon className="icon" aria-hidden="true" />
              )}
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            {t(isDark ? "header.switchToLight" : "header.switchToDark")}
          </TooltipContent>
        </Tooltip>
      </div>
    </TooltipProvider>
  )
}
