import crypto from "node:crypto"

const version = "v1"

export function validateLlmSecretsKey(environment = process.env) {
  if (!String(environment.LLM_SECRETS_KEY || "").trim()) {
    throw new Error("LLM_SECRETS_KEY is not configured; refusing to start")
  }
}

function keyBytes(secret) {
  const value = String(secret || "")
  if (!value) return null
  return crypto.createHash("sha256").update(value).digest()
}

/**
 * Encrypt a runtime secret for storage in SQLite. The encryption key itself
 * remains in the process environment and is never persisted by this module.
 */
export function encryptSecret(value, secret) {
  const plaintext = String(value || "")
  if (!plaintext) return null
  const key = keyBytes(secret)
  if (!key) throw new Error("LLM_SECRETS_KEY is required to store a secret")
  const nonce = crypto.randomBytes(12)
  const cipher = crypto.createCipheriv("aes-256-gcm", key, nonce)
  const ciphertext = Buffer.concat([
    cipher.update(plaintext, "utf8"),
    cipher.final(),
  ])
  const tag = cipher.getAuthTag()
  return [
    version,
    nonce.toString("base64url"),
    tag.toString("base64url"),
    ciphertext.toString("base64url"),
  ].join(":")
}

export function decryptSecret(value, secret) {
  const encoded = String(value || "")
  if (!encoded) return null
  const key = keyBytes(secret)
  if (!key) throw new Error("LLM_SECRETS_KEY is required to read a secret")
  const [format, nonceValue, tagValue, ciphertextValue] = encoded.split(":")
  if (format !== version || !nonceValue || !tagValue || !ciphertextValue) {
    throw new Error("stored LLM secret has an invalid format")
  }
  try {
    const decipher = crypto.createDecipheriv(
      "aes-256-gcm",
      key,
      Buffer.from(nonceValue, "base64url"),
    )
    decipher.setAuthTag(Buffer.from(tagValue, "base64url"))
    return Buffer.concat([
      decipher.update(Buffer.from(ciphertextValue, "base64url")),
      decipher.final(),
    ]).toString("utf8")
  } catch {
    throw new Error("stored LLM secret could not be decrypted")
  }
}

export function secretConfigured(value) {
  return Boolean(String(value || ""))
}
