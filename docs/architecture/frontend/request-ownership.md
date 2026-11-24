# Request and UI Ownership

Where composition, requests, locales, translations and media paths live.

Keep app composition in src/main.tsx, public snapshot requests in
src/dashboard/use-dashboard.ts, and admin requests and polling in
src/admin/use-admin-data.ts. Reuse the existing state and request patterns.
Shared primitives must remain independent of dashboard, history, and admin
features. Extract cohesive helpers when needed; avoid a repository-wide move
for a small change.

Every page, the board included, loads through a dynamic import with its own
CSS and translation chunks, so a direct visit downloads the shared runtime
(React, the settings provider and the base dictionary) and that page's code
only. The first page's import starts as soon as `src/main.tsx` runs. Because
the server answers every page with the same `index.html`, the board's chunks
cannot be listed there statically: the build
(`scripts/vite-page-preload.ts`) writes a small script ahead of the entry
that adds `modulepreload` and style `preload` links for them only when the
address is the board, so they download alongside the entry. The shared
settings provider owns persistent theme, interface language (`kano-locale`)
and content language (`kano-content-locale`, a locale or `interface` to
follow the interface); page message providers
extend its translator without duplicating preferences. The base dictionary
holds the copy shared by pages (header, footer, navigation, page status and
the translation toggle); the board's own copy is in
`src/dashboard/dashboard-i18n.ts`, which takes its post wording (the wall's
filters, cards and media, the month bar, the archive state) from
`src/dashboard/post-i18n.ts`. The `/posts` page loads that post wording and
its own copy, not the rest of the board's, and likewise only the board's
base, posts and month-dialog stylesheets from `src/dashboard/styles/`. Each
page sets its title and link-preview description through `usePageMeta`
(`src/site/page-meta.ts`). While a page's code loads, the current
page stays on screen, or on a direct visit a localized loading status
appears after a short delay; a page error boundary offers a full-page
reload when a chunk fails (including after a deployment). Direct visits and
trailing-slash routes work through the existing SPA fallback, and
`/index.html`, which the server answers as a file, is the board
(`src/lib/page-path.ts`). Any other path
shows a localized not-found view with a link back to the board instead of the
board itself; the server answers it with status 404, so `src/main.tsx` and
`spaPages` in `backend/internal/httpapi/static.go` list the same pages.
History, the LINE BLOG archive and About
remain static and never mount the dashboard hook or request its API.

