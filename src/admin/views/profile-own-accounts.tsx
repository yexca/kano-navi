import { useState } from "react"
import { Check, Download, ImageOff, Search, UserRound } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type {
  AccountAvatarCandidate,
  AccountAvatarItem,
  AccountAvatarsState,
  ProfileMedia,
  ProfileMediaState,
} from "@/admin/types"
import { Badge, Button, StatusBadge } from "@/admin/ui"
import { useAdmin } from "@/admin/use-admin-data"
import { useMeta, Viewer } from "@/admin/views/profile-parts"

export function pickAccountCandidate(account: AccountAvatarItem): {
  ready: AccountAvatarCandidate | null
  pending: AccountAvatarCandidate | null
} {
  const active = account.activeCandidateId
    ? account.candidates.find((c) => c.id === account.activeCandidateId)
    : undefined
  const ready =
    (active?.status === "ready" ? active : undefined) ??
    account.candidates.find((c) => c.status === "ready") ??
    null
  const newest = account.candidates[0] ?? null
  const pending =
    newest && newest.status !== "ready" && newest.id !== ready?.id
      ? newest
      : null
  return { ready, pending }
}

export function isOwnAccountActiveSiteAvatar(
  account: Pick<AccountAvatarItem, "accountHandle">,
  ready: AccountAvatarCandidate | null,
  activeSiteAvatar: ProfileMedia | null,
): boolean {
  if (!activeSiteAvatar || !ready) return false
  const expectedRef = `account-avatar:${account.accountHandle.toLowerCase()}`
  const activeRef = activeSiteAvatar.sourceRef?.toLowerCase() ?? null
  if (activeRef?.startsWith("account-avatar:") && activeRef !== expectedRef) {
    return false
  }
  const activeSha = activeSiteAvatar.activeSha256 ?? activeSiteAvatar.sha256
  if (ready.sha256 && activeSha) {
    return ready.sha256 === activeSha
  }
  return Boolean(
    ready.sourceUrl && activeSiteAvatar.sourceUrl === ready.sourceUrl,
  )
}

export function OwnAccountAvatarCard({
  account,
  activeSiteAvatar,
  onAccountsState,
  onProfileState,
}: {
  account: AccountAvatarItem
  activeSiteAvatar: ProfileMedia | null
  onAccountsState: (state: AccountAvatarsState) => void
  onProfileState: (state: ProfileMediaState) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const meta = useMeta()
  const [busy, setBusy] = useState("")
  const [viewing, setViewing] = useState(false)
  const { ready, pending } = pickAccountCandidate(account)
  const isSiteAvatar = isOwnAccountActiveSiteAvatar(
    account,
    ready,
    activeSiteAvatar,
  )
  const display = ready ?? pending
  const handle = `@${account.accountHandle}`

  const runAvatarMutation = async (
    key: string,
    work: () => Promise<{
      items: AccountAvatarItem[]
      profile?: ProfileMediaState
    }>,
    notice: string,
  ) => {
    setBusy(key)
    const result = await act(work, notice)
    setBusy("")
    if (!result) return
    onAccountsState({ items: result.items })
    if (result.profile) {
      onProfileState(result.profile)
    }
  }

  return (
    <article
      className={`adm-candidate is-row${isSiteAvatar ? " is-active" : ""}`}
    >
      <div className="adm-candidate-preview">
        {ready?.previewUrl ? (
          <button
            type="button"
            className="media-opener"
            aria-label={t("admin.profile.view")}
            onClick={() => setViewing(true)}
          >
            <img src={ready.previewUrl} alt={handle} loading="lazy" />
          </button>
        ) : (
          <ImageOff aria-hidden />
        )}
      </div>
      <div className="adm-candidate-body">
        <span className="adm-chip-row">
          <Badge>{handle}</Badge>
          {account.mode === "hidden" ? (
            <Badge>{t(`admin.avatars.mode.${account.mode}`)}</Badge>
          ) : null}
          {pending ? (
            <StatusBadge
              status={pending.status === "failed" ? "failed" : "pending"}
            />
          ) : null}
        </span>
        {display ? (
          <small className="adm-candidate-meta">
            {meta(display).map((part) => (
              <span key={part}>{part}</span>
            ))}
          </small>
        ) : (
          <small className="adm-candidate-meta">
            <span>{t("admin.avatars.noCandidates")}</span>
          </small>
        )}
        {pending?.lastError ? (
          <small className="adm-error-text" title={pending.lastError}>
            {pending.lastError}
          </small>
        ) : null}
      </div>
      <div className="adm-candidate-actions">
        <Button
          size="sm"
          variant={isSiteAvatar ? "ghost" : undefined}
          icon={isSiteAvatar ? Check : UserRound}
          disabled={!ready || isSiteAvatar}
          busy={busy === "apply"}
          onClick={() =>
            ready &&
            void runAvatarMutation(
              "apply",
              () =>
                api.applyAccountAvatarToProfile(
                  "x",
                  account.accountKey,
                  ready.id,
                ),
              "admin.notice.avatarAppliedProfile",
            )
          }
        >
          {isSiteAvatar
            ? t("admin.profile.siteAvatarInUse")
            : t("admin.profile.useSiteAvatar")}
        </Button>
        <Button
          size="sm"
          variant="ghost"
          icon={Search}
          busy={busy === "discover"}
          onClick={() =>
            void runAvatarMutation(
              "discover",
              () => api.discoverAccountAvatar("x", account.accountKey),
              "admin.notice.avatarDiscovered",
            )
          }
        >
          {t("admin.avatars.discover")}
        </Button>
        {pending ? (
          <Button
            size="sm"
            variant="ghost"
            icon={Download}
            busy={busy === "download"}
            onClick={() =>
              void runAvatarMutation(
                "download",
                () =>
                  api.downloadAccountAvatarCandidate(
                    "x",
                    account.accountKey,
                    pending.id,
                  ),
                "admin.notice.avatarDownloaded",
              )
            }
          >
            {pending.status === "failed"
              ? t("admin.avatars.refresh")
              : t("admin.avatars.download")}
          </Button>
        ) : null}
      </div>
      {viewing && ready?.previewUrl ? (
        <Viewer
          title={handle}
          previewUrl={ready.previewUrl}
          metaParts={meta(ready)}
          onClose={() => setViewing(false)}
        />
      ) : null}
    </article>
  )
}
