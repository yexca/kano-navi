// Low-level transport for /api/admin. The console never talks to X, YouTube
// or a model provider directly; every external request is made by the server
// after an operator action. Endpoint calls live in use-admin-data.ts.

import { formatDateTime } from "@/admin/format"

export class ApiError extends Error {
  status: number
  code: string
  /** When a Bilibili captcha backoff ends (bilibili_blocked). */
  blockedUntil?: string
  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.code = code
  }
}

export interface RequestOptions {
  method?: string
  body?: unknown
  signal?: AbortSignal
}

export async function request<T = any>(
  path: string,
  { method = "GET", body, signal }: RequestOptions = {},
): Promise<T> {
  const raw = typeof Blob !== "undefined" && body instanceof Blob
  const response = await fetch(`/api/admin${path}`, {
    method,
    signal,
    credentials: "same-origin",
    headers: {
      accept: "application/json",
      ...(body !== undefined && !raw
        ? { "content-type": "application/json" }
        : {}),
      ...(raw ? { "content-type": (body as Blob).type } : {}),
    },
    body:
      body === undefined
        ? undefined
        : raw
          ? (body as Blob)
          : JSON.stringify(body),
  })
  if (response.status === 204) return null as T
  const payload = await response.json().catch(() => ({}))
  if (!response.ok) {
    const error = new ApiError(
      response.status,
      payload.error || "request_failed",
      payload.message || payload.error || `API ${response.status}`,
    )
    // A busy queue answers with the run that holds it; a Bilibili captcha
    // backoff with the time reads resume.
    Object.assign(error, {
      run: payload.run,
      blockedUntil: payload.blockedUntil,
    })
    throw error
  }
  return payload as T
}

/** A translatable message: `{ key, values }` or a literal `{ text }`. */
export type Message =
  { key: string; values?: Record<string, any> } | { text: string }

export const message = (key: string, values: Record<string, any> = {}) => ({
  key,
  values,
})

const errorKeys: Record<string, string> = {
  admin_auth_required: "admin.error.authRequired",
  invalid_credentials: "admin.error.invalidCredentials",
  too_many_attempts: "admin.error.tooManyAttempts",
  csrf_origin_mismatch: "admin.error.csrf",
  busy: "admin.error.busy",
  read_only: "admin.error.readOnly",
  unavailable: "admin.error.unavailable",
  admin_unavailable: "admin.error.unavailable",
  database_unavailable: "admin.error.unavailable",
  request_too_large: "admin.error.tooLarge",
  merge_conflict: "admin.error.mergeConflict",
}

/** Operator-facing text for a failed request. Validation messages from the
 * server are already safe to show and stay in English. */
export function errorMessage(error: any): Message {
  if (error?.name === "TimeoutError") return message("admin.error.timeout")
  if (!(error instanceof ApiError)) return message("admin.error.network")
  if (error.code === "invalid_input") return { text: error.message }
  if (errorKeys[error.code]) return message(errorKeys[error.code])
  if (error.code === "bilibili_blocked")
    return message("admin.error.bilibiliBlocked", {
      time: formatDateTime(error.blockedUntil, document.documentElement.lang),
    })
  if (error.code.startsWith("bilibili_"))
    return message("admin.error.bilibiliUnavailable", { code: error.code })
  if (error.status === 404) return message("admin.error.notFound")
  return message("admin.error.requestFailed")
}

export function renderMessage(
  value: Message | null | undefined,
  t: (key: string, values?: Record<string, any>) => string,
) {
  if (!value) return ""
  return "text" in value ? value.text : t(value.key, value.values)
}
