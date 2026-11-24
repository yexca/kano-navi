import { useEffect, useState } from "react"
import {
  Activity,
  KeyRound,
  Pencil,
  Plus,
  Save,
  Send,
  TriangleAlert,
  X,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type {
  AlertChannel,
  AlertChannelInput,
  AlertChannelType,
  AlertSettings,
  AlertTestResult,
} from "@/admin/types"
import {
  Badge,
  Button,
  ConfirmButton,
  Empty,
  Field,
  IconButton,
  Modal,
  Panel,
  Switch,
} from "@/admin/ui"
import { useAdmin } from "@/admin/use-admin-data"
import { cn } from "@/lib/utils"

/** A save keeps every stored secret unless the channel is the one edited. */
export function inputOf(channel: AlertChannel): AlertChannelInput {
  return {
    id: channel.id,
    name: channel.name,
    type: channel.type,
    enabled: channel.enabled,
    server: channel.server,
    topic: channel.topic,
    chatId: channel.chatId,
    link: channel.link,
  }
}

export function destination(channel: AlertChannel) {
  switch (channel.type) {
    case "ntfy":
      return `${channel.server.replace(/^https:\/\//u, "")}/${channel.topic}`
    case "telegram":
      return channel.chatId
    default:
      return channel.urlHost
  }
}

export interface ChannelDraft extends Omit<
  AlertChannelInput,
  "secret" | "url"
> {
  url: string
  secret: string
  clearSecret: boolean
}

export const emptyDraft: ChannelDraft = {
  name: "",
  type: "ntfy",
  enabled: true,
  server: "",
  topic: "",
  chatId: "",
  url: "",
  link: true,
  secret: "",
  clearSecret: false,
}

export function ChannelEditor({
  channel,
  types,
  open,
  onClose,
  onSave,
}: {
  channel: AlertChannel | null
  types: AlertChannelType[]
  open: boolean
  onClose: () => void
  onSave: (input: AlertChannelInput) => Promise<boolean>
}) {
  const { t } = useAppSettings()
  const [draft, setDraft] = useState<ChannelDraft>(emptyDraft)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!open) return
    setDraft(
      channel
        ? { ...emptyDraft, ...inputOf(channel), secret: "", clearSecret: false }
        : emptyDraft,
    )
  }, [open, channel])
  const set = (patch: Partial<ChannelDraft>) =>
    setDraft((current) => ({ ...current, ...patch }))
  const stored = Boolean(channel?.hasSecret) && channel?.type === draft.type
  const storedUrl =
    channel?.type === "webhook" && draft.type === "webhook" && !!channel.urlHost
  const required = draft.type === "telegram" && !stored
  const ready =
    draft.type === "ntfy"
      ? Boolean(draft.topic.trim())
      : draft.type === "telegram"
        ? Boolean(draft.chatId.trim()) && (stored || Boolean(draft.secret))
        : storedUrl || Boolean(draft.url.trim())
  const submit = async () => {
    const { secret, clearSecret, url, ...rest } = draft
    const input: AlertChannelInput = {
      ...rest,
      id: channel?.id,
      name: rest.name.trim(),
      server: rest.server.trim(),
      topic: rest.topic.trim(),
      chatId: rest.chatId.trim(),
    }
    if (draft.type === "webhook" && url.trim()) input.url = url.trim()
    if (secret) input.secret = secret
    else if (
      clearSecret ||
      // A new address cannot keep a secret that no longer decrypts.
      (channel?.secretUnreadable && input.url !== undefined) ||
      (channel && channel.type !== draft.type)
    )
      input.secret = ""
    setBusy(true)
    const saved = await onSave(input)
    setBusy(false)
    if (saved) onClose()
  }
  const secretLabel = t(`admin.alerts.secret.${draft.type}`)
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={t(
        channel ? "admin.alerts.editChannel" : "admin.alerts.addChannel",
      )}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("admin.action.cancel")}
          </Button>
          <Button
            type="submit"
            form="adm-alert-channel-form"
            variant="primary"
            icon={Save}
            busy={busy}
            disabled={!ready}
          >
            {t("admin.action.save")}
          </Button>
        </>
      }
    >
      <form
        id="adm-alert-channel-form"
        className="adm-form"
        onSubmit={(event) => {
          event.preventDefault()
          void submit()
        }}
      >
        <div className="adm-form-row">
          <Field label={t("admin.alerts.type")}>
            <select
              value={draft.type}
              onChange={(event) =>
                set({
                  type: event.target.value as AlertChannelType,
                  secret: "",
                  clearSecret: false,
                })
              }
            >
              {types.map((type) => (
                <option key={type} value={type}>
                  {t(`admin.alerts.channelType.${type}`)}
                </option>
              ))}
            </select>
          </Field>
          <Field label={t("admin.alerts.name")}>
            <input
              value={draft.name}
              maxLength={64}
              onChange={(event) => set({ name: event.target.value })}
            />
          </Field>
        </div>
        {draft.type === "ntfy" ? (
          <div className="adm-form-row">
            <Field label={t("admin.alerts.server")}>
              <input
                value={draft.server}
                type="url"
                autoComplete="off"
                spellCheck={false}
                placeholder="https://ntfy.sh"
                onChange={(event) => set({ server: event.target.value })}
              />
            </Field>
            <Field label={t("admin.alerts.topic")}>
              <input
                value={draft.topic}
                required
                maxLength={64}
                autoComplete="off"
                spellCheck={false}
                onChange={(event) => set({ topic: event.target.value })}
              />
            </Field>
          </div>
        ) : null}
        {draft.type === "telegram" ? (
          <Field label={t("admin.alerts.chatId")}>
            <input
              value={draft.chatId}
              required
              autoComplete="off"
              spellCheck={false}
              placeholder="-1001234567890"
              onChange={(event) => set({ chatId: event.target.value })}
            />
          </Field>
        ) : null}
        {draft.type === "webhook" ? (
          <Field
            label={t("admin.alerts.url")}
            hint={storedUrl ? t("admin.alerts.secretKeepHint") : undefined}
          >
            <input
              value={draft.url}
              required={!storedUrl}
              type="url"
              autoComplete="off"
              spellCheck={false}
              placeholder={
                storedUrl ? `https://${channel?.urlHost}/••••` : "https://"
              }
              onChange={(event) => set({ url: event.target.value })}
            />
          </Field>
        ) : null}
        <Field
          label={secretLabel}
          hint={
            stored && !draft.clearSecret
              ? t(
                  channel?.secretUnreadable
                    ? "admin.alerts.secretUnreadableHint"
                    : "admin.alerts.secretKeepHint",
                )
              : undefined
          }
        >
          <input
            type="password"
            value={draft.secret}
            required={required}
            autoComplete="new-password"
            placeholder={stored && !draft.clearSecret ? "••••••••" : undefined}
            onChange={(event) =>
              set({ secret: event.target.value, clearSecret: false })
            }
          />
        </Field>
        {stored && draft.type !== "telegram" ? (
          <Switch
            checked={draft.clearSecret}
            onChange={(clearSecret) => set({ clearSecret, secret: "" })}
            label={t("admin.alerts.clearSecret")}
          />
        ) : null}
        <Switch
          checked={draft.link}
          onChange={(link) => set({ link })}
          label={t("admin.alerts.link")}
        />
        <Switch
          checked={draft.enabled}
          onChange={(enabled) => set({ enabled })}
          label={t("admin.alerts.enabled")}
        />
      </form>
    </Modal>
  )
}

