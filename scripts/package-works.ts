import crypto from "node:crypto"
import fs from "node:fs/promises"
import path from "node:path"
import { fileURLToPath } from "node:url"

import { imageSize } from "./package-lineblog.ts"
import type {
  DiscImage,
  KanoCredit,
  ParticipationForm,
  ParticipationRelease,
  ParticipationRole,
  PersonalEdition,
  PersonalFormat,
  PersonalRelease,
  StoreBonus,
  StoreGroup,
  StoreLink,
  StorePlatform,
  Upload,
  UploadKind,
  WorksCover,
  WorksCredit,
  WorksMedia,
  WorksTrack,
  WorksVideoRef,
} from "../src/works/types.ts"

const MAX_IMAGE_BYTES = 15 * 1024 * 1024
const MAX_CSV_BYTES = 2 * 1024 * 1024
const MAX_MANIFEST_BYTES = 8 * 1024 * 1024

const projectRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
)

// ---------------------------------------------------------------------------
// CSV
// ---------------------------------------------------------------------------

/** RFC 4180 rows of a UTF-8 file with an optional BOM, keyed by its header. */
export function parseCsv(text: string) {
  const rows: string[][] = []
  let row: string[] = []
  let field = ""
  let quoted = false
  const source = text.replace(/^﻿/u, "")
  for (let at = 0; at < source.length; at += 1) {
    const char = source[at]
    if (quoted) {
      if (char !== '"') field += char
      else if (source[at + 1] === '"') {
        field += '"'
        at += 1
      } else quoted = false
    } else if (char === '"') quoted = true
    else if (char === ",") {
      row.push(field)
      field = ""
    } else if (char === "\n" || char === "\r") {
      if (char === "\r" && source[at + 1] === "\n") at += 1
      row.push(field)
      rows.push(row)
      row = []
      field = ""
    } else field += char
  }
  if (field || row.length) {
    row.push(field)
    rows.push(row)
  }
  const [header, ...body] = rows
  if (!header) return []
  return body
    .filter((cells) => cells.some((cell) => cell !== ""))
    .map((cells) => {
      if (cells.length !== header.length)
        throw new Error(
          `A row has ${cells.length} fields where the header has ${header.length}.`,
        )
      return Object.fromEntries(
        header.map((key, index) => [key, cells[index].trim()]),
      )
    })
}

function requireColumns(rows: Record<string, string>[], columns: string[]) {
  const first = rows[0]
  if (!first) throw new Error("A works table is empty.")
  const missing = columns.filter((column) => !(column in first))
  if (missing.length)
    throw new Error(`A works table lacks columns: ${missing.join(", ")}.`)
}

const isoDate = /^\d{4}-\d{2}-\d{2}$/u

/** A calendar date in YYYY-MM-DD form, such as 2020-02-29 but not 2020-02-30. */
function checkDate(value: string) {
  const parsed = isoDate.test(value) ? new Date(`${value}T00:00:00Z`) : null
  if (
    !parsed ||
    Number.isNaN(parsed.getTime()) ||
    parsed.toISOString().slice(0, 10) !== value
  )
    throw new Error(`Not a Japan date: ${value}`)
  return value
}

/** Stable record id: its Japan date and a hash of its title. */
export function recordId(date: string, title: string) {
  const hash = crypto.createHash("sha256").update(title).digest("hex")
  return `${date}-${hash.slice(0, 6)}`
}

/*
 * The collection's notes are the maintainer's checking record, so packaging
 * keeps none of them. Shown fields lose the parentheticals that cite where a
 * fact was checked, and packaging stops when such wording remains elsewhere.
 */
const checkingWords =
  /\bNAS\b|据|存疑|推定|核实|核定|未确认|不明|unconfirmed|VocaDB|VGMdb|MusicBrainz|Discogs|骏河屋|整理者|使用者|报告|见 |为同曲/u
/**
 * Asides on where a cover file came from, which the source link shows, or on
 * how two official pages disagree.
 */
const fileAsides = /销售页|像素|商品图|实物照片|官网/u

const parenthetical = /\s*(?:（[^（）]*）|\([^()]*\))/gu

export function publicText(text: string) {
  const result = text
    .replace(parenthetical, (part) =>
      checkingWords.test(part) || fileAsides.test(part) ? "" : part,
    )
    .trim()
  if (checkingWords.test(result))
    throw new Error(`Checking wording left in a shown field: ${result}`)
  return result
}

/** A value the collection marks as doubtful is left out rather than shown. */
export function certainText(text: string) {
  return /存疑|unconfirmed|推定|未确认/u.test(text) ? "" : publicText(text)
}

/** A track note that only records an open question is left out. */
export function publicTrackNote(note: string) {
  if (
    /[?？]/u.test(note) ||
    checkingWords.test(note.replace(parenthetical, ""))
  )
    return ""
  return publicText(note)
}

/** Share links carry the sharer's token, so a record keeps none of them. */
const shareLinkHosts = new Set(["m.tb.cn"])

/**
 * Second-hand marketplaces, where a listing is one seller's own page. A file
 * from one names the marketplace but does not link to the seller's listing.
 */
const marketplaces: ReadonlyMap<string, string> = new Map([
  ["jp.mercari.com", "メルカリ"],
  ["auctions.yahoo.co.jp", "Yahoo!オークション"],
  ["paypayfleamarket.yahoo.co.jp", "Yahoo!フリマ"],
])

