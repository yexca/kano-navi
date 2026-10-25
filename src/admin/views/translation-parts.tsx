import { useEffect, useState } from "react"
import { Languages, RotateCcw, Save, TriangleAlert, X } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { formatRelative } from "@/admin/format"
import type {
  AdminTranslation,
  AdminTranslationLocale,
  TranslationLocale,
  TranslationOwner,
} from "@/admin/types"
import { Badge, Button, IconButton, Modal, type Tone } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"

// Translations of X post text and YouTube video titles: readers see a manual
// translation, then a platform's, then a model's. Posts and videos share
// these parts; the owner type names which one an ID belongs to.

const localeShort: Record<TranslationLocale, string> = {
  en: "EN",
  "zh-CN": "中",
}

function originKey(entry: AdminTranslationLocale) {
  if (entry.origin === "platform")
    return `admin.translation.platform.${entry.platform === "bilibili" ? "bilibili" : "youtube"}`
  return `admin.translation.origin.${entry.origin}`
}

function localeTone(entry: AdminTranslationLocale): Tone {
  switch (entry.status) {
    case "translated":
      return entry.origin === "manual"
        ? "success"
        : entry.origin === "platform"
          ? "sky"
          : "lavender"
    case "missing":
    case "stale":
      return "warning"
  }
  return "neutral"
}

/** One chip per target locale, plus the machine step's pending or failed state. */
export function TranslationStatus({
  translation,
}: {
  translation: AdminTranslation | null | undefined
}) {
  const { t } = useAppSettings()
  if (!translation) return null
  return (
    <span className="adm-chip-row adm-translation-status">
      {translation.locales.map((entry) => (
        <Badge
          key={entry.locale}
          tone={localeTone(entry)}
          icon={entry.manualStale ? TriangleAlert : undefined}
          className="adm-translation-chip"
        >
          <span
            title={
              entry.manualStale
                ? t("admin.translation.manualStale")
                : entry.text || undefined
            }
          >
            {localeShort[entry.locale]} ·{" "}
            {t(
              entry.status === "translated"
                ? originKey(entry)
                : `admin.translation.status.${entry.status}`,
            )}
          </span>
        </Badge>
      ))}
      {translation.requested ? (
        <Badge tone="honey">{t("admin.translation.requested")}</Badge>
      ) : null}
      {translation.machineStatus === "failed" ? (
        <Badge tone="danger">
          <span title={translation.lastError || undefined}>
            {t("admin.translation.failed")}
          </span>
        </Badge>
      ) : null}
    </span>
  )
}

function LocaleEditor({
  owner,
  id,
  entry,
  onChanged,
}: {
  owner: TranslationOwner
  id: string
  entry: AdminTranslationLocale
  onChanged: (translation: AdminTranslation) => void
}) {
  const { t, locale } = useAppSettings()
  const { api, act } = useAdmin()
  const [draft, setDraft] = useState(entry.manual ?? entry.text ?? "")
  const [busy, setBusy] = useState("")
  useEffect(
    () => setDraft(entry.manual ?? entry.text ?? ""),
    [entry.manual, entry.text],
  )
  const run = async (
    kind: "save" | "clear",
    work: () => Promise<{ translation: AdminTranslation }>,
  ) => {
    setBusy(kind)
    const result = await act(
      work,
      kind === "save"
        ? "admin.notice.translationSaved"
        : "admin.notice.translationCleared",
    )
    setBusy("")
    if (result) onChanged(result.translation)
  }
  const fieldId = `translation-${owner}-${id}-${entry.locale}`
  return (
    <section className="adm-translation-locale">
      <header>
        <label htmlFor={fieldId}>
          {t(`admin.translation.locale.${entry.locale}`)}
        </label>
        <Badge tone={localeTone(entry)}>
          {t(
            entry.status === "translated"
              ? originKey(entry)
              : `admin.translation.status.${entry.status}`,
          )}
        </Badge>
      </header>
      {entry.machine && entry.origin !== "machine" ? (
        <p className="adm-translation-reference">
          <span>{t("admin.translation.origin.machine")}</span>
          {entry.machine}
        </p>
      ) : null}
      {entry.origin === "machine" && entry.machineAt ? (
        <p className="adm-field-hint">
          {t("admin.translation.machineBy", {
            model: entry.model || "—",
            time: formatRelative(entry.machineAt, locale),
          })}
        </p>
      ) : null}
      {entry.manualStale ? (
        <p className="adm-inline-error">
          <TriangleAlert aria-hidden />
          {t("admin.translation.manualStale")}
        </p>
      ) : null}
      <textarea
        id={fieldId}
        rows={3}
        lang={entry.locale}
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
      />
      <div className="adm-row-actions">
        <Button
          size="sm"
          variant="primary"
          icon={Save}
          busy={busy === "save"}
          disabled={!draft.trim() || draft.trim() === entry.manual}
          onClick={() =>
            run("save", () =>
              api.setTranslation(owner, id, entry.locale, draft),
            )
          }
        >
          {t("admin.translation.saveManual")}
        </Button>
        {entry.manual !== null ? (
          <Button
            size="sm"
            icon={X}
            busy={busy === "clear"}
            onClick={() =>
              run("clear", () => api.clearTranslation(owner, id, entry.locale))
            }
          >
            {t("admin.translation.clearManual")}
          </Button>
        ) : null}
      </div>
    </section>
  )
}

