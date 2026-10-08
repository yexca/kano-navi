/** One deadline covers headers and the complete, byte-bounded body. */
export async function fetchBounded(
  url,
  {
    fetchImpl = fetch,
    timeoutMs = 10_000,
    maxBytes = 4 * 1024 * 1024,
    ...options
  }: Record<string, any> = {},
) {
  const controller = new AbortController()
  let response: Response | undefined
  let reader: ReadableStreamDefaultReader<Uint8Array> | undefined
  const cancel = () => {
    // Cancellation must not wait for an uncooperative upstream stream.
    void (reader ? reader.cancel() : response?.body?.cancel())?.catch(() => {})
  }
  let timer: ReturnType<typeof setTimeout>
  const deadline = new Promise<never>((_resolve, reject) => {
    timer = setTimeout(
      () => {
        controller.abort()
        cancel()
        reject(new Error("request timed out"))
      },
      Math.max(1, Number(timeoutMs) || 10_000),
    )
  })
  const request = (async () => {
    response = await fetchImpl(url, { ...options, signal: controller.signal })
    if (controller.signal.aborted) {
      cancel()
      throw new Error("request timed out")
    }
    if (!response.ok) {
      cancel()
      return { response, body: Buffer.alloc(0) }
    }
    if (Number(response.headers.get("content-length")) > maxBytes)
      throw new Error("response exceeds the size limit")
    if (!response.body) return { response, body: Buffer.alloc(0) }
    reader = response.body.getReader()
    const chunks: Uint8Array[] = []
    let size = 0
    try {
      while (true) {
        const { done, value } = await reader.read()
        if (controller.signal.aborted) throw new Error("request timed out")
        if (done) break
        size += value.byteLength
        if (size > maxBytes) throw new Error("response exceeds the size limit")
        chunks.push(value)
      }
      return { response, body: Buffer.concat(chunks, size) }
    } finally {
      cancel()
      reader.releaseLock()
    }
  })()
  try {
    return await Promise.race([request, deadline])
  } finally {
    clearTimeout(timer!)
    controller.abort()
    cancel()
  }
}
