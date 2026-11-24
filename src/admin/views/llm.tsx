import { useMemo, useRef, useState } from "react"
import { Bot, Languages, Plus, Route, ScanSearch } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { createLatestWins } from "@/admin/latest-wins"
import type { Provider, QueueRoute, RouteTarget } from "@/admin/types"
import { Button, Empty, Loading, Panel } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { SyncSettingsPanel } from "@/admin/views/sync-settings"
import { queueKey, routeOrder } from "@/admin/views/llm-parts"
import { QueueEditor, RecentFailures } from "@/admin/views/llm-routes"
import { ProviderDialog } from "@/admin/views/llm-provider-dialog"
import { ProviderList, ProviderDetail } from "@/admin/views/llm-providers"

/**
 * The model setup on the Detection page: queues, providers, model limits
 * and recent failures. The page tab is the selected provider.
 */
export function ModelSetup({ tab, navigate }: ViewProps) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const catalog = useResource(() => api.llm(), [])
  const [editing, setEditing] = useState<Provider | null | undefined>(undefined)
  const [created, setCreated] = useState("")
  // Queue edits show at once as drafts over the last server-confirmed
  // catalog. Each route sends one save at a time, latest edit first; a
  // refusal drops the draft, which rolls the queue back to what was stored.
  const [drafts, setDrafts] = useState<
    Partial<Record<QueueRoute, RouteTarget[]>>
  >({})
  const latest = useRef({ api, act, setData: catalog.setData })
  latest.current = { api, act, setData: catalog.setData }
  const [sendQueue] = useState(() =>
    createLatestWins<QueueRoute, RouteTarget[]>({
      send: async (route, targets) =>
        (
          await latest.current.act(
            () => latest.current.api.updateQueue(targets, route),
            "admin.notice.queueSaved",
          )
        )?.queue,
      confirmed: (route, queue) =>
        latest.current.setData(
          (current) => current && { ...current, [queueKey(route)]: queue },
        ),
      settled: (route) =>
        setDrafts((current) => {
          const next = { ...current }
          delete next[route]
          return next
        }),
    }),
  )
  const saveQueue = (route: QueueRoute, targets: RouteTarget[]) => {
    setDrafts((current) => ({ ...current, [route]: targets }))
    return sendQueue(route, targets)
  }
  const data = useMemo(
    () =>
      catalog.data && {
        ...catalog.data,
        ...Object.fromEntries(
          routeOrder.flatMap((route) => {
            const draft = drafts[route]
            return draft ? [[queueKey(route), draft]] : []
          }),
        ),
      },
    [catalog.data, drafts],
  )
  const selected =
    data?.providers.find((provider) => provider.id === tab) ||
    data?.providers[0]
  return (
    <>
      <Panel icon={Route} title={t("admin.llm.queues")}>
        {!data ? (
          <Loading />
        ) : (
          <div className="adm-queues">
            <section>
              <h3>
                <Route aria-hidden />
                {t("admin.llm.queue.schedule")}
              </h3>
              <QueueEditor catalog={data} route="schedule" onSave={saveQueue} />
            </section>
            <section>
              <h3>
                <ScanSearch aria-hidden />
                {t("admin.llm.queue.verify")}
              </h3>
              <QueueEditor catalog={data} route="verify" onSave={saveQueue} />
            </section>
            <section>
              <h3>
                <Languages aria-hidden />
                {t("admin.models.translation")}
              </h3>
              <QueueEditor
                catalog={data}
                route="translate"
                onSave={saveQueue}
              />
            </section>
          </div>
        )}
      </Panel>
      <Panel
        icon={Bot}
        title={t("admin.models.providers")}
        actions={
          <Button
            size="sm"
            icon={Plus}
            disabled={!data}
            onClick={() => setEditing(null)}
          >
            {t("admin.provider.create")}
          </Button>
        }
      >
        {!data ? (
          <Loading />
        ) : selected ? (
          <div className="adm-providers">
            <ProviderList
              catalog={data}
              selected={selected.id}
              onSelect={(id) => navigate("detection", id)}
            />
            <ProviderDetail
              key={selected.id}
              provider={selected}
              catalog={data}
              discoverNow={created === selected.id}
              onEdit={() => setEditing(selected)}
              onChanged={() => void catalog.reload()}
              onDeleted={() => {
                navigate("detection")
                void catalog.reload()
              }}
              onQueue={saveQueue}
            />
          </div>
        ) : (
          <Empty icon={Bot} title={t("admin.models.noProviders")}>
            {t("admin.models.noProvidersHint")}
          </Empty>
        )}
      </Panel>
      <SyncSettingsPanel group="llm" level={2} />
      <RecentFailures providers={data?.providers} />
      {editing !== undefined && data ? (
        <ProviderDialog
          provider={editing}
          catalog={data}
          onClose={() => setEditing(undefined)}
          onSaved={(provider, isNew) => {
            setEditing(undefined)
            if (isNew) {
              setCreated(provider.id)
              navigate("detection", provider.id)
            }
            void catalog.reload()
          }}
        />
      ) : null}
    </>
  )
}