/**
 * Where a file came from as the page shows it: a link to its page, or only
 * a marketplace's name for a seller's listing. label is the site's name
 * recorded with the page, if any.
 */
export function publicSource(
  url: string,
  label = "",
  hosts = marketplaces,
): Pick<WorksCover, "source" | "sourceLabel"> {
  const page = publicUrl(url)
  const marketplace = page ? hosts.get(new URL(page).hostname) : null
  if (marketplace) return { sourceLabel: label || marketplace }
  return {
    ...(page ? { source: page } : {}),
    ...(label ? { sourceLabel: label } : {}),
  }
}

export function publicUrl(url: string) {
  if (!url) return ""
  let parsed: URL
  try {
    parsed = new URL(url)
  } catch {
    throw new Error(`Not a source address: ${url}`)
  }
  if (parsed.protocol !== "https:" && parsed.protocol !== "http:")
    throw new Error(`Not a web source address: ${url}`)
  return shareLinkHosts.has(parsed.hostname) ? "" : url
}

function uniqueIds<T extends { id: string }>(items: T[]) {
  const seen = new Set<string>()
  for (const item of items) {
    if (seen.has(item.id)) throw new Error(`Duplicate works record ${item.id}`)
    seen.add(item.id)
  }
  return items
}

function split(value: string, separator: RegExp) {
  return value
    .split(separator)
    .map((part) => part.trim())
    .filter(Boolean)
}

// ---------------------------------------------------------------------------
// Covers
// ---------------------------------------------------------------------------

type CoverFile = { file: string; media: WorksMedia }

const coverExtension: Record<string, string> = {
  "image/jpeg": "jpg",
  "image/png": "png",
}

function mimeOf(body: Buffer) {
  if (body[0] === 0xff && body[1] === 0xd8 && body[2] === 0xff)
    return "image/jpeg"
  if (
    body
      .subarray(0, 8)
      .equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]))
  )
    return "image/png"
  return null
}

/** Reads one cover inside the collection and names it by its bytes. */
async function readCover(root: string, relative: string): Promise<CoverFile> {
  const file = await fs.realpath(path.resolve(root, relative))
  if (!file.startsWith(root + path.sep))
    throw new Error(`A cover lies outside the collection: ${relative}`)
  const stat = await fs.lstat(file)
  if (!stat.isFile() || stat.size > MAX_IMAGE_BYTES)
    throw new Error(`A cover is not a usable image file: ${relative}`)
  const body = await fs.readFile(file)
  const mimeType = mimeOf(body)
  const size = imageSize(body)
  if (!mimeType || !size)
    throw new Error(`A cover is not a JPEG or PNG: ${relative}`)
  const sha256 = crypto.createHash("sha256").update(body).digest("hex")
  return {
    file,
    media: {
      name: `${sha256.slice(0, 12)}.${coverExtension[mimeType]}`,
      mimeType,
      byteSize: body.length,
      sha256,
      ...size,
    },
  }
}

type ManifestImage = {
  file: string
  title: string
  title_zh: string
  source_page: string
  evidence_class: string
  credit_role?: string
  credit_name?: string
  credit_url?: string
}

const creditRoles = new Set<WorksCredit["role"]>(["scan", "photo", "fan"])

/**
 * Who a file from a person's own copy came from. Such a file shows its
 * credit instead of a source page: its record's page is evidence for the
 * collection, not where the file was published.
 */
export function creditOf(record: ManifestImage): WorksCredit | null {
  const role = record.credit_role ?? ""
  if (!role) {
    if (record.credit_name || record.credit_url)
      throw new Error(`A credit needs a role: ${record.file}`)
    return null
  }
  if (!creditRoles.has(role as WorksCredit["role"]))
    throw new Error(`Unknown credit role ${role}: ${record.file}`)
  const name = publicText(record.credit_name ?? "")
  if (role !== "fan" && !name)
    throw new Error(`A ${role} credit needs a name: ${record.file}`)
  const url = publicUrl(record.credit_url ?? "")
  if (url && !name)
    throw new Error(`A credit link needs a name: ${record.file}`)
  return {
    role: role as WorksCredit["role"],
    ...(name ? { name } : {}),
    ...(url ? { url } : {}),
  }
}

// ---------------------------------------------------------------------------
// Personal works
// ---------------------------------------------------------------------------

type Row = Record<string, string>

const personalKinds: Record<string, PersonalFormat> = {
  专辑: "album",
  单曲: "single",
  EP: "ep",
  同人: "doujin",
}

/** The collection's kind of a personal release, as a code. */
export function personalFormat(kind: string) {
  const format = personalKinds[kind]
  if (!format) throw new Error(`Unknown personal kind: ${kind}`)
  return format
}

function trackOf(row: Row): WorksTrack {
  const discLabel = publicText(row.disc_label ?? "")
  return {
    disc: Number(row.disc) || 1,
    ...(discLabel ? { discLabel } : {}),
    no: Number(row.no) || 0,
    title: row.track,
    credit: publicText(row.credit),
    note: publicTrackNote(row.note),
  }
}

/**
 * An edition's tracklist: its own discs, and the release's shared discs
 * whose number it does not replace.
 */
