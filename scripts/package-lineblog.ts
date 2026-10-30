import crypto from "node:crypto"
import { createReadStream, createWriteStream } from "node:fs"
import fs from "node:fs/promises"
import path from "node:path"
import { Readable, Writable } from "node:stream"
import { pipeline } from "node:stream/promises"
import { fileURLToPath } from "node:url"
import zlib from "node:zlib"

import { designMedia } from "../src/lineblog/designs.ts"
import {
  recoveredImages,
  tweetPictures,
  type RecoveredImage,
  type TweetPictures,
} from "../src/lineblog/recovered.ts"
import type {
  LineblogBlock,
  LineblogImage,
  LineblogInline,
  LineblogMedia,
  LineblogPost,
} from "../src/lineblog/types.ts"

const MAX_IMAGE_BYTES = 15 * 1024 * 1024
const MAX_VIDEO_BYTES = 256 * 1024 * 1024
const MAX_POST_BYTES = 512 * 1024

const projectRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
)

/**
 * Archive media are named by the first 12 hex digits of a SHA-256: of an
 * image's bytes, and of a downloaded video's YouTube watch address.
 */
const mediaNamePattern = /^([0-9a-f]{12})\.(jpg|png|mp4)$/u
const mediaReference = /\.\.\/media\/([^)\s"]+)/u

// ---------------------------------------------------------------------------
// Markdown
// ---------------------------------------------------------------------------

interface ParsedFile {
  title: string
  titles: string[]
  published: string
  originalUrl: string
  snapshot: string
  blocks: LineblogBlock[]
  lines: string[]
  owner: number[]
}

/** Amazon links keep their product path; affiliate and API query parts go. */
export function cleanUrl(raw: string) {
  let url: URL
  try {
    url = new URL(raw)
  } catch {
    return raw
  }
  if (/(^|\.)amazon\.co\.jp$/u.test(url.hostname)) {
    const product = /\/dp\/([A-Z0-9]{10})/u.exec(url.pathname)
    return product
      ? `${url.protocol}//${url.hostname}/dp/${product[1]}`
      : `${url.protocol}//${url.hostname}${url.pathname.split("%3F")[0]}`
  }
  if (url.hostname === "twitter.com" || url.hostname === "t.co") {
    url.searchParams.delete("ref_src")
    url.searchParams.delete("src")
  }
  return url.toString()
}

/** Drops escapes and the backslash that marks a hard line break. */
function unescapeMarkdown(text: string) {
  return text
    .replace(/\\(?=\n|$)/gu, "")
    .replace(/\\([\\`*_{}[\]()#+\-.!|<>~])/gu, "$1")
}

function mediaName(src: string) {
  const match = mediaReference.exec(src)
  return match ? match[1] : null
}

function imageInline(alt: string, src: string, href?: string): LineblogImage {
  const media = mediaName(src)
  const image: LineblogImage = { type: "image", alt: unescapeMarkdown(alt) }
  if (media) image.media = media
  else image.remote = cleanUrl(src)
  if (href) image.href = cleanUrl(href)
  return image
}

const inlinePattern =
  /\[!\[((?:[^[\]]|\[[^\]]*\])*)\]\(([^)\s]+)\)\]\(([^)\s]+)\)|!\[((?:[^[\]]|\[[^\]]*\])*)\]\(([^)\s]+)\)|\[((?:[^[\]]|\[[^\]]*\])*)\]\(([^)\s]+)\)|(https?:\/\/[A-Za-z0-9\-._~:/?#@!$&'*+,;=%]*[A-Za-z0-9\-_~/#=&%])/gu

/** Links, images, linked images and bare URLs; everything else is text. */
export function parseInline(source: string): LineblogInline[] {
  const out: LineblogInline[] = []
  const text = (value: string) => {
    if (!value) return
    const last = out.at(-1)
    const unescaped = unescapeMarkdown(value)
    if (last?.type === "text") last.text += unescaped
    else out.push({ type: "text", text: unescaped })
  }
  let index = 0
  for (const match of source.matchAll(inlinePattern)) {
    text(source.slice(index, match.index))
    index = match.index + match[0].length
    if (match[1] !== undefined) {
      out.push(imageInline(match[1], match[2], match[3]))
    } else if (match[4] !== undefined) {
      out.push(imageInline(match[4], match[5]))
    } else if (match[6] !== undefined) {
      out.push({
        type: "link",
        href: cleanUrl(match[7]),
        content: withoutLinks(parseInline(match[6])),
      })
    } else {
      out.push({
        type: "link",
        href: cleanUrl(match[8]),
        content: [{ type: "text", text: match[8] }],
      })
    }
  }
  text(source.slice(index))
  return out
}

/** A link's label keeps its text and images; a bare URL in it stays text. */
function withoutLinks(content: LineblogInline[]) {
  const out: LineblogInline[] = []
  for (const item of content.flatMap((inline) =>
    inline.type === "link" ? inline.content : [inline],
  )) {
    const last = out.at(-1)
    if (item.type === "text" && last?.type === "text") last.text += item.text
    else out.push(item)
  }
  return out
}

function linkTarget(line: string, label: string) {
  const match = new RegExp(`\\[${label}\\]\\(([^)\\s]+)\\)`, "u").exec(line)
  return match ? cleanUrl(match[1]) : undefined
}

function youtubeId(url: string) {
  const match =
    /youtube\.com\/(?:embed\/|watch\?v=)([A-Za-z0-9_-]{11})/u.exec(url) ??
    /youtu\.be\/([A-Za-z0-9_-]{11})/u.exec(url)
  return match ? match[1] : null
}

/** The recovery tool's Obsidian callouts: a video or a link preview. */
function parseCallout(lines: string[]): LineblogBlock {
  const body = lines.map((line) => line.replace(/^>\s?/u, ""))
  const header = /^\[!(\w+)\]\s*(.*)$/u.exec(body[0])
  if (!header) {
    return { type: "quote", content: parseInline(body.join("\n").trim()) }
  }
  const rest = body.slice(1).filter((line) => line.trim() !== "")
  const linked = rest
    .map((line) => /^\[!\[([^\]]*)\]\(([^)\s]+)\)\]\(([^)\s]+)\)$/u.exec(line))
    .find(Boolean)
  const linkLine = rest.find((line) =>
    /查看网页存档|打开原始链接|播放原视频/u.test(line),
  )
  const archive = linkLine ? linkTarget(linkLine, "查看网页存档") : undefined
  if (header[1] === "video") {
    const watch =
      (linkLine && linkTarget(linkLine, "播放原视频")) ?? linked?.[3] ?? ""
    const videoId = youtubeId(watch)
    if (!videoId) throw new Error(`video callout without a YouTube id`)
    const block: LineblogBlock = {
      type: "youtube",
      videoId,
      watch: cleanUrl(watch),
    }
    const cover = linked ? mediaName(linked[2]) : null
    if (cover) block.cover = cover
    if (archive) block.archive = archive
    const coverSource = rest
      .map((line) => linkTarget(line, "封面来源"))
      .find(Boolean)
    if (coverSource) block.coverSource = coverSource
    return block
  }
  if (header[1] !== "link") throw new Error(`unknown callout: ${header[1]}`)
  const url =
    (linkLine && linkTarget(linkLine, "打开原始链接")) ?? linked?.[3] ?? ""
  if (!url) throw new Error("link callout without a target")
  const block: LineblogBlock = {
    type: "card",
    title: unescapeMarkdown(header[2].trim()),
    url,
  }
  if (archive) block.archive = archive
  if (linked) block.image = imageInline(linked[1], linked[2])
  const imageSource = rest
    .map((line) => linkTarget(line, "预览图来源"))
    .find(Boolean)
  if (imageSource && block.image) block.image.source = imageSource
  const description = rest
    .filter((line) => !line.startsWith("[") && !line.startsWith("!["))
    .join("\n")
    .trim()
  if (description) block.description = unescapeMarkdown(description)
  return block
}

function lastImage(content: LineblogInline[]) {
  const image = content.findLast((inline) => inline.type === "image")
  return image?.type === "image" ? image : null
}

const imageSourceLine = /^\[Image source\]\(([^)\s]+)\)(?:\]\(([^)\s]+)\))?/u

/**
 * A paragraph's inline content. A `[Image source](url)` line records where
 * the image before it (in this paragraph or the previous block) came from,
 * and is not content. Links that wrap several images lost their brackets in
 * the recovery: `[![image](a)` opens one, and `[Image source](url)](href)`
 * closes it on the last image.
 */
function parseParagraph(lines: string[], blocks: LineblogBlock[]) {
  const content: LineblogInline[] = []
  let pending: string[] = []
  const emit = () => {
    const text = pending.join("\n")
    pending = []
    if (!text) return
    if (content.length) content.push({ type: "text", text: "\n" })
    content.push(...parseInline(text))
  }
  for (const raw of lines) {
    const line = raw.replace(/^\[(!\[[^\]]*\]\([^)\s]+\))$/u, "$1")
    const source = imageSourceLine.exec(line)
    if (!source) {
      pending.push(line)
      continue
    }
    emit()
    const previous = blocks.at(-1)
    const image =
      lastImage(content) ??
      (previous?.type === "p" ? lastImage(previous.content) : null)
    if (!image) throw new Error(`image source without an image: ${source[1]}`)
    // A source that is the archived file itself adds nothing.
    if (!mediaName(source[1])) image.source = cleanUrl(source[1])
    if (source[2]) image.href = cleanUrl(source[2])
    const rest = line.slice(source[0].length)
    if (rest.trim()) pending.push(rest)
  }
  emit()
  // Adjacent text nodes merge, and the paragraph keeps no outer line breaks.
  const merged: LineblogInline[] = []
  for (const inline of content) {
    const last = merged.at(-1)
    if (inline.type === "text" && last?.type === "text")
      last.text += inline.text
    else merged.push(inline.type === "text" ? { ...inline } : inline)
  }
  const first = merged[0]
  if (first?.type === "text") first.text = first.text.replace(/^\n+/u, "")
  const final = merged.at(-1)
  if (final?.type === "text") final.text = final.text.replace(/\n+$/u, "")
  return merged.filter((inline) => inline.type !== "text" || inline.text)
}

/**
 * An archived YouTube embed is an iframe, the downloaded video and a cover
 * callout in a row; together they are one block.
 */
function mergeYoutube(blocks: LineblogBlock[]) {
  const out: LineblogBlock[] = []
  for (const block of blocks) {
    const previous = out.at(-1)
    if (
      previous?.type === "youtube" &&
      block.type === "video" &&
      !previous.video
    ) {
      previous.video = block.video
      continue
    }
    if (
      previous?.type === "youtube" &&
      block.type === "youtube" &&
      previous.videoId === block.videoId &&
      !previous.cover
    ) {
      if (block.cover) previous.cover = block.cover
      if (block.archive) previous.archive = block.archive
      if (block.coverSource) previous.coverSource = block.coverSource
      continue
    }
    out.push(block)
  }
  return out
}

function lineKey(line: string) {
  return unescapeMarkdown(line).replace(/\s+/gu, " ").trim()
}

export function parsePostFile(source: string): ParsedFile {
  const lines = source.replace(/^\uFEFF/u, "").split(/\r?\n/u)
  const titles: string[] = []
  const meta = new Map<string, string>()
  let index = 0
  for (; index < lines.length; index += 1) {
    const line = lines[index]
    const heading = /^# (.+)$/u.exec(line)
    const field = /^- ([A-Za-z ]+): (.*)$/u.exec(line)
    if (heading) titles.push(unescapeMarkdown(heading[1].trim()))
    else if (field) meta.set(field[1], field[2].trim())
    else if (line.trim() !== "") break
  }
  const published = meta.get("Published") ?? ""
  const originalUrl = meta.get("Original URL") ?? ""
  const snapshot = meta.get("Wayback snapshot") ?? ""
  if (!titles.length || !published || !originalUrl || !snapshot)
    throw new Error("post header is incomplete")

  const body = index
  const blocks: LineblogBlock[] = []
  // The block each source line belongs to, or -1 for blank lines.
  const owner = new Array<number>(lines.length).fill(-1)
  const own = (from: number, to: number) => {
    for (let at = from; at < to; at += 1) owner[at] = blocks.length - 1
  }
  let paragraph: string[] = []
  let paragraphStart = index
  const flush = () => {
    const lines = paragraph
    paragraph = []
    if (!lines.length) return
    if (lines.length === 1 && /^-{3,}$/u.test(lines[0].trim())) {
      blocks.push({ type: "hr" })
    } else {
      const content = parseParagraph(lines, blocks)
      if (content.length) blocks.push({ type: "p", content })
    }
    own(paragraphStart, index)
  }
  while (index < lines.length) {
    const line = lines[index]
    if (line.trim() === "") {
      flush()
      index += 1
      continue
    }
    if (/^> |^>$/u.test(line)) {
      flush()
      const start = index
      const quote: string[] = []
      while (index < lines.length && /^> |^>$/u.test(lines[index])) {
        quote.push(lines[index])
        index += 1
      }
      blocks.push(parseCallout(quote))
      own(start, index)
      continue
    }
    const iframe = /^<iframe\b[^>]*\bsrc="([^"]+)"[^>]*><\/iframe>$/u.exec(line)
    const video = /^<video\b[^>]*\bsrc="([^"]+)"[^>]*><\/video>$/u.exec(line)
    if (iframe || video) {
      flush()
      if (iframe) {
        const videoId = youtubeId(iframe[1])
        if (!videoId) throw new Error(`unsupported embed: ${iframe[1]}`)
        blocks.push({
          type: "youtube",
          videoId,
          watch: `https://www.youtube.com/watch?v=${videoId}`,
        })
      } else {
        const media = mediaName(video![1])
        if (!media) throw new Error(`remote video: ${video![1]}`)
        blocks.push({ type: "video", video: media })
      }
      own(index, index + 1)
      index += 1
      continue
    }
    if (line.startsWith("<")) throw new Error(`unsupported HTML: ${line}`)
    if (!paragraph.length) paragraphStart = index
    paragraph.push(line)
    index += 1
  }
  flush()
  return {
    title: titles[0],
    titles,
    published,
    originalUrl,
    snapshot,
    blocks,
    lines: lines.slice(body),
    owner: owner.slice(body),
  }
}

