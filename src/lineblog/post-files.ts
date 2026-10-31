// Each post file, and each article's comments file, builds into its own
// content-hashed chunk, so a view loads only the posts it shows and an
// article view only its comments. Kept apart from content.ts, which the tests
// import outside Vite.
import { addPostFile, loadedPost, lineblogView } from "./content"
import type { LineblogComment, LineblogPost, LineblogPostFile } from "./types"

const files = import.meta.glob<LineblogPostFile>("./posts/*.json", {
  import: "default",
})
const commentFiles = import.meta.glob<LineblogComment[]>("./comments/*.json", {
  import: "default",
})
const pending = new Map<string, Promise<LineblogPost>>()
const comments = new Map<string, LineblogComment[]>()

/** Loads one post once; requests for the same post share one load. */
export function loadPost(id: string): Promise<LineblogPost> {
  const known = loadedPost(id)
  if (known) return Promise.resolve(known)
  let request = pending.get(id)
  if (!request) {
    const load = files[`./posts/${id}.json`]
    request = (load ? load() : Promise.reject(new Error(`no post ${id}`)))
      .then(addPostFile)
      .finally(() => pending.delete(id))
    pending.set(id, request)
  }
  return request
}

/** An article's archived comments, newest first; none when it has no file. */
export function loadedComments(id: string) {
  return comments.get(id)
}

/**
 * Loads an article's comments once. A failed load shows the article without
 * them, since the browser keeps the failure for the document.
 */
function loadComments(id: string): Promise<unknown> {
  if (comments.has(id)) return Promise.resolve()
  const load = commentFiles[`./comments/${id}.json`]
  return (load ? load() : Promise.resolve([])).then(
    (list) => comments.set(id, list),
    () => comments.set(id, []),
  )
}

/** The posts a query shows, and an article's comments, loaded together. */
export function loadView(search: string) {
  const view = lineblogView(search)
  return Promise.all([
    ...view.shown.map((post) => loadPost(post.id)),
    view.article ? loadComments(view.article.id) : null,
  ])
}

export function viewLoaded(search: string) {
  const view = lineblogView(search)
  return (
    view.shown.every((post) => loadedPost(post.id)) &&
    (!view.article || comments.has(view.article.id))
  )
}
