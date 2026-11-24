import { useRef, useState } from "react"
import { Link2, Search, Upload } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ProfileMediaState, ProfileSlot } from "@/admin/types"
import { Button, Field, Modal, Spinner } from "@/admin/ui"
import { useAdmin } from "@/admin/use-admin-data"

export const UPLOAD_TYPES = [
  "image/png",
  "image/jpeg",
  "image/webp",
  "image/gif",
]

/** The source a pasted image URL belongs to, read from its host. */
export function sourceOf(url: string): "x" | "youtube" {
  try {
    return /(?:^|\.)twimg\.com$/u.test(new URL(url).hostname) ? "x" : "youtube"
  } catch {
    return "x"
  }
}

export type Perform = (
  key: string,
  work: () => Promise<ProfileMediaState>,
  notice: string,
) => Promise<void>

export function UploadTab({
  slot,
  busy,
  perform,
}: {
  slot: ProfileSlot
  busy: string
  perform: Perform
}) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const [url, setUrl] = useState("")
  const [over, setOver] = useState(false)
  const file = useRef<HTMLInputElement>(null)
  const upload = (chosen: File | undefined) => {
    if (chosen && UPLOAD_TYPES.includes(chosen.type))
      void perform(
        "upload",
        () => api.uploadProfileMedia(chosen, slot),
        "admin.notice.mediaUploaded",
      )
  }
  return (
    <div className="adm-profile-add">
      <form
        className="adm-inline-form"
        onSubmit={(event) => {
          event.preventDefault()
          void perform(
            "add",
            () => api.addProfileMedia(sourceOf(url), url, slot),
            "admin.notice.mediaAdded",
          )
        }}
      >
        <Field label={t("admin.profile.addUrl")}>
          <input
            type="url"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://"
            aria-describedby="profile-url-hint"
            required
          />
        </Field>
        <Button type="submit" size="sm" icon={Link2} busy={busy === "add"}>
          {t("admin.profile.add")}
        </Button>
      </form>
      <small id="profile-url-hint" className="adm-field-hint">
        {t("admin.profile.addUrlHint")}
      </small>
      <span className="adm-profile-or">{t("admin.profile.or")}</span>
      <input
        ref={file}
        type="file"
        accept={UPLOAD_TYPES.join(",")}
        hidden
        onChange={(event) => {
          const chosen = event.target.files?.[0]
          event.target.value = ""
          upload(chosen)
        }}
      />
      <button
        type="button"
        className={`adm-profile-drop${over ? " is-over" : ""}`}
        disabled={busy === "upload"}
        onClick={() => file.current?.click()}
        onDragOver={(event) => {
          event.preventDefault()
          event.dataTransfer.dropEffect = "copy"
          setOver(true)
        }}
        onDragLeave={() => setOver(false)}
        onDrop={(event) => {
          event.preventDefault()
          setOver(false)
          upload(event.dataTransfer.files[0])
        }}
      >
        {busy === "upload" ? <Spinner /> : <Upload aria-hidden />}
        <strong>{t("admin.profile.dropTitle")}</strong>
        <small>{t("admin.profile.dropHint")}</small>
      </button>
    </div>
  )
}

/** Upload takes an image URL or a file for the site's avatar or banner. */
export function AddDialog({
  slot,
  onState,
  onClose,
}: {
  slot: ProfileSlot
  onState: (state: ProfileMediaState) => void
  onClose: () => void
}) {
  const { t } = useAppSettings()
  const { act } = useAdmin()
  const [busy, setBusy] = useState("")
  const perform: Perform = async (key, work, notice) => {
    setBusy(key)
    const next = await act(work, notice)
    setBusy("")
    if (!next) return
    onState({ items: next.items, active: next.active })
    onClose()
  }
  return (
    <Modal
      open
      className="adm-profile-dialog"
      title={
        slot === "banner"
          ? t("admin.profile.addBannerTitle")
          : t("admin.profile.addAvatarTitle")
      }
      onClose={onClose}
    >
      <UploadTab slot={slot} busy={busy} perform={perform} />
    </Modal>
  )
}

export function FetchBannerDialog({
  ownHandles,
  onState,
  onClose,
}: {
  ownHandles: string[]
  onState: (state: ProfileMediaState) => void
  onClose: () => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [source, setSource] = useState<"x" | "youtube">("x")
  const [handle, setHandle] = useState(ownHandles[0] ?? "")
  const [busy, setBusy] = useState(false)
  const selectedHandle =
    source === "x" ? handle || ownHandles[0] || undefined : undefined

  const submit = async () => {
    setBusy(true)
    const next = await act(
      () => api.discoverProfileMedia(source, "banner", selectedHandle),
      "admin.notice.mediaDiscovered",
    )
    setBusy(false)
    if (!next) return
    onState({ items: next.items, active: next.active })
    onClose()
  }

  return (
    <Modal
      open
      className="adm-profile-dialog"
      title={t("admin.profile.fetchBannerTitle")}
      onClose={onClose}
    >
      <div className="adm-profile-add">
        <div
          className="adm-chip-row"
          role="group"
          aria-label={t("admin.profile.discoverFrom")}
        >
          {(["x", "youtube"] as const).map((choice) => (
            <Button
              key={choice}
              size="sm"
              variant={source === choice ? undefined : "ghost"}
              onClick={() => setSource(choice)}
            >
              {t(`admin.profile.source.${choice}`)}
            </Button>
          ))}
        </div>
        {source === "x" && ownHandles.length ? (
          <Field label={t("admin.profile.xAccount")}>
            <div className="adm-chip-row">
              {ownHandles.map((itemHandle) => {
                const chosen = (selectedHandle ?? "") === itemHandle
                return (
                  <Button
                    key={itemHandle}
                    size="sm"
                    variant={chosen ? undefined : "ghost"}
                    onClick={() => setHandle(itemHandle)}
                  >
                    @{itemHandle}
                  </Button>
                )
              })}
            </div>
          </Field>
        ) : null}
        <div className="adm-profile-dialog-actions">
          <Button icon={Search} busy={busy} onClick={() => void submit()}>
            {t("admin.profile.discover")}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
