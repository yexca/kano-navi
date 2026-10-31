import index from "./post-index.json" with { type: "json" }
import { inlineImages, isEmojiMedia } from "./post-media.ts"
import type {
  LineblogBlock,
  LineblogInline,
  LineblogMedia,
  LineblogPost,
  LineblogPostFile,
  LineblogSummary,
} from "./types.ts"

/** Every post's index entry, newest first, as the blog listed them. */
export const posts = index as LineblogSummary[]

/** Posts whose files have loaded, and the catalog entries they brought. */
const loadedPosts = new Map<string, LineblogPost>()
export const media = new Map<string, LineblogMedia>()

export function loadedPost(id: string) {
  return loadedPosts.get(id)
}

/** Keeps a loaded post file; its media then have their sizes and types. */
export function addPostFile(file: LineblogPostFile) {
  for (const item of file.media) media.set(item.name, item)
  loadedPosts.set(file.post.id, file.post)
  return file.post
}

export const PAGE_SIZE = 3
export const ORIGINAL_BLOG = "https://lineblog.me/kano/"

/** Recovered media stay in the server's data directory. */
export function mediaUrl(name: string) {
  return `/media/lineblog/${name}`
}

/** Pictographs the blog drew inline with the text, among loaded media. */
export function isEmoji(name: string | undefined) {
  return isEmojiMedia(name ? media.get(name) : undefined)
}

const japanParts = new Intl.DateTimeFormat("en", {
  timeZone: "Asia/Tokyo",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
})

function japanFields(iso: string) {
  const parts = Object.fromEntries(
    japanParts
      .formatToParts(new Date(iso))
      .map((part) => [part.type, part.value]),
  )
  return parts as Record<"year" | "month" | "day" | "hour" | "minute", string>
}

/** The blog's own date line, in Japan time: 2016/07/21 22:40. */
export function postDate(iso: string) {
  const { year, month, day, hour, minute } = japanFields(iso)
  return `${year}/${month}/${day} ${hour}:${minute}`
}

/** The Japan-time month a post belongs to: 2016-07. */
export function postMonth(iso: string) {
  const { year, month } = japanFields(iso)
  return `${year}-${month}`
}

export const postsById = new Map(posts.map((post) => [post.id, post]))

/** Years, newest first, each with its months and their post counts. */
export const archiveMonths = posts.reduce<
  { year: string; months: { month: string; count: number }[] }[]
>((years, post) => {
  const month = postMonth(post.published)
  const year = month.slice(0, 4)
  let entry = years.find((item) => item.year === year)
  if (!entry) {
    entry = { year, months: [] }
    years.push(entry)
  }
  const known = entry.months.find((item) => item.month === month)
  if (known) known.count += 1
  else entry.months.push({ month, count: 1 })
  return years
}, [])

/** Archived pictures a reader can open, in reading order. */
export function postImages(blocks: LineblogBlock[]) {
  return blocks
    .flatMap((block) =>
      block.type === "p" || block.type === "quote"
        ? inlineImages(block.content)
        : [],
    )
    .filter((image) => image.media && !isEmoji(image.media))
}

function inlineText(content: LineblogInline[]): string {
  return content
    .map((inline) =>
      inline.type === "text"
        ? inline.text
        : inline.type === "link"
          ? inlineText(inline.content)
          : "",
    )
    .join("")
}

/**
 * Text cut to a length on one line, as the smartphone template cut a
 * profile or an article's description: whitespace runs become one space and
 * a cut ends in "...".
 */
export function shortText(text: string, length: number) {
  const characters = Array.from(text.replace(/\s+/gu, " ").trim())
  return characters.length > length
    ? `${characters.slice(0, length).join("")}...`
    : characters.join("")
}

/** The start of a post's own words, for its entry in an article list. */
export function postExcerpt(blocks: LineblogBlock[], length = 80) {
  const text = blocks
    .flatMap((block) =>
      block.type === "p" || block.type === "quote"
        ? [inlineText(block.content)]
        : [],
    )
    .join(" ")
  return shortText(text, length)
}

/** Readable alt text; the recovery kept file names and "image" as alts. */
export function meaningfulAlt(alt: string) {
  const value = alt.trim()
  return value && value !== "image" && !/\.(?:jpe?g|png|gif)$/iu.test(value)
    ? value
    : null
}

/** Links between the blog's own articles stay on this page. */
export function articleHref(href: string) {
  const match = /^https?:\/\/lineblog\.me\/kano\/archives\/(\d+)\.html$/u.exec(
    href,
  )
  return match && postsById.has(match[1])
    ? `/lineblog?article=${match[1]}`
    : null
}

export interface LineblogView {
  article: LineblogSummary | null
  /** Unknown article numbers show a not-found note. */
  missingArticle: boolean
  month: string | null
  page: number
  pageCount: number
  shown: LineblogSummary[]
}

/** What /lineblog shows for its query: an article, a month, or a page. */
export function lineblogView(search: string): LineblogView {
  const query = new URLSearchParams(search)
  const pageCount = Math.ceil(posts.length / PAGE_SIZE)
  const id = query.get("article")
  if (id !== null) {
    const article = postsById.get(id) ?? null
    return {
      article,
      missingArticle: !article,
      month: null,
      page: 1,
      pageCount,
      shown: article ? [article] : [],
    }
  }
  const month = query.get("month")
  if (month !== null && /^\d{4}-\d{2}$/u.test(month)) {
    return {
      article: null,
      missingArticle: false,
      month,
      page: 1,
      pageCount,
      shown: posts.filter((post) => postMonth(post.published) === month),
    }
  }
  const requested = Number(query.get("page") ?? "1")
  const page =
    Number.isInteger(requested) && requested >= 1 && requested <= pageCount
      ? requested
      : 1
  return {
    article: null,
    missingArticle: false,
    month: null,
    page,
    pageCount,
    shown: posts.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE),
  }
}

/** The newer and older neighbours of an article. */
export function neighbours(id: string) {
  const at = posts.findIndex((post) => post.id === id)
  return { newer: posts[at - 1] ?? null, older: posts[at + 1] ?? null }
}

/** Page numbers around the current one, as the blog's pager showed them. */
export function pagerPages(page: number, pageCount: number) {
  const first = Math.max(1, Math.min(page - 2, pageCount - 4))
  return Array.from(
    { length: Math.min(5, pageCount) },
    (_, index) => first + index,
  )
}
