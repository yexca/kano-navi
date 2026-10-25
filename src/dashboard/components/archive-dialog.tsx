import type { Locale, Translator } from "@/types"
import type { ReactNode } from "react"
import { useEffect, useRef } from "react"
import { ChevronLeft, ChevronRight, X } from "lucide-react"

import { EmptyNote, Placeholder } from "@/site/site-shell"
import { formatMonth, japanToday, monthStartOf } from "../format"

function shiftMonth(date: Date, offset: number) {
  return new Date(date.getFullYear(), date.getMonth() + offset, 1)
}

function dateFromKey(key: string) {
  const [year, month, day] = key.split("-").map(Number)
  return new Date(year, month - 1, day)
}

/**
 * Native modal sheet for browsing one month of stored records. Escape and the
 * backdrop close it, focus returns to the opener, and an image viewer opened
 * from inside stacks above it.
 */
export function ArchiveDialog({
  open,
  kicker,
  title,
  note,
  summary,
  monthStart,
  onMonthChange,
  latestMonth,
  toolbar,
  onClose,
  children,
  locale,
  t,
}: {
  open: boolean
  kicker: ReactNode
  title: string
  note?: ReactNode
  summary?: ReactNode
  monthStart: Date
  onMonthChange: (month: Date) => void
  /** The last month worth opening; later months are not offered. */
  latestMonth?: Date
  toolbar?: ReactNode
  onClose: () => void
  children: ReactNode
  locale: Locale
  t: Translator
}) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const closeRef = useRef<HTMLButtonElement>(null)
  const bodyRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const dialog = dialogRef.current
    if (!open || !dialog) return undefined
    const previous = document.activeElement as HTMLElement | null
    if (!dialog.open) dialog.showModal?.()
    closeRef.current?.focus()
    document.body.classList.add("has-sheet")
    return () => {
      if (dialog.open) dialog.close()
      document.body.classList.remove("has-sheet")
      previous?.focus?.()
    }
  }, [open])

  const monthKey = `${monthStart.getFullYear()}-${monthStart.getMonth()}`
  useEffect(() => {
    bodyRef.current?.scrollTo?.({ top: 0 })
  }, [monthKey])

  if (!open) return null
  const thisMonth = monthStartOf(japanToday())
  const isCurrent = monthStart.getTime() === thisMonth.getTime()
  const atLatest = latestMonth ? monthStart >= latestMonth : false
  const label = formatMonth(monthStart, locale)

  return (
    <dialog
      ref={dialogRef}
      className="archive-dialog"
      aria-labelledby="archive-dialog-title"
      onCancel={(event) => {
        event.preventDefault()
        onClose()
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className="archive-dialog-frame">
        <header className="archive-dialog-head">
          <div className="archive-dialog-heading">
            <p className="kicker">{kicker}</p>
            <h2 id="archive-dialog-title">{title}</h2>
            {note ? <p className="section-title-note">{note}</p> : null}
          </div>
          <button
            ref={closeRef}
            type="button"
            className="round-button"
            onClick={onClose}
            aria-label={t("common.close")}
            title={t("common.close")}
          >
            <X aria-hidden="true" />
          </button>
          <div className="archive-dialog-bar">
            <div className="archive-month" role="group" aria-label={label}>
              <button
                type="button"
                className="round-button"
                onClick={() => onMonthChange(shiftMonth(monthStart, -1))}
                aria-label={t("common.previousMonth")}
                title={t("common.previousMonth")}
              >
                <ChevronLeft aria-hidden="true" />
              </button>
              <strong className="archive-month-label" aria-live="polite">
                {label}
              </strong>
              <button
                type="button"
                className="round-button"
                disabled={atLatest}
                onClick={() => onMonthChange(shiftMonth(monthStart, 1))}
                aria-label={t("common.nextMonth")}
                title={t("common.nextMonth")}
              >
                <ChevronRight aria-hidden="true" />
              </button>
              <button
                type="button"
                className="pill-button"
                disabled={isCurrent}
                onClick={() => onMonthChange(thisMonth)}
              >
                {t("common.thisMonth")}
              </button>
              {summary ? (
                <span className="archive-summary">{summary}</span>
              ) : null}
            </div>
            {toolbar}
          </div>
        </header>
        <div className="archive-dialog-body" ref={bodyRef}>
          {children}
        </div>
      </div>
    </dialog>
  )
}

/**
 * Shared states of an archive body: an error with retry, the first load, and
 * an empty month that offers the nearest months holding records.
 */
export function ArchiveState({
  error,
  onRetry,
  loading,
  empty,
  emptyTitle,
  hint,
  adjacent,
  onJump,
  locale,
  t,
}: {
  error: Error | null
  onRetry: () => unknown
  loading: boolean
  empty: boolean
  emptyTitle: string
  hint?: string
  adjacent?: { previous: string | null; next: string | null }
  onJump: (month: Date) => void
  locale: Locale
  t: Translator
}) {
  const previous = adjacent?.previous ? dateFromKey(adjacent.previous) : null
  const next = adjacent?.next ? dateFromKey(adjacent.next) : null
  return (
    <>
      {error ? (
        <div className="inline-alert" role="alert">
          <span>{t("error.api")}</span>
          <button type="button" className="pill-button" onClick={onRetry}>
            {t("common.retry")}
          </button>
        </div>
      ) : null}
      {loading ? (
        <Placeholder lines={4} />
      ) : empty ? (
        <EmptyNote title={emptyTitle} hint={hint}>
          {previous || next ? (
            <div className="archive-jumps">
              {previous ? (
                <button
                  type="button"
                  className="pill-button"
                  onClick={() => onJump(monthStartOf(previous))}
                >
                  <ChevronLeft aria-hidden="true" />
                  {t("common.jumpToMonth", {
                    month: formatMonth(previous, locale),
                  })}
                </button>
              ) : null}
              {next ? (
                <button
                  type="button"
                  className="pill-button"
                  onClick={() => onJump(monthStartOf(next))}
                >
                  {t("common.jumpToMonth", {
                    month: formatMonth(next, locale),
                  })}
                  <ChevronRight aria-hidden="true" />
                </button>
              ) : null}
            </div>
          ) : null}
        </EmptyNote>
      ) : null}
    </>
  )
}

/** The archive's "load more" control, disabled while a page is in flight. */
export function ArchiveMore({
  hasNext,
  isLoading,
  label,
  onLoadMore,
  t,
}: {
  hasNext: boolean
  isLoading: boolean
  label: string
  onLoadMore: () => unknown
  t: Translator
}) {
  if (!hasNext) return null
  return (
    <div className="archive-more">
      <button
        type="button"
        className="pill-button"
        disabled={isLoading}
        onClick={onLoadMore}
      >
        {isLoading ? t("common.refreshing") : label}
      </button>
    </div>
  )
}
