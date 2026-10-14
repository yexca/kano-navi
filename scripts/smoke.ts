import assert from "node:assert/strict"
import { execFileSync, spawn } from "node:child_process"
import fs from "node:fs"
import { createServer } from "node:net"
import os from "node:os"
import path from "node:path"
import { setTimeout as delay } from "node:timers/promises"
import { fileURLToPath } from "node:url"

export async function verifySnapshot(
  origin,
  { staticPages = false }: { staticPages?: boolean } = {},
) {
  async function request(route, status = 200) {
    const response = await fetch(`${origin}${route}`, {
      signal: AbortSignal.timeout(5000),
      redirect: "error",
    })
    assert.equal(response.status, status, `Unexpected status for ${route}`)
    return response
  }

  const health = await (await request("/api/health")).json()
  assert.equal(health.ok, true)
  assert.equal(health.service, "kano-status-board")

  const dashboard = await (await request("/api/dashboard?days=3")).json()
  assert.ok(dashboard.profile)
  assert.ok(dashboard.summary)
  for (const field of ["posts", "events", "videos"])
    assert.deepEqual(dashboard[field], [], `Fresh ${field} must be empty`)
  assert.equal(dashboard.meta.fetchedAt, null)

  const revision = await (await request("/api/dashboard/revision")).json()
  assert.ok(Number.isInteger(revision.revision))
  const unchanged = await (
    await request(`/api/dashboard/revision?since=${revision.revision}`)
  ).json()
  assert.equal(unchanged.changed, false)

  const session = await (await request("/api/admin/session")).json()
  assert.equal(session.authenticated, false)
  await request("/api/admin/config", 401)
  await request("/media/not-an-id", 404)

  if (staticPages) {
    for (const route of ["/", "/history", "/admin"]) {
      const response = await request(route)
      assert.match(response.headers.get("content-type"), /text\/html/u)
      assert.match(await response.text(), /<div id="root"><\/div>/u)
    }
  }
}

async function freePort() {
  const server = createServer()
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve))
  const address = server.address()
  await new Promise<void>((resolve) => server.close(() => resolve()))
  if (!address || typeof address === "string")
    throw new Error("No loopback port is available")
  return address.port
}

async function runLocalSmoke() {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
  const go = process.env.GO || "go"
  const temporaryRoot = fs.mkdtempSync(path.join(os.tmpdir(), "kano-smoke-"))
  const binary = path.join(
    temporaryRoot,
    process.platform === "win32" ? "kano-go.exe" : "kano-go",
  )
  const database = path.join(temporaryRoot, "data", "database", "kano.sqlite")
  // The backend runs from the temporary directory with an explicit
  // environment: neither the local .env nor the operator database belongs to
  // a validation fixture.
  const env = {
    PATH: process.env.PATH,
    SystemRoot: process.env.SystemRoot,
    APP_MODE: "production",
    ADMIN_PASSWORD: "synthetic-password",
    LLM_SECRETS_KEY: "synthetic-encryption-key",
    WORKFLOW_SCHEDULER_ENABLED: "0",
    MCP_CONTROL_TOKEN: "",
  }
  let server
  try {
    execFileSync(go, ["build", "-o", binary, "./cmd/kano-go"], {
      cwd: path.join(root, "backend-go"),
      stdio: ["ignore", "inherit", "inherit"],
    })
    // A second seed protects the idempotent initialization contract.
    for (let pass = 0; pass < 2; pass += 1)
      execFileSync(binary, ["seed", "-database", database], {
        cwd: temporaryRoot,
        env,
        stdio: ["ignore", "ignore", "inherit"],
      })
    const port = await freePort()
    server = spawn(
      binary,
      ["serve", "-database", database, "-listen", `127.0.0.1:${port}`],
      { cwd: temporaryRoot, env, stdio: ["ignore", "ignore", "inherit"] },
    )
    const origin = `http://127.0.0.1:${port}`
    let healthy = false
    for (let attempt = 0; attempt < 50 && !healthy; attempt += 1) {
      if (server.exitCode != null)
        throw new Error("The backend exited before health was ready")
      try {
        const response = await fetch(`${origin}/api/health`, {
          signal: AbortSignal.timeout(1000),
          redirect: "error",
        })
        healthy = response.ok
      } catch {
        // The listener may still be starting; retry within the fixed budget.
      }
      if (!healthy) await delay(200)
    }
    if (!healthy) throw new Error("Backend health did not become ready")
    await verifySnapshot(origin)
    console.log("API smoke passed (isolated SQLite, seed, reads, admin guard).")
  } finally {
    if (server && server.exitCode == null) {
      const exited = new Promise((resolve) => server.once("exit", resolve))
      server.kill()
      await exited
    }
    fs.rmSync(temporaryRoot, { recursive: true, force: true, maxRetries: 5 })
  }
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  runLocalSmoke().catch((error) => {
    console.error(error.message)
    process.exitCode = 1
  })
}
