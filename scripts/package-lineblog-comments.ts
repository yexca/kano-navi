import fs from "node:fs/promises"
import path from "node:path"
import { fileURLToPath } from "node:url"

import type { LineblogComment, LineblogSummary } from "../src/lineblog/types.ts"

const MAX_CAPTURE_BYTES = 4 * 1024 * 1024

const projectRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
)

/** The comments one archived article page lists. */
export interface CapturedComments {
  article: string
  comments: LineblogComment[]
}

const canonicalPattern =
  /<link rel="canonical" href="https?:\/\/lineblog\.me\/kano\/archives\/(\d+)\.html"/u
const listPattern = /<ol id="ld_blog_article_comment_entries">([\s\S]*?)<\/ol>/u
const setPattern = /<li class="comment-set (?:odd|even)">([\s\S]*?)<\/ul>/gu
const ratingPattern =
  /<img src="https?:\/\/parts\.(?:blog\.livedoor\.jp|lineblog\.me)\/img\/(star|face)\/([1-5])\.gif"[^>]*class="commentEvaluation"[^>]*>/gu
const emojiPattern =
  /<img src="https?:\/\/parts\.blog\.livedoor\.jp\/img\/emoji\/\d+\/([a-z_]+)\.png"[^>]*>/gu

/**
 * The old comment form's picture emoji that archived comments use, as the
 * nearest Unicode emoji. A capture with any other picture stops the run.
 */
const livedoorEmoji: Record<string, string> = {
  ic_sun: "☀️",
  ic_b_heart: "💓",
}

const namedEntities: Record<string, string> = {
  amp: "&",
  lt: "<",
  gt: ">",
  quot: '"',
  apos: "'",
  nbsp: " ",
}

function decodeEntities(text: string) {
  return text.replace(/&(#x[0-9a-f]+|#\d+|[a-z]+);/giu, (entity, name) => {
    const key = String(name)
    if (key.startsWith("#x") || key.startsWith("#X"))
      return String.fromCodePoint(Number.parseInt(key.slice(2), 16))
    if (key.startsWith("#"))
      return String.fromCodePoint(Number.parseInt(key.slice(1), 10))
    const named = namedEntities[key]
    if (named === undefined) throw new Error(`unknown entity ${entity}`)
    return named
  })
}

function field(set: string, name: string) {
  const match = new RegExp(
    `<li class="comment-${name}">([\\s\\S]*?)</li>`,
    "u",
  ).exec(set)
  if (!match) throw new Error(`a comment without its ${name}`)
  return match[1]
}

/** The Japan-time minute the blog showed (2018/05/10 17:46) as an instant. */
function japanMinute(text: string) {
  const match = /^(\d{4})\/(\d{2})\/(\d{2}) (\d{2}):(\d{2})$/u.exec(text.trim())
  if (!match) throw new Error(`unexpected comment date: ${text.trim()}`)
  const [, y, mo, d, h, mi] = match
  return new Date(`${y}-${mo}-${d}T${h}:${mi}:00+09:00`).toISOString()
}

/**
 * The text as the page showed it: `<br>` breaks lines, other whitespace runs
 * collapse, and each line loses its outer spaces. Full-width spaces stay, as
 * HTML keeps them, except at the very end.
 */
function plainText(html: string) {
  const text = html
    .replace(/\r/gu, "")
    .replace(/<br\s*\/?>[ \t]*\n?/giu, "\u0000")
    .replace(/[ \t\n]+/gu, " ")
  if (/<[a-z/!]/iu.test(text))
    throw new Error(`unexpected markup in a comment: ${text.slice(0, 80)}`)
  return decodeEntities(text)
    .split("\u0000")
    .map((line) => line.replace(/^ +| +$/gu, ""))
    .join("\n")
    .replace(/^\n+|\s+$/gu, "")
}

function parseComment(set: string): LineblogComment {
  const author = /^\s*(\d+)\.([\s\S]*)$/u.exec(field(set, "author"))
  if (!author) throw new Error("a comment without its number")
  // Only the link's text is kept: the commenter's blog address is dropped.
  const name = plainText(author[2].replace(/<a\s[^>]*>|<\/a>/giu, "")).trim()
  if (!name) throw new Error(`comment ${author[1]} has no name`)
  let rating: LineblogComment["rating"]
  const body = plainText(
    field(set, "body")
      .replace(ratingPattern, (_image, icon: "star" | "face", value) => {
        rating = { icon, value: Number(value) }
        return ""
      })
      .replace(emojiPattern, (_image, emoji: string) => {
        const text = livedoorEmoji[emoji]
        if (!text) throw new Error(`unknown comment emoji ${emoji}`)
        return text
      }),
  )
  const comment: LineblogComment = {
    number: Number(author[1]),
    author: name,
    posted: japanMinute(field(set, "date")),
    body,
  }
  if (rating) comment.rating = rating
  return comment
}