/**
 * The Chinese file repeats the Japanese lines in order and inserts its
 * translation lines among them. Matching the two line sequences (earliest
 * matches first) leaves the insertions; each run of them becomes a
 * translation paragraph of the block holding the last matched line.
 * Lines both files share but place differently are not translations.
 */
export function alignTranslation(original: ParsedFile, translated: ParsedFile) {
  const pick = (file: ParsedFile) =>
    file.lines.flatMap((line, at) =>
      line.trim() === "" ? [] : [{ key: lineKey(line), at }],
    )
  const a = pick(original)
  const b = pick(translated)
  const known = new Set(a.map((line) => line.key))
  const table = Array.from(
    { length: a.length + 1 },
    () => new Uint16Array(b.length + 1),
  )
  for (let i = a.length - 1; i >= 0; i -= 1) {
    for (let j = b.length - 1; j >= 0; j -= 1) {
      table[i][j] =
        a[i].key === b[j].key
          ? table[i + 1][j + 1] + 1
          : Math.max(table[i + 1][j], table[i][j + 1])
    }
  }
  const additions = new Map<number, string[][]>()
  let current = -1
  let previousAt = -2
  let i = 0
  let j = 0
  while (j < b.length) {
    if (
      i < a.length &&
      a[i].key === b[j].key &&
      table[i][j] === table[i + 1][j + 1] + 1
    ) {
      current = original.owner[a[i].at]
      i += 1
      j += 1
      continue
    }
    if (i < a.length && table[i][j + 1] !== table[i][j]) {
      i += 1
      continue
    }
    const line = translated.lines[b[j].at]
    if (!known.has(b[j].key) && !mediaReference.test(line)) {
      if (current < 0 || /^(?:> |>$|<[a-z]|\[Image source\])/u.test(line))
        throw new Error(`unexpected translated line: ${b[j].key.slice(0, 40)}`)
      const list = additions.get(current) ?? []
      if (b[j].at === previousAt + 1 && list.length) list.at(-1)!.push(line)
      else list.push([line])
      additions.set(current, list)
      previousAt = b[j].at
    }
    j += 1
  }
  return new Map(
    [...additions].map(([at, paragraphs]) => [
      at,
      paragraphs.map((lines) => parseInline(lines.join("\n").trim())),
    ]),
  )
}

