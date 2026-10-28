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
  if (totalPages <= 1)
    return (
      <p className="adm-pager-total">{t("admin.pager.total", { total })}</p>
    )
  return (
    <nav className="adm-pager" aria-label={t("admin.pager.label")}>
      <span>{t("admin.pager.status", { page, totalPages, total })}</span>
      <IconButton
        icon={ChevronLeft}
        label={t("admin.pager.previous")}
        disabled={page <= 1}
        onClick={() => onPage(page - 1)}
      />
      <IconButton
        icon={ChevronRight}
        label={t("admin.pager.next")}
        disabled={page >= totalPages}
        onClick={() => onPage(page + 1)}
      />
    </nav>
  )
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

/** A native select drawn as a labelled pill. */
export function FilterSelect({
  label,
  value,
  onChange,
  active = value !== "",
  children,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  /** Whether the value narrows the list; by default any non-empty value. */
  active?: boolean
  children: ReactNode
}) {
  return (
    <label className={cn("adm-filter-pill", active && "is-active")}>
      <span className="adm-filter-pill-label" aria-hidden>
        {label}
      </span>
      <select
        value={value}
        onChange={(event) => onChange(event.target.value)}
        aria-label={label}
      >
        {children}
      </select>
      <ChevronDown aria-hidden />
    </label>
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
