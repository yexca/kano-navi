const SEAL = "#4b1f0e"
const CREAM = "#f7e4c9"
const SCARF = "#e3173f"

/** A wax-seal rim: a circle whose radius swells into soft lobes. */
function sealEdge() {
  const points: string[] = []
  for (let step = 0; step < 96; step += 1) {
    const angle = (step / 96) * Math.PI * 2
    const radius =
      45.5 + 2.4 * Math.sin(angle * 9 + 0.6) + 0.8 * Math.sin(angle * 4)
    const x = 50 + radius * Math.cos(angle)
    const y = 50 + radius * Math.sin(angle)
    points.push(`${x.toFixed(2)} ${y.toFixed(2)}`)
  }
  return `M${points.join("L")}Z`
}

const SEAL_EDGE = sealEdge()

/** The deer in a red scarf on a chocolate wax seal. */
export function KanoSeal({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 100 100"
      aria-hidden="true"
      focusable="false"
    >
      <path d={SEAL_EDGE} fill={SEAL} />
      <circle
        cx="50"
        cy="50"
        r="36"
        fill="none"
        stroke={CREAM}
        strokeWidth="1.6"
      />
      <g fill="none" stroke={CREAM} strokeLinecap="round" strokeWidth="3.4">
        <path d="M44.5 41Q42.5 31.5 39.5 24" />
        <path d="M42.6 33.5 37.8 30.8" />
        <path d="M55.5 41Q57.5 31.5 60.5 24" />
        <path d="M57.4 33.5 62.2 30.8" />
      </g>
      <g fill={CREAM}>
        <ellipse
          cx="32"
          cy="38"
          rx="9.5"
          ry="5.2"
          transform="rotate(24 32 38)"
        />
        <ellipse
          cx="68"
          cy="38"
          rx="9.5"
          ry="5.2"
          transform="rotate(-24 68 38)"
        />
      </g>
      <g fill={SCARF} stroke={SCARF} strokeLinejoin="round" strokeWidth="1.2">
        <path d="M37 66.5 30.5 58.5 28.4 62Z" />
        <path d="M37 67.5 26.4 64.2 26.8 69.6Z" />
        <path d="M36 66Q50 63 64 64.5L64.6 70Q63.4 74 59 74Q50 75.6 41 74Q37.2 73.4 36 70Z" />
      </g>
      <path
        d="M36 45Q36 40.5 40.5 40.5H59.5Q64 40.5 64 45V58.5Q64 61.8 61 63.8L50 70.6 39 63.8Q36 61.8 36 58.5Z"
        fill={CREAM}
      />
      <g fill={SEAL}>
        <ellipse cx="41.6" cy="56" rx="1.9" ry="2.6" />
        <ellipse cx="58.4" cy="56" rx="1.9" ry="2.6" />
        <path d="M48.3 62.4H51.7L50 64.8Z" />
      </g>
    </svg>
  )
}