function japanIso(value: string) {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})\+0900$/u.exec(
    value,
  )
  if (!match) throw new Error(`unexpected publication time: ${value}`)
  const [, y, mo, d, h, mi, s] = match
  return new Date(`${y}-${mo}-${d}T${h}:${mi}:${s}+09:00`).toISOString()
}

/** One post from its Japanese file and, when present, the Chinese one. */
export function buildPost(japanese: string, chinese?: string): LineblogPost {
  const original = parsePostFile(japanese)
  const id = /^https?:\/\/lineblog\.me\/kano\/archives\/(\d+)\.html$/u.exec(
    original.originalUrl,
  )?.[1]
  if (!id) throw new Error(`unexpected original URL: ${original.originalUrl}`)
  if (!/^\d{14}$/u.test(original.snapshot))
    throw new Error(`unexpected snapshot: ${original.snapshot}`)
  const post: LineblogPost = {
    id,
    published: japanIso(original.published),
    title: original.title,
    url: `https://lineblog.me/kano/archives/${id}.html`,
    archive: `https://web.archive.org/web/${original.snapshot}/${original.originalUrl}`,
    blocks: mergeYoutube(original.blocks),
  }
  if (chinese) {
    const translated = parsePostFile(chinese)
    if (translated.titles.length > 1) post.titleZh = translated.titles.at(-1)
    const additions = alignTranslation(original, translated)
    // Translations follow the original blocks; merging never moves a
    // paragraph, so its index in the merged list is found by identity.
    for (const [at, content] of additions) {
      const block = original.blocks[at]
      if (block.type === "p") block.zh = content
      else throw new Error("translation follows a non-paragraph block")
    }
  }
  return post
}

