// Generates the parity fixture for backend-go/internal/schedule from the
// TypeScript extractor. Run from the repository root:
//   node <this file> > backend-go/internal/schedule/testdata/parity.json
import { pathToFileURL } from "node:url"
import path from "node:path"
const root = process.cwd()
const load = (file) => import(pathToFileURL(path.join(root, file)).href)
process.env.LLM_SECRETS_KEY = "synthetic-secrets-key"
const db = await load("server/database.ts")
const { encryptSecret } = await load("server/secret-store.ts")
const { extractPendingSchedules } = await load("server/schedule-extractor.ts")

const database = db.initializeDatabase({ seed: false, filename: ":memory:" })
const key = encryptSecret("synthetic-provider-key", process.env.LLM_SECRETS_KEY)
for (const provider of [
  {
    id: "p-resp",
    protocol: "openai-responses",
    baseUrl: "https://provider.example.test/v1",
    model: "model-a",
  },
  {
    id: "p-chat",
    protocol: "openai-chat-completions",
    baseUrl: "https://chat.example.test",
    model: "model-b",
  },
  {
    id: "p-anth",
    protocol: "anthropic-messages",
    baseUrl: "https://anthropic.example.test/v1/",
    model: "model-c",
  },
]) {
  db.upsertLlmProvider(database, {
    ...provider,
    name: provider.id,
    replaceApiKey: true,
    apiKeyCiphertext: key,
  })
}
db.setLlmRouteTargets(database, "schedule_message", [
  { providerId: "p-resp", modelId: "" },
])
db.setLlmRouteTargets(database, "schedule_board", [
  { providerId: "p-chat", modelId: "" },
  { providerId: "p-anth", modelId: "" },
])

const posts = [
  {
    id: "post-a",
    label: "SCHEDULE / 日程",
    text: "今週のスケジュール 😀 です\n5/3 21:00 歌枠 <b>& ",
    published_at: "2026-04-28T03:00:00.000Z",
  },
  {
    id: "post-c",
    label: "",
    text: "予定していた5/3の配信は中止です",
    published_at: "2026-04-27T03:00:00.000Z",
  },
  {
    id: "post-b",
    label: "",
    text: "明日 20時から歌います",
    published_at: "2026-04-26T03:00:00.000Z",
  },
  {
    id: "post-d",
    label: "",
    text: "おはよう",
    published_at: "2026-04-25T03:00:00.000Z",
  },
]
const insert = database.prepare(
  "INSERT INTO posts (id, source, account_handle, type, label, text, published_at, url, raw_json) VALUES (?, 'x', 'synthetic_handle', 'daily', ?, ?, ?, ?, ?)",
)
for (const post of posts) {
  const url = `https://x.com/synthetic_handle/status/${post.id}`
  insert.run(
    post.id,
    post.label,
    post.text,
    post.published_at,
    url,
    JSON.stringify({ id: post.id, text: post.text }),
  )
}

