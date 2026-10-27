import assert from "node:assert/strict"
import test from "node:test"
import { postWallCount } from "../src/dashboard/post-rows.ts"

test("the post wall shows whole rows up to six posts or two rows", () => {
  const cases: [columns: number, available: number, shown: number][] = [
    [1, 9, 6],
    [1, 4, 4],
    [1, 0, 0],
    [2, 9, 6],
    [2, 6, 6],
    [2, 5, 4],
    [2, 3, 2],
    [2, 1, 1],
    [3, 9, 6],
    [3, 6, 6],
    [3, 5, 3],
    [3, 4, 3],
    [3, 2, 2],
    [4, 12, 8],
    [4, 8, 8],
    [4, 7, 4],
    [4, 5, 4],
    [4, 3, 3],
  ]
  for (const [columns, available, shown] of cases) {
    assert.equal(
      postWallCount(available, columns),
      shown,
      `${available} posts in ${columns} columns`,
    )
  }
})
