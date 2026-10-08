import type { Server } from "node:http"

/** Require a TCP listener before constructing a local integration-test URL. */
export function listeningPort(server: Server): number {
  const address = server.address()
  if (!address || typeof address === "string") {
    throw new Error("Expected a listening TCP server")
  }
  return address.port
}
