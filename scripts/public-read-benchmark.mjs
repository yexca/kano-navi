#!/usr/bin/env node

// Repeatable public-read measurement over 10,000 synthetic records. It emits
// measurements rather than enforcing a machine-specific CI threshold. Pass
// --output to save the JSON outside the repository when a run is being
// archived.
import crypto from "node:crypto"
import { spawn, spawnSync } from "node:child_process"
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs"
import { performance } from "node:perf_hooks"
import { tmpdir } from "node:os"
import { join } from "node:path"

const root = join(import.meta.dirname, "..")
const go = process.env.GO || "go"
const directory = mkdtempSync(join(tmpdir(), "kano-public-read-benchmark-"))
const runtimeDirectory = join(root, "data")
const hadRuntimeDirectory = existsSync(runtimeDirectory)
const databaseDirectory = join(runtimeDirectory, "database")
const databasePath = join(
  databaseDirectory,
  `benchmark-${process.pid}-${Date.now()}.sqlite`,
)
const marker = `${process.pid}-${Date.now()}-${crypto.randomUUID()}`
const mediaBody = Buffer.concat([
  Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
  Buffer.from(`benchmark-${marker}`),
])
const mediaHash = crypto.createHash("sha256").update(mediaBody).digest("hex")
const mediaID = crypto
  .createHash("sha256")
  .update(`https://cdn.example.invalid/${marker}.png`)
  .digest("hex")
const mediaRelativePath = `x/sha256/${mediaHash.slice(0, 2)}/${mediaHash}.png`
const mediaPath = join(runtimeDirectory, ...mediaRelativePath.split("/"))
let database
let goProcess

try {
  const { initializeDatabase } = await import("../server/database.ts")
  const { createApp } = await import("../server/app.ts")
  mkdirSync(databaseDirectory, { recursive: true })
  mkdirSync(join(mediaPath, ".."), { recursive: true })
  writeFileSync(mediaPath, mediaBody)
  database = initializeDatabase({ seed: false, filename: databasePath })
  const post = database.prepare(
    "INSERT INTO posts (id,source,type,text,published_at,url) VALUES (?,?,?,?,?,?)",
  )
  const video = database.prepare(
    "INSERT INTO videos (id,source,title,published_at,url,kind,is_upcoming) VALUES (?,?,?,?,?,?,?)",
  )
  const event = database.prepare(
    "INSERT INTO events (id,source,title,starts_on,starts_at,timezone,time_precision,status,event_type,provenance,manual_locked,cancellation_status,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
  )
  const insert = database.transaction(() => {
    for (let index = 0; index < 10000; index++) {
      const id = String(index).padStart(5, "0")
      post.run(
        `post-${id}`,
        "x",
        "daily",
        `Synthetic post ${id}`,
        "2026-10-10T12:00:00Z",
        `https://x.com/synthetic/status/${id}`,
      )
      video.run(
        `video-${id}`,
        "youtube",
        `Synthetic video ${id}`,
        "2026-10-10T00:00:00Z",
        `https://youtube.com/watch?v=${id}`,
        "video",
        0,
      )
      event.run(
        `event-${id}`,
        "x",
        `Synthetic event ${id}`,
        "2026-10-11",
        "2026-10-11T01:00:00Z",
        "Asia/Tokyo",
        "exact",
        "scheduled",
        "event",
        "automatic",
        0,
        "none",
        "",
        "",
      )
    }
    database
      .prepare(
        "INSERT INTO media_assets (id,source,source_url,cache_path,mime_type,byte_size,sha256,status,fetched_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)",
      )
      .run(
        mediaID,
        "x",
        `https://cdn.example.invalid/${marker}.png`,
        mediaRelativePath,
        "image/png",
        mediaBody.length,
        mediaHash,
        "ready",
        "2026-10-11T00:00:00Z",
        "2026-10-11T00:00:00Z",
      )
    database
      .prepare(
        "INSERT INTO app_settings (key,value,updated_at) VALUES ('dashboard_revision','10','2026-10-11T00:00:00Z')",
      )
      .run()
  })
  insert()

  const goBinary = join(
    directory,
    process.platform === "win32"
      ? "kano-go-benchmark.exe"
      : "kano-go-benchmark",
  )
  const built = spawnSync(go, ["build", "-o", goBinary, "./cmd/kano-go"], {
    cwd: join(root, "backend-go"),
    encoding: "utf8",
  })
  if (built.error || built.status !== 0)
    throw new Error("Go benchmark build failed")

  const tsServer = createApp({
    database,
    mcpEnabled: false,
    startWorkflowScheduler: false,
  }).listen(0, "127.0.0.1")
  await new Promise((resolve) => tsServer.once("listening", resolve))
  const tsOrigin = `http://127.0.0.1:${tsServer.address().port}`
  const goPort = await unusedPort()
  goProcess = spawn(
    goBinary,
    ["-listen", `127.0.0.1:${goPort}`, "-database", databasePath],
    {
      stdio: "ignore",
    },
  )
  const goOrigin = `http://127.0.0.1:${goPort}`
  await waitForHealth(`${goOrigin}/api/health`)

  const paths = [
    "/api/dashboard?days=30",
    "/api/calendar?from=2026-10-11&to=2026-10-17&page=1&pageSize=100",
    `/media/${mediaID}?v=${mediaHash}`,
  ]
  const result = {
    generated_at: new Date().toISOString(),
    records: 10000,
    services: {},
  }
  for (const [name, origin] of [
    ["typescript", tsOrigin],
    ["go", goOrigin],
  ]) {
    const rssBefore = rssBytes(goProcess?.pid, name)
    result.services[name] = {
      first_request_ms: {},
      concurrency: {},
      rss_bytes: {},
    }
    for (const path of paths) {
      result.services[name].first_request_ms[path] = await timedFetch(
        origin,
        path,
      )
      result.services[name].rss_bytes[path] = rssBytes(goProcess?.pid, name)
      result.services[name].concurrency[path] = {}
      for (const concurrency of [1, 4]) {
        const samples = []
        let responseBytes = 0
        for (let iteration = 0; iteration < 10; iteration++) {
          const started = performance.now()
          const responses = await Promise.all(
            Array.from({ length: concurrency }, () =>
              fetch(`${origin}${path}`),
            ),
          )
          const elapsed = performance.now() - started
          for (const response of responses) {
            const bytes = Buffer.from(await response.arrayBuffer())
            responseBytes = bytes.length
          }
          samples.push(elapsed)
        }
        samples.sort((left, right) => left - right)
        result.services[name].concurrency[path][concurrency] = {
          median_ms: percentile(samples, 0.5),
          p95_ms: percentile(samples, 0.95),
          response_bytes: responseBytes,
          rss_bytes: rssBytes(goProcess?.pid, name),
        }
      }
    }
    const rssAfter = rssBytes(goProcess?.pid, name)
    result.services[name].rss_growth_bytes =
      rssBefore != null && rssAfter != null ? rssAfter - rssBefore : null
  }
  const output = `${JSON.stringify(result, null, 2)}\n`
  const outputIndex = process.argv.indexOf("--output")
  if (outputIndex >= 0 && process.argv[outputIndex + 1])
    writeFileSync(process.argv[outputIndex + 1], output)
  process.stdout.write(output)
  await new Promise((resolve, reject) =>
    tsServer.close((error) => (error ? reject(error) : resolve())),
  )
} finally {
  if (goProcess?.exitCode === null) {
    if (process.platform === "win32")
      spawnSync("taskkill", ["/pid", String(goProcess.pid), "/t", "/f"], {
        stdio: "ignore",
      })
    else goProcess.kill("SIGTERM")
  }
  database?.close()
  rmSync(directory, { recursive: true, force: true })
  for (const path of [
    databasePath,
    `${databasePath}-wal`,
    `${databasePath}-shm`,
    `${databasePath}-journal`,
    mediaPath,
  ])
    rmSync(path, { force: true })
  if (!hadRuntimeDirectory)
    rmSync(runtimeDirectory, { recursive: true, force: true })
}

