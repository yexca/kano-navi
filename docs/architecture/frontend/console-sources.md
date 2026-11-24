# Console Overview, Sources and Posts

The console's overview, queue, sources, manual fetch, posts and data views.

## Console Overview, Queue and Sources

The console's Overview (`src/admin/views/overview.tsx`) opens with the
queue: the active run with its steps and Cancel, or Idle, then the timers in
the order they start, from the overview's `queue`. A due timer shows Due and
whether it waits for the current run; a source timer that is not due has
Start now, or Run next while a run holds the queue. The live-status probe is
listed without a button. While timers are off in the process the panel says
so instead of listing them. Under the timers, Waiting for a run lists the
overview's `backlog`: each kind of operator request that has any (posts to
extract again, X posts and videos to read again, posts and video titles to
translate), with its count and the run that takes it. A row opens the post
list or the video list on those requests; nothing shows while none waits.
Beside Source timers, What runs next opens the Runs page's Up next tab.

The Runs page switches between History and Up next (`#runs/next`). Up next
(`src/admin/views/queue.tsx`) has one tab per step (`#runs/next/schedule`,
`youtube`, `translate`, `x-refresh`), each read from its
[queue preview](../admin-api/runs.md#queue-preview) when the tab opens, on Refresh
and when a run finishes. Schedule extraction lists the posts one run
processes in order, numbered, each with why (requested, new post, input
changed, backoff over) or the skip that takes its place, its route and
source, and Open post, which opens the post list searched for it; counts of
the posts left for later runs, cache hits, posts in backoff with the next
retry and stopped retries follow, and the candidate counts head the tab.
YouTube details lists the detail queue in group order with each run's
watch-page and Data API budget and notes that videos a run's listing turns
up join it unlisted. Translation lists the step's candidates, marking those
past the per-run calls that a run reaches only when earlier ones need no
call. X refreshes lists refresh requests oldest first, each marked for the
next run, a later run, or an account the X step does not read. A step turned
off for the service or with nothing to work with says so above its list.

The console's Sources page (`src/admin/views/sources.tsx`) has a row of
tabs for X, YouTube, Bilibili uploads, Bilibili dynamics and the official
shop, each with the platform's logo (a shopping bag for the shop), a dot (or
a lock) for whether its timer runs here and the state as its tooltip.
The selected source opens with a summary: its timer state, next run, last run and last successful read, the timer switch (which saves
at once) and Run now. While the timer runs, Run now starts it as the
overview queue's Start now does, and reads Run next while a run holds the
queue; while it is off, Run now runs the source's steps once and waits for a
free queue. A source that `SKIP_*` turns off is locked. The Bilibili
tabs' summaries add the login cookie's state and the shared captcha backoff
as chips, with a note on setting `BILIBILI_COOKIE` or `BILIBILI_SESSDATA` when the
configured cookie expired or, for dynamics, none is configured. Below the
summary the main column holds the timed reads
(`src/admin/views/source-timed.tsx`), one form with one Save and
Discard: the interval and, for X and YouTube, the sync setting; and the
[method chain](../workflows/source-schedules.md#method-chains), with every method's switch and
up/down moves, a method the process cannot run greyed with the variable it
needs; the related-accounts method is read-only, while the Bilibili
dynamics timer exposes its desktop and WBI methods and the shop its signed,
unsigned and Atom reads for ordering and toggling. Save sends only the
changed fields, and the timer switch keeps unsaved edits.

Settings below the timed reads are folded (`src/admin/views/source-parts.tsx`
holds the fold): a header row with the title,
its state and any quick switch opens onto the settings and figures, and
each fold saves on its own. X adds its X API limits (gate and daily cap,
with the API's place in the chain and today's usage in the header, and
today's usage and each account's timeline failures shown as meters against
them). X related adds the related accounts panel
(`src/admin/views/related-accounts.tsx`): each account's handle and keyword
rules side by side (stacked on a phone), the rules entered as chips in the
shared tag input, where Enter or a comma adds one, with its last read,
stored and promoted counts and last error below, an Add account button that
starts with the default rules, and one Save; a link opens External posts.
The panel and the related manual fetch share the tab's one read of the
accounts, made only while the tab is open. YouTube adds its progressive
confirmation (`src/admin/views/source-live.tsx`, with the Bilibili live room
fold), where push and the
live-status probe are folds with their switch in the header (the probe's fold
also lists its active videos, each with Stop checking, and its stopped
videos, each with Check again), followed by
the read-only
[follow-up checks](../sources/youtube-videos.md#follow-up-checks-and-refresh-requests), and
yt-dlp, with its status in the header. Bilibili uploads adds the Bilibili
live room fold, the live-status probe's Bilibili part: the room's state and
the check's switch in the header, its interval, next check and the stream
entries it follows, then the room's last check.
The shop tab (`src/admin/views/shop-parts.tsx`) adds the shop read's state:
its sale entries, last read and error, whether it runs here and the wait a
429 answer set.
X and YouTube fold their timed-read
limits in the main column and the manual-fetch limits, which they share,
under the manual fetch, one field per row with its label beside the input.
Both limit folds edit the page's one read of the settings. Titles, fields
and folds carry labels, states and value ranges only; how a method, step or
limit works is described in the architecture pages they belong to.

The manual fetch (`src/admin/views/source-fetch.tsx`) sits beside the main
column: method, for X every own
account or one of them, mode, count or dates, options, then the newest
fetch's summary. The method and mode pickers show only when there is more
than one to pick, and only the date range carries a hint. The X related
tab's fetch picks one related account,
shows its last read, and offers schedule extraction and translation; its
summary counts the statuses listed, promoted and demoted. The Bilibili tabs'
fetch form is disabled while the captcha backoff lasts, with its end. The
shop's is one button that reads the whole catalog, disabled while a 429's
wait lasts, then the newest read's listed products, dated products of Kano,
sale periods and pages. Below the fetch, YouTube adds one video by ID or
link and the Bilibili tabs one upload by BV ID or link or one dynamic by its
link, each disabled while the source does not run here. On a narrower screen the side column follows
the main one, and on a phone the tabs share the width and show only the
source names, and a fold's header controls move under its title.

## Console Posts and Data

External posts (`src/admin/views/external-posts.tsx`, in the Content group
after Posts) has a platform tab per status pool, X and Bilibili, with no
All tab since the pools take different actions (`#external/bilibili`). The
X tab lists the
X status pool, without Kano's own accounts' statuses, in the post list's
feed layout:
author, name and Japan time; the wording with its image count, links to the
own posts that quote it (opening Posts searched for the post), the accounts
that reposted it, a link to a promoted status's post in Posts and a locked
decision's reason; the translation summary of a status that has no post;
badges for its state (On the board, Demoted, Stored only), a lock
and its reasons; and icon actions to promote a related account's status or
a reposted one, demote a promoted one, return a locked one to the keyword rules and open it
on X. Promote and demote open a dialog that shows the wording and requires
a reason. Filters cover the text or ID, the author (the related accounts),
the reason and the state; a header link opens the related accounts on
Sources.

The Bilibili tab lists the Bilibili status pool, without forwards of
Kano's own dynamics, in the same layout and read-only: the Bilibili mark
with the author's name and mid, the Japan time, the wording with its image
count and a "Forwarded in a post" link to each forward in Posts (marked when
that forward is hidden, hides its quote or shows it unavailable), the
translation editor, badges for whether a forward shows it (Public, Not
public, Gone), Video for an upload and its reason, and a link to it on
Bilibili. It has no promote or demote, since the forward's quote shows the
dynamic. Filters cover the text or ID, the author's mid or name, and the
kind.

The console's post list (`src/admin/views/posts.tsx`, its rows and editor in
`post-item.tsx`, and the images, quote, status and actions it shares with
the Review queue in `post-parts.tsx`) has platform tabs
(All, X, Bilibili; `#posts/x`, `#posts/bilibili`) above a feed of posts
separated by rules, one row of columns per post: the account over the Japan
time, the wording (three lines) and its quote, the post's own cached images
(up to three, "+N" counts the rest), the translation status, the extraction
state with a Hidden badge for a post hidden from the public pages, a
Promoted badge, which opens External posts, for a related account's
promoted status, a Refresh queued badge with the request's time, and a
Schedule image badge for a post with a schedule image record, and the
edit, "process again with the LLM", refresh from the source (an own X post
only, disabled while one is queued) and open actions. The columns after the
wording have fixed widths, so they line up from post to post. A text-only
post shows no image cell; a post whose images are not cached yet shows one
placeholder with the reason. A feed under 1000px puts the account, time,
state and actions on one line above the wording, images and translations,
and under 560px the state gets a line of its own. A hidden post's wording is
dimmed. An own X post has a selection box before its account; shift-click
selects the range from the last toggled post. A selection bar above the
feed, which sticks under the top bar, selects every own X post on the page
and queues a refresh from the source for the selected posts in one request;
a new filter, platform or page clears the selection. Beside the account,
extraction, translation and date filters,
Queued for keeps the posts with an open operator request: any, Extract
again, Read again from X or AI translation. The account filter lists the X
accounts and is absent on the Bilibili tab. A platform keeps the other
filters and starts the list again at its first page. A
`#posts/queued-<kind>` link opens the list on one kind, after the platform
when there is one (`#posts/x/queued-refresh`).

Edit opens the post in a dialog: the whole wording, every cached image, the
translations, the quote, the schedule image form and a switch that hides the
post from the public pages or shows it again, with the LLM, refresh and open
actions in its footer. An X post with images can be marked as a schedule image there:
the image (a choice when the post has several), a required reason and an
optional first and last day; a post with a record shows its badge and a link
to the Schedule list's images.

The quote is a read-only card on the sunken surface under the wording, so
its thumbnails never read as the post's own images. Its header holds the
author, handle and time, a badge for a quote hidden from the board, and icon
actions that hide the quote from the board or show it again and open the
quoted post. Below come the wording, folded to three lines with a
"Show full quote" toggle that appears only when the clamp cuts it, the ready
quote thumbnails that open the image viewer, or the unavailable state. The
edit dialog shows the quote's whole wording. Nothing edits the quote itself.

The Content group also includes `Account avatars`
(`src/admin/views/account-avatars.tsx`) and `Profile images`
(`src/admin/views/profile.tsx`, with its previews in `profile-parts.tsx`, the
own-account cards in `profile-own-accounts.tsx` and the upload and fetch
dialogs in `profile-dialogs.tsx`). `Account avatars` lists tracked own and
related X accounts with role filters, `auto` / `manual` / `hidden` mode
switches, manual latest-avatar checks, image URL or file uploads, candidate
history controls (`Use & lock`, retry download, delete), and an action to copy
an own account's ready avatar into the global site avatar slot. `Profile images`
verifies and previews the global site avatar and banner, groups candidates into
own X account avatars (with latest-avatar check and apply-to-site action),
YouTube & custom avatars (with YouTube discovery and URL/file upload), and
banners (with X account or YouTube discovery and URL/file upload), and links
to `Account avatars`.
