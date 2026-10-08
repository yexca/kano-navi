import catalog from "./media-catalog.json" with { type: "json" }

// Curated fixed images ship in public/assets/history and the production bundle.
// Changing source media still uses the runtime cache; visits stay local.
export const historyMedia = catalog.map((item) => ({
  ...item,
  url: `/assets/history/${item.filename}?v=${item.sha256}`,
}))

export const mediaByMilestone = historyMedia.reduce((groups, item) => {
  if (item.milestoneId) (groups[item.milestoneId] ??= []).push(item)
  return groups
}, {})

export const branchPortraits = {
  singer: "03_2021_official_keyvisual",
  hanayori: "01_2019_artist_intro",
  mahoro: "03_mahoro_2021_model_sheet",
  mklntic: "01_mahoro_2022_mklntic_outfit",
  sona: "12_sona_2026_official_fullbody",
}

export function localized(value, locale) {
  return value?.[locale] ?? value?.en ?? null
}

export function sourceName(href, t) {
  const host = new URL(href).hostname
  if (host.endsWith("amebaownd.com")) return t("history.sourceName.artistSite")
  if (host.endsWith("teichiku.co.jp")) return "TEICHIKU"
  if (host.endsWith("imperialrecords.jp")) return "IMPERIAL RECORDS"
  if (host.endsWith("whv-amusic.com")) return "Warner"
  if (host.endsWith("sonymusic.co.jp")) return "Sony Music"
  if (host.endsWith("youtube.com")) return "YouTube"
  if (host.endsWith("bilibili.com")) return "Bilibili"
  if (host.endsWith("x.com")) return t("history.sourceName.x")
  if (host.endsWith("booth.pm")) return "BOOTH"
  if (host === "shop.milpr.com") return t("history.sourceName.shop")
  if (host.endsWith("milpr.com")) return t("history.sourceName.agency")
  if (host.endsWith("prtimes.jp")) return "PR TIMES"
  if (host === "archive.ragtag.moe") return t("history.sourceName.archive")
  return host.replace(/^www\./u, "")
}
