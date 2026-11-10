package schedule

import "strings"

// These strings define model behaviour and are copied verbatim from
// server/schedule-extractor.ts. extractorVersion is part of stored extraction
// identities, so changing it reprocesses every known post.
const (
	extractorVersion     = "openai-schedule-v7"
	defaultOpenAIBaseURL = "https://api.openai.com/v1"

	protocolResponses = "openai-responses"
	protocolChat      = "openai-chat-completions"
	protocolAnthropic = "anthropic-messages"

	// routeMessage and routeBoard record a post's detection path. Text-only
	// inputs use routeQueue; inputs with images use routeVerify.
	routeMessage = "schedule_message"
	routeBoard   = "schedule_board"
	routeQueue   = "schedule"
	// routeVerify is the image schedule extraction queue; it never falls back
	// to the text extraction queue.
	routeVerify = "verify"
	// routeTranslate is the separate translation queue; it never falls back
	// to the schedule queue.
	routeTranslate = "translate"

	extractionToolName      = "kano_schedule_extraction"
	imageExtractionToolName = "kano_schedule_image_extraction"
	verificationToolName    = "kano_schedule_asset_verification"
	translationToolName     = "kano_text_translation"
	eventTagToolName        = "kano_event_tag"

	// eventTagInstructions classifies one YouTube or Bilibili title for the
	// calendar and the video archive. It is a short-text judgement like
	// translation, so it shares that queue rather than adding a queue and model
	// settings of its own. A video keeps only game, talk and song; live and video
	// name a form the video already has.
	eventTagInstructions = "Classify one video title by Kano Mahoro (鹿乃まほろ), a Japanese virtual singer, for a fan calendar and video archive. " + eventTagRule + " Return null when the title does not show what kind of activity it is; never guess from the channel name alone."

	// eventTagRule defines the five labels for both the title inference and
	// the extraction prompt, so a stream or premiere gets the same label
	// whichever path judges it. game, talk and song name the content; live and
	// video name only the form and are used when no content label applies.
	eventTagRule = "Choose by content first: game for gameplay, talk for a chat or talk stream, and song for singing, including a singing stream, a song cover, an original song, a music video and a song premiere. Use live only for a broadcast or stream that is none of these or whose content is not shown, and video only for an uploaded video or premiere that is none of these or whose content is not shown. When an entry mixes activities, use the one it leads with; when none leads, use live for a stream and video for an upload or premiere."

	eventTagSchema rawJSON = `{"type":"object","additionalProperties":false,"properties":{"tag":{"anyOf":[{"type":"string","enum":["live","game","talk","song","video"]},{"type":"null"}]}},"required":["tag"]}`

	// bilibiliFormInstructions asks for a Bilibili upload's form together
	// with its content label: Bilibili's unsigned endpoints do not say
	// whether an upload is the replay of a live broadcast.
	bilibiliFormToolName     = "kano_bilibili_form"
	bilibiliFormInstructions = "Classify one Bilibili upload by Kano Mahoro (鹿乃まほろ), a Japanese virtual singer, for a fan video archive, from its title, its length and the start of its description. Set form to stream when the upload is the recording or replay of one of her live broadcasts (直播回放, 录播, a full or nearly full stream), upload when it is a video made for upload (a song cover, an original song, a music video, a clip, an announcement or a short video), and uncertain when the title, length and description do not tell. A long length alone does not make a stream. " + eventTagRule + " Return null for tag when the upload does not show what kind of activity it is; never guess from the channel name alone."

	bilibiliFormSchema rawJSON = `{"type":"object","additionalProperties":false,"properties":{"form":{"type":"string","enum":["upload","stream","uncertain"]},"tag":{"anyOf":[{"type":"string","enum":["live","game","talk","song","video"]},{"type":"null"}]}},"required":["form","tag"]}`

	translationInstructions = "Translate one X post, Bilibili dynamic or YouTube video title by Kano Mahoro (鹿乃まほろ), a Japanese virtual singer, for readers of an unofficial fan status board. Set sourceLanguage to the language of the text: ja, en, zh, ko or other, or none when it has nothing to translate (only links, hashtags, @mentions, emoji or kaomoji). Translate the whole text into natural English (english), Japanese (japanese) and Simplified Chinese (simplifiedChinese). Keep URLs, hashtags, #topic# tags, @mentions, emoji, bracketed emoji codes such as [笑], kaomoji, bracketed channel tags and line breaks as written. Keep dates and times as written, with their time zone: translate 北京时间17点 as 17:00 Beijing time, and never convert a time to another zone. Use the usual romanization or official names of people, groups and games, and keep a song title in its original wording unless it has an official translation. Each translation field is the complete text to display, not a list of changes: when a song title or name stays unchanged, include that original wording in the non-empty result. Every field whose language differs from sourceLanguage must be non-empty, even when the whole title stays unchanged. Do not add explanations, quotation marks or notes. Return an empty string only for english when sourceLanguage is en, only for japanese when sourceLanguage is ja, only for simplifiedChinese when sourceLanguage is zh, and for all three when sourceLanguage is none."

	translationSchema rawJSON = `{"type":"object","additionalProperties":false,"properties":{"sourceLanguage":{"type":"string","enum":["ja","en","zh","ko","other","none"]},"english":{"type":"string"},"japanese":{"type":"string"},"simplifiedChinese":{"type":"string"}},"required":["sourceLanguage","english","japanese","simplifiedChinese"]}`

	scheduleInstructions = "First classify whether this single Japanese X post is a concrete schedule notice. Return classification=not_schedule when it is ordinary conversation or has no concrete schedule, and classification=uncertain when the evidence is ambiguous. For classification=schedule, extract only entries explicitly supported by the post text or images. Interpret relative dates from the post publication time in Asia/Tokyo. Never invent a time. Use null time and timePrecision unknown when the date is known but the time is not. " + scheduleDateInstruction + " " + scheduleTypeInstruction + " " + scheduleSaleInstruction + " Set action=cancel only when the post is evidence that an existing schedule may have been cancelled; this is a judgement for review, never a deletion or definitive cancellation. Put the reason in the root reason field and cite the post wording in evidence. For cancellation, events should identify the affected date/title/time when possible. " + scheduleLanguageInstruction + " Return an empty events array for not_schedule. " + scheduleTagInstruction + " " + schedulePlatformInstruction

	// schedulePlatformInstruction asks where each entry happens; the board
	// links Bilibili streams to the account's live room itself.
	schedulePlatformInstruction = "Set platform to where the entry happens: youtube for YouTube, bilibili for Bilibili (a bilibili限定 or B站 stream or upload included), other for another platform or someone else's channel or room such as a collaboration, and unknown when the post does not say. Give url only when the post itself gives that link; never make up a live room link."

	// scheduleDateInstruction matches the stored event check: an entry without
	// a calendar day fails the whole extraction, so the model leaves it out.
	scheduleDateInstruction = "Give date as the Asia/Tokyo calendar date YYYY-MM-DD, and time and endTime as 24-hour HH:MM. An entry needs its calendar day: when the post gives only a month, a season or a vague period such as 4月 or 春頃, leave that entry out, and return classification=not_schedule when no entry remains."

	// scheduleTypeInstruction names what each eventType code covers, so a
	// release (a work becoming available) and a sale (a period to buy or
	// apply) are told apart.
	scheduleTypeInstruction = "Set eventType to stream for a live stream, member for a members-only stream, appearance for a performance or a guest appearance, release for the date a work becomes available (a song, music video, premiere, album or CD), sale for a period to buy, pre-order, order or apply for goods, media or tickets (a ticket lottery included), and event for any other entry."

	// scheduleSaleInstruction asks for one entry per sale period. A post that
	// names only the deadline stores a period whose opening is not known: its
	// date and endDate are both the deadline and it has no start time.
	scheduleSaleInstruction = "Give one sale entry per sale period, never separate entries for its opening and its deadline. Set date and time to when the period opens (本日から or 今日から means the post date) and endDate and endTime to when it closes. When the post gives only the deadline, set date and endDate to the deadline date, time to null and endTime to the deadline time or null. Use null endDate when the close is not given and for every entry that is not a sale. A shipping or delivery date is not an entry."

	// scheduleTagInstruction describes the optional calendar label. The label
	// describes one calendar entry, so it belongs to the event rather than to
	// the video a reader may open from it.
	scheduleTagInstruction = "Set tag only when the post or image itself shows what kind of activity the entry is. " + eventTagRule + " When the entry does not say, use null; never guess from the channel or a person's name."

	// English is the base language of stored text; the board translates
	// status codes and shows titles in their original wording.
	scheduleLanguageInstruction = "Keep each title and the cited evidence in the post's original wording. Write detail and reason in English, and give status as one short lowercase English word such as scheduled, tentative or released."

	scheduleAssetVerificationInstructions = "Inspect the attached image only and decide whether it is a weekly or multi-day schedule board for the creator. Return schedule only when the image itself visibly contains a structured calendar or schedule table with days or dates and planned items. When it is a schedule, copy the visible schedule wording into text, preserving line breaks and the original language. Return an empty text string when it is not a schedule or the image is unreadable. Return not_schedule for a stream thumbnail, promotional art, character art, a single video card, or any ordinary post image. Return uncertain when the evidence is insufficient. Do not infer a schedule from the source post wording. Cite visible image evidence briefly."

	// Image posts use the image queue. The structured events keep the calendar
	// working while text is the faithful transcription that the image queue is
	// responsible for producing.
	imageScheduleInstructions = "Read the attached schedule image as the image extraction stage. First decide whether it contains a concrete schedule. When it is a schedule, copy the visible schedule wording into text, preserving line breaks and the original language, and extract only entries supported by that image. Return an empty text string for not_schedule or uncertain. "

	// JSON.stringify(scheduleSchema) and
	// JSON.stringify(scheduleAssetVerificationSchema), byte-exact.
	scheduleSchema rawJSON = `{"type":"object","additionalProperties":false,"properties":{"classification":{"type":"string","enum":["schedule","not_schedule","uncertain"]},"events":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"title":{"type":"string"},"detail":{"anyOf":[{"type":"string"},{"type":"null"}]},"date":{"type":"string"},"time":{"anyOf":[{"type":"string"},{"type":"null"}]},"endTime":{"anyOf":[{"type":"string"},{"type":"null"}]},"endDate":{"anyOf":[{"type":"string"},{"type":"null"}]},"timePrecision":{"type":"string","enum":["exact","approximate","unknown"]},"status":{"type":"string"},"eventType":{"type":"string","enum":["event","stream","member","release","appearance","sale"]},"tag":{"anyOf":[{"type":"string","enum":["live","game","talk","song","video"]},{"type":"null"}]},"url":{"anyOf":[{"type":"string"},{"type":"null"}]},"platform":{"type":"string","enum":["youtube","bilibili","other","unknown"]},"confidence":{"type":"number","minimum":0,"maximum":1},"evidence":{"type":"string"}},"required":["title","detail","date","time","endTime","endDate","timePrecision","status","eventType","tag","url","platform","confidence","evidence"]}},"confidence":{"type":"number","minimum":0,"maximum":1},"evidence":{"type":"string"},"reason":{"anyOf":[{"type":"string"},{"type":"null"}]},"action":{"type":"string","enum":["add","update","cancel","none"]}},"required":["classification","events","confidence","evidence","reason","action"]}`

	scheduleAssetVerificationSchema rawJSON = `{"type":"object","additionalProperties":false,"properties":{"classification":{"type":"string","enum":["schedule","not_schedule","uncertain"]},"text":{"type":"string"},"confidence":{"type":"number","minimum":0,"maximum":1},"evidence":{"type":"string"},"reason":{"type":"string"}},"required":["classification","text","confidence","evidence","reason"]}`
)

