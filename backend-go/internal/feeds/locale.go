package feeds

import (
	"strings"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// Locale is a feed's language, one of the board's locales. English is the
// base; stored data stays in codes and English, and only rendering picks the
// wording below.
type Locale string

const (
	English  Locale = "en"
	Japanese Locale = "ja"
	Chinese  Locale = "zh-CN"
)

// ParseLocale reads a lang query value; a missing or unknown one is English.
func ParseLocale(value string) Locale {
	switch strings.TrimSpace(value) {
	case string(Japanese):
		return Japanese
	case string(Chinese):
		return Chinese
	}
	return English
}

// messages are the fixed strings feeds write. They follow the board's
// wording (src/i18n.ts) for the site name and generated details.
type messages struct {
	siteName     string
	approximate  string
	confirmed    string
	automatic    string
	unofficial   string
	youtubeLive  string
	youtubePrem  string
	atomSubtitle string
	published    string
	scheduled    string
	aired        string
	cancelled    string
}

var catalog = map[Locale]messages{
	English: {
		siteName:     SiteName,
		approximate:  "The start time is approximate.",
		confirmed:    "Confirmed by the site maintainer.",
		automatic:    "Read automatically from the original source; check it for changes.",
		unofficial:   "Unofficial fan-made schedule.",
		youtubeLive:  "YouTube Live",
		youtubePrem:  "YouTube Premiere",
		atomSubtitle: "Unofficial fan-made updates: Kano Mahoro's videos and streams.",
		published:    "Published %s.",
		scheduled:    "Scheduled for %s.",
		aired:        "Aired %s.",
		cancelled:    "Cancelled. ",
	},
	Japanese: {
		siteName:     "鹿乃ちゃん何してるか",
		approximate:  "開始時刻はおおよそです。",
		confirmed:    "サイト管理者が確認済みです。",
		automatic:    "元の情報から自動で読み取りました。変更がないか元の情報をご確認ください。",
		unofficial:   "非公式のファンメイド予定表です。",
		youtubeLive:  "YouTube ライブ",
		youtubePrem:  "YouTube プレミア公開",
		atomSubtitle: "非公式ファンメイド：鹿乃まほろの動画と配信の更新。",
		published:    "公開：%s。",
		scheduled:    "予定：%s。",
		aired:        "配信：%s。",
		cancelled:    "中止。",
	},
	Chinese: {
		siteName:     "鹿乃在做什么",
		approximate:  "开始时间为大致时间。",
		confirmed:    "已由本站维护者确认。",
		automatic:    "从原始来源自动读取，请以原始来源为准。",
		unofficial:   "非官方粉丝自制日程。",
		youtubeLive:  "YouTube 直播",
		youtubePrem:  "YouTube 首播",
		atomSubtitle: "非官方粉丝自制：鹿乃まほろ的视频与直播动态。",
		published:    "发布于 %s。",
		scheduled:    "预定于 %s。",
		aired:        "播出于 %s。",
		cancelled:    "已取消。",
	},
}

func (l Locale) messages() messages {
	if m, ok := catalog[l]; ok {
		return m
	}
	return catalog[English]
}

// localizedTitle is the wording a reader of the locale sees, as the board
// picks it: Japanese readers and untranslated wording keep the source.
func localizedTitle(source string, translations sqlite.TranslationSet, l Locale) string {
	if l == Japanese {
		return source
	}
	if translation, ok := translations.Locales[string(l)]; ok && strings.TrimSpace(translation.Text) != "" {
		return translation.Text
	}
	return source
}

// detailCodes are the generated YouTube details, including every stored form
// src/lib/event-text.ts recognizes; other detail text is an operator's
// or a model's and stays as written.
var detailCodes = map[string]string{
	"YouTube Live":          "youtube_live",
	"YouTube Premiere":      "youtube_premiere",
	"YouTube Live / 预约":     "youtube_live",
	"YouTube Premiere / 预约": "youtube_premiere",
	"メンバーシップ限定 / YouTube":   "youtube_live",
}

func localizedDetail(detail string, l Locale) string {
	switch detailCodes[detail] {
	case "youtube_live":
		return l.messages().youtubeLive
	case "youtube_premiere":
		return l.messages().youtubePrem
	}
	return detail
}
