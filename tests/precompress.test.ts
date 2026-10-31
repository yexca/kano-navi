import assert from "node:assert/strict"
import {
  mkdir,
  mkdtemp,
  readdir,
  readFile,
  rm,
  writeFile,
} from "node:fs/promises"
import { tmpdir } from "node:os"
import path from "node:path"
import test from "node:test"
import { brotliDecompressSync, gunzipSync } from "node:zlib"

import {
  MIN_PRECOMPRESS_BYTES,
  precompressDirectory,
} from "../scripts/vite-precompress.ts"

test("the build writes .br and .gz siblings for its larger text files only", async (t) => {
  const root = await mkdtemp(path.join(tmpdir(), "kano-precompress-"))
  t.after(() => rm(root, { recursive: true, force: true }))
  const script = "console.log('synthetic')\n".repeat(200)
  const files: Record<string, string> = {
    "index.html": `<!doctype html><title>synthetic</title>${" ".repeat(MIN_PRECOMPRESS_BYTES)}`,
    "assets/index-AbCd1234.js": script,
    "assets/index-AbCd1234.css": "body{color:red}\n".repeat(100),
    "assets/icon.svg": `<svg xmlns="http://www.w3.org/2000/svg">${"<g/>".repeat(400)}</svg>`,
    "assets/data.json": JSON.stringify({ synthetic: "x".repeat(2000) }),
    "assets/small-AbCd1234.js": "console.log(1)",
    "assets/photo.jpg": "j".repeat(4000),
    "robots.txt": "User-agent: *\n".repeat(200),
    ".vite/manifest.json": JSON.stringify({ synthetic: "x".repeat(2000) }),
  }
  for (const [name, content] of Object.entries(files)) {
    await mkdir(path.dirname(path.join(root, name)), { recursive: true })
    await writeFile(path.join(root, name), content)
  }

  const compressed = await precompressDirectory(root)

  const expected = [
    "index.html",
    "assets/index-AbCd1234.js",
    "assets/index-AbCd1234.css",
    "assets/icon.svg",
    "assets/data.json",
  ]
  assert.deepEqual(
    compressed.map((name) => name.split(path.sep).join("/")).sort(),
    [...expected].sort(),
  )
  for (const name of expected) {
    const original = await readFile(path.join(root, name))
    const br = await readFile(path.join(root, `${name}.br`))
    const gz = await readFile(path.join(root, `${name}.gz`))
    assert.ok(br.length < original.length && gz.length < original.length)
    assert.deepEqual(brotliDecompressSync(br), original)
    assert.deepEqual(gunzipSync(gz), original)
  }
  const siblings = async (directory: string) =>
    (await readdir(path.join(root, directory))).filter((name) =>
      /\.(?:br|gz)$/u.test(name),
    )
  assert.deepEqual(await siblings(".vite"), [])
  assert.ok(
    !(await siblings("assets")).some(
      (name) => name.startsWith("small-") || name.startsWith("photo"),
    ),
  )
  assert.ok(!(await siblings(".")).some((name) => name.startsWith("robots")))
})