export function editionTracks(shared: WorksTrack[], own: WorksTrack[]) {
  const replaced = new Set(own.map((track) => track.disc))
  return [...shared.filter((track) => !replaced.has(track.disc)), ...own]
}

const cancelledMark = /（发售中止）$/u

/** "2,000 日元" and "15 元" in the yen and yuan signs; others as written. */
export function publicPrice(price: string) {
  const amount = /^([\d,]+)\s*(日元|元)$/u.exec(price)
  if (!amount) return publicText(price)
  return amount[2] === "日元" ? `¥${amount[1]}` : `CN¥${amount[1]}`
}

/** An edition that came out later says so as "YYYY-MM-DD 发售". */
function editionDate(notes: string) {
  const date = /^(\d{4}-\d{2}-\d{2}) 发售$/u.exec(notes)?.[1]
  return date ? checkDate(date) : null
}

/** The edition formats of a download or stream. */
const digitalFormat = /^(?:配信|数字专辑)$/u

function isDigital(row: Row, editions: PersonalEdition[]) {
  return (
    /配信|数字|デジタル/u.test(row.kind_note) ||
    row.catalog.includes("digital") ||
    editions.some((edition) => digitalFormat.test(edition.format))
  )
}

/**
 * Whether a release's record is its cover printed on a disc: no disc was
 * pressed (every edition a download or withdrawn, or a download sold in one
 * version), or the collection asks for it with printed_disc 封面.
 */
export function printedDiscOf(row: Row, editions: PersonalEdition[]) {
  const asked = row.printed_disc ?? ""
  if (asked && asked !== "封面")
    throw new Error(`Unknown printed_disc for ${row.title}: ${asked}`)
  if (asked) return true
  return editions.length
    ? editions.every((edition) => edition.printedDisc)
    : isDigital(row, editions)
}

const releaseKey = (date: string, title: string) => `${date}\n${title}`

/**
 * An edition's disc photos: its own, then the shared ones for the discs it
 * keeps. A disc the edition replaces with its own tracks drops the shared
 * photo, since that disc is not the shared one.
 */
export function editionDiscs(
  shared: DiscImage[],
  own: DiscImage[],
  ownTracks: WorksTrack[],
  tracks: WorksTrack[],
) {
  const replaced = new Set(ownTracks.map((track) => track.disc))
  const has = new Set(tracks.map((track) => track.disc))
  const mine = new Set(own.map((item) => item.disc))
  return [
    ...own,
    ...shared.filter(
      (item) => !mine.has(item.disc) && !replaced.has(item.disc),
    ),
  ]
    .filter((item) => has.has(item.disc))
    .sort((a, b) => a.disc - b.disc)
}

/** The optional table of disc photos; none when the collection lacks it. */
async function discRowsOf(root: string) {
  let text: string
  try {
    text = await readText(root, "discography_discs.csv")
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ENOENT")
      return []
    throw error
  }
  const rows = parseCsv(text)
  requireColumns(rows, ["date", "release", "edition", "disc", "image_file"])
  return rows
}

/** The optional table of store listings; none when the collection lacks it. */
async function storeRowsOf(root: string) {
  let text: string
  try {
    text = await readText(root, "discography_stores.csv")
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ENOENT")
      return []
    throw error
  }
  const rows = parseCsv(text)
  requireColumns(rows, [
    "date",
    "release",
    "edition",
    "item",
    "platform",
    "url",
  ])
  return rows
}

/**
 * Each platform's hosts, in the order the page lists them: the official
 * multi-store page, streaming services, download stores, then the label.
 */
const storeHosts: Record<StorePlatform, string[]> = {
  smart_link: ["linkco.re", "lnk.to"],
  apple_music: ["music.apple.com"],
  spotify: ["open.spotify.com"],
  amazon_music: ["music.amazon.co.jp"],
  line_music: ["music.line.me"],
  youtube_music: ["music.youtube.com"],
  awa: ["s.awa.fm"],
  itunes: ["music.apple.com"],
  mora: ["mora.jp"],
  recochoku: ["recochoku.jp"],
  ototoy: ["ototoy.jp"],
  qq_music: ["y.qq.com"],
  kugou: ["www.kugou.com"],
  label_page: ["www.sonymusic.co.jp", "www.teichiku.co.jp"],
}
const storeOrder = Object.keys(storeHosts) as StorePlatform[]

/**
 * A listing's canonical address. Only YouTube Music keeps a query, its album
 * playlist; anything that could carry a referral or a sharer's token stops
 * packaging.
 */
export function storeUrl(platform: StorePlatform, value: string) {
  const url = new URL(value)
  const query = [...url.searchParams.keys()]
  const allowed =
    platform === "youtube_music"
      ? url.pathname === "/playlist" &&
        query.length === 1 &&
        query[0] === "list"
      : !url.search
  if (
    url.protocol !== "https:" ||
    !storeHosts[platform].includes(url.hostname) ||
    url.hash ||
    url.username ||
    !allowed
  )
    throw new Error(`Not a canonical ${platform} listing: ${value}`)
  return url.href
}

/**
 * Store rows grouped by item, the main item first, each with its platforms
 * in the page's order. A high-resolution listing marks its store's own.
 */
