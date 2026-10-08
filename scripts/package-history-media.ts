import fs from "node:fs/promises"
import path from "node:path"
import { fileURLToPath } from "node:url"

import { prepareHistoryMedia } from "./import-history-media.ts"

const projectRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
)

export async function packageHistoryMedia(
  directory: string,
  destination = path.join(projectRoot, "public/assets/history"),
  catalog?: import("./import-history-media.ts").HistoryImportItem[],
) {
  const prepared = await prepareHistoryMedia(directory, catalog)
  for (const { item } of prepared) {
    if (!/^[a-zA-Z0-9_-]+\.(png|jpg|jpeg|webp|gif)$/u.test(item.filename))
      throw new Error(`unsafe packaged image filename: ${item.id}`)
  }
  await fs.mkdir(destination, { recursive: true })
  for (const { item, content } of prepared) {
    await fs.writeFile(path.join(destination, item.filename), content)
  }
  return {
    packaged: prepared.length,
    bytes: prepared.reduce((sum, { content }) => sum + content.length, 0),
  }
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const args = process.argv.slice(2)
  if (args.length !== 2 || args[0] !== "--from") {
    console.error(
      "Usage: npm run history:package -- --from <archive-directory>",
    )
    process.exitCode = 1
  } else {
    const result = await packageHistoryMedia(args[1])
    console.log(
      `Packaged ${result.packaged} history images (${(result.bytes / 1024 / 1024).toFixed(2)} MiB).`,
    )
  }
}
