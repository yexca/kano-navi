import { useState } from "react"
import { KeyRound, Plus, ShieldAlert, Tv } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { formatDateTime } from "@/admin/format"
import type { AdminVideo, Sources } from "@/admin/types"
import { Badge, Button, Facts, Field, Panel } from "@/admin/ui"
import { useAdmin } from "@/admin/use-admin-data"

/**
 * The Bilibili source on the Videos page: how its reads are going, and an
 * operator's way to add one upload by BV ID or link. Uploads stay in the
 * console until they can be matched with their YouTube copy.
 */
export function BilibiliSource({
  status,
  onAdded,
}: {
  status: Sources["bilibili"] | undefined
  onAdded: (video: AdminVideo) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const [input, setInput] = useState("")
  const [busy, setBusy] = useState(false)
  const usable = Boolean(status?.available && !status.skipped)
  const blocked =
    status?.blockedUntil && Date.parse(status.blockedUntil) > Date.now()
  const add = async (submit) => {
    submit.preventDefault()
    if (!input.trim()) return
    setBusy(true)
    const result = await act(
      () => api.addBilibiliVideo(input.trim()),
      "admin.notice.bilibiliAdded",
    )
    setBusy(false)
    if (!result) return
    setInput("")
    onAdded(result.video)
  }
  const when = (value: string | null | undefined) =>
    value ? formatDateTime(value, locale) : t("admin.bilibili.never")
  return (
    <Panel
      icon={Tv}
      title={t("admin.bilibili.title")}
      description={t("admin.bilibili.description")}
      actions={
        status ? (
          <span className="adm-chip-row">
            {!usable ? <Badge>{t("admin.overview.skipped")}</Badge> : null}
            {status.signedIn ? (
              <Badge tone="leaf" icon={KeyRound}>
                {t("admin.bilibili.signedIn")}
              </Badge>
            ) : (
              <Badge icon={KeyRound}>{t("admin.bilibili.anonymous")}</Badge>
            )}
            {blocked ? (
              <Badge tone="danger" icon={ShieldAlert}>
                {t("admin.bilibili.blocked")}
              </Badge>
            ) : null}
          </span>
        ) : null
      }
    >
      {status?.available ? (
        <Facts
          items={[
            [t("admin.bilibili.account"), status.account],
            [t("admin.bilibili.videos"), String(status.videos)],
            [t("admin.bilibili.lastSuccess"), when(status.lastSuccessAt)],
            [
              t("admin.bilibili.interval"),
              t("admin.bilibili.everyMinutes", {
                count: status.minIntervalMinutes,
              }),
            ],
            [
              t("admin.bilibili.backoff"),
              blocked
                ? t("admin.bilibili.blockedUntil", {
                    time: formatDateTime(status.blockedUntil, locale),
                  })
                : t("admin.bilibili.notBlocked"),
            ],
            [t("admin.bilibili.lastError"), status.lastError ?? "—"],
          ]}
        />
      ) : null}
      {!status?.signedIn && status?.available ? (
        <p className="adm-note">{t("admin.bilibili.signInHint")}</p>
      ) : null}
      <form className="adm-bilibili-add" onSubmit={add}>
        <Field
          label={t("admin.bilibili.add")}
          hint={t("admin.bilibili.addHint")}
        >
          <input
            value={input}
            placeholder="BV1xxxxxxxxx"
            maxLength={300}
            disabled={!usable}
            onChange={(e) => setInput(e.target.value)}
          />
        </Field>
        <Button
          type="submit"
          icon={Plus}
          busy={busy}
          disabled={!usable || !input.trim()}
        >
          {t("admin.bilibili.addButton")}
        </Button>
      </form>
    </Panel>
  )
}