export function storeGroups(rows: Row[]): StoreGroup[] {
  const groups = new Map<string, Map<StorePlatform, StoreLink>>()
  for (const row of rows) {
    const hiRes = row.platform.endsWith("_hires")
    const platform = row.platform.replace(/_hires$/u, "") as StorePlatform
    if (!storeOrder.includes(platform))
      throw new Error(`Unknown store platform: ${row.platform}`)
    const url = storeUrl(platform, row.url)
    const item = publicText(row.item ?? "")
    const links = groups.get(item) ?? new Map<StorePlatform, StoreLink>()
    groups.set(item, links)
    const known = links.get(platform)
    if (hiRes) {
      if (known) known.hiRes = true
      else links.set(platform, { platform, url, hiRes: true })
    } else if (known && !known.hiRes) {
      throw new Error(`${platform} is listed twice for ${row.release} ${item}`)
    } else {
      // The standard listing replaces a high-resolution one seen first.
      links.set(platform, { platform, url, hiRes: Boolean(known?.hiRes) })
    }
  }
  return [...groups.entries()]
    .sort(([a], [b]) => Number(Boolean(a)) - Number(Boolean(b)))
    .map(([item, links]) => ({
      item,
      links: [...links.values()].sort(
        (a, b) =>
          storeOrder.indexOf(a.platform) - storeOrder.indexOf(b.platform),
      ),
    }))
}

async function personalWorks(root: string, images: Images) {
  const rows = parseCsv(await readText(root, "discography.csv"))
  requireColumns(rows, [
    "date",
    "title",
    "category",
    "kind",
    "kind_note",
    "group",
    "label",
    "catalog",
    "cover_files",
  ])
  const trackRows = parseCsv(await readText(root, "discography_tracks.csv"))
  requireColumns(trackRows, [
    "date",
    "release",
    "edition",
    "disc",
    "disc_label",
    "no",
    "track",
    "credit",
    "note",
  ])
  const editionRows = parseCsv(await readText(root, "discography_editions.csv"))
  requireColumns(editionRows, [
    "date",
    "release",
    "edition",
    "catalog",
    "jan",
    "price",
    "format",
    "packaging",
    "cover_files",
    "bonus",
    "bonus_images",
    "notes",
  ])
  const bonusRows = parseCsv(
    await readText(root, "discography_store_bonuses.csv"),
  )
  requireColumns(bonusRows, [
    "date",
    "release",
    "store",
    "item",
    "applies_to",
    "image_files",
  ])

  const discRows = await discRowsOf(root)
  const storeRows = await storeRowsOf(root)

  const known = new Set(rows.map((row) => releaseKey(row.date, row.title)))
  for (const row of [
    ...trackRows,
    ...editionRows,
    ...bonusRows,
    ...discRows,
    ...storeRows,
  ])
    if (!known.has(releaseKey(row.date, row.release)))
      throw new Error(`A works row names an unknown release: ${row.release}`)

  const releases: PersonalRelease[] = []
  for (const row of rows) {
    if (!["独立作品", "组合作品"].includes(row.category))
      throw new Error(`Unknown personal category: ${row.category}`)
    const date = checkDate(row.date)
    const ofRelease = (item: Row) =>
      item.date === row.date && item.release === row.title
    const tracks = trackRows.filter(ofRelease)
    const shared = tracks.filter((track) => !track.edition).map(trackOf)
    const bonuses = bonusRows.filter(ofRelease)
    const editionNames = new Set<string>()
    const discPhotos = discRows.filter(ofRelease)
    const listings = storeRows.filter(ofRelease)
    const releaseStores = storeGroups(listings.filter((item) => !item.edition))
    const discsOf = async (edition: string) => {
      const items: DiscImage[] = []
      for (const item of discPhotos.filter((d) => d.edition === edition)) {
        const [image, ...rest] = await images.list(item.image_file)
        const disc = Number(item.disc)
        if (!image || rest.length || !Number.isInteger(disc) || disc < 1)
          throw new Error(
            `A disc photo of ${row.title} needs one image and a disc number`,
          )
        items.push({ disc, image })
      }
      return items
    }
    const sharedDiscs = await discsOf("")
    const discMedia = new Set<string>()

    const editions: PersonalEdition[] = []
    for (const edition of editionRows.filter(ofRelease)) {
      editionNames.add(edition.edition)
      const appliesTo = (bonus: Row) =>
        split(bonus.applies_to, /、/u).includes(edition.edition)
      const ownTracks = tracks
        .filter((track) => track.edition === edition.edition)
        .map(trackOf)
      const editionTrackList = editionTracks(shared, ownTracks)
      const ownDiscs = await discsOf(edition.edition)
      for (const item of ownDiscs)
        if (!editionTrackList.some((track) => track.disc === item.disc))
          throw new Error(
            `${row.title} ${edition.edition} has no disc ${item.disc}`,
          )
      // A download has no disc, so a digital edition shares no photo.
      const discs = editionDiscs(
        digitalFormat.test(edition.format) ? [] : sharedDiscs,
        ownDiscs,
        ownTracks,
        editionTrackList,
      )
      for (const item of discs) discMedia.add(item.image.media)
      const stores = storeGroups(
        listings.filter((item) => item.edition === edition.edition),
      )
      // An edition shows its own items and the release's together, so only
      // one of them may go unnamed.
      if (
        [...stores, ...releaseStores].filter((group) => !group.item).length > 1
      )
        throw new Error(
          `${row.title} ${edition.edition} needs a name for one of its store items`,
        )
      editions.push({
        name: publicText(edition.edition.replace(cancelledMark, "")),
        date: editionDate(edition.notes),
        cancelled: cancelledMark.test(edition.edition),
        catalog: certainText(edition.catalog),
        jan: edition.jan,
        price: publicPrice(edition.price),
        format: publicText(edition.format),
        packaging: publicText(edition.packaging),
        covers: await images.list(edition.cover_files),
        bonuses: split(edition.bonus, /；/u)
          .filter((item) => !/^无/u.test(item))
          .map(publicText),
        bonusImages: await images.list(edition.bonus_images),
        storeBonuses: await storeBonuses(bonuses.filter(appliesTo), images),
        stores,
        tracks: editionTrackList,
        discs,
        printedDisc:
          digitalFormat.test(edition.format) ||
          cancelledMark.test(edition.edition),
      })
    }
    for (const item of [...tracks, ...bonuses, ...discPhotos, ...listings])
      for (const name of split(item.edition ?? item.applies_to, /、/u))
        if (!editionNames.has(name))
          throw new Error(`${row.title} has no edition named ${name}`)
    const sharedNumbers = new Set(shared.map((track) => track.disc))
    for (const item of sharedDiscs) {
      if (!sharedNumbers.has(item.disc))
        throw new Error(`${row.title} shares no disc ${item.disc}`)
      discMedia.add(item.image.media)
    }
    // Disc photos show with their discs, not among the covers, unless a
    // release has no other image: then its disc is its cover.
    for (const edition of editions)
      edition.covers = edition.covers.filter(
        (image) => !discMedia.has(image.media),
      )
    const listed = await images.list(row.cover_files)
    const sleeves = listed.filter((image) => !discMedia.has(image.media))
    const covers = sleeves.length ? sleeves : listed
    // The disc face on the shelf: an optional disc_file, the first disc 1
    // photo, or an image captioned 盘面.
    const disc =
      (await images.list(row.disc_file ?? ""))[0] ??
      [...sharedDiscs, ...editions.flatMap((edition) => edition.discs)].find(
        (item) => item.disc === 1,
      )?.image ??
      [...listed, ...editions.flatMap((edition) => edition.covers)].find(
        (image) => image.caption === "盘面",
      ) ??
      null
    releases.push({
      id: recordId(date, row.title),
      date,
      title: row.title,
      format: personalFormat(row.kind),
      digital: isDigital(row, editions),
      cancelled:
        editions.length > 0 && editions.every((edition) => edition.cancelled),
      unit: row.category === "组合作品" ? row.group || null : null,
      label: publicText(row.label),
      catalog: certainText(row.catalog),
      covers,
      disc,
      printedDisc: printedDiscOf(row, editions),
      cutout: null,
      tracks: shared,
      discs: sharedDiscs,
      editions,
      storeBonuses: await storeBonuses(
        bonuses.filter((bonus) => !bonus.applies_to),
        images,
      ),
      stores: releaseStores,
    })
  }
  return uniqueIds(releases)
}

