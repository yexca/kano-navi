// Synthetic, disposable snapshots only. Server and HTTP clients run in separate
// processes so synchronous SQLite work cannot stall the measuring event loop.
import { fork } from "node:child_process"
import http from "node:http"
import path from "node:path"
import { fileURLToPath } from "node:url"
import { gunzipSync, brotliDecompressSync, gzipSync } from "node:zlib"
import fs from "node:fs"
import express from "express"
import { createApp } from "../server/app.ts"
import { initializeDatabase } from "../server/database.ts"
import { listeningPort } from "../server/http-address.ts"

const script = fileURLToPath(import.meta.url)
const sizes = {
  small: { posts: 100, events: 20, media: 40, assets: 2 },
  large: { posts: 20000, events: 4000, media: 10000, assets: 150 },
}

export function syntheticDatabase(size: keyof typeof sizes) {
  const database = initializeDatabase({ filename: ":memory:", seed: false })
  const count = sizes[size]
  database.exec(
    "INSERT INTO profiles (id, display_name) VALUES ('main', 'Synthetic board')",
  )
  const post = database.prepare(
    "INSERT INTO posts (id, source, account_handle, text, published_at, url, raw_json) VALUES (?, 'x', 'example', ?, ?, ?, ?)",
  )
  const event = database.prepare(
    "INSERT INTO events (id, source, title, starts_on, starts_at, created_at, updated_at) VALUES (?, 'x', ?, ?, ?, '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')",
  )
  const media = database.prepare(
    "INSERT INTO media_assets (id, source, source_url, status, created_at, updated_at) VALUES (?, 'x', ?, 'pending', ?, ?)",
  )
  const asset = database.prepare(
    "INSERT INTO assets (id, kind, url, source_url, week_start, updated_at) VALUES (?, 'schedule', ?, ?, ?, ?)",
  )
  const review = database.prepare(
    "INSERT INTO schedule_asset_reviews (asset_id, llm_status, manual_status, updated_at) VALUES (?, 'schedule', 'unreviewed', ?)",
  )
  database.transaction(() => {
    for (let i = 0; i < count.posts; i++) {
      const timestamp =
        i < 20
          ? "2099-01-01T00:00:00Z"
          : new Date(Date.UTC(2025, 0, 1) - i * 3600000).toISOString()
      const text =
        i < count.assets
          ? "今週のスケジュール Synthetic weekly board"
          : `Synthetic stored post ${i}: ${"ordinary public text ".repeat(8)}`
      post.run(
        `post-${i}`,
        text,
        timestamp,
        `https://example.invalid/posts/${i}`,
        JSON.stringify({ search_text: text }),
      )
    }
    for (let i = 0; i < count.events; i++) {
      const timestamp = new Date(
        Date.UTC(2025, 0, 1) - i * 3600000,
      ).toISOString()
      event.run(
        `event-${i}`,
        `Synthetic historical activity ${i}`,
        timestamp.slice(0, 10),
        timestamp,
      )
    }
    for (let i = 0; i < count.media; i++)
      media.run(
        i.toString(16).padStart(64, "0"),
        `https://cdn.example.invalid/${i}.png`,
        "2025-01-01T00:00:00Z",
        "2025-01-01T00:00:00Z",
      )
    for (let i = 0; i < count.assets; i++) {
      const timestamp = new Date(
        Date.UTC(2025, 0, 6) - i * 7 * 86400000,
      ).toISOString()
      asset.run(
        `schedule-${i}`,
        `https://cdn.example.invalid/${i}.png`,
        `https://example.invalid/posts/${i}`,
        timestamp.slice(0, 10),
        timestamp,
      )
      review.run(`schedule-${i}`, timestamp)
    }
  })()
  return database
}

async function request(origin: string, route: string, encoding: string) {
  const start = performance.now()
  return new Promise<{
    ms: number
    bytes: number
    decodedBytes: number
    encoding: string
    vary: string
    cache: string
  }>((resolve, reject) => {
    const req = http.get(
      `${origin}${route}`,
      { headers: { "accept-encoding": encoding } },
      (response) => {
        const chunks: Buffer[] = []
        response.on("data", (chunk) => chunks.push(chunk))
        response.on("end", () => {
          if (response.statusCode !== 200)
            return reject(new Error(`HTTP ${response.statusCode}`))
          const body = Buffer.concat(chunks)
          const coding = String(
            response.headers["content-encoding"] || "identity",
          )
          const decoded =
            coding === "gzip"
              ? gunzipSync(body)
              : coding === "br"
                ? brotliDecompressSync(body)
                : body
          resolve({
            ms: performance.now() - start,
            bytes: body.length,
            decodedBytes: decoded.length,
            encoding: coding,
            vary: String(response.headers.vary || ""),
            cache: String(response.headers["cache-control"] || ""),
          })
        })
      },
    )
    req.setTimeout(30000, () => req.destroy(new Error("benchmark timeout")))
    req.on("error", reject)
  })
}

