import { useEffect, useRef, useState } from "react"
import {
  ArrowUpRight,
  ChevronLeft,
  ChevronRight,
  ImageOff,
  Maximize2,
  X,
} from "lucide-react"

import { ExternalLink } from "@/dashboard/components/primitives"
import { localized, sourceName } from "./media"

export function HistoryImage({
  item,
  locale,
  t,
  eager = false,
  decorative = false,
}) {
  const [failed, setFailed] = useState(false)
  return failed ? (
    <span
      className="history-image-missing"
      role={decorative ? undefined : "img"}
      aria-label={decorative ? undefined : localized(item.title, locale)}
    >
      <ImageOff aria-hidden="true" />
      <span>{t("history.imageUnavailable")}</span>
    </span>
  ) : (
    <img
      src={item.url}
      alt={decorative ? "" : localized(item.title, locale)}
      width={item.width}
      height={item.height}
      loading={eager ? "eager" : "lazy"}
      decoding="async"
      onError={() => setFailed(true)}
    />
  )
}

export function ImageCard({ item, locale, t, onOpen, compact = false }) {
  return (
    <figure className={`history-image-card${compact ? " is-compact" : ""}`}>
      <button
        type="button"
        className="history-image-preview"
        onClick={onOpen}
        aria-label={t("history.openImage", {
          title: localized(item.title, locale),
        })}
      >
        <HistoryImage item={item} locale={locale} t={t} />
        <span className="history-image-zoom" aria-hidden="true">
          <Maximize2 />
        </span>
        {item.frameTime ? (
          <span className="history-frame-time">{item.frameTime}</span>
        ) : null}
      </button>
      <figcaption>
        <strong>{localized(item.title, locale)}</strong>
        <span className="history-image-meta">
          {item.date ? (
            <time dateTime={item.date}>{item.date}</time>
          ) : (
            t("history.dateUnknown")
          )}
          {item.date ? (
            <span> · {t(`history.dateKind.${item.dateKind}`)}</span>
          ) : null}
        </span>
        <span className="history-image-meta">
          {item.width} × {item.height} · {item.mimeType.slice(6).toUpperCase()}
        </span>
        <ExternalLink className="journey-source" href={item.sourcePage}>
          {sourceName(item.sourcePage, t)}
          <ArrowUpRight aria-hidden="true" />
        </ExternalLink>
        {item.credit ? (
          <small className="history-image-credit">{item.credit}</small>
        ) : null}
      </figcaption>
    </figure>
  )
}

export function HistoryImageViewer({
  media,
  index = 0,
  locale,
  t,
  onClose,
  onStep,
}) {
  const dialogRef = useRef(null)
  const closeRef = useRef(null)
  const hasMedia = Boolean(media?.length)
  const item = media?.[index]

  useEffect(() => {
    if (!hasMedia) return undefined
    const dialog = dialogRef.current
    const previousFocus = document.activeElement
    dialog.showModal()
    closeRef.current?.focus()
    document.body.classList.add("has-lightbox")
    return () => {
      dialog.close()
      document.body.classList.remove("has-lightbox")
      previousFocus?.focus?.()
    }
  }, [hasMedia])

  if (!item) return null
  const many = media.length > 1
  return (
    <dialog
      ref={dialogRef}
      className="history-viewer"
      aria-labelledby="history-image-title"
      onCancel={(event) => {
        event.preventDefault()
        onClose()
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
      onKeyDown={(event) => {
        if (many && event.key === "ArrowLeft") {
          event.preventDefault()
          onStep(-1)
        }
        if (many && event.key === "ArrowRight") {
          event.preventDefault()
          onStep(1)
        }
      }}
    >
      <div className="history-viewer-card">
        <div className="history-viewer-image">
          <HistoryImage key={item.id} item={item} locale={locale} t={t} eager />
        </div>
        <div className="history-viewer-details">
          <p className="eyebrow">{t(`history.mediaKind.${item.category}`)}</p>
          <h2 id="history-image-title">{localized(item.title, locale)}</h2>
          <dl>
            <div>
              <dt>{t(`history.dateKind.${item.dateKind}`)}</dt>
              <dd>{item.date || t("history.dateUnknown")}</dd>
            </div>
            <div>
              <dt>{t("history.imageSize")}</dt>
              <dd>
                {item.width} × {item.height} ·{" "}
                {item.mimeType.slice(6).toUpperCase()}
              </dd>
            </div>
            <div>
              <dt>{t("history.imageSource")}</dt>
              <dd>{t(`history.sourceKind.${item.sourceKind}`)}</dd>
            </div>
            {item.creator ? (
              <div>
                <dt>{t("history.creator")}</dt>
                <dd>{item.creator}</dd>
              </div>
            ) : null}
            {item.frameTime ? (
              <div>
                <dt>{t("history.frameTime")}</dt>
                <dd>{item.frameTime}</dd>
              </div>
            ) : null}
          </dl>
          {item.note ? (
            <p className="history-image-note">
              {t(`history.imageNote.${item.note}`)}
            </p>
          ) : null}
          <div className="history-viewer-links">
            <ExternalLink className="soft-button" href={item.sourcePage}>
              {t("history.openSource")} · {sourceName(item.sourcePage, t)}
              <ArrowUpRight aria-hidden="true" />
            </ExternalLink>
            <ExternalLink className="journey-source" href={item.url}>
              {t("history.cachedOriginal")}
              <ArrowUpRight aria-hidden="true" />
            </ExternalLink>
            <ExternalLink className="journey-source" href={item.sourceUrl}>
              {t(
                item.frameTime
                  ? "history.originalStream"
                  : "history.sourceImage",
              )}
              <ArrowUpRight aria-hidden="true" />
            </ExternalLink>
          </div>
          {item.credit ? (
            <p className="history-image-credit">{item.credit}</p>
          ) : null}
          <p className="history-image-rights">{t("history.imageRights")}</p>
          {many ? (
            <div className="history-viewer-pagination">
              <button
                type="button"
                className="icon-button"
                onClick={() => onStep(-1)}
                aria-label={t("feed.previousImage")}
              >
                <ChevronLeft aria-hidden="true" />
              </button>
              <span aria-live="polite">
                {index + 1} / {media.length}
              </span>
              <button
                type="button"
                className="icon-button"
                onClick={() => onStep(1)}
                aria-label={t("feed.nextImage")}
              >
                <ChevronRight aria-hidden="true" />
              </button>
            </div>
          ) : null}
        </div>
        <button
          type="button"
          className="icon-button history-viewer-close"
          ref={closeRef}
          onClick={onClose}
          aria-label={t("common.close")}
        >
          <X aria-hidden="true" />
        </button>
      </div>
    </dialog>
  )
}