async function storeBonuses(rows: Row[], images: Images) {
  const items: StoreBonus[] = []
  for (const row of rows)
    items.push({
      store: publicText(row.store),
      item: publicText(row.item),
      images: await images.list(row.image_files),
    })
  return items
}

type Images = {
  /** The JPEG and PNG files of a ";"-separated list, in its order. */
  list(files: string): Promise<WorksCover[]>
  /** Listed files in another format, which the page does not serve. */
  skipped: string[]
}

function collectionImages(
  root: string,
  covers: Map<string, CoverFile>,
  manifest: Map<string, ManifestImage>,
): Images {
  const skipped: string[] = []
  return {
    skipped,
    async list(files) {
      const shown: WorksCover[] = []
      for (const relative of split(files, /;/u)) {
        const record = manifest.get(relative)
        if (!record)
          throw new Error(`A works image is not in the manifest: ${relative}`)
        if (!/.(?:jpe?g|png)$/iu.test(relative)) {
          if (!skipped.includes(relative)) skipped.push(relative)
          continue
        }
        const cover = await coverOf(root, relative, covers)
        const credit = creditOf(record)
        shown.push({
          ...cover,
          caption: publicText(
            record.title_zh.split("：").slice(1).join("：") || record.title_zh,
          ),
          ...(credit ? { credit } : publicSource(record.source_page)),
          thirdParty: record.evidence_class.includes("third_party"),
        })
      }
      return shown
    },
  }
}

async function coverOf(
  root: string,
  relative: string,
  covers: Map<string, CoverFile>,
) {
  let cover = covers.get(relative)
  if (!cover) {
    cover = await readCover(root, relative)
    covers.set(relative, cover)
  }
  const { name: media, width, height } = cover.media
  return { media, width, height }
}

// ---------------------------------------------------------------------------
// Participation
// ---------------------------------------------------------------------------

const roleWords: Record<string, ParticipationRole> = {
  演唱: "vocal",
  作词: "lyrics",
  作曲: "composition",
  声演: "voice",
  配音: "voice",
  嘉宾: "guest",
}

