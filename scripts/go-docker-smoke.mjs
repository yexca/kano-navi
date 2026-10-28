#!/usr/bin/env node

// Exercise read-only defaults and opt-in development writes against disposable
// state. No source endpoint is invoked and no external synchronization runs.
import assert from "node:assert/strict"
import {
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  statSync,
} from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { spawnSync } from "node:child_process"

const root = join(import.meta.dirname, "..")
const go = process.env.GO || "go"
const docker = process.env.DOCKER || "docker"
const image = process.env.GO_DOCKER_IMAGE || "kano-navi-go:ci"
const fixture = mkdtempSync(join(tmpdir(), "kano-go-docker-"))
const dataRoot = join(fixture, "data")
const databasePath = join(dataRoot, "database", "snapshot.sqlite")
let container = null

try {
  const fixtureInfo = JSON.parse(
    run(
      go,
      ["run", "./cmd/kano-fixture", "-database", databasePath],
      join(root, "backend-go"),
      true,
    ),
  )
  const before = treeSnapshot(dataRoot)
  const started = spawnSync(
    docker,
    [
      "run",
      "-d",
      "--read-only",
      "--tmpfs",
      "/tmp:rw,noexec,nosuid,size=16m",
      "-v",
      `${dataRoot}:/data:ro`,
      "-p",
      "127.0.0.1::7658",
      image,
      "-database",
      "/data/database/snapshot.sqlite",
      "-listen",
      "0.0.0.0:7658",
    ],
    { cwd: root, encoding: "utf8" },
  )
  if (started.error || started.status !== 0)
    throw new Error(`docker run failed: ${started.stderr || started.error}`)
  container = started.stdout.trim()
  const portOutput = run(docker, ["port", container, "7658/tcp"], root).trim()
  const portMatch = portOutput.match(/:(\d+)\s*$/u)
  if (!portMatch) throw new Error("docker did not publish the Go port")
  const origin = `http://127.0.0.1:${portMatch[1]}`
  await waitForHealth(`${origin}/api/health`)
  for (const path of [
    "/api/health",
    "/api/dashboard?days=1",
    "/api/calendar?from=2026-10-11&to=2026-10-11",
  ]) {
    const response = await fetch(`${origin}${path}`)
    assert.equal(response.status, 200, path)
  }
  const media = await fetch(`${origin}/media/${fixtureInfo.mediaId}`)
  assert.equal(media.status, 200, "ready synthetic media must be readable")
  const after = treeSnapshot(dataRoot)
  assert.deepEqual(
    after,
    before,
    "read-only container changed snapshot or media tree",
  )
  run(docker, ["rm", "-f", container], root, true)
  container = null
  const writable = run(
    docker,
    [
      "run",
      "-d",
      "--read-only",
      "--tmpfs",
      "/tmp:rw,noexec,nosuid,size=16m",
      "-v",
      `${dataRoot}:/data:rw`,
      "-p",
      "127.0.0.1::7658",
      "-e",
      "APP_MODE=development",
      "-e",
      "LLM_SECRETS_KEY=synthetic-encryption-key",
      image,
      "-database",
      "/data/database/fresh.sqlite",
      "-listen",
      "0.0.0.0:7658",
      "-initialize",
      "-enable-admin-writes",
      "-enable-x-sync",
      "-enable-youtube-sync",
      "-enable-media-downloads",
    ],
    root,
    true,
  )
  container = writable.trim()
  const writablePort = run(
    docker,
    ["port", container, "7658/tcp"],
    root,
    true,
  ).match(/:(\d+)\s*$/u)?.[1]
  assert.ok(writablePort)
  const writableOrigin = `http://127.0.0.1:${writablePort}`
  await waitForHealth(`${writableOrigin}/api/health`)
  const send = (path, method, body) =>
    fetch(`${writableOrigin}${path}`, {
      method,
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
  const event = await send("/api/admin/events", "POST", {
    title: "Synthetic manual event",
    startsOn: "2026-01-02",
  })
  assert.equal(
    event.status,
    201,
    "source flags must not disable admin event writes",
  )
  // The server assigns manual event IDs; a client-supplied ID is ignored.
  const eventId = (await event.json()).event.id
  assert.match(eventId, /^manual-[0-9a-f-]{36}$/u)
  // A one-off step run (ADR-0003). These read-only mode flags wire no
  // schedule adapter, so the step is blocked.
  const submitted = await send("/api/admin/runs", "POST", {
    kind: "workflow",
    steps: ["schedule"],
  })
  assert.equal(submitted.status, 202)
  const runID = (await submitted.json()).run.id
  let finished = null
  for (let attempt = 0; attempt < 100; attempt++) {
    const response = await fetch(`${writableOrigin}/api/admin/runs/${runID}`)
    assert.equal(response.status, 200)
    const { run } = await response.json()
    if (run.status !== "queued" && run.status !== "running") {
      finished = run
      break
    }
    await new Promise((resolve) => setTimeout(resolve, 50))
  }
  assert.ok(finished, "the step run must finish")
  // A blocked capability ends the run as failed without fetching.
  assert.equal(finished.kind, "workflow")
  assert.equal(finished.workflowId, null, "step runs save no workflow")
  assert.deepEqual(finished.steps, ["schedule", "reconcile"])
  assert.equal(
    finished.status,
    "failed",
    "unmigrated capabilities must finish as blocked without fetching",
  )
  assert.equal(finished.error, "capability_not_migrated")
  assert.equal(finished.progress.schedule, "failed")
  assert.deepEqual(finished.run.counts, {})
  const deleted = await send(
    `/api/admin/events/${encodeURIComponent(eventId)}`,
    "DELETE",
    undefined,
  )
  assert.equal(deleted.status, 204)
  const denied = spawnSync(
    docker,
    ["run", "--rm", "-e", "APP_MODE=production", image, "-enable-youtube-sync"],
    { cwd: root, encoding: "utf8" },
  )
  assert.equal(
    denied.status,
    2,
    "production rejects development source flags before opening SQLite",
  )
  console.log("go-docker-smoke: PASS")
} finally {
  if (container) spawnSync(docker, ["rm", "-f", container], { stdio: "ignore" })
  rmSync(fixture, { recursive: true, force: true })
}

function run(command, args, cwd, captureOnly = false) {
  const result = spawnSync(command, args, { cwd, encoding: "utf8" })
  if (result.stdout && !captureOnly) process.stdout.write(result.stdout)
  if (result.stderr && !captureOnly) process.stderr.write(result.stderr)
  if (result.error || result.status !== 0)
    throw new Error(`${command} ${args.join(" ")} failed`)
  return result.stdout
}

function treeSnapshot(rootDirectory) {
  const out = []
  walk(rootDirectory, out)
  return out.sort()
}

function walk(directory, out) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    const relative = path.slice(directory.length + 1)
    const stat = statSync(path)
    out.push(
      `${relative}:${entry.isDirectory() ? "dir" : `${stat.size}:${readFileSync(path).toString("hex")}`}`,
    )
    if (entry.isDirectory()) walk(path, out)
  }
}

async function waitForHealth(url) {
  for (let attempt = 0; attempt < 100; attempt++) {
    try {
      const response = await fetch(url)
      if (response.ok) return
    } catch {
      // Image startup may take a moment after the container is created.
    }
    await new Promise((resolve) => setTimeout(resolve, 50))
  }
  throw new Error("Go Docker service did not become ready")
}