export function testSummary(
  result: AlertTestResult,
  t: (key: string, values?: Record<string, any>) => string,
) {
  return result.ok
    ? t("admin.alerts.testOk", { ms: result.latencyMs })
    : t(`admin.alerts.error.${result.error ?? "failed"}`)
}

export function ChannelsPanel({
  settings,
  save,
  onTested,
}: {
  settings: AlertSettings
  save: (channels: AlertChannelInput[], success?: string) => Promise<boolean>
  onTested: () => void
}) {
  const { t } = useAppSettings()
  const { api, notify, fail } = useAdmin()
  const [editing, setEditing] = useState<AlertChannel | null>(null)
  const [adding, setAdding] = useState(false)
  const [testing, setTesting] = useState<string | null>(null)
  const [results, setResults] = useState<
    Record<string, { ok: boolean; text: string }>
  >({})
  const channels = settings.channels
  const inputs = channels.map(inputOf)
  const test = async (channel: AlertChannel) => {
    setTesting(channel.id)
    try {
      const result = await api.testAlertChannel(channel.id)
      setResults((current) => ({
        ...current,
        [channel.id]: { ok: result.ok, text: testSummary(result, t) },
      }))
      if (result.ok)
        notify("admin.notice.alertTestSent", {
          name: channel.name || t(`admin.alerts.channelType.${channel.type}`),
        })
      onTested()
    } catch (error) {
      fail(error)
    } finally {
      setTesting(null)
    }
  }
  const editorChannel = adding ? null : editing
  return (
    <Panel
      icon={Send}
      title={t("admin.alerts.channels")}
      actions={
        <Button
          size="sm"
          variant="primary"
          icon={Plus}
          disabled={channels.length >= 8}
          onClick={() => setAdding(true)}
        >
          {t("admin.alerts.addChannel")}
        </Button>
      }
    >
      {channels.length ? (
        <ul className="adm-alert-list">
          {channels.map((channel, index) => {
            const result = results[channel.id]
            return (
              <li
                key={channel.id}
                className={cn(
                  "adm-alert-row",
                  !channel.enabled && "is-off",
                  channel.secretUnreadable && "has-issue",
                )}
              >
                <span className="adm-alert-main">
                  <strong>
                    {channel.name ||
                      t(`admin.alerts.channelType.${channel.type}`)}
                  </strong>
                  <small className="adm-alert-mono">
                    {destination(channel)}
                  </small>
                  <span className="adm-chip-row">
                    <Badge>
                      {t(`admin.alerts.channelType.${channel.type}`)}
                    </Badge>
                    {!channel.enabled ? (
                      <Badge>{t("admin.alerts.off")}</Badge>
                    ) : null}
                    {channel.secretUnreadable ? (
                      <Badge tone="danger" icon={TriangleAlert}>
                        {t("admin.alerts.secretUnreadable")}
                      </Badge>
                    ) : channel.hasSecret ? (
                      <Badge icon={KeyRound}>
                        {t(`admin.alerts.secret.${channel.type}`)}
                      </Badge>
                    ) : null}
                    {result ? (
                      <Badge tone={result.ok ? "success" : "danger"}>
                        {result.text}
                      </Badge>
                    ) : null}
                  </span>
                </span>
                <span className="adm-row-actions">
                  <IconButton
                    icon={Activity}
                    label={t("admin.alerts.test")}
                    busy={testing === channel.id}
                    disabled={channel.secretUnreadable || testing !== null}
                    onClick={() => void test(channel)}
                  />
                  <IconButton
                    icon={Pencil}
                    label={t("admin.alerts.edit")}
                    onClick={() => setEditing(channel)}
                  />
                  <ConfirmButton
                    size="sm"
                    variant="ghost"
                    icon={X}
                    aria-label={t("admin.alerts.remove")}
                    title={t("admin.alerts.remove")}
                    onConfirm={() =>
                      void save(
                        inputs.filter((_, at) => at !== index),
                        "admin.notice.alertChannelRemoved",
                      )
                    }
                  />
                </span>
              </li>
            )
          })}
        </ul>
      ) : (
        <Empty title={t("admin.alerts.noChannels")} />
      )}
      <ChannelEditor
        open={adding || editing !== null}
        channel={editorChannel}
        types={settings.channelTypes}
        onClose={() => {
          setAdding(false)
          setEditing(null)
        }}
        onSave={(input) =>
          save(
            editorChannel
              ? inputs.map((item) => (item.id === input.id ? input : item))
              : [...inputs, input],
            "admin.notice.alertChannelSaved",
          )
        }
      />
    </Panel>
  )
}
