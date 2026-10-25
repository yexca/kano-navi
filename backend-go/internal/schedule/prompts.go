package schedule

// These strings define model behaviour and are copied verbatim from
// server/schedule-extractor.ts. extractorVersion is part of stored extraction
// identities, so changing it reprocesses every known post.
const (
	extractorVersion     = "openai-schedule-v4"
	defaultOpenAIBaseURL = "https://api.openai.com/v1"

	protocolResponses = "openai-responses"
	protocolChat      = "openai-chat-completions"
	protocolAnthropic = "anthropic-messages"

	// routeMessage and routeBoard record a post's detection path; every path
	// draws its models from the one priority queue stored as routeQueue.
	routeMessage = "schedule_message"
	routeBoard   = "schedule_board"
	routeQueue   = "schedule"
	// routeTranslate is the separate translation queue; it never falls back
	// to the schedule queue.
	routeTranslate = "translate"

	llmMaxRetries = 2

	extractionToolName   = "kano_schedule_extraction"
	verificationToolName = "kano_schedule_asset_verification"
	translationToolName  = "kano_text_translation"

	translationInstructions = "Translate one X post or YouTube video title by Kano Mahoro (鹿乃まほろ), a Japanese virtual singer, for readers of an unofficial fan status board. Set sourceLanguage to the language of the text: ja, en, zh, ko or other, or none when it has nothing to translate (only links, hashtags, @mentions, emoji or kaomoji). Translate the whole text into natural English (english) and Simplified Chinese (simplifiedChinese). Keep URLs, hashtags, @mentions, emoji, kaomoji, bracketed channel tags and line breaks as written. Use the usual romanization or official names of people, groups and games, and keep a song title in its original wording unless it has an official translation. Each translation field is the complete text to display, not a list of changes: when a song title or name stays unchanged, include that original wording in the non-empty result. For sourceLanguage ja, ko or other, both english and simplifiedChinese must be non-empty, even when the whole title stays unchanged. Do not add explanations, quotation marks or notes. Return an empty string only for english when sourceLanguage is en, only for simplifiedChinese when sourceLanguage is zh, and for both when sourceLanguage is none."

	translationSchema rawJSON = `{"type":"object","additionalProperties":false,"properties":{"sourceLanguage":{"type":"string","enum":["ja","en","zh","ko","other","none"]},"english":{"type":"string"},"simplifiedChinese":{"type":"string"}},"required":["sourceLanguage","english","simplifiedChinese"]}`

	scheduleInstructions = "First classify whether this single Japanese X post is a concrete schedule notice. Return classification=not_schedule when it is ordinary conversation or has no concrete schedule, and classification=uncertain when the evidence is ambiguous. For classification=schedule, extract only entries explicitly supported by the post text or images. Interpret relative dates from the post publication time in Asia/Tokyo. Never invent a time. Use null time and timePrecision unknown when the date is known but the time is not. Set action=cancel only when the post is evidence that an existing schedule may have been cancelled; this is a judgement for review, never a deletion or definitive cancellation. Put the reason in the root reason field and cite the post wording in evidence. For cancellation, events should identify the affected date/title/time when possible. " + scheduleLanguageInstruction + " Return an empty events array for not_schedule."

	// English is the base language of stored text; the board translates
	// status codes and shows titles in their original wording.
	scheduleLanguageInstruction = "Keep each title and the cited evidence in the post's original wording. Write detail and reason in English, and give status as one short lowercase English word such as scheduled, tentative or released."

	scheduleAssetVerificationInstructions = "Inspect the attached image only and decide whether it is a weekly or multi-day schedule board for the creator. Return schedule only when the image itself visibly contains a structured calendar or schedule table with days or dates and planned items. Return not_schedule for a stream thumbnail, promotional art, character art, a single video card, or any ordinary post image. Return uncertain when the image is unreadable or the evidence is insufficient. Do not infer a schedule from the source post wording. Cite visible image evidence briefly."

	// JSON.stringify(scheduleSchema) and
	// JSON.stringify(scheduleAssetVerificationSchema) from the TypeScript module.
	scheduleSchema rawJSON = `{"type":"object","additionalProperties":false,"properties":{"classification":{"type":"string","enum":["schedule","not_schedule","uncertain"]},"events":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"title":{"type":"string"},"detail":{"anyOf":[{"type":"string"},{"type":"null"}]},"date":{"type":"string"},"time":{"anyOf":[{"type":"string"},{"type":"null"}]},"endTime":{"anyOf":[{"type":"string"},{"type":"null"}]},"timePrecision":{"type":"string","enum":["exact","approximate","unknown"]},"status":{"type":"string"},"eventType":{"type":"string","enum":["event","stream","member","release","appearance"]},"url":{"anyOf":[{"type":"string"},{"type":"null"}]},"confidence":{"type":"number","minimum":0,"maximum":1},"evidence":{"type":"string"}},"required":["title","detail","date","time","endTime","timePrecision","status","eventType","url","confidence","evidence"]}},"confidence":{"type":"number","minimum":0,"maximum":1},"evidence":{"type":"string"},"reason":{"anyOf":[{"type":"string"},{"type":"null"}]},"action":{"type":"string","enum":["add","update","cancel","none"]}},"required":["classification","events","confidence","evidence","reason","action"]}`

	scheduleAssetVerificationSchema rawJSON = `{"type":"object","additionalProperties":false,"properties":{"classification":{"type":"string","enum":["schedule","not_schedule","uncertain"]},"confidence":{"type":"number","minimum":0,"maximum":1},"evidence":{"type":"string"},"reason":{"type":"string"}},"required":["classification","confidence","evidence","reason"]}`
)

var defaultScheduleKeywords = []string{
	"schedule",
	"スケジュール",
	"配信予定",
	"週間予定",
	"今週の予定",
	"予定",
}
