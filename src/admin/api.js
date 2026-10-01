// Thin wrapper around the local admin API. The console never talks to X,
// YouTube, or a model provider directly; every external request is made by
// the server after an operator action.
export async function request(path, options = {}) {
  const isRawBody = typeof Blob !== "undefined" && options.body instanceof Blob
  const response = await fetch(`/api/admin${path}`, {
    credentials: "same-origin",
    ...options,
    headers: {
      accept: "application/json",
      ...(options.body && !isRawBody
        ? { "content-type": "application/json" }
        : {}),
      ...(options.headers || {}),
    },
  })
  if (response.status === 204) return null
  const payload = await response.json().catch(() => ({}))
  if (!response.ok) {
    const error = new Error(
      payload.message || payload.error || `API ${response.status}`,
    )
    error.status = response.status
    error.code = payload.code || payload.error || "request_failed"
    error.payload = payload
    throw error
  }
  return payload
}

export function jsonBody(value) {
  return JSON.stringify(value)
}

const errorMessageKeys = {
  admin_auth_required: "errors.authRequired",
  invalid_credentials: "errors.invalidCredentials",
  too_many_attempts: "errors.tooManyAttempts",
  csrf_origin_mismatch: "errors.csrf",
  event_not_found: "errors.notFound",
  llm_provider_not_found: "errors.notFound",
  llm_model_not_found: "errors.notFound",
  workflow_not_found: "errors.notFound",
  profile_media_not_found: "errors.notFound",
  profile_media_not_ready: "errors.notFound",
  sync_job_not_found: "errors.notFound",
  sync_jobs_unavailable: "errors.syncUnavailable",
}

/** A translatable message: `{ key, values }` or literal `{ text }`. */
export function adminMessage(key, values = {}) {
  return { key, values }
}

export function renderAdminMessage(message, t) {
  if (!message) return ""
  if (typeof message === "string") return message
  if (message.key) return t(message.key, message.values)
  return message.text || ""
}

export function formatAdminError(error) {
  const key = errorMessageKeys[error?.code]
  if (key) return adminMessage(key)
  if (error?.status === 401) return adminMessage("errors.authRequired")
  if (error?.status >= 500) return adminMessage("errors.requestFailed")
  if (error?.name === "TypeError" && /fetch|network/i.test(error.message)) {
    return adminMessage("errors.network")
  }
  if (!error?.message || /^API \d+$/u.test(error.message)) {
    return adminMessage("errors.requestFailed")
  }
  // Server validation messages are English; anything else is generic.
  if (/[぀-ヿ㐀-鿿]/u.test(error.message)) {
    return adminMessage("errors.requestFailed")
  }
  return { text: error.message }
}
