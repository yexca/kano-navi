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
