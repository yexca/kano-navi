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
  upsertVideos,
  setAppSetting,
  getAppSetting,
  getVideoRecord,
  upsertProfile,
  getDashboardRevision,
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

for (const schema of ["legacy", "incorrect-active", "incorrect-mime"])
  test(`profile ${schema} upgrade recovers selected A bytes independently of candidate B and is idempotent`, async () => {
    const directory = fs.mkdtempSync(
      path.join(os.tmpdir(), "kano-profile-upgrade-"),
    )
    const filename = path.join(directory, "fixture.sqlite")
    let database = initializeDatabase({ seed: false, filename })
    const marker = crypto.randomUUID()
    const activePath = `avatar/upgrade-${marker}.png`
    const candidatePath = `x/upgrade-${marker}.jpg`
    const bodyA = Buffer.concat([
      Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
      Buffer.from(`selected-${marker}`),
    ])
    const bodyB = Buffer.concat([
      Buffer.from([0xff, 0xd8, 0xff]),
      Buffer.from(`candidate-${marker}`),
    ])
    const hashA = crypto.createHash("sha256").update(bodyA).digest("hex")
    const hashB = crypto.createHash("sha256").update(bodyB).digest("hex")
    let server
    try {
      fs.writeFileSync(resolveMediaCachePath(activePath), bodyA)
      fs.writeFileSync(resolveMediaCachePath(candidatePath), bodyB)
      const candidate = upsertProfileMediaCandidate(database, {
        slot: "avatar",
        source: "x",
        sourceUrl: "https://pbs.twimg.com/synthetic-legacy-avatar.jpg",
      })
      database
        .prepare(
          "UPDATE profile_media SET is_active=1, status='ready', cache_path=?, active_cache_path=?, sha256=?, mime_type='image/jpeg', active_sha256=?, active_mime_type='image/jpeg' WHERE id=?",
        )
        .run(
          candidatePath,
          activePath,
          hashB,
          schema === "incorrect-mime" ? hashA : hashB,
          candidate.id,
        )
      upsertProfile(database, {
        id: "synthetic-profile",
        display_name: "Synthetic profile",
        romanized_name: "Synthetic profile",
        bio: "Synthetic fixture",
        avatar_url: "/assets/avatar.svg",
        banner_url: "/assets/banner.svg",
        x_url: "https://example.invalid/profile",
        youtube_url: "https://example.invalid/channel",
      })
      if (schema !== "legacy")
        database
          .prepare(
            "UPDATE profile_media SET download_status='failed', last_download_at='2098-12-31T00:00:00Z', last_error='synthetic retry failure' WHERE id=?",
          )
          .run(candidate.id)
      upsertVideos(database, [
        {
          id: "upgrade-snapshot",
          source: "youtube",
          title: "Keep snapshot",
          url: "https://example.invalid/upgrade",
          published_at: "2099-01-01T00:00:00Z",
        },
      ])
      setAppSetting(database, "featured_video_id", "upgrade-snapshot")
      const snapshot = getVideoRecord(database, "upgrade-snapshot")
      const candidateBefore = getProfileMedia(database, candidate.id)
      setAppSetting(database, "dashboard_revision", "17")
      server = await listen(createApp({ database }))
      let origin = `http://127.0.0.1:${server.address().port}`
      let browserSnapshot = await (
        await fetch(`${origin}/api/dashboard`)
      ).json()
      assert.equal(browserSnapshot.meta.revision, 17)
      assert.equal(
        browserSnapshot.profile.avatarUrl,
        `/media/profile/avatar?v=${schema === "incorrect-mime" ? hashA : hashB}`,
      )
      await close(server)
      server = null
      // Only this disposable fixture is shaped into the pre-upgrade schema.
      if (schema === "legacy")
        for (const column of [
          "active_sha256",
          "active_mime_type",
          "download_status",
          "last_download_at",
        ])
          database.exec(`ALTER TABLE profile_media DROP COLUMN ${column}`)
      let previousActive
      for (let startup = 0; startup < 2; startup++) {
        database.close()
        database = initializeDatabase({ seed: false, filename })
        assert.equal(getDashboardRevision(database), 18)
        const active = getActiveProfileMedia(database, "avatar")
        assert.equal(active.sha256, hashA)
        assert.equal(active.mimeType, "image/png")
        assert.equal(active.activeCachePath, activePath)
        assert.equal(active.isActive, true)
        assert.equal(active.publicUrl, `/media/profile/avatar?v=${hashA}`)
        if (previousActive) assert.deepEqual(active, previousActive)
        previousActive = active
        const candidateAfter = getProfileMedia(database, candidate.id)
        for (const field of [
          "sha256",
          "mimeType",
          "cachePath",
          "isActive",
          "updatedAt",
          "activeCachePath",
          "status",
          "lastError",
          "sourceUrl",
          "downloadStatus",
          "lastDownloadAt",
        ])
          assert.equal(candidateAfter[field], candidateBefore[field])
        assert.deepEqual(getVideoRecord(database, "upgrade-snapshot"), snapshot)
        assert.equal(
          getAppSetting(database, "featured_video_id"),
          "upgrade-snapshot",
        )
        server = await listen(createApp({ database }))
        origin = `http://127.0.0.1:${server.address().port}`
        // Follow useDashboard's existing revision comparison before reloading.
        const hint = await (
          await fetch(
            `${origin}/api/dashboard/revision?since=${browserSnapshot.meta.revision}`,
          )
        ).json()
        assert.equal(hint.revision, 18)
        assert.equal(hint.changed, startup === 0)
        if (Number(hint.revision) !== Number(browserSnapshot.meta.revision))
          browserSnapshot = await (
            await fetch(`${origin}/api/dashboard`)
          ).json()
        assert.equal(browserSnapshot.meta.revision, 18)
        assert.equal(browserSnapshot.profile.avatarUrl, active.publicUrl)
        for (const url of [active.publicUrl, "/media/profile/avatar"]) {
          const response = await fetch(`${origin}${url}`)
          assert.equal(response.status, 200)
          assert.equal(response.headers.get("content-type"), "image/png")
          assert.equal(response.headers.get("etag"), `"${hashA}"`)
          assert.deepEqual(Buffer.from(await response.arrayBuffer()), bodyA)
        }
        assert.equal(
          (await fetch(`${origin}/media/profile/avatar?v=${hashB}`)).status,
          404,
        )
        await close(server)
        server = null
      }
    } finally {
      await close(server)
      database.close()
      for (const relative of [activePath, candidatePath])
        fs.rmSync(resolveMediaCachePath(relative), { force: true })
      assert.ok(
        path
          .resolve(directory)
          .startsWith(`${path.resolve(os.tmpdir())}${path.sep}`),
      )
      fs.rmSync(directory, { recursive: true, force: true })
    }
  })

