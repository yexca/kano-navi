import assert from "node:assert/strict"
import crypto from "node:crypto"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import test from "node:test"

import { createApp } from "./app.ts"
import { listeningPort } from "./http-address.ts"
import { initializeDatabase } from "./database.ts"
import { fetchRemoteModels } from "./llm-catalog.ts"
import { readLlmJson } from "./llm-http.ts"
import { inferenceEndpoint, modelsEndpoint } from "../src/lib/llm-endpoints.ts"
import {
  callOpenAiScheduleExtraction,
  callOpenAiScheduleAssetVerification,
} from "./schedule-extractor.ts"

test("provider endpoints accept host-only URLs and preserve explicit API roots", () => {
  for (const base of [
    "https://api.example.invalid",
    "https://api.example.invalid/",
    "https://api.example.invalid/v1/",
    "https://api.example.invalid/v1/messages",
  ]) {
    assert.equal(modelsEndpoint(base), "https://api.example.invalid/v1/models")
    assert.equal(
      inferenceEndpoint(base, "anthropic-messages"),
      "https://api.example.invalid/v1/messages",
    )
    assert.equal(
      inferenceEndpoint(base, "openai-responses"),
      "https://api.example.invalid/v1/responses",
    )
    assert.equal(
      inferenceEndpoint(base, "openai-chat-completions"),
      "https://api.example.invalid/v1/chat/completions",
    )
  }
  assert.equal(
    modelsEndpoint("https://openrouter.ai"),
    "https://openrouter.ai/api/v1/models",
  )
  assert.equal(
    modelsEndpoint("https://generativelanguage.googleapis.com"),
    "https://generativelanguage.googleapis.com/v1beta/openai/models",
  )
  for (const prefix of ["/gateway/v2", "/v1beta/openai", "/custom-root"]) {
    assert.equal(
      modelsEndpoint(new URL(prefix, "https://api.example.invalid").href),
      new URL(`${prefix}/models`, "https://api.example.invalid").href,
    )
  }
})

test("Anthropic discovery paginates with bounded requests and retains display names", async () => {
  const calls = []
  const apiKey = crypto.randomBytes(16).toString("hex")
  const models = await fetchRemoteModels(
    {
      baseUrl: "https://api.example.invalid",
      apiKey,
      protocol: "anthropic-messages",
    },
    {
      fetchImpl: async (url, options) => {
        calls.push(url)
        assert.equal(options.headers["x-api-key"], apiKey)
        assert.equal(options.headers["anthropic-version"], "2023-06-01")
        assert.equal(options.headers.authorization, undefined)
        assert.equal(options.redirect, "error")
        return Response.json(
          calls.length === 1
            ? {
                data: [{ id: "claude-example-a", display_name: "Claude A" }],
                has_more: true,
                last_id: "claude-example-a",
              }
            : {
                data: [{ id: "claude-example-b", display_name: "Claude B" }],
                has_more: false,
              },
        )
      },
    },
  )
  assert.deepEqual(calls, [
    "https://api.example.invalid/v1/models?limit=100",
    "https://api.example.invalid/v1/models?limit=100&after_id=claude-example-a",
  ])
  assert.deepEqual(
    models.map(({ id, name }) => [id, name]),
    [
      ["claude-example-a", "Claude A"],
      ["claude-example-b", "Claude B"],
    ],
  )
  let repeats = 0
  await assert.rejects(
    fetchRemoteModels(
      {
        baseUrl: "https://api.example.invalid",
        apiKey,
        protocol: "anthropic-messages",
      },
      {
        fetchImpl: async () => {
          repeats += 1
          return Response.json({
            data: [],
            has_more: true,
            last_id: "repeated-cursor",
          })
        },
      },
    ),
    /model list request failed/u,
  )
  assert.equal(repeats, 2)
})

test("LLM responses cancel oversized streams without relying on Content-Length", async () => {
  let cancelled = false
  const response = new Response(
    new ReadableStream({
      pull(controller) {
        controller.enqueue(new Uint8Array(16))
      },
      cancel() {
        cancelled = true
      },
    }),
  )
  await assert.rejects(readLlmJson(response, 8), /LLM response too large/u)
  assert.equal(cancelled, true)
})

