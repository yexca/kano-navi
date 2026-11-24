import { useId, useState, type SubmitEvent } from "react"
import {
  ChevronDown,
  Download,
  History,
  KeyRound,
  ShieldAlert,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { formatRelative, japanStamp, japanToday } from "@/admin/format"
import type {
  FetchItem,
  FetchMethod,
  FetchMode,
  FetchSource,
  Run,
  Sources,
  XRelatedAccounts,
} from "@/admin/types"
import {
  Button,
  Facts,
  Field,
  FieldGroup,
  Loading,
  Panel,
  Segmented,
  StatusBadge,
  Switch,
} from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { AddPanel } from "@/admin/views/bilibili-parts"
import {
  explainCode,
  metricLabel,
  RunMark,
  runOutcome,
  stepResult,
  windowText,
} from "@/admin/views/run-parts"
import { cn } from "@/lib/utils"

/** Whether a time is still ahead, such as a backoff's end. */
const ahead = (value: string | null | undefined): value is string =>
  Boolean(value && Date.parse(value) > Date.now())

/** The mode a method serves, keeping the current one when it can. */
function servedMode(method: FetchMethod, mode: FetchMode) {
  return method.modes.includes(mode) ? mode : method.modes[0]
}

/**
 * A manual fetch, method first: the listing method, for X every own account
 * or one of them, what it reads, its count or dates, and the steps that
 * follow. The run fails with the method's code when the method fails;
 * nothing falls back.
 */
function FetchForm({
  source,
  methods,
  accounts,
  hasRecords,
  disabled,
  onStarted,
}: {
  source: FetchSource
  methods: FetchMethod[]
  /** X's own accounts (X_HANDLES), in their configured order. */
  accounts: string[]
  hasRecords: boolean
  disabled: boolean
  onStarted: (run: Run) => void
}) {
  const { t } = useAppSettings()
  const { start } = useAdmin()
  const [methodId, setMethodId] = useState(
    () => (methods.find((item) => item.available) || methods[0])?.id,
  )
  // "" reads every account.
  const [account, setAccount] = useState("")
  const [mode, setMode] = useState<FetchMode>("count")
  const [count, setCount] = useState(
    source === "x" ? 50 : source === "youtube" ? 15 : 20,
  )
  const [days, setDays] = useState(30)
  const [startDate, setStartDate] = useState("")
  const [endDate, setEndDate] = useState(japanToday())
  const [extract, setExtract] = useState(false)
  const [translate, setTranslate] = useState(false)
  const [refresh, setRefresh] = useState(false)
  const [busy, setBusy] = useState(false)
  const method = methods.find((item) => item.id === methodId) || methods[0]
  if (!method) return null
  const current = servedMode(method, mode)
  const limit = Math.min(count, method.maxCount)
  const needsRecords = current === "before" && !hasRecords
  const chosen = accounts.includes(account) ? account : ""
  const submit = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    const selection =
      current === "count"
        ? { mode: current, count: limit }
        : current === "range"
          ? { mode: current, startDate, endDate }
          : current === "before"
            ? { mode: current, days }
            : { mode: current }
    const run = await start({
      kind: "fetch",
      source,
      method: method.id,
      window: chosen ? { ...selection, account: chosen } : selection,
      options: {
        extract: method.extract && extract,
        translate,
        refresh: method.refresh && refresh,
      },
    })
    setBusy(false)
    if (run) onStarted(run)
  }
  return (
    <form className="adm-form" onSubmit={submit}>
      {/* A source with one method, such as Bilibili's, has nothing to pick. */}
      {methods.length > 1 ? (
        <FieldGroup label={t("admin.fetch.method")}>
          <Segmented
            label={t("admin.fetch.method")}
            value={method.id}
            onChange={setMethodId}
            options={methods.map((item) => ({
              value: item.id,
              label: (
                <>
                  {item.available ? null : <KeyRound aria-hidden />}
                  {t(`admin.fetch.method.${source}.${item.id}`)}
                </>
              ),
            }))}
          />
        </FieldGroup>
      ) : null}
      {method.available ? null : (
        <p className="adm-inline-error">
          <KeyRound aria-hidden />
          {method.requires
            ? t("admin.fetch.needs", { variable: method.requires })
            : t("admin.fetch.unavailable")}
        </p>
      )}
      {source === "x" && accounts.length > 1 ? (
        <FieldGroup label={t("admin.fetch.account")}>
          <Segmented
            label={t("admin.fetch.account")}
            value={chosen}
            onChange={setAccount}
            options={[
              { value: "", label: t("admin.fetch.account.all") },
              ...accounts.map((handle) => ({
                value: handle,
                label: `@${handle}`,
              })),
            ]}
          />
        </FieldGroup>
      ) : null}
      {/* A method with one mode has nothing to pick; only a date range needs
      its bounds. */}
      {method.modes.length > 1 ? (
        <FieldGroup
          label={t("admin.fetch.selection")}
          hint={
            current === "range"
              ? t(`admin.fetch.modeHint.range.${source}`)
              : undefined
          }
        >
          <Segmented
            label={t("admin.fetch.selection")}
            value={current}
            onChange={setMode}
            options={method.modes.map((value) => ({
              value,
              label: t(`admin.fetch.mode.${value}`),
            }))}
          />
        </FieldGroup>
      ) : null}
      {current === "count" ? (
        <Field
          label={t(
            source === "x"
              ? "admin.fetch.count"
              : `admin.fetch.count.${source}`,
          )}
          hint={t("admin.fetch.countRange", { max: method.maxCount })}
        >
          <input
            type="number"
            min={1}
            max={method.maxCount}
            value={limit}
            onChange={(e) => setCount(Number(e.target.value))}
            required
          />
        </Field>
      ) : current === "range" ? (
        <div className="adm-form-row">
          <Field label={t("admin.fetch.start")}>
            <input
              type="date"
              value={startDate}
              max={endDate}
              onChange={(e) => setStartDate(e.target.value)}
              required
            />
          </Field>
          <Field label={t("admin.fetch.end")}>
            <input
              type="date"
              value={endDate}
              min={startDate}
              onChange={(e) => setEndDate(e.target.value)}
              required
            />
          </Field>
        </div>
      ) : current === "before" ? (
        <Field
          label={t("admin.fetch.backfillDays")}
          hint={t("admin.fetch.daysHint")}
        >
          <input
            type="number"
            min={1}
            max={365}
            value={days}
            onChange={(e) => setDays(Number(e.target.value))}
            required
          />
        </Field>
      ) : null}
      {needsRecords ? (
        <p className="adm-inline-error">{t("admin.fetch.noRecords")}</p>
      ) : null}
      <fieldset className="adm-fetch-options">
        <legend>{t("admin.fetch.options")}</legend>
        {method.extract ? (
          <Switch
            checked={extract}
            onChange={setExtract}
            label={t("admin.fetch.option.extract")}
          />
        ) : null}
        <Switch
          checked={translate}
          onChange={setTranslate}
          label={t("admin.fetch.option.translate")}
        />
        {method.refresh ? (
          <Switch
            checked={refresh}
            onChange={setRefresh}
            label={t("admin.fetch.option.refresh")}
          />
        ) : null}
      </fieldset>
      <div className="adm-form-actions">
        <Button
          type="submit"
          variant="primary"
          icon={Download}
          busy={busy}
          disabled={disabled || !method.available || needsRecords}
        >
          {t("admin.fetch.run")}
        </Button>
      </div>
    </form>
  )
}

