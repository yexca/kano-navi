import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
import { dashboardMessages } from "../src/dashboard/dashboard-i18n.ts"
import { postMessages } from "../src/dashboard/post-i18n.ts"

// The board modules the posts page renders, and the page's own.
const rendered = [
  "src/dashboard/components/archive-dialog.tsx",
  "src/dashboard/components/month-picker.tsx",
  "src/dashboard/components/post-wall.tsx",
  "src/dashboard/components/sheet-dialog.tsx",
  "src/dashboard/format.ts",
  "src/posts/account-band.tsx",
  "src/posts/activity-map.tsx",
  "src/posts/post-timeline.tsx",
  "src/posts/posts-app.tsx",
]

test("the posts page carries every board key its post cards name", () => {
  const board = Object.keys(dashboardMessages.en)
  const needed = new Set<string>()
  for (const file of rendered) {
    const source = readFileSync(new URL(`../${file}`, import.meta.url), "utf8")
    for (const key of board)
      if (source.includes(`"${key}"`) || source.includes(`\`${key}\``))
        needed.add(key)
    for (const [, prefix] of source.matchAll(/`([a-z]+\.[\w.]*?)\$\{/giu))
      for (const key of board) if (key.startsWith(prefix)) needed.add(key)
  }
  assert.ok(needed.size > 0)
  for (const key of needed)
    assert.ok(Object.hasOwn(postMessages.en, key), `${key} in post-i18n.ts`)
})

test("the shared post copy is the board's, in every locale, without the rest", () => {
  for (const locale of ["en", "ja", "zh-CN"] as const) {
    assert.deepEqual(
      Object.keys(postMessages[locale]).sort(),
      Object.keys(postMessages.en).sort(),
      locale,
    )
    for (const key of Object.keys(postMessages[locale]))
      assert.equal(
        (dashboardMessages[locale] as Record<string, unknown>)[key],
        (postMessages[locale] as Record<string, unknown>)[key],
        `${locale} ${key}`,
      )
  }
  for (const key of Object.keys(postMessages.en))
    assert.doesNotMatch(key, /^(schedule|videos|spotlight|sales)\./u)
})