export function participationRoles(kanoTracks: string) {
  const head = kanoTracks.split("：")[0]
  const roles = split(head, /[、,]/u).map((word) => {
    const role = roleWords[word]
    if (!role) throw new Error(`Unknown participation role: ${word}`)
    return role
  })
  return [...new Set(roles)]
}

const formWords: [string, ParticipationForm][] = [
  ["配信", "digital"],
  ["特典", "bonus"],
  ["会场限定", "bonus"],
  ["广播 CD", "drama"],
  ["CD-R", "cdr"],
  ["同梱", "boxSet"],
  ["非卖品", "notForSale"],
  ["宣传视频", "promo"],
]

export function participationForms(form: string) {
  const forms = new Set<ParticipationForm>()
  for (const part of split(form, /[/·]/u)) {
    const matches = formWords.filter(([word]) => part.includes(word))
    if (!matches.length && part !== "众筹回报")
      throw new Error(`Unknown participation form: ${part}`)
    for (const [, code] of matches) forms.add(code)
  }
  return [...forms]
}

const kanoCredit = /\s*\(鹿乃: ([^)]*)\)/u

const creditWords: Record<string, KanoCredit> = {
  Lyricist: "lyrics",
  Composer: "composition",
  Arranger: "arrangement",
  Animator: "animation",
  Other: "other",
}

/** A database's roles for Kano on a track, as in "(鹿乃: Lyricist)". */
export function kanoCredits(credit: string) {
  const match = kanoCredit.exec(credit)
  if (!match) return []
  return split(match[1], /,/u).map((word) => {
    const code = creditWords[word]
    if (!code) throw new Error(`Unknown credit for Kano: ${word}`)
    return code
  })
}

async function participationWorks(
  root: string,
  covers: Map<string, CoverFile>,
) {
  const rows = parseCsv(await readText(root, "参与专辑/participation.csv"))
  requireColumns(rows, [
    "date",
    "title",
    "kano_tracks",
    "member_of",
    "form",
    "label",
    "event",
    "catalog",
    "cover_file",
    "source_url",
    "cover_source",
  ])
  const trackRows = parseCsv(
    await readText(root, "参与专辑/participation_tracks.csv"),
  )
  requireColumns(trackRows, [
    "date",
    "release",
    "disc",
    "no",
    "track",
    "credit",
    "note",
    "kano",
  ])
  const releases: ParticipationRelease[] = []
  for (const row of rows) {
    const date = checkDate(row.date)
    let cover: WorksCover | null = null
    if (row.cover_file) {
      const shown = await coverOf(
        path.join(root, "参与专辑"),
        row.cover_file,
        covers,
      )
      const [, label = "", url = ""] =
        /^(.*?)\s*(https?:\/\/\S+)?$/u.exec(row.cover_source) ?? []
      cover = { ...shown, ...publicSource(url, label), thirdParty: true }
    }
    releases.push({
      id: recordId(date, row.title),
      date,
      title: row.title,
      roles: participationRoles(row.kano_tracks),
      forms: participationForms(row.form),
      unit: row.member_of || null,
      label: publicText(row.label),
      event: certainText(row.event).replace(parenthetical, ""),
      catalog: certainText(row.catalog),
      source: publicUrl(row.source_url),
      cover,
      tracks: trackRows
        .filter((track) => track.date === date && track.release === row.title)
        .map((track) => ({
          disc: Number(track.disc) || 1,
          no: Number(track.no) || 0,
          title: track.track,
          credit: publicText(track.credit.replace(kanoCredit, "")),
          kano: track.kano === "1",
          kanoCredits: kanoCredits(track.credit),
        })),
    })
  }
  return uniqueIds(releases)
}

// ---------------------------------------------------------------------------
// Uploads
// ---------------------------------------------------------------------------

const uploadKinds: Record<string, UploadKind> = {
  翻唱: "cover",
  原创: "original",
  "官方 MV": "mv",
  参与企划: "project",
  合集: "compilation",
  "游戏 / 企划曲": "game",
  作曲: "composition",
}

const niconicoId = /^(sm|so|nm)\d+$/u
const youtubeId = /^[\w-]{11}$/u
const bilibiliId = /^BV[0-9A-Za-z]{10}$/u

function checkId(value: string, pattern: RegExp) {
  if (value && !pattern.test(value)) throw new Error(`Not a video id: ${value}`)
  return value
}

/** Figures cut out of a release's cover, named by the release title. */
const CUTOUT_DIR = "抠图"

/** PNG colour types with an alpha channel: grey and truecolour. */
const alphaColourTypes = new Set([4, 6])

/**
 * Gives each personal release its cutout: a transparent PNG in the cutout
 * folder named by its title. A file that names no release, or that is not a
 * transparent PNG, stops packaging.
 */
export async function addCutouts(
  root: string,
  personal: PersonalRelease[],
  covers: Map<string, CoverFile>,
) {
  let names: string[]
  try {
    names = await fs.readdir(path.join(root, CUTOUT_DIR))
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ENOENT")
      return
    throw error
  }
  const byTitle = new Map(
    personal.map((release) => [release.title.normalize("NFC"), release]),
  )
  for (const name of names.sort()) {
    if (name.startsWith(".")) continue
    const match = /^(.+)\.png$/iu.exec(name)
    const release = match && byTitle.get(match[1].normalize("NFC"))
    if (!release) throw new Error(`A cutout names no personal release: ${name}`)
    const relative = `${CUTOUT_DIR}/${name}`
    const { media, width, height } = await coverOf(root, relative, covers)
    const cover = covers.get(relative)!
    const head = await fs.readFile(cover.file)
    if (cover.media.mimeType !== "image/png" || !alphaColourTypes.has(head[25]))
      throw new Error(`A cutout is not a transparent PNG: ${name}`)
    release.cutout = { media, width, height }
  }
}