async function timedFetch(origin, path) {
  const started = performance.now()
  const response = await fetch(`${origin}${path}`)
  await response.arrayBuffer()
  return Number((performance.now() - started).toFixed(3))
}

function percentile(values, fraction) {
  return Number(
    values[
      Math.min(values.length - 1, Math.ceil(values.length * fraction) - 1)
    ].toFixed(3),
  )
}

function rssBytes(pid, service) {
  if (service === "typescript") return process.memoryUsage().rss
  if (!pid) return null
  if (process.platform === "linux") {
    try {
      const status = readFileSync(`/proc/${pid}/status`, "utf8")
      const match = status.match(/^VmRSS:\s+(\d+)\s+kB$/mu)
      return match ? Number(match[1]) * 1024 : null
    } catch {
      return null
    }
  }
  if (process.platform === "win32") {
    const result = spawnSync(
      "tasklist",
      ["/FI", `PID eq ${pid}`, "/FO", "CSV", "/NH"],
      { encoding: "utf8" },
    )
    const match = result.stdout?.match(/"([\d,]+) K"\s*$/u)
    if (match) return Number(match[1].replaceAll(",", "")) * 1024
  }
  return null
}

async function unusedPort() {
  const { createServer } = await import("node:http")
  const server = createServer()
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve))
  const port = server.address().port
  await new Promise((resolve) => server.close(resolve))
  return port
}

async function waitForHealth(url) {
  for (let attempt = 0; attempt < 120; attempt++) {
    try {
      if ((await fetch(url)).ok) return
    } catch {
      // Go may still be compiling.
    }
    await new Promise((resolve) => setTimeout(resolve, 50))
  }
  throw new Error("Go benchmark service did not become ready")
}
