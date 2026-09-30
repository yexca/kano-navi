import { useEffect, useRef } from "react"
import { ChevronLeft, ChevronRight, X } from "lucide-react"

export function Lightbox({ media, index, onClose, onStep, t }) {
  const closeRef = useRef(null)
  const hasMedia = Boolean(media?.length)

  useEffect(() => {
    if (!hasMedia) return undefined
    const previouslyFocused = document.activeElement
    const onKeyDown = (event) => {
      if (event.key === "Escape") onClose()
      if (event.key === "ArrowLeft") onStep(-1)
      if (event.key === "ArrowRight") onStep(1)
    }
    document.addEventListener("keydown", onKeyDown)
    document.body.classList.add("has-lightbox")
    closeRef.current?.focus()
    return () => {
      document.removeEventListener("keydown", onKeyDown)
      document.body.classList.remove("has-lightbox")
      previouslyFocused?.focus?.()
    }
  }, [hasMedia, onClose, onStep])

  if (!hasMedia || !media[index]) return null
  const item = media[index]
  const many = media.length > 1

  return (
    <div
      className="lightbox"
      role="dialog"
      aria-modal="true"
      aria-label={t("feed.openMedia")}
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <button
        ref={closeRef}
        type="button"
        className="lightbox-button lightbox-close"
        onClick={onClose}
        aria-label={t("common.close")}
      >
        <X aria-hidden="true" />
      </button>
      {many ? (
        <button
          type="button"
          className="lightbox-button lightbox-prev"
          onClick={() => onStep(-1)}
          aria-label={t("feed.previousImage")}
        >
          <ChevronLeft aria-hidden="true" />
        </button>
      ) : null}
      <figure>
        <img src={item.url} alt={item.alt || ""} />
        {many ? (
          <figcaption>
            {index + 1} / {media.length}
          </figcaption>
        ) : null}
      </figure>
      {many ? (
        <button
          type="button"
          className="lightbox-button lightbox-next"
          onClick={() => onStep(1)}
          aria-label={t("feed.nextImage")}
        >
          <ChevronRight aria-hidden="true" />
        </button>
      ) : null}
    </div>
  )
}
