// Each post file builds into its own content-hashed chunk, so a view loads
// only the posts it shows. Kept apart from content.ts, which the tests import
// outside Vite.
import { addPostFile, loadedPost, lineblogView } from "./content"
import type { LineblogPost, LineblogPostFile } from "./types"

const files = import.meta.glob<LineblogPostFile>("./posts/*.json", {
  import: "default",
})
const pending = new Map<string, Promise<LineblogPost>>()

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

/** The posts a query shows, loaded together. */
export function loadView(search: string) {
  return Promise.all(
    lineblogView(search).shown.map((post) => loadPost(post.id)),
  )
}

export function viewLoaded(search: string) {
  return lineblogView(search).shown.every((post) => loadedPost(post.id))
}
