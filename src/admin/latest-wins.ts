/**
 * Saves one value per key with at most one request in flight. An edit made
 * while a request runs replaces any edit still waiting, so only the latest
 * desired value is sent next.
 *
 * `send` resolves with the value the server stored, or undefined when it
 * refused. Every stored value reaches `confirmed`. `settled` runs once the key
 * has nothing left to send: `ok` is false after a refusal, which also drops a
 * waiting edit because it was made on top of the refused one.
 */
export function createLatestWins<K, T>({
  send,
  confirmed,
  settled,
}: {
  send: (key: K, value: T) => Promise<T | undefined>
  confirmed: (key: K, value: T) => void
  settled: (key: K, ok: boolean) => void
}) {
  type Slot = { waiting?: { value: T }; resolvers: ((ok: boolean) => void)[] }
  const slots = new Map<K, Slot>()

  const drain = async (key: K, slot: Slot) => {
    let ok = true
    while (slot.waiting) {
      const { value } = slot.waiting
      slot.waiting = undefined
      let stored: T | undefined
      try {
        stored = await send(key, value)
      } catch {
        stored = undefined
      }
      if (stored === undefined) {
        slot.waiting = undefined
        ok = false
        break
      }
      confirmed(key, stored)
    }
    slots.delete(key)
    settled(key, ok)
    for (const resolve of slot.resolvers) resolve(ok)
  }

  /** Resolves with whether this value, or a later one for the key, stored. */
  return (key: K, value: T) =>
    new Promise<boolean>((resolve) => {
      const running = slots.get(key)
      const slot = running || { resolvers: [] }
      slot.waiting = { value }
      slot.resolvers.push(resolve)
      if (!running) {
        slots.set(key, slot)
        void drain(key, slot)
      }
    })
}
