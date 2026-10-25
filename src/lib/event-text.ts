import type { Translator } from "../types.ts"

// Stored event status is a code the interface translates: synchronization
// writes scheduled, live or ended for YouTube and pending for an extraction
// without one. Earlier releases wrote Chinese display text, which locked rows
// and tombstones keep; it reads as the same codes.
const legacyStatusCodes: Record<string, string> = {
  已预约: "scheduled",
  会员限定: "scheduled",
  直播中: "live",
  已完成: "ended",
  待确认: "pending",
}

export const eventStatusCodes = [
  "scheduled",
  "tentative",
  "pending",
  "confirmed",
  "released",
  "live",
  "ended",
  "cancelled",
]

/** The translatable code of a stored status, or null for an operator note. */
export function eventStatusCode(status: string | null | undefined) {
  const value = String(status || "").trim()
  const code = legacyStatusCodes[value] || value.toLowerCase()
  return eventStatusCodes.includes(code) ? code : null
}

// YouTube synchronization writes one of these English details, which the
// interface translates like a status code. Earlier releases wrote the Chinese
// and Japanese forms, which locked rows and tombstones keep.
const detailCodes: Record<string, string> = {
  "YouTube Live": "youtube_live",
  "YouTube Premiere": "youtube_premiere",
  "YouTube Live / 预约": "youtube_live",
  "YouTube Premiere / 预约": "youtube_premiere",
  "メンバーシップ限定 / YouTube": "youtube_live",
}

/** The translatable code of a generated detail, or null for other text. */
export function eventDetailCode(detail: string | null | undefined) {
  return detailCodes[String(detail || "").trim()] ?? null
}

/**
 * Stored detail as shown: a generated YouTube detail in the reader's
 * language; operator and model text stay as written.
 */
export function eventDetailText(
  detail: string | null | undefined,
  t: Translator,
) {
  const code = eventDetailCode(detail)
  return code ? t(`schedule.detail.${code}`) : String(detail || "")
}
