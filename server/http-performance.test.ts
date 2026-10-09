import assert from "node:assert/strict"
import crypto from "node:crypto"
import fs from "node:fs"
import http from "node:http"
import os from "node:os"
import path from "node:path"
import test from "node:test"
import { gunzipSync, brotliDecompressSync } from "node:zlib"
import { createApp } from "./app.ts"
import { initializeDatabase, upsertEvents } from "./database.ts"
import { listeningPort } from "./http-address.ts"

function wire(
  origin: string,
  route: string,
  headers: Record<string, string> = {},
  method = "GET",
  body?: string,
) {
  return new Promise<{
    status: number
    headers: http.IncomingHttpHeaders
    body: Buffer
  }>((resolve, reject) => {
    const req = http.request(
      `${origin}${route}`,
      { method, headers },
      (response) => {
        const chunks: Buffer[] = []
        response.on("data", (chunk) => chunks.push(chunk))
        response.on("end", () =>
          resolve({
            status: response.statusCode,
            headers: response.headers,
            body: Buffer.concat(chunks),
          }),
        )
      },
    )
    req.on("error", reject)
    req.end(body)
  })
}

test("public text negotiates compression and validators; secrets, media and fixed assets retain their boundaries", async (t) => {
  t.mock.timers.enable({
    apis: ["Date"],
    now: new Date("2025-01-01T00:00:00Z"),
  })
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "kano-http-cache-"))
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const authentication = { password: crypto.randomUUID() }
  const js = "console.log('synthetic bundle');\n".repeat(200)
  const css = ".synthetic { color: red; }\n".repeat(200)
  const html = `<main>${"Synthetic build ".repeat(200)}</main>`
  fs.mkdirSync(path.join(directory, "assets"))
  fs.mkdirSync(path.join(directory, ".vite"))
  fs.writeFileSync(path.join(directory, "index.html"), html)
  for (const [file, content] of Object.entries({
    "entry-abcdefgh.js": js,
    "entry-abcdefgh.css": css,
    "replaceable-abcdefgh.js": js,
    "icon.svg": "<svg><title>Synthetic cache fixture</title></svg>",
    "fixed.png": Buffer.alloc(2048, 1),
  }))
    fs.writeFileSync(path.join(directory, "assets", file), content)
  fs.writeFileSync(
    path.join(directory, ".vite/manifest.json"),
    JSON.stringify({
      "index.html": {
        file: "assets/entry-abcdefgh.js",
        css: ["assets/entry-abcdefgh.css"],
      },
    }),
  )
  upsertEvents(
    database,
    Array.from({ length: 20 }, (_, i) => ({
      id: `http-event-${i}`,
      source: "x",
      title: `Synthetic activity ${i}`,
      starts_on: "2025-01-01",
    })),
  )
  const server = createApp({
    database,
    staticDirectory: directory,
    adminMode: "production",
    adminPassword: authentication.password,
    mcpControlToken: "",
  }).listen(0, "127.0.0.1")
  await new Promise<void>((resolve) => server.once("listening", resolve))
  const origin = `http://127.0.0.1:${listeningPort(server)}`
  try {
    for (const route of [
      "/api/dashboard",
      "/assets/entry-abcdefgh.js",
      "/assets/entry-abcdefgh.css",
      "/",
      "/history",
      "/about",
      "/admin",
    ]) {
      const identity = await wire(origin, route, {
        "accept-encoding": "identity",
      })
      assert.equal(identity.status, 200)
      assert.equal(identity.headers["content-encoding"], undefined)
      assert.match(String(identity.headers.vary), /Accept-Encoding/i)
      for (const encoding of ["gzip", "br"]) {
        const encoded = await wire(origin, route, {
          "accept-encoding": encoding,
        })
        assert.equal(encoded.headers["content-encoding"], encoding)
        assert.ok(encoded.body.length < identity.body.length)
        assert.deepEqual(
          encoding === "gzip"
            ? gunzipSync(encoded.body)
            : brotliDecompressSync(encoded.body),
          identity.body,
        )
        assert.equal(encoded.headers.etag, identity.headers.etag)
        assert.match(String(encoded.headers.etag), /^W\//)
        assert.equal(encoded.headers["content-length"], undefined)
        const conditional = await wire(origin, route, {
          "accept-encoding": encoding,
          "if-none-match": String(encoded.headers.etag),
        })
        assert.equal(conditional.status, 304)
        assert.equal(conditional.body.length, 0)
        assert.equal(conditional.headers["content-encoding"], undefined)
        assert.match(String(conditional.headers.vary), /Accept-Encoding/i)
      }
      const refused = await wire(origin, route, {
        "accept-encoding": "gzip;q=0, br;q=0, deflate;q=0, identity;q=1",
      })
      assert.equal(refused.headers["content-encoding"], undefined)
      const head = await wire(
        origin,
        route,
        { "accept-encoding": "gzip" },
        "HEAD",
      )
      assert.equal(head.body.length, 0)
      assert.equal(head.headers["content-encoding"], undefined)
    }
    const negotiated = await wire(origin, "/assets/entry-abcdefgh.js", {
      "accept-encoding": "gzip;q=0.9, br;q=0.1",
    })
    assert.equal(negotiated.headers["content-encoding"], "gzip")
    const ranged = await wire(origin, "/assets/entry-abcdefgh.js", {
      range: "bytes=0-31",
      "accept-encoding": "gzip",
    })
    assert.equal(ranged.status, 206)
    assert.equal(ranged.headers["content-encoding"], undefined)
    assert.equal(ranged.body.length, 32)
    const modified = await wire(origin, "/assets/entry-abcdefgh.js")
    const unchanged = await wire(origin, "/assets/entry-abcdefgh.js", {
      "if-modified-since": String(modified.headers["last-modified"]),
      "accept-encoding": "gzip",
    })
    assert.equal(unchanged.status, 304)
    assert.match(String(unchanged.headers.vary), /Accept-Encoding/)
    const unadvertised = await wire(origin, "/assets/entry-abcdefgh.js")
    assert.equal(unadvertised.headers["content-encoding"], undefined)
    for (const route of [
      "/assets/entry-abcdefgh.js",
      "/assets/entry-abcdefgh.css",
    ]) {
      const response = await wire(origin, route)
      assert.equal(
        response.headers["cache-control"],
        "public, max-age=31536000, immutable",
      )
    }
    for (const route of [
      "/",
      "/history",
      "/about",
      "/assets/icon.svg",
      "/assets/fixed.png",
      "/assets/replaceable-abcdefgh.js",
    ]) {
      const response = await wire(origin, route, { "accept-encoding": "gzip" })
      assert.equal(
        response.headers["cache-control"],
        "public, max-age=0, must-revalidate",
      )
      if (/\.(?:png|svg)$/.test(route))
        assert.equal(response.headers["content-encoding"], undefined)
    }
    const revision = await wire(origin, "/api/dashboard/revision?since=0", {
      "accept-encoding": "gzip",
    })
    assert.equal(revision.headers["content-encoding"], undefined) // Below 1 KiB.
    assert.match(String(revision.headers.vary), /Accept-Encoding/)
    assert.equal(revision.headers["cache-control"], "no-store")
    const login = await wire(
      origin,
      "/api/admin/login",
      { "content-type": "application/json", "accept-encoding": "gzip" },
      "POST",
      JSON.stringify(authentication),
    )
    assert.equal(login.status, 200)
    assert.equal(login.headers["content-encoding"], undefined)
    const cookie = login.headers["set-cookie"][0].split(";", 1)[0]
    const admin = await wire(origin, "/api/admin/events", {
      cookie,
      "accept-encoding": "gzip",
    })
    assert.equal(admin.status, 200)
    assert.ok(admin.body.length > 1024)
    assert.equal(admin.headers["content-encoding"], undefined)
    assert.equal(admin.headers["cache-control"], "no-store, no-transform")
    for (const route of [
      "/media/missing",
      "/api/admin/providers",
      "/api/missing",
    ]) {
      const response = await wire(origin, route, { "accept-encoding": "gzip" })
      assert.equal(response.headers["content-encoding"], undefined)
    }
    const mcp = await wire(
      origin,
      "/mcp",
      {
        "content-type": "application/json",
        accept: "application/json, text/event-stream",
        "accept-encoding": "gzip",
      },
      "POST",
      JSON.stringify({
        jsonrpc: "2.0",
        id: 1,
        method: "initialize",
        params: {
          protocolVersion: "2025-03-26",
          capabilities: {},
          clientInfo: { name: "synthetic-test", version: "1" },
        },
      }),
    )
    assert.equal(mcp.status, 200)
    assert.equal(mcp.headers["content-encoding"], undefined)
    assert.equal(mcp.headers["cache-control"], "no-store, no-transform")
    // An older dist without a manifest cannot accidentally grant permanence.
    fs.rmSync(path.join(directory, ".vite/manifest.json"))
    const legacyServer = createApp({
      database,
      staticDirectory: directory,
      mcpEnabled: false,
    }).listen(0, "127.0.0.1")
    await new Promise<void>((resolve) =>
      legacyServer.once("listening", resolve),
    )
    try {
      const legacy = await wire(
        `http://127.0.0.1:${listeningPort(legacyServer)}`,
        "/assets/entry-abcdefgh.js",
      )
      assert.equal(
        legacy.headers["cache-control"],
        "public, max-age=0, must-revalidate",
      )
    } finally {
      await new Promise<void>((resolve) => legacyServer.close(() => resolve()))
    }
  } finally {
    await new Promise<void>((resolve) => server.close(() => resolve()))
    database.close()
    fs.rmSync(directory, { recursive: true, force: true })
  }
})
