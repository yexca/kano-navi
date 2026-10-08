export function providerHeaders(protocol: string, apiKey: string) {
  return {
    "content-type": "application/json",
    ...(protocol === "anthropic-messages"
      ? { "x-api-key": apiKey, "anthropic-version": "2023-06-01" }
      : { authorization: `Bearer ${apiKey}` }),
  }
}

/** Bound actual streamed bytes, including bodies with no Content-Length. */
export async function readLlmJson(
  response: Response,
  maxBytes = 4 * 1024 * 1024,
) {
  if (Number(response.headers.get("content-length")) > maxBytes) {
    await response.body?.cancel()
    throw new Error("LLM response too large")
  }
  if (!response.body) throw new Error("LLM response is empty")
  const reader = response.body.getReader()
  const chunks: Uint8Array[] = []
  let size = 0
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      size += value.byteLength
      if (size > maxBytes) {
        await reader.cancel()
        throw new Error("LLM response too large")
      }
      chunks.push(value)
    }
  } finally {
    reader.releaseLock()
  }
  return JSON.parse(Buffer.concat(chunks).toString("utf8"))
}

export function outputTokenLimit(protocol: string, limit: number) {
  if (protocol === "anthropic-messages") return { max_tokens: limit }
  if (protocol === "openai-chat-completions")
    return { max_completion_tokens: limit }
  return { max_output_tokens: limit }
}
