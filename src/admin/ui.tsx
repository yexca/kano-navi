import {
  useEffect,
  useId,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type ComponentType,
  type ReactNode,
} from "react"
import {
  CalendarRange,
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  LoaderCircle,
  RotateCcw,
  TriangleAlert,
  X,
} from "lucide-react"

import { useAppSettings } from "@/app-settings"
import { renderMessage } from "@/admin/api"
import { statusLabel, statusTone } from "@/admin/format"
import type { Toast } from "@/admin/use-admin-data"
import { cn } from "@/lib/utils"

type Icon = ComponentType<{ className?: string; "aria-hidden"?: boolean }>

export type Tone =
  | "neutral"
  | "berry"
  | "sky"
  | "leaf"
  | "honey"
  | "lavender"
  | "danger"
  | "running"
  | "success"
  | "warning"

export function Spinner({ className }: { className?: string }) {
  return <LoaderCircle className={cn("adm-spin", className)} aria-hidden />
}

export function Button({
  variant = "outline",
  size = "md",
  icon: IconComponent,
  busy = false,
  className,
  children,
  disabled,
  type = "button",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "outline" | "ghost" | "danger"
  size?: "sm" | "md"
  icon?: Icon
  busy?: boolean
}) {
  return (
    <button
      type={type}
      className={cn(
        "adm-button",
        `is-${variant}`,
        size === "sm" && "is-small",
        className,
      )}
      disabled={disabled || busy}
      aria-busy={busy || undefined}
      {...props}
    >
      {busy ? (
        <Spinner />
      ) : IconComponent ? (
        <IconComponent aria-hidden />
      ) : null}
      {children ? <span>{children}</span> : null}
    </button>
  )
}

export function IconButton({
  icon: IconComponent,
  label,
  className,
  busy,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  icon: Icon
  label: string
  busy?: boolean
}) {
  return (
    <button
      type="button"
      className={cn("adm-icon-button", className)}
      aria-label={label}
      title={label}
      disabled={props.disabled || busy}
      {...props}
    >
      {busy ? <Spinner /> : <IconComponent aria-hidden />}
    </button>
  )
}

/** A destructive or irreversible action that asks once more inline. */
export function ConfirmButton({
  onConfirm,
  confirmLabel,
  children,
  ...props
}: Omit<Parameters<typeof Button>[0], "onClick"> & {
  onConfirm: () => void
  confirmLabel?: string
}) {
  const { t } = useAppSettings()
  const [armed, setArmed] = useState(false)
  useEffect(() => {
    if (!armed) return undefined
    const timer = window.setTimeout(() => setArmed(false), 3500)
    return () => window.clearTimeout(timer)
  }, [armed])
  return (
    <Button
      {...props}
      variant={armed ? "danger" : props.variant}
      onClick={() => {
        if (!armed) return setArmed(true)
        setArmed(false)
        onConfirm()
      }}
    >
      {armed ? confirmLabel || t("admin.action.confirmAgain") : children}
    </Button>
  )
}

export function Badge({
  tone = "neutral",
  icon: IconComponent,
  children,
  className,
}: {
  tone?: Tone
  icon?: Icon
  children: ReactNode
  className?: string
}) {
  return (
    <span className={cn("adm-badge", `adm-tone-${tone}`, className)}>
      {IconComponent ? <IconComponent aria-hidden /> : null}
      {children}
    </span>
  )
}

export function StatusBadge({ status }: { status: string | null | undefined }) {
  const { t } = useAppSettings()
  const tone = statusTone(status)
  return (
    <Badge tone={tone} className="adm-status">
      {tone === "running" ? <Spinner /> : <i className="adm-dot" />}
      {statusLabel(status, t)}
    </Badge>
  )
}

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string
  description?: ReactNode
  actions?: ReactNode
}) {
  return (
    <header className="adm-page-header">
      <div>
        <h1>{title}</h1>
        {description ? <p>{description}</p> : null}
      </div>
      {actions ? <div className="adm-page-actions">{actions}</div> : null}
    </header>
  )
}

