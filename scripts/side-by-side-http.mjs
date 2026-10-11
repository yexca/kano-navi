#!/usr/bin/env node

// Start the two local public-read services on one disposable snapshot and
// compare their wire responses. This is deliberately separate from production
// configuration and never reads operator data/ or .env.
import assert from "node:assert/strict"
import { spawn, spawnSync } from "node:child_process"
import crypto from "node:crypto"
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs"
import { createServer } from "node:http"
import { tmpdir } from "node:os"
import { join } from "node:path"

const root = join(import.meta.dirname, "..")
const go = process.env.GO || "go"
let databaseModule
try {
  databaseModule = await import("../server/database.ts")
} catch (error) {
  if (
    error?.code === "ERR_MODULE_NOT_FOUND" &&
    !existsSync(join(root, "node_modules"))
  ) {
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
// Put the disposable snapshot below data/database so both implementations
// derive the same runtime media root. No operator snapshot or media is read.
const runtimeDirectory = join(root, "data")
const hadRuntimeDirectory = existsSync(runtimeDirectory)
const databaseDirectory = join(runtimeDirectory, "database")
mkdirSync(databaseDirectory, { recursive: true })
const databasePath = join(
  databaseDirectory,
  `side-by-side-${process.pid}-${Date.now()}.sqlite`,
)
const marker = `${process.pid}-${Date.now()}-${crypto.randomUUID()}`
const mediaBody = Buffer.concat([
  Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
  Buffer.from(`synthetic-side-by-side-image-${marker}`),
])
const mediaHash = crypto.createHash("sha256").update(mediaBody).digest("hex")
const mediaID = crypto
  .createHash("sha256")
  .update("https://cdn.example.invalid/side-by-side.png")
  .digest("hex")
const mediaRelativePath = `x/sha256/${mediaHash.slice(0, 2)}/${mediaHash}.png`
const profileRelativePath = `avatar/side-by-side-${marker}.png`
const pendingMediaID = "0".repeat(63) + "1"
const mediaPath = join(runtimeDirectory, ...mediaRelativePath.split("/"))
const profilePath = join(runtimeDirectory, ...profileRelativePath.split("/"))
mkdirSync(join(mediaPath, ".."), { recursive: true })
mkdirSync(join(profilePath, ".."), { recursive: true })
writeFileSync(mediaPath, mediaBody)
writeFileSync(profilePath, mediaBody)
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
  INSERT INTO media_assets (id, source, source_url, cache_path, mime_type, byte_size, sha256, status, fetched_at, updated_at)
    VALUES ('${mediaID}', 'x', 'https://cdn.example.invalid/side-by-side.png', '${mediaRelativePath}', 'image/png', ${mediaBody.length}, '${mediaHash}', 'ready', '2026-10-11T00:00:00.000Z', '2026-10-11T00:00:00.000Z');
  INSERT INTO media_assets (id, source, source_url, status)
    VALUES ('${pendingMediaID}', 'x', 'https://cdn.example.invalid/pending.png', 'pending');
  INSERT INTO profile_media (id, slot, source, source_url, active_cache_path, active_sha256, active_mime_type, status, is_active, updated_at)
    VALUES ('profile-avatar', 'avatar', 'upload', 'https://cdn.example.invalid/profile.png', '${profileRelativePath}', '${mediaHash}', 'image/png', 'ready', 1, '2026-10-11T00:00:00.000Z');
  INSERT INTO focus (id, video_id, date_label, title, description, updated_at)
    VALUES (1, 'video-1', '2026-10-11', 'Focus', 'Synthetic focus', '2026-10-11T00:00:00.000Z');
  INSERT INTO timeline (id, year, title, detail, sort_order) VALUES (1, '2026', 'Timeline', 'Synthetic timeline', 1);
  INSERT INTO resources (id, title, detail, icon, tone, url, sort_order) VALUES (1, 'Resource', 'Synthetic resource', 'link', 'blue', 'https://example.invalid/resource', 1);
  INSERT INTO app_settings (key, value, updated_at) VALUES ('dashboard_revision', '7', '2026-10-11T00:00:00.000Z');
`)

const protectedFiles = [
  databasePath,
  `${databasePath}-wal`,
  `${databasePath}-shm`,
  `${databasePath}-journal`,
  mediaPath,
  profilePath,
]
const beforeReads = fingerprintFiles(protectedFiles)

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
let comparisonPassed = false
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
    "/api/calendar?from=2026-10-11&to=2026-10-11&page=1&pageSize=100",
    "/api/calendar?from=2026-10-11&to=2026-10-17&page=1&pageSize=1",
    "/api/calendar?from=2026-10-11&to=2026-10-18&page=1&pageSize=100",
    `/media/${mediaID}?v=${mediaHash}`,
    `/media/profile/avatar?v=${mediaHash}`,
    `/media/${pendingMediaID}`,
    "/media/profile/banner",
  ]
  for (const path of requests) {
    const [typescript, goResponse] = await Promise.all([
      fetch(`http://127.0.0.1:${tsPort}${path}`),
      fetch(`http://127.0.0.1:${goPort}${path}`),
    ])
    assert.equal(typescript.status, goResponse.status, path)
    const headers = ["content-type", "cache-control", "vary"]
    if (path.startsWith("/media/") && typescript.status === 200)
      headers.push("etag", "content-length", "last-modified")
    for (const header of headers)
      assert.equal(
        typescript.headers.get(header),
        goResponse.headers.get(header),
        `${path} ${header}`,
      )
    if (path.startsWith("/media/")) {
      assert.deepEqual(
        Buffer.from(await goResponse.arrayBuffer()),
        Buffer.from(await typescript.arrayBuffer()),
        path,
      )
    } else {
      const left = normalize(path, await typescript.json())
      const right = normalize(path, await goResponse.json())
      assert.deepEqual(right, left, path)
    }
  }
  for (const [method, path, headers] of [
    ["HEAD", `/media/${mediaID}?v=${mediaHash}`, {}],
    ["HEAD", `/media/profile/avatar?v=${mediaHash}`, {}],
    ["GET", `/media/${mediaID}`, { Range: "bytes=0-3" }],
    ["GET", `/media/${mediaID}`, { "If-None-Match": `"${mediaHash}"` }],
  ]) {
    const [typescript, goResponse] = await Promise.all([
      fetch(`http://127.0.0.1:${tsPort}${path}`, { method, headers }),
      fetch(`http://127.0.0.1:${goPort}${path}`, { method, headers }),
    ])
    assert.equal(typescript.status, goResponse.status, `${method} ${path}`)
    for (const header of [
      "content-type",
      "cache-control",
      "etag",
      "content-length",
      "last-modified",
      "accept-ranges",
      "content-range",
    ])
      assert.equal(
        typescript.headers.get(header),
        goResponse.headers.get(header),
        `${method} ${path} ${header}`,
      )
    assert.deepEqual(
      Buffer.from(await goResponse.arrayBuffer()),
      Buffer.from(await typescript.arrayBuffer()),
      `${method} ${path}`,
    )
  }
  for (const [method, path] of [
    ["GET", "/api/unknown"],
    ["POST", "/api/dashboard"],
    ["POST", `/media/${mediaID}`],
  ]) {
    const [typescript, goResponse] = await Promise.all([
      fetch(`http://127.0.0.1:${tsPort}${path}`, { method }),
      fetch(`http://127.0.0.1:${goPort}${path}`, { method }),
    ])
    assert.equal(typescript.status, goResponse.status, `${method} ${path}`)
    assert.equal(
      typescript.headers.get("allow"),
      goResponse.headers.get("allow"),
      `${method} ${path} allow`,
    )
    const left = await typescript.text()
    const right = await goResponse.text()
    assert.equal(right, left, `${method} ${path} body`)
    assert.match(left, /^\{"error":"[a-z_]+"\}$/)
    assert.doesNotMatch(left, /sqlite|snapshot|database|[A-Za-z]:\\|\/home\//iu)
  }
  assert.deepEqual(
    fingerprintFiles(protectedFiles),
    beforeReads,
    "public reads changed the SQLite snapshot, sidecars, or media bytes",
  )
  console.log("side-by-side-http: PASS")
  comparisonPassed = true
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
  let rollbackError
  if (comparisonPassed) {
    try {
      const typescriptStillHealthy = await fetch(
        `http://127.0.0.1:${tsPort}/api/health`,
      )
      assert.equal(
        typescriptStillHealthy.status,
        200,
        "TypeScript rollback health",
      )
    } catch (error) {
      rollbackError = error
    }
  }
  await new Promise((resolve, reject) =>
    tsServer.close((error) => (error ? reject(error) : resolve())),
  )
  database.close()
  rmSync(directory, { recursive: true, force: true })
  for (const path of [
    databasePath,
    `${databasePath}-wal`,
    `${databasePath}-shm`,
    `${databasePath}-journal`,
    mediaPath,
    profilePath,
  ])
    rmSync(path, { force: true })
  if (!hadRuntimeDirectory)
    rmSync(runtimeDirectory, { recursive: true, force: true })
  if (goProcess.exitCode && goOutput) process.stderr.write(goOutput)
  if (rollbackError) throw rollbackError
}

function fingerprintFiles(paths) {
  return paths.map((path) => {
    if (!existsSync(path)) return { path, present: false }
    const bytes = readFileSync(path)
    return {
      path,
      present: true,
      size: bytes.length,
      sha256: crypto.createHash("sha256").update(bytes).digest("hex"),
    }
  })
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
