# Sources and Schedule Extraction

## Current Sources

### X

The script reads each public profile named by `X_HANDLES` (default
`kano_2525,_Kanotic`), extracts status IDs, and decodes their Snowflake
timestamps before making detail requests. Each account has its own cursor in
`sync_state`; with no cursor, IDs older than `X_BOOTSTRAP_DAYS` (seven by
default) are skipped. Later runs prioritize unknown IDs and may refresh only a
small `X_REFRESH_KNOWN` budget. Discovery and detail-request limits cap the
work per run; `X_MAX_STATUS_REQUESTS` is the total detail-request budget shared
by all configured accounts. Successful posts are stored with `account_handle`
and returned as one time-ordered feed. Image-only posts are retained as
candidates as long as they have a publication time and a media URL.
`X_HANDLE` remains a single-account compatibility fallback.

Normal incremental runs persist admitted but unfinished status IDs in the
account's `sync_state.metadata_json.pendingStatusIds` before detail requests.
The array's order is the per-account retry order. Newly admitted IDs join after
existing pending work; IDs selected within the account's request allocation move
behind unattempted work, with that rotation persisted before detail requests.
Thus a repeatedly failing ID cannot keep blocking other admitted work, even
after a process restart. Retries use each run's existing budget without time
backoff or discarding failures. Pending work remains retryable when it falls out
of the profile or ages beyond the bootstrap window. Successful posts,
pending-work removal, and the latest successful cursor commit atomically.
Budget exhaustion and failed/invalid detail responses retain unfinished IDs;
an all-failed run does not record a successful cursor or timestamp. Date-window
fetches neither consume this queue nor update normal incremental state.

Each discovered image URL is registered and linked to its post. A bounded media
stage later in the same command downloads pending image files from X's
`*.twimg.com` media CDN subdomains.
The X adapter also accepts media and schedule keywords carried by a quoted
tweet (`qrt`) in the detail response.
When a schedule asset already has a source status URL but no image URL, the
synchronizer adds up to `X_SCHEDULE_REFRESH_LIMIT` recent source IDs per
account to the detail-request queue. On a cursorless bootstrap with no asset
source for an account, a bounded set of keyword-matching snapshot posts can
serve the same purpose. These requests supplement the profile HTML without
bypassing the shared `X_MAX_STATUS_REQUESTS` budget and can recover a schedule
post that has fallen out of the profile page's visible status list.

### YouTube

The script reads the RSS feed for `YOUTUBE_CHANNEL_ID` (the current main channel
by default). With no cursor, it stores the latest `YOUTUBE_BOOTSTRAP_VIDEOS`
entries (six by default). Later runs store only entries before the prior cursor,
or entries newer than its timestamp when that cursor has fallen out of the RSS
window. It separately inspects the streams page and keeps rechecking active or
recent reservations for `scheduledStartTime`, writing them to both `videos` and
`events`. When no schedule time is available, the script does not invent one. RSS insertion
does not count as a reservation detail check. Successful stream-page detail
checks are recorded in incremental `sync_state.metadata.streamDetailsCheckedIds`
for currently discovered/active videos. Unchecked discoveries precede known
reservation refreshes within the detail budget; failed checks remain eligible,
and failed refreshes retain existing video/event snapshots. HTTP 200 alone is
not success: a reservation needs a parsed timestamp and page title; an ordinary
video without a reservation needs a complete, playable player response with a
matching video ID and title. Missing/truncated details, invalid timestamps, and
upcoming pages without a start time remain retryable within the existing request
budget. They record the protected `youtube_watch_details_incomplete` diagnostic
without updating video/event details or claiming a successful check. Incremental
metadata distinguishes successful `inspected` checks from `detailRequested`
attempts. Historical windows
do not write this incremental metadata.

Video thumbnails are registered and linked to their rows, then the bounded media
stage downloads pending files from YouTube's `*.ytimg.com` thumbnail CDN
subdomains (including `i.ytimg.com` and its numbered hosts).

### Schedule Extraction Pipeline

Schedule extraction has two independent candidate paths:

1. The schedule-board path uses the admin-configurable keyword list to select
   posts that look like a schedule image or board. It uses the
   `schedule_board` provider route.