export function Panel({
  title,
  description,
  actions,
  children,
  className,
  icon: IconComponent,
  level = 2,
}: {
  title?: ReactNode
  description?: ReactNode
  actions?: ReactNode
  children?: ReactNode
  className?: string
  icon?: Icon
  /** 3 for a panel inside a page section that has its own heading. */
  level?: 2 | 3
}) {
  const id = useId()
  const Heading = level === 3 ? "h3" : "h2"
  return (
    <section
      className={cn("adm-panel", className)}
      aria-labelledby={title ? id : undefined}
    >
      {title || actions ? (
        <header className="adm-panel-header">
          <div>
            {title ? (
              <Heading id={id}>
                {IconComponent ? <IconComponent aria-hidden /> : null}
                {title}
              </Heading>
            ) : null}
            {description ? <p>{description}</p> : null}
          </div>
          {actions ? <div className="adm-panel-actions">{actions}</div> : null}
        </header>
      ) : null}
      {children}
    </section>
  )
}

export function Field({
  label,
  hint,
  children,
  className,
}: {
  label: ReactNode
  hint?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <label className={cn("adm-field", className)}>
      <span className="adm-field-label">{label}</span>
      {children}
      {hint ? <span className="adm-field-hint">{hint}</span> : null}
    </label>
  )
}

export function Switch({
  checked,
  onChange,
  label,
  description,
  disabled,
  compact,
}: {
  checked: boolean
  onChange: (value: boolean) => void
  label: ReactNode
  description?: ReactNode
  disabled?: boolean
  /** Only the track; the label stays for tooltips and readers. */
  compact?: boolean
}) {
  return (
    <label
      className={cn(
        "adm-switch",
        disabled && "is-disabled",
        compact && "is-compact",
      )}
      title={compact && typeof label === "string" ? label : undefined}
    >
      <input
        type="checkbox"
        role="switch"
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span className="adm-switch-track" aria-hidden />
      <span className={compact ? "adm-visually-hidden" : "adm-switch-text"}>
        <strong>{label}</strong>
        {description ? <small>{description}</small> : null}
      </span>
    </label>
  )
}

/** A small radio group drawn as a segmented control. */
export function Segmented<T extends string>({
  value,
  options,
  onChange,
  label,
}: {
  value: T
  options: { value: NoInfer<T>; label: ReactNode; count?: number }[]
  onChange: (value: NoInfer<T>) => void
  label: string
}) {
  return (
    <div className="adm-segmented" role="radiogroup" aria-label={label}>
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          role="radio"
          aria-checked={value === option.value}
          className={cn(value === option.value && "is-active")}
          onClick={() => onChange(option.value)}
        >
          {option.label}
          {option.count ? <em>{option.count}</em> : null}
        </button>
      ))}
    </div>
  )
}

export function Empty({
  icon: IconComponent,
  title,
  children,
}: {
  icon?: Icon
  title: ReactNode
  children?: ReactNode
}) {
  return (
    <div className="adm-empty">
      {IconComponent ? <IconComponent aria-hidden /> : null}
      <strong>{title}</strong>
      {children ? <p>{children}</p> : null}
    </div>
  )
}

export function Loading() {
  const { t } = useAppSettings()
  return (
    <div className="adm-loading-block" role="status">
      <Spinner />
      {t("admin.loading")}
    </div>
  )
}

export function Pager({
  page,
  totalPages,
  total,
  onPage,
}: {
  page: number
  totalPages: number
  total: number
  onPage: (page: number) => void
}) {
  const { t } = useAppSettings()
  const [draft, setDraft] = useState("")
  if (totalPages <= 1)
    return (
      <p className="adm-pager-total">{t("admin.pager.total", { total })}</p>
    )
  const jump = () => {
    const target = Math.round(Number(draft))
    setDraft("")
    if (Number.isFinite(target) && target >= 1)
      onPage(Math.min(target, totalPages))
  }
  return (
    <nav className="adm-pager" aria-label={t("admin.pager.label")}>
      <span className="adm-pager-count">
        {t("admin.pager.total", { total })}
      </span>
      <span className="adm-pager-pages">
        <IconButton
          icon={ChevronLeft}
          label={t("admin.pager.previous")}
          disabled={page <= 1}
          onClick={() => onPage(page - 1)}
        />
        {pageWindow(page, totalPages).map((number, index) =>
          number === null ? (
            <span key={`gap-${index}`} className="adm-pager-gap" aria-hidden>
              …
            </span>
          ) : (
            <button
              key={number}
              type="button"
              className="adm-pager-page"
              aria-current={number === page ? "page" : undefined}
              aria-label={t("admin.pager.page", { page: number })}
              onClick={() => onPage(number)}
            >
              {number}
            </button>
          ),
        )}
        <IconButton
          icon={ChevronRight}
          label={t("admin.pager.next")}
          disabled={page >= totalPages}
          onClick={() => onPage(page + 1)}
        />
      </span>
      <form
        className="adm-pager-jump"
        // A number past either end goes to that end instead of failing.
        noValidate
        onSubmit={(event) => {
          event.preventDefault()
          jump()
        }}
      >
        <label>
          <span>{t("admin.pager.jump")}</span>
          <input
            type="number"
            inputMode="numeric"
            min={1}
            max={totalPages}
            value={draft}
            placeholder={String(page)}
            onChange={(event) => setDraft(event.target.value)}
          />
        </label>
        <Button size="sm" type="submit" disabled={!draft}>
          {t("admin.pager.go")}
        </Button>
      </form>
    </nav>
  )
}

