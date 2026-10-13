// Generates unit vectors for backend-go/internal/schedule from the TypeScript
// sources and the Node runtime. Run from the repository root:
//   node <this file> > backend-go/internal/schedule/testdata/vectors.json
import crypto from "node:crypto"
import { pathToFileURL } from "node:url"
import path from "node:path"
const load = (file) =>
  import(pathToFileURL(path.join(process.cwd(), file)).href)
const { isLikelyScheduleBoardText } = await load("server/schedule-asset.ts")
const { normalizeExtractionResult, normalizeScheduleAssetVerification } =
  await load("server/schedule-extractor.ts")
const { normalizeLlmCapabilities, normalizeLlmModelTags } =
  await load("server/database.ts")
const { mediaIdForSourceUrl } = await load("server/media-cache.ts")
const { providerApiRoot, inferenceEndpoint } = await load(
  "src/lib/llm-endpoints.ts",
)
const sha256 = (value) =>
  crypto.createHash("sha256").update(value).digest("hex")
const out = {}

out.board = [
  "今週のスケジュール",
  "Weekly　Schedule board",
  "weekly_schedule",
  "this week's schedule",
  "This Week schedule",
  "来週\nの予定",
  "今月のざっくり予定",
  "今週はとてもながい予定",
  "予定表です",
  "reschedule",
  "schedules",
  "配信予定",
  "SCHEDULE-TABLE",
  "週間 予定",
].map((text) => [text, isLikelyScheduleBoardText(text)])

out.numbers = [
  0, -0, 1, 0.1, 1e21, 1e-7, 123456789012345680000, 1.5e-10, 0.000001, 100,
  1e300, 5e-324, 0.30000000000000004, -12.5,
].map((n) => [n, String(n)])
const quotedInput = 'a"b\\c\b\f\n\r\t\u0001\u001f<>&  \u007f😀é'
out.quoted = [quotedInput, JSON.stringify(quotedInput)]
const reorderInput =
  '{"b":1,"2":{"x":1e3,"y":1.0},"a":[1,-0,2.50],"1":null,"b":3,"01":true}'
out.reorder = [reorderInput, JSON.stringify(JSON.parse(reorderInput))]

out.strings = [
  "ΑΣ",
  "ΑΣ.",
  "ΑΣa",
  "Σ",
  "aΣ b",
  "İstanbul",
  "ÁΣ",
  "ΑΣ́Β",
  "ＡＢＣ Schedule スケジュール",
  "﻿　 x \u0085 ",
  "\u0085x ",
].map((text) => [text, text.toLowerCase(), text.trim()])
out.slices = [
  ["a" + "😀".repeat(5), 4],
  ["a" + "😀".repeat(5), 5],
  ["😀😀", 1],
  ["日本語テキスト", 3],
  ["abc", 10],
].map(([text, limit]) => [
  text,
  limit,
  sha256(text.slice(0, limit)),
  text.slice(0, limit).length,
])

// Verbatim copy of the module-private helper in server/schedule-extractor.ts.
function youtubeIdFromUrl(value) {
  if (!value) return null
  try {
    const url = new URL(value)
    if (url.hostname === "youtu.be") return url.pathname.slice(1) || null
    if (url.hostname.endsWith("youtube.com")) {
      return (
        url.searchParams.get("v") ||
        url.pathname.match(/\/(?:live|shorts)\/([^/]+)/u)?.[1] ||
        null
      )
    }
  } catch {
    return null
  }
  return null
}
function describeUrl(value) {
  let href = null
  try {
    href = new URL(value).toString()
  } catch {}
  return [value, href, youtubeIdFromUrl(value), mediaIdForSourceUrl(value)]
}
out.urls = [
  'https://a.example.test/a^b`{}|c d"<>?q=1 \'"<>^`{}|#f "<>`{}^',
  "HTTPS://Example.TEST:443/a/../b/./c%2e/%2E%2e/d",
  "https:example.test",
  "https://example.test?",
  "https://example.test/#",
  "http://example.test:80/日本語?ク=え#フ",
  "  https://example.test/a\tb\n ",
  "https://youtu.be/abcDEF12345?t=3",
  "https://youtu.be/",
  "https://www.youtube.com/watch?v=abc%20d+e&x=1",
  "https://www.youtube.com/watch?x=1&v=&v=second",
  "https://m.youtube.com/live/XYZ/extra",
  "https://www.youtube.com/channel/UCsynthetic",
  "youtu.be/abc",
  "ftp://youtu.be/abc",
  "https://EXAMPLE.test:0443/",
  "https://[::1]:8080/v1",
  "https://127.0.0.1:11434",
  "https://example.test/a//b///",
  "https://example.test/..",
  "https://example.test/a/.",
  "https://example.test/%zz%41",
  "mailto:a@b",
  "https://",
  "https://example.test/a?b=c?d#e#f",
  "https://exa%6dple.test/x",
  "https://example.test:/x",
  "https://pbs.twimg.com/media/synthetic.png?name=orig#frag",
  "",
  "   ",
  "/assets/local.png",
].map(describeUrl)

// Deliberately malformed or userinfo-bearing inputs are assembled from their
// parts, here and in the Go test, so no data file stores them as links.
const urlParts = (value) => (value == null ? null : value.split("://"))
out.oddUrls = [
  ["https", "us er:p@ss:w@example.test:8443/x\\y"],
  ["https", "example.test:99999/"],
  ["https", "exa mple.test/"],
  ["https", "example.test:80a/x"],
  ["https", "a|b.test/"],
].map((pieces) => {
  const [, href, youtubeId, mediaId] = describeUrl(pieces.join("://"))
  return [pieces, urlParts(href), youtubeId, mediaId]
})

