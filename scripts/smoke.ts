import { listeningPort } from "../server/http-address.ts"
import assert from "node:assert/strict"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import { once } from "node:events"
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

async function runLocalSmoke() {
  // Do not import dotenv or the runtime entry point: neither local .env nor the
  // operator database belongs to a validation fixture.
  const [{ createApp }, { initializeDatabase, seedDatabase }] =
    await Promise.all([
      import("../server/app.ts"),
      import("../server/database.ts"),
    ])
  const temporaryRoot = fs.mkdtempSync(path.join(os.tmpdir(), "kano-smoke-"))
  let database
  let server
  try {
    database = initializeDatabase({
      filename: path.join(temporaryRoot, "snapshot.sqlite"),
    })
    // A second seed protects the idempotent initialization contract.
    seedDatabase(database)
    const app = createApp({
      database,
      databaseLabel: "smoke/snapshot.sqlite",
      adminMode: "production",
      adminPassword: "synthetic-password",
      mcpControlToken: "",
      startWorkflowScheduler: false,
    })
    server = app.listen(0, "127.0.0.1")
    await once(server, "listening")
    await verifySnapshot(`http://127.0.0.1:${listeningPort(server)}`)
    console.log("API smoke passed (isolated SQLite, seed, reads, admin guard).")
  } finally {
    if (server) {
      server.closeAllConnections()
      await new Promise<void>((resolve, reject) =>
        server.close((error) => (error ? reject(error) : resolve())),
      )
    }
    database?.close()
    fs.rmSync(temporaryRoot, { recursive: true, force: true })
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