test("Anthropic extraction and image verification use cached images and schema tools", async () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "kano-anthropic-"))
  const filePath = path.join(directory, "image.png")
  fs.writeFileSync(filePath, Buffer.from("synthetic-image"))
  const image = { filePath, mimeType: "image/png" }
  const result = {
    classification: "not_schedule",
    confidence: 0.9,
    evidence: "synthetic",
    events: [],
    action: "none",
  }
  const bodies = []
  const options = {
    apiKey: crypto.randomBytes(16).toString("hex"),
    model: "claude-example",
    protocol: "anthropic-messages",
    baseUrl: "https://provider.example.invalid",
    endpoint: undefined,
    fetchImpl: async (url, options) => {
      assert.equal(url, "https://provider.example.invalid/v1/messages")
      assert.equal(options.headers["anthropic-version"], "2023-06-01")
      assert.equal(options.redirect, "error")
      const body = JSON.parse(options.body)
      bodies.push(body)
      assert.equal(body.max_tokens > 0, true)
      assert.deepEqual(body.tool_choice, {
        type: "tool",
        name: body.tools[0].name,
      })
      assert.equal(body.tools[0].input_schema.type, "object")
      assert.equal(body.system.length > 0, true)
      return Response.json({
        content: [
          { type: "thinking", thinking: "ignored" },
          { type: "tool_use", name: body.tools[0].name, input: result },
        ],
      })
    },
  }
  try {
    for (const inputMode of ["text", "image", "text_image"]) {
      assert.deepEqual(
        await callOpenAiScheduleExtraction(
          { text: "synthetic source", publishedAt: "2026-09-01T00:00:00Z" },
          [image],
          { ...options, inputMode },
        ),
        result,
      )
      const content = bodies.at(-1).messages[0].content
      assert.equal(
        content.some((item) => item.type === "text"),
        inputMode !== "image",
      )
      assert.equal(
        content.some((item) => item.type === "image"),
        inputMode !== "text",
      )
      if (inputMode !== "text") {
        assert.deepEqual(content[0].source, {
          type: "base64",
          media_type: "image/png",
          data: Buffer.from("synthetic-image").toString("base64"),
        })
      }
    }
    await callOpenAiScheduleAssetVerification(
      { sourceUrl: "https://example.invalid/source", text: "must stay out" },
      image,
      options,
    )
    assert.equal(
      bodies.at(-1).tools[0].name,
      "kano_schedule_asset_verification",
    )
    assert.equal(JSON.stringify(bodies.at(-1)).includes("must stay out"), false)
    await assert.rejects(
      callOpenAiScheduleExtraction(
        { text: "synthetic", publishedAt: "2026-09-01T00:00:00Z" },
        [],
        {
          ...options,
          fetchImpl: async () =>
            Response.json({
              content: [{ type: "text", text: "no structured result" }],
            }),
        },
      ),
      /structured output/u,
    )
  } finally {
    fs.rmSync(directory, { recursive: true, force: true })
  }
})

test("admin supports Anthropic CRUD, model discovery, and inference connection tests", async () => {
  const previousKey = process.env.LLM_SECRETS_KEY
  process.env.LLM_SECRETS_KEY = crypto.randomBytes(32).toString("base64")
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const apiKey = crypto.randomBytes(16).toString("hex")
  const requests = []
  const app = createApp({
    database,
    databaseLabel: ":memory:",
    adminMode: "development",
    adminFetchImpl: async (url, options) => {
      requests.push({ url, method: options.method })
      assert.equal(options.headers["x-api-key"], apiKey)
      assert.equal(options.redirect, "error")
      if (options.method === "GET")
        return Response.json({
          data: [{ id: "claude-example", display_name: "Claude Example" }],
        })
      const body = JSON.parse(options.body)
      assert.equal(body.model, "claude-example")
      assert.equal(body.messages[0].content, "ping")
      assert.equal(body.max_tokens, 1)
      assert.equal(body.input, undefined)
      return Response.json({ content: [{ type: "text", text: "ok" }] })
    },
  })
  const server = app.listen(0, "127.0.0.1")
  await new Promise<void>((resolve) => server.once("listening", resolve))
  const origin = `http://127.0.0.1:${listeningPort(server)}/api/admin`
  const request = async (suffix, body = {}) => {
    const response = await fetch(`${origin}${suffix}`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    })
    assert.equal(response.ok, true)
    const payload = await response.json()
    assert.equal(JSON.stringify(payload).includes(apiKey), false)
    return payload
  }
  try {
    const created = await request("/providers", {
      id: "anthropic-example",
      name: "Anthropic",
      baseUrl: "https://provider.example.invalid",
      protocol: "anthropic-messages",
      apiKey,
    })
    assert.equal(created.provider.protocol, "anthropic-messages")
    const discovered = await request(
      "/providers/anthropic-example/models/discover",
    )
    assert.equal(discovered.models[0].id, "claude-example")
    await request("/providers/anthropic-example/models", {
      origin: "remote",
      models: [{ id: "claude-example" }],
    })
    await request("/providers/anthropic-example/test", {
      model: "claude-example",
    })
    assert.deepEqual(
      requests.map(({ url }) => url),
      [
        "https://provider.example.invalid/v1/models?limit=100",
        "https://provider.example.invalid/v1/messages",
      ],
    )
  } finally {
    await new Promise<void>((resolve) => server.close(() => resolve()))
    database.close()
    if (previousKey == null) delete process.env.LLM_SECRETS_KEY
    else Reflect.set(process.env, "LLM_SECRETS_KEY", previousKey)
  }
})
