#!/usr/bin/env node

// Controlled HTTP comparison for the phase-eight event mutation slice.
// Each service owns an independent disposable SQLite fixture.
import assert from "node:assert/strict"
import { spawn, spawnSync } from "node:child_process"
import { mkdtempSync, readdirSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { createServer } from "node:http"
import { initializeDatabase } from "../server/database.ts"
import { createApp } from "../server/app.ts"

const root = join(import.meta.dirname, "..")
const go = process.env.GO || "go"
const temp = mkdtempSync(join(tmpdir(), "kano-admin-mutation-"))
const tsPath = join(temp, "typescript.sqlite")
const goPath = join(temp, "go.sqlite")
const tsDb = initializeDatabase({ filename: tsPath, seed: false })
const goDb = initializeDatabase({ filename: goPath, seed: false })
const seed = (db) =>
  db.exec(`
  INSERT INTO events (id,source,title,starts_on,timezone,time_precision,status,event_type,url,provenance,manual_locked,cancellation_status,created_at,updated_at)
  VALUES ('automatic-1','x','Synthetic automatic','2026-10-12','Asia/Tokyo','date','scheduled','event','https://example.invalid/source','automatic',0,'none','2026-10-11T00:00:00.000Z','2026-10-11T00:00:00.000Z');
  INSERT INTO event_sources (event_id,source,source_item_id,source_key,url,raw_json,created_at,updated_at)
  VALUES ('automatic-1','x','source-1','post','https://example.invalid/source','{}','2026-10-11T00:00:00.000Z','2026-10-11T00:00:00.000Z');
  INSERT INTO app_settings (key,value,updated_at) VALUES ('dashboard_revision','7','2026-10-11T00:00:00.000Z');
`)
seed(tsDb)
seed(goDb)
const tsServer = createApp({ database: tsDb, adminMode: "development" }).listen(
  0,
  "127.0.0.1",
)
await new Promise((resolve) => tsServer.once("listening", resolve))
const tsPort = tsServer.address().port
const goPort = await unusedPort()
const child = spawn(
  go,
  [
    "run",
    "./cmd/kano-go",
    "-listen",
    `127.0.0.1:${goPort}`,
    "-database",
    goPath,
    "-enable-admin-writes",
  ],
  {
    cwd: join(root, "backend-go"),
    env: { ...process.env, APP_MODE: "development" },
    stdio: ["ignore", "pipe", "pipe"],
  },
)
let output = ""
child.stdout.on("data", (value) => {
  output += value.toString()
})
child.stderr.on("data", (value) => {
  output += value.toString()
})
const jars = [{ cookies: new Map() }, { cookies: new Map() }]
try {
  await waitForHealth(`http://127.0.0.1:${goPort}/api/health`)
  const base = {
    id: "manual-compare",
    title: "Manual synthetic",
    detail: "Controlled",
    startsOn: "2026-10-12",
    eventType: "event",
    status: "scheduled",
    url: "https://example.invalid/manual",
  }
  const operations = [["create", "POST", "/api/admin/events", base]]
  let tsID, goID
  for (const [name, method, path, body] of operations) {
    const [left, right] = await Promise.all([
      request(tsPort, method, path, body, jars[0]),
      request(goPort, method, path, body, jars[1]),
    ])
    if (right.status !== left.status)
      console.error(`${name} responses`, left, right)
    assert.equal(right.status, left.status, name)
    tsID = left.json.event.id
    goID = right.json.event.id
    assert.deepEqual(
      normalize(right.json, goID),
      normalize(left.json, tsID),
      name,
    )
    const [tsState, goState] = [tsDb, goDb].map((db) =>
      state(db, db === tsDb ? tsID : goID),
    )
    assert.deepEqual(goState, tsState, `${name} state`)
    console.log(`admin-mutation: ${name} PASS`)
  }
  const followups = [
    [
      "update",
      "PUT",
      "/api/admin/events/{id}",
      {
        ...base,
        title: "Manual edited",
        startsAt: "2026-10-12T04:00:00Z",
        endsAt: "2026-10-12T05:00:00Z",
      },
    ],
    [
      "llm-suspected-without-evidence",
      "PUT",
      "/api/admin/events/{id}",
      {
        ...base,
        title: "Manual edited",
        startsAt: "2026-10-12T04:00:00Z",
        endsAt: "2026-10-12T05:00:00Z",
        cancellationStatus: "llm_suspected",
      },
    ],
    [
      "confirm",
      "POST",
      "/api/admin/events/{id}/confirm",
      {
        cancellationStatus: "manual_confirmed",
        cancellationReason: "Synthetic reason",
        ...base,
        title: "Manual edited",
        startsAt: "2026-10-12T04:00:00Z",
        endsAt: "2026-10-12T05:00:00Z",
      },
    ],
    ["delete", "DELETE", "/api/admin/events/{id}"],
  ]
  for (const [name, method, template, body] of followups) {
    const [left, right] = await Promise.all([
      request(tsPort, method, template.replace("{id}", tsID), body, jars[0]),
      request(goPort, method, template.replace("{id}", goID), body, jars[1]),
    ])
    if (right.status !== left.status)
      console.error(`${name} responses`, left, right)
    assert.equal(right.status, left.status, name)
    assert.deepEqual(
      normalize(right.json, goID),
      normalize(left.json, tsID),
      name,
    )
    const [tsState, goState] = [tsDb, goDb].map((db) =>
      state(db, db === tsDb ? tsID : goID),
    )
    assert.deepEqual(goState, tsState, `${name} state`)
    console.log(`admin-mutation: ${name} PASS`)
  }
  const before = state(tsDb, tsID)
  const [left, right] = await Promise.all([
    request(
      tsPort,
      "PUT",
      `/api/admin/events/${tsID}`,
      { ...base, startsOn: "2026-02-30" },
      jars[0],
    ),
    request(
      goPort,
      "PUT",
      `/api/admin/events/${goID}`,
      { ...base, startsOn: "2026-02-30" },
      jars[1],
    ),
  ])
  assert.equal(left.status, 400)
  assert.equal(right.status, 400)
  assert.deepEqual(state(tsDb, tsID), before)
  assert.deepEqual(state(goDb, goID), before)
  const unexpected = fixtureFiles(temp).filter(
    (file) => !/^(typescript|go)\.sqlite(?:-(?:wal|shm|journal))?$/u.test(file),
  )
  assert.deepEqual(
    unexpected,
    [],
    "mutation fixture created an unexpected file",
  )
  console.log("admin-mutation: invalid rollback PASS")
  console.log("admin-mutation: PASS")
} finally {
  if (child.exitCode === null) {
    if (process.platform === "win32")
      spawnSync("taskkill", ["/pid", String(child.pid), "/t", "/f"], {
        stdio: "ignore",
      })
    else child.kill("SIGTERM")
  }
  await new Promise((resolve) =>
    child.exitCode === null ? child.once("exit", resolve) : resolve(),
  )
  await new Promise((resolve) => tsServer.close(resolve))
  tsDb.close()
  goDb.close()
  try {
    rmSync(temp, {
      recursive: true,
      force: true,
      maxRetries: 5,
      retryDelay: 100,
    })
  } catch {}
  if (child.exitCode && output) process.stderr.write(output)
}

function state(db, id) {
  return {
    events: db
      .prepare(
        "SELECT id,source,title,starts_on,starts_at,ends_at,timezone,time_precision,status,event_type,url,provenance,manual_locked,cancellation_status,cancellation_source,cancellation_reason,cancellation_evidence,cancellation_source_item_id,cancellation_confidence,cancellation_at,deleted_at FROM events ORDER BY id",
      )
      .all()
      .map((row) => {
        if (row.starts_at) row.starts_at = new Date(row.starts_at).toISOString()
        if (row.ends_at) row.ends_at = new Date(row.ends_at).toISOString()
        if (row.cancellation_at) row.cancellation_at = "<dynamic>"
        if (row.deleted_at) row.deleted_at = "<dynamic>"
        return row.id === id ? { ...row, id: "<manual>" } : row
      }),
    sources: db
      .prepare(
        "SELECT event_id,source,source_item_id,source_key,url,deleted_at FROM event_sources ORDER BY event_id,source_item_id",
      )
      .all()
      .map((row) =>
        row.event_id === id
          ? {
              ...row,
              event_id: "<manual>",
              source_item_id: "<manual>",
              source_key: "<manual>",
            }
          : row,
      ),
    revision:
      db
        .prepare(
          "SELECT value FROM app_settings WHERE key='dashboard_revision'",
        )
        .get()?.value ?? null,
  }
}
function normalize(value, id) {
  const copy = structuredClone(value)
  const event = copy?.event
  if (event) {
    for (const key of ["createdAt", "updatedAt", "cancellationAt"])
      event[key] = event[key] == null ? null : "<dynamic>"
    for (const key of ["startsAt", "endsAt"])
      if (event[key]) event[key] = new Date(event[key]).toISOString()
    event.manualLocked = Boolean(event.manualLocked)
    if (event.id === id) {
      event.id = "<manual>"
      event.sourceItemId = "<manual>"
      event.sourceKey = "<manual>"
    }
    if (event.cancellationSource === "none") event.cancellationSource = null
  }
  return copy
}
async function request(port, method, path, body, jar) {
  const headers = {
    "content-type": "application/json",
    origin: `http://127.0.0.1:${port}`,
  }
  if (jar.cookies.size)
    headers.cookie = [...jar.cookies]
      .map(([key, value]) => `${key}=${value}`)
      .join("; ")
  const response = await fetch(`http://127.0.0.1:${port}${path}`, {
    method,
    headers,
    body: body == null ? undefined : JSON.stringify(body),
  })
  for (const value of response.headers.getSetCookie?.() || []) {
    const [pair] = value.split(";", 1)
    const index = pair.indexOf("=")
    if (index > 0) jar.cookies.set(pair.slice(0, index), pair.slice(index + 1))
  }
  const text = await response.text()
  return { status: response.status, json: text ? JSON.parse(text) : null }
}
async function unusedPort() {
  const server = createServer()
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve))
  const port = server.address().port
  await new Promise((resolve) => server.close(resolve))
  return port
}
async function waitForHealth(url) {
  for (let i = 0; i < 1200; i++) {
    try {
      if ((await fetch(url)).ok) return
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 50))
  }
  throw new Error("Go service did not become ready")
}

function fixtureFiles(rootDirectory) {
  return readdirSync(rootDirectory).sort()
}
