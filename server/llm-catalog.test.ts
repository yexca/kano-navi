import { listeningPort } from "./http-address.ts"
import assert from "node:assert/strict"
import crypto from "node:crypto"
import test from "node:test"

import { createApp } from "./app.ts"
import {
  getLlmProvider,
  getLlmRouteTargets,
  getScheduleProviderOrders,
  initializeDatabase,
  listLlmModels,
  setLlmRouteTargets,
  upsertLlmModels,
  upsertLlmProvider,
} from "./database.ts"
import {
  fetchRemoteModels,
  inferModelTags,
  modelsEndpoint,
  parseModelList,
} from "./llm-catalog.ts"
import { extractSchedulePost } from "./schedule-extractor.ts"
import { encryptSecret } from "./secret-store.ts"

function setRuntimeEnv(name, value) {
  if (value == null) delete process.env[name]
  else Reflect.set(process.env, name, value)
}

async function listen(app) {
  return new Promise((resolve) => {
    const server = app.listen(0, "127.0.0.1", () => resolve(server))
  })
}

async function close(server) {
  if (!server) return
  await new Promise<void>((resolve, reject) =>
    server.close((error: Error) => (error ? reject(error) : resolve())),
  )
}

async function jsonRequest(origin, path, options: Record<string, any> = {}) {
  const response = await fetch(`${origin}${path}`, {
    ...options,
    headers: {
      accept: "application/json",
      ...(options.body ? { "content-type": "application/json" } : {}),
      ...(options.headers || {}),
    },
  })
  const payload = await response.json().catch(() => null)
  return { response, payload }
}

test("model tags are inferred from common model identifiers", () => {
  assert.deepEqual(inferModelTags("gpt-4o-mini"), ["text", "image", "tools"])
  assert.deepEqual(inferModelTags("deepseek-reasoner-r1"), [
    "text",
    "reasoning",
  ])
  assert.deepEqual(inferModelTags("text-embedding-3-small"), ["embedding"])
  assert.deepEqual(inferModelTags("dall-e-3"), [])
  assert.deepEqual(inferModelTags("Qwen/Qwen2.5-VL-72B-Instruct"), [
    "text",
    "image",
    "tools",
  ])
  assert.deepEqual(inferModelTags("claude-sonnet-5"), [
    "text",
    "image",
    "reasoning",
    "tools",
  ])
  assert.deepEqual(inferModelTags("plain-chat-model"), ["text"])
})

test("model lists are parsed from compatible payload shapes", () => {
  assert.equal(
    modelsEndpoint("https://api.example.invalid/v1/chat/completions"),
    "https://api.example.invalid/v1/models",
  )
  assert.equal(
    modelsEndpoint("https://api.example.invalid/v1/"),
    "https://api.example.invalid/v1/models",
  )
  assert.deepEqual(
    parseModelList({
      data: [
        { id: "zeta-model", owned_by: "example" },
        { id: "alpha/model:free" },
        { id: "alpha/model:free" },
        { id: "bad model id" },
        { id: "" },
      ],
    }).map((model) => model.id),
    ["alpha/model:free", "zeta-model"],
  )
  assert.deepEqual(
    parseModelList({ models: [{ name: "models/gemini-example" }] }).map(
      (model) => model.id,
    ),
    ["gemini-example"],
  )
  assert.deepEqual(
    parseModelList(["one", "two"]).map((model) => model.id),
    ["one", "two"],
  )
})

test("remote model errors never include the upstream body", async () => {
  await assert.rejects(
    fetchRemoteModels(
      { baseUrl: "https://api.example.invalid/v1", apiKey: "placeholder" },
      {
        fetchImpl: async () =>
          new Response("upstream-secret-placeholder", { status: 401 }),
      },
    ),
    (error: Error) =>
      error.message === "model list request failed" &&
      !error.message.includes("upstream"),
  )
})

