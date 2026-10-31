type MessageCatalog = Record<
  string,
  string | string[] | ((values: any) => string)
>

export type PageMessages = Partial<
  Record<import("./types.ts").Locale, MessageCatalog>
>

export const localeOptions = [
  { value: "en", label: "English", htmlLang: "en" },
  { value: "ja", label: "日本語", htmlLang: "ja" },
  { value: "zh-CN", label: "简体中文", htmlLang: "zh-CN" },
]

const messages = {
  "zh-CN": {
    "site.name": "鹿乃在做什么",
    "header.backToTop": "回到顶部",
    "header.unofficial": "非官方",
    "header.language": "切换语言",
    "header.interfaceLanguage": "界面语言",
    "header.contentLanguage": "内容语言",
    "header.contentSameAsInterface": "与界面相同",
    "header.switchToDark": "切换深色模式",
    "header.switchToLight": "切换浅色模式",
    "header.pages": "页面",
    "header.japanTime": "日本时间",
    "visitorRegion.title": "中国大陆站点",
    "visitorRegion.message":
      "检测到您貌似处于中国大陆，是否访问由 {provider} 提供的中国大陆服务器站点？",
    "visitorRegion.visit": "访问",
    "visitorRegion.dismiss": "关闭",
    "nav.now": "近况",
    "hero.since": "2010 年起的歌声",
    "page.loading": "正在加载页面…",
    "page.error": "页面暂时无法加载。请重试。",
    "page.retry": "重新加载页面",
    "page.notFound": "找不到这个页面。",
    "page.home": "回到首页",
    "about.label": "关于本站",
    "lineblog.label": "Line Blog",
    "common.close": "关闭",
    "common.all": "全部",
    "feed.previousImage": "上一张",
    "feed.nextImage": "下一张",
    "translation.machine": "AI 翻译",
    "translation.machineShort": "AI",
    "translation.machineHint": "由 AI 模型自动翻译，可能有误。",
    "translation.manual": "人工翻译",
    "translation.platform.youtube": "YouTube 提供的翻译",
    "translation.platform.bilibili": "哔哩哔哩标题",
    "translation.platform.other": "平台提供的翻译",
    "translation.originalShown": "正在显示原文",
    "translation.showOriginal": "显示原文",
    "translation.showTranslation": "显示译文",
    "translation.originalIs": "原文：{text}",
    "translation.translated": "译文",
    "translation.original": "原文",
    "schedule.weekLabel": ({ range }: { range: any }) => `${range} 周视图`,
    "schedule.detail.youtube_live": "YouTube 直播",
    "schedule.detail.youtube_premiere": "YouTube 首播",
    "footer.fanMade": "粉丝制作的非官方整理页",
    "footer.disclaimer":
      "与本人及所属事务所无关 · 内容来自本地快照 · 一切以各平台原页面为准",
    "footer.backToTop": "回到顶部",
    "archive.fullHistory": "完整历程",
    "history.metaTitle": "历程 · 鹿乃在做什么",
    "history.metaDescription":
      "鹿乃从 2010 年翻唱投稿到 2026 年加入ミリプロSONA 的公开足迹，附官方来源。",
    "history.back": "回到近况",
    "history.eyebrow": "历程",
    "history.title": "从歌い手到 Virtual Artist",
    "history.lead":
      "从 2010 年的第一首翻唱、2015 年的主流出道，经过花寄女子寮、鹿乃まほろ与 MKLNtic，到 2026 年加入ミリプロSONA。这里按时间排列鹿乃的公开足迹，日期保留原有精度，并附上公开来源。",
    "history.stat.yearsLabel": "自 2010 年",
    "history.stat.years": ({ count }: { count: any }) => `${count} 年`,
    "history.stat.milestones": "收录节点",
    "history.stat.releases": "作品发行",
    "history.filter": "按活动身份筛选",
    "history.branchSince": ({ year }: { year: any }) => `${year} 起`,
    "history.source": "来源",
    "history.archiveOnly": "依据官方配信存档",
    "history.wholeYear": "这一年",
    "history.now": "现在",
    "history.nowTitle": "故事还在继续",
    "history.nowDetail": "之后的配信、日程与动态，请看近况页。",
    "history.empty": "这个身份暂时没有收录节点。",
    "history.note":
      "资料整理自本人官网、唱片公司、事务所与制作方的公开页面。歌手、直播角色与音乐组合的活动彼此并行，同一年可能出现多个身份。发行以唱片公司公布的发售或配信日为准。",
    "history.disclaimer":
      "与本人及所属事务所无关 · 节点为人工整理，可能不完整 · 一切以各来源原页面为准",
    "history.kind.start": "起点",
    "history.kind.debut": "出道",
    "history.kind.release": "发行",
    "history.kind.character": "角色公开",
    "history.kind.outfit": "新形象",
    "history.kind.model3d": "3D",
    "history.kind.stream": "配信",
    "history.kind.group": "组合",
    "history.kind.live": "演出",
    "history.kind.collab": "合作",
    "history.kind.agency": "所属",
    "relative.justNow": "刚刚",
  },
  ja: {
    "site.name": "鹿乃ちゃん何してるか",
    "header.backToTop": "ページ上部へ",
    "header.unofficial": "非公式",
    "header.language": "言語を切り替える",
    "header.interfaceLanguage": "表示言語",
    "header.contentLanguage": "コンテンツの言語",
    "header.contentSameAsInterface": "表示言語と同じ",
    "header.switchToDark": "ダークモードに切り替える",
    "header.switchToLight": "ライトモードに切り替える",
    "header.pages": "ページ",
    "header.japanTime": "日本時間",
    "visitorRegion.title": "中国大陸向けサイト",
    "visitorRegion.message":
      "中国大陸にいるようです。{provider} が提供する中国大陸向けサイトを表示しますか？",
    "visitorRegion.visit": "アクセス",
    "visitorRegion.dismiss": "閉じる",
    "nav.now": "いま",
    "hero.since": "2010 年から歌い続けて",
    "page.loading": "ページを読み込み中…",
    "page.error": "ページを読み込めませんでした。もう一度お試しください。",
    "page.retry": "ページを再読み込み",
    "page.notFound": "このページは見つかりませんでした。",
    "page.home": "トップへ戻る",
    "about.label": "このサイトについて",
    "lineblog.label": "Line Blog",
    "common.close": "閉じる",
    "common.all": "すべて",
    "feed.previousImage": "前の画像",
    "feed.nextImage": "次の画像",
    "translation.machine": "AI 翻訳",
    "translation.machineShort": "AI",
    "translation.machineHint":
      "AI モデルによる自動翻訳です。誤りを含む場合があります。",
    "translation.manual": "編集者による翻訳",
    "translation.platform.youtube": "YouTube の翻訳",
    "translation.platform.bilibili": "bilibili のタイトル",
    "translation.platform.other": "プラットフォームの翻訳",
    "translation.originalShown": "原文を表示中",
    "translation.showOriginal": "原文を表示",
    "translation.showTranslation": "翻訳を表示",
    "translation.originalIs": "原文：{text}",
    "translation.translated": "翻訳",
    "translation.original": "原文",
    "schedule.weekLabel": ({ range }: { range: any }) => `${range} の週`,
    "schedule.detail.youtube_live": "YouTube ライブ",
    "schedule.detail.youtube_premiere": "YouTube プレミア公開",
    "footer.fanMade": "ファンによる非公式まとめ",
    "footer.disclaimer":
      "ご本人・所属事務所とは無関係です · ローカルスナップショットを表示 · 各プラットフォームの元ページが正です",
    "footer.backToTop": "ページ上部へ",
    "archive.fullHistory": "あゆみを全部見る",
    "history.metaTitle": "あゆみ · 鹿乃ちゃん何してるか",
    "history.metaDescription":
      "2010 年の歌ってみた投稿から 2026 年のミリプロSONA 所属まで、鹿乃の公開されたあゆみを出典つきで。",
    "history.back": "いまのまほろへ",
    "history.eyebrow": "あゆみ",
    "history.title": "歌い手から Virtual Artist へ",
    "history.lead":
      "2010 年の最初の歌ってみた、2015 年のメジャーデビュー、花寄女子寮、鹿乃まほろ、MKLNtic を経て、2026 年のミリプロSONA 所属へ。鹿乃の公開されたあゆみを時系列で並べました。日付は元の精度のまま、出典を添えています。",
    "history.stat.yearsLabel": "2010 年から",
    "history.stat.years": ({ count }: { count: any }) => `${count} 年`,
    "history.stat.milestones": "収録した出来事",
    "history.stat.releases": "リリース",
    "history.filter": "活動名義で絞り込む",
    "history.branchSince": ({ year }: { year: any }) => `${year}〜`,
    "history.source": "出典",
    "history.archiveOnly": "公式配信アーカイブによる",
    "history.wholeYear": "この年",
    "history.now": "いま",
    "history.nowTitle": "物語はまだ続く",
    "history.nowDetail": "このあとの配信・予定・ポストはトップページへ。",
    "history.empty": "この名義の出来事はまだ収録していません。",
    "history.note":
      "ご本人の公式サイト、レーベル、事務所、制作者の公開ページをもとに整理しています。歌手・配信キャラクター・音楽ユニットの活動は並行しており、同じ年に複数の名義が登場します。リリースはレーベル発表の発売日・配信日に基づきます。",
    "history.disclaimer":
      "ご本人・所属事務所とは無関係です · 手作業の整理のため網羅的ではありません · 各出典の元ページが正です",
    "history.kind.start": "はじまり",
    "history.kind.debut": "デビュー",
    "history.kind.release": "リリース",
    "history.kind.character": "キャラクター公開",
    "history.kind.outfit": "新衣装",
    "history.kind.model3d": "3D",
    "history.kind.stream": "配信",
    "history.kind.group": "ユニット",
    "history.kind.live": "ライブ",
    "history.kind.collab": "コラボ",
    "history.kind.agency": "所属",
    "relative.justNow": "たった今",
  },
  en: {
    "site.name": "What’s Kano Up To?",
    "header.backToTop": "Back to top",
    "header.unofficial": "Unofficial",
    "header.language": "Change language",
    "header.interfaceLanguage": "Interface language",
    "header.contentLanguage": "Content language",
    "header.contentSameAsInterface": "Same as interface",
    "header.switchToDark": "Switch to dark mode",
    "header.switchToLight": "Switch to light mode",
    "header.pages": "Pages",
    "header.japanTime": "Japan time",
    "visitorRegion.title": "Mainland China site",
    "visitorRegion.message":
      "It looks like you may be in mainland China. Would you like to visit the mainland China site provided by {provider}?",
    "visitorRegion.visit": "Visit",
    "visitorRegion.dismiss": "Dismiss",
    "nav.now": "Now",
    "hero.since": "Singing since 2010",
    "page.loading": "Loading the page…",
    "page.error": "This page could not be loaded. Please try again.",
    "page.retry": "Reload page",
    "page.notFound": "This page doesn’t exist.",
    "page.home": "Back to the board",
    "about.label": "About",
    "lineblog.label": "Line Blog",
    "common.close": "Close",
    "common.all": "All",
    "feed.previousImage": "Previous image",
    "feed.nextImage": "Next image",
    "translation.machine": "AI translation",
    "translation.machineShort": "AI",
    "translation.machineHint":
      "Translated automatically by an AI model; it may contain mistakes.",
    "translation.manual": "Translated by the editor",
    "translation.platform.youtube": "Translation from YouTube",
    "translation.platform.bilibili": "Title from Bilibili",
    "translation.platform.other": "Platform translation",
    "translation.originalShown": "Showing the original",
    "translation.showOriginal": "Show original",
    "translation.showTranslation": "Show translation",
    "translation.originalIs": "Original: {text}",
    "translation.translated": "Translated",
    "translation.original": "Original",
    "schedule.weekLabel": ({ range }: { range: any }) => `Week of ${range}`,
    "schedule.detail.youtube_live": "YouTube Live",
    "schedule.detail.youtube_premiere": "YouTube Premiere",
    "footer.fanMade": "A fan-made, unofficial board",
    "footer.disclaimer":
      "Not affiliated with the artist or agency · Shows a local snapshot · Original platform pages are authoritative",
    "footer.backToTop": "Back to top",
    "archive.fullHistory": "Full journey",
    "history.metaTitle": "Journey · What’s Kano Up To?",
    "history.metaDescription":
      "Kano's public milestones from the first 2010 covers to joining Milpro SONA in 2026, with sources.",
    "history.back": "Back to the board",
    "history.eyebrow": "Journey",
    "history.title": "From utaite to Virtual Artist",
    "history.lead":
      "From the first covers in 2010 and the 2015 major debut, through Hanayori Joshiryo, Kano Mahoro, and MKLNtic, to joining Milpro SONA in 2026. Kano's public milestones in order, each date kept at its original precision and linked to a public source.",
    "history.stat.yearsLabel": "Since 2010",
    "history.stat.years": ({ count }: { count: any }) => `${count} yrs`,
    "history.stat.milestones": "Milestones",
    "history.stat.releases": "Releases",
    "history.filter": "Filter by identity",
    "history.branchSince": ({ year }: { year: any }) => `since ${year}`,
    "history.source": "Source",
    "history.archiveOnly": "From the official stream archive",
    "history.wholeYear": "This year",
    "history.now": "Now",
    "history.nowTitle": "The story continues",
    "history.nowDetail":
      "For later streams, schedules, and posts, see the board.",
    "history.empty": "No milestones for this identity yet.",
    "history.note":
      "Compiled from the artist's official site and public pages from labels, the agency, and creators. Singer, streaming-character, and music-unit activities run in parallel, so one year can show several identities. Releases use the label's release or distribution date.",
    "history.disclaimer":
      "Not affiliated with the artist or agency · Hand-curated and not exhaustive · Each source page is authoritative",
    "history.kind.start": "Beginning",
    "history.kind.debut": "Debut",
    "history.kind.release": "Release",
    "history.kind.character": "Character reveal",
    "history.kind.outfit": "New look",
    "history.kind.model3d": "3D",
    "history.kind.stream": "Stream",
    "history.kind.group": "Unit",
    "history.kind.live": "Live",
    "history.kind.collab": "Collab",
    "history.kind.agency": "Agency",
    "relative.justNow": "just now",
  },
}

export function detectLocale(languages = []) {
  const candidates = Array.isArray(languages) ? languages : [languages]

  for (const language of candidates) {
    const normalized = String(language || "").toLowerCase()
    if (normalized === "zh" || normalized.startsWith("zh-")) return "zh-CN"
    if (normalized === "ja" || normalized.startsWith("ja-")) return "ja"
    if (normalized === "en" || normalized.startsWith("en-")) return "en"
  }

  return "en"
}

export function createTranslator(
  locale: import("./types.ts").Locale,
  pageMessages: PageMessages = {},
): import("./types.ts").Translator {
  const selectedMessages: MessageCatalog = {
    ...messages[locale],
    ...(pageMessages[locale] || {}),
  }
  const fallbackMessages: MessageCatalog = {
    ...messages.en,
    ...pageMessages.en,
  }

  return (key, values: Record<string, any> = {}) => {
    const message = selectedMessages[key] ?? fallbackMessages[key] ?? key
    if (typeof message === "function") return message(values)
    // List messages such as "schedule.weekdays" are returned for callers to index.
    if (typeof message !== "string") return message as unknown as string

    return message.replace(
      /\{(\w+)\}/g,
      (_, name) => values[name] ?? `{${name}}`,
    )
  }
}
