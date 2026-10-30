/** The board lists at least this many posts, and two full rows when wider. */
export const POST_WALL_MIN = 6

/**
 * How many of the available posts the wall shows at a column count: up to
 * POST_WALL_MIN or two full rows, whichever is more, and only whole rows, so
 * the grid never ends on a ragged one. Fewer posts than one row show as they
 * are.
 */
export function postWallCount(available: number, columns: number): number {
  const width = Math.max(1, Math.floor(columns))
  const shown = Math.min(
    Math.max(0, available),
    Math.max(POST_WALL_MIN, width * 2),
  )
  return shown < width ? shown : shown - (shown % width)
}

/** The post archive's masonry: up to three columns at least this wide. */
export const MASONRY_MAX_COLUMNS = 3
export const MASONRY_MIN_WIDTH = 300
export const MASONRY_GAP = 16

/** How many masonry columns fit a wall of this width. */
export function masonryColumns(width: number): number {
  const fit = Math.floor(
    (Math.max(0, width) + MASONRY_GAP) / (MASONRY_MIN_WIDTH + MASONRY_GAP),
  )
  return Math.min(MASONRY_MAX_COLUMNS, Math.max(1, fit))
}

export interface MasonryLayout {
  /** Each item's column, in item order. */
  columns: number[]
  /** Each item's distance from the top of the wall, in pixels. */
  tops: number[]
  /** The tallest column, which is the wall's height. */
  height: number
}

/**
 * Places items in order, each in the column whose bottom is highest (the
 * leftmost on a tie), so the wall reads row by row: the first items open the
 * columns left to right and each later one sits below an earlier one.
 */
export function masonryLayout(
  heights: number[],
  columnCount: number,
  gap = MASONRY_GAP,
): MasonryLayout {
  const bottoms = new Array<number>(Math.max(1, Math.floor(columnCount))).fill(
    0,
  )
  const columns: number[] = []
  const tops: number[] = []
  for (const height of heights) {
    const column = bottoms.indexOf(Math.min(...bottoms))
    columns.push(column)
    tops.push(bottoms[column])
    bottoms[column] += Math.max(0, height) + gap
  }
  return {
    columns,
    tops,
    height: heights.length ? Math.max(...bottoms) - gap : 0,
  }
}
