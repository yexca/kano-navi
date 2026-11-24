# Translation

How post text and titles are translated.

The `translate` step machine-translates X post and Bilibili dynamic text,
the titles of the unmerged videos public readers list (YouTube videos and
Bilibili uploads whose form is settled), the X statuses own posts quote, the
related accounts' statuses own accounts repost and the
dynamics own forwards forward into
English, Japanese and Simplified Chinese, each
locale only when it is not the detected source language (see
[Translations](../data-model/translations.md) for storage and precedence).
A merged Bilibili upload's title is its video's platform translation and is
never sent to a model, and the step never requests a source. Before any
model call it settles [Bilibili forms](../data-model/video-form.md#bilibili-form) by rule,
whether or not a queue is configured, and the same queue then asks the
unsettled uploads for form and tag. Its models come only from the separate
translation queue (`llm_route_targets.route = 'translate'`), configured in
`/admin` → Detection; it never falls back to the schedule extraction queue. Targets need
the `text` tag. Each target gets at most the
[attempts per model](../../operations/configuration/sync.md#sync-settings)
in calls (three by default) before the next configured target takes over,
in queue order.
Without a usable target the step records `disabled` with the reason and
keeps operator requests.

Candidates are posts, YouTube videos, pooled quoted X statuses and reposted
statuses of related accounts that are not stored posts (`owner_type` `x_status`, see
[Quoted posts](quoted-posts.md)) and pooled forwarded dynamics that are not
stored posts (`owner_type` `bilibili_status`, see [Forwards](bilibili-dynamics.md#forwards))
whose current wording lacks a usable translation of a needed locale:
operator requests first (forced passes that replace machine rows, whatever
their age), then posts and videos, then pooled X statuses, then pooled
dynamics; within each, never-attempted wording, then the least recently
attempted, newest first. Unrequested candidates are limited to the
translation lookback (30 days by default), by publication, for a pooled
status by when an own post last quoted it or an own account first reposted
it, and for a pooled dynamic by when
an own forward last forwarded it, and at most the translation items per run
(20 by default) are sent per run, pooled statuses included (see
[sync settings](../../operations/configuration/sync.md#sync-settings)). A locale already covered
by a manual or platform row is not a reason to call a model.

Wording with only links, hashtags, mentions or emoji is recorded as
`skipped` with language `none` without a call; wording over 4000 characters
is skipped without a call. The model receives the kind (X post, which a
pooled status also is, Bilibili dynamic, which a pooled dynamic also is,
YouTube video title or Bilibili video title) and the text, returns `sourceLanguage` plus English, Japanese and
Simplified Chinese, and is told to judge the language by the text's own
wording rather than a song title or name it quotes (a Bilibili title in
Chinese around a Japanese song title is `zh`) and to keep URLs, tags, mentions, emoji,
bracketed emoji codes and bracketed channel tags, and to keep times as
written with their zone (北京时间17点 becomes 17:00 Beijing time; converting
a time belongs to extraction). A result is accepted only with a known language and a
non-empty, bounded translation for every locale that differs from it. The
result is stored only if the wording still has the hash that was sent.
The translation fields hold the complete displayed text. Preserving an
untranslated song title or name means including that wording in the result,
even if the whole title stays unchanged; it never justifies an empty field.

Translation uses native JSON Schema output for all three provider protocols;
Anthropic sends `output_config.format` and reads text blocks rather than
asking the model to call a result tool. Schedule extraction keeps its tool
protocol. Haiku 5.5 translation requests explicitly disable thinking and set
`effort` to `low`; these model-specific options are not sent to other models.
The initial output budget is `max(1024, 400 + 3 * source characters)`, capped
at 4000 tokens. A truncated response is never saved, even if its JSON parses:
the next call doubles the budget up to 4000, without a backoff wait. Invalid
JSON, missing fields or invalid translation values get one corrective prompt;
a repeated validation failure moves to the next target. Connection failures,
timeouts, HTTP 408/429 and 5xx retry with bounded backoff. Other HTTP errors
and model refusals move directly to the next target. No target is added to
the operator's queue automatically.

The protected run's translation counts include `diagnostics`: at most 600
model-attempt entries with owner type/ID, configured provider/model, attempt,
output budget, allowlisted stop reason, bounded input/output token usage
when reported, and a fixed error category. Additional entries increment
`diagnosticsOmitted`. No response text, reasoning, refusal wording, headers
or credentials are stored in these diagnostics. Public projections omit them.

Platform translations belong to source synchronization, not to this step,
which only reads them. Writers store them through
`UpsertPlatformTranslationTx` and remove them through
`DeletePlatformTranslationTx` (see [Translations](../data-model/translations.md)).
Two writers store them: the YouTube source, for the
[titles a creator localized](youtube-videos.md#localized-titles) as a Data API answer reports
them (platform `youtube`, `en` and `zh-CN`), and the
[video merge](../data-model/videos.md#video-merging),
for the title of a Bilibili upload merged into a YouTube video (platform
`bilibili`, `zh-CN`).
