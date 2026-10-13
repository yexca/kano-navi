// Generates the image parity fixture for backend-go/internal/schedule from the
// TypeScript extractor. The extractor reads cached media below its own data/
// directory, so run this from a disposable copy of the repository's server/
// and src/lib/ sources (with node_modules reachable), never from the checkout:
//   node <this file> > parity-media.json
import { pathToFileURL } from "node:url"
import path from "node:path"
const root = process.cwd()
const load = (file) => import(pathToFileURL(path.join(root, file)).href)
process.env.LLM_SECRETS_KEY = "synthetic-secrets-key"
const db = await load("server/database.ts")
const media = await load("server/media-cache.ts")
const { encryptSecret } = await load("server/secret-store.ts")
const { extractPendingSchedules } = await load("server/schedule-extractor.ts")

const database = db.initializeDatabase({ seed: false, filename: ":memory:" })
const key = encryptSecret("synthetic-provider-key", process.env.LLM_SECRETS_KEY)
for (const provider of [
  {
    id: "p-text",
    protocol: "openai-chat-completions",
    baseUrl: "https://chat.example.test/v1",
    model: "model-t",
    capabilities: ["text"],
  },
  {
    id: "p-vision",
    protocol: "openai-responses",
    baseUrl: "https://provider.example.test/v1",
    model: "model-v",
    capabilities: ["text", "image"],
  },
  {
    id: "p-anth",
    protocol: "anthropic-messages",
    baseUrl: "https://anthropic.example.test",
    model: "model-c",
    capabilities: ["text", "image"],
  },
  {
    id: "p-chat",
    protocol: "openai-chat-completions",
    baseUrl: "https://vision-chat.example.test/v1",
    model: "model-w",
    capabilities: ["image"],
  },
]) {
  db.upsertLlmProvider(database, {
    ...provider,
    name: provider.id,
    replaceApiKey: true,
    apiKeyCiphertext: key,
  })
}
db.setLlmRouteTargets(database, "schedule_vision", [
  { providerId: "p-text", modelId: "" },
  { providerId: "p-vision", modelId: "" },
  { providerId: "p-anth", modelId: "" },
  { providerId: "p-chat", modelId: "" },
])
db.setAppSetting(database, "schedule_message_enabled", "0")

const boardUrl =
  "https://pbs.twimg.com/media/synthetic-board.png?name=orig#frag"
const otherUrl = "https://pbs.twimg.com/media/synthetic-other.png"
const pendingUrl = "https://pbs.twimg.com/media/synthetic-pending.png"
const status = (id) => `https://x.com/synthetic_handle/status/${id}`
db.upsertPosts(database, [
  {
    id: "img-a",
    source: "x",
    type: "notice",
    label: "SCHEDULE / 日程",
    text: "今週のスケジュールです",
    published_at: "2026-04-28T03:00:00.000Z",
    url: status("img-a"),
    media_url: boardUrl,
    media_urls: [boardUrl, otherUrl],
  },
  {
    id: "img-b",
    source: "x",
    type: "notice",
    label: "SCHEDULE / 日程",
    text: "",
    published_at: "2026-04-27T03:00:00.000Z",
    url: status("img-b"),
    media_url: otherUrl,
    media_urls: [otherUrl],
  },
  {
    id: "img-c",
    source: "x",
    type: "daily",
    label: "",
    text: "来週の予定表",
    published_at: "2026-04-26T03:00:00.000Z",
    url: status("img-c"),
    media_url: pendingUrl,
    media_urls: [pendingUrl],
  },
  {
    id: "img-d",
    source: "x",
    type: "daily",
    label: "",
    text: "今日は22時から配信予定です",
    published_at: "2026-04-25T03:00:00.000Z",
    url: status("img-d"),
    media_url: otherUrl,
    media_urls: [otherUrl],
  },
])
const files = {}
for (const [index, sourceUrl] of [boardUrl, otherUrl].entries()) {
  const content = Buffer.from([
    0x89,
    0x50,
    0x4e,
    0x47,
    0x0d,
    0x0a,
    0x1a,
    0x0a,
    0x10 + index,
  ])
  const written = await media.writeMediaFileAtomic({
    content,
    extension: "png",
  })
  db.upsertMediaAsset(database, {
    source: "x",
    sourceUrl,
    status: "ready",
    cachePath: written.relativePath,
    mimeType: "image/png",
    sha256: written.sha256,
    byteSize: written.byteSize,
  })
  files[sourceUrl] = {
    relativePath: written.relativePath,
    sha256: written.sha256,
    base64: content.toString("base64"),
    mediaId: media.mediaIdForSourceUrl(sourceUrl),
  }
}
db.upsertAssets(database, [
  {
    id: "schedule-2026-04-27",
    kind: "schedule",
    url: boardUrl,
    source_url: status("img-a"),
    week_start: "2026-04-27",
  },
  {
    id: "weekly-schedule",
    kind: "schedule",
    url: boardUrl,
    source_url: status("img-a"),
    week_start: "2026-04-27",
  },
  {
    id: "schedule-2026-04-20",
    kind: "schedule",
    url: otherUrl,
    source_url: status("img-d"),
    week_start: "2026-04-20",
  },
  {
    id: "schedule-2026-04-13",
    kind: "schedule",
    url: pendingUrl,
    source_url: status("img-c"),
    week_start: "2026-04-13",
  },
  {
    id: "schedule-2026-04-06",
    kind: "schedule",
    url: otherUrl,
    source_url: null,
    week_start: "2026-04-06",
  },
])
// Review order follows these timestamps, so pin them instead of the clock.
const reviewOrder = [
  "schedule-2026-04-27",
  "weekly-schedule",
  "schedule-2026-04-20",
  "schedule-2026-04-13",
  "schedule-2026-04-06",
]
for (const [index, id] of reviewOrder.entries()) {
  database
    .prepare(
      "UPDATE schedule_asset_reviews SET updated_at = ? WHERE asset_id = ?",
    )
    .run(`2026-04-01T00:00:0${index + 1}.000Z`, id)
}
const mediaLinks = database
  .prepare(
    "SELECT media_id, owner_type, owner_id, role, position FROM media_links ORDER BY id",
  )
  .all()
