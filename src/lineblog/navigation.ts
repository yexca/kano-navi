import type { MouseEvent, SyntheticEvent } from "react"
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react"
import { flushSync } from "react-dom"

import { loadedPost } from "./content"
import { loadView, viewLoaded } from "./post-files"
import type { LineblogSummary } from "./types"

function newEntry() {
  return `${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`
}

function isLineblogPath(pathname: string) {
  return pathname.replace(/\/+$/u, "") === "/lineblog"
}

/** The address of a link to another view of this page, if it is one. */
function viewUrl(target: EventTarget | null) {
  const link = (target as Element | null)?.closest?.("a[href]")
  if (!(link instanceof HTMLAnchorElement) || link.target) return null
  const url = new URL(link.href, window.location.href)
  return url.origin === window.location.origin &&
    isLineblogPath(url.pathname) &&
    !url.hash
    ? url
    : null
}

/**
 * The query picks an article, a month or a page. Moving between them stays
 * in this document: the click pushes an entry, and the view follows once its
 * posts have loaded, keeping the current one on screen meanwhile. Back and
 * forward restore the query before the site's navigation restores the
 * entry's scroll position; a view whose posts are not loaded yet restores
 * it again once they are. Pointing at or focusing a link starts its load.
 */
export function useLineblogLocation() {
  const [search, setSearch] = useState(() => window.location.search)
  const ticket = useRef(0)
  useEffect(() => {
    const onPopState = (event: PopStateEvent) => {
      if (!isLineblogPath(window.location.pathname)) return
      const next = window.location.search
      const current = ++ticket.current
      if (viewLoaded(next)) {
        flushSync(() => setSearch(next))
        return
      }
      const top: unknown = event.state?.kanoScroll
      const show = () => {
        if (current !== ticket.current || window.location.search !== next)
          return
        flushSync(() => setSearch(next))
        if (typeof top === "number")
          window.scrollTo({ top, behavior: "instant" })
      }
      void loadView(next).then(show, show)
    }
    window.addEventListener("popstate", onPopState)
    return () => window.removeEventListener("popstate", onPopState)
  }, [])

  const go = useCallback((url: URL) => {
    if (url.href !== window.location.href) {
      window.history.replaceState(
        { ...window.history.state, kanoScroll: window.scrollY },
        "",
      )
      window.history.pushState({ kanoEntry: newEntry() }, "", url.href)
    }
    const current = ++ticket.current
    const show = () => {
      if (current !== ticket.current) return
      flushSync(() => setSearch(url.search))
      const main = document.getElementById("lineblog-content")
      if (main && main.getBoundingClientRect().top < 0)
        main.scrollIntoView({ behavior: "instant" })
    }
    if (viewLoaded(url.search)) show()
    else void loadView(url.search).then(show, show)
  }, [])

  const onClick = useCallback(
    (event: MouseEvent<HTMLElement>) => {
      if (
        event.defaultPrevented ||
        event.button !== 0 ||
        event.metaKey ||
        event.ctrlKey ||
        event.shiftKey ||
        event.altKey
      )
        return
      const url = viewUrl(event.target)
      if (!url) return
      event.preventDefault()
      go(url)
    },
    [go],
  )

  const onIntent = useCallback((event: SyntheticEvent<HTMLElement>) => {
    const url = viewUrl(event.target)
    if (url) loadView(url.search).catch(() => undefined)
  }, [])

  return { search, go, onClick, onIntent }
}

/**
 * LINE BLOG served phones its smartphone template at the same addresses.
 * Phones of the blog's years were 320 to 480 CSS pixels wide upright, and
 * tablets from 600 got the PC page, so a window up to 600 pixels wide gets
 * the smartphone template.
 */
const PHONE_QUERY = "(max-width: 600px)"

function subscribePhone(onChange: () => void) {
  const query = window.matchMedia?.(PHONE_QUERY)
  query?.addEventListener("change", onChange)
  return () => query?.removeEventListener("change", onChange)
}

export function usePhoneLayout() {
  return useSyncExternalStore(
    subscribePhone,
    () => window.matchMedia?.(PHONE_QUERY).matches ?? false,
  )
}

/**
 * The view's posts, once they and an article's comments have loaded.
 * Browsers keep a failed chunk load for the document, so a failed view offers
 * a reload rather than a second request.
 */
export function useViewPosts(search: string, shown: LineblogSummary[]) {
  const ready = viewLoaded(search)
  const [, setLoaded] = useState(0)
  const [failed, setFailed] = useState<string | null>(null)
  useEffect(() => {
    if (ready) return
    let current = true
    loadView(search).then(
      () => {
        if (current) setLoaded((count) => count + 1)
      },
      () => {
        if (current) setFailed(search)
      },
    )
    return () => {
      current = false
    }
  }, [ready, search])
  return {
    posts: ready ? shown.map((post) => loadedPost(post.id)!) : null,
    failed: !ready && failed === search,
  }
}

export function lineblogHref(query: Record<string, string | number> = {}) {
  const params = new URLSearchParams(
    Object.entries(query).map(([key, value]) => [key, String(value)]),
  )
  const text = params.toString()
  return text ? `/lineblog?${text}` : "/lineblog"
}

export function articleLink(post: { id: string }) {
  return lineblogHref({ article: post.id })
}