test("catalog models keep default-model capabilities in sync", () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    upsertLlmProvider(database, {
      id: "catalog",
      name: "Catalog",
      baseUrl: "https://catalog.example.invalid/v1",
      model: "default-model",
      capabilities: ["text", "image"],
    })
    assert.deepEqual(
      listLlmModels(database, { providerId: "catalog" }).map((model) => [
        model.id,
        model.tags,
      ]),
      [["default-model", ["text", "image"]]],
    )
    upsertLlmModels(database, "catalog", [
      { id: "default-model", tags: ["text", "reasoning"] },
      { id: "second-model", tags: ["text", "image", "tools"] },
    ])
    assert.deepEqual(getLlmProvider(database, "catalog").capabilities, ["text"])
    upsertLlmProvider(database, {
      ...getLlmProvider(database, "catalog"),
      capabilities: ["text", "image"],
    })
    assert.deepEqual(
      listLlmModels(database, { providerId: "catalog" })[0].tags,
      ["text", "image", "reasoning"],
    )

    setLlmRouteTargets(database, "schedule_message", [
      { providerId: "catalog", modelId: "second-model" },
      { providerId: "catalog", modelId: "" },
      { providerId: "catalog", modelId: "second-model" },
    ])
    assert.deepEqual(getLlmRouteTargets(database, "schedule_message"), [
      { providerId: "catalog", modelId: "second-model" },
      { providerId: "catalog", modelId: "" },
    ])
    assert.deepEqual(getScheduleProviderOrders(database).schedule_message, [
      "catalog",
    ])
    assert.throws(() =>
      setLlmRouteTargets(database, "schedule_message", [
        { providerId: "catalog", modelId: "missing-model" },
      ]),
    )
  } finally {
    database.close()
  }
})

test("provider center discovers, tags, and routes catalog models", async () => {
  const previousSecretsKey = process.env.LLM_SECRETS_KEY
  setRuntimeEnv("LLM_SECRETS_KEY", crypto.randomBytes(32).toString("base64"))
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const requests = []
  let server
  try {
    server = await listen(
      createApp({
        database,
        databaseLabel: ":memory:",
        adminMode: "development",
        adminFetchImpl: async (url, options) => {
          requests.push({ url, method: options.method })
          assert.match(options.headers.authorization, /^Bearer /u)
          return new Response(
            JSON.stringify({
              data: [
                { id: "vision-chat-gpt-4o", owned_by: "example" },
                { id: "text-embedding-small" },
              ],
            }),
            { status: 200, headers: { "content-type": "application/json" } },
          )
        },
      }),
    )
    const origin = `http://127.0.0.1:${listeningPort(server)}`
    const created = await jsonRequest(origin, "/api/admin/providers", {
      method: "POST",
      body: JSON.stringify({
        id: "center",
        name: "Center",
        protocol: "openai-chat-completions",
        baseUrl: "https://center.example.invalid/v1",
        apiKey: crypto.randomBytes(16).toString("hex"),
      }),
    })
    assert.equal(created.response.status, 201)
    assert.equal(created.payload.provider.model, "")
    // A provider without a default model is not appended to legacy routes.
    assert.equal(
      getScheduleProviderOrders(database).schedule_board.includes("center"),
      false,
    )

    const discovered = await jsonRequest(
      origin,
      "/api/admin/providers/center/models/discover",
      { method: "POST" },
    )
    assert.equal(discovered.response.status, 200)
    assert.equal(requests[0].url, "https://center.example.invalid/v1/models")
    assert.equal(requests[0].method, "GET")
    assert.deepEqual(
      discovered.payload.models.map((model) => [
        model.id,
        model.suggestedTags,
        model.added,
      ]),
      [
        ["text-embedding-small", ["embedding"], false],
        ["vision-chat-gpt-4o", ["text", "image", "tools"], false],
      ],
    )

    const added = await jsonRequest(
      origin,
      "/api/admin/providers/center/models",
      {
        method: "POST",
        body: JSON.stringify({
          origin: "remote",
          models: [{ id: "vision-chat-gpt-4o" }],
        }),
      },
    )
    assert.equal(added.response.status, 201)
    assert.equal(added.payload.provider.model, "vision-chat-gpt-4o")
    assert.deepEqual(added.payload.models[0].tags, ["text", "image", "tools"])
    assert.equal(added.payload.models[0].origin, "remote")

    const retagged = await jsonRequest(
      origin,
      "/api/admin/providers/center/models",
      {
        method: "PUT",
        body: JSON.stringify({
          id: "vision-chat-gpt-4o",
          tags: ["text", "reasoning"],
        }),
      },
    )
    assert.equal(retagged.response.status, 200)
    assert.deepEqual(retagged.payload.model.capabilities, ["text"])
    const badTag = await jsonRequest(
      origin,
      "/api/admin/providers/center/models",
      {
        method: "PUT",
        body: JSON.stringify({ id: "vision-chat-gpt-4o", tags: ["magic"] }),
      },
    )
    assert.equal(badTag.response.status, 400)

    const routed = await jsonRequest(
      origin,
      "/api/admin/routes/schedule_board",
      {
        method: "PUT",
        body: JSON.stringify({
          targets: [{ providerId: "center", modelId: "vision-chat-gpt-4o" }],
        }),
      },
    )
    assert.equal(routed.response.status, 200)
    assert.deepEqual(routed.payload.routeTargets.schedule_board, [
      { providerId: "center", modelId: "vision-chat-gpt-4o" },
    ])
    const missing = await jsonRequest(
      origin,
      "/api/admin/routes/schedule_board",
      {
        method: "PUT",
        body: JSON.stringify({
          targets: [{ providerId: "center", modelId: "unknown-model" }],
        }),
      },
    )
    assert.equal(missing.response.status, 400)

    const removed = await jsonRequest(
      origin,
      "/api/admin/providers/center/models/remove",
      {
        method: "POST",
        body: JSON.stringify({ ids: ["vision-chat-gpt-4o"] }),
      },
    )
    assert.equal(removed.response.status, 200)
    assert.equal(removed.payload.removed, 1)
    assert.equal(removed.payload.provider.model, "")
    assert.deepEqual(getLlmRouteTargets(database, "schedule_board"), [])
  } finally {
    await close(server)
    database.close()
    setRuntimeEnv("LLM_SECRETS_KEY", previousSecretsKey)
  }
})

