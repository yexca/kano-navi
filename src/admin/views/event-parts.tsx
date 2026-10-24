import { Combine, Lock, Sparkles, Split } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { formatDay, formatTime } from "@/admin/format"
import type { AdminEvent, EventInput } from "@/admin/types"
import { Badge } from "@/admin/ui"

/** The writable fields of a stored event, for a save that keeps them. */
export function eventToInput(event: AdminEvent): EventInput {
  return {
    title: event.title,
    detail: event.detail || "",
    startsOn: event.startsOn,
    startsAt: event.startsAt || "",
    endsAt: event.endsAt || "",
    timePrecision: event.startsAt ? event.timePrecision : undefined,
    eventType: event.eventType || "event",
    status: event.status || "",
    url: event.url || "",
    cancellationStatus: event.cancellationStatus,
    cancellationReason: event.cancellationReason || "",
  }
}

/** Japan date and, only when known, its time; date-only entries never gain one. */
export function EventWhen({ event }: { event: AdminEvent }) {
  const { t, locale } = useAppSettings()
  return (
    <span className="adm-when">
      <strong>{formatDay(event.startsOn, locale)}</strong>
      <small>
        {event.startsAt
          ? `${event.timePrecision === "approximate" ? "~" : ""}${formatTime(event.startsAt, locale)}${
              event.endsAt ? `–${formatTime(event.endsAt, locale)}` : ""
            } JST`
          : t("admin.event.dateOnly")}
      </small>
    </span>
  )
}

export function ProvenanceBadge({ event }: { event: AdminEvent }) {
  const { t } = useAppSettings()
  return event.provenance === "manual" || event.manualLocked ? (
    <Badge tone="sky" icon={Lock}>
      {t("admin.event.manual")}
    </Badge>
  ) : (
    <Badge tone="lavender" icon={Sparkles}>
      {t("admin.event.automatic")}
    </Badge>
  )
}

/** A merged duplicate names the entry it is hidden behind; an undone merge
 * says the entry is kept apart from automatic merging. */
export function MergeBadge({ event }: { event: AdminEvent }) {
  const { t } = useAppSettings()
  if (event.mergedInto)
    return (
      <Badge tone="lavender" icon={Combine} className="adm-merge-badge">
        <span title={event.mergeReason || undefined}>
          {t("admin.event.mergedInto", {
            title: event.mergedIntoTitle || event.mergedInto,
          })}
        </span>
      </Badge>
    )
  if (event.mergeSource === "manual")
    return <Badge icon={Split}>{t("admin.event.keptSeparate")}</Badge>
  return null
}

export function CancellationBadge({ event }: { event: AdminEvent }) {
  const { t } = useAppSettings()
  if (event.cancellationStatus === "manual_confirmed")
    return <Badge tone="danger">{t("admin.event.cancelled")}</Badge>
  if (event.cancellationStatus === "llm_suspected")
    return <Badge tone="honey">{t("admin.event.suspected")}</Badge>
  return null
}
