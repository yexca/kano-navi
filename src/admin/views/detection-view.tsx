import { useEffect, useMemo, useState } from "react"
import {
  ChevronDown,
  ChevronUp,
  Image as ImageIcon,
  MessageSquareText,
  Plus,
  Route,
  Save,
  ScanSearch,
  Tags,
  Trash2,
  TriangleAlert,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { adminMessage, jsonBody, request } from "@/admin/api"
import { describeTarget, modelTags } from "@/admin/llm"
import {
  Btn,
  Card,
  CardHeader,
  EmptyState,
  IconBtn,
  PageHeader,
  Switch,
  Tag,
} from "@/admin/components/primitives"
import { cn } from "@/lib/utils"

const targetSeparator = "\u0000"

function TargetTags({ tags, t }: { tags: any; t: any }) {
  return (
    <span className="adm-tag-row">
      {modelTags
        .filter((tag) => tags.includes(tag.id))
        .map((tag) => {
          const Icon = tag.icon
          return (
            <Tag key={tag.id} tone={tag.tone}>
              <Icon aria-hidden="true" />
              {t(`admin.model.tag.${tag.id}`)}
            </Tag>
          )
        })}
    </span>
  )
}

/**
 * Ordered failover list for one extraction route. Each entry is a provider
 * plus a catalog model; the server retries a target three times before moving
 * to the next compatible one.
 */
function RouteEditor({
  route,
  config,
  onSaved,
  onError,
  t,
  needsImage = false,
}: {
  route: any
  config: any
  onSaved: any
  onError: any
  t: any
  needsImage?: any
}) {
  const providers = config?.providers || []
  const models = config?.models || []
  const configured = config?.configuredRouteTargets?.[route] || []
  const effective = config?.routeTargets?.[route] || []
  const usesFallback = !configured.length && route !== "schedule_vision"
  const [choice, setChoice] = useState("")
  const [busy, setBusy] = useState(false)

  const options = useMemo(
    () =>
      providers
        .map((provider) => ({
          provider,
          models: models.filter(
            (model) =>
              model.providerId === provider.id &&
              model.capabilities.length > 0 &&
              !configured.some(
                (target) =>
                  target.providerId === provider.id &&
                  target.modelId === model.id,
              ),
          ),
        }))
        .filter((group) => group.models.length),
    [configured, models, providers],
  )

  const save = async (targets) => {
    setBusy(true)
    try {
      const payload = await request(`/routes/${route}`, {
        method: "PUT",
        body: jsonBody({ targets }),
      })
      await onSaved(payload)
    } catch (error) {
      onError(error)
    } finally {
      setBusy(false)
    }
  }

  const add = (event) => {
    event.preventDefault()
    if (!choice) return
    const [providerId, modelId] = choice.split(targetSeparator)
    setChoice("")
    save([...configured, { providerId, modelId }])
  }

  const move = (index, direction) => {
    const next = [...configured]
    const target = index + direction
    if (target < 0 || target >= next.length) return
    ;[next[index], next[target]] = [next[target], next[index]]
    save(next)
  }

  const list = usesFallback ? effective : configured
  const described = list.map((target) => ({
    target,
    ...describeTarget(target, providers, models),
  }))
  const usable = described.filter((item) => !item.issue)
  const imageReady = usable.some((item) => item.capabilities.includes("image"))

  return (
    <div className="adm-route">
      {usesFallback ? (
        <p className="adm-callout">
          <Route aria-hidden="true" />
          {t("admin.route.usesFallback")}
        </p>
      ) : null}
      {described.length ? (
        <ol className={cn("adm-route-list", usesFallback && "is-fallback")}>
          {described.map((item, index) => (
            <li
              key={`${item.target.providerId}-${item.target.modelId}`}
              className={cn("adm-route-row", item.issue && "has-issue")}
            >
              <span className="adm-route-rank">{index + 1}</span>
              <div className="adm-route-copy">
                <strong>
                  {item.provider?.name || item.target.providerId}
                  <span aria-hidden="true"> / </span>
                  <code>{item.modelId || "—"}</code>
                </strong>
                <span className="adm-route-meta">
                  <TargetTags tags={item.tags} t={t} />
                  {item.followsDefault ? (
                    <Tag tone="neutral">{t("admin.route.followsDefault")}</Tag>
                  ) : null}
                  {item.issue ? (
                    <span className="adm-route-issue">
                      <TriangleAlert aria-hidden="true" />
                      {t(`admin.route.issue.${item.issue}`)}
                    </span>
                  ) : null}
                </span>
              </div>
              {usesFallback ? null : (
                <div className="adm-route-actions">
                  <IconBtn
                    label={t("admin.action.moveUp")}
                    icon={ChevronUp}
                    disabled={busy || index === 0}
                    onClick={() => move(index, -1)}
                  />
                  <IconBtn
                    label={t("admin.action.moveDown")}
                    icon={ChevronDown}
                    disabled={busy || index === described.length - 1}
                    onClick={() => move(index, 1)}
                  />
                  <IconBtn
                    label={t("admin.action.remove")}
                    icon={Trash2}
                    disabled={busy}
                    onClick={() =>
                      save(
                        configured.filter((_, position) => position !== index),
                      )
                    }
                  />
                </div>
              )}
            </li>
          ))}
        </ol>
      ) : (
        <EmptyState icon={Route} title={t("admin.route.empty")}>
          {t("admin.route.emptyHint")}
        </EmptyState>
      )}
      {needsImage && usable.length > 0 && !imageReady ? (
        <p className="adm-callout is-warning">
          <TriangleAlert aria-hidden="true" />
          {t("admin.route.needsImage")}
        </p>
      ) : null}
      <form className="adm-route-add" onSubmit={add}>
        <select
          value={choice}
          onChange={(event) => setChoice(event.target.value)}
          aria-label={t("admin.route.addLabel")}
          disabled={busy || !options.length}
        >
          <option value="">
            {options.length
              ? t("admin.route.choose")
              : t("admin.route.noModels")}
          </option>
          {options.map((group) => (
            <optgroup key={group.provider.id} label={group.provider.name}>
              {group.models.map((model) => (
                <option
                  key={model.id}
                  value={`${group.provider.id}${targetSeparator}${model.id}`}
                >
                  {model.id} ·{" "}
                  {model.capabilities
                    .map((capability) => t(`admin.model.tag.${capability}`))
                    .join(" + ")}
                </option>
              ))}
            </optgroup>
          ))}
        </select>
        <Btn
          type="submit"
          variant="outline"
          icon={Plus}
          busy={busy}
          disabled={!choice}
        >
          {t("admin.route.add")}
        </Btn>
      </form>
    </div>
  )
}

export function DetectionView({
  data,
  onNavigate,
}: {
  data: any
  onNavigate: any
}) {
  const { t } = useAppSettings()
  const { config, setConfig, notify, fail, startJob, activeJob } = data
  const [form, setForm] = useState(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!config) return
    setForm({
      keywordEnabled: config.scheduleKeywordEnabled !== false,
      visionEnabled: config.scheduleVisionEnabled !== false,
      messageEnabled: config.scheduleMessageEnabled !== false,
      keywords: (config.scheduleKeywords || []).join("\n"),
    })
  }, [config])

  if (!form) return null

  const dirty =
    form.keywordEnabled !== (config.scheduleKeywordEnabled !== false) ||
    form.visionEnabled !== (config.scheduleVisionEnabled !== false) ||
    form.messageEnabled !== (config.scheduleMessageEnabled !== false) ||
    form.keywords !== (config.scheduleKeywords || []).join("\n")

  const save = async (event) => {
    event?.preventDefault()
    setSaving(true)
    try {
      const payload = await request("/config", {
        method: "PUT",
        body: jsonBody({
          scheduleKeywordEnabled: form.keywordEnabled,
          scheduleVisionEnabled: form.visionEnabled,
          scheduleMessageEnabled: form.messageEnabled,
          scheduleKeywords: form.keywords,
        }),
      })
      setConfig(payload)
      notify(adminMessage("admin.notice.configSaved"))
    } catch (error) {
      fail(error)
    } finally {
      setSaving(false)
    }
  }

  const routeSaved = async (payload) => {
    setConfig(payload)
    notify(adminMessage("admin.route.saved"))
  }

  const setFlag = (name) => (value) =>
    setForm((current) => ({ ...current, [name]: value }))
  const anyStage =
    form.messageEnabled || (form.keywordEnabled && form.visionEnabled)

  return (
    <section className="adm-view" aria-labelledby="adm-detection-title">
      <PageHeader
        id="adm-detection-title"
        title={t("admin.detection.title")}
        description={t("admin.detection.description")}
        actions={
          <>
            <Btn
              variant="outline"
              icon={ScanSearch}
              onClick={() => startJob("/scan/automatic")}
              disabled={Boolean(activeJob) || !anyStage || dirty}
              title={dirty ? t("admin.provider.saveFirst") : undefined}
            >
              {t("admin.detection.runScan")}
            </Btn>
            <Btn
              variant="primary"
              icon={Save}
              busy={saving}
              disabled={!dirty}
              onClick={save}
            >
              {t("admin.detection.save")}
            </Btn>
          </>
        }
      />
      {dirty ? (
        <p className="adm-callout is-info">
          <Save aria-hidden="true" />
          {t("admin.detection.unsaved")}
        </p>
      ) : null}
      <div className="adm-stages">
        <Card className={cn("adm-stage", !form.keywordEnabled && "is-off")}>
          <span className="adm-stage-index">01</span>
          <CardHeader
            icon={Tags}
            title={t("admin.detection.keywordTitle")}
            description={t("admin.detection.keywordDescription")}
            actions={
              <Switch
                checked={form.keywordEnabled}
                onChange={setFlag("keywordEnabled")}
                label={t("admin.detection.keywordTitle")}
              />
            }
          />
          <label className="adm-field">
            <span className="adm-field-label">
              {t("admin.detection.keywords")}
            </span>
            <textarea
              value={form.keywords}
              onChange={(event) =>
                setForm((current) => ({
                  ...current,
                  keywords: event.target.value,
                }))
              }
              rows={6}
              placeholder={t("admin.detection.keywordsPlaceholder")}
              disabled={!form.keywordEnabled}
            />
            <small className="adm-field-hint">
              {t("admin.detection.keywordsHint")}
            </small>
          </label>
        </Card>

        <Card className={cn("adm-stage", !form.visionEnabled && "is-off")}>
          <span className="adm-stage-index">02</span>
          <CardHeader
            icon={ImageIcon}
            title={t("admin.detection.boardTitle")}
            description={t("admin.detection.boardDescription")}
            actions={
              <Switch
                checked={form.visionEnabled}
                onChange={setFlag("visionEnabled")}
                label={t("admin.detection.boardTitle")}
              />
            }
          />
          {!form.keywordEnabled && form.visionEnabled ? (
            <p className="adm-callout is-warning">
              <TriangleAlert aria-hidden="true" />
              {t("admin.detection.boardNeedsKeywords")}
            </p>
          ) : null}
          <RouteEditor
            route="schedule_board"
            config={config}
            onSaved={routeSaved}
            onError={fail}
            needsImage
            t={t}
          />
        </Card>

        <Card className={cn("adm-stage", !form.messageEnabled && "is-off")}>
          <span className="adm-stage-index">03</span>
          <CardHeader
            icon={MessageSquareText}
            title={t("admin.detection.messageTitle")}
            description={t("admin.detection.messageDescription")}
            actions={
              <Switch
                checked={form.messageEnabled}
                onChange={setFlag("messageEnabled")}
                label={t("admin.detection.messageTitle")}
              />
            }
          />
          <RouteEditor
            route="schedule_message"
            config={config}
            onSaved={routeSaved}
            onError={fail}
            t={t}
          />
        </Card>

        <Card className="adm-stage is-muted">
          <span className="adm-stage-index">↺</span>
          <CardHeader
            icon={Route}
            title={t("admin.route.legacy")}
            description={t("admin.route.legacyHint")}
          />
          <RouteEditor
            route="schedule_vision"
            config={config}
            onSaved={routeSaved}
            onError={fail}
            t={t}
          />
        </Card>
      </div>
      <p className="adm-card-foot">
        <Tags aria-hidden="true" />
        {t("admin.detection.modalityNote")}
        <button
          type="button"
          className="adm-link"
          onClick={() => onNavigate("providers")}
        >
          {t("admin.detection.manageModels")}
        </button>
      </p>
    </section>
  )
}
