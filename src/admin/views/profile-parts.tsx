import { useState } from "react"
import {
  Check,
  Download,
  Eye,
  ImageOff,
  RectangleHorizontal,
  RotateCcw,
  Trash2,
  UserRound,
  UsersRound,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { japanStamp } from "@/admin/format"
import type {
  ProfileMedia,
  ProfileMediaState,
  ProfileSlot,
} from "@/admin/types"
import {
  Badge,
  Button,
  ConfirmButton,
  Modal,
  Panel,
  StatusBadge,
} from "@/admin/ui"
import { useAdmin } from "@/admin/use-admin-data"

export const SLOTS: ProfileSlot[] = ["avatar", "banner"]

export const SLOT_ICONS = { avatar: UserRound, banner: RectangleHorizontal }

/** What the board shows while no candidate is selected for a slot. */
export const BUILT_IN: Record<ProfileSlot, string> = {
  avatar: "/assets/kano-avatar.jpg",
  banner: "/assets/kano-banner.jpg",
}

/** Size and time of a candidate, the muted line under its image. */
export function useMeta() {
  useAppSettings()
  return (item: {
    width: number | null
    height: number | null
    updatedAt: string
  }) =>
    [
      item.width && item.height ? `${item.width}×${item.height}` : "",
      japanStamp(item.updatedAt),
    ].filter(Boolean)
}

/** Sort slot candidates with the active item first, YouTube avatars before custom ones, then newest. */
export function orderedForSlot(items: ProfileMedia[], slot: ProfileSlot) {
  return [...items].sort((a, b) => {
    if (a.isActive !== b.isActive) return a.isActive ? -1 : 1
    if (slot === "avatar" && a.source !== b.source) {
      if (a.source === "youtube") return -1
      if (b.source === "youtube") return 1
    }
    return b.updatedAt.localeCompare(a.updatedAt)
  })
}

export function Viewer({
  title,
  previewUrl,
  metaParts,
  onClose,
}: {
  title: string
  previewUrl: string
  metaParts: string[]
  onClose: () => void
}) {
  return (
    <Modal open className="adm-image-viewer" title={title} onClose={onClose}>
      <div className="adm-image-viewer-stage">
        <img src={previewUrl} alt="" />
      </div>
      {metaParts.length ? (
        <div className="adm-image-viewer-nav">{metaParts.join(" · ")}</div>
      ) : null}
    </Modal>
  )
}

/** The board's profile card drawn with the images in use, and what each is. */
export function Preview({
  state,
  onState,
  onManageAvatars,
}: {
  state: ProfileMediaState
  onState: (state: ProfileMediaState) => void
  onManageAvatars: () => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const meta = useMeta()
  const [clearing, setClearing] = useState<ProfileSlot | null>(null)
  const url = (slot: ProfileSlot) =>
    state.active[slot]?.previewUrl || BUILT_IN[slot]
  const clearSlot = async (slot: ProfileSlot) => {
    setClearing(slot)
    const next = await act(
      () => api.clearProfileMedia(slot),
      "admin.notice.mediaCleared",
    )
    setClearing(null)
    if (next) onState({ items: next.items, active: next.active })
  }
  return (
    <Panel
      icon={Eye}
      title={t("admin.profile.preview")}
      actions={
        <Button
          size="sm"
          variant="ghost"
          icon={UsersRound}
          onClick={onManageAvatars}
        >
          {t("admin.profile.manageAvatars")}
        </Button>
      }
      className="adm-profile-preview-panel"
    >
      <div className="adm-profile-preview">
        <div className="adm-profile-card" aria-hidden>
          <div className="adm-profile-card-banner">
            <img src={url("banner")} alt="" />
          </div>
          <img className="adm-profile-card-avatar" src={url("avatar")} alt="" />
          <span className="adm-profile-card-line" />
          <span className="adm-profile-card-line is-short" />
        </div>
        <dl className="adm-profile-slots">
          {SLOTS.map((slot) => {
            const active = state.active[slot]
            const SlotIcon = SLOT_ICONS[slot]
            return (
              <div key={slot}>
                <dt>
                  <SlotIcon aria-hidden />
                  <strong>{t(`admin.profile.${slot}`)}</strong>
                </dt>
                <dd>
                  {active?.previewUrl ? (
                    <>
                      <Badge tone="leaf" icon={Check}>
                        {t("admin.profile.active")}
                      </Badge>
                      <Badge>
                        {t(`admin.profile.source.${active.source}`)}
                      </Badge>
                      <small>{meta(active).join(" · ")}</small>
                      <Button
                        size="sm"
                        variant="ghost"
                        icon={RotateCcw}
                        busy={clearing === slot}
                        onClick={() => void clearSlot(slot)}
                      >
                        {t("admin.profile.useBuiltIn")}
                      </Button>
                    </>
                  ) : (
                    <Badge>{t("admin.profile.builtIn")}</Badge>
                  )}
                </dd>
              </div>
            )
          })}
        </dl>
      </div>
    </Panel>
  )
}

export function Candidate({
  item,
  onState,
}: {
  item: ProfileMedia
  onState: (state: ProfileMediaState) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const meta = useMeta()
  const [busy, setBusy] = useState("")
  const [viewing, setViewing] = useState(false)
  const run = async (
    key: string,
    work: () => Promise<ProfileMediaState>,
    notice: string,
  ) => {
    setBusy(key)
    const state = await act(work, notice)
    setBusy("")
    if (state) onState({ items: state.items, active: state.active })
  }
  const ready = item.status === "ready"
  return (
    <article
      className={`adm-candidate is-row${item.isActive ? " is-active" : ""}`}
    >
      <div className="adm-candidate-preview">
        {item.previewUrl ? (
          <button
            type="button"
            className="media-opener"
            aria-label={t("admin.profile.view")}
            onClick={() => setViewing(true)}
          >
            <img src={item.previewUrl} alt="" loading="lazy" />
          </button>
        ) : (
          <ImageOff aria-hidden />
        )}
      </div>
      <div className="adm-candidate-body">
        <span className="adm-chip-row">
          <Badge>{t(`admin.profile.source.${item.source}`)}</Badge>
          {ready ? null : (
            <StatusBadge
              status={item.status === "failed" ? "failed" : "pending"}
            />
          )}
        </span>
        <small className="adm-candidate-meta">
          {meta(item).map((part) => (
            <span key={part}>{part}</span>
          ))}
        </small>
        {item.lastError ? (
          <small className="adm-error-text" title={item.lastError}>
            {item.lastError}
          </small>
        ) : null}
      </div>
      <div className="adm-candidate-actions">
        <Button
          size="sm"
          variant={item.isActive ? "ghost" : undefined}
          icon={item.isActive ? Check : SLOT_ICONS[item.slot]}
          disabled={!ready || item.isActive}
          aria-pressed={item.isActive}
          busy={busy === item.slot}
          onClick={() =>
            run(
              item.slot,
              () => api.selectProfileMedia(item.id, item.slot),
              "admin.notice.mediaSelected",
            )
          }
        >
          {item.isActive
            ? t("admin.profile.active")
            : t(`admin.profile.useSlot.${item.slot}`)}
        </Button>
        {item.source !== "upload" ? (
          <Button
            size="sm"
            variant="ghost"
            icon={Download}
            busy={busy === "download"}
            onClick={() =>
              run(
                "download",
                () => api.downloadProfileMedia(item.id),
                "admin.notice.mediaDownloaded",
              )
            }
          >
            {ready ? t("admin.profile.refresh") : t("admin.profile.download")}
          </Button>
        ) : null}
        <ConfirmButton
          size="sm"
          variant="ghost"
          icon={Trash2}
          confirmLabel={t("admin.profile.deleteConfirm")}
          busy={busy === "delete"}
          onConfirm={() =>
            run(
              "delete",
              () => api.deleteProfileMedia(item.id),
              "admin.notice.mediaDeleted",
            )
          }
        >
          {t("admin.profile.delete")}
        </ConfirmButton>
      </div>
      {viewing && item.previewUrl ? (
        <Viewer
          title={t(`admin.profile.source.${item.source}`)}
          previewUrl={item.previewUrl}
          metaParts={meta(item)}
          onClose={() => setViewing(false)}
        />
      ) : null}
    </article>
  )
}
