import { useState, type ReactNode, type SubmitEvent } from "react"
import { KeyRound, Plus, RadioTower, ShieldAlert } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { ApiError } from "@/admin/api"
import { formatRelative, japanStamp } from "@/admin/format"
import type { BilibiliLiveRoom, BilibiliLogin, Sources } from "@/admin/types"
import { Badge, Button, Facts, Field, Panel, type Tone } from "@/admin/ui"
import { useAdmin } from "@/admin/use-admin-data"
import { explainCode } from "@/admin/views/run-parts"

const roomStates: Record<number, { key: string; tone: Tone }> = {
  0: { key: "admin.bilibili.live.off", tone: "neutral" },
  1: { key: "admin.bilibili.live.on", tone: "leaf" },
  2: { key: "admin.bilibili.live.rotation", tone: "neutral" },
}

const loginStates: Record<BilibiliLogin, { key: string; tone: Tone }> = {
  signed_in: { key: "admin.bilibili.signedIn", tone: "leaf" },
  expired: { key: "admin.bilibili.loginExpired", tone: "danger" },
  unchecked: { key: "admin.bilibili.loginUnchecked", tone: "honey" },
  not_configured: { key: "admin.bilibili.anonymous", tone: "neutral" },
}

/** A refusal by the local backoff that the server lets one read override. */
function forceable(error: unknown): error is ApiError {
  return (
    error instanceof ApiError &&
    error.code === "bilibili_blocked" &&
    error.force === true
  )
}

/**
 * A single-item Bilibili read (adding an upload or a dynamic, reading an
 * upload again) that the captcha backoff refused can be sent once anyway:
 * `read` runs `send(false)` like any action, and after such a refusal
 * `offer` is a "Try once anyway" button that sends `send(true)` once.
 * Timers and Run now never get this override.
 */
export function useForcedRead() {
  const { t } = useAppSettings()
  const { act } = useAdmin()
  const [pending, setPending] = useState<{
    until?: string
    send: () => Promise<unknown>
  } | null>(null)
  const [busy, setBusy] = useState(false)
  const read = async <T,>(
    send: (force: boolean) => Promise<T>,
    notice: string,
    done: (result: T) => void,
  ) => {
    setPending(null)
    const result = await act(async () => {
      try {
        return await send(false)
      } catch (error) {
        if (forceable(error))
          setPending({
            until: error.blockedUntil,
            send: async () => {
              const forced = await act(() => send(true), notice)
              if (forced) done(forced)
            },
          })
        throw error
      }
    }, notice)
    if (result) done(result)
    return Boolean(result)
  }
  const force = async () => {
    if (!pending) return
    setBusy(true)
    await pending.send()
    setBusy(false)
    setPending(null)
  }
  const offer: ReactNode = pending ? (
    <Button
      size="sm"
      variant="ghost"
      icon={ShieldAlert}
      busy={busy}
      onClick={force}
      title={t("admin.bilibili.forceHint")}
    >
      {t("admin.bilibili.force")}
    </Button>
  ) : null
  return { read, offer }
}

/** One add form of the panel: a link or ID field and its button. */
function AddForm({
  label,
  hint,
  placeholder,
  disabled,
  notice,
  send,
  onAdded,
}: {
  label: string
  hint?: string
  placeholder: string
  disabled: boolean
  notice: string
  send: (input: string, force: boolean) => Promise<unknown>
  onAdded: () => void
}) {
  const { t } = useAppSettings()
  const [input, setInput] = useState("")
  const [busy, setBusy] = useState(false)
  const { read, offer } = useForcedRead()
  const submit = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    const value = input.trim()
    if (!value) return
    setBusy(true)
    await read(
      (force) => send(value, force),
      notice,
      () => {
        setInput("")
        onAdded()
      },
    )
    setBusy(false)
  }
  return (
    <form className="adm-bilibili-add" onSubmit={submit}>
      <Field label={label} hint={hint}>
        <input
          value={input}
          placeholder={placeholder}
          maxLength={300}
          disabled={disabled}
          onChange={(e) => setInput(e.target.value)}
        />
      </Field>
      <Button
        type="submit"
        icon={Plus}
        busy={busy}
        disabled={disabled || !input.trim()}
      >
        {t("admin.bilibili.addButton")}
      </Button>
      {offer}
    </form>
  )
}

/** The live room's last check: its state, title and when it went live. */
export function LiveRoomState({ live }: { live: BilibiliLiveRoom }) {
  const { t } = useAppSettings()
  const state = live.liveStatus == null ? null : roomStates[live.liveStatus]
  return state ? (
    <Badge tone={state.tone} icon={RadioTower}>
      {t(state.key)}
    </Badge>
  ) : null
}

const future = (value: string | null | undefined) =>
  Boolean(value && Date.parse(value) > Date.now())

/** A Japan time with how long ago or until it is, or null. */
function useStamp() {
  const { locale } = useAppSettings()
  return (value: string | null | undefined) =>
    value ? `${japanStamp(value)} · ${formatRelative(value, locale)}` : null
}

/** An error code explained, with the code as its tooltip, or nothing. */
function ErrorText({ code }: { code: string | null | undefined }) {
  const { t } = useAppSettings()
  return code ? <span title={code}>{explainCode(code, t) ?? code}</span> : null
}

