import * as React from "react"
import { cva } from "class-variance-authority"
import { cn } from "@/lib/utils"

const badgeVariants = cva(
  "inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2",
  {
    variants: {
      variant: {
        default: "border-transparent bg-ink text-white",
        secondary: "border-transparent bg-secondary text-secondary-foreground",
        outline: "text-foreground",
        coral: "border-transparent bg-coral/15 text-[#b43d48]",
        blue: "border-transparent bg-periwinkle/20 text-[#4e5da5]",
        mint: "border-transparent bg-lagoon/60 text-[#28745c]",
        butter: "border-transparent bg-butter/45 text-[#765a1a]",
      },
    },
    defaultVariants: { variant: "default" },
  },
)

function Badge({ className, variant, ...props }) {
  return (
    <div className={cn(badgeVariants({ variant }), className)} {...props} />
  )
}

export { Badge, badgeVariants }
