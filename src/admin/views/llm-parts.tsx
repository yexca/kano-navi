import { Languages, Route, ScanSearch } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { modelTags } from "@/admin/llm"
import { providerIcon } from "@/admin/provider-icons"
import type {
  LlmCatalog,
  Provider,
  QueueRoute,
  RouteTarget,
} from "@/admin/types"
import { Badge, type Tone } from "@/admin/ui"

export function ProviderMark({ provider }: { provider: Partial<Provider> }) {
  const icon = providerIcon(provider)
  return (
    <span className="adm-provider-mark" aria-hidden>
      {icon ? (
        <img src={icon} alt="" />
      ) : (
        (provider.name || provider.id || "?").slice(0, 1).toUpperCase()
      )}
    </span>
  )
}

export function TagChips({
  tags,
  routing,
}: {
  tags: string[]
  routing?: boolean
}) {
  const { t } = useAppSettings()
  return (
    <span className="adm-chip-row">
      {modelTags
        .filter((tag) => tags.includes(tag.id) && (!routing || tag.routing))
        .map((tag) => (
          <Badge key={tag.id} tone={tag.tone as Tone} icon={tag.icon}>
            {t(`admin.tag.${tag.id}`)}
          </Badge>
        ))}
    </span>
  )
}

/**
 * Each queue's catalog field and the stages it serves: the routing tag each
 * stage needs. Schedule extraction is text-only; image extraction handles
 * image inputs and the translation queue handles short text tasks.
 */
export const queueRoutes: Record<
  QueueRoute,
  {
    key: "queue" | "verificationQueue" | "translationQueue"
    stages: readonly ("text" | "image")[]
    needs?: "text" | "image"
  }
> = {
  schedule: { key: "queue", stages: ["text"], needs: "text" },
  verify: { key: "verificationQueue", stages: ["image"], needs: "image" },
  translate: { key: "translationQueue", stages: ["text"], needs: "text" },
}

export const routeOrder: QueueRoute[] = ["schedule", "verify", "translate"]

export const routeIcons = {
  schedule: Route,
  verify: ScanSearch,
  translate: Languages,
}

export const queueKey = (route: QueueRoute) => queueRoutes[route].key

export const queueOf = (catalog: LlmCatalog, route: QueueRoute) =>
  catalog[queueKey(route)] || []

export const sameTarget = (
  target: RouteTarget,
  providerId: string,
  id: string,
) => target.providerId === providerId && target.modelId === id