English is the base locale. Every message exists in English, a key missing
from Japanese or Simplified Chinese falls back to it, and a browser language
that is none of the three selects it. Stored records are not interface copy:
event status codes and the generated YouTube details become labels through
`src/lib/event-text.ts`, which also reads the stored legacy Chinese values,
while other details and operator notes show as stored (see
[Stored event text](../data-model/events.md#stored-event-text)).

## Translations and Media Paths

Post text and video titles come with server-resolved `translations` (see
[Translations](../data-model/translations.md)). `src/components/translated-text.tsx`
chooses what a reader sees from the content language, not the interface
locale: each content language gets that locale's translation by default,
except that Japanese content keeps wording detected as Japanese
(`pickTranslation`). The header's language menu
(`src/components/preference-controls.tsx`) sets both; the content language
defaults to "Same as interface", and the operator console shows only the
interface choice. The LINE BLOG paragraph translations and the schedule
month grid follow the same content locale, while feed links, history copy
and the bilibili-first rule follow the interface locale. Translated wording
is followed by `TranslationToggle`, a translate icon followed by the
origin's mark (`translationMarkKind`): "AI" for a machine translation, the
YouTube or bilibili logo for a platform translation, and nothing for a manual
translation or another platform's (`is-icon`). It is lit (`is-active`,
`aria-pressed`) while the translation shows; selecting it shows the source
and turns it off. Its
tooltip and accessible name spell out the origin (AI, platform or editor),
and on a title the tooltip holds the source. A post's switch follows its
last word; while the board's six-line clamp hides the end of the post, it
keeps its hidden place in the text and a copy shows beside Read more. The
LINE BLOG archive's marks use the same class and states. Video and schedule titles
go through `src/dashboard/components/translated-title.tsx`: `TranslatedTitle`
for a card title (spotlight tickets, shelf tickets, cards and rows, archive
rows, week-guide entries) and `TranslatedHeading` for the shelf's lead card.
A button cannot go inside a link, so these cards are
not links themselves: the title is the link (`.card-link`), and
`followCardLink` in `src/site/links.tsx` opens it from anywhere else on the
card. A week-guide entry's title is instead the button that opens the
entry's sheet, which holds its link, and `followCardLink` presses it the
same way. A clamped title clamps only its wording, so the switch stays visible,
and while the clamp cuts it, hovering the wording shows it whole as its tooltip.
Month-grid chips are too small for a switch and show the reader's wording
unmarked. An operator's
Featured title from the focus record shows as written. The wording shown
carries its `lang` attribute. The console's
completeness chips and translation editor are in
`src/admin/views/translation-parts.tsx`, shared by posts and videos.

Media paths the page builds from its packaged catalogs (Journey images, LINE
BLOG media, Works covers) and the bundled fallback branding pass through
`src/lib/cdn.ts`, which prefixes the base the server writes into
`index.html` (`CDN_PUBLIC_BASE`); without it the paths stay root-relative.
They resolve when read (`mediaUrl`, the `url` getters of the Journey items and
the fallback profile), not when a module loads, so import order does not
matter; the export scripts take the root-relative `mediaPath`/`path` instead,
and the packaged API stays root-relative for the server to rewrite.
Addresses the API returns already carry that base. `vite build` validates and
normalizes the variable with `scripts/cdn-base.ts`, which follows the server's
rules, so the bundle names the same base as the page and the API.

Board images from the media cache, LINE BLOG archive images, Works covers
and Journey images load through `srcset`: `src/lib/media.ts` lists the server's
[resized copies](../media-cache/resized-copies.md) below an image's known width,
and each card or thumbnail passes a `sizes` hint for its layout. The
full-image viewers, profile media and other bundled assets keep their single
URL.
The schedule header's Subscribe menu (`calendar-subscribe.tsx`) offers the
[calendar feed](../public-api/feeds-and-endpoints.md#public-feeds) as a `webcal:` link and a copyable
`https` address built by `src/lib/feeds.ts` from the page's origin. Both carry
the reader's language as `?lang=ja` or `?lang=zh-CN`; English keeps the bare
path. The settings provider points `index.html`'s Atom and calendar discovery
links at the same language whenever the locale changes.

SQLite stores one text per seeded timeline and link row. The board translates
that text through src/dashboard/seed-copy.ts, keyed by the exact seed wording
in static-seed.json, so a row whose text was changed shows as stored. Update
both files together; the frontend tests require English and Japanese copy for
every seed row.

## Installable App

`index.html` links `public/manifest.webmanifest` (name "What’s Kano Up To?",
short name "Kano Navi", `start_url` and `scope` `/`, `display: standalone`,
background and theme colour `#fdf9f6`, the light `--background` token) and
the icons in `public/assets/icons/`: a 32 px favicon, 192 and 512 px icons, a
512 px maskable icon with the avatar inside the 80% safe zone on the
avatar's own pink, and a 180 px `apple-touch-icon`. They are resized copies of
the bundled `public/assets/kano-avatar.jpg` and follow its provenance; the
theme-color metas switch between the light and dark backgrounds.
There is no service worker or offline mode: an installed app loads the same
pages, which read the live local API. The Go static server serves
`.webmanifest` as `application/manifest+json` and, like other unhashed public
files, revalidates the manifest and icons on each use, so a replaced icon
takes effect without renaming it.

## Provider Company Icons

The provider presets are OpenAI, Anthropic, DeepSeek, OpenRouter, Gemini, and
Custom. Company marks are bundled local SVGs from Lobe Icons 1.90.0 (MIT);
they never require a browser CDN request. The license ships in
`public/assets/providers/LICENSE.txt`. Configured providers use the API host,
name, or stable ID to identify a mark; unknown custom providers retain their
initial. Removing a preset does not delete saved providers.

The Detection view shows the detection mode and keywords, then the text
schedule extraction, image extraction and translation/light-task queues side
by side, then
the providers as a list beside the selected provider's detail
(`#detection/<provider>`), the model limits, and the recent failures. Recent failures
lists each stored failure's stage, relative time (the Japan time on hover),
message, model and a link to its post or video; it loads with the view and
on its reload button. A queue change
(add from the model picker, drag or move, remove) shows at once and is saved
in order: each queue has at most one save in flight, and edits made meanwhile
collapse into the latest order, sent next. A refused save returns the queue to
its last stored order and drops the edits that followed it. Each queue marks
the model each stage calls first. The provider dialog holds only the connection, with the presets above
it; there is no default model. Saving a new provider with a key selects it and
lists its models, an explicit operator action. The detail adds ticked models
to the catalog, edits their tags, turns them on or off, puts them in any queue
whose tag they carry and tests them one at a time. Removing a model takes a
second click on its danger-toned icon within a few seconds.

List filters in the console are one row of pills, one per filter, with a
date range pill and a clear button that appears while a filter narrows the
list. A choice filter opens its own list under the pill, which follows the
listbox keyboard pattern. Paged lists show the total, the first and last
page numbers with two either side of the current page, and a jump field
that goes to the nearest existing page. The Runs page puts the run history
beside the open run on a wide
screen (the newest run is open until another is chosen) and opens a run in
place on a narrower one. The open run is in the address (`#runs/<id>`): the
overview's recent runs link there, and a linked run scrolls into view, read
on its own and listed first when newer runs pushed it off the first page. A
run lists its steps one line each: the step's
outcome, its window and up to three non-zero counts, problems first. A step
opens to everything it counted, as label and value lines with zeros
receding; failed and partial steps open by themselves. Run details explain
stored error codes in words and keep the code beside them. A failed or
partial run copies its diagnostics for an agent or an issue: each step's
outcome and error code, then the stored run as JSON, with codes and English
labels so the text matches the source. A browser that refuses the clipboard
shows the text in a dialog to copy by hand.
