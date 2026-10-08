import assert from "node:assert/strict"
import crypto from "node:crypto"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import { spawnSync } from "node:child_process"
import { fileURLToPath } from "node:url"
import test from "node:test"

import { validateLlmSecretsKey } from "./secret-store.ts"

test("startup encryption key rejects missing and whitespace-only configuration", () => {
  for (const environment of [
    {},
    { LLM_SECRETS_KEY: "" },
    { LLM_SECRETS_KEY: "".padEnd(20) },
  ]) {
    assert.throws(
      () => validateLlmSecretsKey(environment),
      /LLM_SECRETS_KEY is not configured; refusing to start/u,
    )
  }
  assert.doesNotThrow(() =>
    validateLlmSecretsKey({
      LLM_SECRETS_KEY: crypto.randomBytes(32).toString("base64"),
    }),
  )
})

test("runtime entry refuses an unconfigured encryption key before creating SQLite", () => {
  const root = fileURLToPath(new URL("../", import.meta.url))
  const fixture = fs.mkdtempSync(path.join(os.tmpdir(), "kano-startup-"))
  try {
    for (const directory of ["server", "scripts", "src/lib", "src/history"]) {
      fs.cpSync(path.join(root, directory), path.join(fixture, directory), {
        recursive: true,
        filter: (filename) => !filename.endsWith(".test.ts"),
      })
    }
    fs.writeFileSync(
      path.join(fixture, "package.json"),
      JSON.stringify({ type: "module" }),
    )
    fs.symlinkSync(
      path.join(root, "node_modules"),
      path.join(fixture, "node_modules"),
      process.platform === "win32" ? "junction" : "dir",
    )
    for (const mode of ["development", "production"]) {
      const result = spawnSync(process.execPath, ["server/index.ts"], {
        cwd: fixture,
        timeout: 10000,
        encoding: "utf8",
        env: {
          PATH: process.env.PATH,
          SystemRoot: process.env.SystemRoot,
          APP_MODE: mode,
          ADMIN_PASSWORD: "synthetic-password",
          WORKFLOW_SCHEDULER_ENABLED: "0",
        },
      })
      assert.equal(result.status, 1)
      assert.match(
        result.stderr,
        /LLM_SECRETS_KEY is not configured; refusing to start/u,
      )
      assert.equal(
        fs.existsSync(path.join(fixture, "data/database/kano.sqlite")),
        false,
      )
    }
    const configured = spawnSync(
      process.execPath,
      ["--input-type=module", "-e", "await import('./server/index.ts')"],
      {
        cwd: fixture,
        timeout: 10000,
        encoding: "utf8",
        env: {
          PATH: process.env.PATH,
          SystemRoot: process.env.SystemRoot,
          APP_MODE: "production",
          ADMIN_PASSWORD: "synthetic-password",
          LLM_SECRETS_KEY: crypto.randomBytes(32).toString("base64"),
          WORKFLOW_SCHEDULER_ENABLED: "0",
        },
      },
    )
    assert.equal(configured.status, 0, configured.stderr)
    assert.equal(
      fs.existsSync(path.join(fixture, "data/database/kano.sqlite")),
      true,
    )
  } finally {
    fs.rmSync(fixture, { recursive: true, force: true })
  }
})