// The Bilibili variant of the extraction prompt. A dynamic is written in
// Chinese for Chinese viewers, and its times are Beijing time unless it says
// otherwise. The model gives each entry's date and times as written with the
// zone they are written in; code converts them to Asia/Tokyo, so the model
// never does time arithmetic.
const (
	bilibiliScheduleInstructions = "First classify whether this single Chinese Bilibili dynamic, written by the Japanese virtual singer Kano Mahoro (鹿乃) for Chinese viewers, is a concrete schedule notice. Return classification=not_schedule when it is ordinary conversation or has no concrete schedule, and classification=uncertain when the evidence is ambiguous. For classification=schedule, extract only entries explicitly supported by the post text or images. Interpret relative dates such as 今天, 明天 and 后天 from the post publication time in Asia/Shanghai (Beijing time). Never invent a time. Use null time and timePrecision unknown when the date is known but the time is not. " + bilibiliScheduleDateInstruction + " " + scheduleTypeInstruction + " " + scheduleSaleInstruction + " Set action=cancel only when the post is evidence that an existing schedule may have been cancelled; this is a judgement for review, never a deletion or definitive cancellation. Put the reason in the root reason field and cite the post wording in evidence. For cancellation, events should identify the affected date/title/time when possible. " + scheduleLanguageInstruction + " Return an empty events array for not_schedule. " + scheduleTagInstruction + " " + bilibiliSchedulePlatformInstruction

	// bilibiliSchedulePlatformInstruction asks for the platforms the wording
	// names; code decides whether bilibili alone is stated and gives a
	// stream that names none the account's room.
	bilibiliSchedulePlatformInstruction = "Set platforms to the platforms the post itself names for the entry: youtube when it names YouTube (油管), bilibili when it names Bilibili (B站, bilibili限定) and other for another platform or someone else's channel or room such as a collaboration. List both youtube and bilibili for a broadcast on both at once (同时直播). Use an empty array when the post names no platform for the entry, as for a plain 直播. Give url only when the post itself gives that link; never make up a live room link."

	// bilibiliScheduleDateInstruction keeps the stored event check and asks
	// for the written zone instead of a conversion.
	bilibiliScheduleDateInstruction = "Give date as the calendar date YYYY-MM-DD and time and endTime as 24-hour HH:MM exactly as the post writes them, without converting between time zones. Set timeZone to Asia/Tokyo only when the post states Japan time (日本时间); otherwise use Asia/Shanghai, because times written without a zone or as 北京时间 are Beijing time. An entry needs its calendar day: when the post gives only a month, a season or a vague period, leave that entry out, and return classification=not_schedule when no entry remains."
)

