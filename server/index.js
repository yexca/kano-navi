import "dotenv/config"

import path from "node:path"
import { fileURLToPath } from "node:url"

import { createApp } from "./app.js"
import { databasePath, initializeDatabase } from "./database.js"

const projectDirectory = path.dirname(fileURLToPath(import.meta.url))
const distDirectory = path.resolve(projectDirectory, "../dist")
const port = Number(process.env.PORT || 8787)
const adminMode =
  process.env.APP_MODE ||
  (process.env.NODE_ENV === "production" ? "production" : "development")

export const database = initializeDatabase()
export const app = createApp({
  database,
  databaseLabel: path.relative(
    path.resolve(projectDirectory, ".."),
    databasePath,
  ),
  staticDirectory: distDirectory,
  adminMode,
  adminPassword: process.env.ADMIN_PASSWORD || "",
})

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  app.listen(port, () => {
    console.log(`Kano status board API listening on http://localhost:${port}`)
  })
}
