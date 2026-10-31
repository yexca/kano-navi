import type { ReactNode } from "react"
import { useEffect, useRef } from "react"
import { ChevronLeft, ChevronRight, X } from "lucide-react"

import { useAppSettings } from "@/app-settings"

/** Parts of the viewer a click keeps open; a click anywhere else closes it. */
const KEEP_OPEN =
  "img, a, button, .media-dialog-bar, .media-dialog-details, .media-dialog-note, .history-missing"

/**
 * Native modal image viewer shared by the public pages. Escape, the close
 * button in the screen corner and a click outside the image and its controls
 * close it; arrow keys page through a set, and focus returns to the opener.
 * Details show in a panel beside the image; an item's small-print note
 * closes the panel, or the viewer when the set has no details.
 */
export function MediaDialog<T>({
  items,
  index,
  label,
  onClose,
  onStep,
  renderImage,
  renderDetails,
  renderNote,
}: {
  items: T[] | null | undefined
  index: number
  label: string
  onClose: () => void
  onStep: (offset: number) => void
  renderImage: (item: T) => ReactNode
  /** The item's panel content; null when it has none. */
  renderDetails?: (item: T) => ReactNode
  /** Small print at the bottom of the panel or the viewer. */
  renderNote?: (item: T) => ReactNode
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
  // A set keeps one layout while it steps: the panel shows for every item
  // once any item has details.
  const panel =
    renderDetails !== undefined &&
    items.some((entry) => (renderDetails(entry) ?? null) !== null)
  const details = panel ? renderDetails(item) : null
  const note = renderNote?.(item) ?? null
  const noteText =
    note !== null ? <p className="media-dialog-note">{note}</p> : null

  return (
    <dialog
      ref={dialogRef}
      className={`media-dialog${panel ? " has-details" : ""}${!panel && note !== null ? " has-note" : ""}${many ? " has-steps" : ""}`}
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
        {panel ? (
          <div className="media-dialog-details">
            {details}
            {noteText}
          </div>
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
        {panel ? null : noteText}
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
