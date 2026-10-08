/** Shared by the admin previews and server requests; contains no credentials. */
export function providerApiRoot(baseUrl: string) {
  const base = String(baseUrl || "")
    .trim()
    .replace(/\/+$/u, "")
    .replace(/\/(?:responses|chat\/completions|messages|models)$/u, "")
  if (!base) return ""
  try {
    const url = new URL(base)
    const path = url.pathname.replace(/\/+$/u, "")
    const roots = {
      "openrouter.ai": "/api/v1",
      "generativelanguage.googleapis.com": "/v1beta/openai",
      "dashscope.aliyuncs.com": "/compatible-mode/v1",
    }
    if (!path || path === "/api" || path === "/compatible-mode") {
      url.pathname = roots[url.hostname] || `${path}/v1`
      return url.toString().replace(/\/+$/u, "")
    }
  } catch {
    // Incomplete URLs remain editable in the form; the API validates on save.
  }
  return base
}

export function inferenceEndpoint(baseUrl: string, protocol: string) {
  const base = providerApiRoot(baseUrl)
  if (!base) return ""
  const suffix =
    protocol === "anthropic-messages"
      ? "messages"
      : protocol === "openai-chat-completions"
        ? "chat/completions"
        : "responses"
  return `${base}/${suffix}`
}

export function modelsEndpoint(baseUrl: string) {
  const base = providerApiRoot(baseUrl)
  return base ? `${base}/models` : ""
}
