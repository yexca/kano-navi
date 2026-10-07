import assert from "node:assert/strict"
import crypto from "node:crypto"
import test from "node:test"

import { createApp } from "./app.js"
import {
  getLlmProvider,
  getLlmProviderSecret,
  initializeDatabase,
} from "./database.js"
import { decryptSecret } from "./secret-store.js"

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
  await new Promise((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  )
}

async function jsonRequest(origin, path, options = {}) {
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

test("admin provider CRUD keeps keys encrypted and orders provider routes", async () => {
  const previousSecretsKey = process.env.LLM_SECRETS_KEY
  const previousOpenAiKey = process.env.OPENAI_API_KEY
  setRuntimeEnv("LLM_SECRETS_KEY", crypto.randomBytes(32).toString("base64"))
  delete process.env.OPENAI_API_KEY
  const database = initializeDatabase({ seed: true, filename: ":memory:" })
  let server
  try {
    server = await listen(
      createApp({
        database,
        databaseLabel: ":memory:",
        adminMode: "development",
        openAiKeyConfigured: false,
      }),
    )
    const origin = `http://127.0.0.1:${server.address().port}`
    const apiKey = crypto.randomBytes(24).toString("hex")
    const created = await jsonRequest(origin, "/api/admin/providers", {
      method: "POST",
      body: JSON.stringify({
        id: "fallback-provider",
        name: "备用视觉模型",
        protocol: "openai-chat-completions",
        baseUrl: "https://api.example.invalid/v1",
        model: "vision-model",
        apiKey,
        timeoutMs: 5000,
        maxRetries: 2,
        enabled: true,
        visionCapable: true,
      }),
    })
    assert.equal(created.response.status, 201)
    assert.equal(created.payload.provider.apiKeyConfigured, true)
    assert.deepEqual(created.payload.provider.capabilities, ["text", "image"])
    assert.equal(created.payload.provider.visionCapable, true)
    assert.equal("apiKey" in created.payload.provider, false)
    assert.equal("apiKeyCiphertext" in created.payload.provider, false)
    assert.equal(JSON.stringify(created.payload).includes(apiKey), false)
    const storedSecret = getLlmProviderSecret(database, "fallback-provider")
    assert.ok(storedSecret.apiKeyCiphertext)
    assert.notEqual(storedSecret.apiKeyCiphertext, apiKey)
    assert.equal(
      decryptSecret(storedSecret.apiKeyCiphertext, process.env.LLM_SECRETS_KEY),
      apiKey,
    )

    const edited = await jsonRequest(
      origin,
      "/api/admin/providers/fallback-provider",
      {
        method: "PUT",
        body: JSON.stringify({ model: "vision-model-v2" }),
      },
    )
    assert.equal(edited.response.status, 200)
    assert.equal(edited.payload.provider.model, "vision-model-v2")
    assert.deepEqual(edited.payload.provider.capabilities, ["text", "image"])
    assert.equal(
      decryptSecret(
        getLlmProviderSecret(database, "fallback-provider").apiKeyCiphertext,
        process.env.LLM_SECRETS_KEY,
      ),
      apiKey,
    )

    const legacyCapabilityEdit = await jsonRequest(
      origin,
      "/api/admin/providers/fallback-provider",
      {
        method: "PUT",
        body: JSON.stringify({ visionCapable: false }),
      },
    )
    assert.equal(legacyCapabilityEdit.response.status, 200)
    assert.deepEqual(legacyCapabilityEdit.payload.provider.capabilities, [
      "text",
    ])
    assert.equal(legacyCapabilityEdit.payload.provider.visionCapable, false)

    const restoredCapabilityEdit = await jsonRequest(
      origin,
      "/api/admin/providers/fallback-provider",
      {
        method: "PUT",
        body: JSON.stringify({ visionCapable: true }),
      },
    )
    assert.equal(restoredCapabilityEdit.response.status, 200)
    assert.deepEqual(restoredCapabilityEdit.payload.provider.capabilities, [
      "text",
      "image",
    ])

    const disabled = await jsonRequest(
      origin,
      "/api/admin/providers/fallback-provider",
      {
        method: "PUT",
        body: JSON.stringify({ enabled: false }),
      },
    )
    assert.equal(disabled.response.status, 200)
    assert.equal(disabled.payload.provider.enabled, false)

    const reenabled = await jsonRequest(
      origin,
      "/api/admin/providers/fallback-provider",
      {
        method: "PUT",
        body: JSON.stringify({ enabled: true }),
      },
    )
    assert.equal(reenabled.response.status, 200)
    assert.equal(reenabled.payload.provider.enabled, true)

    const cleared = await jsonRequest(
      origin,
      "/api/admin/providers/fallback-provider",
      {
        method: "PUT",
        body: JSON.stringify({ apiKey: null }),
      },
    )
    assert.equal(cleared.response.status, 200)
    assert.equal(cleared.payload.provider.apiKeyConfigured, false)
    assert.equal(
      getLlmProviderSecret(database, "fallback-provider").apiKeyCiphertext,
      null,
    )

    const second = await jsonRequest(origin, "/api/admin/providers", {
      method: "POST",
      body: JSON.stringify({
        id: "second-provider",
        name: "第二模型",
        baseUrl: "https://second.example.invalid/v1",
        model: "second-model",
      }),
    })
    assert.equal(second.response.status, 201)
    assert.deepEqual(second.payload.provider.capabilities, ["text", "image"])
    const ordered = await jsonRequest(origin, "/api/admin/providers/order", {
      method: "PUT",
      body: JSON.stringify({
        providerOrder: ["second-provider", "fallback-provider"],
      }),
    })
    assert.equal(ordered.response.status, 200)
    assert.deepEqual(ordered.payload.providerOrder, [
      "second-provider",
      "fallback-provider",
    ])
    assert.deepEqual(
      new Set(ordered.payload.providers.map((provider) => provider.id)),
      new Set(["fallback-provider", "second-provider"]),
    )

    const messageOrder = await jsonRequest(
      origin,
      "/api/admin/providers/order",
      {
        method: "PUT",
        body: JSON.stringify({
          route: "schedule_message",
          providerOrder: ["fallback-provider", "second-provider"],
        }),
      },
    )
    assert.equal(messageOrder.response.status, 200)
    assert.deepEqual(messageOrder.payload.providerOrder, [
      "fallback-provider",
      "second-provider",
    ])
    assert.deepEqual(messageOrder.payload.providerOrders.schedule_board, [
      "fallback-provider",
      "second-provider",
    ])
    const routeView = await jsonRequest(
      origin,
      "/api/admin/providers?route=schedule_message",
    )
    assert.deepEqual(routeView.payload.providerOrder, [
      "fallback-provider",
      "second-provider",
    ])

    const securePrefix = ["https", "://"].join("")
    const rejectedUrls = [
      `${securePrefix}${[127, 0, 0, 1].join(".")}/v1`,
      `${securePrefix}[${["fd00", "1"].join("::")}]/v1`,
      `${securePrefix}[${["::ffff", "127.0.0.1"].join(":")}]/v1`,
      `${securePrefix}${["user", "pass"].join(":")}@api.example.invalid/v1`,
      "https://api.example.invalid/v1?query=".concat("1"),
      "https://api.example.invalid/v1#".concat("fragment"),
    ]
    for (const [index, baseUrl] of rejectedUrls.entries()) {
      const rejected = await jsonRequest(origin, "/api/admin/providers", {
        method: "POST",
        body: JSON.stringify({
          id: `rejected-${index}`,
          name: "无效地址",
          baseUrl,
          model: "model",
        }),
      })
      assert.equal(rejected.response.status, 400, baseUrl)
    }
  } finally {
    await close(server)
    database.close()
    setRuntimeEnv("LLM_SECRETS_KEY", previousSecretsKey)
    setRuntimeEnv("OPENAI_API_KEY", previousOpenAiKey)
  }
})

test("provider connection errors do not expose upstream response bodies", async () => {
  const previousSecretsKey = process.env.LLM_SECRETS_KEY
  setRuntimeEnv("LLM_SECRETS_KEY", crypto.randomBytes(32).toString("base64"))
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  let server
  let calls = 0
  try {
    server = await listen(
      createApp({
        database,
        databaseLabel: ":memory:",
        adminMode: "development",
        openAiKeyConfigured: false,
        adminFetchImpl: async (url, options) => {
          calls += 1
          assert.equal(url, "https://api.example.invalid/v1/responses")
          assert.match(options.headers.authorization, /^Bearer /u)
          return calls === 1
            ? new Response("upstream-body-placeholder", { status: 502 })
            : new Response(JSON.stringify({ output_text: "ok" }), {
                status: 200,
                headers: { "content-type": "application/json" },
              })
        },
      }),
    )
    const origin = `http://127.0.0.1:${server.address().port}`
    const apiKey = crypto.randomBytes(24).toString("hex")
    const created = await jsonRequest(origin, "/api/admin/providers", {
      method: "POST",
      body: JSON.stringify({
        id: "connection-provider",
        name: "连接测试模型",
        baseUrl: "https://api.example.invalid/v1",
        model: "model",
        apiKey,
      }),
    })
    assert.equal(created.response.status, 201)

    const failed = await jsonRequest(
      origin,
      "/api/admin/providers/connection-provider/test",
      { method: "POST" },
    )
    assert.equal(failed.response.status, 400)
    assert.equal(failed.payload.error, "invalid_input")
    assert.equal(failed.payload.message, "provider request failed")
    assert.equal(
      JSON.stringify(failed.payload).includes("upstream-body-placeholder"),
      false,
    )
    assert.equal(
      getLlmProvider(database, "connection-provider").lastError,
      "provider request failed",
    )

    const succeeded = await jsonRequest(
      origin,
      "/api/admin/providers/connection-provider/test",
      { method: "POST" },
    )
    assert.equal(succeeded.response.status, 200)
    assert.equal(succeeded.payload.ok, true)
    assert.equal(succeeded.payload.provider.apiKeyConfigured, true)
  } finally {
    await close(server)
    database.close()
    setRuntimeEnv("LLM_SECRETS_KEY", previousSecretsKey)
  }
})
