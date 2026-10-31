// Picture rules shared by the page and scripts/package-lineblog.ts.
import type {
  LineblogImage,
  LineblogInline,
  LineblogMedia,
  LineblogPost,
} from "./types.ts"

/** Pictographs the blog drew inline with the text (15 and 78 px). */
export function isEmojiMedia(item: LineblogMedia | undefined) {
  return item?.width !== undefined && item.width <= 80
}

export function inlineImages(content: LineblogInline[]): LineblogImage[] {
  return content.flatMap((inline) =>
    inline.type === "image"
      ? [inline]
      : inline.type === "link"
        ? inlineImages(inline.content)
        : [],
  )
}

/** The first archived picture or video cover, for the recent-posts list. */
export function postThumbnail(
  post: LineblogPost,
  media: Map<string, LineblogMedia>,
) {
  for (const block of post.blocks) {
    if (block.type === "youtube" && block.cover) return block.cover
    if (block.type === "p") {
      const image = inlineImages(block.content).find(
        (item) => item.media && !isEmojiMedia(media.get(item.media)),
      )
      if (image?.media) return image.media
    }
  }
  return undefined
}
