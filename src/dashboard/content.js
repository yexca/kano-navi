// Static, public-facing content that does not come from synchronization.
// Every external host here is listed in scripts/privacy-allowlist.json.

export const POST_WINDOW_DAYS = 3

export const fallbackProfile = {
  displayName: "鹿乃まほろ",
  romanizedName: "Kano Mahoro",
  bio: "歌手 / Virtual Artist / みんなの毎日を、まほろばに。",
  avatarUrl: "/assets/kano-avatar.jpg",
  bannerUrl: "/assets/kano-banner.jpg",
  xUrl: "https://x.com/kano_2525",
  youtubeUrl: "https://www.youtube.com/channel/UCShXNLMXCfstmWKH_q86B8w",
}

export const sections = [
  { id: "now", labelKey: "nav.now" },
  { id: "schedule", labelKey: "nav.schedule" },
  { id: "feed", labelKey: "nav.feed" },
  { id: "videos", labelKey: "nav.videos" },
  { id: "archive", labelKey: "nav.archive" },
]

export const hashtags = [
  {
    tag: "鹿乃まほろ",
    url: "https://x.com/hashtag/%E9%B9%BF%E4%B9%83%E3%81%BE%E3%81%BB%E3%82%8D",
  },
  { tag: "鹿友", url: "https://x.com/hashtag/%E9%B9%BF%E5%8F%8B" },
  {
    tag: "まほろたいむ",
    url: "https://x.com/hashtag/%E3%81%BE%E3%81%BB%E3%82%8D%E3%81%9F%E3%81%84%E3%82%80",
  },
  {
    tag: "まほろくりっぷ",
    url: "https://x.com/hashtag/%E3%81%BE%E3%81%BB%E3%82%8D%E3%81%8F%E3%82%8A%E3%81%A3%E3%81%B7",
  },
]

export const archiveLinks = {
  wikipedia: "https://ja.wikipedia.org/wiki/%E9%B9%BF%E4%B9%83",
  mylist: "https://www.nicovideo.jp/user/15078610/mylist/16997570",
}
