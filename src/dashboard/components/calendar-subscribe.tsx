import type { Translator } from "@/types"
import { useState } from "react"
import { CalendarPlus, Check, Copy } from "lucide-react"

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { calendarFeedUrls } from "@/lib/feeds"

type CopyState = "idle" | "copied" | "failed"

/** Offers the schedule as a calendar subscription. */
export function CalendarSubscribe({ t }: { t: Translator }) {
  const [copy, setCopy] = useState<CopyState>("idle")
  const urls = calendarFeedUrls(window.location.origin)
  const copyLink = async (event: Event) => {
    // Keep the menu open so the result is visible.
    event.preventDefault()
    try {
      await navigator.clipboard.writeText(urls.https)
      setCopy("copied")
    } catch {
      // A browser that refuses the clipboard shows the address instead.
      setCopy("failed")
    }
  }
  return (
    <DropdownMenu onOpenChange={(open) => open || setCopy("idle")}>
      <DropdownMenuTrigger asChild>
        <button type="button" className="pill-button">
          <CalendarPlus aria-hidden="true" />
          {t("schedule.subscribe")}
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="subscribe-menu">
        <DropdownMenuLabel>{t("schedule.subscribeNote")}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <a href={urls.webcal}>
            <CalendarPlus aria-hidden="true" />
            {t("schedule.subscribeApp")}
          </a>
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={copyLink}>
          {copy === "copied" ? (
            <Check aria-hidden="true" />
          ) : (
            <Copy aria-hidden="true" />
          )}
          {copy === "copied"
            ? t("schedule.subscribeCopied")
            : t("schedule.subscribeCopy")}
        </DropdownMenuItem>
        {copy === "failed" ? (
          <p className="subscribe-address" role="status">
            {urls.https}
          </p>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
