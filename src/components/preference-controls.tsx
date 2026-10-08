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
import { useAppSettings } from "@/app-settings"

export function PreferenceControls({ className = "" }: { className?: string }) {
  const { isDark, locale, setIsDark, setLocale, t } = useAppSettings()
  const classes = ["preference-controls", className].filter(Boolean).join(" ")

  return (
    <TooltipProvider delayDuration={250}>
      <div className={classes}>
        <DropdownMenu>
          <Tooltip>
            <TooltipTrigger asChild>
              <DropdownMenuTrigger asChild>
                <Button
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
          <DropdownMenuContent align="end" className="language-menu-content">
            <DropdownMenuLabel>{t("header.language")}</DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuRadioGroup value={locale} onValueChange={setLocale}>
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
