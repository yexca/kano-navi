import { useEffect, useState } from "react"
import {
  Check,
  Clapperboard,
  Download,
  Image as ImageIcon,
  LoaderCircle,
  Plus,
  Save,
  Search,
  Upload,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { SourceFetchCard } from "@/admin/components/source-fetch-card"
import { adminMessage, formatAdminError, jsonBody, request } from "@/admin/api"
import {
  Btn,
  Card,
  CardHeader,
  Field,
  IconBtn,
  PageHeader,
  Tag,
} from "@/admin/components/primitives"
import { cn } from "@/lib/utils"

const profileSlots = ["avatar", "banner"]

function slotLabel(slot, t) {
  return t(slot === "banner" ? "admin.profile.banner" : "admin.profile.avatar")
}

function sourceLabel(source, t) {
  if (source === "youtube") return t("admin.profile.source.youtube")
  if (source === "upload") return t("admin.profile.source.upload")
  return t("admin.profile.source.x")
}

function FeaturedVideoCard({ config, videos, onSaved, onError, t }) {
  const [value, setValue] = useState(config?.featuredVideoId || "")
  const [busy, setBusy] = useState(false)
  useEffect(
    () => setValue(config?.featuredVideoId || ""),
    [config?.featuredVideoId],
  )
  const selected = videos.find((video) => video.id === value)
  // Only local cache/static paths are rendered; the console never loads a
  // platform CDN directly.
  const preview = String(selected?.thumbnailUrl || "").startsWith("/")
    ? selected.thumbnailUrl
    : null

  const save = async (event) => {
    event.preventDefault()
    setBusy(true)
    try {
      const payload = await request("/config", {
        method: "PUT",
        body: jsonBody({ featuredVideoId: value || null }),
      })
      onSaved(payload)
    } catch (error) {
      onError(error)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card>
      <CardHeader
        icon={Clapperboard}
        title={t("admin.content.featuredTitle")}
        description={t("admin.content.featuredDescription")}
      />
      <form className="adm-featured" onSubmit={save}>
        <div className="adm-featured-preview">
          {preview ? (
            <img src={preview} alt="" />
          ) : (
            <Clapperboard aria-hidden="true" />
          )}
        </div>
        <div className="adm-featured-form">
          <Field label={t("admin.content.featuredVideo")}>
            <select
              value={value}
              onChange={(event) => setValue(event.target.value)}
            >
              <option value="">{t("admin.content.notSet")}</option>
              {videos.map((video) => (
                <option value={video.id} key={video.id}>
                  {video.title}
                </option>
              ))}
            </select>
          </Field>
          <Btn
            type="submit"
            variant="primary"
            icon={Save}
            busy={busy}
            disabled={value === (config?.featuredVideoId || "")}
          >
            {t("admin.action.save")}
          </Btn>
        </div>
      </form>
    </Card>
  )
}

function ProfileMediaCard({ value, onChange, onNotice, onError, t }) {
  const [source, setSource] = useState({ avatar: "x", banner: "x" })
  const [urls, setUrls] = useState({ avatar: "", banner: "" })
  const [busyKey, setBusyKey] = useState("")
  const items = value?.items || []
  const active = value?.active || {}

  const run = async (key, action) => {
    setBusyKey(key)
    try {
      const payload = await action()
      if (payload?.items) onChange(payload)
      return payload
    } catch (error) {
      onError(formatAdminError(error))
      return null
    } finally {
      setBusyKey("")
    }
  }

  const discover = (slot, targetSource) =>
    run(`discover-${slot}-${targetSource}`, () =>
      request("/profile-media/discover", {
        method: "POST",
        body: jsonBody({ slot, source: targetSource }),
      }),
    ).then((payload) => {
      if (payload)
        onNotice(
          adminMessage("admin.profile.candidateUpdated", {
            source: sourceLabel(targetSource, t),
            slot: slotLabel(slot, t),
          }),
        )
    })

  const addUrl = (event, slot) => {
    event.preventDefault()
    if (!urls[slot].trim()) return
    run(`add-${slot}`, () =>
      request("/profile-media", {
        method: "POST",
        body: jsonBody({ slot, source: source[slot], sourceUrl: urls[slot] }),
      }),
    ).then((payload) => {
      if (!payload) return
      setUrls((current) => ({ ...current, [slot]: "" }))
      onNotice(adminMessage("admin.profile.candidateAdded"))
    })
  }

  const upload = (event, slot) => {
    const file = event.target.files?.[0]
    event.target.value = ""
    if (!file) return
    run(`upload-${slot}`, () =>
      request(`/profile-media/upload/${slot}`, {
        method: "POST",
        headers: { "content-type": file.type || "application/octet-stream" },
        body: file,
      }),
    ).then((payload) => {
      if (payload)
        onNotice(
          adminMessage("admin.profile.candidateUploaded", {
            slot: slotLabel(slot, t),
          }),
        )
    })
  }

  const mutate = (item, action) =>
    run(`${action}-${item.id}`, () =>
      request(`/profile-media/${encodeURIComponent(item.id)}/${action}`, {
        method: "POST",
      }),
    ).then((payload) => {
      if (!payload) return
      onNotice(
        action === "select"
          ? adminMessage("admin.profile.switched", {
              slot: slotLabel(item.slot, t),
            })
          : adminMessage("admin.profile.downloaded"),
      )
    })

  return (
    <Card>
      <CardHeader
        icon={ImageIcon}
        title={t("admin.profile.title")}
        description={t("admin.profile.help")}
      />
      <div className="adm-profile-slots">
        {profileSlots.map((slot) => {
          const current = active[slot]
          const slotItems = items.filter((item) => item.slot === slot)
          return (
            <article className="adm-profile-slot" key={slot}>
              <div className={cn("adm-profile-current", `is-${slot}`)}>
                {current?.publicUrl ? (
                  <img src={current.publicUrl} alt="" />
                ) : (
                  <ImageIcon aria-hidden="true" />
                )}
                <div>
                  <strong>{slotLabel(slot, t)}</strong>
                  <small>
                    {current
                      ? `${sourceLabel(current.source, t)} · ${t("admin.profile.current")}`
                      : t("admin.profile.notSelected")}
                  </small>
                </div>
              </div>
              <div className="adm-profile-actions">
                {["x", "youtube"].map((targetSource) => (
                  <Btn
                    key={targetSource}
                    variant="outline"
                    size="sm"
                    icon={Search}
                    busy={busyKey === `discover-${slot}-${targetSource}`}
                    disabled={busyKey !== ""}
                    onClick={() => discover(slot, targetSource)}
                  >
                    {t("admin.profile.discover")} {sourceLabel(targetSource, t)}
                  </Btn>
                ))}
                <label
                  className={cn(
                    "adm-btn adm-btn-outline adm-btn-sm adm-upload",
                  )}
                >
                  {busyKey === `upload-${slot}` ? (
                    <LoaderCircle className="adm-spin" aria-hidden="true" />
                  ) : (
                    <Upload aria-hidden="true" />
                  )}
                  {t("admin.action.upload")}
                  <input
                    type="file"
                    accept="image/avif,image/gif,image/jpeg,image/png,image/webp"
                    onChange={(event) => upload(event, slot)}
                    disabled={busyKey !== ""}
                  />
                </label>
              </div>
              <form
                className="adm-url-row"
                onSubmit={(event) => addUrl(event, slot)}
              >
                <select
                  value={source[slot]}
                  onChange={(event) =>
                    setSource((currentSource) => ({
                      ...currentSource,
                      [slot]: event.target.value,
                    }))
                  }
                  aria-label={t("admin.profile.sourceLabel", {
                    slot: slotLabel(slot, t),
                  })}
                >
                  <option value="x">{t("admin.profile.source.x")}</option>
                  <option value="youtube">
                    {t("admin.profile.source.youtube")}
                  </option>
                </select>
                <input
                  type="url"
                  value={urls[slot]}
                  onChange={(event) =>
                    setUrls((currentUrls) => ({
                      ...currentUrls,
                      [slot]: event.target.value,
                    }))
                  }
                  placeholder={t("admin.profile.imageUrl.placeholder")}
                  aria-label={t("admin.profile.imageUrl.aria", {
                    slot: slotLabel(slot, t),
                  })}
                />
                <IconBtn
                  type="submit"
                  label={t("admin.action.addImageUrl")}
                  icon={Plus}
                  busy={busyKey === `add-${slot}`}
                  disabled={busyKey !== ""}
                />
              </form>
              <div className="adm-candidates">
                {slotItems.map((item) => (
                  <div className="adm-candidate" key={item.id}>
                    <div className="adm-candidate-preview">
                      {item.previewUrl ? (
                        <img src={item.previewUrl} alt="" />
                      ) : (
                        <ImageIcon aria-hidden="true" />
                      )}
                    </div>
                    <div className="adm-candidate-copy">
                      <strong>{sourceLabel(item.source, t)}</strong>
                      <small>
                        {t(
                          `admin.profile.status.${
                            ["ready", "failed"].includes(item.status)
                              ? item.status
                              : "pending"
                          }`,
                        )}
                      </small>
                    </div>
                    <div className="adm-candidate-actions">
                      {item.status !== "ready" ? (
                        <IconBtn
                          label={t("admin.action.download")}
                          icon={Download}
                          busy={busyKey === `download-${item.id}`}
                          disabled={busyKey !== ""}
                          onClick={() => mutate(item, "download")}
                        />
                      ) : null}
                      {item.status === "ready" && !item.isActive ? (
                        <Btn
                          variant="outline"
                          size="sm"
                          icon={Check}
                          busy={busyKey === `select-${item.id}`}
                          disabled={busyKey !== ""}
                          onClick={() => mutate(item, "select")}
                        >
                          {t("admin.action.select")}
                        </Btn>
                      ) : item.isActive ? (
                        <Tag tone="leaf">{t("admin.profile.current")}</Tag>
                      ) : null}
                    </div>
                  </div>
                ))}
                {!slotItems.length ? (
                  <p className="adm-muted adm-small">
                    {t("admin.profile.noCandidates")}
                  </p>
                ) : null}
              </div>
            </article>
          )
        })}
      </div>
      <p className="adm-card-foot">{t("admin.profile.footnote")}</p>
    </Card>
  )
}

export function ContentView({ data }) {
  const { t } = useAppSettings()
  const {
    config,
    setConfig,
    videos,
    profileMedia,
    setProfileMedia,
    notify,
    fail,
  } = data
  return (
    <section className="adm-view" aria-labelledby="adm-content-title">
      <PageHeader
        id="adm-content-title"
        title={t("admin.content.title")}
        description={t("admin.content.description")}
      />
      <SourceFetchCard source="youtube" data={data} />
      <FeaturedVideoCard
        config={config}
        videos={videos}
        onSaved={(payload) => {
          setConfig(payload)
          notify(adminMessage("admin.content.featuredSaved"))
        }}
        onError={fail}
        t={t}
      />
      <ProfileMediaCard
        value={profileMedia}
        onChange={setProfileMedia}
        onNotice={notify}
        onError={fail}
        t={t}
      />
    </section>
  )
}
