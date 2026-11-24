# Events and Schedule Images

Curating events and deciding schedule images.

## Events

| Method and path             | Body / query                                                                                                            | Result            |
| --------------------------- | ----------------------------------------------------------------------------------------------------------------------- | ----------------- |
| `GET /events`               | `page`, `pageSize` (≤ 100), `search`, `provenance`, `source`, `cancellation`, `merge`, `from`, `to`, `order`, `deleted` | list of events    |
| `GET /events/{id}/evidence` |                                                                                                                         | evidence          |
| `POST /events`              | event fields                                                                                                            | `201 {event}`     |
| `PUT /events/{id}`          | event fields                                                                                                            | `{event}`         |
| `POST /events/{id}/confirm` | optional overrides                                                                                                      | `{event}`         |
| `POST /events/{id}/merge`   | `{into}` (event ID)                                                                                                     | `{event}`         |
| `POST /events/{id}/unmerge` |                                                                                                                         | `{event}`         |
| `DELETE /events/{id}`       |                                                                                                                         | `204` (tombstone) |

Every `{event}` result and the evidence read's event have the fields of a
`GET /events` item, `extractionId` (the extraction that wrote the entry, or
`null`) included.

Each event carries `review`, why it needs an operator's look, or `null`:
`{kind: "suspected", reasons: []}` for a live suspected cancellation,
`{kind: "unlisted", reasons: []}` for an unconfirmed automatic entry whose
posts were all read again as no schedule (see
[Event precedence](../data-model/event-precedence.md)), which the operator keeps
with `confirm` or removes with `DELETE`, or
`{kind: "uncertain", reasons}` for an uncertain addition, an unconfirmed
automatic entry starting on or after today in Japan whose model confidence
is below 0.7 (`lowConfidence`) or whose status is `tentative`
(`tentative`). Deleted and merged entries need no look. The overview's
review counts and `GET /schedule-attention` follow the same rule.

`provenance` is `automatic`, `manual` or `unconfirmed` (automatic and not
locked by an operator); `cancellation` is `none`,
`llm_suspected` or `manual_confirmed`; `order` is `asc` or `desc` (default);
`deleted=1` includes tombstones and `deleted=only` lists only them. Event fields are `title`, `detail`,
`startsOn`, `startsAt`, `endsAt`, `endsOn`, `timePrecision`, `eventType`, `tag`, `status`,
`url`, `platforms`, `cancellationStatus` and `cancellationReason` (required for
`manual_confirmed`). `eventType` is `event` (the default), `stream`,
`member`, `release`, `appearance` or `sale`. `endsOn` is a sale's close date
and is rejected for any other type; it is not before `startsOn`, `endsAt` then
falls on it in Japan, and a deadline (`endsOn` equal to `startsOn`) may have
`endsAt` without `startsAt`. A sale's `endsAt` requires `endsOn`. Saving
another type clears it. Every write is manual: it locks the event against
automatic extraction, and a delete is a durable tombstone. Confirming keeps
the stored values unless the body overrides them.

