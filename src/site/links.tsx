import type { ComponentProps, SVGProps } from "react"

/** Original platform pages always open in a new tab without a referrer. */
export function ExternalLink({ children, ...props }: ComponentProps<"a">) {
  return (
    <a target="_blank" rel="noreferrer noopener" {...props}>
      {children}
    </a>
  )
}

export function XLogo(props: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" {...props}>
      <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z" />
    </svg>
  )
}

/** A plain television mark for bilibili links, drawn in currentColor. */
export function BilibiliLogo(props: SVGProps<SVGSVGElement>) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.2"
      strokeLinecap="round"
      strokeLinejoin="round"
      {...props}
    >
      <path d="m7.5 2.8 2.4 2.6M16.5 2.8l-2.4 2.6" />
      <rect x="2.6" y="5.6" width="18.8" height="14.2" rx="3.6" />
      <path d="m7.6 11.2 2.4-.9M16.4 11.2l-2.4-.9M9.2 15.4c.9.7 1.9.7 2.8 0 .9.7 1.9.7 2.8 0" />
    </svg>
  )
}

export function YouTubeLogo(props: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" {...props}>
      <path d="M23 7.2a3 3 0 0 0-2.1-2.1C19 4.6 12 4.6 12 4.6s-7 0-8.9.5A3 3 0 0 0 1 7.2 31 31 0 0 0 .5 12 31 31 0 0 0 1 16.8a3 3 0 0 0 2.1 2.1c1.9.5 8.9.5 8.9.5s7 0 8.9-.5a3 3 0 0 0 2.1-2.1 31 31 0 0 0 .5-4.8 31 31 0 0 0-.5-4.8ZM9.7 15.1V8.9L15.5 12Z" />
    </svg>
  )
}
