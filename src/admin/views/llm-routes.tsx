import { useEffect, useRef, useState } from "react"
import {
  ArrowDown,
  ArrowUp,
  Check,
  ExternalLink,
  GripVertical,
  Plus,
  RefreshCw,
  Route,
  Search,
  TriangleAlert,
  X,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { formatRelative, japanStamp } from "@/admin/format"
import { describeTarget } from "@/admin/llm"
import type {
  LlmCatalog,
  Provider,
  QueueRoute,
  RouteTarget,
} from "@/admin/types"
import { Badge, Button, Empty, IconButton, Loading, Panel } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { cn } from "@/lib/utils"
import {
  sameTarget,
  ProviderMark,
  TagChips,
  queueRoutes,
  queueOf,
  routeIcons,
} from "@/admin/views/llm-parts"

// --- queues -----------------------------------------------------------------

/** A searchable list of enabled catalog models, opened from a button. */
function ModelPicker({
  catalog,
  targets,
  needs,
  onPick,
}: {
  catalog: LlmCatalog
  targets: RouteTarget[]
  needs?: "text" | "image"
  onPick: (target: RouteTarget) => void
}) {
  const { t } = useAppSettings()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return undefined
    const onDown = (event: MouseEvent) => {
      if (!box.current?.contains(event.target as Node)) setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false)
    }
    document.addEventListener("mousedown", onDown)
    document.addEventListener("keydown", onKey)
    return () => {
      document.removeEventListener("mousedown", onDown)
      document.removeEventListener("keydown", onKey)
    }
  }, [open])
  const needle = query.trim().toLowerCase()
  const candidates = catalog.models.filter(
    (model) =>
      model.enabled &&
      (!needs || model.capabilities.includes(needs)) &&
      !targets.some((target) =>
        sameTarget(target, model.providerId, model.id),
      ) &&
      `${model.id} ${model.name || ""}`.toLowerCase().includes(needle),
  )
  return (
    <div className="adm-picker" ref={box}>
      <Button
        size="sm"
        icon={Plus}
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        {t("admin.route.addModel")}
      </Button>
      {open ? (
        <div className="adm-picker-menu">
          <span className="adm-input-icon">
            <Search aria-hidden />
            <input
              type="search"
              autoFocus
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t("admin.provider.filterRemote")}
              aria-label={t("admin.provider.filterRemote")}
            />
          </span>
          {candidates.length ? (
            <ul>
              {catalog.providers.map((provider) => {
                const models = candidates.filter(
                  (model) => model.providerId === provider.id,
                )
                return models.length ? (
                  <li key={provider.id}>
                    <p className="adm-picker-group">
                      <ProviderMark provider={provider} />
                      {provider.name}
                    </p>
                    {models.map((model) => (
                      <button
                        key={model.id}
                        type="button"
                        onClick={() => {
                          onPick({ providerId: provider.id, modelId: model.id })
                          setOpen(false)
                          setQuery("")
                        }}
                      >
                        <span>{model.name || model.id}</span>
                        <TagChips tags={model.tags} routing />
                      </button>
                    ))}
                  </li>
                ) : null
              })}
            </ul>
          ) : (
            <p className="adm-muted adm-picker-empty">
              {t("admin.route.noModels")}
            </p>
          )}
        </div>
      ) : null}
    </div>
  )
}

/**
 * One ordered queue: text schedule extraction, image schedule extraction, or
 * translation/light tasks, whose models need the image or text tag. Every
 * change is saved at once.
 */
