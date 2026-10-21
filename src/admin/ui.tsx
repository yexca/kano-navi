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
  Check,
  ChevronLeft,
  ChevronRight,
  LoaderCircle,
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
}: {
  title?: ReactNode
  description?: ReactNode
  actions?: ReactNode
  children?: ReactNode
  className?: string
  icon?: Icon
}) {
  const id = useId()
  return (
    <section
      className={cn("adm-panel", className)}
      aria-labelledby={title ? id : undefined}
    >
      {title || actions ? (
        <header className="adm-panel-header">
          <div>
            {title ? (
              <h2 id={id}>
                {IconComponent ? <IconComponent aria-hidden /> : null}
                {title}
              </h2>
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
}: {
  checked: boolean
  onChange: (value: boolean) => void
  label: ReactNode
  description?: ReactNode
  disabled?: boolean
}) {
  return (
    <label className={cn("adm-switch", disabled && "is-disabled")}>
      <input
        type="checkbox"
        role="switch"
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span className="adm-switch-track" aria-hidden />
      <span className="adm-switch-text">
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
}: {
  open: boolean
  title: ReactNode
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  wide?: boolean
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
      className={cn("adm-modal", wide && "is-wide")}
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
