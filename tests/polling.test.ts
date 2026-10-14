import assert from "node:assert/strict"
import test from "node:test"
import { createRequestScope, startPolling } from "../src/lib/polling.ts"
const flush = async () => {
  for (let index = 0; index < 20; index++) await Promise.resolve()
}

test("polling continues after one 503, backs off to a cap, then recovers its cadence", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  let calls = 0,
    failures = 0,
    failing = true
  const stop = startPolling(
    async () => {
      calls++
      if (failing)
        throw Object.assign(new Error("synthetic unavailable"), { status: 503 })
    },
    { delay: () => 10, maxDelay: 40, onError: () => failures++ },
  )
  t.after(stop)
  for (const [advance, expected] of [
    [10, 1],
    [19, 1],
    [1, 2],
    [39, 2],
    [1, 3],
    [40, 4],
  ]) {
    t.mock.timers.tick(advance)
    await flush()
    assert.equal(calls, expected)
  }
  assert.equal(failures, 4)
  failing = false
  t.mock.timers.tick(40)
  await flush()
  assert.equal(calls, 5)
  t.mock.timers.tick(10)
  await flush()
  assert.equal(calls, 6)
  stop()
  t.mock.timers.tick(1000)
  await flush()
  assert.equal(calls, 6)
})

test("polling never overlaps a stalled request and cleanup prevents later scheduling", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  let calls = 0,
    release
  const stop = startPolling(
    async () => {
      calls++
      await new Promise<void>((resolve) => {
        release = resolve
      })
    },
    { delay: () => 10 },
  )
  t.mock.timers.tick(10)
  await flush()
  t.mock.timers.tick(10000)
  await flush()
  assert.equal(calls, 1)
  stop()
  release()
  await flush()
  t.mock.timers.tick(10000)
  await flush()
  assert.equal(calls, 1)
})

test("request deadlines abort even an uncooperative response and preserve a subsequent success", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] })
  const scope = createRequestScope(20)
  let signal
  const pending = scope.run("snapshot", async (value) => {
    signal = value
    return new Promise(() => {})
  })
  const rejected = assert.rejects(pending, { name: "TimeoutError" })
  t.mock.timers.tick(20)
  await rejected
  assert.equal(signal.aborted, true)
  assert.equal(
    await scope.run("snapshot", async () => "recovered"),
    "recovered",
  )
})

test("superseded and unmounted reads cannot commit late responses", async () => {
  const scope = createRequestScope(1000)
  let release
  const pending = scope.run(
    "snapshot",
    async () =>
      new Promise<string>((resolve) => {
        release = resolve
      }),
  )
  const rejected = assert.rejects(pending, { name: "AbortError" })
  assert.equal(await scope.run("snapshot", async () => "new"), "new")
  release("old")
  await rejected
  const second = scope.run("activity", async () => new Promise(() => {}))
  const unmounted = assert.rejects(second, { name: "AbortError" })
  scope.dispose()
  await unmounted
  await assert.rejects(
    scope.run("activity", async () => "late"),
    { name: "AbortError" },
  )
})