const mediaAssets = database
  .prepare("SELECT id, source_url, status FROM media_assets ORDER BY id")
  .all()

const event = (title) => ({
  title,
  detail: null,
  date: "2099-05-03",
  time: "21:00",
  endTime: null,
  timePrecision: "exact",
  status: "scheduled",
  eventType: "stream",
  url: null,
  confidence: 0.9,
  evidence: "board",
})
const boardResult = {
  classification: "schedule",
  events: [event("歌枠")],
  confidence: 0.9,
  evidence: "board",
  reason: null,
  action: "add",
}
const imageOnlyResult = {
  classification: "not_schedule",
  events: [],
  confidence: 0.6,
  evidence: "art",
  reason: "illustration",
  action: "none",
}
const verdicts = [
  {
    classification: "Schedule ",
    confidence: 0.93,
    evidence: " weekly table ",
    reason: "grid of days",
  },
  {
    classification: "not_schedule",
    confidence: 0.2,
    evidence: "thumbnail",
    reason: "single card",
  },
]
let verified = 0
const requests = []
const json = (value, code = 200) =>
  new Response(JSON.stringify(value), {
    status: code,
    headers: { "content-type": "application/json" },
  })
const fetchImpl = async (url, init) => {
  const headers = { ...init.headers }
  for (const name of ["authorization", "x-api-key"])
    if (headers[name]) headers[name] = "<redacted>"
  requests.push({ url: String(url), headers, body: init.body })
  const host = new URL(url).host
  if (init.body.includes("kano_schedule_asset_verification")) {
    if (host === "provider.example.test" && verified > 0)
      return json({ error: "synthetic" }, 503)
    const verdict = verdicts[verified++]
    if (host === "anthropic.example.test")
      return json({
        content: [
          {
            type: "tool_use",
            name: "kano_schedule_asset_verification",
            input: verdict,
          },
        ],
      })
    return json({ output_text: JSON.stringify(verdict) })
  }
  const parts = JSON.parse(init.body).input[0].content
  return json({
    output_text: JSON.stringify(
      parts.some((part) => part.type === "input_text")
        ? boardResult
        : imageOnlyResult,
    ),
  })
}

const first = await extractPendingSchedules(database, { fetchImpl })
const dump = () => ({
  extractions: database
    .prepare(
      `SELECT id, source_item_id, content_fingerprint, extractor_version, model, status, result_json, error FROM schedule_extractions ORDER BY id`,
    )
    .all(),
  postStates: database
    .prepare(
      `SELECT post_id, status, route, last_error, last_extraction_id, reprocess_requested FROM post_llm_states ORDER BY post_id`,
    )
    .all(),
  reviews: database
    .prepare(
      `SELECT asset_id, llm_status, llm_confidence, llm_reason, llm_evidence, llm_model, llm_checked_at IS NOT NULL AS checked, llm_input_fingerprint, manual_status FROM schedule_asset_reviews ORDER BY asset_id`,
    )
    .all(),
  providers: database
    .prepare(
      `SELECT id, last_status, last_error FROM llm_providers ORDER BY id`,
    )
    .all(),
  events: database
    .prepare(
      `SELECT id, source_item_id, title, raw_json FROM events ORDER BY id`,
    )
    .all(),
})
const afterFirst = dump()
const firstRequests = requests.splice(0)
const second = await extractPendingSchedules(database, { fetchImpl })
process.stdout.write(
  JSON.stringify(
    {
      files,
      mediaLinks,
      mediaAssets,
      first,
      requests: firstRequests,
      afterFirst,
      second,
      secondRequests: requests,
      afterSecond: dump(),
    },
    null,
    1,
  ) + "\n",
)
