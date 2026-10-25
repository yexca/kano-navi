import { useRef, useState } from "react"
import {
  Check,
  Download,
  ImageOff,
  Link2,
  Search,
  Upload,
  UserRound,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatDateTime } from "@/admin/format"
import type { ProfileMedia, ProfileMediaState } from "@/admin/types"
import {
  Badge,
  Button,
  Empty,
  Field,
  Loading,
  PageHeader,
  Panel,
  StatusBadge,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"

function Candidate({
  item,
  onState,
}: {
  item: ProfileMedia
  onState: (state: ProfileMediaState) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const [busy, setBusy] = useState("")
  const run = async (action: "download" | "select") => {
    setBusy(action)
    const state = await act(
      () =>
        action === "download"
          ? api.downloadProfileMedia(item.id)
          : api.selectProfileMedia(item.id),
      action === "download"
        ? "admin.notice.mediaDownloaded"
        : "admin.notice.mediaSelected",
    )
    setBusy("")
    if (state) onState(state)
  }
  const ready = item.status === "ready"
  return (
    <article className={`adm-candidate${item.isActive ? " is-active" : ""}`}>
      <div className={`adm-candidate-preview is-${item.slot}`}>
        {item.previewUrl ? (
          <img src={item.previewUrl} alt="" loading="lazy" />
        ) : (
          <ImageOff aria-hidden />
        )}
      </div>
      <div className="adm-candidate-body">
        <span className="adm-chip-row">
          <Badge>{t(`admin.profile.source.${item.source}`)}</Badge>
          {item.isActive ? (
            <Badge tone="leaf" icon={Check}>
              {t("admin.profile.active")}
            </Badge>
          ) : (
            <StatusBadge
              status={
                ready
                  ? "completed"
                  : item.status === "failed"
                    ? "failed"
                    : "pending"
              }
            />
          )}
        </span>
        <small>
          {item.width && item.height ? `${item.width}×${item.height} · ` : ""}
          {formatDateTime(item.updatedAt, locale)}
        </small>
        {item.lastError ? (
          <small className="adm-error-text">{item.lastError}</small>
        ) : null}
        <div className="adm-actions">
          {item.source !== "upload" ? (
            <Button
              size="sm"
              variant="ghost"
              icon={Download}
              busy={busy === "download"}
              onClick={() => run("download")}
            >
              {ready ? t("admin.profile.refresh") : t("admin.profile.download")}
            </Button>
          ) : null}
          {!item.isActive ? (
            <Button
              size="sm"
              icon={Check}
              disabled={!ready}
              busy={busy === "select"}
              onClick={() => run("select")}
            >
              {t("admin.profile.use")}
            </Button>
          ) : null}
        </div>
      </div>
    </article>
  )
}

function SlotPanel({
  slot,
  state,
  onState,
}: {
  slot: "avatar" | "banner"
  state: ProfileMediaState
  onState: (state: ProfileMediaState) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [url, setUrl] = useState("")
  const [source, setSource] = useState<"x" | "youtube">("x")
  const [busy, setBusy] = useState("")
  const file = useRef<HTMLInputElement>(null)
  const items = state.items.filter((item) => item.slot === slot)
  const active = state.active[slot]
  const perform = async (
    key: string,
    work: () => Promise<ProfileMediaState>,
    notice: string,
  ) => {
    setBusy(key)
    const next = await act(work, notice)
    setBusy("")
    if (next) onState({ items: next.items, active: next.active })
    return next
  }
  return (
    <Panel
      icon={UserRound}
      title={t(`admin.profile.${slot}`)}
      description={t(`admin.profile.${slot}Hint`)}
    >
      <div className={`adm-profile-current is-${slot}`}>
        {active?.previewUrl ? (
          <img src={active.previewUrl} alt="" />
        ) : (
          <span>{t("admin.profile.noneActive")}</span>
        )}
      </div>
      <div className="adm-actions">
        <select
          value={source}
          onChange={(e) => setSource(e.target.value as "x" | "youtube")}
          aria-label={t("admin.profile.discoverFrom")}
        >
          <option value="x">{t("admin.profile.source.x")}</option>
          <option value="youtube">{t("admin.profile.source.youtube")}</option>
        </select>
        <Button
          size="sm"
          icon={Search}
          busy={busy === "discover"}
          onClick={() =>
            perform(
              "discover",
              () => api.discoverProfileMedia(slot, source),
              "admin.notice.mediaDiscovered",
            )
          }
        >
          {t("admin.profile.discover")}
        </Button>
        <input
          ref={file}
          type="file"
          accept="image/png,image/jpeg,image/webp,image/gif"
          hidden
          onChange={(event) => {
            const chosen = event.target.files?.[0]
            event.target.value = ""
            if (chosen)
              void perform(
                "upload",
                () => api.uploadProfileMedia(slot, chosen),
                "admin.notice.mediaUploaded",
              )
          }}
        />
        <Button
          size="sm"
          variant="ghost"
          icon={Upload}
          busy={busy === "upload"}
          onClick={() => file.current?.click()}
        >
          {t("admin.profile.upload")}
        </Button>
      </div>
      <form
        className="adm-inline-form"
        onSubmit={async (event) => {
          event.preventDefault()
          if (
            await perform(
              "add",
              () => api.addProfileMedia(slot, source, url),
              "admin.notice.mediaAdded",
            )
          )
            setUrl("")
        }}
      >
        <Field
          label={t("admin.profile.addUrl")}
          hint={t("admin.profile.addUrlHint")}
        >
          <input
            type="url"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://"
            required
          />
        </Field>
        <Button type="submit" size="sm" icon={Link2} busy={busy === "add"}>
          {t("admin.profile.add")}
        </Button>
      </form>
      {items.length ? (
        <div className="adm-candidate-grid">
          {items.map((item) => (
            <Candidate key={item.id} item={item} onState={onState} />
          ))}
        </div>
      ) : (
        <Empty title={t("admin.profile.noCandidates")} />
      )}
    </Panel>
  )
}

export function ProfileView(_: ViewProps) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const media = useResource(() => api.profileMedia(), [])
  const state = media.data
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.profile.title")}
        description={t("admin.profile.description")}
      />
      {!state ? (
        <Loading />
      ) : (
        <div className="adm-grid is-two">
          {(["avatar", "banner"] as const).map((slot) => (
            <SlotPanel
              key={slot}
              slot={slot}
              state={state}
              onState={media.setData}
            />
          ))}
        </div>
      )}
    </div>
  )
}