export function sha256ForContent(content: Buffer) {
  return crypto.createHash("sha256").update(content).digest("hex")
}

/** Each downloaded video and the watch address it was named after. */
export function videoSources(posts: LineblogPost[]) {
  const sources = new Map<string, string>()
  for (const post of posts) {
    for (const block of post.blocks) {
      if (block.type === "youtube" && block.video)
        sources.set(
          block.video,
          `https://www.youtube.com/watch?v=${block.videoId}`,
        )
    }
  }
  return sources
}

/**
 * An image the archive kept only as a remote address takes its later
 * recovered files, and an embedded tweet's pictures follow its paragraph.
 * Each recovered image records the address its bytes came from.
 */
export function withRecoveredImages(
  posts: LineblogPost[],
  recovered: RecoveredImage[],
  tweets: TweetPictures[] = [],
) {
  const byAddress = new Map(recovered.map((item) => [item.address, item]))
  const byLink = new Map(tweets.map((item) => [item.link, item]))
  const recover = (inline: LineblogInline): LineblogInline[] => {
    if (inline.type === "link")
      return [{ ...inline, content: inline.content.flatMap(recover) }]
    const item =
      inline.type === "image" && inline.remote
        ? byAddress.get(inline.remote)
        : undefined
    if (inline.type !== "image" || !item) return [inline]
    const { remote: _remote, ...image } = inline
    return item.files.map((file) => ({
      ...image,
      media: file.media,
      source: file.source,
      ...(item.standIn ? { recovery: "stand-in" as const } : {}),
    }))
  }
  const pictures = (content: LineblogInline[]): LineblogBlock[] =>
    content.flatMap((inline) => {
      const item = inline.type === "link" ? byLink.get(inline.href) : undefined
      if (!item) return []
      return [
        {
          type: "p",
          content: item.files.map((file) => ({
            type: "image",
            alt: "image",
            media: file.media,
            source: file.source,
            recovery: "tweet",
          })),
        },
      ]
    })
  return posts.map((post) => ({
    ...post,
    blocks: post.blocks.flatMap((block): LineblogBlock[] => {
      if (block.type === "p" || block.type === "quote") {
        const content = block.content.flatMap(recover)
        return [{ ...block, content }, ...pictures(content)]
      }
      if (block.type === "card" && block.image) {
        const [image] = recover(block.image)
        return [{ ...block, image: image as LineblogImage }]
      }
      return [block]
    }),
  }))
}

