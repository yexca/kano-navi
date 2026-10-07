import assert from "node:assert/strict"
import test from "node:test"

import { createAdminAuth, resolveAdminConfig } from "./admin-auth.js"

test("startup requires a production password unless development is explicit", () => {
  for (const environment of [
    {},
    { NODE_ENV: "development" },
    { NODE_ENV: "production" },
    { APP_MODE: "" },
    { APP_MODE: "production" },
  ]) {
    assert.throws(
      () => resolveAdminConfig(environment),
      /ADMIN_PASSWORD must contain at least 12 characters/u,
    )
  }

  const config = resolveAdminConfig({
    NODE_ENV: "development",
    ADMIN_PASSWORD: "not-a-real-password",
  })
  assert.equal(config.mode, "production")
  assert.equal(config.adminPassword, "not-a-real-password")
})

test("production rejects empty, whitespace-only, and padded short passwords", () => {
  for (const environment of [
    { ADMIN_PASSWORD: "" },
    { ADMIN_PASSWORD: "".padEnd(20) },
    { ADMIN_PASSWORD: "not-a-real" },
    { ADMIN_PASSWORD: "not-a-real".padStart(11).padEnd(12) },
  ]) {
    assert.throws(
      () => resolveAdminConfig(environment),
      /ADMIN_PASSWORD must contain at least 12 characters/u,
    )
    assert.throws(
      () =>
        createAdminAuth({
          mode: "production",
          adminPassword: environment.ADMIN_PASSWORD,
        }),
      /ADMIN_PASSWORD must contain at least 12 characters/u,
    )
  }
})

test("explicit development permits password-free local startup", () => {
  assert.deepEqual(resolveAdminConfig({ APP_MODE: "development" }), {
    mode: "development",
    adminPassword: "",
  })
  assert.throws(
    () => resolveAdminConfig({ APP_MODE: "invalid" }),
    /APP_MODE must be development or production/u,
  )
})
