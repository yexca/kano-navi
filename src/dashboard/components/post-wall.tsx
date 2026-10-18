import type { Locale, MediaItem, Post, Translator } from "@/types"
import { useMemo, useState } from "react"
import {
  ArrowUpRight,
  Heart,
  ImageOff,
  MessageCircle,
  Repeat2,
} from "lucide-react"

import { EmptyNote, Placeholder, SectionTitle } from "@/site/site-shell"
import { ExternalLink, XLogo } from "@/site/links"
import { formatCompact, formatRelative, formatStamp } from "../format"

/** Ready local media can open in the viewer; anything else stays labelled. */
export function postMedia(post: Post): MediaItem[] {
  if (Array.isArray(post.media) && post.media.length) return post.media
  return post.mediaUrl
    ? [
        {
          id: null,
          url: post.mediaUrl,
          status: post.mediaStatus || "ready",
          sourceUrl: null,
        },
      ]
    : []
}

function Engagement({
  post,
  locale,
  t,
}: {
  post: Post
  locale: Locale
  t: Translator
}) {
  const items = [
    { icon: MessageCircle, value: post.replies, label: t("feed.replies") },
    { icon: Repeat2, value: post.reposts, label: t("feed.reposts") },
    { icon: Heart, value: post.likes, label: t("feed.likes") },
  ].filter((item) => Number(item.value) > 0)
  if (!items.length) return null
  return (
    <p className="post-engagement">
      {items.map(({ icon: Icon, value, label }) => (
        <span key={label} title={label}>
          <Icon aria-hidden="true" />
          <span className="sr-only">{label}</span>
          {formatCompact(value, locale)}
        </span>
      ))}
    </p>
  )
}

function PostNote({
  post,
  avatarUrl,
  locale,
  now,
  t,
  onOpenMedia,
}: {
  post: Post
  avatarUrl: string | null
  locale: Locale
  now: number
  t: Translator
  onOpenMedia: (media: MediaItem[], index: number) => void
}) {
  const media = postMedia(post)
  const ready = media.filter((item) => item.url)
  return (
    <article className="post-note">
      <header className="post-note-head">
        <img src={avatarUrl || undefined} alt="" />
        {post.accountHandle ? (
          <ExternalLink
            className="post-note-handle"
            href={post.accountUrl || `https://x.com/${post.accountHandle}`}
          >
            @{post.accountHandle}
          </ExternalLink>
        ) : (
          <span className="post-note-handle">X</span>
        )}
        <time
          dateTime={post.publishedAt}
          title={`${formatStamp(post.publishedAt)} ${t("common.timezoneShort")}`}
        >
          {formatRelative(post.publishedAt, locale, now)}
        </time>
        <ExternalLink
          className="post-note-open"
          href={post.url}
          aria-label={t("feed.originalPost")}
          title={t("feed.originalPost")}
        >
          <ArrowUpRight aria-hidden="true" />
        </ExternalLink>
      </header>
      <p className="post-note-text">{post.text}</p>
      {media.length ? (
        <div className={`post-note-media count-${Math.min(4, media.length)}`}>
          {media.slice(0, 4).map((item, index) =>
            item.url ? (
              <button
                type="button"
                key={item.id || item.url}
                onClick={() => onOpenMedia(ready, ready.indexOf(item))}
                aria-label={`${t("feed.openMedia")} ${index + 1}`}
              >
                <img src={item.url} alt={item.alt || ""} loading="lazy" />
              </button>
            ) : (
              <span key={item.id || index} className="post-note-missing">
                <ImageOff aria-hidden="true" />
                <span>{t("media.unavailable")}</span>
              </span>
            ),
          )}
        </div>
      ) : null}
      <Engagement post={post} locale={locale} t={t} />
    </article>
  )
}

export function PostWall({
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
}: {
  posts: Post[]
  latestPost: Post | null
  accounts: string[]
  avatarUrl: string | null
  windowDays: number
  isLoading: boolean
  locale: Locale
  now: number
  t: Translator
  onOpenMedia: (media: MediaItem[], index: number) => void
}) {
  const [account, setAccount] = useState<string | null>(null)
  const visible = useMemo(
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
  const countFor = (handle: string) =>
    posts.filter(
      (post) =>
        String(post.accountHandle || "").toLowerCase() === handle.toLowerCase(),
    ).length

  return (
    <section className="section posts" id="feed" aria-labelledby="posts-title">
      <SectionTitle
        id="posts-title"
        kicker={
          <>
            <XLogo className="kicker-icon" aria-hidden="true" />{" "}
            {accounts.map((handle) => `@${handle}`).join(" · ")}
          </>
        }
        title={t("feed.title")}
        note={t("feed.subtitle", { days: windowDays })}
      >
        {accounts.length > 1 ? (
          <div className="segmented" role="group" aria-label={t("feed.filter")}>
            <button
              type="button"
              aria-pressed={!account}
              onClick={() => setAccount(null)}
            >
              {t("common.all")}
              <small>{posts.length}</small>
            </button>
            {accounts.map((handle) => (
              <button
                type="button"
                key={handle}
                aria-pressed={account === handle}
                onClick={() => setAccount(handle)}
              >
                @{handle}
                <small>{countFor(handle)}</small>
              </button>
            ))}
          </div>
        ) : null}
      </SectionTitle>

      {isLoading ? (
        <Placeholder lines={5} />
      ) : visible.length ? (
        <div className="post-wall">
          {visible.map((post) => (
            <PostNote
              key={post.id}
              post={post}
              avatarUrl={avatarUrl}
              locale={locale}
              now={now}
              t={t}
              onOpenMedia={onOpenMedia}
            />
          ))}
        </div>
      ) : (
        <div className="post-empty">
          <EmptyNote title={t("feed.empty", { days: windowDays })} />
          {latestPost && !account ? (
            <div className="post-last">
              <p className="kicker">{t("feed.lastSeen")}</p>
              <PostNote
                post={latestPost}
                avatarUrl={avatarUrl}
                locale={locale}
                now={now}
                t={t}
                onOpenMedia={onOpenMedia}
              />
            </div>
          ) : null}
        </div>
      )}
    </section>
  )
}
