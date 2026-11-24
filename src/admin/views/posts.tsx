import { useEffect, useRef, useState } from "react"
import { Download, FileText, Search } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import type { Sources } from "@/admin/types"
import {
  Button,
  DateRange,
  Empty,
  FilterReset,
  FilterSelect,
  Loading,
  PageHeader,
  Pager,
  Panel,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { PlatformTabs, useScopedPage } from "@/admin/views/platform-tabs"
import { PostItem } from "@/admin/views/post-item"
import {
  refreshable,
  type BulkAction,
  PostSelectionBar,
} from "@/admin/views/post-parts"
import { TranslationBacklog } from "@/admin/views/translation-parts"

const blankFilters = {
  search: "",
  account: "",
  llm: "",
  translation: "",
  queued: "",
  from: "",
  to: "",
}

/** Open operator requests the list can keep: any, or one kind. */
const queuedKinds = ["any", "extraction", "refresh", "translation"]

/** A `#posts/queued-<kind>` link opens the list on that kind of request. */
function queuedPreset(tab: string) {
  const kind = tab.replace(/^queued-/u, "")
  return tab.startsWith("queued-") && queuedKinds.includes(kind) ? kind : ""
}

const postPlatforms = ["all", "x", "bilibili"] as const
type PostPlatform = (typeof postPlatforms)[number]

/**
 * `#posts/<platform>/<preset>`: the platform may be left out, and the preset
 * (`queued-<kind>` or `find-<id>`) is what the list opens on.
 */
function readPostTab(tab: string) {
  const [first = "", ...rest] = tab.split("/")
  return first === "x" || first === "bilibili"
    ? { platform: first as PostPlatform, preset: rest.join("/") }
    : { platform: "all" as PostPlatform, preset: tab }
}

const llmStatuses = [
  "never",
  "pending",
  "processed",
  "uncertain",
  "failed",
  "dismissed",
  "skipped",
]
const translationStatuses = ["incomplete", "complete", "requested", "failed"]

function PostList({
  platform,
  sources,
  queued,
  find,
  navigate,
}: {
  platform: PostPlatform
  sources: Sources | undefined
  queued: string
  /** A status ID the list opens searched for, from External posts. */
  find: string
  navigate: ViewProps["navigate"]
}) {
  const { t } = useAppSettings()
  const { api, act, notify } = useAdmin()
  const [filters, setFilters] = useState({
    ...blankFilters,
    queued,
    search: find,
  })
  const [selection, setSelection] = useState<ReadonlySet<string>>(new Set())
  const [busy, setBusy] = useState<BulkAction | "">("")
  // The last toggled post, where a shift-click range starts.
  const anchor = useRef<string | null>(null)
  const [search, setSearch] = useState(find)
  const [page, setPage] = useScopedPage(platform)
  // Bilibili has one account, so its tab has no account filter.
  const accounts = platform !== "bilibili"
  const applied = accounts ? filters : { ...filters, account: "" }
  const posts = useResource(
    () =>
      api.posts({
        ...applied,
        source: platform === "all" ? "" : platform,
        page,
        pageSize: 30,
      }),
    [filters, platform, page],
  )
  const update = (patch: Partial<typeof filters>) => {
    setFilters((current) => ({ ...current, ...patch }))
    setPage(1)
  }
  // Own X posts can be refreshed from the source together. A new filter or
  // page starts with nothing selected; a reload keeps only the selected
  // posts that are still listed.
  const selectable = (posts.data?.items ?? []).filter(refreshable)
  useEffect(() => {
    setSelection(new Set())
    anchor.current = null
  }, [filters, platform, page])
  useEffect(() => {
    setSelection((current) => {
      const listed = new Set(
        posts.data?.items.filter(refreshable).map((post) => post.id),
      )
      const kept = [...current].filter((id) => listed.has(id))
      return kept.length === current.size ? current : new Set(kept)
    })
  }, [posts.data])
  const toggle = (id: string, range: boolean) => {
    const ids = selectable.map((post) => post.id)
    const on = !selection.has(id)
    const from = range && anchor.current ? ids.indexOf(anchor.current) : -1
    const to = ids.indexOf(id)
    const span =
      from < 0 ? [id] : ids.slice(Math.min(from, to), Math.max(from, to) + 1)
    setSelection((current) => {
      const next = new Set(current)
      for (const each of span) {
        if (on) next.add(each)
        else next.delete(each)
      }
      return next
    })
    anchor.current = id
  }
  const refreshSelected = async () => {
    const ids = selectable
      .filter((post) => selection.has(post.id))
      .map((post) => post.id)
    if (!ids.length) return
    setBusy("refresh")
    const count = await act(async () => (await api.refreshPosts(ids)).queued)
    setBusy("")
    if (count === undefined) return
    notify("admin.notice.refreshesQueued", { count })
    setSelection(new Set())
    anchor.current = null
    await posts.reload()
  }
  const active = (
    Object.keys(blankFilters) as (keyof typeof blankFilters)[]
  ).filter((key) => applied[key] !== blankFilters[key]).length
  const any = { value: "", label: t("admin.filter.any") }
  return (
    <>
      <form
        className="adm-filters"
        onSubmit={(event) => {
          event.preventDefault()
          update({ search })
        }}
      >
        <label className="adm-search">
          <Search aria-hidden />
          <input
            type="search"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onBlur={() => search !== filters.search && update({ search })}
            placeholder={t("admin.posts.search")}
            aria-label={t("admin.posts.search")}
          />
        </label>
        {accounts ? (
          <FilterSelect
            label={t("admin.posts.account")}
            value={filters.account}
            onChange={(account) => update({ account })}
            options={[
              any,
              ...(sources?.x.accounts || []).map(({ account }) => ({
                value: account,
                label: `@${account}`,
              })),
            ]}
          />
        ) : null}
        <FilterSelect
          label={t("admin.posts.llm")}
          value={filters.llm}
          onChange={(llm) => update({ llm })}
          options={[
            any,
            ...llmStatuses.map((status) => ({
              value: status,
              label: t(`admin.status.${status}`),
            })),
          ]}
        />
        <FilterSelect
          label={t("admin.translation.filter")}
          value={filters.translation}
          onChange={(translation) => update({ translation })}
          options={[
            any,
            ...translationStatuses.map((status) => ({
              value: status,
              label: t(`admin.translation.filter.${status}`),
            })),
          ]}
        />
        <FilterSelect
          label={t("admin.posts.queue")}
          value={filters.queued}
          onChange={(queued) => update({ queued })}
          options={[
            any,
            ...queuedKinds.map((kind) => ({
              value: kind,
              label: t(`admin.posts.queue.${kind}`),
            })),
          ]}
        />
        <DateRange
          from={filters.from}
          to={filters.to}
          onChange={(range) => update(range)}
        />
        <FilterReset
          count={active}
          onReset={() => {
            setSearch("")
            update(blankFilters)
          }}
        />
      </form>
      <div className="adm-list-bar">
        <span className="adm-muted">
          {posts.data
            ? t("admin.pager.total", { total: posts.data.total })
            : ""}
        </span>
        <TranslationBacklog
          owner="post"
          source={platform === "all" ? undefined : platform}
          onQueued={() => void posts.reload()}
        />
      </div>
      {!posts.data ? (
        <Loading />
      ) : posts.data.items.length ? (
        <>
          {selectable.length ? (
            <PostSelectionBar
              actions={["refresh"]}
              selected={selection.size}
              pageCount={selectable.length}
              busy={busy}
              onSelectPage={(all) =>
                setSelection(
                  all ? new Set(selectable.map((post) => post.id)) : new Set(),
                )
              }
              onClear={() => {
                setSelection(new Set())
                anchor.current = null
              }}
              onAction={() => void refreshSelected()}
            />
          ) : null}
          <ul className="adm-post-feed" aria-label={t("admin.posts.title")}>
            {posts.data.items.map((post) => (
              <PostItem
                key={post.id}
                post={post}
                onDone={() => void posts.reload()}
                navigate={navigate}
                selected={selection.has(post.id)}
                onSelect={
                  refreshable(post)
                    ? (range) => toggle(post.id, range)
                    : undefined
                }
              />
            ))}
          </ul>
          <Pager
            page={posts.data.page}
            totalPages={posts.data.totalPages}
            total={posts.data.total}
            onPage={setPage}
          />
        </>
      ) : (
        <Empty icon={FileText} title={t("admin.posts.empty")} />
      )}
    </>
  )
}

export function PostsView({ tab, navigate }: ViewProps) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  // The account filter lists the configured X accounts.
  const sources = useResource(() => api.sources(), [])
  const { platform, preset } = readPostTab(tab)
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.posts.title")}
        actions={
          <Button
            size="sm"
            variant="ghost"
            icon={Download}
            onClick={() => navigate("sources")}
          >
            {t("admin.fetch.openSources")}
          </Button>
        }
      />
      <PlatformTabs
        platforms={postPlatforms}
        value={platform}
        onSelect={(next) =>
          navigate(
            "posts",
            [next, preset].filter((part) => part && part !== "all").join("/"),
          )
        }
      >
        <Panel>
          <PostList
            key={preset}
            platform={platform}
            sources={sources.data}
            queued={queuedPreset(preset)}
            find={preset.startsWith("find-") ? preset.slice(5) : ""}
            navigate={navigate}
          />
        </Panel>
      </PlatformTabs>
    </div>
  )
}