out.endpoints = [
  ["https://api.openai.com/v1", "openai-responses"],
  ["https://api.openai.com", "openai-chat-completions"],
  ["https://api.openai.com/v1/chat/completions/", "openai-chat-completions"],
  ["https://openrouter.ai", "openai-chat-completions"],
  ["https://openrouter.ai/api/", "openai-chat-completions"],
  ["https://generativelanguage.googleapis.com", "openai-chat-completions"],
  ["https://dashscope.aliyuncs.com/compatible-mode", "openai-responses"],
  ["http://localhost:11434/api", "anthropic-messages"],
  ["https://API.Example.test:443/", "anthropic-messages"],
  [
    "  https://provider.example.test/custom/path/messages ",
    "anthropic-messages",
  ],
  ["provider.example.test", "openai-responses"],
  ["", "openai-responses"],
  ["https://provider.example.test?x=1", "openai-responses"],
  ["https://provider.example.test/v1/models", "unknown-protocol"],
].map(([base, protocol]) => [
  base,
  protocol,
  providerApiRoot(base),
  inferenceEndpoint(base, protocol),
])

const post = { id: "post-1", url: "https://x.com/synthetic_handle/status/1" }
const base = {
  title: "T",
  detail: null,
  date: "2099-01-02",
  time: null,
  endTime: null,
  timePrecision: "exact",
  status: "ok",
  eventType: "event",
  url: null,
  confidence: 0.5,
  evidence: "",
}
const cases = {
  invalidClassification: { classification: "maybe", events: [] },
  eventsInNotSchedule: { classification: "not_schedule", events: [base] },
  eventsInUncertain: { classification: "uncertain", events: [base] },
  badDate: { events: [{ ...base, date: "2099-02-30" }] },
  earlyYear: { events: [{ ...base, date: "0099-01-01" }] },
  badTime: { events: [{ ...base, time: "24:00" }] },
  emptyTime: { events: [{ ...base, time: "" }] },
  endWithoutStart: { events: [{ ...base, endTime: "10:00" }] },
  badPrecision: { events: [{ ...base, timePrecision: "Exact" }] },
  badType: { events: [{ ...base, eventType: "party" }] },
  badConfidence: { events: [{ ...base, confidence: 1.5 }] },
  stringConfidence: { events: [{ ...base, confidence: " 0.25 " }] },
  nullConfidence: { events: [{ ...base, confidence: null }] },
  missingConfidence: { events: [{ ...base, confidence: undefined }] },
  blankTitle: { events: [{ ...base, title: " 　 " }] },
  nullEvent: { events: [null] },
  badRootConfidence: {
    classification: "schedule",
    events: [],
    confidence: "x",
  },
  badAction: { classification: "schedule", events: [], action: "delete" },
  legacyNoClassification: {
    events: [
      {
        ...base,
        time: "23:30",
        endTime: "00:15",
        status: "キャンセル",
        url: "https://youtu.be/abcdefghijk?t=1",
        detail: "  d  ",
        evidence: " e ",
      },
    ],
  },
  pastEvent: {
    classification: "schedule",
    events: [
      {
        ...base,
        date: "2020-02-29",
        time: "00:00",
        endTime: "00:00",
        timePrecision: "approximate",
        status: " Canceled soon ",
        url: "javascript:alert(1)",
      },
    ],
    confidence: null,
  },
  emptyLegacy: {},
  nullResult: null,
  defaults: {
    classification: " SCHEDULE ",
    events: [
      {
        ...base,
        title: 12,
        status: null,
        url: "https://m.youtube.com/shorts/short_id-1/x",
      },
    ],
    action: " UPDATE ",
    evidence: 5,
    reason: " r ",
  },
}
Date.now = () => Date.parse("2026-04-01T00:00:00.000Z")
out.normalized = Object.entries(cases).map(([name, value]) => {
  const input = JSON.stringify(value)
  try {
    return [
      name,
      input,
      JSON.stringify(normalizeExtractionResult(value, post)),
      null,
    ]
  } catch (error) {
    return [name, input, null, error.message]
  }
})
const verdicts = {
  ok: {
    classification: " Uncertain",
    confidence: "0.5",
    evidence: 1,
    reason: null,
  },
  badClass: { classification: "yes", confidence: 0.5 },
  badConfidence: { classification: "schedule", confidence: -1 },
  missing: {},
}
out.verification = Object.entries(verdicts).map(([name, value]) => {
  try {
    return [
      name,
      JSON.stringify(value),
      JSON.stringify(normalizeScheduleAssetVerification(value)),
      null,
    ]
  } catch (error) {
    return [name, JSON.stringify(value), null, error.message]
  }
})

out.capabilities = [
  ['["text","image"]', true],
  ['["Vision"]', false],
  ["text, image", false],
  ["", true],
  ["", false],
  ['"text"', true],
  ["[]", false],
  ['["image","text","image"]', true],
  ["audio", true],
  ['["TEXT "]', true],
].map(([raw, vision]) => [raw, vision, normalizeLlmCapabilities(raw, vision)])
out.tags = [
  '["text"]',
  '["image","reasoning","text"]',
  '["vision","tools"]',
  "[]",
  "embedding",
  '["reasoning"]',
  "text image",
].map((raw) => [
  raw,
  normalizeLlmModelTags(raw).filter((tag) => ["text", "image"].includes(tag)),
])

process.stdout.write(JSON.stringify(out, null, 1) + "\n")