async function client(origin: string) {
  const rows = []
  for (const encoding of ["identity", "gzip"]) {
    for (const route of [
      "/api/dashboard?days=3",
      "/api/dashboard/revision?since=0",
      "/api/admin/schedule-assets?limit=200",
    ]) {
      for (let i = 0; i < 5; i++) await request(origin, route, encoding)
      for (const concurrency of [1, 4]) {
        const samples = []
        for (let batch = 0; batch < (concurrency === 1 ? 50 : 15); batch++)
          samples.push(
            ...(await Promise.all(
              Array.from({ length: concurrency }, () =>
                request(origin, route, encoding),
              ),
            )),
          )
        const timings = samples.map((sample) => sample.ms).sort((a, b) => a - b)
        const { ms: _ms, ...response } = samples[0]
        rows.push({
          route,
          acceptEncoding: encoding,
          concurrency,
          samples: samples.length,
          medianMs: Number(timings[Math.floor(timings.length / 2)].toFixed(2)),
          p95Ms: Number(
            timings[Math.ceil(timings.length * 0.95) - 1].toFixed(2),
          ),
          ...response,
        })
      }
    }
  }
  const html = await fetch(origin).then((response) => response.text())
  const resources = [
    ...html.matchAll(/(?:src|href)="(\/assets\/[^\"]+\.(?:js|css))"/g),
  ].map((match) => match[1])
  const staticResponses = []
  for (const resource of resources)
    staticResponses.push({
      resource,
      ...(await request(origin, resource, "gzip")),
    })
  const files = fs
    .readdirSync("dist/assets")
    .filter((file) => /\.(?:js|css)$/.test(file))
    .map((file) => {
      const bytes = fs.readFileSync(path.join("dist/assets", file))
      return { file, bytes: bytes.length, gzipBytes: gzipSync(bytes).length }
    })
  return { rows, staticResponses, files }
}

function childResult(args: string[]) {
  const child = fork(script, args, {
    stdio: ["ignore", "inherit", "inherit", "ipc"],
  })
  const result = new Promise<any>((resolve, reject) => {
    child.once("message", resolve)
    child.once("error", reject)
    child.once("exit", (code) => {
      if (code) reject(new Error(`benchmark child exit ${code}`))
    })
  })
  return { child, result }
}

const [mode, argument] = process.argv.slice(2)
if (mode === "--serve" || mode === "--browser") {
  const size = (argument || "small") as keyof typeof sizes
  if (!sizes[size]) throw new Error("Use small or large")
  const database = syntheticDatabase(size)
  if (mode === "--browser")
    database
      .prepare(
        "UPDATE profiles SET x_url='https://example.invalid/profile', youtube_url='https://example.invalid/channel', avatar_url='/assets/kano-avatar.jpg', banner_url='/assets/kano-banner.jpg', bio='Synthetic public snapshot for UI checks'",
      )
      .run()
  const app = createApp({
    database,
    staticDirectory: path.resolve("dist"),
    mcpControlToken: "",
    startWorkflowScheduler: false,
  })
  const fixtureApp = express()
  if (mode === "--browser") {
    let snapshotFailures = 0
    let snapshotDelay = 0
    let chunkDelay = 0
    let chunkFailure = false
    const counts: Record<string, number> = {}
    // Disposable fixture controls via stdin only; never part of the runtime API.
    process.stdin.on("data", (input) => {
      const [command, value] = String(input).trim().split(/\s+/)
      if (command === "fail") snapshotFailures = Number(value)
      if (command === "delay") snapshotDelay = Number(value)
      if (command === "chunks") chunkDelay = Number(value)
      if (command === "chunk-fail") chunkFailure = value === "1"
      if (command === "counts") console.log(JSON.stringify(counts))
      if (command === "revision")
        database
          .prepare(
            "INSERT INTO app_settings (key, value, updated_at) VALUES ('dashboard_revision', ?, '2025-01-01T00:00:00Z') ON CONFLICT(key) DO UPDATE SET value=excluded.value",
          )
          .run(value)
    })
    fixtureApp.use((request, response, next) => {
      counts[request.path] = (counts[request.path] || 0) + 1
      if (request.path === "/api/dashboard") {
        if (snapshotFailures > 0) {
          snapshotFailures--
          response.status(503).json({ error: "synthetic_unavailable" })
          return
        }
        if (snapshotDelay > 0) {
          setTimeout(next, snapshotDelay)
          return
        }
      }
      if (/^\/assets\/(?:admin|history|about).+\.js$/.test(request.path)) {
        if (chunkFailure) {
          response.status(503).end()
          return
        }
        if (chunkDelay > 0) {
          setTimeout(next, chunkDelay)
          return
        }
      }
      next()
    })
  }
  fixtureApp.use(app)
  const server = (mode === "--browser" ? fixtureApp : app).listen(
    0,
    "127.0.0.1",
  )
  await new Promise<void>((resolve) => server.once("listening", resolve))
  const origin = `http://127.0.0.1:${listeningPort(server)}`
  const plan = database
    .prepare(
      "EXPLAIN QUERY PLAN SELECT COALESCE(json_extract(raw_json, '$.search_text'), text) FROM posts WHERE url = ? ORDER BY published_at DESC LIMIT 1",
    )
    .all("https://example.invalid/posts/100")
  if (mode === "--browser") console.log(JSON.stringify({ origin, size, plan }))
  else process.send?.({ origin, size, count: sizes[size], plan })
  const stop = () =>
    server.close(() => {
      database.close()
      process.exit(0)
    })
  process.on("message", stop)
  process.on("SIGTERM", stop)
} else if (mode === "--client") {
  process.send?.(await client(argument))
  process.disconnect?.()
} else {
  const report = {
    label: mode || "measurement",
    node: process.version,
    snapshots: [] as any[],
  }
  for (const size of ["small", "large"]) {
    const serving = childResult(["--serve", size])
    try {
      const fixture = await serving.result
      const measuring = childResult(["--client", fixture.origin])
      report.snapshots.push({ ...fixture, ...(await measuring.result) })
    } finally {
      serving.child.send("stop")
    }
  }
  console.log(JSON.stringify(report, null, 2))
}
