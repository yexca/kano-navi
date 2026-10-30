// Shapes of src/lineblog/posts.json and media-catalog.json, written by
// scripts/package-lineblog.ts from the local recovery archive.

export interface LineblogText {
  type: "text"
  text: string
}

export interface LineblogLink {
  type: "link"
  href: string
  content: LineblogInline[]
}

/** An archived image (`media`) or one the archive never captured (`remote`). */
export interface LineblogImage {
  type: "image"
  alt: string
  media?: string
  remote?: string
  /** The link the image opened in the post. */
  href?: string
  /** Where the archived bytes were recovered from. */
  source?: string
  /**
   * A picture added after the recovery archive: a stand-in for an original
   * that was never archived, or a picture of an embedded tweet.
   */
  recovery?: "stand-in" | "tweet"
}

export type LineblogInline = LineblogText | LineblogLink | LineblogImage

export type LineblogBlock =
  | {
      type: "p"
      content: LineblogInline[]
      /** Simplified Chinese translation paragraphs. */
      zh?: LineblogInline[][]
    }
  | { type: "quote"; content: LineblogInline[] }
  | { type: "hr" }
  | { type: "video"; video: string }
  | {
      type: "youtube"
      videoId: string
      watch: string
      /** The downloaded copy of the video. */
      video?: string
      cover?: string
      coverSource?: string
      archive?: string
    }
  | {
      type: "card"
      title: string
      url: string
      archive?: string
      description?: string
      image?: LineblogImage
    }

export interface LineblogPost {
  /** The article number of its lineblog.me address. */
  id: string
  /** ISO instant; the blog showed it in Japan time. */
  published: string
  title: string
  titleZh?: string
  url: string
  /** The Wayback Machine capture the post was recovered from. */
  archive: string
  blocks: LineblogBlock[]
}

export interface LineblogMedia {
  /** The first 12 hex digits of `sha256` and the extension. */
  name: string
  mimeType: string
  byteSize: number
  sha256: string
  width?: number
  height?: number
}