export function QueueEditor({
  catalog,
  route,
  onSave,
}: {
  catalog: LlmCatalog
  route: QueueRoute
  onSave: (route: QueueRoute, targets: RouteTarget[]) => Promise<boolean>
}) {
  const { t } = useAppSettings()
  const { stages: needed, needs } = queueRoutes[route]
  const targets = queueOf(catalog, route)
  const [dragging, setDragging] = useState<number | null>(null)
  const [over, setOver] = useState<number | null>(null)
  const described = targets.map((target) =>
    describeTarget(target, catalog.providers, catalog.models),
  )
  // Each stage calls the first usable model with the tag it needs.
  const stages = needed.map((tag) => ({
    needs: tag,
    index: described.findIndex(
      (item) => !item.issue && item.capabilities.includes(tag),
    ),
  }))
  const save = (next: RouteTarget[]) => void onSave(route, next)
  const move = (from: number, to: number) => {
    if (from === to) return
    const next = [...targets]
    const [item] = next.splice(from, 1)
    next.splice(to, 0, item)
    save(next)
  }
  return (
    <div className="adm-queue">
      {targets.length ? (
        <ol className="adm-queue-list">
          {described.map((item, index) => {
            const target = targets[index]
            const roles = stages.filter((stage) => stage.index === index)
            return (
              <li
                key={`${target.providerId}:${target.modelId}`}
                className={cn(
                  item.issue && "has-issue",
                  roles.length && "is-used",
                  dragging === index && "is-dragging",
                  over === index && dragging !== index && "is-over",
                )}
                draggable
                onDragStart={(event) => {
                  event.dataTransfer.effectAllowed = "move"
                  setDragging(index)
                }}
                onDragOver={(event) => {
                  if (dragging === null) return
                  event.preventDefault()
                  setOver(index)
                }}
                onDrop={(event) => {
                  event.preventDefault()
                  if (dragging !== null) move(dragging, index)
                  setDragging(null)
                  setOver(null)
                }}
                onDragEnd={() => {
                  setDragging(null)
                  setOver(null)
                }}
              >
                <GripVertical className="adm-queue-grip" aria-hidden />
                <span className="adm-route-rank">{index + 1}</span>
                {item.provider ? (
                  <ProviderMark provider={item.provider} />
                ) : null}
                <span className="adm-route-model">
                  <strong>{item.model?.name || item.modelId}</strong>
                  <small>{item.provider?.name || target.providerId}</small>
                  <span className="adm-chip-row adm-queue-tags">
                    {item.issue ? (
                      <Badge tone="danger" icon={TriangleAlert}>
                        {t(`admin.route.issue.${item.issue}`)}
                      </Badge>
                    ) : roles.length ? (
                      roles.map((role) => (
                        <Badge key={role.needs} tone="leaf" icon={Check}>
                          {t(
                            needs
                              ? `admin.route.usedFor.${route}`
                              : `admin.route.usedFor.${role.needs}`,
                          )}
                        </Badge>
                      ))
                    ) : (
                      <span className="adm-muted">
                        {t("admin.route.fallback")}
                      </span>
                    )}
                  </span>
                </span>
                <span className="adm-row-actions">
                  <IconButton
                    icon={ArrowUp}
                    label={t("admin.route.up")}
                    disabled={index === 0}
                    onClick={() => move(index, index - 1)}
                  />
                  <IconButton
                    icon={ArrowDown}
                    label={t("admin.route.down")}
                    disabled={index === targets.length - 1}
                    onClick={() => move(index, index + 1)}
                  />
                  <IconButton
                    icon={X}
                    label={t("admin.route.remove")}
                    onClick={() =>
                      save(targets.filter((_, at) => at !== index))
                    }
                  />
                </span>
              </li>
            )
          })}
        </ol>
      ) : (
        <Empty title={t("admin.route.empty")}>
          {t(
            {
              schedule: "admin.route.emptyHint",
              verify: "admin.route.verifyEmptyHint",
              translate: "admin.route.translationEmptyHint",
            }[route],
          )}
        </Empty>
      )}
      {targets.length
        ? stages
            .filter((stage) => stage.index < 0)
            .map((stage) => (
              <p key={stage.needs} className="adm-inline-error">
                <TriangleAlert aria-hidden />
                {t(
                  {
                    schedule: `admin.route.needs.${stage.needs}`,
                    verify: "admin.route.verifyNeedsImage",
                    translate: "admin.route.translationNeedsText",
                  }[route],
                )}
              </p>
            ))
        : null}
      <div className="adm-queue-foot">
        <ModelPicker
          catalog={catalog}
          targets={targets}
          needs={needs}
          onPick={(target) => save([...targets, target])}
        />
        {targets.length > 1 ? (
          <span className="adm-muted">{t("admin.route.dragHint")}</span>
        ) : null}
      </div>
    </div>
  )
}

const failureRoutes: Record<QueueRoute, string> = {
  schedule: "admin.llm.queue.schedule",
  verify: "admin.llm.queue.verify",
  translate: "admin.llm.failures.translate",
}

const failureSubjects = {
  post: "admin.llm.failures.post",
  image: "admin.llm.failures.image",
  video: "admin.llm.failures.video",
} as const

/**
 * The newest stored model failures of every queue, so a failed call can be
 * read without the database. The list loads with the view and on demand.
 */
export function RecentFailures({
  providers,
}: {
  providers: Provider[] | undefined
}) {
  const { t, locale } = useAppSettings()
  const { api } = useAdmin()
  const failures = useResource(() => api.llmFailures(), [])
  const providerName = (id: string | null) =>
    id ? providers?.find((provider) => provider.id === id)?.name || id : null
  const items = failures.data?.items
  return (
    <Panel
      icon={TriangleAlert}
      title={t("admin.llm.failures.title")}
      description={t("admin.llm.failures.hint")}
      actions={
        <IconButton
          icon={RefreshCw}
          label={t("admin.llm.failures.reload")}
          busy={failures.loading}
          onClick={() => void failures.reload()}
        />
      }
    >
      {!items ? (
        <Loading />
      ) : items.length ? (
        <ul className="adm-failure-list">
          {items.map((failure) => {
            const RouteIcon = routeIcons[failure.route] || Route
            const provider = providerName(failure.providerId)
            const subject = `${t(failureSubjects[failure.subject])} ${failure.subjectId}`
            return (
              <li
                key={`${failure.route}:${failure.subject}:${failure.subjectId}:${failure.at}`}
              >
                <div className="adm-failure-head">
                  <Badge tone="danger" icon={RouteIcon}>
                    {t(failureRoutes[failure.route])}
                  </Badge>
                  <time dateTime={failure.at} title={japanStamp(failure.at)}>
                    {formatRelative(failure.at, locale)}
                  </time>
                </div>
                <p className="adm-error-text">{failure.message}</p>
                <small className="adm-failure-meta">
                  {failure.url ? (
                    <a href={failure.url} target="_blank" rel="noreferrer">
                      {subject}
                      <ExternalLink aria-hidden />
                    </a>
                  ) : (
                    <span>{subject}</span>
                  )}
                  {failure.model ? (
                    <span>
                      {provider ? `${provider} · ` : ""}
                      {failure.model}
                    </span>
                  ) : null}
                </small>
              </li>
            )
          })}
        </ul>
      ) : (
        <Empty icon={Check} title={t("admin.llm.failures.none")}>
          {t("admin.llm.failures.noneHint")}
        </Empty>
      )}
    </Panel>
  )
}
