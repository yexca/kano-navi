import type { Locale } from "../types.ts"
import personal from "./personal.json" with { type: "json" }
import summary from "./summary.json" with { type: "json" }
import type {
  PersonalRelease,
  Upload,
  UploadVideos,
  WorksCover,
} from "./types.ts"

/** Kano's own releases, oldest first as the collection lists them. */
export const personalReleases = personal as PersonalRelease[]

/** Counts and years for the page's figures; the other sections load on demand. */
export const worksSummary = summary

export const OFFICIAL_DISCOGRAPHY =
  "https://kano-official.amebaownd.com/posts/categories/1163113"

/** Covers stay in the server's data directory. */
export function mediaUrl(name: string) {
  return `/media/works/${name}`
}

export function coverUrl(cover: WorksCover) {
  return mediaUrl(cover.media)
}

/** One of the page's tones, the same for an upload on every visit. */
export function uploadTone(id: string) {
  let hash = 0
  for (const char of id) hash = (hash * 31 + char.charCodeAt(0)) >>> 0
  return hash % 5
}

/** MM.DD of a Japan date, shown under its year. */
export function dayLabel(date: string) {
  const [, month, day] = date.split("-")
  return `${month}.${day}`
}

/** A Japan date in the reader's locale, without inventing a time. */
export function fullDate(date: string, locale: Locale) {
  const [year, month, day] = date.split("-").map(Number)
  return new Intl.DateTimeFormat(locale, {
    year: "numeric",
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  }).format(new Date(Date.UTC(year, month - 1, day)))
}

/** Records grouped under their Japan year, in the order given. */
export function groupByYear<T extends { date: string }>(items: T[]) {
  const groups: { year: string; items: T[] }[] = []
  for (const item of items) {
    const year = item.date.slice(0, 4)
    const last = groups[groups.length - 1]
    if (last?.year === year) last.items.push(item)
    else groups.push({ year, items: [item] })
  }
  return groups
}

/** Records by Japan date, newest or oldest first; ties keep their order. */
export function byDate<T extends { date: string }>(
  items: T[],
  order: "newest" | "oldest",
) {
  const sign = order === "newest" ? -1 : 1
  return [...items].sort((a, b) => sign * a.date.localeCompare(b.date))
}

/**
 * The edition a release opens on: the one with the most disc photos, so a
 * pressed CD comes before its download; the earlier edition on a tie. A
 * later edition holding every disc photo of the earlier pick, such as a
 * store set of the limited edition and a bonus disc, bundles it and does not
 * take its place.
 */
export function firstEdition(
  editions: { discs: { image: { media: string } }[] }[],
) {
  const photos = editions.map(
    (edition) => new Set(edition.discs.map((item) => item.image.media)),
  )
  let best = 0
  editions.forEach((edition, index) => {
    const bundles =
      photos[best].size > 0 &&
      [...photos[best]].every((media) => photos[index].has(media))
    if (edition.discs.length > editions[best].discs.length && !bundles)
      best = index
  })
  return best
}

/** Discs in order, each with its tracks; a one-disc release has one. */
export function discsOf<T extends { disc: number; no: number }>(tracks: T[]) {
  const discs = new Map<number, T[]>()
  for (const track of tracks) {
    const list = discs.get(track.disc) ?? []
    list.push(track)
    discs.set(track.disc, list)
  }
  return [...discs.entries()]
    .sort(([a], [b]) => a - b)
    .map(([disc, list]) => ({
      disc,
      tracks: [...list].sort((a, b) => a.no - b.no),
    }))
}

export function uploadLinks(upload: UploadVideos) {
  const links: {
    platform: "niconico" | "youtube" | "bilibili"
    /** The platform's video ID. */
    id: string
    href: string
    /** Only channel members can watch it. */
    members?: true
  }[] = []
  if (upload.niconico)
    links.push({
      platform: "niconico",
      id: upload.niconico,
      href: `https://www.nicovideo.jp/watch/${upload.niconico}`,
    })
  for (const id of upload.youtube)
    links.push({
      platform: "youtube",
      id,
      href: `https://www.youtube.com/watch?v=${id}`,
    })
  for (const id of upload.youtubeMembers)
    links.push({
      platform: "youtube",
      id,
      href: `https://www.youtube.com/watch?v=${id}`,
      members: true,
    })
  if (upload.bilibili)
    links.push({
      platform: "bilibili",
      id: upload.bilibili,
      href: `https://www.bilibili.com/video/${upload.bilibili}`,
    })
  return links
}

/** Case- and width-insensitive text match for the search boxes. */
export function matches(query: string, ...fields: string[]) {
  const needle = query.normalize("NFKC").toLowerCase().trim()
  if (!needle) return true
  return fields.some((field) =>
    field.normalize("NFKC").toLowerCase().includes(needle),
  )
}