test("extraction uses the routed catalog model and its capabilities", async () => {
  const previousSecretsKey = process.env.LLM_SECRETS_KEY
  setRuntimeEnv("LLM_SECRETS_KEY", ["catalog", "route", "secret"].join("-"))
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    upsertLlmProvider(database, {
      id: "routed",
      name: "Routed",
      baseUrl: "https://routed.example.invalid/v1",
      model: "",
      replaceApiKey: true,
      apiKeyCiphertext: encryptSecret(
        "placeholder-key",
        process.env.LLM_SECRETS_KEY,
      ),
    })
    upsertLlmModels(database, "routed", [
      { id: "image-only-model", tags: ["image"] },
      { id: "text-model", tags: ["text"] },
    ])
    setLlmRouteTargets(database, "schedule_message", [
      { providerId: "routed", modelId: "image-only-model" },
      { providerId: "routed", modelId: "text-model" },
    ])
    const models = []
    const result = await extractSchedulePost(
      database,
      {
        id: "catalog-route-post",
        source: "x",
        text: "9月6日に配信予定",
        publishedAt: "2026-08-28T01:00:00.000Z",
        url: "https://x.com/example/status/catalog-route-post",
      },
      {
        detectionType: "message",
        fetchImpl: async (_url, options) => {
          models.push(JSON.parse(options.body).model)
          return new Response(
            JSON.stringify({
              output_text: JSON.stringify({
                classification: "not_schedule",
                events: [],
                confidence: 0.9,
                evidence: "synthetic",
                action: "none",
              }),
            }),
            { status: 200, headers: { "content-type": "application/json" } },
          )
        },
      },
    )
    assert.equal(result.status, "success")
    assert.deepEqual(models, ["text-model"])
  } finally {
    database.close()
    setRuntimeEnv("LLM_SECRETS_KEY", previousSecretsKey)
  }
})

test("provider-only routes migrate once into model route targets", async () => {
  const fs = await import("node:fs")
  const os = await import("node:os")
  const path = await import("node:path")
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "kano-routes-"))
  const filename = path.join(directory, "routes.sqlite")
  try {
    let database = initializeDatabase({ seed: false, filename })
    upsertLlmProvider(database, {
      id: "legacy",
      name: "Legacy",
      baseUrl: "https://legacy.example.invalid/v1",
      model: "legacy-model",
      capabilities: ["text"],
    })
    // Recreate a pre-catalog database: routes only in the old table.
    database.exec("DELETE FROM llm_route_targets; DELETE FROM llm_models;")
    database
      .prepare<unknown[], Record<string, any>>(
        "DELETE FROM app_settings WHERE key = ?",
      )
      .run("llm_route_targets_migrated")
    database
      .prepare<unknown[], Record<string, any>>(
        `INSERT INTO llm_route_providers (route, provider_id, priority, created_at, updated_at)
         VALUES ('schedule_vision', 'legacy', 0, ?, ?)`,
      )
      .run(new Date().toISOString(), new Date().toISOString())
    database.close()

    database = initializeDatabase({ seed: false, filename })
    assert.deepEqual(getLlmRouteTargets(database, "schedule_vision"), [
      { providerId: "legacy", modelId: "" },
    ])
    assert.deepEqual(getLlmRouteTargets(database, "schedule_board"), [
      { providerId: "legacy", modelId: "" },
    ])
    assert.deepEqual(
      listLlmModels(database).map((model) => [model.id, model.origin]),
      [["legacy-model", "legacy"]],
    )
    // Emptying a route later must not resurrect the old provider order.
    setLlmRouteTargets(database, "schedule_vision", [])
    database.close()
    database = initializeDatabase({ seed: false, filename })
    assert.deepEqual(getLlmRouteTargets(database, "schedule_vision"), [])
    database.close()
  } finally {
    fs.rmSync(directory, { recursive: true, force: true })
  }
})
