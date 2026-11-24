import { useState } from "react"
import { ExternalLink, EyeOff, FileSearch } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { japanStamp } from "@/admin/format"
import type { AdminPost, EventEvidence, EvidenceRole } from "@/admin/types"
import { Badge, Empty, Loading } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { PostImages, PostQuote } from "@/admin/views/post-parts"

type EvidencePost = AdminPost & { roles: EvidenceRole[] }

/** The host a link points at, or the link itself when it is not a URL. */
function linkHost(value: string) {
  try {
    return new URL(value).hostname
  } catch {
    return value
  }
}

const roleTone = {
  source: "leaf",
  merged: "sky",
  cancellation: "honey",
} as const

/** One post of the timeline; evidence posts carry their roles and stand out,
 * nearby posts are quieter context. */
function EvidenceItem({
  post,
  onDone,
}: {
  post: EvidencePost
  onDone: () => void
}) {
  const { t } = useAppSettings()
  const evidence = post.roles.length > 0
  return (
    <li
      className={`adm-evidence-post${evidence ? " is-evidence" : ""}${post.hiddenAt ? " is-hidden" : ""}`}
    >
      <header>
        <strong>
          {post.source === "x" && post.accountHandle
            ? `@${post.accountHandle.replace(/^@/, "")}`
            : t(`admin.step.${post.source}`)}
        </strong>
        <time dateTime={post.publishedAt}>{japanStamp(post.publishedAt)}</time>
        <span className="adm-chip-row">
          {post.roles.map((role) => (
            <Badge key={role} tone={roleTone[role]}>
              {t(`admin.evidence.role.${role}`)}
            </Badge>
          ))}
          {post.hiddenAt ? (
            <Badge tone="warning" icon={EyeOff}>
              {t("admin.posts.hidden")}
            </Badge>
          ) : null}
        </span>
        <a
          className="adm-icon-button"
          href={post.url}
          target="_blank"
          rel="noreferrer"
          aria-label={t("admin.evidence.open")}
          title={t("admin.evidence.open")}
        >
          <ExternalLink aria-hidden />
        </a>
      </header>
      <p className="adm-evidence-text">{post.text}</p>
      <PostQuote post={post} onDone={onDone} />
      <PostImages post={post} limit={4} />
    </li>
  )
}

/**
 * What an entry was read from, as one timeline: the posts it was extracted
 * from, those of the entries merged into it and the post that suggested its
 * cancellation, between the same accounts' posts around them. An entry
 * without a stored post shows its own accounts' posts in the days before it.
 * Nearby posts can be folded away to leave only the evidence.
 */
export function EventEvidencePanel({ eventId }: { eventId: string }) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const evidence = useResource<EventEvidence>(
    () => api.eventEvidence(eventId),
    [eventId],
  )
  const [context, setContext] = useState(true)
  const data = evidence.data
  if (!data) return evidence.error ? null : <Loading />
  const timeline: EvidencePost[] = [
    ...data.posts,
    ...(context ? data.nearby.map((post) => ({ ...post, roles: [] })) : []),
  ].sort(
    (a, b) =>
      Date.parse(a.publishedAt) - Date.parse(b.publishedAt) ||
      a.id.localeCompare(b.id),
  )
  const reload = () => void evidence.reload()
  return (
    <section className="adm-evidence" aria-label={t("admin.evidence.title")}>
      <header className="adm-evidence-head">
        <h3>
          <FileSearch aria-hidden />
          {t("admin.evidence.title")}
          <em>{data.posts.length}</em>
        </h3>
        {data.nearby.length ? (
          <label className="adm-evidence-toggle">
            <input
              type="checkbox"
              checked={context}
              onChange={(event) => setContext(event.target.checked)}
            />
            {t("admin.evidence.nearby", { count: data.nearby.length })}
          </label>
        ) : null}
      </header>
      {data.posts.length === 0 ? (
        <p className="adm-muted">
          {data.nearby.length
            ? t("admin.evidence.noPostsLeadUp")
            : t("admin.evidence.noPosts")}
        </p>
      ) : null}
      {data.links.length ? (
        <ul className="adm-evidence-links">
          {data.links.map((link) => (
            <li key={link.url}>
              <Badge tone={roleTone[link.role]}>
                {t(`admin.evidence.role.${link.role}`)}
              </Badge>
              <a
                className="adm-link"
                href={link.url}
                target="_blank"
                rel="noreferrer"
              >
                {linkHost(link.url)}
                <ExternalLink aria-hidden />
              </a>
            </li>
          ))}
        </ul>
      ) : null}
      {timeline.length ? (
        <ol className="adm-evidence-timeline">
          {timeline.map((post) => (
            <EvidenceItem key={post.id} post={post} onDone={reload} />
          ))}
        </ol>
      ) : data.links.length ? null : (
        <Empty icon={FileSearch} title={t("admin.evidence.empty")} />
      )}
    </section>
  )
}
