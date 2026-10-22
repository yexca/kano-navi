import type { Locale } from "../types.ts"

// Localized copy for the static timeline and link seeds in
// backend-go/internal/store/sqlite/static-seed.json. SQLite stores one text
// per row, so the board looks the stored text up here by its exact seed
// wording. A row whose text no longer matches its seed shows as stored.
// Brand names that read the same in every language need no entry.
const seedCopy: Record<string, Partial<Record<Locale, string>>> = {
  // Timeline titles and details.
  鹿乃まほろ: { en: "Kano Mahoro" },
  "8 月加入ミリプロSONA，以新形象开始配信。": {
    ja: "8 月にミリプロSONA へ加入し、新しい姿で配信を開始。",
    en: "Joined Milpro SONA in August and began streaming with a new look.",
  },
  "与ろん组成音乐组合。": {
    ja: "ろんと音楽ユニットを結成。",
    en: "Formed a music unit with Ron.",
  },
  "以粉发双马尾的まほろ形象开始配信。": {
    ja: "ピンク髪ツインテールのまほろとして配信を開始。",
    en: "Began streaming as Mahoro, with pink twin tails.",
  },
  花寄女子寮: { en: "Hanayori Joshiryo" },
  "以花寄女子寮成员身份开始 VTuber 活动。": {
    ja: "花寄女子寮のメンバーとして VTuber 活動を開始。",
    en: "Started VTuber activities as a member of Hanayori Joshiryo.",
  },
  "Major debut": { "zh-CN": "主流出道", ja: "メジャーデビュー" },
  "以《Stella-rium》正式出道。": {
    ja: "「Stella-rium」でメジャーデビュー。",
    en: "Debuted with “Stella-rium”.",
  },
  第一首投稿: { ja: "はじめての投稿", en: "First upload" },
  "从 NicoNico 的歌唱投稿开始。": {
    ja: "ニコニコ動画への歌ってみた投稿から。",
    en: "Started with song covers on NicoNico.",
  },

  // Link titles and details.
  "@kano_2525 · 近况首发": {
    ja: "@kano_2525 · 最新情報はまずここ",
    en: "@kano_2525 · News first",
  },
  "@_Kanotic · 个人向": {
    ja: "@_Kanotic · プライベート",
    en: "@_Kanotic · Personal",
  },
  "鹿乃まほろ · 配信与歌": {
    ja: "鹿乃まほろ · 配信と歌",
    en: "Kano Mahoro · Streams and songs",
  },
  "鹿乃 / MKLNtic · 旧频道": {
    ja: "鹿乃 / MKLNtic · 以前のチャンネル",
    en: "Kano / MKLNtic · Earlier channel",
  },
  "鹿乃ちゃん · 中文圈": {
    ja: "鹿乃ちゃん · 中国語圏向け",
    en: "Kano-chan · Chinese-speaking fans",
  },
  直播存档与通知: {
    ja: "配信アーカイブとお知らせ",
    en: "Stream archives and notices",
  },
  "ミリプロ / SONA": { en: "Milpro / SONA" },
  "事务所个人页 · 资料与公告": {
    ja: "事務所のタレントページ · プロフィールとお知らせ",
    en: "Agency talent page · Profile and news",
  },
  早期投稿存档: { ja: "初期の投稿アーカイブ", en: "Early upload archive" },
  "official site": {
    "zh-CN": "官方网站",
    ja: "公式サイト",
    en: "Official site",
  },
  "ミリプロ shop": { en: "Milpro shop" },
  事务所官方商店: { ja: "事務所公式ショップ", en: "Official agency shop" },
}

/** The seed text in `locale`, or `text` itself when it has no translation. */
export function localizeSeedText(text: string | null, locale: Locale) {
  return text == null ? text : (seedCopy[text]?.[locale] ?? text)
}
