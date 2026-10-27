import assert from "node:assert/strict"
import test from "node:test"
import { mediaSrcSet, mediaVariantUrl } from "../src/lib/media.ts"
import {
  ATOM_FEED_PATH,
  calendarFeedUrls,
  feedPath,
  syncFeedLinks,
} from "../src/lib/feeds.ts"

const id = "a".repeat(64)
const cached = `/media/${id}?v=${"b".repeat(64)}`

test("cached media lists smaller copies below its original width", () => {
  assert.equal(
    mediaSrcSet(cached, 1000),
    [
      `${cached}&w=320 320w`,
      `${cached}&w=640 640w`,
      `${cached}&w=960 960w`,
      `${cached} 1000w`,
    ].join(", "),
  )
  assert.equal(mediaSrcSet(cached, 300), `${cached} 300w`)
})

test("cached media of unknown width lists every copy", () => {
  const srcset = mediaSrcSet(cached, null)
  assert.equal(srcset?.split(", ").length, 5)
  assert.ok(srcset?.endsWith(`${cached}&w=1920 1920w`))
  assert.equal(mediaVariantUrl(`/media/${id}`, 640), `/media/${id}?w=640`)
})

test("other image URLs get no srcset", () => {
  for (const url of [
    null,
    undefined,
    "",
    "/assets/kano-banner.jpg",
    "/media/profile/banner?v=abc",
    "/media/not-a-hash",
    "https://pbs.twimg.com/media/synthetic.jpg",
  ]) {
    assert.equal(mediaSrcSet(url, 1200), undefined, String(url))
  }
  assert.equal(
    mediaVariantUrl("/assets/kano-banner.jpg", 640),
    "/assets/kano-banner.jpg",
  )
})

test("calendar subscription addresses follow the page origin", () => {
  assert.deepEqual(calendarFeedUrls("https://kano.example.invalid"), {
    https: "https://kano.example.invalid/api/calendar.ics",
    webcal: "webcal://kano.example.invalid/api/calendar.ics",
  })
  assert.equal(
    calendarFeedUrls("http://127.0.0.1:7657").webcal,
    "webcal://127.0.0.1:7657/api/calendar.ics",
  )
})

test("feed addresses carry a non-English reader's language", () => {
  assert.equal(feedPath(ATOM_FEED_PATH), "/api/feed.xml")
  assert.equal(feedPath(ATOM_FEED_PATH, "en"), "/api/feed.xml")
  assert.equal(feedPath(ATOM_FEED_PATH, "ja"), "/api/feed.xml?lang=ja")
  assert.deepEqual(calendarFeedUrls("https://kano.example.invalid", "zh-CN"), {
    https: "https://kano.example.invalid/api/calendar.ics?lang=zh-CN",
    webcal: "webcal://kano.example.invalid/api/calendar.ics?lang=zh-CN",
  })
})

test("feed discovery links follow the reader's language", () => {
  const links: Record<string, { href: string }> = {
    "application/atom+xml": { href: "/api/feed.xml" },
    "text/calendar": { href: "/api/calendar.ics" },
  }
  const root = {
    querySelector(selector: string) {
      const type = /type="([^"]+)"/u.exec(selector)?.[1] || ""
      const link = links[type]
      return link
        ? {
            setAttribute: (_name: string, value: string) => {
              link.href = value
            },
          }
        : null
    },
  } as unknown as ParentNode
  syncFeedLinks("ja", root)
  assert.equal(links["application/atom+xml"].href, "/api/feed.xml?lang=ja")
  assert.equal(links["text/calendar"].href, "/api/calendar.ics?lang=ja")
  syncFeedLinks("en", root)
  assert.equal(links["application/atom+xml"].href, "/api/feed.xml")
})