/**
 * The article and comments of one archived article page, newest first; null
 * for any other page. A page without a comment list has no comments.
 */
export function parseCapture(html: string): CapturedComments | null {
  const article = canonicalPattern.exec(html)?.[1]
  if (!article) return null
  const list = listPattern.exec(html)?.[1] ?? ""
  const comments = [...list.matchAll(setPattern)].map((match) =>
    parseComment(match[1]),
  )
  comments.sort((a, b) => b.number - a.number)
  if (
    new Set(comments.map((comment) => comment.number)).size !== comments.length
  )
    throw new Error(`article ${article} lists a comment number twice`)
  return { article, comments }
}

/**
 * Each article's comments from the capture listing the most; equal counts
 * keep the first capture in the given order. Articles without comments are
 * left out.
 */
export function pickComments(captures: CapturedComments[]) {
  const best = new Map<string, LineblogComment[]>()
  for (const { article, comments } of captures) {
    const known = best.get(article)
    if (comments.length && (!known || comments.length > known.length))
      best.set(article, comments)
  }
  return new Map([...best].sort(([a], [b]) => Number(a) - Number(b)))
}

/** Every archived article page in a directory, read in file-name order. */
export async function readCaptures(directory: string) {
  const root = path.resolve(directory)
  const names = (await fs.readdir(root))
    .filter((name) => name.endsWith(".html"))
    .sort()
  const captures: CapturedComments[] = []
  for (const name of names) {
    const file = path.join(root, name)
    const stat = await fs.stat(file)
    if (!stat.isFile() || stat.size > MAX_CAPTURE_BYTES) continue
    try {
      const capture = parseCapture(await fs.readFile(file, "utf8"))
      if (capture) captures.push(capture)
    } catch (error) {
      throw new Error(
        `${name}: ${error instanceof Error ? error.message : String(error)}`,
      )
    }
  }
  return captures
}

async function writeJson(file: string, value: unknown) {
  const temporary = `${file}.tmp`
  await fs.writeFile(temporary, `${JSON.stringify(value, null, 2)}\n`)
  await fs.rename(temporary, file)
}

/**
 * Writes `comments/<id>.json` for each post of the index with archived
 * comments and removes the files of posts that have none.
 */
export async function packageLineblogComments(
  directory: string,
  {
    contentDir = path.join(projectRoot, "src/lineblog"),
  }: { contentDir?: string } = {},
) {
  const index = JSON.parse(
    await fs.readFile(path.join(contentDir, "post-index.json"), "utf8"),
  ) as LineblogSummary[]
  const known = new Set(index.map((post) => post.id))
  const picked = pickComments(await readCaptures(directory))
  const commentsDir = path.join(contentDir, "comments")
  await fs.mkdir(commentsDir, { recursive: true })
  const files = new Set<string>()
  let count = 0
  for (const [article, comments] of picked) {
    if (!known.has(article)) continue
    const name = `${article}.json`
    files.add(name)
    count += comments.length
    await writeJson(path.join(commentsDir, name), comments)
  }
  for (const entry of await fs.readdir(commentsDir)) {
    if (/^\d+\.json$/u.test(entry) && !files.has(entry))
      await fs.rm(path.join(commentsDir, entry))
  }
  return { posts: files.size, comments: count }
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const args = process.argv.slice(2)
  if (args.length !== 2 || args[0] !== "--from") {
    console.error(
      "Usage: npm run lineblog:comments -- --from <captures-directory>",
    )
    process.exitCode = 1
  } else {
    try {
      const result = await packageLineblogComments(args[1])
      console.log(`Wrote ${result.comments} comments on ${result.posts} posts.`)
    } catch (error) {
      console.error(
        error instanceof Error && "code" in error && error.code
          ? "Unable to read the captures directory."
          : error instanceof Error
            ? error.message
            : String(error),
      )
      process.exitCode = 1
    }
  }
}
