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

	routeVision  = "schedule_vision"
	routeMessage = "schedule_message"
	routeBoard   = "schedule_board"

	llmMaxRetries = 2

	extractionToolName   = "kano_schedule_extraction"
	verificationToolName = "kano_schedule_asset_verification"

	scheduleInstructions = "First classify whether this single Japanese X post is a concrete schedule notice. Return classification=not_schedule when it is ordinary conversation or has no concrete schedule, and classification=uncertain when the evidence is ambiguous. For classification=schedule, extract only entries explicitly supported by the post text or images. Interpret relative dates from the post publication time in Asia/Tokyo. Never invent a time. Use null time and timePrecision unknown when the date is known but the time is not. Set action=cancel only when the post is evidence that an existing schedule may have been cancelled; this is a judgement for review, never a deletion or definitive cancellation. Put the reason in the root reason field and cite the post wording in evidence. For cancellation, events should identify the affected date/title/time when possible. Return an empty events array for not_schedule."

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
