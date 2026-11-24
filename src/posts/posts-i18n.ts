import { postMessages } from "@/dashboard/post-i18n"

// The page reuses the board's post wording (filters, cards, the viewer), not
// the rest of the board's copy, and adds its own heading and link preview.
const pageMessages = {
  "zh-CN": {
    "posts.metaTitle": "动态 · 鹿乃在做什么",
    "posts.metaDescription":
      "鹿乃 2010 年以来在 X 和哔哩哔哩上的动态，按月份和账号浏览。",
    "posts.skip": "跳到动态列表",
    "posts.title": "每一条动态",
    "posts.months": "按月份浏览",
    "posts.newer": "更新的动态",
    "posts.loadMore": "加载更早的动态",
    "posts.empty": "没有已保存的动态",
  },
  ja: {
    "posts.metaTitle": "ポスト · 鹿乃ちゃん何してるか",
    "posts.metaDescription":
      "2010 年からの鹿乃の X と bilibili のポストを、月とアカウントごとに。",
    "posts.skip": "ポスト一覧へスキップ",
    "posts.title": "これまでのポスト",
    "posts.months": "月ごとに見る",
    "posts.newer": "新しいポスト",
    "posts.loadMore": "以前のポストを読み込む",
    "posts.empty": "保存されたポストはありません",
  },
  en: {
    "posts.metaTitle": "Posts · What’s Kano Up To?",
    "posts.metaDescription":
      "Kano's posts on X and bilibili since 2010, by month and by account.",
    "posts.skip": "Skip to the posts",
    "posts.title": "Every post",
    "posts.months": "Browse by month",
    "posts.newer": "Newer posts",
    "posts.loadMore": "Load earlier posts",
    "posts.empty": "No stored posts",
  },
}

export const postsMessages = {
  "zh-CN": { ...postMessages["zh-CN"], ...pageMessages["zh-CN"] },
  ja: { ...postMessages.ja, ...pageMessages.ja },
  en: { ...postMessages.en, ...pageMessages.en },
}
