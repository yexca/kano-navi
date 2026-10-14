#!/usr/bin/env node

// Exercise the two deployable images against copies of one synthetic snapshot.
// This is a canary/test check only: it never mounts repository data or reads
// .env. The TypeScript image gets a writable copy because its normal startup
// opens SQLite in WAL mode; the Go image receives its copy read-only.
import assert from "node:assert/strict"
import { spawnSync } from "node:child_process"
import {
  cpSync,
  existsSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs"
import crypto from "node:crypto"
import { tmpdir } from "node:os"
import { join, sep } from "node:path"

const root = join(import.meta.dirname, "..")
const docker = process.env.DOCKER || "docker"
const go = process.env.GO || "go"
const goImage =
  process.env.PUBLIC_READ_GO_IMAGE ||
  process.env.GO_DOCKER_IMAGE ||
  "kano-navi-go:ci"
const tsImage = process.env.PUBLIC_READ_TS_IMAGE || "kano-navi:public-read-ci"
const fixture = mkdtempSync(join(tmpdir(), "kano-public-read-deploy-"))
const sourceData = join(fixture, "source-data")
const tsData = join(fixture, "ts-data")
const goData = join(fixture, "go-data")
let tsContainer = null
let goContainer = null
let goVolume = null

try {
  const sourceDatabase = join(sourceData, "data", "database", "kano.sqlite")
  runCapture(
    go,
    ["run", "./cmd/kano-fixture", "-database", sourceDatabase],
    join(root, "backend-go"),
  )

  if (!process.env.PUBLIC_READ_GO_IMAGE) {
    run(
      docker,
      ["build", "-f", "backend-go/Dockerfile", "-t", goImage, "backend-go"],
      root,
    )
  }
  if (!process.env.PUBLIC_READ_TS_IMAGE) {
    run(docker, ["build", "-f", "ts.Dockerfile", "-t", tsImage, "."], root)
  }

  // The production TypeScript entry point performs the normal additive seed
  // pass at startup. Materialize that synthetic-only state before cloning the
  // snapshot so both services observe exactly the same public projection.
  run(
    docker,
    [
      "run",
      "--rm",
      "-e",
      "APP_MODE=production",
      "-e",
      "ADMIN_PASSWORD=synthetic-public-read-password",
      "-e",
      "LLM_SECRETS_KEY=synthetic-public-read-secrets-key",
      "-v",
      `${join(sourceData, "data")}:/app/data`,
      tsImage,
      "node",
      "--input-type=module",
      "-e",
      "import { initializeDatabase } from './server/database.ts'; const database = initializeDatabase({ filename: '/app/data/database/kano.sqlite' }); database.prepare(\"UPDATE profiles SET banner_url = NULL WHERE id = 'kano'\").run(); database.close();",
    ],
    root,
  )
  // Preserve the synthetic WAL-mode snapshot's sidecar shape. Empty WAL and
  // shared-memory files are a valid committed snapshot and let a strict
  // read-only mount open it without creating new files.
  const sourceDatabaseDirectory = join(sourceData, "data", "database")
  writeFileSync(
    join(sourceDatabaseDirectory, "kano.sqlite-wal"),
    Buffer.alloc(0),
  )
  writeFileSync(
    join(sourceDatabaseDirectory, "kano.sqlite-shm"),
    Buffer.alloc(32768),
  )
  cpSync(join(sourceData, "data"), tsData, { recursive: true })
  cpSync(join(sourceData, "data"), goData, { recursive: true })
  goVolume = runCapture(
    docker,
    ["volume", "create", `kano-public-read-${process.pid}`],
    root,
  )
  const volumeHelper = runCapture(
    docker,
    [
      "create",
      "--name",
      `kano-public-read-copy-${process.pid}`,
      "-v",
      `${goVolume}:/data`,
      goImage,
    ],
    root,
  )
  run(docker, ["cp", `${goData}${sep}.`, `${volumeHelper}:/data`], root)
  run(docker, ["rm", volumeHelper], root)

  const sourceHash = hashFile(
    join(sourceData, "data", "database", "kano.sqlite"),
  )
  const firstGo = await startGoContainer(`kano-go-public-read-${process.pid}`)
  goContainer = firstGo.container
  const goOrigin = firstGo.origin
  tsContainer = runDetached(docker, [
    "run",
    "-d",
    "--name",
    `kano-ts-public-read-${process.pid}`,
    "-e",
    "APP_MODE=production",
    "-e",
    "PORT=7657",
    "-e",
    "ADMIN_PASSWORD=synthetic-public-read-password",
    "-e",
    "LLM_SECRETS_KEY=synthetic-public-read-secrets-key",
    "-e",
    "MCP_ENABLED=0",
    "-e",
    "WORKFLOW_SCHEDULER_ENABLED=0",
    "-v",
    `${tsData}:/app/data`,
    "-p",
    "127.0.0.1::7657",
    tsImage,
  ])
  const tsOrigin = origin(docker, tsContainer, "7657")
  await waitForHealth(`${tsOrigin}/api/health`)
  const tsStartupHash = hashFile(join(tsData, "database", "kano.sqlite"))
  const tsStartupTree = persistentTreeSnapshot(tsData)
  await comparePublicReads(tsOrigin, goOrigin)

  assert.equal(
    hashFile(join(goData, "database", "kano.sqlite")),
    sourceHash,
    "Go changed the snapshot",
  )
  assert.deepEqual(
    goTreeSnapshot(goData),
    goTreeSnapshot(join(sourceData, "data")),
    "Go changed the mounted data tree",
  )
  assert.equal(
    hashFile(join(tsData, "database", "kano.sqlite")),
    tsStartupHash,
    "TypeScript changed the snapshot during reads",
  )
  const tsAfterReads = persistentTreeSnapshot(tsData)
  assert.deepEqual(
    tsAfterReads,
    tsStartupTree,
    "TypeScript created an unexpected file during reads",
  )

  stop(docker, goContainer)
  stop(docker, tsContainer)
  goContainer = null
  tsContainer = null

  // Restart both services using the same copies. A restart must retain the
  // snapshot and may only leave the TypeScript SQLite WAL sidecars it created
  // during its own normal startup.
  const restartedGo = await startGoContainer(
    `kano-go-public-read-restart-${process.pid}`,
  )
  goContainer = restartedGo.container
  const restartedGoOrigin = restartedGo.origin
  tsContainer = runDetached(docker, [
    "run",
    "-d",
    "--name",
    `kano-ts-public-read-restart-${process.pid}`,
    "-e",
    "APP_MODE=production",
    "-e",
    "PORT=7657",
    "-e",
    "ADMIN_PASSWORD=synthetic-public-read-password",
    "-e",
    "LLM_SECRETS_KEY=synthetic-public-read-secrets-key",
    "-e",
    "MCP_ENABLED=0",
    "-e",
    "WORKFLOW_SCHEDULER_ENABLED=0",
    "-v",
    `${tsData}:/app/data`,
    "-p",
    "127.0.0.1::7657",
    tsImage,
  ])
  const restartedTsOrigin = origin(docker, tsContainer, "7657")
  await waitForHealth(`${restartedTsOrigin}/api/health`)
  assert.equal((await fetch(`${restartedTsOrigin}/api/health`)).status, 200)
  assert.equal((await fetch(`${restartedGoOrigin}/api/health`)).status, 200)
  assert.equal(
    hashFile(join(tsData, "database", "kano.sqlite")),
    tsStartupHash,
    "restart changed TypeScript snapshot",
  )
  assert.equal(
    hashFile(join(goData, "database", "kano.sqlite")),
    sourceHash,
    "restart changed Go snapshot",
  )
  assert.deepEqual(
    goTreeSnapshot(goData),
    goTreeSnapshot(join(sourceData, "data")),
    "Go restart changed mounted data",
  )
  assert.deepEqual(
    persistentTreeSnapshot(tsData),
    tsAfterReads,
    "TypeScript restart created an unexpected data file",
  )
  console.log("public-read-deployment-smoke: PASS")
} finally {
  if (goContainer) stop(docker, goContainer)
  if (tsContainer) stop(docker, tsContainer)
  if (goVolume)
    spawnSync(docker, ["volume", "rm", "-f", goVolume], { stdio: "ignore" })
  rmSync(fixture, { recursive: true, force: true })
}

async function comparePublicReads(tsOrigin, goOrigin) {
  const paths = [
    "/api/health",
    "/api/dashboard/revision",
    "/api/dashboard?days=3",
    "/api/calendar?from=2026-10-11&to=2026-10-11",
    "/media/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "/media/profile/avatar",
  ]
  for (const path of paths) {
    const [left, right] = await Promise.all([
      fetch(`${tsOrigin}${path}`),
      fetch(`${goOrigin}${path}`),
    ])
    assert.equal(left.status, right.status, path)
    const headers = ["content-type", "cache-control"]
    if (path.startsWith("/media/") && left.status === 200)
      headers.push("etag", "last-modified", "content-length")
    for (const header of headers)
      assert.equal(
        left.headers.get(header),
        right.headers.get(header),
        `${path} ${header}`,
      )
    if (path.startsWith("/media/")) {
      assert.deepEqual(
        Buffer.from(await left.arrayBuffer()),
        Buffer.from(await right.arrayBuffer()),
        path,
      )
    } else {
      assert.deepEqual(
        normalize(path, await left.json()),
        normalize(path, await right.json()),
        path,
      )
    }
  }
}

function normalize(path, value) {
  const copy = structuredClone(value)
  if (path === "/api/health") copy.now = "<dynamic>"
  if (path.startsWith("/api/dashboard?")) copy.meta.generatedAt = "<dynamic>"
  return copy
}

function run(command, args, cwd) {
  const result = spawnSync(command, args, {
    cwd,
    encoding: "utf8",
    maxBuffer: 8 * 1024 * 1024,
  })
  if (result.error || result.status !== 0) throw new Error(`${command} failed`)
  return result.stdout
}

function runCapture(command, args, cwd) {
  const result = spawnSync(command, args, {
    cwd,
    encoding: "utf8",
    maxBuffer: 8 * 1024 * 1024,
  })
  if (result.error || result.status !== 0) throw new Error(`${command} failed`)
  return result.stdout.trim()
}

function runDetached(command, args) {
  const result = spawnSync(command, args, { encoding: "utf8" })
  if (result.error || result.status !== 0)
    throw new Error(`${command} run failed`)
  return result.stdout.trim()
}

async function startGoContainer(name) {
  for (let attempt = 0; attempt < 2; attempt++) {
    const container = runDetached(docker, [
      "run",
      "-d",
      "--name",
      name,
      "--read-only",
      "--tmpfs",
      "/tmp:rw,noexec,nosuid,size=16m",
      "-v",
      `${goVolume}:/data:ro`,
      "-p",
      "127.0.0.1::7658",
      goImage,
      "-database",
      "/data/database/kano.sqlite",
      "-listen",
      "0.0.0.0:7658",
    ])
    try {
      const serviceOrigin = origin(docker, container, "7658")
      await waitForHealth(`${serviceOrigin}/api/health`)
      return { container, origin: serviceOrigin }
    } catch (error) {
      stop(docker, container)
      if (attempt === 1) throw error
      await new Promise((resolve) => setTimeout(resolve, 500))
    }
  }
  throw new Error("Go container did not start")
}

function origin(command, container, port) {
  const output = runCapture(command, ["port", container, `${port}/tcp`], root)
  const match = output.match(/:(\d+)\s*$/u)
  if (!match) throw new Error("container port was not published")
  return `http://127.0.0.1:${match[1]}`
}

function stop(command, container) {
  if (container)
    spawnSync(command, ["rm", "-f", container], { stdio: "ignore" })
}

async function waitForHealth(url) {
  for (let attempt = 0; attempt < 120; attempt++) {
    try {
      const response = await fetch(url)
      if (response.ok) return
    } catch {
      // Startup is intentionally polled without printing addresses or paths.
    }
    await new Promise((resolve) => setTimeout(resolve, 250))
  }
  throw new Error("container health check timed out")
}

function hashFile(path) {
  return crypto.createHash("sha256").update(readFileSync(path)).digest("hex")
}

function treeSnapshot(directory) {
  const output = []
  walk(directory, output)
  return output.sort()
}

function persistentTreeSnapshot(directory) {
  return treeSnapshot(directory).filter(
    (entry) => !/(?:-wal|-shm|-journal):/u.test(entry),
  )
}

function goTreeSnapshot(directory) {
  return treeSnapshot(directory)
}

function walk(directory, output) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    const relative = path.slice(directory.length + 1)
    const stat = statSync(path)
    output.push(
      `${relative}:${entry.isDirectory() ? "dir" : `${stat.size}:${hashFile(path)}`}`,
    )
    if (entry.isDirectory()) walk(path, output)
  }
}
