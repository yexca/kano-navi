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
import { Badge, Button, Modal, type Tone } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { cn } from "@/lib/utils"

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

/**
 * One button per owner: a chip for each target locale, coloured by where its
 * text comes from, plus the machine step's queued or failed state. It opens
 * the editor on the locale that was clicked.
 */
export function TranslationStatus({
  translation,
  onOpen,
}: {
  translation: AdminTranslation | null | undefined
  onOpen?: (locale: TranslationLocale) => void
}) {
  const { t } = useAppSettings()
  if (!translation) return null
  return (
    <span className="adm-translation-status">
      <span
        className="adm-translation-pill"
        role={onOpen ? "group" : undefined}
        aria-label={onOpen ? t("admin.translation.edit") : undefined}
      >
        {translation.locales.map((entry) => {
          const label = t(
            entry.status === "translated"
              ? originKey(entry)
              : `admin.translation.status.${entry.status}`,
          )
          const title = [
            `${t(`admin.translation.locale.${entry.locale}`)} · ${label}`,
            entry.manualStale ? t("admin.translation.manualStale") : "",
            entry.text || "",
          ]
            .filter(Boolean)
            .join("\n")
          const content = (
            <>
              <i className="adm-dot" aria-hidden />
              <b>{localeShort[entry.locale]}</b>
              <span>{label}</span>
              {entry.manualStale ? <TriangleAlert aria-hidden /> : null}
            </>
          )
          return onOpen ? (
            <button
              key={entry.locale}
              type="button"
              className={cn(`adm-tone-${localeTone(entry)}`)}
              title={title}
              aria-label={`${t("admin.translation.edit")}: ${title}`}
              onClick={() => onOpen(entry.locale)}
            >
              {content}
            </button>
          ) : (
            <span
              key={entry.locale}
              className={cn(`adm-tone-${localeTone(entry)}`)}
              title={title}
            >
              {content}
            </span>
          )
        })}
      </span>
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
        {/* The selected tab already names the locale on screen. */}
        <label htmlFor={fieldId} className="adm-visually-hidden">
          {t(`admin.translation.locale.${entry.locale}`)}
        </label>
        <Badge tone={localeTone(entry)}>
          {t(
            entry.status === "translated"
              ? originKey(entry)
              : `admin.translation.status.${entry.status}`,
          )}
        </Badge>
        {entry.origin === "machine" && entry.machineAt ? (
          <small className="adm-muted">
            {t("admin.translation.machineBy", {
              model: entry.model || "—",
              time: formatRelative(entry.machineAt, locale),
            })}
          </small>
        ) : null}
      </header>
      {entry.manualStale ? (
        <p className="adm-inline-error">
          <TriangleAlert aria-hidden />
          {t("admin.translation.manualStale")}
        </p>
      ) : null}
      <textarea
        id={fieldId}
        rows={5}
        lang={entry.locale}
        value={draft}
        placeholder={t("admin.translation.placeholder")}
        onChange={(event) => setDraft(event.target.value)}
      />
      {entry.machine && entry.origin !== "machine" ? (
        <figure className="adm-translation-reference">
          <figcaption>{t("admin.translation.machineReference")}</figcaption>
          <p>{entry.machine}</p>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setDraft(entry.machine || "")}
          >
            {t("admin.translation.useMachine")}
          </Button>
        </figure>
      ) : null}
      <div className="adm-translation-actions">
        {entry.manual !== null ? (
          <Button
            size="sm"
            variant="ghost"
            icon={X}
            busy={busy === "clear"}
            onClick={() =>
              run("clear", () => api.clearTranslation(owner, id, entry.locale))
            }
          >
            {t("admin.translation.clearManual")}
          </Button>
        ) : null}
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
      </div>
    </section>
  )
}

/**
 * Edit an owner's manual translations and queue a fresh machine pass: the
 * original beside one locale at a time, picked from tabs.
 */
export function TranslationEditor({
  owner,
  id,
  original,
  translation,
  initialLocale,
  onClose,
  onChanged,
}: {
  owner: TranslationOwner
  id: string
  original: string
  translation: AdminTranslation
  initialLocale?: TranslationLocale
  onClose: () => void
  onChanged: (translation: AdminTranslation) => void
}) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const [current, setCurrent] = useState(translation)
  const [active, setActive] = useState<TranslationLocale>(
    initialLocale || translation.locales[0]?.locale || "en",
  )
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
  const entry =
    current.locales.find((item) => item.locale === active) || current.locales[0]
  return (
    <Modal
      open
      wide
      className="adm-translation-editor"
      title={t("admin.translation.title")}
      onClose={onClose}
      footer={
        <>
          <span className="adm-muted">
            {current.requested
              ? t("admin.translation.requestedHint")
              : t("admin.translation.precedence")}
          </span>
          <Button
            icon={RotateCcw}
            busy={busy}
            disabled={current.requested}
            onClick={retranslate}
          >
            {t("admin.translation.retranslate")}
          </Button>
        </>
      }
    >
      <div className="adm-translation-layout">
        <figure className="adm-translation-original">
          <figcaption>
            {t("admin.translation.original")}
            {current.sourceLanguage
              ? ` · ${t(`admin.translation.language.${current.sourceLanguage}`)}`
              : ""}
          </figcaption>
          <blockquote>{original}</blockquote>
        </figure>
        <div className="adm-translation-side">
          <div
            className="adm-translation-tabs"
            role="tablist"
            aria-label={t("admin.translation.title")}
          >
            {current.locales.map((item) => (
              <button
                key={item.locale}
                type="button"
                role="tab"
                aria-selected={item.locale === entry?.locale}
                className={cn(
                  `adm-tone-${localeTone(item)}`,
                  item.locale === entry?.locale && "is-active",
                )}
                onClick={() => setActive(item.locale)}
              >
                <i className="adm-dot" aria-hidden />
                {t(`admin.translation.locale.${item.locale}`)}
                {item.manualStale ? <TriangleAlert aria-hidden /> : null}
              </button>
            ))}
          </div>
          {entry ? (
            <LocaleEditor
              key={entry.locale}
              owner={owner}
              id={id}
              entry={entry}
              onChanged={changed}
            />
          ) : null}
        </div>
      </div>
    </Modal>
  )
}

/**
 * The translation chips of a post or video; each opens the editor on its
 * locale. Videos use it the same way with owner "video" and the title.
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
  const [editing, setEditing] = useState<TranslationLocale | null>(null)
  const [local, setLocal] = useState(translation)
  useEffect(() => setLocal(translation), [translation])
  if (!local) return null
  return (
    <span className="adm-translation-cell">
      <TranslationStatus translation={local} onOpen={setEditing} />
      {editing ? (
        <TranslationEditor
          owner={owner}
          id={id}
          original={original}
          translation={local}
          initialLocale={editing}
          onChanged={setLocal}
          onClose={() => {
            setEditing(null)
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
