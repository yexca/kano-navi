import assert from "node:assert/strict"
import test from "node:test"
import { pagePath } from "../src/lib/page-path.ts"

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
