import assert from "node:assert/strict"
import test from "node:test"
import { historyMove, inPlaceUrl, pagePath } from "../src/lib/page-path.ts"

test("page paths drop trailing slashes", () => {
  assert.equal(pagePath("/"), "/")
  assert.equal(pagePath(""), "/")
  assert.equal(pagePath("/history/"), "/history")
  assert.equal(pagePath("/about//"), "/about")
})

test("the board's index.html is the board", () => {
  assert.equal(pagePath("/index.html"), "/")
  assert.equal(pagePath("/index.html/"), "/")
  assert.equal(pagePath("/history/index.html"), "/history/index.html")
})

const pages = ["/", "/history", "/lineblog", "/about"]
const board = "https://kano.example.invalid/"

test("links to public pages open in place", () => {
  assert.equal(inPlaceUrl("/history", board, pages)?.pathname, "/history")
  assert.equal(inPlaceUrl("/about/", board, pages)?.pathname, "/about/")
  assert.equal(
    inPlaceUrl("/", "https://kano.example.invalid/history", pages)?.pathname,
    "/",
  )
  assert.equal(inPlaceUrl("/history#2020", board, pages)?.hash, "#2020")
  assert.equal(
    inPlaceUrl("/lineblog?article=1", board, pages)?.search,
    "?article=1",
  )
})

test("the current page's own link opens in place without a fragment", () => {
  assert.equal(inPlaceUrl("/", board, pages)?.href, board)
  assert.equal(inPlaceUrl("#top", board, pages), null)
  assert.equal(inPlaceUrl("/#top", board, pages), null)
})

test("other pages, origins and addresses load normally", () => {
  assert.equal(inPlaceUrl("/admin", board, pages), null)
  assert.equal(inPlaceUrl("/api/feed.xml", board, pages), null)
  assert.equal(
    inPlaceUrl("https://other.example.invalid/history", board, pages),
    null,
  )
  assert.equal(inPlaceUrl("mailto:fan@example.com", board, pages), null)
  assert.equal(inPlaceUrl("/history", "not a page address", pages), null)
})

test("back and forward swap public pages in place", () => {
  assert.equal(historyMove("/history", "/", pages), "show")
  assert.equal(historyMove("/", "/about", pages), "show")
  assert.equal(historyMove("/history", "/history", pages), "show")
})

test("a fragment move within another page stays with that page", () => {
  assert.equal(historyMove("/admin", "/admin", pages), "stay")
  assert.equal(historyMove("/missing", "/missing", pages), "stay")
})

test("moves to or from another page load that document", () => {
  assert.equal(historyMove("/admin", "/", pages), "load")
  assert.equal(historyMove("/", "/admin", pages), "load")
  assert.equal(historyMove("/missing", "/history", pages), "load")
})
