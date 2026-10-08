import { forwardRef, useEffect, useId, useRef } from "react"
import { LoaderCircle, X } from "lucide-react"

import { cn } from "@/lib/utils"
import { statusLabel, statusTone } from "@/admin/format"

/*
 * Small console primitives. They are styled by src/admin/admin.css on top of
 * the shared tokens in src/index.css so both themes stay in sync with the
 * public board.
 */

export const Btn = forwardRef<
  HTMLButtonElement,
  React.ComponentPropsWithoutRef<"button"> & {
    variant?: string
    size?: string
    busy?: boolean
    icon?: React.ElementType
  }
>(function Btn(
  {
    variant = "default",
    size = "md",
    busy = false,
    icon: Icon = null,
    className,
    children,
    disabled,
    type = "button",
    ...props
  },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type}
      className={cn(
        "adm-btn",
        `adm-btn-${variant}`,
        `adm-btn-${size}`,
        className,
      )}
      disabled={disabled || busy}
      aria-busy={busy || undefined}
      {...props}
    >
      {busy ? (
        <LoaderCircle className="adm-spin" aria-hidden="true" />
      ) : Icon ? (
        <Icon aria-hidden="true" />
      ) : null}
      {children}
    </button>
  )
})

export function IconBtn({
  label,
  icon: Icon,
  busy = false,
  className,
  ...props
}: {
  label?: string
  icon?: React.ElementType
  busy?: boolean
  className?: string
  [key: string]: any
}) {
  return (
    <Btn
      variant="ghost"
      size="icon"
      className={className}
      aria-label={label}
      title={label}
      busy={busy}
      icon={Icon}
      {...props}
    />
  )
}

export function Switch({
  checked,
  onChange,
  label,
  disabled = false,
  size,
}: {
  checked: boolean
  onChange: any
  label?: string
  disabled?: boolean
  size?: string
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={Boolean(checked)}
      aria-label={label}
      title={label}
      className={cn("adm-switch", size === "sm" && "adm-switch-sm")}
      onClick={() => onChange(!checked)}
      disabled={disabled}
    >
      <span aria-hidden="true" />
    </button>
  )
}

export function Tag({
  tone = "neutral",
  active = true,
  className,
  children,
  ...props
}: {
  tone?: string
  active?: boolean
  className?: string
  children?: React.ReactNode
  [key: string]: any
}) {
  return (
    <span
      className={cn(
        "adm-tag",
        `adm-tone-${tone}`,
        !active && "is-inactive",
        className,
      )}
      {...props}
    >
      {children}
    </span>
  )
}

export function StatusPill({
  status,
  label,
  t,
}: {
  status: any
  label?: string
  t: any
}) {
  const tone = statusTone(status)
  return (
    <span className={cn("adm-status", `is-${tone}`)}>
      <span className="adm-status-dot" aria-hidden="true" />
      {label || statusLabel(status, t)}
    </span>
  )
}

export function Card({
  as: Component = "section",
  className,
  children,
  ...props
}: {
  as?: React.ElementType
  className?: string
  children?: React.ReactNode
  [key: string]: any
}) {
  return (
    <Component className={cn("adm-card", className)} {...props}>
      {children}
    </Component>
  )
}

export function CardHeader({
  title,
  description,
  icon: Icon,
  actions,
  id,
}: {
  title?: React.ReactNode
  description?: React.ReactNode
  icon?: React.ElementType
  actions?: React.ReactNode
  id?: string
}) {
  return (
    <div className="adm-card-header">
      {Icon ? (
        <span className="adm-card-icon" aria-hidden="true">
          <Icon />
        </span>
      ) : null}
      <div className="adm-card-heading">
        <h3 id={id}>{title}</h3>
        {description ? <p>{description}</p> : null}
      </div>
      {actions ? <div className="adm-card-actions">{actions}</div> : null}
    </div>
  )
}

