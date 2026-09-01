import assert from "node:assert/strict"
import test from "node:test"

import { createApp } from "./app.js"
import {
  deleteManualEvent,
  getDashboardRevision,
  initializeDatabase,
  upsertEvents,
} from "./database.js"

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

async function mcpRequest(origin, body, headers = {}) {
  const response = await fetch(`${origin}/mcp`, {
    method: "POST",
    headers: {
      accept: "application/json, text/event-stream",
      "content-type": "application/json",
      ...headers,
    },
    body: JSON.stringify(body),
  })
  const text = await response.text()
  let payload = null
  try {
    payload = text ? JSON.parse(text) : null
  } catch {
    payload = { raw: text }
  }
  return { response, payload }
}

function toolCall(name, argumentsValue = {}) {
  return {
    jsonrpc: "2.0",
    id: `${name}-request`,
    method: "tools/call",
    params: { name, arguments: argumentsValue },
  }
}

function toolText(payload) {
  return JSON.parse(payload.result.content[0].text)
}

test("MCP exposes public reads without a key and scopes control tools", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const controlToken = ["control", "token", "placeholder"].join("-")
  const wrongToken = ["wrong", "token", "placeholder"].join("-")
  let server
  try {
    upsertEvents(database, [
      {
        id: "automatic-public-event",
        source: "x",
        source_item_id: "source-post-placeholder",
        source_key: "date-key-placeholder",
        title: "公开自动日程",
        detail: "公开详情",
        starts_on: "2026-09-02",
        starts_at: "2026-09-02T03:00:00.000Z",
        event_type: "stream",
        extraction_id: 42,
        provenance: "automatic",
      },
      {
        id: "manual-public-event",
        source: "manual",
        title: "人工日程",
        starts_on: "2026-09-03",
        provenance: "manual",
        manual_locked: 1,
      },
      {
        id: "deleted-public-event",
        source: "manual",
        title: "已删除日程",
        starts_on: "2026-09-04",
        provenance: "manual",
        manual_locked: 1,
      },
    ])
    assert.equal(deleteManualEvent(database, "deleted-public-event"), true)

    const appOptions = {
      database,
      databaseLabel: ":memory:",
      adminMode: "development",
    }
    Object.defineProperty(appOptions, "mcpControlToken", {
      value: controlToken,
      enumerable: true,
    })
    server = await listen(createApp(appOptions))
    const origin = `http://127.0.0.1:${server.address().port}`

    const publicTools = await mcpRequest(origin, {
      jsonrpc: "2.0",
      id: 1,
      method: "tools/list",
      params: {},
    })
    assert.equal(publicTools.response.status, 200)
    assert.deepEqual(
      publicTools.payload.result.tools.map((tool) => tool.name),
      [
        "dashboard_read",
        "schedule_list",
        "posts_list",
        "health_read",
        "sync_status",
      ],
    )
    assert.equal(publicTools.response.headers.get("vary"), "Authorization")

    const status = await mcpRequest(origin, toolCall("sync_status"))
    assert.equal(status.response.status, 200)
    assert.deepEqual(toolText(status.payload), { jobs: [] })

    const schedules = await mcpRequest(
      origin,
      toolCall("schedule_list", { pageSize: 100 }),
    )
    assert.equal(schedules.response.status, 200)
    const schedulePayload = toolText(schedules.payload)
    assert.equal(schedulePayload.total, 2)
    assert.deepEqual(
      schedulePayload.items.map((event) => event.id),
      ["manual-public-event", "automatic-public-event"],
    )
    for (const event of schedulePayload.items) {
      assert.equal("manualLocked" in event, false)
      assert.equal("deletedAt" in event, false)
      assert.equal("sourceItemId" in event, false)
      assert.equal("sourceKey" in event, false)
      assert.equal("extractionId" in event, false)
    }

    const cookieOnly = await mcpRequest(
      origin,
      toolCall("dashboard_request_reload"),
      { cookie: "kano_admin_session=not-a-real-session" },
    )
    assert.equal(cookieOnly.response.status, 401)

    const missingToken = await mcpRequest(
      origin,
      toolCall("dashboard_request_reload"),
    )
    assert.equal(missingToken.response.status, 401)
    assert.equal(
      missingToken.response.headers.get("www-authenticate"),
      'Bearer realm="kano-mcp"',
    )

    const invalidToken = await mcpRequest(
      origin,
      toolCall("dashboard_request_reload"),
      { authorization: `Bearer ${wrongToken}` },
    )
    assert.equal(invalidToken.response.status, 401)

    const controlTools = await mcpRequest(
      origin,
      {
        jsonrpc: "2.0",
        id: 2,
        method: "tools/list",
        params: {},
      },
      { authorization: `Bearer ${controlToken}` },
    )
    assert.equal(controlTools.response.status, 200)
    assert.deepEqual(
      controlTools.payload.result.tools.map((tool) => tool.name).slice(-3),
      ["dashboard_request_reload", "sync_start", "scan_run_automatic"],
    )

    const revisionBefore = getDashboardRevision(database)
    const reload = await mcpRequest(
      origin,
      toolCall("dashboard_request_reload"),
      { authorization: `Bearer ${controlToken}` },
    )
    assert.equal(reload.response.status, 200)
    assert.equal(toolText(reload.payload).revision, Number(revisionBefore) + 1)

    const dashboard = await mcpRequest(origin, toolCall("dashboard_read"))
    assert.equal(dashboard.response.status, 200)
    const dashboardPayload = toolText(dashboard.payload)
    for (const event of dashboardPayload.events) {
      assert.equal("manualLocked" in event, false)
      assert.equal("sourceItemId" in event, false)
    }
  } finally {
    await close(server)
    database.close()
  }
})

test("MCP endpoint rejects unsupported methods and can be disabled", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  let server
  try {
    server = await listen(
      createApp({ database, mcpEnabled: false, databaseLabel: ":memory:" }),
    )
    const origin = `http://127.0.0.1:${server.address().port}`
    const response = await fetch(`${origin}/mcp`, { method: "GET" })
    assert.equal(response.status, 404)
    assert.deepEqual(await response.json(), { error: "mcp_disabled" })
  } finally {
    await close(server)
    database.close()
  }
})
