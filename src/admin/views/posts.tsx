import { useState } from "react"
import { FileText, Search } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { Empty, Loading, PageHeader, Pager, Panel } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { QuickFetch } from "@/admin/views/fetch-parts"
import { PostRow } from "@/admin/views/review"

function PostList() {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const [filters, setFilters] = useState({
    search: "",
    source: "",
    llm: "",
    media: "",
  })
  const [search, setSearch] = useState("")
  const [page, setPage] = useState(1)
  const posts = useResource(
    () => api.posts({ ...filters, page, pageSize: 30 }),
    [filters, page],
  )
  const update = (patch: Partial<typeof filters>) => {
    setFilters((current) => ({ ...current, ...patch }))
    setPage(1)
  }
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
            placeholder={t("admin.posts.search")}
            aria-label={t("admin.posts.search")}
          />
        </label>
        <select
          value={filters.source}
          onChange={(e) => update({ source: e.target.value })}
          aria-label={t("admin.posts.source")}
        >
          <option value="">{t("admin.posts.anySource")}</option>
          <option value="x">{t("admin.step.x")}</option>
          <option value="youtube">{t("admin.step.youtube")}</option>
        </select>
        <select
          value={filters.llm}
          onChange={(e) => update({ llm: e.target.value })}
          aria-label={t("admin.posts.llm")}
        >
          <option value="">{t("admin.posts.anyLlm")}</option>
          {[
            "never",
            "pending",
            "processed",
            "uncertain",
            "failed",
            "skipped",
          ].map((status) => (
            <option key={status} value={status}>
              {t(`admin.status.${status}`)}
            </option>
          ))}
        </select>
        <select
          value={filters.media}
          onChange={(e) => update({ media: e.target.value })}
          aria-label={t("admin.posts.media")}
        >
          <option value="">{t("admin.posts.anyMedia")}</option>
          <option value="present">{t("admin.posts.withMedia")}</option>
          <option value="missing">{t("admin.posts.withoutMedia")}</option>
        </select>
      </form>
      {!posts.data ? (
        <Loading />
      ) : posts.data.items.length ? (
        <>
          <div className="adm-post-list">
            {posts.data.items.map((post) => (
              <PostRow
                key={post.id}
                post={post}
                onDone={() => void posts.reload()}
              />
            ))}
          </div>
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

export function PostsView({ navigate }: ViewProps) {
  const { t } = useAppSettings()
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.posts.title")}
        description={t("admin.posts.description")}
      />
      <QuickFetch source="x" navigate={navigate} />
      <Panel>
        <PostList />
      </Panel>
    </div>
  )
}
