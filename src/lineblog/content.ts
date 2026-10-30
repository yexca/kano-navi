import catalog from "./media-catalog.json" with { type: "json" }
import archive from "./posts.json" with { type: "json" }
import type {
  LineblogBlock,
  LineblogImage,
  LineblogInline,
  LineblogMedia,
  LineblogPost,
} from "./types.ts"

/** Newest first, as the blog listed them. */
export const posts = archive as LineblogPost[]
export const media = new Map(
  (catalog as LineblogMedia[]).map((item) => [item.name, item]),
)

export const PAGE_SIZE = 3
export const ORIGINAL_BLOG = "https://lineblog.me/kano/"
/** The blog's menu bar linked Kano's official site. */
export const OFFICIAL_SITE = "https://whv-amusic.com/kano/"

/** Recovered media stay in the server's data directory. */
export function mediaUrl(name: string) {
  return `/media/lineblog/${name}`
}

/** Pictographs the blog drew inline with the text (15 and 78 px). */
export function isEmoji(name: string | undefined) {
  const width = name ? media.get(name)?.width : undefined
  return width !== undefined && width <= 80
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

function inlineImages(content: LineblogInline[]): LineblogImage[] {
  return content.flatMap((inline) =>
    inline.type === "image"
      ? [inline]
      : inline.type === "link"
        ? inlineImages(inline.content)
        : [],
  )
}

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

/** The first archived picture or video cover, for the recent-posts list. */
export function postThumbnail(post: LineblogPost) {
  for (const block of post.blocks) {
    if (block.type === "youtube" && block.cover) return block.cover
    if (block.type === "p") {
      const image = inlineImages(block.content).find(
        (item) => item.media && !isEmoji(item.media),
      )
      if (image?.media) return image.media
    }
  }
  return null
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
  article: LineblogPost | null
  /** Unknown article numbers show a not-found note. */
  missingArticle: boolean
  month: string | null
  page: number
  pageCount: number
  shown: LineblogPost[]
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
export function neighbours(post: LineblogPost) {
  const at = posts.indexOf(post)
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
