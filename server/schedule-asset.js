const explicitScheduleBoardPattern =
  /(?:\b(?:weekly[\s_-]+)?schedule(?:[\s_-]+(?:board|table))?\b|\bthis\s+week(?:'s)?\s+schedule\b|スケジュール|予定表|(?:今週|来週|今月|週間).{0,4}(?:予定|スケジュール))/iu

/**
 * Schedule images must be weekly-board notices, not any post that merely
 * mentions a future stream. Keep this predicate independent of the LLM
 * keyword detector so a generic `予定` keyword cannot promote a thumbnail.
 */
export function isLikelyScheduleBoardText(value) {
  return explicitScheduleBoardPattern.test(String(value || ""))
}

/**
 * Only the author's own wording (including a quoted post) counts. The stored
 * `label` is derived by a looser classifier (`配信予定` maps to
 * `SCHEDULE / 日程`), so feeding it back in would reopen the generic path.
 */
export function scheduleBoardSourceText(post) {
  return String(post?.search_text ?? post?.searchText ?? post?.text ?? "")
}

export function isLikelyScheduleBoardPost(post) {
  return isLikelyScheduleBoardText(scheduleBoardSourceText(post))
}