`platforms` are the entry's [announced platforms](../data-model/events.md#event-platforms):
an array of `youtube`, `bilibili` and `other`, each at most once, both
`youtube` and `bilibili` for a simulcast; null names none, an update
without the field keeps the stored ones, and any other value answers
`400 invalid_input`. A save writes them as the operator's (not inferred)
under the manual lock. A live-room link (`live.bilibili.com`) as `url`
answers `400 event_live_room_url` unless the platforms include `other`
(someone else's room); the schedule editor turns a pasted room link into
the bilibili platform instead. Confirming an entry whose stored `url` is a
room link without `other` drops that link, as readers already do. Each
event carries `platforms`, its announced platforms as readers see them (a
stored room link without `other` adds `bilibili`), `platformsInferred` as
stored, and `shownPlatforms`, the platforms the public reads show (with
those its merged entries lend it and a simulcast the live room observed);
the list marks the ones it does not announce. The order of `platforms` is
kept: the first is the entry's own platform.

`tag` is the calendar label: `live`, `game`, `talk`, `song` or `video`, an
empty value clears it, and `auto` hands it back to the automatic layers. A
saved label that differs from the stored one, a cleared one included, is an
operator's decision (`tagSource = manual`) and outranks the automatic ones; a
cleared entry shows no label. `auto` stores no label, no `tagSource` and no
attempt, so the entry reads on to its videos' tags and they are asked about
again; on a new entry it is the same as an empty value. The schedule editor
offers it as Automatic and selects it for an entry without an operator's
label;
a save that sends the stored label back unchanged, such as a cancellation
decision, keeps its `tagSource`. The whole row being locked already keeps a
later automatic write from replacing it. The list
reports `tagSource` as `manual`, `llm` (extraction prompt or merged X notice),
`title` (title inference, also set with a null `tag` when the title gave no
label) or null; see [Event labels](../data-model/events.md#event-labels) for their
precedence. `tagSource` is admin-only. An unknown value answers
`400 invalid_input`. Every
reader, including the board, `/api/calendar`, MCP
`dashboard_read` and `schedule_list`, reports the same label, derived from the
stored one and the entry's videos (see
[Shown label](../data-model/events.md#shown-label)); this list shows the stored label.

The list includes merged duplicates by default; `merge=merged` lists only
them and `merge=visible` excludes them, as every public reader does. Each
event carries `mergedInto`, `mergedIntoTitle`, `mergeSource` (`rule` or
`manual`), `mergeReason` and `mergedAt`. `hiddenByPosts` is true for an
automatic event that every public reader leaves out because each of its
source posts is hidden or deleted on X (see
[Events of hidden posts](../data-model/posts.md#events-of-hidden-posts)); the console
list keeps it under every filter.

`merge` hides the event behind the event `into` names, on any date, as a
durable operator decision (`mergeSource = manual`, `mergeReason = manual`)
that the reconciler never changes. The target must be another visible event:
itself, a merged or a deleted target, or a missing `into`, answers `400`; a
missing or deleted `{id}` answers `404`. Rule merges into the merged event are
released with it, and an operator's or another owner's merge into it answers
`409 {"error":"event_merge_conflict"}`, so no merge points at a merged event.
Repeating the same merge changes nothing. Deleting the target undoes the
operator merges into it, as `unmerge` would. `unmerge` shows a merged event again
as a durable operator decision (`mergeSource = manual` without a target) that
the reconciler never overrides; an event that is not merged answers `400`.
Editing or confirming a rule-merged event also shows it again, because a
manual event is never merged away; an operator merge stays. Merging,
unmerging and edits advance the dashboard revision.

`evidence` answers what an operator needs to judge an entry, deleted and
merged ones included, or `404 event_not_found`. `event` is the entry as the
list reports it. `posts` are the stored posts it was read from, oldest first,
each a post as `GET /posts` reports it with `roles`: `source` (one of its own
event sources), `merged` (a source of an entry merged into it) and
`cancellation` (the post that suggested its cancellation). `links` are the
`{source, url, role}` of event sources that are not stored posts, such as a
YouTube reservation. `nearby` are up to 20 other posts, nearest first and
listed oldest first, by the same source accounts from a day before the first
evidence post to a day after the last; an entry without a stored evidence
post reads the own X and Bilibili accounts' posts in the 72 hours before it
starts. Stored source JSON never appears.

## Schedule images

| Method and path                     | Body / query                  | Result                                    |
| ----------------------------------- | ----------------------------- | ----------------------------------------- |
| `GET /schedule-images`              | `limit` (≤ 200), `from`, `to` | `{items}`                                 |
| `GET /schedule-attention`           |                               | schedule work by week                     |
| `POST /schedule-images/{id}/review` | `{status, reason}`            | `{image}`; `404 schedule_image_not_found` |
| `PUT /schedule-images/{id}/period`  | `{start, end, reason}`        | `{image}`                                 |

`from` and `to`, both dates, keep the images whose period overlaps them; the
list is ordered by period start, newest first. `periodBasis` is `events` for
a period derived from the source post's events, and `inferredPeriodStart` and
`inferredPeriodEnd` give the period without the manual correction.
`sourcePost` is the stored source post's `text` and `publishedAt`, or `null`;
the board gate's stored search text stays on the server.
Each item also includes `llmText`, the model's visible schedule transcription,
or `null` when the image model has not returned one.

`GET /schedule-attention` is the Review queue's schedule work:
`{today, weeks: [{weekStart, images, events}], undated, truncated}`.
`weeks`, oldest first, hold from Monday `weekStart` the images that wait for
a decision (`needsReview`) whose period starts in the week, and the events
whose `review` is set that start in it, as `GET /events` reports them;
`undated` holds the waiting images without a period. Each kind reads at most
100 items; `truncated` says one had more.

An image waits for a decision when it has a URL and either has a
`previousDecision`, or has no label and a model verdict that is `pending`,
`uncertain` or `failed` or a `lastCheck` that `failed` or was `skipped`.
`previousDecision` is `{status, reason, at}` of the operator's label on the
image the asset showed before its URL changed, or `null`; any label, a
cleared one included, answers it. `lastCheck` is `{status, reason, at}` of
the latest check that left the model verdict in place (`running`, `failed`
or `skipped`), or `null`.

`status` is `schedule`, `not_schedule` or `unreviewed` (which clears the
label; the others require `reason`). A `schedule` label is the manual gate
that lets an image reach the image model. Each item's `mediaStatus` is
`ready`, `pending`, `failed`, `missing` (no usable cache file yet) or
`none` (the asset has no image URL, such as the legacy `weekly-schedule`
alias); `none` never needs review and keeps `previewUrl` null.