/** An upload's videos in the order the page and the cover step try them. */
export function uploadVideoRefs(upload: Upload): WorksVideoRef[] {
  return [
    ...upload.youtube.map((id) => ({ platform: "youtube" as const, id })),
    ...upload.youtubeMembers.map((id) => ({
      platform: "youtube" as const,
      id,
      members: true,
    })),
    ...(upload.niconico
      ? [{ platform: "niconico" as const, id: upload.niconico }]
      : []),
    ...(upload.bilibili
      ? [{ platform: "bilibili" as const, id: upload.bilibili }]
      : []),
  ]
}

/** The index of upload videos the server embeds (backend/internal/works). */
export function worksVideoIndex(uploads: Upload[], updatedAt: string) {
  return {
    updatedAt,
    uploads: uploads.map((upload) => ({
      id: upload.id,
      date: upload.date,
      videos: uploadVideoRefs(upload),
    })),
  }
}

async function uploads(root: string) {
  const rows = parseCsv(await readText(root, "投稿作品/works.csv"))
  requireColumns(rows, [
    "date",
    "song",
    "original_artist",
    "kind",
    "with",
    "niconico",
    "youtube",
    "youtube_members",
    "bilibili",
    "bilibili_title",
    "album",
    "tracks",
  ])
  const items: Upload[] = []
  for (const row of rows) {
    // The collection marks YouTube Shorts in the optional format column; the
    // page lists full uploads only.
    const format = row.format ?? ""
    if (format === "Shorts") continue
    if (format) throw new Error(`Unknown upload format: ${format}`)
    const kind = uploadKinds[row.kind]
    if (!kind) throw new Error(`Unknown upload kind: ${row.kind}`)
    const date = checkDate(row.date)
    const youtube = split(row.youtube, /;/u)
    const youtubeMembers = split(row.youtube_members, /;/u)
    for (const id of [...youtube, ...youtubeMembers]) checkId(id, youtubeId)
    const upload: Upload = {
      id: recordId(
        date,
        `${row.song}\n${row.niconico}${row.youtube}${row.youtube_members}`,
      ),
      date,
      song: row.song,
      originalArtist: row.original_artist,
      kind,
      with: row.with,
      niconico: checkId(row.niconico, niconicoId) || null,
      youtube,
      youtubeMembers,
      bilibili: checkId(row.bilibili, bilibiliId) || null,
      bilibiliTitle: row.bilibili_title,
      albums: split(row.album, /、/u),
      tracks: split(row.tracks, /；/u),
    }
    items.push(upload)
  }
  return uniqueIds(items)
}

// ---------------------------------------------------------------------------
// Uploads on albums
// ---------------------------------------------------------------------------

