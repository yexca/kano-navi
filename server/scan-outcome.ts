/** Posts and image reviews are independent work within one schedule step. */
export function scheduleScanStatus(result) {
  const parts = [result, result?.assetVerification].filter(Boolean)
  const failed = parts.reduce((sum, part) => sum + Number(part.failed || 0), 0)
  if (!failed) return "success"
  const succeeded = parts.reduce(
    (sum, part) =>
      sum +
      Number(part.success || 0) +
      Number(part.cached || 0) +
      Number(part.uncertain || 0),
    0,
  )
  return succeeded ? "partial" : "failed"
}
