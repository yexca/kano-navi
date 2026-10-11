#!/usr/bin/env node

// Start the two local public-read services on one disposable snapshot and
// compare their wire responses. This is deliberately separate from production
// configuration and never reads data/ or .env.
import assert from "node:assert/strict"
import { spawn, spawnSync } from "node:child_process"
import { mkdtempSync, rmSync } from "node:fs"
import { createServer } from "node:http"
import { tmpdir } from "node:os"
import { join } from "node:path"

const root = join(import.meta.dirname, "..")
const go = process.env.GO || "go"
let databaseModule
try {
  databaseModule = await import("../server/database.ts")
} catch (error) {
  if (error?.code === "ERR_MODULE_NOT_FOUND") {
    console.error(
      "side-by-side-http: SKIPPED (Node dependencies are unavailable)",
    )
    process.exit(2)
  }
  throw error
}
const { initializeDatabase } = databaseModule
const { createApp } = await import("../server/app.ts")

const directory = mkdtempSync(join(tmpdir(), "kano-side-by-side-"))
const databasePath = join(directory, "snapshot.sqlite")
const database = initializeDatabase({ seed: false, filename: databasePath })
database.exec(`
  INSERT INTO profiles (id, display_name, romanized_name, bio, x_url, youtube_url, updated_at)
    VALUES ('profile', '鹿乃まほろ', 'Kano Mahoro', 'synthetic', 'https://x.com/kano_2525', 'https://youtube.com/@kano', '2026-10-11T00:00:00.000Z');
  INSERT INTO posts (id, source, type, text, published_at, url, likes, reposts, replies)
    VALUES ('post-1', 'x', 'daily', 'synthetic', '2026-10-10T12:00:00.000Z', 'https://x.com/kano_2525/status/1', 1, 2, 3);
  INSERT INTO videos (id, source, title, published_at, scheduled_at, url, kind, is_upcoming)
    VALUES ('video-1', 'youtube', 'Synthetic stream', '2026-10-10T00:00:00.000Z', '2026-10-11T04:00:00Z', 'https://www.youtube.com/watch?v=video-1', 'stream', 1);
  INSERT INTO events (id, source, title, starts_on, starts_at, ends_at, timezone, time_precision, status, event_type, url, provenance, manual_locked, cancellation_status, created_at, updated_at)
    VALUES ('event-1', 'youtube', 'Synthetic stream', '2026-10-11', '2026-10-11T04:00:00Z', '2026-10-11T05:00:00Z', 'Asia/Tokyo', 'exact', 'scheduled', 'stream', 'https://youtu.be/video-1?si=fixture', 'automatic', 0, 'none', '', '');
  INSERT INTO focus (id, video_id, date_label, title, description, updated_at)
    VALUES (1, 'video-1', '2026-10-11', 'Focus', 'Synthetic focus', '2026-10-11T00:00:00.000Z');
  INSERT INTO timeline (id, year, title, detail, sort_order) VALUES (1, '2026', 'Timeline', 'Synthetic timeline', 1);
  INSERT INTO resources (id, title, detail, icon, tone, url, sort_order) VALUES (1, 'Resource', 'Synthetic resource', 'link', 'blue', 'https://example.invalid/resource', 1);
  INSERT INTO app_settings (key, value, updated_at) VALUES ('dashboard_revision', '7', '2026-10-11T00:00:00.000Z');
`)

const tsServer = createApp({ database }).listen(0, "127.0.0.1")
await new Promise((resolve) => tsServer.once("listening", resolve))
const tsPort = tsServer.address().port
const goPort = await unusedPort()
const goProcess = spawn(
  go,
  [
    "run",
    "./cmd/kano-go",
    "-listen",
    `127.0.0.1:${goPort}`,
    "-database",
    databasePath,
  ],
  {
    cwd: join(root, "backend-go"),
    stdio: ["ignore", "pipe", "pipe"],
  },
)
let goOutput = ""
goProcess.stdout.on("data", (chunk) => {
  goOutput += chunk.toString()
})
goProcess.stderr.on("data", (chunk) => {
  goOutput += chunk.toString()
})

try {
  await waitForHealth(`http://127.0.0.1:${goPort}/api/health`)
  const requests = [
    "/api/health",
    "/api/dashboard/revision",
    "/api/dashboard/revision?since=0",
    "/api/dashboard?days=1",
    "/api/dashboard?days=3",
    "/api/dashboard?days=30",
    "/api/dashboard?days=invalid",
    "/api/dashboard?days=999",
  ]
  for (const path of requests) {
    const [typescript, goResponse] = await Promise.all([
      fetch(`http://127.0.0.1:${tsPort}${path}`),
      fetch(`http://127.0.0.1:${goPort}${path}`),
    ])
    assert.equal(typescript.status, goResponse.status, path)
    for (const header of ["content-type", "cache-control", "vary"])
      assert.equal(
        typescript.headers.get(header),
        goResponse.headers.get(header),
        `${path} ${header}`,
      )
    const left = normalize(path, await typescript.json())
    const right = normalize(path, await goResponse.json())
    assert.deepEqual(right, left, path)
  }
  for (const [method, path] of [
    ["GET", "/api/unknown"],
    ["POST", "/api/dashboard"],
  ]) {
    const [typescript, goResponse] = await Promise.all([
      fetch(`http://127.0.0.1:${tsPort}${path}`, { method }),
      fetch(`http://127.0.0.1:${goPort}${path}`, { method }),
    ])
    for (const response of [typescript, goResponse]) {
      const body = await response.text()
      assert.match(body, /^\{"error":"[a-z_]+"\}$/)
      assert.doesNotMatch(
        body,
        /sqlite|snapshot|database|[A-Za-z]:\\|\/home\//iu,
      )
    }
  }
  console.log("side-by-side-http: PASS")
} finally {
  if (goProcess.exitCode === null) {
    if (process.platform === "win32")
      spawnSync("taskkill", ["/pid", String(goProcess.pid), "/t", "/f"], {
        stdio: "ignore",
      })
    else goProcess.kill("SIGTERM")
  }
  await new Promise((resolve) => {
    if (goProcess.exitCode !== null) resolve()
    else goProcess.once("exit", resolve)
  })
  await new Promise((resolve, reject) =>
    tsServer.close((error) => (error ? reject(error) : resolve())),
  )
  database.close()
  rmSync(directory, { recursive: true, force: true })
  if (goProcess.exitCode && goOutput) process.stderr.write(goOutput)
}

function normalize(path, value) {
  const copy = structuredClone(value)
  if (path === "/api/health") copy.now = "<dynamic>"
  if (path.startsWith("/api/dashboard?")) copy.meta.generatedAt = "<dynamic>"
  return copy
}

async function unusedPort() {
  const server = createServer()
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve))
  const port = server.address().port
  await new Promise((resolve) => server.close(resolve))
  return port
}

async function waitForHealth(url) {
  for (let attempt = 0; attempt < 100; attempt++) {
    try {
      const response = await fetch(url)
      if (response.ok) return
    } catch {
      // The Go process may still be compiling.
    }
    await new Promise((resolve) => setTimeout(resolve, 50))
  }
  throw new Error("Go service did not become ready")
}