/** A title for matching: width, case, spacing, quotes and heart marks aside. */
function matchKey(title: string) {
  return title
    .normalize("NFKC")
    .toLowerCase()
    .replace(/[︎️]/gu, "")
    .replace(/[❤♥♡]/gu, "♡")
    .replace(/[\s「」『』"“”]/gu, "")
}

const withoutAsides = (key: string) => key.replace(/[(（][^)）]*[)）]/gu, "")

/**
 * Gives each track of a personal release the videos of the uploads that name
 * the release among their albums and the track as their song.
 */
export function linkUploads(personal: PersonalRelease[], uploads: Upload[]) {
  const releases = new Map(
    personal.map((release) => [matchKey(release.title), release]),
  )
  for (const upload of uploads)
    for (const album of upload.albums) {
      const release = releases.get(
        matchKey(album.replace(/[（(]\d{4}[)）]$/u, "")),
      )
      if (!release) continue
      const tracks = [
        ...release.tracks,
        ...release.editions.flatMap((edition) => edition.tracks),
      ]
      const song = matchKey(upload.song)
      const exact = tracks.filter((track) => matchKey(track.title) === song)
      const matched = exact.length
        ? exact
        : tracks.filter(
            (track) =>
              withoutAsides(matchKey(track.title)) === withoutAsides(song),
          )
      for (const track of matched) {
        const listen = (track.listen ??= {
          niconico: null,
          youtube: [],
          youtubeMembers: [],
          bilibili: null,
        })
        listen.niconico ??= upload.niconico
        listen.bilibili ??= upload.bilibili
        for (const id of upload.youtube)
          if (!listen.youtube.includes(id)) listen.youtube.push(id)
        for (const id of upload.youtubeMembers)
          if (!listen.youtubeMembers.includes(id))
            listen.youtubeMembers.push(id)
      }
    }
}

// ---------------------------------------------------------------------------
// Files
// ---------------------------------------------------------------------------

async function readText(root: string, relative: string, limit = MAX_CSV_BYTES) {
  const file = path.join(root, relative)
  const stat = await fs.lstat(file)
  if (!stat.isFile() || stat.size > limit)
    throw new Error(`Not a readable works file: ${relative}`)
  return fs.readFile(file, "utf8")
}

async function readManifest(root: string) {
  const manifest = JSON.parse(
    await readText(root, "manifest.json", MAX_MANIFEST_BYTES),
  ) as { images?: ManifestImage[] }
  if (!Array.isArray(manifest.images))
    throw new Error("The collection manifest lists no images.")
  return new Map(manifest.images.map((image) => [image.file, image]))
}

async function writeJson(file: string, value: unknown) {
  const temporary = `${file}.tmp`
  await fs.writeFile(temporary, `${JSON.stringify(value, null, 2)}\n`)
  await fs.rename(temporary, file)
}

async function sameFile(target: string, media: WorksMedia) {
  try {
    const stat = await fs.lstat(target)
    if (!stat.isFile() || stat.size !== media.byteSize) return false
    const body = await fs.readFile(target)
    return (
      crypto.createHash("sha256").update(body).digest("hex") === media.sha256
    )
  } catch {
    return false
  }
}

/**
 * Reads the whole selection before writing: every table, cover path, image
 * type and hash must check out, or nothing changes.
 */
export async function packageWorks(
  directory: string,
  {
    contentDir = path.join(projectRoot, "src/works"),
    mediaDir = path.join(projectRoot, "data/works"),
    indexFile,
    through,
  }: {
    contentDir?: string
    mediaDir?: string
    indexFile?: string
    /** The Japan date the upload list was checked through. */
    through?: string
  } = {},
) {
  // Another content directory needs its own index file, so a trial run never
  // writes the server's.
  const index =
    indexFile ??
    (path.resolve(contentDir) === path.join(projectRoot, "src/works")
      ? path.join(projectRoot, "backend/internal/works/videos.json")
      : null)
  if (!index)
    throw new Error("An index file is needed with a content directory")
  const root = await fs.realpath(directory)
  const covers = new Map<string, CoverFile>()
  const manifest = await readManifest(root)
  const images = collectionImages(root, covers, manifest)
  const personal = await personalWorks(root, images)
  const participation = await participationWorks(root, covers)
  const uploaded = await uploads(root)
  linkUploads(personal, uploaded)
  await addCutouts(root, personal, covers)

  const byName = new Map<string, CoverFile>()
  for (const cover of covers.values()) byName.set(cover.media.name, cover)
  const catalog = [...byName.values()]
    .map((cover) => cover.media)
    .sort((a, b) => a.name.localeCompare(b.name))
  const dates = [...personal, ...participation, ...uploaded]
    .map((item) => item.date)
    .sort()
  if (!dates.length) throw new Error("The collection lists no works.")
  const newestUpload = uploaded
    .map((item) => item.date)
    .sort()
    .at(-1)
  const updatedAt = through
    ? checkDate(through)
    : (newestUpload ?? dates.at(-1)!)
  if (newestUpload && updatedAt < newestUpload)
    throw new Error(
      `The upload list is checked through ${updatedAt}, before its newest upload`,
    )

  // Every check above passed; only now does anything change.
  await fs.mkdir(mediaDir, { recursive: true })
  for (const { file, media } of byName.values()) {
    const target = path.join(mediaDir, media.name)
    if (await sameFile(target, media)) continue
    const staged = `${target}.staged`
    await fs.copyFile(file, staged)
    await fs.rename(staged, target)
  }
  await fs.mkdir(contentDir, { recursive: true })
  await writeJson(path.join(contentDir, "personal.json"), personal)
  await writeJson(path.join(contentDir, "participation.json"), participation)
  await writeJson(path.join(contentDir, "uploads.json"), uploaded)
  await writeJson(path.join(contentDir, "media-catalog.json"), catalog)
  await writeJson(path.join(contentDir, "summary.json"), {
    personal: personal.length,
    participation: participation.length,
    uploads: uploaded.length,
    firstYear: Number(dates[0].slice(0, 4)),
    lastYear: Number(dates[dates.length - 1].slice(0, 4)),
    updatedAt,
  })
  await fs.mkdir(path.dirname(index), { recursive: true })
  await writeJson(index, worksVideoIndex(uploaded, updatedAt))
  return {
    personal: personal.length,
    participation: participation.length,
    uploads: uploaded.length,
    media: catalog.length,
    bytes: catalog.reduce((sum, media) => sum + media.byteSize, 0),
    skipped: images.skipped,
  }
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const args = process.argv.slice(2)
  const through =
    args.length === 4 && args[2] === "--through" ? args[3] : undefined
  if ((args.length !== 2 && through === undefined) || args[0] !== "--from") {
    console.error(
      "Usage: npm run works:package -- --from <kano_official> [--through YYYY-MM-DD]",
    )
    process.exitCode = 1
  } else {
    try {
      const result = await packageWorks(args[1], { through })
      console.log(
        `Packaged ${result.personal} personal works, ${result.participation} participations, ${result.uploads} uploads and ${result.media} covers (${(result.bytes / 1024 / 1024).toFixed(1)} MiB).`,
      )
      for (const file of result.skipped)
        console.warn(`Left out an image that is not a JPEG or PNG: ${file}`)
    } catch (error) {
      console.error(
        error instanceof Error && "code" in error && error.code
          ? "Unable to read the local kano_official collection."
          : error instanceof Error
            ? error.message
            : String(error),
      )
      process.exitCode = 1
    }
  }
}
