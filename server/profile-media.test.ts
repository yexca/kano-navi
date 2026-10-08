import assert from "node:assert/strict"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import crypto from "node:crypto"
import test from "node:test"

import { createApp } from "./app.ts"
import {
  initializeDatabase,
  getProfileMedia,
  getActiveProfileMedia,
  selectProfileMedia,
  upsertProfileMediaCandidate,
} from "./database.ts"
import { resolveMediaCachePath } from "./media-cache.ts"
import {
  discoverProfileMedia,
  downloadProfileMedia,
  saveUploadedProfileMedia,
} from "./profile-media.ts"

async function listen(app) {
  return new Promise((resolve) => {
    const server = app.listen(0, "127.0.0.1", () => resolve(server))
  })
}

async function close(server) {
  if (!server) return
  await new Promise<void>((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  )
}

function imageResponse(body, mimeType = "image/jpeg") {
  return new Response(body, {
    status: 200,
    headers: { "content-type": mimeType },
  })
}

test("profile candidates use source namespaces and selection uses avatar slots", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const avatarPath = resolveMediaCachePath("avatar/avatar.jpg")
  const bannerPath = resolveMediaCachePath("avatar/banner.jpg")
  const previousAvatar =
    avatarPath && fs.existsSync(avatarPath) ? fs.readFileSync(avatarPath) : null
  const previousBanner =
    bannerPath && fs.existsSync(bannerPath) ? fs.readFileSync(bannerPath) : null
  const files = []
  try {
    const xCandidate = upsertProfileMediaCandidate(database, {
      slot: "avatar",
      source: "x",
      sourceRef: "https://x.com/kano_2525",
      sourceUrl: "https://pbs.twimg.com/profile-x-test.jpg",
    })
    const youtubeCandidate = upsertProfileMediaCandidate(database, {
      slot: "banner",
      source: "youtube",
      sourceRef: "https://www.youtube.com/channel/test",
      sourceUrl: "https://i.ytimg.com/vi/test-profile/maxresdefault.jpg",
    })
    const xBody = Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0x01, 0x58, 0xd9])
    const youtubeBody = Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0x01, 0x59, 0xd9])
    const fetchImpl = async (_url, options) => {
      assert.equal(options.redirect, "manual")
      return _url.toString().includes("ytimg")
        ? imageResponse(youtubeBody)
        : imageResponse(xBody)
    }

    const xReady = await downloadProfileMedia(database, xCandidate.id, {
      fetchImpl,
    })
    const youtubeReady = await downloadProfileMedia(
      database,
      youtubeCandidate.id,
      { fetchImpl },
    )
    assert.match(xReady.cachePath, /^x\/sha256\//u)
    assert.match(youtubeReady.cachePath, /^youtube\/sha256\//u)
    files.push(
      resolveMediaCachePath(xReady.cachePath),
      resolveMediaCachePath(youtubeReady.cachePath),
    )

    const selectedAvatar = selectProfileMedia(database, xCandidate.id)
    const selectedBanner = selectProfileMedia(database, youtubeCandidate.id)
    assert.equal(selectedAvatar.activeCachePath, "avatar/avatar.jpg")
    assert.equal(selectedBanner.activeCachePath, "avatar/banner.jpg")
    assert.equal(
      selectedAvatar.publicUrl,
      `/media/profile/avatar?v=${xReady.sha256}`,
    )
    assert.equal(
      selectedBanner.publicUrl,
      `/media/profile/banner?v=${youtubeReady.sha256}`,
    )
    files.push(avatarPath, bannerPath)
  } finally {
    database.close()
    for (const file of files) if (file) fs.rmSync(file, { force: true })
    if (avatarPath && previousAvatar)
      fs.writeFileSync(avatarPath, previousAvatar)
    if (bannerPath && previousBanner)
      fs.writeFileSync(bannerPath, previousBanner)
  }
})

