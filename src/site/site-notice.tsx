import { useEffect, useId, useRef } from "react"
import { ArrowUpRight, X } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { ExternalLink } from "./links"
import { LICENSE_URL } from "./repository"

const noticeKeys = [
  "notice.unofficial",
  "notice.snapshot",
  "notice.curated",
  "notice.translation",
  "notice.rights",
]

/**
 * The site's one notice, the same on every page: what the site is, how far
 * to trust it, whose rights its content is, and the code license. The
 * footer's Notes opens it in a dialog; About shows it in place.
 */
export function SiteNotice() {
  const { t } = useAppSettings()
  return (
    <ul className="site-notice">
      {noticeKeys.map((key) => (
        <li key={key}>{t(key)}</li>
      ))}
      <li>
        {t("notice.license")}{" "}
        <ExternalLink className="text-link" href={LICENSE_URL}>
          {t("notice.fullLicense")}
          <ArrowUpRight aria-hidden="true" />
        </ExternalLink>
      </li>
    </ul>
  )
}

/**
 * The notice in a native modal dialog. Escape and the backdrop close it, its
 * close button takes focus, and focus returns to the opener without
 * scrolling the page.
 */
export function NoticeDialog({ onClose }: { onClose: () => void }) {
  const { t } = useAppSettings()
  const ref = useRef<HTMLDialogElement>(null)
  const titleId = useId()
  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return undefined
    const previous = document.activeElement as HTMLElement | null
    if (!dialog.open) dialog.showModal?.()
    dialog.querySelector<HTMLElement>(".site-notice-close")?.focus()
    document.body.classList.add("has-detail-sheet")
    return () => {
      if (dialog.open) dialog.close()
      document.body.classList.remove("has-detail-sheet")
      previous?.focus?.({ preventScroll: true })
    }
  }, [])
  return (
    <dialog
      ref={ref}
      className="site-notice-dialog"
      aria-labelledby={titleId}
      onCancel={(event) => {
        event.preventDefault()
        onClose()
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className="site-notice-sheet">
        <header>
          <h2 id={titleId}>{t("footer.notes")}</h2>
          <button
            type="button"
            className="round-button site-notice-close"
            aria-label={t("common.close")}
            onClick={onClose}
          >
            <X aria-hidden="true" />
          </button>
        </header>
        <SiteNotice />
      </div>
    </dialog>
  )
}