const longTitle = "a" + "😀".repeat(130)
const scheduleResult = {
  classification: "schedule",
  events: [
    {
      title: "  歌枠 Singing Stream ",
      detail: "詳細 ",
      date: "2099-05-03",
      time: "21:00",
      endTime: "01:30",
      timePrecision: "exact",
      status: "scheduled",
      eventType: "stream",
      url: null,
      confidence: 0.9,
      evidence: "5/3 21:00 歌枠",
    },
    {
      title: "メン限 ΑΣ İ",
      detail: null,
      date: "2099-05-04",
      time: null,
      endTime: null,
      timePrecision: "exact",
      status: "",
      eventType: "member",
      url: "https://www.youtube.com/watch?v=abcdefghijk&t=1",
      confidence: 1,
      evidence: "",
    },
    {
      title: "Old Event",
      detail: "",
      date: "2020-01-02",
      time: "09:05",
      endTime: null,
      timePrecision: "approximate",
      status: "Cancelled",
      eventType: "event",
      url: "HTTPS://Example.TEST/a b?q=1#f",
      confidence: 0.25,
      evidence: "e",
    },
    {
      title: longTitle,
      detail: "d",
      date: "2099-05-05",
      time: null,
      endTime: null,
      timePrecision: "unknown",
      status: "  ",
      eventType: "release",
      url: "ftp://example.test/x",
      confidence: 1e-7,
      evidence: "long",
    },
  ],
  confidence: 0.8,
  evidence: "ev <tag>",
  reason: null,
  action: "add",
}
const cancelResult = {
  classification: "schedule",
  events: [
    {
      title: "歌枠・Singing Stream！",
      detail: null,
      date: "2099-05-03",
      time: "21:00",
      endTime: null,
      timePrecision: "exact",
      status: "cancelled",
      eventType: "stream",
      url: null,
      confidence: 0.7,
      evidence: "中止です",
    },
  ],
  confidence: 0.7,
  evidence: "中止です",
  reason: "配信中止",
  action: "cancel",
}
const uncertainResult = {
  classification: "uncertain",
  events: [],
  confidence: 0.4,
  evidence: "",
  reason: "曖昧",
  action: "none",
}

const requests = []
const json = (value, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: { "content-type": "application/json" },
  })
const fetchImpl = async (url, init) => {
  const headers = { ...init.headers }
  for (const name of ["authorization", "x-api-key"])
    if (headers[name]) headers[name] = "<redacted>"
  requests.push({
    url: String(url),
    method: init.method,
    headers,
    body: init.body,
  })
  const host = new URL(url).host
  if (host === "chat.example.test") {
    if (init.body.includes("中止")) return json({ error: "synthetic" }, 500)
    return json({
      choices: [{ message: { content: JSON.stringify(scheduleResult) } }],
    })
  }
  if (host === "anthropic.example.test") {
    return json({
      content: [
        { type: "text", text: "x" },
        {
          type: "tool_use",
          name: "kano_schedule_extraction",
          input: cancelResult,
        },
      ],
    })
  }
  return json({
    output: [
      { type: "reasoning" },
      {
        type: "message",
        content: [
          { type: "output_text", text: JSON.stringify(uncertainResult) },
        ],
      },
    ],
  })
}

const first = await extractPendingSchedules(database, { fetchImpl })
const dump = () => ({
  events: database
    .prepare(
      `SELECT id, source, source_item_id, source_key, title, detail, starts_on, starts_at, ends_at, timezone, time_precision, status, event_type, url, is_upcoming, provenance, manual_locked, confidence, extraction_id, cancellation_status, cancellation_source, cancellation_reason, cancellation_evidence, cancellation_source_item_id, cancellation_confidence, deleted_at IS NOT NULL AS deleted, raw_json FROM events ORDER BY id`,
    )
    .all(),
  eventSources: database
    .prepare(
      `SELECT event_id, source, source_item_id, source_key, url, raw_json, deleted_at IS NOT NULL AS deleted FROM event_sources ORDER BY id`,
    )
    .all(),
  extractions: database
    .prepare(
      `SELECT id, source, source_item_id, content_fingerprint, extractor_version, model, status, result_json, error FROM schedule_extractions ORDER BY id`,
    )
    .all(),
  postStates: database
    .prepare(
      `SELECT post_id, status, route, last_attempt_at IS NOT NULL AS attempted, last_processed_at IS NOT NULL AS processed, last_error, last_extraction_id, reprocess_requested FROM post_llm_states ORDER BY post_id`,
    )
    .all(),
  providers: database
    .prepare(
      `SELECT id, last_status, last_error FROM llm_providers ORDER BY id`,
    )
    .all(),
})
const afterFirst = dump()
const firstRequests = requests.splice(0)
const second = await extractPendingSchedules(database, { fetchImpl })
process.stdout.write(
  JSON.stringify(
    {
      first,
      requests: firstRequests,
      afterFirst,
      second,
      secondRequests: requests.length,
      afterSecond: dump(),
    },
    null,
    1,
  ) + "\n",
)
