import { useId, useState, type ComponentType, type ReactNode } from "react"
import { ChevronDown, type LucideIcon } from "lucide-react"

import { cn } from "@/lib/utils"

/**
 * A folded section: a header row that opens onto its settings. Controls in
 * the header, such as a quick switch or a status badge, work while it is
 * closed, and pressing anywhere else on the header opens or closes it. The
 * body stays mounted, so unsaved edits survive folding.
 */
export function Fold({
  icon: FoldIcon,
  title,
  level = 3,
  hint,
  controls,
  className,
  children,
}: {
  icon: LucideIcon | ComponentType<{ "aria-hidden"?: boolean }>
  title: string
  /** 4 for a fold inside a panel, such as a confirmation step. */
  level?: 3 | 4
  hint?: ReactNode
  controls?: ReactNode
  className?: string
  children: ReactNode
}) {
  const [open, setOpen] = useState(false)
  const id = useId()
  const Heading = level === 4 ? "h4" : "h3"
  return (
    <section
      className={cn("adm-fold", open && "is-open", className)}
      aria-labelledby={`${id}-title`}
    >
      <header
        className="adm-fold-head"
        onClick={(event) => {
          // The title button and the controls handle their own.
          if (
            !(event.target as Element).closest(
              "button, a, input, label, select",
            )
          )
            setOpen(!open)
        }}
      >
        <span className="adm-fold-mark">
          <FoldIcon aria-hidden />
        </span>
        <span className="adm-fold-title">
          <span className="adm-fold-name">
            <Heading id={`${id}-title`}>
              <button
                type="button"
                aria-expanded={open}
                aria-controls={`${id}-body`}
                onClick={() => setOpen(!open)}
              >
                {title}
              </button>
            </Heading>
          </span>
          {hint ? <small>{hint}</small> : null}
        </span>
        {controls ? (
          <span className="adm-fold-controls">{controls}</span>
        ) : null}
        <ChevronDown aria-hidden className="adm-fold-chevron" />
      </header>
      <div id={`${id}-body`} className="adm-fold-body" hidden={!open}>
        {children}
      </div>
    </section>
  )
}