2. The single-message path applies a cheap local date/time and scheduling-verb
   heuristic to ordinary X messages, including messages without a configured
   keyword. Suspected messages are sent to the `schedule_message` provider
   route, which first classifies the post as `schedule`, `not_schedule`, or
   `uncertain` before extracting events.

The two candidate lists are merged, de-duplicated by X post ID, sorted by
least recent attempt (unattempted posts first), with publication time as a tie
breaker. The shared limit counts candidates requiring work; unchanged fingerprint
cache hits do not consume it. Failures and blocked candidates move behind older
unattempted work on later scans. Manual reprocess requests still run first. A post selected by the
board path is not sent a second time through the message path in the same scan.
The legacy `schedule_vision` route remains available for older API clients and
is used as the compatibility fallback when a new route has no explicit order.

Each route is an ordered list of targets: a provider plus one of its catalog
models. An empty model ID means "the provider's default model" (`llm_providers.model`)
and uses the provider-level capabilities; this is how legacy provider-only
routes were migrated and how the old `PUT /api/admin/providers/order` endpoint
still writes. The default model's catalog tags and the provider capability
columns are kept identical whichever side is edited. Target selection follows
the original post modality. Text-only input requires `text`; image-only input
requires `image`; mixed input requires a model with both tags. Only ready cached images are sent to the model. A post that
declares an image but whose cache is not ready is recorded as `media_pending`
and is not silently reduced to text-only input. Each provider uses OpenAI
Responses, Chat Completions, or Anthropic Messages, receives at most three total calls (one
initial request plus two retries), and then yields to the next enabled, keyed,
capability-compatible provider in that route's priority order. The persisted
`max_retries` field is retained for old clients and migrated to this fixed
policy; it is not a cost-control override.

### LLM Provider Catalog

`/admin` manages providers centrally. A provider stores the API host, format
(Responses, Chat Completions, or Anthropic Messages), timeout, and encrypted key. On an operator
action the server requests `GET <baseUrl>/models` with the stored key, a
timeout, `redirect: "error"`, and a 4 MB response limit; it accepts OpenAI-style
`{ data: [{ id }] }`, `{ models: [...] }`, or bare arrays. Upstream bodies and
errors are reduced to fixed messages. Tags for newly added models are suggested
from the model ID and stay editable. Removing a model removes its route
targets; removing the default model clears `llm_providers.model`. The private
and loopback address guard for provider URLs applies to model discovery too.

API hosts can omit `/v1`: bare origins use `/v1`, while OpenRouter and Gemini
use their preset API roots. Explicit versioned roots and custom paths are
preserved, and a pasted inference endpoint is reduced to its API root.
`src/lib/llm-endpoints.ts` owns this rule for both UI previews and server calls.
Anthropic requests use `x-api-key` and `anthropic-version: 2023-06-01`.
Model discovery follows `has_more`/`last_id` with 100 models per page and a
20-page cap under one timeout. Extraction uses a forced tool with the existing
JSON schema and validates its `tool_use.input` locally; cached images are sent
as base64 image blocks. Model and inference responses are bounded to 4 MiB of
streamed bytes, and redirects are rejected. Connection tests and inference share
the protocol-specific output limit: `max_output_tokens` for Responses,
`max_completion_tokens` for Chat Completions, and `max_tokens` for Anthropic.

A local validator rejects invalid calendar dates, time formats, enumerations,
or confidence values before any event write. `schedule` results update automatic
events. A model never deletes or definitively cancels an event: `action=cancel`
matches the clearest existing event and writes an `llm_suspected` cancellation
overlay containing the reason, source evidence, confidence, and post ID. The
overlay also handles cancellation-like event status strings on add/update
results. Such entries never create a definitively cancelled event or retire
existing source evidence; mixed results still update their scheduled entries.
Cancellation-only entries without an unambiguous existing target remain in
extraction metadata. The overlay applies to manually locked events too, so a manually entered event can
remain visible with the model's cancellation evidence. `not_schedule` and
`uncertain` results only record the extraction outcome and leave existing events
untouched. Input fingerprints (including text, publication time, URL, and cached image
hashes), provider endpoint/model identity, and the extractor version make these
outcomes idempotent. Successful/uncertain posts stay eligible for cache checks
and can run again when their input or provider configuration changes. Date-only events store `starts_on`, a null `starts_at`, and
`time_precision = unknown`; the model must not invent a specific time.

