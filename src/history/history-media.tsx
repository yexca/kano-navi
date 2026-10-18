import type { Locale, Translator } from "@/types"
import { useState } from "react"
import { ArrowUpRight, ImageOff, Images } from "lucide-react"

import { ExternalLink } from "@/site/links"
import { localized, sourceName } from "./media"

export type HistoryMediaItem =
  (typeof import("./media"))["historyMedia"][number]

/** Local fixed image; a missing file shows an explicit unavailable state. */
export function HistoryImage({
  item,
  locale,
  t,
  eager = false,
  decorative = false,
}: {
  item: HistoryMediaItem
  locale: Locale
  t: Translator
  eager?: boolean
  decorative?: boolean
}) {
  const [failed, setFailed] = useState(false)
  if (failed) {
    return (
      <span
        className="history-missing"
        role={decorative ? undefined : "img"}
        aria-label={decorative ? undefined : localized(item.title, locale)}
      >
        <ImageOff aria-hidden="true" />
        <span>{t("history.imageUnavailable")}</span>
      </span>
    )
  }
  return (
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

/** Up to four timeline thumbnails; the rest are a count on the last one. */
export function MilestoneMedia({
  media,
  locale,
  t,
  onOpen,
}: {
  media: HistoryMediaItem[]
  locale: Locale
  t: Translator
  onOpen: (index: number) => void
}) {
  const shown = media.slice(0, 4)
  const hidden = media.length - shown.length
  return (
    <div className={`milestone-media count-${shown.length}`}>
      {shown.map((item, index) => (
        <button
          key={item.id}
          type="button"
          onClick={() => onOpen(index)}
          aria-label={t("history.openImage", {
            title: localized(item.title, locale),
          })}
        >
          <HistoryImage item={item} locale={locale} t={t} decorative />
          {item.frameTime ? (
            <span className="frame-time">{item.frameTime}</span>
          ) : null}
          {index === shown.length - 1 && hidden ? (
            <span className="milestone-media-more">
              <Images aria-hidden="true" />+{hidden}
            </span>
          ) : null}
        </button>
      ))}
    </div>
  )
}

export function GalleryTile({
  item,
  locale,
  t,
  onOpen,
}: {
  item: HistoryMediaItem
  locale: Locale
  t: Translator
  onOpen: () => void
}) {
  return (
    <figure className="gallery-tile">
      <button
        type="button"
        className="gallery-tile-image"
        onClick={onOpen}
        aria-label={t("history.openImage", {
          title: localized(item.title, locale),
        })}
      >
        <HistoryImage item={item} locale={locale} t={t} />
        {item.frameTime ? (
          <span className="frame-time">{item.frameTime}</span>
        ) : null}
      </button>
      <figcaption>
        <span className="gallery-tile-meta">
          <span>{t(`history.mediaKind.${item.category}`)}</span>
          <span>{item.date || t("history.dateUnknown")}</span>
        </span>
        <strong>{localized(item.title, locale)}</strong>
        {item.credit ? <small>{item.credit}</small> : null}
      </figcaption>
    </figure>
  )
}

/** Provenance panel for the shared image dialog. */
export function ImageDetails({
  item,
  locale,
  t,
}: {
  item: HistoryMediaItem
  locale: Locale
  t: Translator
}) {
  return (
    <div className="image-details">
      <p className="kicker">{t(`history.mediaKind.${item.category}`)}</p>
      <h2>{localized(item.title, locale)}</h2>
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
        <p className="image-details-note">
          {t(`history.imageNote.${item.note}`)}
        </p>
      ) : null}
      <div className="image-details-links">
        <ExternalLink className="pill-button" href={item.sourcePage}>
          {t("history.openSource")} · {sourceName(item.sourcePage, t)}
          <ArrowUpRight aria-hidden="true" />
        </ExternalLink>
        <ExternalLink className="text-link" href={item.url}>
          {t("history.cachedOriginal")}
          <ArrowUpRight aria-hidden="true" />
        </ExternalLink>
        <ExternalLink className="text-link" href={item.sourceUrl}>
          {t(item.frameTime ? "history.originalStream" : "history.sourceImage")}
          <ArrowUpRight aria-hidden="true" />
        </ExternalLink>
      </div>
      {item.credit ? (
        <p className="image-details-credit">{item.credit}</p>
      ) : null}
      <p className="image-details-rights">{t("history.imageRights")}</p>
    </div>
  )
}
