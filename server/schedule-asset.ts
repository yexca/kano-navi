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

const japanDateFormatter = new Intl.DateTimeFormat("en-CA", {
  timeZone: "Asia/Tokyo",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
})

function weekStartInJapan(value) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return null
  const parts = japanDateFormatter
    .formatToParts(date)
    .reduce<Record<string, string>>((result, part) => {
      result[part.type] = part.value
      return result
    }, {})
  const monday = new Date(`${parts.year}-${parts.month}-${parts.day}T00:00:00Z`)
  monday.setUTCDate(monday.getUTCDate() - ((monday.getUTCDay() + 6) % 7))
  return monday.toISOString().slice(0, 10)
}

/** Shared by profile synchronization and official date-window pages. */
export function scheduleAssetsFromPosts(posts, handle) {
  const byWeek = new Map<string, Record<string, any>>()
  for (const post of [...posts].sort(
    (left, right) =>
      Date.parse(right.published_at) - Date.parse(left.published_at),
  )) {
    if (!post.media_url || !isLikelyScheduleBoardPost(post)) continue
    const weekStart = weekStartInJapan(post.published_at)
    const id = `schedule-${weekStart || post.id}`
    if (byWeek.has(id)) continue
    byWeek.set(id, {
      id,
      kind: "schedule",
      url: post.media_url,
      source_url: post.url,
      alt: post.media_alt || "Kano Mahoro weekly schedule",
      week_start: weekStart,
      source_account: handle,
      updated_at: post.published_at,
    })
  }
  return [...byWeek.values()]
}
