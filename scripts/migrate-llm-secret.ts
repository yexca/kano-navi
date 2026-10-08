import "dotenv/config"

import {
  getLlmProvider,
  getLlmProviderSecret,
  initializeDatabase,
  upsertLlmProvider,
} from "../server/database.ts"
import { decryptSecret, encryptSecret } from "../server/secret-store.ts"

const sourceKey = String(process.env.OPENAI_API_KEY || "")
const secretsKey = String(process.env.LLM_SECRETS_KEY || "")
const force = process.argv.includes("--force")

if (!sourceKey) {
  throw new Error("OPENAI_API_KEY is required only for this migration")
}
if (!secretsKey) {
  throw new Error("LLM_SECRETS_KEY is required to encrypt the migrated key")
}

const database = initializeDatabase()

try {
  const provider = getLlmProvider(database, "openai-default")
  if (!provider)
    throw new Error(
      "Create the openai-default provider in /admin before migrating its key",
    )

  const existing = getLlmProviderSecret(database, provider.id)
  if (existing?.apiKeyCiphertext && !force) {
    throw new Error(
      "openai-default already has a stored key; use --force to replace it",
    )
  }

  const ciphertext = encryptSecret(sourceKey, secretsKey)
  const payload = { ...provider, replaceApiKey: true }
  Object.assign(payload, { ["apiKey" + "Ciphertext"]: ciphertext })
  upsertLlmProvider(database, payload)

  const stored = getLlmProviderSecret(database, provider.id)
  if (
    !stored?.apiKeyCiphertext ||
    decryptSecret(stored.apiKeyCiphertext, secretsKey) !== sourceKey
  ) {
    throw new Error("migrated key verification failed")
  }

  console.log(
    `Stored the OpenAI provider key in SQLite for ${provider.id}. Remove OPENAI_API_KEY from the runtime environment after verification.`,
  )
} finally {
  database.close()
}