Every stored post has an LLM processing state. Failed, skipped, and uncertain
attempts remain visible to operators, and `/api/admin/posts/:id/llm/reprocess`
queues a forced pass that bypasses the successful-result cache. The automatic
scan consumes queued posts even when they no longer match the current keyword
heuristic; a missing provider or pending image keeps the queue request pending,
while a provider failure consumes it. Queued posts run before keyword
candidates within the shared scan limit. They use the route the operator
requested; without one, a post that a keyword path also selected keeps that
route, image posts use `schedule_board`, and text posts use `schedule_message`.

Providers and model routes are configured explicitly in `/admin`; initialization
does not create or enable an OpenAI provider. The legacy model preference comes
from `app_settings.llm_model`. Provider API keys are encrypted with
`LLM_SECRETS_KEY`; ciphertext is never returned by an API response or written
to extraction `raw_json`. A legacy `OPENAI_API_KEY` can be imported once with
`npm run migrate:llm` after creating an `openai-default` provider in `/admin`;
synchronization never reads that environment variable.
Candidates from either X account use the same idempotent fingerprint and
manual-lock rules. The stage flags are stored as `schedule_keyword_enabled`,
`schedule_vision_enabled`, and `schedule_message_enabled` and can be changed
independently in `/admin`. The legacy `schedule_extraction_enabled` setting is
retained for old clients and for seeding the stage defaults; it is no longer a
runtime master switch, so a newly enabled stage can run even when an older
database still contains `0` there. The old admin API field updates all three
stage flags together for compatibility.

The `featured_video_id` setting is maintained through `/admin`. It points to an
existing local `videos` row (or an empty value for no Featured item), and source
synchronization never changes it. The dashboard includes the selected video
even when it falls outside the most recent 30 rows.

### Schedule Images

Public-profile synchronization and official X date-window pages share the same
candidate builder. Each page registers its newest matching image per Japan
Monday week, together with its source account. Candidates, post/media records,
and historical continuation checkpoints commit atomically. Older pages or
accounts cannot replace a newer image for that week or the `weekly-schedule`
compatibility alias. Registration does not run extraction or approve an image.

A schedule image (`assets.kind = schedule`) is public only after two gates:

1. Sync promotes a post's image only when the author's own wording, including
   quoted text, explicitly reads as a weekly or multi-day board notice
   (`server/schedule-asset.ts`). The stored post `label` is not part of that
   input, because the looser post classifier labels any `配信予定` post as
   `SCHEDULE / 日程`. The configurable extraction keywords do not promote images.
2. The automatic scan sends each unreviewed candidate's ready cached image,
   without the post text, to the `schedule_board` route and records
   `schedule`, `not_schedule`, or `uncertain` in `schedule_asset_reviews`. A
   candidate whose source post no longer passes gate 1 (written by an older
   sync) is recorded as `skipped` / `source_not_board` without a model call. A
   pending cache, or no keyed Image provider, is also `skipped` and retried.

`/api/dashboard` returns a schedule asset only when an operator labelled it
`schedule`, or when it has no manual label, still passes gate 1, and the model
returned `schedule`. A manual `not_schedule` always hides it. Labels require a
reason and are set in the `/admin` schedule-images view. Sync reuses the
`schedule-<week>` IDs and the `weekly-schedule` alias, so a review belongs to
the image URL it judged: when an asset ID receives a different URL, its model
verdict and manual label reset to `pending` / `unreviewed`.

Source HTML/XML/JSON reads have a 4 MiB response-byte limit, reject redirects,
and keep the configured timeout active through body consumption. A timed-out or
oversized stream is cancelled and releases its reader so the single-flight queue
can run a subsequent job.

Image review budgets apply after cache checks and manual-label filtering across
all stored candidates, rather than only the newest page. Retry order follows
the least recent check/review attempt. Reviews persist an input fingerprint
including image hash, source gate input, and ordered provider/model targets;
changed content or configuration can invalidate a successful verdict.

## Related Docs

- [Workflow and historical fetch windows](workflows.md)
- [Event precedence](data-model.md#event-precedence)
- [Configuration](../operations/configuration.md)
- [Secure development](../development/security.md)