for (const schema of ["legacy", "incorrect-active"])
  for (const condition of [
    "missing",
    "traversal",
    "source-namespace",
    "symlink-escape",
    "invalid-media",
    "oversize",
  ])
    test(`profile ${schema} upgrade rejects ${condition} selected files while retaining selection, candidate and unrelated state`, async () => {
      const directory = fs.mkdtempSync(
        path.join(os.tmpdir(), "kano-profile-invalid-"),
      )
      const filename = path.join(directory, "fixture.sqlite")
      let database = initializeDatabase({ seed: false, filename })
      const marker = crypto.randomUUID()
      const safePath = `avatar/invalid-${marker}.png`
      const sourcePath = `x/invalid-${marker}.png`
      const linkPath = resolveMediaCachePath(`avatar/link-${marker}`)
      const body = Buffer.concat([
        Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
        Buffer.from(marker),
      ])
      const hash = crypto.createHash("sha256").update(body).digest("hex")
      let server
      let linked = false
      try {
        let activePath = safePath
        if (condition === "traversal") activePath = "avatar/../x/illegal.png"
        if (condition === "source-namespace") {
          fs.writeFileSync(resolveMediaCachePath(sourcePath), body)
          activePath = sourcePath
        }
        if (condition === "invalid-media")
          fs.writeFileSync(
            resolveMediaCachePath(safePath),
            "<svg>unsupported</svg>",
          )
        if (condition === "oversize")
          fs.writeFileSync(
            resolveMediaCachePath(safePath),
            Buffer.concat([body, Buffer.alloc(15 * 1024 * 1024)]),
          )
        if (condition === "symlink-escape") {
          fs.writeFileSync(path.join(directory, "outside.png"), body)
          fs.symlinkSync(
            directory,
            linkPath,
            process.platform === "win32" ? "junction" : "dir",
          )
          linked = true
          activePath = `avatar/link-${marker}/outside.png`
        }
        const candidate = upsertProfileMediaCandidate(database, {
          slot: "avatar",
          source: "x",
          sourceUrl: "https://pbs.twimg.com/synthetic-invalid-upgrade.png",
        })
        database
          .prepare(
            "UPDATE profile_media SET is_active=1, status='ready', active_cache_path=?, sha256=?, mime_type='image/png', active_sha256=?, active_mime_type='image/png' WHERE id=?",
          )
          .run(activePath, hash, hash, candidate.id)
        upsertVideos(database, [
          {
            id: "preserved",
            source: "youtube",
            title: "Keep snapshot",
            url: "https://example.invalid/preserved",
          },
        ])
        setAppSetting(database, "featured_video_id", "preserved")
        const candidateBefore = getProfileMedia(database, candidate.id)
        const snapshot = getVideoRecord(database, "preserved")
        if (schema === "legacy")
          for (const column of ["active_sha256", "active_mime_type"])
            database.exec(`ALTER TABLE profile_media DROP COLUMN ${column}`)
        for (let startup = 0; startup < 2; startup++) {
          database.close()
          database = initializeDatabase({ seed: false, filename })
          const active = getActiveProfileMedia(database, "avatar")
          assert.equal(active.sha256, null)
          assert.equal(active.mimeType, null)
          assert.equal(active.publicUrl, null)
          assert.equal(active.activeCachePath, activePath)
          assert.equal(active.isActive, true)
          const current = getProfileMedia(database, candidate.id)
          for (const field of [
            "sha256",
            "mimeType",
            "status",
            "isActive",
            "updatedAt",
          ])
            assert.equal(current[field], candidateBefore[field])
          assert.deepEqual(getVideoRecord(database, "preserved"), snapshot)
          assert.equal(
            getAppSetting(database, "featured_video_id"),
            "preserved",
          )
          server = await listen(createApp({ database }))
          const origin = `http://127.0.0.1:${server.address().port}`
          const response = await fetch(
            `${origin}/media/profile/avatar?v=${hash}`,
          )
          assert.equal(response.status, 404)
          assert.deepEqual(Object.keys(await response.json()), ["error"])
          await close(server)
          server = null
        }
      } finally {
        await close(server)
        database.close()
        for (const relative of [safePath, sourcePath])
          fs.rmSync(resolveMediaCachePath(relative), { force: true })
        if (linked) fs.rmdirSync(linkPath)
        const resolved = path.resolve(directory)
        assert.ok(
          resolved.startsWith(`${path.resolve(os.tmpdir())}${path.sep}`),
        )
        fs.rmSync(resolved, { recursive: true, force: true })
      }
    })