test("profile image redirects are checked hop by hop and size limited", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    const redirectCandidate = upsertProfileMediaCandidate(database, {
      slot: "avatar",
      source: "x",
      sourceUrl: "https://pbs.twimg.com/profile-redirect-test.jpg",
    })
    let calls = 0
    await assert.rejects(
      downloadProfileMedia(database, redirectCandidate.id, {
        fetchImpl: async (_url, options) => {
          calls += 1
          assert.equal(options.redirect, "manual")
          return new Response(null, {
            status: 302,
            headers: { location: "https://evil.example.invalid/image.jpg" },
          })
        },
      }),
      /profile image host is not allowed/u,
    )
    assert.equal(calls, 1)

    const largeCandidate = upsertProfileMediaCandidate(database, {
      slot: "banner",
      source: "youtube",
      sourceUrl: "https://i.ytimg.com/vi/large-test/maxresdefault.jpg",
    })
    const largeBody = Buffer.alloc(32, 0x41)
    await assert.rejects(
      downloadProfileMedia(database, largeCandidate.id, {
        maxBytes: 8,
        fetchImpl: async (_url, options) => {
          assert.equal(options.redirect, "manual")
          return imageResponse(largeBody)
        },
      }),
      /size limit/u,
    )
  } finally {
    database.close()
  }
})

test("profile discovery validates page redirects against the profile allowlist", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    let calls = 0
    await assert.rejects(
      discoverProfileMedia(database, {
        slot: "avatar",
        source: "x",
        fetchImpl: async (_url, options) => {
          calls += 1
          assert.equal(options.redirect, "manual")
          return new Response(null, {
            status: 302,
            headers: { location: "https://evil.example.invalid/profile" },
          })
        },
      }),
      /profile page host is not allowed/u,
    )
    assert.equal(calls, 1)
  } finally {
    database.close()
  }
})

test("admin profile preview serves source candidates without allowing traversal", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  let server
  let candidate
  let cachedPath
  try {
    candidate = await saveUploadedProfileMedia(
      database,
      "avatar",
      Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0x01, 0x5a, 0xd9]),
      { mimeType: "image/jpeg" },
    )
    cachedPath = resolveMediaCachePath(candidate.cachePath)
    server = await listen(
      createApp({
        database,
        databaseLabel: ":memory:",
        adminMode: "development",
      }),
    )
    const { port } = server.address()
    const origin = `http://127.0.0.1:${port}`
    const preview = await fetch(
      `${origin}/api/admin/profile-media/${candidate.id}/preview`,
    )
    assert.equal(preview.status, 200)
    assert.equal(preview.headers.get("content-type"), "image/jpeg")

    database
      .prepare<unknown[], Record<string, any>>(
        "UPDATE profile_media SET cache_path = ? WHERE id = ?",
      )
      .run("avatar/../database/kano.sqlite", candidate.id)
    const traversal = await fetch(
      `${origin}/api/admin/profile-media/${candidate.id}/preview`,
    )
    assert.equal(traversal.status, 404)
  } finally {
    await close(server)
    database.close()
    if (cachedPath) fs.rmSync(cachedPath, { force: true })
  }
})