/** What a fetch step counted, as the console's summary reads it. */
interface FetchCounts {
  listed?: number
  new?: number
  stored?: number
  failed?: number
  pages?: number
  /** YouTube API fetches: videos whose details were checked, and quota units. */
  inspected?: number
  apiRequests?: number
  reachedEnd?: boolean
  items?: FetchItem[]
  errors?: string[]
  error?: string
  /** Shop reads: tagged products with a dated period, and the period
   * groups they make. */
  candidates?: number
  sales?: number
  /** Related account reads: statuses promoted onto and demoted from the
   * board. */
  promoted?: number
  demoted?: number
}

/**
 * The newest manual fetch of a source: its outcome, what the method listed
 * split into new, already stored and failed records, the pages it read,
 * whether it reached the end, and the first new records.
 */
function FetchSummary({
  run,
  source,
  onOpenRuns,
}: {
  run: Run
  source: FetchSource | "x_related" | "shop"
  onOpenRuns: () => void
}) {
  const { t, locale } = useAppSettings()
  const outcome = runOutcome(run)
  const window = run.fetchWindows?.[source]
  const counts = (stepResult(run, source) || {}) as FetchCounts
  const progress = run.progress[source] || "pending"
  const finished = progress !== "pending" && progress !== "running"
  const items = counts.items || []
  const [open, setOpen] = useState(true)
  const id = useId()
  return (
    <section
      className={cn("adm-fetch-summary", open && "is-open")}
      aria-label={t("admin.fetch.last")}
    >
      <header
        onClick={(event) => {
          // The title button handles its own click.
          if (!(event.target as Element).closest("button")) setOpen(!open)
        }}
      >
        <RunMark status={outcome} />
        <span className="adm-fetch-summary-title">
          <button
            type="button"
            aria-expanded={open}
            aria-controls={`${id}-body`}
            onClick={() => setOpen(!open)}
          >
            {t("admin.fetch.last")}
          </button>
          <small>
            {window ? `${windowText(window, source, t)} · ` : ""}
            <time dateTime={run.createdAt} title={japanStamp(run.createdAt)}>
              {formatRelative(run.createdAt, locale)}
            </time>
          </small>
        </span>
        <StatusBadge status={outcome} />
        <ChevronDown aria-hidden className="adm-fold-chevron" />
      </header>
      <div id={`${id}-body`} className="adm-fetch-summary-body" hidden={!open}>
        {counts.error ? (
          <p className="adm-inline-error">
            <ShieldAlert aria-hidden />
            <code>{counts.error}</code>
          </p>
        ) : null}
        {finished && counts.listed !== undefined ? (
          <>
            <Facts
              items={
                source === "x_related"
                  ? [
                      [t("admin.metric.listed"), counts.listed],
                      [t("admin.metric.promoted"), counts.promoted],
                      [t("admin.metric.demoted"), counts.demoted],
                    ]
                  : source === "shop"
                    ? [
                        [t("admin.metric.listed"), counts.listed],
                        [t("admin.metric.candidates"), counts.candidates],
                        [t("admin.metric.sales"), counts.sales],
                        [t("admin.metric.pages"), counts.pages],
                      ]
                    : [
                        [t("admin.metric.listed"), counts.listed],
                        [t("admin.metric.new"), counts.new],
                        [t("admin.metric.stored"), counts.stored],
                        [t("admin.metric.failed"), counts.failed],
                        [t("admin.metric.pages"), counts.pages],
                        ...(counts.inspected === undefined
                          ? []
                          : ([
                              [t("admin.metric.inspected"), counts.inspected],
                              [
                                t("admin.metric.apiRequests"),
                                counts.apiRequests,
                              ],
                            ] as [string, number | undefined][])),
                      ]
              }
            />
            {/* A related read stops at its cursor or its bound, as its
                timer's does. */}
            {source === "x_related" ? null : (
              <p className="adm-field-hint">
                {counts.reachedEnd
                  ? t("admin.fetch.reachedEnd")
                  : t(
                      source === "shop"
                        ? "admin.fetch.shopPageLimit"
                        : "admin.fetch.repeat",
                    )}
              </p>
            )}
            {items.length ? (
              <div className="adm-fetch-items">
                <span>
                  {t("admin.fetch.newItems", { count: counts.new || 0 })}
                </span>
                <ul>
                  {items.map((item) => (
                    <li key={item.id}>
                      <code>{item.id}</code>
                      <span>{item.title}</span>
                      {item.account ? <small>@{item.account}</small> : null}
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
            {counts.errors?.length ? (
              <div className="adm-metric-list">
                <span>{metricLabel("errors", t)}</span>
                <span className="adm-chip-row">
                  {counts.errors.map((code, index) => (
                    <code key={index} title={explainCode(code, t) || ""}>
                      {code}
                    </code>
                  ))}
                </span>
              </div>
            ) : null}
          </>
        ) : null}
        <div className="adm-form-actions">
          <Button size="sm" variant="ghost" icon={History} onClick={onOpenRuns}>
            {t("admin.fetch.openRuns")}
          </Button>
        </div>
      </div>
    </section>
  )
}

/**
 * What a source's manual fetch reads through: its methods, whether the
 * source runs in this process, the oldest stored record per account and,
 * for Bilibili, the captcha backoff a fetch waits out rather than forces.
 */
function fetchState(
  source: FetchSource,
  sources: Sources,
  t: ReturnType<typeof useAppSettings>["t"],
) {
  const bilibili = sources.bilibili
  switch (source) {
    case "x":
      return {
        methods: sources.x.methods,
        skipped: sources.x.skipped,
        oldest: sources.x.accounts.map(
          (account) => [account.account, account.oldest] as const,
        ),
        blockedUntil: null,
      }
    case "youtube":
      return {
        methods: sources.youtube.methods,
        skipped: sources.youtube.skipped,
        oldest: [[t("admin.sources.channel"), sources.youtube.oldest] as const],
        blockedUntil: null,
      }
    default:
      return {
        methods:
          (source === "bilibili"
            ? bilibili.methods
            : bilibili.dynamics?.methods) ?? [],
        skipped: !bilibili.available || bilibili.skipped,
        oldest: [
          [
            t("admin.bilibili.account"),
            source === "bilibili" ? bilibili.oldest : bilibili.dynamics?.oldest,
          ] as const,
        ],
        blockedUntil: ahead(bilibili.blockedUntil)
          ? bilibili.blockedUntil
          : null,
      }
  }
}

/** The manual fetch of the selected source: the form, then the newest
 * fetch's summary. */
export function FetchPanel({
  source,
  sources,
  onOpenRuns,
}: {
  source: FetchSource
  sources: Sources
  onOpenRuns: () => void
}) {
  const { t } = useAppSettings()
  const { api, overview } = useAdmin()
  const state = fetchState(source, sources, t)
  const [started, setStarted] = useState<string | null>(null)
  // The run started here, or else the newest manual fetch of this source
  // among the recent runs.
  const recent = overview?.recentRuns.find(
    (run) => run.fetchWindows?.[source]?.method,
  )
  const shown = started || recent?.id || null
  const last = useResource(
    () =>
      shown ? api.run(shown).then((data) => data.run) : Promise.resolve(null),
    [shown],
  )
  const oldest = state.oldest
  return (
    <Panel
      icon={Download}
      title={t("admin.fetch.headingFor", {
        source: t(`admin.step.${source}`),
      })}
    >
      <Facts
        items={[
          ...oldest.map(
            ([label, time]) =>
              [
                t("admin.sources.oldest", { label }),
                time ? japanStamp(time) : t("admin.sources.none"),
              ] as [string, string],
          ),
        ]}
      />
      {state.blockedUntil ? (
        <p className="adm-inline-error">
          <ShieldAlert aria-hidden />
          {t("admin.fetch.bilibiliBlocked", {
            time: japanStamp(state.blockedUntil),
          })}
        </p>
      ) : null}
      <FetchForm
        source={source}
        methods={state.methods}
        accounts={
          source === "x" ? sources.x.accounts.map((entry) => entry.account) : []
        }
        hasRecords={oldest.some(([, time]) => Boolean(time))}
        disabled={
          state.skipped ||
          Boolean(state.blockedUntil) ||
          Boolean(overview?.activeRun)
        }
        onStarted={(run) => setStarted(run.id)}
      />
      {last.data &&
      last.data.id === shown &&
      last.data.fetchWindows?.[source] ? (
        <FetchSummary run={last.data} source={source} onOpenRuns={onOpenRuns} />
      ) : shown ? null : (
        <p className="adm-fetch-empty">{t("admin.fetch.noneYet")}</p>
      )}
    </Panel>
  )
}

/**
 * Adds or re-reads one video of the channel by ID or link, at once: through
 * the Data API with a key, its watch page without. Disabled while the
 * source does not run here.
 */
export function YoutubeAddVideo({
  youtube,
  onAdded,
}: {
  youtube: Sources["youtube"]
  onAdded: () => void
}) {
  const { t } = useAppSettings()
  const { api } = useAdmin()
  return (
    <AddPanel
      usable={!youtube.skipped}
      title={t("admin.youtube.add")}
      label={t("admin.youtube.addField")}
      placeholder="https://www.youtube.com/watch?v=…"
      notice="admin.notice.youtubeAdded"
      send={(input) => api.addYoutubeVideo(input)}
      onAdded={onAdded}
    />
  )
}

/**
 * The shop's manual read: the whole catalog, at most six pages three seconds
 * apart, for products the timer's newest-50 window never listed. It waits
 * out a 429's Retry-After like the timer. Then the newest read's summary.
 */
export function ShopFetchPanel({
  sources,
  onOpenRuns,
}: {
  sources: Sources
  onOpenRuns: () => void
}) {
  const { t } = useAppSettings()
  const { api, overview, start } = useAdmin()
  const shop = sources.shop
  const method = shop.methods.find((item) => item.id === "catalog")
  const [busy, setBusy] = useState(false)
  const [started, setStarted] = useState<string | null>(null)
  const recent = overview?.recentRuns.find(
    (run) => run.fetchWindows?.shop?.method,
  )
  const shown = started || recent?.id || null
  const last = useResource(
    () =>
      shown ? api.run(shown).then((data) => data.run) : Promise.resolve(null),
    [shown],
  )
  const waiting = Boolean(
    shop.status.blockedUntil &&
    Date.parse(shop.status.blockedUntil) > Date.now(),
  )
  const readAll = async () => {
    setBusy(true)
    const run = await start({
      kind: "fetch",
      source: "shop",
      method: "catalog",
      window: { mode: "all" },
    })
    setBusy(false)
    if (run) setStarted(run.id)
  }
  return (
    <Panel
      icon={Download}
      title={t("admin.fetch.headingFor", { source: t("admin.step.shop") })}
    >
      <div className="adm-form-actions">
        <Button
          variant="primary"
          icon={Download}
          busy={busy}
          disabled={
            shop.skipped ||
            waiting ||
            !method?.available ||
            Boolean(overview?.activeRun)
          }
          onClick={() => void readAll()}
        >
          {t("admin.shop.readAll")}
        </Button>
      </div>
      {last.data && last.data.id === shown && last.data.fetchWindows?.shop ? (
        <FetchSummary run={last.data} source="shop" onOpenRuns={onOpenRuns} />
      ) : shown ? null : (
        <p className="adm-fetch-empty">{t("admin.fetch.noneYet")}</p>
      )}
    </Panel>
  )
}

/**
 * A related account's manual read: the timer's incremental read of the one
 * account picked here, through the FxTwitter timeline, with schedule
 * extraction and translation after it on request. Then the newest such
 * fetch's summary.
 */
export function RelatedFetchPanel({
  related,
  skipped,
  onOpenRuns,
}: {
  related: XRelatedAccounts | undefined
  skipped: boolean
  onOpenRuns: () => void
}) {
  const { t } = useAppSettings()
  const { api, overview, start } = useAdmin()
  const [account, setAccount] = useState("")
  const [extract, setExtract] = useState(false)
  const [translate, setTranslate] = useState(false)
  const [busy, setBusy] = useState(false)
  const [started, setStarted] = useState<string | null>(null)
  const recent = overview?.recentRuns.find(
    (run) => run.fetchWindows?.x_related?.method,
  )
  const shown = started || recent?.id || null
  const last = useResource(
    () =>
      shown ? api.run(shown).then((data) => data.run) : Promise.resolve(null),
    [shown],
  )
  const handles = related?.accounts.map((entry) => entry.handle) || []
  const chosen = handles.includes(account) ? account : handles[0] || ""
  const status = related?.status.find((entry) => entry.handle === chosen)
  const submit = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!chosen) return
    setBusy(true)
    const run = await start({
      kind: "fetch",
      source: "x_related",
      method: "timeline",
      window: { mode: "incremental", account: chosen },
      options: { extract, translate },
    })
    setBusy(false)
    if (run) setStarted(run.id)
  }
  return (
    <Panel
      icon={Download}
      title={t("admin.fetch.headingFor", {
        source: t("admin.step.x_related"),
      })}
    >
      {!related ? (
        <Loading />
      ) : handles.length ? (
        <form className="adm-form" onSubmit={submit}>
          <Field
            label={t("admin.fetch.account")}
            hint={
              status
                ? status.lastSuccessAt
                  ? t("admin.related.lastRead", {
                      time: japanStamp(status.lastSuccessAt),
                    })
                  : t("admin.related.neverRead")
                : undefined
            }
          >
            <select
              value={chosen}
              onChange={(event) => setAccount(event.target.value)}
            >
              {handles.map((handle) => (
                <option key={handle} value={handle}>
                  @{handle}
                </option>
              ))}
            </select>
          </Field>
          <fieldset className="adm-fetch-options">
            <legend>{t("admin.fetch.options")}</legend>
            <Switch
              checked={extract}
              onChange={setExtract}
              label={t("admin.fetch.option.extract")}
            />
            <Switch
              checked={translate}
              onChange={setTranslate}
              label={t("admin.fetch.option.translate")}
            />
          </fieldset>
          <div className="adm-form-actions">
            <Button
              type="submit"
              variant="primary"
              icon={Download}
              busy={busy}
              disabled={skipped || Boolean(overview?.activeRun)}
            >
              {t("admin.fetch.run")}
            </Button>
          </div>
        </form>
      ) : (
        <p className="adm-fetch-empty">{t("admin.related.fetchNone")}</p>
      )}
      {last.data &&
      last.data.id === shown &&
      last.data.fetchWindows?.x_related ? (
        <FetchSummary
          run={last.data}
          source="x_related"
          onOpenRuns={onOpenRuns}
        />
      ) : shown || !handles.length ? null : (
        <p className="adm-fetch-empty">{t("admin.fetch.noneYet")}</p>
      )}
    </Panel>
  )
}
