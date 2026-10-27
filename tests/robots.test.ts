import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
import { ATOM_FEED_PATH, CALENDAR_FEED_PATH } from "../src/lib/feeds.ts"

// The longest matching rule wins and Allow wins a tie, as RFC 9309 and
// Google's subscription fetcher read robots.txt.
function allowed(robots: string, path: string): boolean {
  let best = { length: -1, allow: true }
  for (const line of robots.split(/\r?\n/u)) {
    const match = /^(allow|disallow):\s*(\S*)/iu.exec(line.trim())
    if (!match || !match[2] || !path.startsWith(match[2])) continue
    const allow = match[1].toLowerCase() === "allow"
    if (
      match[2].length > best.length ||
      (match[2].length === best.length && allow)
    ) {
      best = { length: match[2].length, allow }
    }
  }
  return best.allow
}

const robots = readFileSync(
  new URL("../public/robots.txt", import.meta.url),
  "utf8",
)

test("robots.txt lets subscription clients fetch the public feeds", () => {
  for (const path of [
    CALENDAR_FEED_PATH,
    `${CALENDAR_FEED_PATH}?lang=ja`,
    ATOM_FEED_PATH,
    `${ATOM_FEED_PATH}?lang=zh-CN`,
    "/",
    "/history",
  ]) {
    assert.ok(allowed(robots, path), path)
  }
})

test("robots.txt keeps the console and machine endpoints out", () => {
  for (const path of ["/admin", "/api/dashboard", "/api/calendar", "/mcp"]) {
    assert.ok(!allowed(robots, path), path)
  }
})
