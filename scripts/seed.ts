import {
  databasePath,
  initializeDatabase,
  seedDatabase,
} from "../server/database.ts"

const overwrite = process.argv.includes("--overwrite")
const database = initializeDatabase({ seed: false })

try {
  seedDatabase(database, undefined, { overwrite })
  const counts = {}
  for (const table of [
    "profiles",
    "posts",
    "events",
    "videos",
    "focus",
    "timeline",
    "resources",
    "assets",
    "media_assets",
    "media_links",
  ]) {
    counts[table] = database
      .prepare<unknown[], Record<string, any>>(
        `SELECT COUNT(*) AS count FROM ${table}`,
      )
      .get().count
  }
  console.log(`Seeded ${databasePath}`)
  console.log(JSON.stringify(counts))
} finally {
  database.close()
}
