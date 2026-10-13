// Export only canonical schema and public static seeds from a disposable
// in-memory database. Never read operator snapshots, configuration or .env.
import { writeFileSync } from "node:fs"
import { join } from "node:path"
import { openDatabase } from "../server/database.ts"
import { seedData } from "../server/seed-data.ts"

const target = join(import.meta.dirname, "../backend-go/internal/store/sqlite")
const database = openDatabase({ filename: ":memory:" })
try {
  const rows = database
    .prepare(
      "SELECT type,name,sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY CASE type WHEN 'table' THEN 0 ELSE 1 END,name",
    )
    .all()
  const schema = rows.map((row) => `${row.sql};`).join("\n\n")
  writeFileSync(join(target, "bootstrap.sql"), `${schema}\n`)
  writeFileSync(
    join(target, "static-seed.json"),
    `${JSON.stringify({ profile: seedData.profile, timeline: seedData.timeline, resources: seedData.resources }, null, 2)}\n`,
  )
  console.log("Go bootstrap schema and public static seeds exported")
} finally {
  database.close()
}
