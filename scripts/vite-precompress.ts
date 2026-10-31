import { readdir, readFile, stat, writeFile } from "node:fs/promises"
import path from "node:path"
import { promisify } from "node:util"
import zlib from "node:zlib"
import type { Plugin } from "vite"

const brotli = promisify(zlib.brotliCompress)
const gzip = promisify(zlib.gzip)

/** The text build files that get siblings, matching the Go static site. */
const PRECOMPRESSIBLE = /\.(?:html|js|css|json|svg)$/iu

/** Smaller files gain too little to be worth a second request path. */
export const MIN_PRECOMPRESS_BYTES = 1024

/**
 * Writes `.br` and `.gz` siblings next to the build's text files once the
 * bundle and the public directory are in the output directory. The Go static
 * site sends a sibling to a client that accepts its encoding.
 */
export function precompress(): Plugin {
  return {
    name: "kano-precompress",
    apply: "build",
    writeBundle: {
      sequential: true,
      order: "post",
      async handler(options) {
        if (options.dir) await precompressDirectory(options.dir)
      },
    },
  }
}

/**
 * Compresses every eligible file under root at the strongest settings. Dot
 * directories (Vite's manifest) are skipped, and a sibling that would not be
 * smaller than its original is not written. Returns the compressed paths,
 * relative to root.
 */
export async function precompressDirectory(root: string): Promise<string[]> {
  const files = await textFiles(root, "")
  await Promise.all(
    files.map(async (relative) => {
      const name = path.join(root, relative)
      const content = await readFile(name)
      const [br, gz] = await Promise.all([
        brotli(content, {
          params: {
            [zlib.constants.BROTLI_PARAM_MODE]: zlib.constants.BROTLI_MODE_TEXT,
            [zlib.constants.BROTLI_PARAM_QUALITY]:
              zlib.constants.BROTLI_MAX_QUALITY,
            [zlib.constants.BROTLI_PARAM_SIZE_HINT]: content.length,
          },
        }),
        gzip(content, { level: zlib.constants.Z_BEST_COMPRESSION }),
      ])
      if (br.length < content.length) await writeFile(`${name}.br`, br)
      if (gz.length < content.length) await writeFile(`${name}.gz`, gz)
    }),
  )
  return files
}

async function textFiles(root: string, relative: string): Promise<string[]> {
  const entries = await readdir(path.join(root, relative), {
    withFileTypes: true,
  })
  const found: string[] = []
  for (const entry of entries) {
    if (entry.name.startsWith(".")) continue
    const child = path.join(relative, entry.name)
    if (entry.isDirectory()) {
      found.push(...(await textFiles(root, child)))
    } else if (entry.isFile() && PRECOMPRESSIBLE.test(entry.name)) {
      const { size } = await stat(path.join(root, child))
      if (size >= MIN_PRECOMPRESS_BYTES) found.push(child)
    }
  }
  return found
}