export function referencedMedia(posts: LineblogPost[]) {
  const names = new Set<string>()
  const image = (inline: LineblogInline) => {
    if (inline.type === "image" && inline.media) names.add(inline.media)
    if (inline.type === "link") inline.content.forEach(image)
  }
  for (const post of posts) {
    for (const block of post.blocks) {
      if (block.type === "p" || block.type === "quote")
        block.content.forEach(image)
      if (block.type === "video") names.add(block.video)
      if (block.type === "youtube") {
        if (block.video) names.add(block.video)
        if (block.cover) names.add(block.cover)
      }
      if (block.type === "card" && block.image) image(block.image)
    }
  }
  return [...names].sort()
}

// ---------------------------------------------------------------------------
// Media
// ---------------------------------------------------------------------------

function sniffMediaType(head: Buffer) {
  if (
    head.length >= 3 &&
    head[0] === 0xff &&
    head[1] === 0xd8 &&
    head[2] === 0xff
  )
    return "image/jpeg"
  if (
    head.length >= 8 &&
    head
      .subarray(0, 8)
      .equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]))
  )
    return "image/png"
  if (head.length >= 12 && head.subarray(4, 8).toString("ascii") === "ftyp")
    return "video/mp4"
  return null
}

const mimeByExtension: Record<string, string> = {
  jpg: "image/jpeg",
  png: "image/png",
  mp4: "video/mp4",
}

