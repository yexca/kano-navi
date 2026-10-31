import { useState } from "react"
import { Combine, Lock, Sparkles, Split } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { formatDay, formatTime, japanDay } from "@/admin/format"
import type { AdminEvent, EventInput } from "@/admin/types"
import { Badge, Button, Loading, Modal } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"

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

/** The Japan date `days` away from `day`. */
export function shiftDay(day: string, days: number) {
  const date = new Date(`${day}T12:00:00+09:00`)
  date.setUTCDate(date.getUTCDate() + days)
  return japanDay(date.toISOString())
}

/**
 * Hides an entry behind another one the board shows, as an operator decision
 * automatic merging never changes. It offers the visible entries from the day
 * before to the day after, since an X post's date can be a day off.
 */
export function EventMergeDialog({
  event,
  onClose,
}: {
  event: AdminEvent
  onClose: (merged?: boolean) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const [busy, setBusy] = useState("")
  const candidates = useResource(
    () =>
      api.events({
        from: shiftDay(event.startsOn, -1),
        to: shiftDay(event.startsOn, 1),
        merge: "visible",
        order: "asc",
        pageSize: 50,
      }),
    [event.id, event.startsOn],
  )
  const items = (candidates.data?.items ?? []).filter(
    (item) => item.id !== event.id,
  )
  const merge = async (into: string) => {
    setBusy(into)
    const result = await act(
      () => api.mergeEvent(event.id, into),
      "admin.notice.eventMerged",
    )
    setBusy("")
    if (result) onClose(true)
  }
  return (
    <Modal
      open
      className="adm-event-merge"
      title={t("admin.schedule.merge.title")}
      onClose={() => onClose()}
    >
      <p className="adm-muted">
        {t("admin.schedule.merge.hint", {
          title: event.title,
          day: formatDay(event.startsOn, locale),
        })}
      </p>
      {!candidates.data ? (
        <Loading />
      ) : items.length ? (
        <ul className="adm-merge-candidates">
          {items.map((item) => (
            <li key={item.id}>
              <span className="adm-merge-video-body">
                <strong>{item.title}</strong>
                <small>
                  {formatDay(item.startsOn, locale)} ·{" "}
                  {item.startsAt
                    ? `${formatTime(item.startsAt, locale)} JST`
                    : t("admin.event.dateOnly")}{" "}
                  · {t(`admin.event.type.${item.eventType || "event"}`)} ·{" "}
                  {t(`admin.schedule.merge.source.${item.source}`)}
                </small>
              </span>
              <Button
                size="sm"
                icon={Combine}
                busy={busy === item.id}
                disabled={Boolean(busy)}
                onClick={() => merge(item.id)}
              >
                {t("admin.schedule.merge.action")}
              </Button>
            </li>
          ))}
        </ul>
      ) : (
        <p className="adm-muted">{t("admin.schedule.merge.none")}</p>
      )}
    </Modal>
  )
}

export function CancellationBadge({ event }: { event: AdminEvent }) {
  const { t } = useAppSettings()
  if (event.cancellationStatus === "manual_confirmed")
    return <Badge tone="danger">{t("admin.event.cancelled")}</Badge>
  if (event.cancellationStatus === "llm_suspected")
    return <Badge tone="honey">{t("admin.event.suspected")}</Badge>
  return null
}