/** The live room's last check, as the live-check panel lists it. */
export function LiveRoomFacts({ live }: { live: BilibiliLiveRoom }) {
  const { t } = useAppSettings()
  const stamp = useStamp()
  return (
    <Facts
      items={[
        [
          t("admin.bilibili.live.room"),
          live.roomId ? (
            <a
              className="adm-link"
              href={`https://live.bilibili.com/${encodeURIComponent(live.roomId)}`}
              target="_blank"
              rel="noreferrer"
            >
              {live.roomId}
            </a>
          ) : null,
        ],
        [t("admin.bilibili.live.streamTitle"), live.title],
        [
          t("admin.bilibili.live.liveTime"),
          live.liveTime
            ? stamp(live.liveTime)
            : live.liveStatus === 1
              ? t("admin.bilibili.live.startUnknown")
              : null,
        ],
        [t("admin.bilibili.live.checkedAt"), stamp(live.checkedAt)],
        [t("admin.bilibili.live.lastLiveAt"), stamp(live.lastLiveAt)],
        [
          t("admin.bilibili.live.blockedUntil"),
          future(live.blockedUntil) ? stamp(live.blockedUntil) : null,
        ],
        [
          t("admin.bilibili.lastError"),
          live.lastError ? <ErrorText code={live.lastError} /> : null,
        ],
      ]}
    />
  )
}

/**
 * The login cookie as both reads send it: `signedIn` only says a cookie is
 * configured, `login` is its last check.
 */
function loginOf(status: Sources["bilibili"] | undefined): BilibiliLogin {
  return status?.login ?? (status?.signedIn ? "signed_in" : "not_configured")
}

function LoginBadge({ login }: { login: BilibiliLogin }) {
  const { t } = useAppSettings()
  const state = loginStates[login] ?? loginStates.not_configured
  return (
    <Badge tone={state.tone} icon={KeyRound}>
      {t(state.key)}
    </Badge>
  )
}

/** Whether the operator's single reads can run: the source is on here. */
const usableOf = (status: Sources["bilibili"] | undefined) =>
  Boolean(status?.available && !status.skipped)

/** The login cookie both Bilibili reads send, as a chip of the summary. */
export function BilibiliLoginBadge({
  status,
}: {
  status: Sources["bilibili"] | undefined
}) {
  return status?.available ? <LoginBadge login={loginOf(status)} /> : null
}

/**
 * What a Bilibili read needs of its login cookie, under the summary: how to
 * set one while none is configured (the uploads read anonymously, the
 * dynamics listing not at all) and that an expired one needs replacing.
 */
export function BilibiliLoginNote({
  status,
  dynamics,
}: {
  status: Sources["bilibili"] | undefined
  dynamics: boolean
}) {
  const { t } = useAppSettings()
  if (!status?.available) return null
  const login = loginOf(status)
  const key =
    login === "expired"
      ? "admin.bilibili.loginExpiredHint"
      : login === "not_configured" && dynamics
        ? "admin.bilibili.dynamics.loginHint"
        : null
  return key ? (
    <p className="adm-source-note">
      <KeyRound aria-hidden />
      {t(key)}
    </p>
  ) : null
}

/**
 * The operator's single read beside a source's manual fetch: one form,
 * disabled while the source does not run here. A Bilibili read respects the
 * shared captcha backoff and offers one forced read after a refusal.
 */
export function AddPanel({
  usable,
  title,
  ...form
}: {
  /** The source runs here; otherwise the panel is marked skipped. */
  usable: boolean
  title: string
  label: string
  hint?: string
  placeholder: string
  notice: string
  send: (input: string, force: boolean) => Promise<unknown>
  onAdded: () => void
}) {
  const { t } = useAppSettings()
  return (
    <Panel
      icon={Plus}
      title={title}
      actions={usable ? null : <Badge>{t("admin.overview.skipped")}</Badge>}
    >
      <AddForm {...form} disabled={!usable} />
    </Panel>
  )
}

/** Adds or re-reads one upload of the account by BV ID or video link. */
export function BilibiliAddUpload({
  status,
  onAdded,
}: {
  status: Sources["bilibili"] | undefined
  onAdded: () => void
}) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  return (
    <AddPanel
      usable={usableOf(status)}
      title={t("admin.bilibili.add")}
      label={t("admin.bilibili.addField")}
      placeholder="BV1xxxxxxxxx"
      notice="admin.notice.bilibiliAdded"
      send={(input, force) => api.addBilibiliVideo(input, force)}
      onAdded={onAdded}
    />
  )
}

/** Adds or re-reads one dynamic of the account by its link; needs no login. */
export function BilibiliAddDynamic({
  status,
  onAdded,
}: {
  status: Sources["bilibili"] | undefined
  onAdded: () => void
}) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  return (
    <AddPanel
      usable={usableOf(status)}
      title={t("admin.bilibili.addPost")}
      label={t("admin.bilibili.addPostField")}
      placeholder="https://www.bilibili.com/opus/…"
      notice="admin.notice.bilibiliPostAdded"
      send={(input, force) => api.addBilibiliPost(input, force)}
      onAdded={onAdded}
    />
  )
}