/**
 * The page numbers a pager shows: the first and last pages and two either
 * side of the current one. A gap of one page shows that page; a longer gap
 * is null and reads as an ellipsis.
 */
export function pageWindow(page: number, totalPages: number) {
  const pages = [1, totalPages]
  for (let number = page - 2; number <= page + 2; number++)
    if (number > 1 && number < totalPages) pages.push(number)
  const sorted = [...new Set(pages)].sort((a, b) => a - b)
  const shown: (number | null)[] = []
  sorted.forEach((number, index) => {
    const gap = index ? number - sorted[index - 1] : 1
    if (gap === 2) shown.push(number - 1)
    else if (gap > 2) shown.push(null)
    shown.push(number)
  })
  return shown
}

/** A native modal dialog: Escape closes it and focus returns to the opener. */
export function Modal({
  open,
  title,
  onClose,
  children,
  footer,
  wide,
  className,
}: {
  open: boolean
  title: ReactNode
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  wide?: boolean
  className?: string
}) {
  const { t } = useAppSettings()
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    if (open && !dialog.open) {
      dialog.showModal()
      // showModal focuses the close button; start in the first field instead.
      dialog
        .querySelector<HTMLElement>(
          ".adm-modal-content :is(input, select, textarea, button)",
        )
        ?.focus()
    }
    if (!open && dialog.open) dialog.close()
  }, [open])
  return (
    <dialog
      ref={ref}
      className={cn("adm-modal", wide && "is-wide", className)}
      onClose={onClose}
      onClick={(event) => {
        if (event.target === ref.current) onClose()
      }}
    >
      {open ? (
        <div className="adm-modal-body">
          <header>
            <h2>{title}</h2>
            <IconButton
              icon={X}
              label={t("admin.action.close")}
              onClick={onClose}
            />
          </header>
          <div className="adm-modal-content">{children}</div>
          {footer ? <footer>{footer}</footer> : null}
        </div>
      ) : null}
    </dialog>
  )
}

export function Toasts({
  toasts,
  onDismiss,
}: {
  toasts: Toast[]
  onDismiss: (id: number) => void
}) {
  const { t } = useAppSettings()
  return (
    <div className="adm-toasts" aria-live="polite">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          className={cn("adm-toast", `is-${toast.tone}`)}
          role={toast.tone === "danger" ? "alert" : "status"}
        >
          {toast.tone === "danger" ? (
            <TriangleAlert aria-hidden />
          ) : (
            <Check aria-hidden />
          )}
          <span>{renderMessage(toast.message, t)}</span>
          <IconButton
            icon={X}
            label={t("admin.action.close")}
            onClick={() => onDismiss(toast.id)}
          />
        </div>
      ))}
    </div>
  )
}

/** Key/value facts laid out as a definition list. */
export function Facts({ items }: { items: [ReactNode, ReactNode][] }) {
  return (
    <dl className="adm-facts">
      {items.map(([term, value], index) => (
        <div key={index}>
          <dt>{term}</dt>
          <dd>{value ?? "—"}</dd>
        </div>
      ))}
    </dl>
  )
}

