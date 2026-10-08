import { ArrowUpRight } from "lucide-react"

export function ExternalLink({
  href,
  className,
  children,
  ...props
}: {
  href: string
  className?: string
  children?: React.ReactNode
  [key: string]: any
}) {
  return (
    <a
      className={className}
      href={href}
      target="_blank"
      rel="noreferrer noopener"
      {...props}
    >
      {children}
    </a>
  )
}

export function SectionHeading({
  id,
  eyebrow,
  title,
  subtitle,
  children,
}: {
  id?: string
  eyebrow?: React.ReactNode
  title?: React.ReactNode
  subtitle?: React.ReactNode
  children?: React.ReactNode
}) {
  return (
    <div className="section-heading">
      <div className="section-heading-copy">
        {eyebrow ? <p className="eyebrow">{eyebrow}</p> : null}
        <h2 id={id}>{title}</h2>
        {subtitle ? <p className="section-subtitle">{subtitle}</p> : null}
      </div>
      {children ? (
        <div className="section-heading-actions">{children}</div>
      ) : null}
    </div>
  )
}

export function PillLink({
  href,
  children,
  icon: Icon,
  className = "",
}: {
  href: string
  children?: React.ReactNode
  icon?: React.ElementType
  className?: string
}) {
  return (
    <ExternalLink href={href} className={`pill-link ${className}`.trim()}>
      {Icon ? <Icon className="pill-link-icon" aria-hidden="true" /> : null}
      <span>{children}</span>
      <ArrowUpRight className="pill-link-arrow" aria-hidden="true" />
    </ExternalLink>
  )
}

export function Skeleton({ lines = 3 }: { lines?: number }) {
  return (
    <div className="skeleton" aria-hidden="true">
      {Array.from({ length: lines }, (_, index) => (
        <span key={index} style={{ width: `${92 - index * 14}%` }} />
      ))}
    </div>
  )
}

export function EmptyState({
  icon: Icon,
  title,
  hint,
  children,
}: {
  icon?: React.ElementType
  title?: React.ReactNode
  hint?: React.ReactNode
  children?: React.ReactNode
}) {
  return (
    <div className="empty-state">
      {Icon ? (
        <span className="empty-state-icon" aria-hidden="true">
          <Icon />
        </span>
      ) : null}
      <p className="empty-state-title">{title}</p>
      {hint ? <p className="empty-state-hint">{hint}</p> : null}
      {children}
    </div>
  )
}

export function XLogo(props) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" {...props}>
      <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z" />
    </svg>
  )
}

export function YouTubeLogo(props) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" {...props}>
      <path d="M23 7.2a3 3 0 0 0-2.1-2.1C19 4.6 12 4.6 12 4.6s-7 0-8.9.5A3 3 0 0 0 1 7.2 31 31 0 0 0 .5 12 31 31 0 0 0 1 16.8a3 3 0 0 0 2.1 2.1c1.9.5 8.9.5 8.9.5s7 0 8.9-.5a3 3 0 0 0 2.1-2.1 31 31 0 0 0 .5-4.8 31 31 0 0 0-.5-4.8ZM9.7 15.1V8.9L15.5 12Z" />
    </svg>
  )
}
