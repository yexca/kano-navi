import assert from "node:assert/strict"
import test from "node:test"
import { createLatestWins } from "../src/admin/latest-wins.ts"

const flush = async () => {
  for (let index = 0; index < 20; index++) await Promise.resolve()
}

const setup = () => {
  const sent: {
    key: string
    value: string[]
    resolve: (stored: string[] | undefined) => void
  }[] = []
  const log: string[] = []
  const save = createLatestWins<string, string[]>({
    send: (key, value) =>
      new Promise((resolve) => sent.push({ key, value, resolve })),
    confirmed: (key, value) => log.push(`confirmed ${key} ${value.join("")}`),
    settled: (key, ok) => log.push(`settled ${key} ${ok}`),
  })
  return { sent, log, save }
}

test("queue saves send one request at a time and coalesce to the latest edit", async () => {
  const { sent, log, save } = setup()
  const first = save("schedule", ["b", "a", "c"])
  const second = save("schedule", ["b", "c", "a"])
  const third = save("schedule", ["c", "b", "a"])
  // Another route keeps its own order and is not held behind this one.
  const other = save("translate", ["x"])
  await flush()
  assert.deepEqual(
    sent.map((item) => `${item.key} ${item.value.join("")}`),
    ["schedule bac", "translate x"],
  )

  sent[0].resolve(["b", "a", "c"])
  await flush()
  // Only the latest waiting order follows; the skipped edit is never sent.
  assert.deepEqual(sent[2].value, ["c", "b", "a"])
  assert.equal(sent.length, 3)
  assert.deepEqual(log, ["confirmed schedule bac"])

  sent[2].resolve(["c", "b", "a"])
  sent[1].resolve(["x"])
  assert.deepEqual(await Promise.all([first, second, third, other]), [
    true,
    true,
    true,
    true,
  ])
  assert.deepEqual(log, [
    "confirmed schedule bac",
    "confirmed schedule cba",
    "settled schedule true",
    "confirmed translate x",
    "settled translate true",
  ])

  // An idle route starts a fresh request straight away.
  void save("schedule", ["a"])
  await flush()
  assert.deepEqual(sent[3].value, ["a"])
})

test("a refused queue save settles as failed and drops the edits made on top of it", async () => {
  const { sent, log, save } = setup()
  const first = save("schedule", ["b", "a"])
  const second = save("schedule", ["a"])
  await flush()
  sent[0].resolve(undefined)
  assert.deepEqual(await Promise.all([first, second]), [false, false])
  assert.equal(sent.length, 1)
  assert.deepEqual(log, ["settled schedule false"])

  // A request that throws is a refusal too, and the route recovers after it.
  const failing = createLatestWins<string, string[]>({
    send: async () => {
      throw new Error("synthetic network failure")
    },
    confirmed: () => assert.fail("nothing was stored"),
    settled: (key, ok) => log.push(`settled ${key} ${ok}`),
  })
  assert.equal(await failing("schedule", ["a"]), false)
  const retry = save("schedule", ["a", "b"])
  await flush()
  sent[1].resolve(["a", "b"])
  assert.equal(await retry, true)
})
