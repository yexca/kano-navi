import type { ReactNode } from "react"
import { useEffect, useRef } from "react"
import { ChevronLeft, ChevronRight, X } from "lucide-react"

import { useAppSettings } from "@/app-settings"

/** Parts of the viewer a click keeps open; a click anywhere else closes it. */
const KEEP_OPEN =
  "img, a, button, .media-dialog-bar, .media-dialog-details, .history-missing"

/**
 * Native modal image viewer shared by the public pages. Escape, the close
 * button in the screen corner and a click outside the image and its controls
 * close it; arrow keys page through a set, and focus returns to the opener.
 */
export function MediaDialog<T>({
  items,
  index,
  label,
  onClose,
  onStep,
  renderImage,
  renderDetails,
}: {
  items: T[] | null | undefined
  index: number
  label: string
  onClose: () => void
  onStep: (offset: number) => void
  renderImage: (item: T) => ReactNode
  renderDetails?: (item: T) => ReactNode
}) {
  const { t } = useAppSettings()
  const dialogRef = useRef<HTMLDialogElement>(null)
  const closeRef = useRef<HTMLButtonElement>(null)
  const open = Boolean(items?.length)

  useEffect(() => {
    const dialog = dialogRef.current
    if (!open || !dialog) return undefined
    const previous = document.activeElement as HTMLElement | null
    if (!dialog.open) dialog.showModal?.()
    closeRef.current?.focus()
    document.body.classList.add("has-lightbox")
    return () => {
      if (dialog.open) dialog.close()
      document.body.classList.remove("has-lightbox")
      previous?.focus?.()
    }
  }, [open])

  const item = items?.[index]
  if (!open || !item) return null
  const many = items.length > 1

  return (
    <dialog
      ref={dialogRef}
      className={`media-dialog${renderDetails ? " has-details" : ""}${many ? " has-steps" : ""}`}
      aria-label={label}
      onCancel={(event) => {
        event.preventDefault()
        onClose()
      }}
      onClick={(event) => {
        if (!(event.target as Element).closest(KEEP_OPEN)) onClose()
      }}
      onKeyDown={(event) => {
        if (!many) return
        if (event.key === "ArrowLeft") {
          event.preventDefault()
          onStep(-1)
        } else if (event.key === "ArrowRight") {
          event.preventDefault()
          onStep(1)
        }
      }}
    >
      <div className="media-dialog-frame">
        <div className="media-dialog-stage">{renderImage(item)}</div>
        {renderDetails ? (
          <div className="media-dialog-details">{renderDetails(item)}</div>
        ) : null}
        {many ? (
          <div className="media-dialog-bar">
            <button
              type="button"
              className="round-button"
              onClick={() => onStep(-1)}
              aria-label={t("feed.previousImage")}
            >
              <ChevronLeft aria-hidden="true" />
            </button>
            <span className="media-dialog-count" aria-live="polite">
              {index + 1} / {items.length}
            </span>
            <button
              type="button"
              className="round-button"
              onClick={() => onStep(1)}
              aria-label={t("feed.nextImage")}
            >
              <ChevronRight aria-hidden="true" />
            </button>
          </div>
        ) : null}
      </div>
      <button
        ref={closeRef}
        type="button"
        className="round-button media-dialog-close"
        onClick={onClose}
        aria-label={t("common.close")}
        title={t("common.close")}
      >
        <X aria-hidden="true" />
      </button>
    </dialog>
  )
}

/** Index state for a MediaDialog: open a set at a position, then wrap. */
export function stepIndex(index: number, offset: number, length: number) {
  return (index + offset + length) % length
}
