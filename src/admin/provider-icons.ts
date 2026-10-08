import openai from "../assets/providers/openai.svg"
import anthropic from "../assets/providers/anthropic.svg"
import deepseek from "../assets/providers/deepseek.svg"
import openrouter from "../assets/providers/openrouter.svg"
import gemini from "../assets/providers/gemini.svg"

const icons = { openai, anthropic, deepseek, openrouter, gemini }
const hosts = {
  "api.openai.com": "openai",
  "api.anthropic.com": "anthropic",
  "api.deepseek.com": "deepseek",
  "openrouter.ai": "openrouter",
  "generativelanguage.googleapis.com": "gemini",
}

export function providerIcon(provider: {
  id?: string
  name?: string
  baseUrl?: string
}) {
  try {
    const brand = hosts[new URL(provider.baseUrl).hostname]
    if (brand) return icons[brand]
  } catch {
    // Unsaved preset forms and custom providers may have no URL yet.
  }
  const brand = Object.keys(icons).find(
    (id) =>
      provider.name?.toLowerCase() === id ||
      provider.id === id ||
      provider.id?.startsWith(`${id}-`),
  )
  return brand ? icons[brand] : null
}
