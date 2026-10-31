import assert from "node:assert/strict"
import test from "node:test"
import {
  masonryColumns,
  masonryLayout,
  postWallCount,
} from "../src/dashboard/post-rows.ts"

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

test("the archive fits up to three masonry columns of at least 300px", () => {
  const cases: [width: number, columns: number][] = [
    [0, 1],
    [375, 1],
    [615, 1],
    [616, 2],
    [931, 2],
    [932, 3],
    [1600, 3],
  ]
  for (const [width, columns] of cases) {
    assert.equal(masonryColumns(width), columns, `${width}px`)
  }
})

test("the archive masonry reads row by row", () => {
  // The first three posts open the columns left to right; the fourth goes
  // under the shortest column, not under the first post.
  const layout = masonryLayout([300, 100, 200, 50, 50, 50], 3, 10)
  assert.deepEqual(layout.columns, [0, 1, 2, 1, 1, 2])
  assert.deepEqual(layout.tops, [0, 0, 0, 110, 170, 210])
  assert.equal(layout.height, 300)
})

test("a single masonry column stacks posts in order", () => {
  const layout = masonryLayout([40, 60, 20], 1, 16)
  assert.deepEqual(layout.columns, [0, 0, 0])
  assert.deepEqual(layout.tops, [0, 56, 132])
  assert.equal(layout.height, 152)
  assert.equal(masonryLayout([], 3).height, 0)
})
