import assert from "node:assert/strict"
import test from "node:test"
import { adminMessages } from "../src/admin-i18n.ts"
import { createTranslator } from "../src/i18n.ts"
import {
  eventDetailCode,
  eventDetailText,
  eventStatusCode,
  eventStatusCodes,
} from "../src/lib/event-text.ts"

const cjk = /[぀-ヿ一-鿿]/u

test("stored codes and the earlier Chinese statuses read as codes", () => {
  for (const [stored, code] of [
    ["scheduled", "scheduled"],
    [" Live ", "live"],
    ["ended", "ended"],
    ["已预约", "scheduled"],
    ["会员限定", "scheduled"],
    ["直播中", "live"],
    ["已完成", "ended"],
    ["待确认", "pending"],
  ])
    assert.equal(eventStatusCode(stored), code, stored)
  for (const note of ["", null, undefined, "after the 3D showcase"])
    assert.equal(eventStatusCode(note), null)
})

test("every status code has English and Chinese admin labels", () => {
  for (const code of eventStatusCodes) {
    const key =
      `admin.event.status.${code}` as keyof (typeof adminMessages)["en"]
    assert.equal(typeof adminMessages.en[key], "string", key)
    assert.doesNotMatch(String(adminMessages.en[key]), cjk, key)
    // New admin text is English only, so the Chinese catalog is partial.
    const chinese: Record<string, unknown> = adminMessages["zh-CN"]
    assert.equal(typeof chinese[key], "string", key)
  }
})

test("generated YouTube details, current and earlier, read as codes", () => {
  for (const [stored, code] of [
    ["YouTube Live", "youtube_live"],
    ["YouTube Premiere", "youtube_premiere"],
    ["YouTube Live / 预约", "youtube_live"],
    ["YouTube Premiere / 预约", "youtube_premiere"],
    ["メンバーシップ限定 / YouTube", "youtube_live"],
  ])
    assert.equal(eventDetailCode(stored), code, stored)
  for (const note of ["", null, "YouTube Live with guests"])
    assert.equal(eventDetailCode(note), null)
})

test("every detail code has its own label in each locale", () => {
  const en = createTranslator("en")
  for (const code of ["youtube_live", "youtube_premiere"]) {
    const key = `schedule.detail.${code}`
    assert.notEqual(en(key), key)
    assert.doesNotMatch(en(key), cjk, key)
    // A missing message would fall back to the English label.
    for (const locale of ["ja", "zh-CN"] as const)
      assert.notEqual(
        createTranslator(locale)(key),
        en(key),
        `${locale} ${key}`,
      )
  }
})

test("details show in the reader's language; other text stays as written", () => {
  const zh = createTranslator("zh-CN")
  assert.equal(eventDetailText("YouTube Premiere", zh), "YouTube 首播")
  assert.equal(
    eventDetailText("YouTube Live / 预约", createTranslator("ja")),
    "YouTube ライブ",
  )
  assert.equal(
    eventDetailText("YouTube Live", createTranslator("en")),
    "YouTube Live",
  )
  assert.equal(
    eventDetailText("朝、なにかするかも？", zh),
    "朝、なにかするかも？",
  )
  assert.equal(eventDetailText(null, zh), "")
})