/** EXIF orientations 5–8 rotate the stored frame by a quarter turn. */
function exifTurnsQuarter(segment: Buffer) {
  if (segment.subarray(0, 6).toString("binary") !== "Exif\0\0") return false
  const tiff = segment.subarray(6)
  const little = tiff.subarray(0, 2).toString("ascii") === "II"
  const u16 = (at: number) =>
    little ? tiff.readUInt16LE(at) : tiff.readUInt16BE(at)
  const u32 = (at: number) =>
    little ? tiff.readUInt32LE(at) : tiff.readUInt32BE(at)
  try {
    const ifd = u32(4)
    const count = u16(ifd)
    for (let entry = 0; entry < count; entry += 1) {
      const at = ifd + 2 + entry * 12
      if (u16(at) === 0x0112) return u16(at + 8) >= 5 && u16(at + 8) <= 8
    }
  } catch {
    return false
  }
  return false
}

/** Displayed pixel size of a JPEG or PNG, or null when it cannot be read. */
export function imageSize(body: Buffer) {
  if (sniffMediaType(body) === "image/png" && body.length >= 24) {
    return { width: body.readUInt32BE(16), height: body.readUInt32BE(20) }
  }
  if (sniffMediaType(body) !== "image/jpeg") return null
  let quarter = false
  let at = 2
  while (at + 4 <= body.length) {
    if (body[at] !== 0xff) return null
    const marker = body[at + 1]
    if (marker === 0xff) {
      at += 1
      continue
    }
    if (
      marker === 0xd8 ||
      marker === 0x01 ||
      (marker >= 0xd0 && marker <= 0xd7)
    ) {
      at += 2
      continue
    }
    const length = body.readUInt16BE(at + 2)
    if (marker === 0xe1)
      quarter ||= exifTurnsQuarter(body.subarray(at + 4, at + 2 + length))
    if (
      marker >= 0xc0 &&
      marker <= 0xcf &&
      marker !== 0xc4 &&
      marker !== 0xc8 &&
      marker !== 0xcc
    ) {
      const height = body.readUInt16BE(at + 5)
      const width = body.readUInt16BE(at + 7)
      return quarter ? { width: height, height: width } : { width, height }
    }
    at += 2 + length
  }
  return null
}

async function hashFile(file: string) {
  const hash = crypto.createHash("sha256")
  await pipeline(createReadStream(file), hash)
  return hash.digest("hex")
}

async function isFile(file: string) {
  try {
    return (await fs.lstat(file)).isFile()
  } catch {
    return false
  }
}

/** Reads one member of a gzip-compressed tar stream into `destination`. */
async function extractTarMember(
  parts: string[],
  member: string,
  destination: string,
) {
  let found = false
  let header = Buffer.alloc(0)
  let remaining = 0
  let padding = 0
  let output: import("node:fs").WriteStream | null = null
  let ended = false
  const reader = new Writable({
    write(chunk: Buffer, _encoding, done) {
      let data = chunk
      const step = async () => {
        while (data.length && !ended) {
          if (remaining > 0) {
            const take = data.subarray(0, Math.min(remaining, data.length))
            data = data.subarray(take.length)
            remaining -= take.length
            if (output && !output.write(take))
              await new Promise<void>((resolve) =>
                output!.once("drain", () => resolve()),
              )
            if (remaining === 0 && output) {
              await new Promise<void>((resolve, reject) =>
                output!.end((error?: Error | null) =>
                  error ? reject(error) : resolve(),
                ),
              )
              output = null
            }
            continue
          }
          if (padding > 0) {
            const skip = Math.min(padding, data.length)
            data = data.subarray(skip)
            padding -= skip
            continue
          }
          const need = 512 - header.length
          header = Buffer.concat([header, data.subarray(0, need)])
          data = data.subarray(need)
          if (header.length < 512) continue
          const block = header
          header = Buffer.alloc(0)
          if (block.every((byte) => byte === 0)) {
            ended = true
            break
          }
          const name = block
            .subarray(0, 100)
            .toString("utf8")
            .replace(/\0.*$/su, "")
          const size = Number.parseInt(
            block
              .subarray(124, 136)
              .toString("ascii")
              .replace(/\0.*$/su, "")
              .trim() || "0",
            8,
          )
          const type = String.fromCharCode(block[156] || 0x30)
          if (!Number.isSafeInteger(size) || size < 0 || size > MAX_VIDEO_BYTES)
            throw new Error(`archive member too large: ${name}`)
          remaining = size
          padding = (512 - (size % 512)) % 512
          if (name === member && (type === "0" || type === "\0")) {
            if (found) throw new Error(`archive repeats ${member}`)
            found = true
            output = createWriteStream(destination, { flags: "wx" })
            if (size === 0) {
              output.end()
              output = null
            }
          }
        }
      }
      step().then(() => done(), done)
    },
  })
  const joined = Readable.from(
    (async function* () {
      for (const part of parts) yield* createReadStream(part)
    })(),
  )
  await pipeline(joined, zlib.createGunzip(), reader)
  if (output) throw new Error(`archive ends inside ${member}`)
  if (!found) throw new Error(`archive does not contain ${member}`)
}

