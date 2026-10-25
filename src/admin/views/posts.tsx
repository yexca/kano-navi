import { useState } from "react"
import { FileText, ImageIcon, Search } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatDateTime } from "@/admin/format"
import type { AdminPost } from "@/admin/types"
import { Badge, Empty, Loading, PageHeader, Pager, Panel } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { QuickFetch } from "@/admin/views/fetch-parts"
import { PostActions, PostStatus } from "@/admin/views/review"

function PostTableRow({
  post,
  onDone,
}: {
  post: AdminPost
  onDone: () => void
}) {
  const { t, locale } = useAppSettings()
  return (
    <div role="row" className="adm-post-table-row">
      <div role="cell" className="adm-post-origin">
        <strong>{post.accountHandle || t(`admin.step.${post.source}`)}</strong>
        <time dateTime={post.publishedAt}>
          {formatDateTime(post.publishedAt, locale)}
        </time>
      </div>
      <div role="cell" className="adm-post-body">
        <p className="adm-post-text">{post.text}</p>
        {post.mediaUrl ? (
          <Badge icon={ImageIcon}>{t("admin.posts.withMedia")}</Badge>
        ) : null}
      </div>
      <div role="cell" className="adm-chip-row adm-post-state">
        <PostStatus post={post} />
      </div>
      <div role="cell" className="adm-post-controls">
        <PostActions post={post} onDone={onDone} compact />
      </div>
    </div>
  )
}

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
          <div
            className="adm-table adm-post-table"
            role="table"
            aria-label={t("admin.posts.title")}
          >
            <div role="row" className="adm-post-table-row adm-table-head">
              <span role="columnheader">{t("admin.posts.source")}</span>
              <span role="columnheader">{t("admin.posts.column.text")}</span>
              <span role="columnheader">{t("admin.posts.llm")}</span>
              <span role="columnheader">{t("admin.posts.column.actions")}</span>
            </div>
            {posts.data.items.map((post) => (
              <PostTableRow
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