test("failed and successful redownloads retain selected bytes, URL and ETag until explicit selection", async () => {
  const directory = fs.mkdtempSync(
    path.join(os.tmpdir(), "kano-selected-media-"),
  )
  const database = initializeDatabase({
    seed: false,
    filename: path.join(directory, "fixture.sqlite"),
  })
  const activeFile = resolveMediaCachePath("avatar/avatar.jpg")
  const backup = path.join(directory, "previous-avatar.jpg")
  const hadFile = fs.existsSync(activeFile)
  if (hadFile) fs.copyFileSync(activeFile, backup)
  const files = []
  let server
  try {
    const candidate = upsertProfileMediaCandidate(database, {
      slot: "avatar",
      source: "x",
      sourceUrl: "https://pbs.twimg.com/synthetic-redownload.jpg",
    })
    const oldBody = Buffer.concat([
      Buffer.from([0xff, 0xd8, 0xff, 0xe0]),
      crypto.randomBytes(12),
    ])
    const newBody = Buffer.concat([
      Buffer.from([0xff, 0xd8, 0xff, 0xe0]),
      crypto.randomBytes(12),
    ])
    const oldReady = await downloadProfileMedia(database, candidate.id, {
      fetchImpl: async () => imageResponse(oldBody),
    })
    files.push(resolveMediaCachePath(oldReady.cachePath))
    selectProfileMedia(database, candidate.id)
    server = await listen(createApp({ database }))
    const origin = `http://127.0.0.1:${server.address().port}`
    const oldUrl = getActiveProfileMedia(database, "avatar").publicUrl
    const assertOld = async () => {
      const active = getActiveProfileMedia(database, "avatar")
      assert.equal(active.publicUrl, oldUrl)
      assert.equal(active.sha256, oldReady.sha256)
      const response = await fetch(`${origin}${oldUrl}`)
      assert.equal(response.status, 200)
      assert.equal(response.headers.get("etag"), `"${oldReady.sha256}"`)
      assert.deepEqual(Buffer.from(await response.arrayBuffer()), oldBody)
    }
    await assertOld()
    await assert.rejects(
      downloadProfileMedia(database, candidate.id, {
        fetchImpl: async () => new Response(null, { status: 503 }),
      }),
      /503/,
    )
    assert.equal(
      getProfileMedia(database, candidate.id).downloadStatus,
      "failed",
    )
    assert.match(getProfileMedia(database, candidate.id).lastError, /503/)
    assert.equal(getProfileMedia(database, candidate.id).status, "ready")
    await assertOld()
    const newReady = await downloadProfileMedia(database, candidate.id, {
      fetchImpl: async () => imageResponse(newBody),
    })
    files.push(resolveMediaCachePath(newReady.cachePath))
    assert.equal(newReady.downloadStatus, "success")
    assert.notEqual(newReady.sha256, oldReady.sha256)
    await assertOld()
    assert.equal(
      (await fetch(`${origin}/media/profile/avatar?v=${newReady.sha256}`))
        .status,
      404,
    )
    selectProfileMedia(database, candidate.id)
    const changed = await fetch(
      `${origin}${getActiveProfileMedia(database, "avatar").publicUrl}`,
    )
    assert.equal(changed.status, 200)
    assert.equal(changed.headers.get("etag"), `"${newReady.sha256}"`)
    assert.deepEqual(Buffer.from(await changed.arrayBuffer()), newBody)
    assert.equal((await fetch(`${origin}${oldUrl}`)).status, 404)
  } finally {
    await close(server)
    database.close()
    for (const file of files) fs.rmSync(file, { force: true })
    if (hadFile) fs.copyFileSync(backup, activeFile)
    else fs.rmSync(activeFile, { force: true })
    const resolvedDirectory = path.resolve(directory)
    assert.ok(
      resolvedDirectory.startsWith(`${path.resolve(os.tmpdir())}${path.sep}`),
    )
    fs.rmSync(resolvedDirectory, { recursive: true, force: true })
  }
})

test("profile active metadata upgrades additively and survives subsequent candidate downloads", () => {
  const directory = fs.mkdtempSync(
    path.join(os.tmpdir(), "kano-profile-upgrade-"),
  )
  const filename = path.join(directory, "fixture.sqlite")
  let database = initializeDatabase({ seed: false, filename })
  try {
    const candidate = upsertProfileMediaCandidate(database, {
      slot: "avatar",
      source: "x",
      sourceUrl: "https://pbs.twimg.com/synthetic-legacy-avatar.jpg",
    })
    const hash = "a".repeat(64)
    database
      .prepare(
        "UPDATE profile_media SET is_active=1, status='ready', cache_path='x/legacy.jpg', active_cache_path='avatar/avatar.jpg', sha256=?, mime_type='image/jpeg' WHERE id=?",
      )
      .run(hash, candidate.id)
    // Only this disposable fixture is shaped into the pre-upgrade schema.
    for (const column of [
      "active_sha256",
      "active_mime_type",
      "download_status",
      "last_download_at",
    ])
      database.exec(`ALTER TABLE profile_media DROP COLUMN ${column}`)
    database.close()
    database = initializeDatabase({ seed: false, filename })
    const active = getActiveProfileMedia(database, "avatar")
    assert.equal(active.sha256, hash)
    assert.equal(active.mimeType, "image/jpeg")
    assert.equal(active.activeCachePath, "avatar/avatar.jpg")
    assert.equal(active.publicUrl, `/media/profile/avatar?v=${hash}`)
    database.close()
    database = initializeDatabase({ seed: false, filename })
    assert.deepEqual(getActiveProfileMedia(database, "avatar"), active)
  } finally {
    database.close()
    assert.ok(
      path
        .resolve(directory)
        .startsWith(`${path.resolve(os.tmpdir())}${path.sep}`),
    )
    fs.rmSync(directory, { recursive: true, force: true })
  }
})
