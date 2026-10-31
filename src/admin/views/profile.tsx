import { useRef, useState } from "react"
import {
  Check,
  Download,
  Eye,
  ImageOff,
  Images,
  Link2,
  Plus,
  RectangleHorizontal,
  Search,
  Trash2,
  Upload,
  UserRound,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import { formatDateTime } from "@/admin/format"
import type {
  ProfileMedia,
  ProfileMediaState,
  ProfileSlot,
} from "@/admin/types"
import {
  Badge,
  Button,
  ConfirmButton,
  Empty,
  Field,
  Loading,
  Modal,
  PageHeader,
  Panel,
  Segmented,
  Spinner,
  StatusBadge,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"

const SLOTS: ProfileSlot[] = ["avatar", "banner"]

const SLOT_ICONS = { avatar: UserRound, banner: RectangleHorizontal }

/** What the board shows while no candidate is selected for a slot. */
const BUILT_IN: Record<ProfileSlot, string> = {
  avatar: "/assets/kano-avatar.jpg",
  banner: "/assets/kano-banner.jpg",
}

/** Size and time of a candidate, the muted line under its image. */
function useMeta() {
  const { locale } = useAppSettings()
  return (item: ProfileMedia) =>
    [
      item.width && item.height ? `${item.width}×${item.height}` : "",
      formatDateTime(item.updatedAt, locale),
    ].filter(Boolean)
}

/** The images in use first, avatar before banner, then the newest. */
function ordered(items: ProfileMedia[]) {
  const rank = (item: ProfileMedia) =>
    item.isActive ? SLOTS.indexOf(item.slot) : SLOTS.length
  return [...items].sort(
    (a, b) => rank(a) - rank(b) || b.updatedAt.localeCompare(a.updatedAt),
  )
}

function Viewer({
  item,
  onClose,
}: {
  item: ProfileMedia
  onClose: () => void
}) {
  const { t } = useAppSettings()
  const meta = useMeta()
  return (
    <Modal
      open
      className="adm-image-viewer"
      title={t(`admin.profile.source.${item.source}`)}
      onClose={onClose}
    >
      <div className="adm-image-viewer-stage">
        <img src={item.previewUrl ?? ""} alt="" />
      </div>
      <div className="adm-image-viewer-nav">{meta(item).join(" · ")}</div>
    </Modal>
  )
}

/** The board's profile card drawn with the images in use, and what each is. */
function Preview({ state }: { state: ProfileMediaState }) {
  const { t } = useAppSettings()
  const meta = useMeta()
  const url = (slot: ProfileSlot) =>
    state.active[slot]?.previewUrl || BUILT_IN[slot]
  return (
    <Panel
      icon={Eye}
      title={t("admin.profile.preview")}
      description={t("admin.profile.previewHint")}
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
                  <small>{t(`admin.profile.${slot}Hint`)}</small>
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
                    </>
                  ) : (
                    <>
                      <Badge>{t("admin.profile.builtIn")}</Badge>
                      <small>{t("admin.profile.noneActive")}</small>
                    </>
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

function Candidate({
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
    <article className={`adm-candidate${item.isActive ? " is-active" : ""}`}>
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
        {item.isActive ? (
          <Badge tone="leaf" icon={Check} className="adm-candidate-flag">
            {t(`admin.profile.activeSlot.${item.slot}`)}
          </Badge>
        ) : null}
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
        <div
          className="adm-candidate-use"
          role="group"
          aria-label={t("admin.profile.useAs")}
        >
          <span aria-hidden>{t("admin.profile.useAs")}</span>
          {SLOTS.map((slot) => {
            const current = item.isActive && item.slot === slot
            return (
              <Button
                key={slot}
                size="sm"
                variant={current ? "ghost" : undefined}
                icon={current ? Check : SLOT_ICONS[slot]}
                disabled={!ready || current}
                aria-pressed={current}
                busy={busy === slot}
                onClick={() =>
                  run(
                    slot,
                    () => api.selectProfileMedia(item.id, slot),
                    "admin.notice.mediaSelected",
                  )
                }
              >
                {t(`admin.profile.${slot}`)}
              </Button>
            )
          })}
        </div>
        <div className="adm-candidate-tools">
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
      </div>
      {viewing ? (
        <Viewer item={item} onClose={() => setViewing(false)} />
      ) : null}
    </article>
  )
}

const UPLOAD_TYPES = ["image/png", "image/jpeg", "image/webp", "image/gif"]

/** The source a pasted image URL belongs to, read from its host. */
function sourceOf(url: string): "x" | "youtube" {
  try {
    return /(?:^|\.)twimg\.com$/u.test(new URL(url).hostname) ? "x" : "youtube"
  } catch {
    return "x"
  }
}

type Perform = (
  key: string,
  work: () => Promise<ProfileMediaState>,
  notice: string,
) => Promise<void>

function FetchTab({ busy, perform }: { busy: string; perform: Perform }) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const [source, setSource] = useState<"x" | "youtube">("x")
  return (
    <div className="adm-profile-add">
      <p>{t("admin.profile.fetchHint")}</p>
      <div className="adm-profile-add-source">
        <Segmented
          value={source}
          onChange={setSource}
          label={t("admin.profile.discoverFrom")}
          options={[
            { value: "x", label: t("admin.profile.source.x") },
            { value: "youtube", label: t("admin.profile.source.youtube") },
          ]}
        />
        <Button
          icon={Search}
          busy={busy === "discover"}
          onClick={() =>
            perform(
              "discover",
              () => api.discoverProfileMedia(source),
              "admin.notice.mediaDiscovered",
            )
          }
        >
          {t("admin.profile.discover")}
        </Button>
      </div>
    </div>
  )
}

function UploadTab({ busy, perform }: { busy: string; perform: Perform }) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const [url, setUrl] = useState("")
  const [over, setOver] = useState(false)
  const file = useRef<HTMLInputElement>(null)
  const upload = (chosen: File | undefined) => {
    if (chosen && UPLOAD_TYPES.includes(chosen.type))
      void perform(
        "upload",
        () => api.uploadProfileMedia(chosen),
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
            () => api.addProfileMedia(sourceOf(url), url),
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

/** Fetch reads a profile page; Upload takes an image URL or a file. */
function AddDialog({
  onState,
  onClose,
}: {
  onState: (state: ProfileMediaState) => void
  onClose: () => void
}) {
  const { t } = useAppSettings()
  const { act } = useAdmin()
  const [tab, setTab] = useState<"fetch" | "upload">("fetch")
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
      title={t("admin.profile.addTitle")}
      onClose={onClose}
    >
      <div
        className="adm-tabs is-bare"
        role="tablist"
        aria-label={t("admin.profile.addTitle")}
      >
        {(["fetch", "upload"] as const).map((id) => (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={tab === id}
            className={tab === id ? "is-active" : undefined}
            onClick={() => setTab(id)}
          >
            {t(`admin.profile.${id}`)}
          </button>
        ))}
      </div>
      <div role="tabpanel" className="adm-tab-panel">
        {tab === "fetch" ? (
          <FetchTab busy={busy} perform={perform} />
        ) : (
          <UploadTab busy={busy} perform={perform} />
        )}
      </div>
    </Modal>
  )
}

export function ProfileView(_: ViewProps) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  const media = useResource(() => api.profileMedia(), [])
  const [adding, setAdding] = useState(false)
  const state = media.data
  const items = state ? ordered(state.items) : []
  return (
    <div className="adm-view">
      <PageHeader
        title={t("admin.profile.title")}
        description={t("admin.profile.description")}
      />
      {!state ? (
        <Loading />
      ) : (
        <div className="adm-grid">
          <Preview state={state} />
          <Panel
            icon={Images}
            title={
              <>
                {t("admin.profile.candidates")}
                {items.length ? (
                  <em className="adm-profile-count">{items.length}</em>
                ) : null}
              </>
            }
            actions={
              <Button size="sm" icon={Plus} onClick={() => setAdding(true)}>
                {t("admin.profile.addButton")}
              </Button>
            }
          >
            {items.length ? (
              <div className="adm-candidate-grid">
                {items.map((item) => (
                  <Candidate
                    key={item.id}
                    item={item}
                    onState={media.setData}
                  />
                ))}
              </div>
            ) : (
              <Empty title={t("admin.profile.noCandidates")} />
            )}
          </Panel>
          {adding ? (
            <AddDialog
              onState={media.setData}
              onClose={() => setAdding(false)}
            />
          ) : null}
        </div>
      )}
    </div>
  )
}
