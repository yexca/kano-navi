import crypto from "node:crypto"

const cookieName = "kano_admin_session"

function parseCookies(header = "") {
  const cookies = new Map()
  for (const item of String(header).split(";")) {
    const separator = item.indexOf("=")
    if (separator <= 0) continue
    cookies.set(
      item.slice(0, separator).trim(),
      item.slice(separator + 1).trim(),
    )
  }
  return cookies
}

function secureRequest(request) {
  return (
    request.secure ||
    String(request.headers["x-forwarded-proto"] || "").toLowerCase() === "https"
  )
}

function passwordMatches(actual, expected) {
  const actualBuffer = Buffer.from(String(actual || ""))
  const expectedBuffer = Buffer.from(String(expected || ""))
  return (
    actualBuffer.length === expectedBuffer.length &&
    crypto.timingSafeEqual(actualBuffer, expectedBuffer)
  )
}

export function createAdminAuth({
  mode = "development",
  adminPassword = "",
  sessionTtlMs = 8 * 60 * 60 * 1000,
} = {}) {
  if (!["development", "production"].includes(mode)) {
    throw new Error("APP_MODE must be development or production")
  }
  if (mode === "production" && String(adminPassword).length < 12) {
    throw new Error(
      "ADMIN_PASSWORD must contain at least 12 characters in production mode",
    )
  }

  const sessions = new Map()
  const attempts = new Map()

  function purgeExpired() {
    const now = Date.now()
    for (const [token, expiresAt] of sessions) {
      if (expiresAt <= now) sessions.delete(token)
    }
    for (const [address, record] of attempts) {
      if (record.resetAt <= now) attempts.delete(address)
    }
  }

  function isAuthenticated(request) {
    if (mode === "development") return true
    purgeExpired()
    const token = parseCookies(request.headers.cookie).get(cookieName)
    return Boolean(token && (sessions.get(token) || 0) > Date.now())
  }

  function sessionPayload(request) {
    return {
      mode,
      requiresPassword: mode === "production",
      authenticated: isAuthenticated(request),
    }
  }

  function login(request, response) {
    if (mode === "development") {
      response.json(sessionPayload(request))
      return
    }
    purgeExpired()
    const address = String(
      request.ip || request.socket.remoteAddress || "unknown",
    )
    const now = Date.now()
    const record = attempts.get(address) || {
      count: 0,
      resetAt: now + 5 * 60 * 1000,
    }
    if (record.count >= 5 && record.resetAt > now) {
      response.status(429).json({ error: "too_many_attempts" })
      return
    }
    if (!passwordMatches(request.body?.password, adminPassword)) {
      attempts.set(address, { ...record, count: record.count + 1 })
      response.status(401).json({ error: "invalid_credentials" })
      return
    }
    attempts.delete(address)
    const token = crypto.randomBytes(32).toString("base64url")
    sessions.set(token, now + sessionTtlMs)
    const secure = secureRequest(request) ? "; Secure" : ""
    response.set(
      "Set-Cookie",
      `${cookieName}=${token}; Path=/api/admin; HttpOnly; SameSite=Strict; Max-Age=${Math.floor(sessionTtlMs / 1000)}${secure}`,
    )
    response.json({
      mode,
      requiresPassword: true,
      authenticated: true,
    })
  }

  function logout(request, response) {
    const token = parseCookies(request.headers.cookie).get(cookieName)
    if (token) sessions.delete(token)
    const secure = secureRequest(request) ? "; Secure" : ""
    response.set(
      "Set-Cookie",
      `${cookieName}=; Path=/api/admin; HttpOnly; SameSite=Strict; Max-Age=0${secure}`,
    )
    response.status(204).end()
  }

  function requireAuth(request, response, next) {
    if (isAuthenticated(request)) {
      next()
      return
    }
    response.status(401).json({ error: "admin_auth_required" })
  }

  return {
    isAuthenticated,
    login,
    logout,
    mode,
    requireAuth,
    sessionPayload,
  }
}

export { cookieName }