/** A highlighted note: an icon, an optional title and its body. */
export function Callout({
  tone = "neutral",
  icon: IconComponent,
  title,
  children,
  className,
}: {
  tone?: Tone
  icon?: Icon
  title?: ReactNode
  children?: ReactNode
  className?: string
}) {
  return (
    <div className={cn("adm-callout", `adm-tone-${tone}`, className)}>
      {IconComponent ? <IconComponent aria-hidden /> : null}
      <div>
        {title ? <strong>{title}</strong> : null}
        {children}
      </div>
    </div>
  )
}

// --- filter bars -------------------------------------------------------------
// A list's filters read as one row of pills: each names what it filters, and
// one that narrows the list is highlighted so the active filters stand out.

export type FilterOption = { value: string; label: string }

/**
 * A labelled pill that opens the list of its choices. The list follows the
 * listbox pattern: the arrows, Home and End move, Enter or Space picks, and
 * Escape, Tab or a click elsewhere closes it.
 */
export function FilterSelect({
  label,
  value,
  onChange,
  options,
  active = value !== "",
}: {
  label: string
  value: string
  onChange: (value: string) => void
  options: FilterOption[]
  /** Whether the value narrows the list; by default any non-empty value. */
  active?: boolean
}) {
  const id = useId()
  const root = useRef<HTMLDivElement>(null)
  const button = useRef<HTMLButtonElement>(null)
  const list = useRef<HTMLUListElement>(null)
  const [open, setOpen] = useState(false)
  const [cursor, setCursor] = useState(0)
  const selected = Math.max(
    0,
    options.findIndex((option) => option.value === value),
  )
  useEffect(() => {
    if (!open) return undefined
    list.current?.focus()
    const onPointer = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener("pointerdown", onPointer)
    return () => document.removeEventListener("pointerdown", onPointer)
  }, [open])
  useEffect(() => {
    if (open)
      list.current?.children[cursor]?.scrollIntoView({ block: "nearest" })
  }, [open, cursor])
  const show = () => {
    setCursor(selected)
    setOpen(true)
  }
  const close = () => {
    setOpen(false)
    button.current?.focus()
  }
  const pick = (index: number) => {
    close()
    if (options[index] && options[index].value !== value)
      onChange(options[index].value)
  }
  const last = options.length - 1
  return (
    <div
      ref={root}
      className={cn(
        "adm-filter-pill adm-select",
        active && "is-active",
        open && "is-open",
      )}
    >
      <button
        ref={button}
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? `${id}-list` : undefined}
        onClick={() => (open ? setOpen(false) : show())}
        onKeyDown={(event) => {
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault()
            show()
          }
        }}
      >
        <span className="adm-filter-pill-label">{label}</span>
        <span className="adm-select-value">{options[selected]?.label}</span>
        <ChevronDown aria-hidden />
      </button>
      {open ? (
        <ul
          ref={list}
          id={`${id}-list`}
          role="listbox"
          tabIndex={-1}
          aria-label={label}
          aria-activedescendant={`${id}-${cursor}`}
          className="adm-select-list"
          onKeyDown={(event) => {
            const moves: Record<string, number> = {
              ArrowDown: Math.min(cursor + 1, last),
              ArrowUp: Math.max(cursor - 1, 0),
              Home: 0,
              End: last,
            }
            if (event.key in moves) setCursor(moves[event.key])
            else if (event.key === "Enter" || event.key === " ") pick(cursor)
            else if (event.key === "Escape") close()
            else if (event.key === "Tab") return setOpen(false)
            else return undefined
            event.preventDefault()
          }}
        >
          {options.map((option, index) => (
            <li
              key={option.value}
              id={`${id}-${index}`}
              role="option"
              aria-selected={index === selected}
              className={cn(index === cursor && "is-cursor")}
              onPointerMove={() => setCursor(index)}
              onClick={() => pick(index)}
            >
              <span>{option.label}</span>
              {index === selected ? <Check aria-hidden /> : null}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}

/** A checkbox drawn as a pill. */
export function FilterToggle({
  checked,
  onChange,
  children,
}: {
  checked: boolean
  onChange: (checked: boolean) => void
  children: ReactNode
}) {
  return (
    <label className={cn("adm-filter-pill is-toggle", checked && "is-active")}>
      <input
        type="checkbox"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
      />
      {children}
    </label>
  )
}

/** Inclusive from and to dates in one pill; either end may stay open. */
export function DateRange({
  from,
  to,
  onChange,
}: {
  from: string
  to: string
  onChange: (range: { from?: string; to?: string }) => void
}) {
  const { t } = useAppSettings()
  return (
    <div
      className={cn(
        "adm-filter-pill adm-date-range",
        (from || to) && "is-active",
      )}
      role="group"
      aria-label={t("admin.filter.dates")}
    >
      <CalendarRange aria-hidden />
      <input
        type="date"
        value={from}
        className={from ? undefined : "is-empty"}
        max={to || undefined}
        onChange={(event) => onChange({ from: event.target.value })}
        aria-label={t("admin.schedule.filter.from")}
      />
      <span aria-hidden>–</span>
      <input
        type="date"
        value={to}
        className={to ? undefined : "is-empty"}
        min={from || undefined}
        onChange={(event) => onChange({ to: event.target.value })}
        aria-label={t("admin.schedule.filter.to")}
      />
    </div>
  )
}

/** Shown only while a filter differs from the default. */
export function FilterReset({
  count,
  onReset,
}: {
  count: number
  onReset: () => void
}) {
  const { t } = useAppSettings()
  if (!count) return null
  return (
    <button type="button" className="adm-filter-reset" onClick={onReset}>
      <RotateCcw aria-hidden />
      {t("admin.filter.reset", { count })}
    </button>
  )
}

// --- tag input ---------------------------------------------------------------

const tagSeparators = /[\n,，、]/u

/**
 * A list of short values edited as chips: Enter or a comma adds what is
 * typed, Backspace in the empty field removes the last chip, and pasting a
 * list adds each entry. Duplicates are ignored without regard to case.
 */
export function TagInput({
  value,
  onChange,
  label,
  placeholder,
  max,
  maxLength,
}: {
  value: string[]
  onChange: (value: string[]) => void
  label: string
  placeholder?: string
  max?: number
  maxLength?: number
}) {
  const { t } = useAppSettings()
  const [draft, setDraft] = useState("")
  const input = useRef<HTMLInputElement>(null)
  const full = max !== undefined && value.length >= max
  const add = (text: string) => {
    const seen = new Set(value.map((item) => item.toLowerCase()))
    const next = [...value]
    for (const part of text.split(tagSeparators)) {
      const item = part.trim().slice(0, maxLength)
      if (!item || seen.has(item.toLowerCase())) continue
      if (max !== undefined && next.length >= max) break
      seen.add(item.toLowerCase())
      next.push(item)
    }
    if (next.length !== value.length) onChange(next)
    setDraft("")
  }
  const remove = (index: number) => {
    onChange(value.filter((_, at) => at !== index))
    input.current?.focus()
  }
  return (
    <div
      className={cn("adm-tag-input", full && "is-full")}
      onClick={(event) => {
        if (event.target === event.currentTarget) input.current?.focus()
      }}
    >
      {value.map((item, index) => (
        <span key={item} className="adm-tag">
          <span>{item}</span>
          <button
            type="button"
            aria-label={t("admin.tags.remove", { tag: item })}
            title={t("admin.tags.remove", { tag: item })}
            onClick={() => remove(index)}
          >
            <X aria-hidden />
          </button>
        </span>
      ))}
      <input
        ref={input}
        value={draft}
        aria-label={label}
        placeholder={
          full ? t("admin.tags.full", { max: max ?? 0 }) : placeholder
        }
        maxLength={maxLength}
        onChange={(event) => {
          const text = event.target.value
          // An input method composing Japanese or Chinese text keeps its draft.
          const composing = (event.nativeEvent as InputEvent).isComposing
          if (!composing && tagSeparators.test(text)) add(text)
          else setDraft(text)
        }}
        onCompositionEnd={(event) => {
          const text = event.currentTarget.value
          if (tagSeparators.test(text)) add(text)
        }}
        onKeyDown={(event) => {
          if (event.nativeEvent.isComposing) return
          if (event.key === "Enter") {
            event.preventDefault()
            add(draft)
          } else if (event.key === "Backspace" && !draft && value.length) {
            event.preventDefault()
            remove(value.length - 1)
          }
        }}
        onPaste={(event) => {
          const text = event.clipboardData.getData("text")
          if (!tagSeparators.test(text)) return
          event.preventDefault()
          add(`${draft}${text}`)
        }}
        onBlur={() => add(draft)}
      />
    </div>
  )
}