/** Edit an owner's manual translations and queue a fresh machine pass. */
export function TranslationEditor({
  owner,
  id,
  original,
  translation,
  onClose,
  onChanged,
}: {
  owner: TranslationOwner
  id: string
  original: string
  translation: AdminTranslation
  onClose: () => void
  onChanged: (translation: AdminTranslation) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [current, setCurrent] = useState(translation)
  const [busy, setBusy] = useState(false)
  const changed = (next: AdminTranslation) => {
    setCurrent(next)
    onChanged(next)
  }
  const retranslate = async () => {
    setBusy(true)
    const result = await act(
      () => api.requestTranslations({ ownerType: owner, ids: [id] }),
      "admin.notice.translationQueued",
    )
    setBusy(false)
    if (result) changed({ ...current, requested: true })
  }
  return (
    <Modal
      open
      wide
      className="adm-translation-editor"
      title={t("admin.translation.title")}
      onClose={onClose}
      footer={
        <Button icon={RotateCcw} busy={busy} onClick={retranslate}>
          {t("admin.translation.retranslate")}
        </Button>
      }
    >
      <figure className="adm-translation-original">
        <figcaption>
          {t("admin.translation.original")}
          {current.sourceLanguage
            ? ` · ${t(`admin.translation.language.${current.sourceLanguage}`)}`
            : ""}
        </figcaption>
        <blockquote>{original}</blockquote>
      </figure>
      <p className="adm-field-hint">{t("admin.translation.precedence")}</p>
      {current.locales.map((entry) => (
        <LocaleEditor
          key={entry.locale}
          owner={owner}
          id={id}
          entry={entry}
          onChanged={changed}
        />
      ))}
    </Modal>
  )
}

/**
 * The translation chips of a post or video, with the button that opens its
 * editor. Videos use it the same way with owner "video" and the title.
 */
export function TranslationCell({
  owner,
  id,
  original,
  translation,
  onChanged,
}: {
  owner: TranslationOwner
  id: string
  original: string
  translation: AdminTranslation | null | undefined
  onChanged: () => void
}) {
  const { t } = useAppSettings()
  const [editing, setEditing] = useState(false)
  const [local, setLocal] = useState(translation)
  useEffect(() => setLocal(translation), [translation])
  if (!local) return null
  return (
    <span className="adm-translation-cell">
      <TranslationStatus translation={local} />
      <IconButton
        icon={Languages}
        label={t("admin.translation.edit")}
        onClick={() => setEditing(true)}
      />
      {editing ? (
        <TranslationEditor
          owner={owner}
          id={id}
          original={original}
          translation={local}
          onChanged={setLocal}
          onClose={() => {
            setEditing(false)
            onChanged()
          }}
        />
      ) : null}
    </span>
  )
}

/**
 * How many posts or videos lack a usable translation, and the incremental
 * pass that queues every one of them for the translate step.
 */
export function TranslationBacklog({
  owner,
  onQueued,
}: {
  owner: TranslationOwner
  onQueued: () => void
}) {
  const { t } = useAppSettings()
  const { api, act, notify } = useAdmin()
  const summary = useResource(() => api.translations(), [])
  const [busy, setBusy] = useState(false)
  const counts = summary.data?.counts[owner]
  const request = async () => {
    setBusy(true)
    const result = await act(() =>
      api.requestTranslations({ missing: true, ownerType: owner }),
    )
    setBusy(false)
    if (!result) return
    if (result.queued === 0) notify("admin.notice.translationNothing")
    else
      notify(
        result.busy
          ? "admin.notice.translationWaiting"
          : "admin.notice.translationStarted",
        { count: result.queued },
      )
    void summary.reload()
    onQueued()
  }
  return (
    <span className="adm-translation-backlog">
      {counts ? (
        <span className="adm-muted">
          {t("admin.translation.backlog", {
            incomplete: counts.incomplete,
            total: counts.total,
            requested: counts.requested,
          })}
        </span>
      ) : null}
      <Button
        size="sm"
        icon={Languages}
        busy={busy}
        disabled={!counts || counts.incomplete === 0}
        onClick={request}
      >
        {t("admin.translation.requestMissing")}
      </Button>
    </span>
  )
}
