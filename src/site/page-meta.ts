import { useEffect } from "react"

/** The page's document title and link-preview description, in its locale. */
export function usePageMeta(title: string, description: string) {
  useEffect(() => {
    document.title = title
    document
      .querySelector('meta[name="description"]')
      ?.setAttribute("content", description)
  }, [title, description])
}
