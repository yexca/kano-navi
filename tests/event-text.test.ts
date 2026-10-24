import assert from "node:assert/strict"
import test from "node:test"
import { adminMessages } from "../src/admin-i18n.ts"
import {
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
    const key = `admin.event.status.${code}`
    assert.equal(typeof adminMessages.en[key], "string", key)
    assert.doesNotMatch(adminMessages.en[key], cjk, key)
    assert.equal(typeof adminMessages["zh-CN"][key], "string", key)
  }
})

test("generated YouTube details read without CJK text", () => {
  for (const detail of [
    "YouTube Live / 预约",
    "YouTube Premiere / 预约",
    "メンバーシップ限定 / YouTube",
  ])
    assert.doesNotMatch(eventDetailText(detail), cjk, detail)
  assert.equal(eventDetailText("朝、なにかするかも？"), "朝、なにかするかも？")
  assert.equal(eventDetailText(null), "")
})
