import { useMemo, useState } from "react"
import { ExternalLink, FileText, RefreshCw, Search } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { SourceFetchCard } from "@/admin/components/source-fetch-card"
import {
  Btn,
  Card,
  EmptyState,
  PageHeader,
  Tag,
} from "@/admin/components/primitives"

const statusTone = {
  success: "leaf",
  uncertain: "honey",
  failed: "danger",
  running: "sky",
  queued: "lavender",
  skipped: "neutral",
  never: "neutral",
}

export function PostsView({ data }) {
  const { t } = useAppSettings()
  const {
    postLlm = [],
    reprocessPostLlm,
    startJob,
    activeJob,
    fail,
    notify,
  } = data
  const [query, setQuery] = useState("")
  const [status, setStatus] = useState("")
  const [busyId, setBusyId] = useState(null)
  const filteredPosts = useMemo(() => {
    const normalized = query.trim().toLowerCase()
    return postLlm.filter((post) => {
      if (status && post.llmStatus !== status) return false
      if (!normalized) return true
      return `${post.text || ""} ${post.url || ""} ${post.id}`
        .toLowerCase()
        .includes(normalized)
    })
  }, [postLlm, query, status])

  const reprocess = async (post) => {
    setBusyId(post.id)
    try {
      await reprocessPostLlm(post.id, post.llmRoute)
      // startJob already reports a started scan; when one is running, the
      // request waits for the next scan instead.
      const job = await startJob("/scan/automatic")
      if (!job) notify({ key: "admin.notice.postQueued" })
    } catch (error) {
      fail(error)
    } finally {
      setBusyId(null)
    }
  }

  const statusOptions = [
    "never",
    "queued",
    "running",
    "success",
    "uncertain",
    "failed",
    "skipped",
  ]

  return (
    <section className="adm-view" aria-labelledby="adm-posts-title">
      <PageHeader
        id="adm-posts-title"
        title={t("admin.posts.title")}
        description={t("admin.posts.description")}
        actions={
          <Btn
            variant="outline"
            icon={RefreshCw}
            disabled={Boolean(activeJob)}
            onClick={() => startJob("/scan/automatic")}
          >
            {t("admin.posts.runScan")}
          </Btn>
        }
      />
      <Card className="adm-filter-card">
        <div className="adm-filters">
          <label className="adm-search">
            <Search aria-hidden="true" />
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t("admin.posts.searchPlaceholder")}
              aria-label={t("admin.posts.search")}
            />
          </label>
          <select
            value={status}
            onChange={(event) => setStatus(event.target.value)}
            aria-label={t("admin.posts.statusFilter")}
          >
            <option value="">{t("admin.posts.allStatuses")}</option>
            {statusOptions.map((value) => (
              <option key={value} value={value}>
                {t(`admin.posts.status.${value}`)}
              </option>
            ))}
          </select>
        </div>
      </Card>
      <SourceFetchCard source="x" data={data} />
      <Card className="adm-posts-card">
        <div className="adm-table-meta">
          <span>{t("admin.posts.total", { count: filteredPosts.length })}</span>
        </div>
        {filteredPosts.length ? (
          <ul className="adm-post-list">
            {filteredPosts.map((post) => (
              <li key={post.id} className="adm-post-row">
                <div className="adm-post-copy">
                  <p
                    className="adm-post-text"
                    title={post.text || post.url || post.id}
                  >
                    {post.text || post.url || post.id}
                  </p>
                  <small>
                    {post.publishedAt || "—"} · {post.llmRoute || "—"}
                    {post.llmLastError ? ` · ${post.llmLastError}` : ""}
                  </small>
                  {post.url ? (
                    <a
                      href={post.url}
                      target="_blank"
                      rel="noreferrer noopener"
                      className="adm-post-link"
                    >
                      {t("admin.posts.openSource")}
                      <ExternalLink aria-hidden="true" />
                    </a>
                  ) : null}
                </div>
                <div className="adm-post-actions">
                  <Tag tone={statusTone[post.llmStatus] || "neutral"}>
                    {t(`admin.posts.status.${post.llmStatus}`)}
                  </Tag>
                  {post.llmReprocessRequested && post.llmStatus !== "queued" ? (
                    <Tag tone={statusTone.queued}>
                      {t("admin.posts.status.queued")}
                    </Tag>
                  ) : null}
                  <Btn
                    variant="outline"
                    size="sm"
                    icon={RefreshCw}
                    busy={busyId === post.id}
                    disabled={
                      busyId != null ||
                      Boolean(activeJob) ||
                      post.llmStatus === "running"
                    }
                    onClick={() => reprocess(post)}
                  >
                    {t("admin.posts.reprocess")}
                  </Btn>
                </div>
              </li>
            ))}
          </ul>
        ) : (
          <EmptyState icon={FileText} title={t("admin.posts.empty")} />
        )}
      </Card>
    </section>
  )
}
