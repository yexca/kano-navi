import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
import { localizeSeedText } from "../src/dashboard/seed-copy.ts"

type SeedRow = { title: string | null; detail: string | null }

const seed: { timeline: SeedRow[]; resources: SeedRow[] } = JSON.parse(
  readFileSync(
    new URL(
      "../backend/internal/store/sqlite/static-seed.json",
      import.meta.url,
    ),
    "utf8",
  ),
)

const seedTexts = [
  ...seed.timeline.flatMap((item) => [item.title, item.detail]),
  ...seed.resources.flatMap((item) => [item.title, item.detail]),
].filter((text) => text != null)

const cjk = /[぀-ヿ一-鿿]/u

test("every timeline and link seed reads without CJK text in English", () => {
  for (const text of seedTexts) {
    const localized = localizeSeedText(text, "en")
    assert.ok(localized !== null, text)
    assert.doesNotMatch(localized, cjk, text)
  }
})

test("every seed detail has a Japanese translation", () => {
  const details = [...seed.timeline, ...seed.resources]
    .map((item) => item.detail)
    .filter((detail) => detail != null)
  for (const detail of details)
    assert.notEqual(localizeSeedText(detail, "ja"), detail, detail)
})

test("rows that differ from the seed keep their stored text", () => {
  assert.equal(localizeSeedText("运营者修改的说明", "en"), "运营者修改的说明")
  assert.equal(localizeSeedText(null, "ja"), null)
  assert.equal(
    localizeSeedText("与ろん组成音乐组合。", "zh-CN"),
    "与ろん组成音乐组合。",
  )
})
