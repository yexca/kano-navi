import { useEffect, useState } from "react"
import { KeyRound, Link2, Radio, Save } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { adminMessage, jsonBody, request } from "@/admin/api"
import { formatDateTime } from "@/admin/format"
import {
  Btn,
  Card,
  CardHeader,
  Field,
  Switch,
  Tag,
} from "@/admin/components/primitives"

const knownErrors = new Set([
  "websub_hub_timeout",
  "websub_hub_unavailable",
  "websub_hub_http_error",
  "websub_hub_rate_limited",
  "websub_hub_redirect_rejected",
  "websub_invalid_request",
  "websub_hub_denied",
  "websub_verification_missing",
])

function pushState(push) {
  if (!push?.enabled) return { tone: "neutral", key: "admin.push.state.off" }
  if (push.verified)
    return { tone: "success", key: "admin.push.state.verified" }
  if (push.lastError) return { tone: "danger", key: "admin.push.state.failed" }
  return { tone: "running", key: "admin.push.state.waiting" }
}

/**
 * Operator settings for the YouTube WebSub subscription. The server owns the
 * signing secret; the console only learns whether one exists.
 */
export function YoutubePushCard({ data }: { data: any }) {
  const { t, locale } = useAppSettings()
  const push = data.workflowState?.youtubePush
  const [enabled, setEnabled] = useState(false)
  const [callbackUrl, setCallbackUrl] = useState("")
  const [busy, setBusy] = useState("")
  useEffect(() => {
    setEnabled(Boolean(push?.enabled))
    setCallbackUrl(push?.callbackUrl || "")
  }, [push?.enabled, push?.callbackUrl])

  if (!push) return null
  const callbackPath = push.callbackPath || "/api/websub/youtube"
  const suggested = `${window.location.origin}${callbackPath}`
  const unavailable = push.available === false
  const dirty =
    enabled !== Boolean(push.enabled) ||
    callbackUrl.trim() !== (push.callbackUrl || "")
  const state = pushState(push)
  const time = (value) =>
    value ? formatDateTime(value, locale) : t("admin.push.never")

  const run = async (kind, path, options) => {
    setBusy(kind)
    try {
      await request(path, options)
      await data.loadWorkflows()
      data.notify(
        adminMessage(
          kind === "rotate" ? "admin.push.rotated" : "admin.push.saved",
        ),
      )
    } catch (error) {
      data.fail(error)
    } finally {
      setBusy("")
    }
  }
  const save = (event) => {
    event.preventDefault()
    void run("save", "/youtube-push", {
      method: "PUT",
      body: jsonBody({ enabled, callbackUrl: callbackUrl.trim() || null }),
    })
  }
  const rotate = () => {
    if (!window.confirm(t("admin.push.rotateConfirm"))) return
    void run("rotate", "/youtube-push/rotate-secret", { method: "POST" })
  }

  return (
    <Card>
      <CardHeader
        icon={Radio}
        title={t("admin.push.title")}
        description={t("admin.push.description")}
        actions={<Tag tone={state.tone}>{t(state.key)}</Tag>}
      />
      <form className="adm-form" onSubmit={save}>
        <div className="adm-push-toggle is-wide">
          <Switch
            checked={enabled}
            onChange={setEnabled}
            label={t("admin.push.enabled")}
            disabled={unavailable}
          />
          <span>{t("admin.push.enabled")}</span>
        </div>
        <Field
          label={t("admin.push.callback")}
          hint={t("admin.push.callbackHint", { path: callbackPath })}
          wide
        >
          <input
            type="url"
            inputMode="url"
            autoComplete="off"
            spellCheck={false}
            placeholder={suggested}
            value={callbackUrl}
            required={enabled}
            disabled={unavailable}
            onChange={(event) => setCallbackUrl(event.target.value)}
          />
        </Field>
        {unavailable ? (
          <p className="adm-muted is-wide">{t("admin.push.skipped")}</p>
        ) : null}
        {push.lastError ? (
          <p className="adm-inline-error is-wide" role="status">
            {t(
              knownErrors.has(push.lastError)
                ? `admin.push.error.${push.lastError}`
                : "admin.push.error.other",
            )}
          </p>
        ) : null}
        <dl className="adm-push-facts is-wide">
          <div>
            <dt>{t("admin.push.secret")}</dt>
            <dd>
              {t(
                push.secretConfigured
                  ? "admin.push.secretReady"
                  : "admin.push.secretMissing",
              )}
            </dd>
          </div>
          <div>
            <dt>{t("admin.push.lease")}</dt>
            <dd>{time(push.leaseExpiresAt)}</dd>
          </div>
          <div>
            <dt>{t("admin.push.renewal")}</dt>
            <dd>{time(push.nextRenewalAt)}</dd>
          </div>
          <div>
            <dt>{t("admin.push.notification")}</dt>
            <dd>
              {time(push.lastNotificationAt)}
              {push.pending ? ` · ${t("admin.push.pending")}` : ""}
            </dd>
          </div>
          <div>
            <dt>{t("admin.push.triggered")}</dt>
            <dd>{time(push.lastTriggeredAt)}</dd>
          </div>
        </dl>
        <p className="adm-muted is-wide">{t("admin.push.footnote")}</p>
        <div className="adm-form-actions is-wide">
          {!callbackUrl && !unavailable ? (
            <Btn
              variant="outline"
              icon={Link2}
              onClick={() => setCallbackUrl(suggested)}
            >
              {t("admin.push.useOrigin")}
            </Btn>
          ) : null}
          {push.secretConfigured ? (
            <Btn
              variant="outline"
              icon={KeyRound}
              busy={busy === "rotate"}
              disabled={Boolean(busy) || unavailable}
              onClick={rotate}
            >
              {t("admin.push.rotate")}
            </Btn>
          ) : null}
          <Btn
            type="submit"
            variant="primary"
            icon={Save}
            busy={busy === "save"}
            disabled={!dirty || Boolean(busy) || unavailable}
          >
            {t("admin.action.save")}
          </Btn>
        </div>
      </form>
    </Card>
  )
}