interface PreparedMedia {
  item: LineblogMedia
  /** File to copy, or a restored temporary file to move into place. */
  file: string
  temporary: boolean
}

async function locateMedia(
  root: string,
  name: string,
  scratch: string,
): Promise<{ file: string; temporary: boolean }> {
  const direct = path.join(root, "media", name)
  if (await isFile(direct)) return { file: direct, temporary: false }
  const partsDir = path.join(root, "media-parts")
  let entries: string[] = []
  try {
    entries = await fs.readdir(partsDir)
  } catch {
    entries = []
  }
  const prefix = `${name}.tar.gz.part-`
  const parts = entries
    .filter(
      (entry) =>
        entry.startsWith(prefix) && /^\d{3}$/u.test(entry.slice(prefix.length)),
    )
    .sort()
  if (!parts.length) throw new Error(`missing archive media: ${name}`)
  parts.forEach((part, index) => {
    if (part !== `${prefix}${String(index + 1).padStart(3, "0")}`)
      throw new Error(`incomplete split archive: ${name}`)
  })
  const restored = path.join(scratch, name)
  await extractTarMember(
    parts.map((part) => path.join(partsDir, part)),
    name,
    restored,
  )
  return { file: restored, temporary: true }
}

async function prepareMedia(
  root: string,
  names: string[],
  videoSources: Map<string, string>,
  scratch: string,
  log: (message: string) => void,
) {
  const prepared: PreparedMedia[] = []
  for (const name of names) {
    const match = mediaNamePattern.exec(name)
    if (!match) throw new Error(`unsafe archive media name: ${name}`)
    const located = await locateMedia(root, name, scratch)
    const stat = await fs.stat(located.file)
    const mimeType = mimeByExtension[match[2]]
    const limit = mimeType === "video/mp4" ? MAX_VIDEO_BYTES : MAX_IMAGE_BYTES
    if (!stat.isFile() || stat.size === 0 || stat.size > limit)
      throw new Error(`archive media size out of range: ${name}`)
    const handle = await fs.open(located.file)
    const head = Buffer.alloc(
      Math.min(stat.size, mimeType === "video/mp4" ? 64 : 256 * 1024),
    )
    await handle.read(head, 0, head.length, 0)
    await handle.close()
    if (sniffMediaType(head) !== mimeType)
      throw new Error(`archive media type mismatch: ${name}`)
    const sha256 = await hashFile(located.file)
    const source = videoSources.get(name)
    const identity =
      mimeType === "video/mp4"
        ? source && sha256ForContent(Buffer.from(source))
        : sha256
    if (!identity?.startsWith(match[1]))
      throw new Error(`archive media identity mismatch: ${name}`)
    const item: LineblogMedia = {
      name,
      mimeType,
      byteSize: stat.size,
      sha256,
    }
    if (mimeType !== "video/mp4") {
      const size = imageSize(await fs.readFile(located.file))
      if (!size) throw new Error(`unreadable image size: ${name}`)
      item.width = size.width
      item.height = size.height
    }
    if (located.temporary) log(`Restored ${name} from its split archive.`)
    prepared.push({ item, ...located })
  }
  return prepared
}

// ---------------------------------------------------------------------------
// Packaging
// ---------------------------------------------------------------------------

async function readPostFiles(root: string, folder: string) {
  const directory = path.join(root, folder)
  const files = new Map<string, { version: number; file: string }>()
  for (const entry of await fs.readdir(directory)) {
    const match = /^(\d{8})_([0-9A-F]{16})_v(\d{2})\.md$/u.exec(entry)
    if (!match) continue
    const key = match[2]
    const version = Number(match[3])
    const known = files.get(key)
    if (!known || known.version < version)
      files.set(key, { version, file: path.join(directory, entry) })
  }
  return files
}

async function readPost(file: string) {
  const stat = await fs.stat(file)
  if (!stat.isFile() || stat.size > MAX_POST_BYTES)
    throw new Error(`post size out of range: ${path.basename(file)}`)
  return fs.readFile(file, "utf8")
}

