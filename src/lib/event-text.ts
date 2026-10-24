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

// The neutral wording of the YouTube details earlier releases generated.
const legacyDetails: Record<string, string> = {
  "YouTube Live / 预约": "YouTube Live",
  "YouTube Premiere / 预约": "YouTube Premiere",
  "メンバーシップ限定 / YouTube": "YouTube Live",
}

/** Stored detail as shown; operator and model text stay as written. */
export function eventDetailText(detail: string | null | undefined) {
  const value = String(detail || "")
  return legacyDetails[value] ?? value
}
