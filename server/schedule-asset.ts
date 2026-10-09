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

function shiftDate(key, days) {
  const date = new Date(`${key}T00:00:00Z`)
  date.setUTCDate(date.getUTCDate() + days)
  return date.toISOString().slice(0, 10)
}

function dateParts(year, month, day) {
  const key = `${year}-${String(month).padStart(2, "0")}-${String(day).padStart(2, "0")}`
  const time = Date.parse(`${key}T00:00:00Z`)
  return Number.isFinite(time) &&
    new Date(time).toISOString().slice(0, 10) === key
    ? key
    : null
}

/** Publication anchors relative wording and an omitted year, never an unknown period. */
export function schedulePeriod(text, publishedAt) {
  const published = weekStartInJapan(publishedAt)
  const unknown = {
    period_start: null,
    period_end: null,
    period_basis: "unknown",
    week_start: null,
  }
  if (!published) return unknown
  const year = Number(published.slice(0, 4))
  const matches = [
    ...String(text).matchAll(
      /(?:(\d{4})[年/.-])?(\d{1,2})[月/.-](\d{1,2})(?:日)?/gu,
    ),
  ]
  if (matches.length) {
    const ranges = matches
      .slice(0, -1)
      .map((first, index) => ({ first, last: matches[index + 1] }))
      .filter(({ first, last }) =>
        /^\s*(?:[（(][^）)]{1,3}[）)])?\s*(?:[–—~〜～-]|から|至|to)\s*$/iu.test(
          String(text).slice(first.index + first[0].length, last.index),
        ),
      )
    if (ranges.length > 1 || (!ranges.length && matches.length > 1))
      return unknown
    const first = ranges[0]?.first || matches[0]
    const years = first[1] ? [Number(first[1])] : [year - 1, year, year + 1]
    const start = years
      .map((value) => dateParts(value, Number(first[2]), Number(first[3])))
      .filter(Boolean)
      .sort(
        (a, b) =>
          Math.abs(Date.parse(a) - Date.parse(published)) -
          Math.abs(Date.parse(b) - Date.parse(published)),
      )[0]
    if (!start) return unknown
    let end = start
    if (ranges.length) {
      const last = ranges[0].last
      let endYear = last[1] ? Number(last[1]) : Number(start.slice(0, 4))
      if (!last[1] && Number(last[2]) < Number(first[2])) endYear++
      end = dateParts(endYear, Number(last[2]), Number(last[3]))
    } else {
      const tail = String(text).slice(first.index + first[0].length)
      const dayRange = /^\s*[–—~〜～-]\s*(\d{1,2})(?:日)?(?![\d/])/u.exec(tail)
      if (dayRange) {
        let month = Number(first[2]),
          endYear = Number(start.slice(0, 4))
        if (Number(dayRange[1]) < Number(first[3])) {
          month++
          if (month > 12) {
            month = 1
            endYear++
          }
        }
        end = dateParts(endYear, month, Number(dayRange[1]))
      }
    }
    if (
      !end ||
      end < start ||
      Date.parse(end) - Date.parse(start) > 366 * 86400000
    )
      return unknown
    return {
      period_start: start,
      period_end: end,
      period_basis: "explicit",
      week_start: weekStartInJapan(`${start}T00:00:00+09:00`),
    }
  }
  const next = /来週|下周|next\s+week/iu.test(text)
  const current = /今週|本周|this\s+week/iu.test(text)
  if (next === current) return unknown
  const start = shiftDate(published, next ? 7 : 0)
  return {
    period_start: start,
    period_end: shiftDate(start, 6),
    period_basis: "relative",
    week_start: start,
  }
}

/** Shared by profile synchronization and official date-window pages. */
export function scheduleAssetsFromPosts(posts, handle) {
  const byWeek = new Map<string, Record<string, any>>()
  for (const post of [...posts].sort(
    (a, b) => Date.parse(b.published_at) - Date.parse(a.published_at),
  )) {
    if (!post.media_url || !isLikelyScheduleBoardPost(post)) continue
    const period = schedulePeriod(
      scheduleBoardSourceText(post),
      post.published_at,
    )
    const id = `schedule-${period.week_start || post.id}`
    if (byWeek.has(id)) continue
    byWeek.set(id, {
      id,
      kind: "schedule",
      url: post.media_url,
      source_url: post.url,
      alt: post.media_alt || "Kano Mahoro weekly schedule",
      ...period,
      source_account: handle,
      updated_at: post.published_at,
    })
  }
  return [...byWeek.values()]
}
