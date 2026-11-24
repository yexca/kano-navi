# Schedule Extraction Pipeline

How source text becomes schedule entries and how extraction failures back off.

Schedule extraction has two independent candidate paths:

1. The schedule-board path uses the admin-configurable keyword list, matched
   case-insensitively against a post's `label` and text, to select
   posts that look like a schedule image or board; its posts are recorded with
   the `schedule_board` detection route.
2. The single-message path applies a cheap local date/time and scheduling-verb
   heuristic to ordinary X posts and Bilibili dynamics, including messages
   without a configured keyword. A Bilibili dynamic also matches the Chinese
   words 今天, 明天, 后天, `<n>点`, 北京时间, 直播, 开播, 播, 开始, 预约,
   上传 and 投稿. Suspected messages are recorded with the `schedule_message`
   detection route, and the model first classifies the post as `schedule`,
   `not_schedule`, or `uncertain` before extracting events. With `schedule_message_all_posts`
   enabled, the path skips the heuristic and selects every post with text or
   images; each new post then costs one model call, still within the shared
   scan limit.

The model text starts with the stored publication timestamp followed by its
date, weekday, and time in the source's zone (Asia/Tokyo for X, Asia/Shanghai
for Bilibili), so relative dates such as 今日, 明天, or a bare weekday resolve
without a UTC conversion. Fingerprints use the stored timestamp alone.

