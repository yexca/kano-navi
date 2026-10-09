/** Shared lifecycle/deadline handling; callers own URLs, payloads and state. */
export function createRequestScope(timeoutMs = 12000) {
  let active = true
  const requests = new Map<string, AbortController>()
  return {
    get active() {
      return active
    },
    busy(key: string) {
      return requests.has(key)
    },
    activate() {
      active = true
    },
    cancelKey(key: string) {
      requests.get(key)?.abort()
      requests.delete(key)
    },
    cancel() {
      for (const controller of requests.values()) controller.abort()
      requests.clear()
    },
    dispose() {
      active = false
      this.cancel()
    },
    async run<T>(
      key: string,
      work: (signal: AbortSignal) => Promise<T>,
    ): Promise<T> {
      if (!active) throw new DOMException("Unmounted", "AbortError")
      requests.get(key)?.abort()
      const controller = new AbortController()
      requests.set(key, controller)
      const timeout = setTimeout(
        () =>
          controller.abort(
            new DOMException("Request timed out", "TimeoutError"),
          ),
        timeoutMs,
      )
      let rejectAbort: () => void
      const aborted = new Promise<never>((_, reject) => {
        rejectAbort = () =>
          reject(
            controller.signal.reason ||
              new DOMException("Request cancelled", "AbortError"),
          )
        controller.signal.addEventListener("abort", rejectAbort, { once: true })
      })
      try {
        const value = await Promise.race([work(controller.signal), aborted])
        if (
          !active ||
          controller.signal.aborted ||
          requests.get(key) !== controller
        )
          throw new DOMException("Stale request", "AbortError")
        return value
      } finally {
        controller.abort()
        clearTimeout(timeout)
        controller.signal.removeEventListener("abort", rejectAbort)
        if (requests.get(key) === controller) requests.delete(key)
      }
    },
  }
}

/** Reschedule after success AND failure, with one task in flight and bounded backoff. */
export function startPolling(
  task: () => Promise<unknown>,
  {
    delay,
    maxDelay = 60000,
    onError = () => {},
  }: {
    delay: () => number
    maxDelay?: number
    onError?: (error: any) => void
  },
) {
  let active = true,
    failures = 0
  let timer: ReturnType<typeof setTimeout>
  const schedule = () => {
    if (active)
      timer = setTimeout(
        tick,
        Math.min(maxDelay, delay() * 2 ** Math.min(failures, 8)),
      )
  }
  const tick = async () => {
    if (!active) return
    try {
      await task()
      failures = 0
    } catch (error) {
      failures++
      if (active) onError(error)
    } finally {
      schedule()
    }
  }
  schedule()
  return () => {
    active = false
    clearTimeout(timer)
  }
}
