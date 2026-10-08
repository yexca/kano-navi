// Public projections shared by the HTTP dashboard and MCP reads. Operator
// metadata such as per-source counters, raw fetch errors, job identifiers, and
// source item IDs stays behind /api/admin.

export const publicEventFields = [
  "id",
  "source",
  "title",
  "detail",
  "startsOn",
  "startsAt",
  "endsAt",
  "timezone",
  "timePrecision",
  "status",
  "eventType",
  "url",
  "provenance",
  "cancellationStatus",
  "cancellationSource",
  "cancellationReason",
  "cancellationEvidence",
  "cancellationAt",
]

export function publicEvent(event, extraFields = []) {
  return Object.fromEntries(
    [...publicEventFields, ...extraFields]
      .filter((field) =>
        Object.prototype.hasOwnProperty.call(event || {}, field),
      )
      .map((field) => [field, event[field]]),
  )
}

export function publicSyncRun(run) {
  if (!run) return null
  return {
    id: run.id,
    source: run.source,
    startedAt: run.startedAt,
    finishedAt: run.finishedAt,
    status: run.status,
    message:
      {
        success: "同步完成",
        partial: "部分数据源不可用或存在警告，保留已有快照",
        failed: "同步失败，保留已有快照",
        running: "同步进行中",
      }[run.status] || null,
  }
}

export function publicProfileMedia(asset) {
  if (!asset) return null
  return Object.fromEntries(
    [
      "id",
      "slot",
      "source",
      "sourceUrl",
      "status",
      "isActive",
      "publicUrl",
    ].map((field) => [field, asset[field]]),
  )
}

export function publicProfileMediaSlots(slots) {
  return {
    avatar: publicProfileMedia(slots?.avatar),
    banner: publicProfileMedia(slots?.banner),
  }
}