Every returned entry must pass the event check: a non-blank title, a real
`YYYY-MM-DD` date, `HH:MM` times (an end time only with a start time, or for
a sale with a close date), for a sale a close date (`endDate`) that is a real
date not before its opening date, known `timePrecision` and `eventType` codes
and a confidence from 0 to 1. One
entry that fails it fails the whole extraction with `schedule extraction
contained an invalid event`. The prompt therefore states these formats and
asks the model to leave out entries that give only a month, a season or a
vague period, and to classify a post with no remaining entry as
`not_schedule`. The console's
[recent failures](../admin-api/models.md#extraction-and-models) show the newest
stored failures with their messages.

The prompt defines each `eventType` in one sentence; a `sale` is a period to
buy, pre-order, order or apply for goods, media or tickets, and a `release`
the date a work becomes available. It asks for one entry per sale period with
its opening as `date` and `time` and its close as `endDate` and `endTime`;
本日から means the post date. A post that names only the deadline gives the
deadline as both dates and no start time. The close is stored as the
[sale period](../data-model/events.md#sale-periods) for a sale and ignored for every
other type, and a shipping or delivery date is not an entry. A sale's
`endTime` without `endDate` closes it on the Japan date that time falls on.

Both paths select only posts published (`posts.published_at`) within the
extraction lookback before the scan starts: 30 days by default, `0` for no
limit (see [sync settings](../../operations/configuration/sync.md#sync-settings)).
Posts outside it are not read, so a change of their input, a pending retry,
a new extractor version or a backfilled or imported history sends none of
them to a model. An operator's reprocess request bypasses the lookback,
whatever the post's age, as forced translation requests bypass the
translation lookback.

Neither path, nor an operator's reprocess request, selects a post with
`posts.hidden_at` set; a request for a hidden post waits until the post is
shown again, and showing it makes it a candidate again. The events such a
post alone supports leave the public pages while it is hidden (see
[Events of hidden posts](../data-model/posts.md#events-of-hidden-posts)).

A post that quotes another status gives the model the quoted wording after
its own text, marked as quoted context (`[Quoted post by @handle. …]`),
because a quoted status reaches extraction no other way: posts often say
little more than 「これ！」 over a quoted announcement. A Bilibili
[forward](bilibili-dynamics.md#forwards) marks the forwarded dynamic's wording the same way but
names its author by display name (`[Forwarded dynamic by <name>. …]`) and
says that times written in it without a zone are Beijing time, as in the
forward's own. The quoted wording joins the post's extraction fingerprint,
the single-message heuristic (the Chinese one for a forward too) and the
reading of the entry's platform; a post without a quote has the
fingerprint of its own wording alone. An unavailable quote, or one an
operator hid from the public pages (`posts.quote_hidden_at`), adds nothing,
so hiding or showing a quote changes the input and the post is extracted
again. Quoted images never join the input (see
[Quoted posts](quoted-posts.md)).

The two candidate lists are merged, de-duplicated by post ID, sorted by
least recent attempt (unattempted posts first), with publication time as a tie
breaker. The shared limit counts candidates requiring work; unchanged fingerprint
cache hits and posts held back by the failure backoff below do not consume it.
Failures and blocked candidates move behind older
unattempted work on later scans. Manual reprocess requests still run first. A post selected by the
board path is not sent a second time through the message path in the same scan.

Each post is judged before anything is written: no content or images still
downloading is a skip, a stored success or `uncertain` result for the same
input is a cache hit, a post in its failure backoff is held back, and any
other post gets a model call. The scan and the console's
[queue preview](../admin-api/runs.md#queue-preview) share that judgement and the
order above, so the preview lists what a run started at that moment
processes. A cache hit whose post state already records that result writes
nothing. A cache hit of a successful result the post did not apply last,
as when an input returns to one read before (a quote hidden, then shown
again), applies that stored result to the post's events again, exactly as
a model answer would; a post state that names no extraction is left as it
is. A skip, and a cache hit that writes, keep the stored reprocess
request flag, so a request an operator makes after the post was read runs on
the next scan. An accepted model result replaces the post's automatic events
and applies its cancellation overlay in one transaction that also advances
the dashboard revision when it changed a value of the post's events, the
events merged into them or its evidence; a post that is no schedule, or a
stored result applied again, leaves the revision as it is.

The run reads what judging needs (post states, post images, their media
assets and the stored extractions) for the next 200 unsettled posts of its
queue at once, and judges and carries them out in order. Carrying out a post
writes only that post's state and extractions, so the batch stays current
while the run walks it. A model call drops the batch, because an operator may
change a later post while the call waits, and the next post reads a new one.
A scan resolves each media cache directory's symlinks once and checks only
the file itself after that, resolving a file that is itself a symlink whole,
under the same containment rules.
The candidate query reads only the image lists of a post's stored source
record, which SQLite extracts with `schedule_media_json`, not the whole
record.

A settled post is a cache hit decided without loading it. Each detection
stage reads every shown post within the lookback with its input stamp for
that stage, computed in SQLite from everything the extraction identity and
the image check read
(see [Post LLM states](../data-model/posts.md#post-llm-states)). A post whose state
records a cache hit on that stage's route for exactly that stamp, with no
reprocess request, error or failure backoff, keeps its place among the
candidates but counts as a cache hit without its images, cached files or
stored extractions being read. Its stage still selects it, so a board post
that stays on the board path is not sent through the message path, and the
order, the limit and the outcome of every other post are those of judging it.

The step result records the candidate phase as `candidateMs`
(listing requests and candidates, with their stamps, and ordering them),
`postsRead` (rows the detection stages read, each stage reading every shown
X and Bilibili post within the lookback) and `candidatePosts` (the posts queued), then `judged`
(posts the scan loaded and judged), `settled` (settled posts it counted as
cache hits without loading them) and `judgeMs` (time spent loading and
judging, without model calls or writes). `cached` counts both kinds of cache
hit.

## Extraction Failure Backoff

A post fails when every compatible queue target exhausted its calls. The
scan then records a consecutive failure count for that input (its fingerprint
plus the ordered targets that were tried) and leaves the post alone for 1 hour
after the first failure, 6 hours after the second and 24 hours after the
third. The fourth consecutive failure stops automatic retries. A post held
back this way makes no provider call, keeps its `failed` state and last error,
and is counted as `backoff` (waiting) or `retryStopped` in the step result;
these keys appear only when a post was held back, and held posts are not
failures of that run. X timers run as often as every 15 minutes and each
failure already costs up to three calls per target, so the first wait covers a
short provider outage, the 24-hour wait a daily quota reset, and a post that
still fails after about 31 hours most likely fails on its content.

The backoff starts over when the input changes (edited text, publication
time, URL or cached images), when the text or image extraction queue's targets
or models change, and when an operator requests a reprocess, which also clears it immediately. A
queued reprocess request always runs, whatever the backoff, and a forced pass
that fails counts as the first failure. A success or `uncertain` result,
including a cache hit, clears the backoff. A cancelled run is not a failure.
`skipped` outcomes that make no call (`media_pending`, `no_content`, no
configured or compatible target) are not failures, keep any earlier count,
and stay eligible for later scans. The console shows the next retry time or
that retries stopped next to the post's state. A failed post without a
recorded failure count has no backoff and is tried on the next scan.

An operator can dismiss a failed post. It becomes `dismissed`, leaves the
console's failed list, and is held back like a post whose retries stopped
until its input or targets change or an operator requests a reprocess.

Text-only posts draw from the schedule extraction queue and posts with images
draw from the image extraction queue. Each queue is an ordered list of targets, a
provider plus one of its catalog models, and a target's capabilities are its
model's `text`/`image` tags.
Providers have no default model, and nothing reads the
`llm_providers.model` column. A one-time startup upgrade, recorded as
`llm_default_model_retired`, rewrites each queue target without a model
(including legacy provider-only routes) to name its provider's stored model.
Where that pair is already queued, the earlier position stays, and a target
whose provider has no stored model is dropped because it could never be
called. Target selection follows
the original post modality. Text-only input requires `text`; image-only input
requires `image`; mixed input uses the image extraction queue and requires an
image-capable model. Only ready cached images are sent to the model. A post that
declares an image but whose cache is not ready is recorded as `media_pending`
and is not silently reduced to text-only input. Each provider uses OpenAI
Responses, Chat Completions, or Anthropic Messages, receives at most the
[attempts per model](../../operations/configuration/sync.md#sync-settings) in total
calls (three by default), and then yields to the next enabled, keyed,
capability-compatible target in queue order; targets whose capabilities do
not fit the input are skipped without a call. `llm_providers.max_retries`
always stores 2 (startup sets every row to it), and the admin API validates
but does not store a different `maxRetries`; calls follow the attempts
setting, not this column.
