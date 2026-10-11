#!/usr/bin/env node

// Run the independent scratch Go image against a disposable, read-only
// snapshot and media tree. The production Dockerfile and routes are not used.
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