export function PageHeader({
  title,
  description,
  actions,
  id,
}: {
  title?: React.ReactNode
  description?: React.ReactNode
  actions?: React.ReactNode
  id?: string
}) {
  return (
    <div className="adm-page-header">
      <div>
        <h2 id={id}>{title}</h2>
        {description ? <p>{description}</p> : null}
      </div>
      {actions ? <div className="adm-page-actions">{actions}</div> : null}
    </div>
  )
}

export function Field({
  label,
  hint,
  className,
  children,
  wide = false,
}: {
  label?: string
  hint?: React.ReactNode
  className?: string
  children?: React.ReactNode
  wide?: boolean
}) {
  return (
    <label className={cn("adm-field", wide && "is-wide", className)}>
      <span className="adm-field-label">{label}</span>
      {children}
      {hint ? <small className="adm-field-hint">{hint}</small> : null}
    </label>
  )
}

export function EmptyState({
  icon: Icon,
  title,
  children,
}: {
  icon?: React.ElementType
  title?: React.ReactNode
  children?: React.ReactNode
}) {
  return (
    <div className="adm-empty">
      {Icon ? (
        <span className="adm-empty-icon" aria-hidden="true">
          <Icon />
        </span>
      ) : null}
      <strong>{title}</strong>
      {children ? <p>{children}</p> : null}
    </div>
  )
}

/**
 * Accessible modal dialog with a focus trap, Escape to close, and focus
 * restoration. Clicking the backdrop closes it.
 */
export function Dialog({
  title,
  description,
  onClose,
  children,
  footer,
  size = "md",
  closeLabel = "Close",
}: {
  title?: React.ReactNode
  description?: React.ReactNode
  onClose: any
  children?: React.ReactNode
  footer?: React.ReactNode
  size?: string
  closeLabel?: string
}) {
  const dialogRef = useRef(null)
  const titleId = useId()
  const closeRef = useRef(onClose)
  closeRef.current = onClose

  useEffect(() => {
    const dialog = dialogRef.current
    const previousFocus = document.activeElement
    if (!dialog) return undefined
    const focusable = () =>
      [
        ...dialog.querySelectorAll(
          "button, input, select, textarea, [tabindex]:not([tabindex='-1'])",
        ),
      ].filter((element) => !element.disabled && element.offsetParent !== null)
    const preferred = dialog.querySelector("[data-autofocus]")
    ;(preferred || focusable()[1] || focusable()[0])?.focus()
    document.body.classList.add("adm-dialog-open")
    const handleKeyDown = (event) => {
      if (event.key === "Escape") {
        event.preventDefault()
        closeRef.current()
        return
      }
      if (event.key !== "Tab") return
      const elements = focusable()
      if (!elements.length) return
      const first = elements[0]
      const last = elements[elements.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    document.addEventListener("keydown", handleKeyDown)
    return () => {
      document.removeEventListener("keydown", handleKeyDown)
      document.body.classList.remove("adm-dialog-open")
      if (previousFocus instanceof HTMLElement) previousFocus.focus()
    }
  }, [])

  return (
    <div
      className="adm-dialog-backdrop"
      role="presentation"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <section
        ref={dialogRef}
        className={cn("adm-dialog", `adm-dialog-${size}`)}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
      >
        <header className="adm-dialog-header">
          <div>
            <h3 id={titleId}>{title}</h3>
            {description ? <p>{description}</p> : null}
          </div>
          <IconBtn label={closeLabel} icon={X} onClick={onClose} />
        </header>
        <div className="adm-dialog-body">{children}</div>
        {footer ? (
          <footer className="adm-dialog-footer">{footer}</footer>
        ) : null}
      </section>
    </div>
  )
}

export function Metric({
  label,
  value,
  note,
  tone = "neutral",
  icon: Icon,
}: {
  label?: string
  value: React.ReactNode
  note?: React.ReactNode
  tone?: string
  icon?: React.ElementType
}) {
  return (
    <div className={cn("adm-metric", `adm-tone-${tone}`)}>
      <div className="adm-metric-top">
        <span>{label}</span>
        {Icon ? <Icon aria-hidden="true" /> : null}
      </div>
      <strong>{value}</strong>
      {note ? <small>{note}</small> : null}
    </div>
  )
}