// bilibiliScheduleSchema is scheduleSchema with each entry's timeZone and
// its platforms array in place of the single platform.
var bilibiliScheduleSchema = rawJSON(strings.NewReplacer(
	`"url":{"anyOf":[{"type":"string"},{"type":"null"}]},"platform":{"type":"string","enum":["youtube","bilibili","other","unknown"]},`,
	`"url":{"anyOf":[{"type":"string"},{"type":"null"}]},"timeZone":{"type":"string","enum":["Asia/Tokyo","Asia/Shanghai"]},"platforms":{"type":"array","items":{"type":"string","enum":["youtube","bilibili","other"]}},`,
	`"tag","url","platform","confidence","evidence"]`, `"tag","url","platforms","timeZone","confidence","evidence"]`,
).Replace(string(scheduleSchema)))

// imageScheduleSchema adds the faithful transcription returned by the image
// queue without changing the text queue's established request schema.
func imageScheduleSchema(base rawJSON) rawJSON {
	value := strings.Replace(string(base), `"events":`, `"text":{"type":"string"},"events":`, 1)
	value = strings.Replace(value, `"classification","events"`, `"classification","text","events"`, 1)
	return rawJSON(value)
}

var defaultScheduleKeywords = []string{
	"schedule",
	"スケジュール",
	"配信予定",
	"週間予定",
	"今週の予定",
	"予定",
}
