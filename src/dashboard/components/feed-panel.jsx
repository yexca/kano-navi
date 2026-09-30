import { useMemo, useState } from "react"
import { ArrowUpRight, Heart, MessageCircle, Repeat2 } from "lucide-react"

import { formatCompact, formatRelative, formatStamp } from "../format"
import {
  EmptyState,
  ExternalLink,
  SectionHeading,
  Skeleton,
  XLogo,
} from "./primitives"

function postMedia(post) {
  if (Array.isArray(post.media)) return post.media.filter((item) => item?.url)
  return post.mediaUrl ? [{ url: post.mediaUrl, alt: post.mediaAlt }] : []
}

function Engagement({ post, locale, t }) {
  const items = [
    { icon: MessageCircle, value: post.replies, label: t("feed.replies") },
    { icon: Repeat2, value: post.reposts, label: t("feed.reposts") },
    { icon: Heart, value: post.likes, label: t("feed.likes") },
  ].filter((item) => Number(item.value) > 0)
  if (!items.length) return null
  return (
    <span className="post-engagement">
      {items.map(({ icon: Icon, value, label }) => (
        <span key={label} title={label}>
          <Icon aria-hidden="true" />
          <span className="sr-only">{label}</span>
          {formatCompact(value, locale)}
        </span>
      ))}
    </span>
  )
}

export function PostCard({ post, avatarUrl, locale, now, t, onOpenMedia }) {
  const media = postMedia(post)
  return (
    <article className="post">
      <img className="post-avatar" src={avatarUrl} alt="" />
      <div className="post-main">
        <header className="post-header">
          {post.accountHandle ? (
            <ExternalLink
              className="post-handle"
              href={post.accountUrl || `https://x.com/${post.accountHandle}`}
            >
              @{post.accountHandle}
            </ExternalLink>
          ) : (
            <span className="post-handle">X</span>
          )}
          <time
            dateTime={post.publishedAt}
            title={`${formatStamp(post.publishedAt)} JST`}
          >
            {formatRelative(post.publishedAt, locale, now)}
          </time>
          <ExternalLink
            className="post-open"
            href={post.url}
            aria-label={t("feed.originalPost")}
            title={t("feed.originalPost")}
          >
            <ArrowUpRight aria-hidden="true" />
          </ExternalLink>
        </header>
        <p className="post-text">{post.text}</p>
        {media.length ? (
          <div className={`post-media count-${Math.min(4, media.length)}`}>
            {media.slice(0, 4).map((item, index) => (
              <button
                type="button"
                key={item.id || item.url}
                onClick={() => onOpenMedia(media, index)}
                aria-label={`${t("feed.openMedia")} ${index + 1}`}
              >
                <img src={item.url} alt={item.alt || ""} loading="lazy" />
              </button>
            ))}
          </div>
        ) : null}
        <Engagement post={post} locale={locale} t={t} />
      </div>
    </article>
  )
}

export function FeedPanel({
  posts,
  latestPost,
  accounts,
  avatarUrl,
  windowDays,
  isLoading,
  locale,
  now,
  t,
  onOpenMedia,
}) {
  const [account, setAccount] = useState(null)
  const visiblePosts = useMemo(
    () =>
      account
        ? posts.filter(
            (post) =>
              String(post.accountHandle || "").toLowerCase() ===
              account.toLowerCase(),
          )
        : posts,
    [account, posts],
  )

  return (
    <section
      className="panel feed-panel"
      id="feed"
      aria-labelledby="feed-title"
    >
      <SectionHeading
        id="feed-title"
        eyebrow={<XLogo className="eyebrow-icon" aria-hidden="true" />}
        title={t("feed.title")}
        subtitle={t("feed.subtitle", { days: windowDays })}
      />

      {accounts.length > 1 ? (
        <div className="segmented" role="group" aria-label={t("feed.filter")}>
          <button
            type="button"
            aria-pressed={!account}
            onClick={() => setAccount(null)}
          >
            {t("common.all")}
          </button>
          {accounts.map((handle) => (
            <button
              type="button"
              key={handle}
              aria-pressed={account === handle}
              onClick={() => setAccount(handle)}
            >
              @{handle}
            </button>
          ))}
        </div>
      ) : null}

      <div className="feed-scroll">
        {isLoading ? (
          <Skeleton lines={5} />
        ) : visiblePosts.length ? (
          visiblePosts.map((post) => (
            <PostCard
              key={post.id}
              post={post}
              avatarUrl={avatarUrl}
              locale={locale}
              now={now}
              t={t}
              onOpenMedia={onOpenMedia}
            />
          ))
        ) : (
          <>
            <EmptyState title={t("feed.empty", { days: windowDays })} />
            {latestPost && !account ? (
              <div className="feed-last-seen">
                <p className="eyebrow">{t("feed.lastSeen")}</p>
                <PostCard
                  post={latestPost}
                  avatarUrl={avatarUrl}
                  locale={locale}
                  now={now}
                  t={t}
                  onOpenMedia={onOpenMedia}
                />
              </div>
            ) : null}
          </>
        )}
      </div>
    </section>
  )
}