/** Parses every post; the latest retained version of an article wins. */
export async function readLineblogArchive(directory: string) {
  const root = await fs.realpath(directory)
  const japanese = await readPostFiles(root, "BLOG")
  let chinese = new Map<string, { version: number; file: string }>()
  try {
    chinese = await readPostFiles(root, "BLOG_zh-cn")
  } catch {
    chinese = new Map()
  }
  if (!japanese.size) throw new Error("the archive holds no posts")
  const posts: LineblogPost[] = []
  for (const [key, { file, version }] of japanese) {
    const translation = chinese.get(key)
    try {
      posts.push(
        buildPost(
          await readPost(file),
          translation?.version === version
            ? await readPost(translation.file)
            : undefined,
        ),
      )
    } catch (error) {
      throw new Error(
        `${path.basename(file)}: ${error instanceof Error ? error.message : String(error)}`,
      )
    }
  }
  posts.sort(
    (a, b) =>
      b.published.localeCompare(a.published) || b.id.localeCompare(a.id),
  )
  if (new Set(posts.map((post) => post.id)).size !== posts.length)
    throw new Error("two posts share an article number")
  return { root, posts }
}

async function writeJson(file: string, value: unknown) {
  const temporary = `${file}.tmp`
  await fs.writeFile(temporary, `${JSON.stringify(value, null, 2)}\n`)
  await fs.rename(temporary, file)
}

/**
 * Validates every post and media file before writing, then writes the post
 * and media catalogs into the frontend and the media into `mediaDir`. The
 * blog's header and profile art (`designArt`) and the later recovered
 * pictures (`recovered`, `tweets`) are archive media too.
 */
export async function packageLineblog(
  directory: string,
  {
    contentDir = path.join(projectRoot, "src/lineblog"),
    mediaDir = path.join(projectRoot, "data/lineblog"),
    designArt = designMedia(),
    recovered = recoveredImages,
    tweets = tweetPictures,
    log = (_message: string) => {},
  }: {
    contentDir?: string
    mediaDir?: string
    designArt?: string[]
    recovered?: RecoveredImage[]
    tweets?: TweetPictures[]
    log?: (message: string) => void
  } = {},
) {
  const archive = await readLineblogArchive(directory)
  const { root } = archive
  const posts = withRecoveredImages(archive.posts, recovered, tweets)
  const names = [...new Set([...referencedMedia(posts), ...designArt])].sort()
  await fs.mkdir(mediaDir, { recursive: true })
  const scratch = await fs.mkdtemp(path.join(mediaDir, ".restore-"))
  try {
    const prepared = await prepareMedia(
      root,
      names,
      videoSources(posts),
      scratch,
      log,
    )
    for (const { item, file, temporary } of prepared) {
      const target = path.join(mediaDir, item.name)
      if (await isFile(target)) {
        const stat = await fs.stat(target)
        if (
          stat.size === item.byteSize &&
          (await hashFile(target)) === item.sha256
        )
          continue
      }
      const staged = path.join(scratch, `${item.name}.staged`)
      if (temporary) await fs.rename(file, staged)
      else await fs.copyFile(file, staged)
      await fs.rename(staged, target)
    }
    await fs.mkdir(contentDir, { recursive: true })
    await writeJson(path.join(contentDir, "posts.json"), posts)
    await writeJson(
      path.join(contentDir, "media-catalog.json"),
      prepared.map(({ item }) => item),
    )
    return {
      posts: posts.length,
      media: prepared.length,
      bytes: prepared.reduce((sum, { item }) => sum + item.byteSize, 0),
    }
  } finally {
    await fs.rm(scratch, { recursive: true, force: true })
  }
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const args = process.argv.slice(2)
  if (args.length !== 2 || args[0] !== "--from") {
    console.error(
      "Usage: npm run lineblog:package -- --from <archive-directory>",
    )
    process.exitCode = 1
  } else {
    try {
      const result = await packageLineblog(args[1], { log: console.log })
      console.log(
        `Packaged ${result.posts} posts and ${result.media} media files (${(result.bytes / 1024 / 1024).toFixed(1)} MiB).`,
      )
    } catch (error) {
      console.error(
        error instanceof Error && "code" in error && error.code
          ? "Unable to read the local LINE BLOG archive."
          : error instanceof Error
            ? error.message
            : String(error),
      )
      process.exitCode = 1
    }
  }
}
