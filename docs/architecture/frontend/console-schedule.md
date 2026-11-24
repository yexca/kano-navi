# Console Schedule, Videos and Review

The console's schedule, video list and review queue.

## Console Schedule

The console's schedule (`src/admin/views/schedule.tsx`) has a calendar and
a list. It opens on the calendar unless the browser remembers the list in
local storage. `#schedule/YYYY-MM-DD` opens the calendar on that day,
`#schedule/look-YYYY-MM-DD` on that day with only what needs a look, and
`#schedule/YYYY-MM` on the first of that month; `#schedule/images` opens
the list on the schedule images.

The calendar (`schedule-calendar.tsx`) works like a phone calendar around a
selected day, today at first. A compact Monday-first month grid sits on the
left, with previous, next and Today buttons above it: each day shows up to
four dots coloured by its entries' states (on the board, uncertain
addition, suspected cancellation, cancelled, hidden by its posts) as the
legend names, and each week row ends in an image mark when a schedule image
covers that week, honey while one waits for review. Choosing a day selects
it and its week, which is tinted; a double click starts a new entry on that
day. Paging months selects the first day, or today in the current month.
Under the grid, held in view, sit the selected week's schedule images
(`schedule-week-image.tsx`); those labelled Not a schedule fold away below
them, where This is a schedule takes them back. Each shows its state,
dates, source link and post, and offers Week earlier and Week later, which
shift its dates by seven days as a manual correction and select that week;
Fit to this week, when its dates are not exactly the week; and the label
decisions. The right side lists the selected week's
seven days, Monday to Sunday with their own previous and next buttons, each
with its rows and new entry button, empty days included, the selected day
marked. Merged duplicates sit under the entry they are merged into, each
with its undo. All and Needs a look (with the month's count) filter the
entries; while Needs a look is on, Next week to look at selects the next
week holding something that needs a look, round to the earliest after the
last, and Review queue (with the queue's count) returns to the queue. The
calendar has no search of its own: searching opens the list on all dates
with the matches. Under 1000px the grid, the images and the days stack.

The server decides which entries need a look and reports it as each
entry's `review` (see [Admin API](../admin-api/events.md#events)): the model suspects
its cancellation, or it is an uncertain addition. Its row's edge takes the
colour, honey or lavender, and a note in the row names the reason; a
suspected cancellation's note shows the model's evidence and confidence with
Confirm cancelled (saving the evidence as the reason) and Not cancelled, and
either note opens the entry's source posts below it. An uncertain addition
is settled with the row's Confirm or delete.

The list shows the entries or the schedule images. Its entries are grouped
under one heading per Japan date, with the day's
count and a button that opens a new entry on that date; Upcoming, Past and
All dates tabs set the date range beside it, and the status tabs All, Not
confirmed, Suspected (cancellation) and Deleted with a search field filter
it. Each row (`schedule-parts.tsx`) shows the time in Japan (or All day), the
title, which opens the entry dialog, its badges and its review note; an
unconfirmed entry has a labelled Confirm button before the icon actions. An
automatic entry whose every source post is hidden or deleted on X carries a
Hidden by its posts badge and is dimmed, since the public pages leave it out.

The list's schedule images are the newest stored ones as decision cards
(`schedule-images.tsx`) under Active, Approved, Undated (with its count) and
Not a schedule tabs. Active leaves out images an operator labelled Not a
schedule, so a rejected card leaves the list; the Not a schedule tab keeps
them, where the label can be cleared. Undated holds those without covered
dates, which the calendar cannot show. A card shows its source post's time and wording, folded to four
lines. Correct dates opens a wide dialog with the image and the whole post
above the form, side by side or stacked on a phone. A card's covered dates
note when they come from the post's events, and a dated card offers Show
its week, which opens the calendar on the day its dates start.

The entry dialog (`event-dialog.tsx`) is either timed or all day. A timed entry needs a start time. Its length
starts at the type default, the [calendar feed's](../public-api/feeds-and-endpoints.md#public-feeds)
length for the type, and saves that end as `endsAt` like the presets and a
custom end. **No end time** saves none, so the board treats a stream as
open-ended; a saved entry without an end opens on it, so an unrelated edit
never adds one. A sale replaces the length with a close date and an optional
close time (`endsOn` and `endsAt`).
A preview line renders the entry as calendar subscribers will see it.
An existing entry's dialog puts its evidence (`event-evidence.tsx`) beside
the form, scrolling on its own and below the form on narrow screens: one
timeline of the posts it was read from, labelled Source, Merged entry or
Cancellation, between the same accounts' nearby posts, which are quieter
and can be folded away; links of other evidence sit above it.

## Console Videos and Review

The console's video list (`src/admin/views/videos.tsx`) has platform tabs
(All, YouTube, Bilibili) above the featured video and a list with task tabs
(All, Upcoming, Missing translation and Queued, an open refresh or
translation request), which any platform narrows, and a search field.
Both are in the URL, `#videos/<platform>/<task>`, and either part may be
left out: `#videos/bilibili` opens the Bilibili uploads and `#videos/queued`
the Queued tab on every platform. Uploads merged into another video appear
on that video's row, so only the Bilibili tab lists them on their own. Each row shows the
thumbnail, the title, which opens the correction dialog, the platform, time,
kind (for a Bilibili upload with who settled it, or a honey badge while it
is unsettled and not public), tag, length and format, its badges and merge
line, the translation chips, and the feature (public videos only), correct
and open actions. A merge badge opens the other side's correction dialog:
the YouTube row's badge the upload, the upload's badge its YouTube video.
The correction dialog
(`video-editor.tsx`) carries no explanatory text. Its left column shows the
board's row as the draft leaves it (the chosen thumbnail, title, source, kind,
tag, format and the number of corrections) and when the page was last read,
then below a hairline the corrections: title, kind, tag, format, who can
watch, and the thumbnail files when any are stored, as fixed-size tiles with
a check on the chosen one. Its right column is one soft panel as tall as the
left, holding the platform's record: the merge section, its field and button
on one row, and, below a hairline, what the platform replaced when it
replaced anything, each entry a two-line title with its date. Narrow screens
stack the panel below the corrections. A corrected control is tinted and
offers an icon reset to the platform value beside its label. The footer holds
the refresh as a quiet button, the platform link, cancel and save.

The Review queue (`src/admin/views/review.tsx`) lists only what waits for a
decision, in tabs for the schedule, video merges and posts; a decision
removes the item and the sidebar badge counts again; its post rows use the
pieces shared with Posts in `post-parts.tsx`. `#review/images` and
`#review/cancellations` open the schedule tab. The schedule tab
(`review-schedule.tsx`) reads the schedule work by week (see
[Admin API](../admin-api/events.md#schedule-images)) and shows one group per week,
oldest first: its dates, marked when it is this week, its counts of images
to review, suspected cancellations and uncertain additions, and Open in
calendar, which opens `#schedule/look-<Monday>`. The uncertain additions
follow as quiet lines with their reasons, to settle on the calendar beside
the rest of the week; then the pending images' and suspected cancellations'
decision cards. Weeks before last week fold under Earlier weeks, and
undated images form their own group, whose cards set their dates. An image
or a suspected cancellation is a decision
card: its left edge takes the state colour (honey
while it needs a decision, leaf once the image reaches the board, neutral
when rejected), the image or the entry's date sits on the left, and the body
shows the badges and source link, the model's verdict with a confidence bar,
the extracted schedule text when present, and its reason or evidence, the facts,
and a bottom bar with the note field
and the decision buttons. A cancellation needs a reason before it can be
confirmed, and its Source posts button opens the same evidence timeline in
the card. The video merges tab (`merge-parts.tsx`) lists each Bilibili
upload the merge rule merges at its next run or leaves to the operator,
beside its candidates: Merge attaches it to one, and Keep separate keeps it
apart so the rule never merges it; a live replay's candidate is marked as
the archive of the same broadcast. The Bilibili form tab lists the uploads
the model was unsure about or failed on, each with its cover, title, length,
publication time and the broadcast the replay rule read, and Upload and Live
replay buttons that set its form. The posts tab has two labelled filters:
Step, Schedule extraction or Translation, each with the overview's failure
count, and State within the step: Failed or Uncertain for extraction;
translation has only failures and shows no State filter. A new step keeps
the state when the step has it and falls back to Failed otherwise; below
560px each filter takes its own full-width line. Posts queued for a run wait
on the run, not the operator, so the tab lists none of them: a note under
the filters counts the step's queued posts from the overview's `backlog` and
links to them in the post list. Each post is a card whose left edge takes
its state's colour, danger for a failure and honey for an uncertain
extraction: the account, time and post link above the wording, with up to
three of its images beside it (below it under 640px) and its quote under
it, then, past a hairline, a verdict line and the actions. The verdict
names the state (Schedule extraction failed, Extraction uncertain or
Translation failed) with its icon in the edge colour, the error, when the
step last tried, the automatic retry or its stop, and a Queued mark while a
run waits for the post. A failed extraction offers Dismiss beside Process
again; a translation card offers Translate again, which queues a machine
pass. The list pages 50 posts at a time.
Each row has a selection box on its left edge, and shift-click selects the range from the last toggled row. A selection
bar above the list, which sticks under the top bar, selects the whole page
and applies the state's actions to every selected post in one request:
Dismiss, Process again and Refresh from source for failed extractions,
Process again and Refresh from source for uncertain posts, Translate again
for failed translations; a refresh skips posts that are not own X posts. A new filter or
page clears the selection.

Console dates are `YYYY-MM-DD` and times `YYYY-MM-DD HH:mm`, in Japan time
and the same in every console language (`src/admin/format.ts`); a stored
day adds its short weekday.
