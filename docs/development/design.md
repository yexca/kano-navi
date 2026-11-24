# Design and Interaction

The public board should be easy to scan and keep source provenance visible.
Each area styles its own markup in its own stylesheet (`src/site/site.css`
for the public shell, `src/dashboard/board.css`, `src/admin/admin.css`, and
one per page or shared component), with class names scoped to the area or
component, over the CSS variables in `src/index.css`. New markup takes such
classes rather than Tailwind utilities; the utility theme in `src/index.css`
serves the `src/components/ui/` wrappers and the error pages in
`src/main.tsx`. Public pages reuse the shell's controls in `src/site/site.css`,
such as `.round-button` and `.pill-button`; the console reuses the
`src/admin/ui.tsx` components (buttons, badges, panels, fields, switches,
filters, pager, modal, toasts). Menus, tabs and tooltips use the Radix
wrappers in `src/components/ui/`, restyled with an area class.
`src/components/ui/button.tsx` serves only the header's preference controls:
its colours are the fixed `ink` and `coral`, which have no dark-mode values.
Public pages use cream paper with hairline rules rather
than boxing everything, a rounded display face for names and titles, tabular
numerals for times, and strawberry gingham as the signature edge. Admin keeps
its own adm- styles on the same tokens. Preserve light/dark themes and the
English (base), Japanese, and Simplified Chinese interface copy. Do not
introduce a separate token system for a single page.

## Page Boundaries

- / is the public snapshot board. Refresh reads the local API and preserves known
  state when a request fails.
- /posts is the post archive on the public shell: one newest-first column
  grouped by month and day, the day's date in the display face beside its
  posts, which lie on the paper between hairlines rather than in cards; a
  year opens with its number large under a gingham strip. On wide screens a
  sticky rail beside it holds the account cards (portrait, years and total;
  the chosen one in berry), one or two a row, over
  a map of every year's months shaded in berry; narrower screens put the cards
  above the column and get one slim band of month controls joined to the
  header instead of the map, naming the month and day in view. The cards
  scroll sideways in one row on phones, and the date heads each day there
  without sticking.
- /journey is static, sourced content with local fixed images and original links.
- /works is the static discography in an editorial treatment: on top of the
  shared shell it gives the shared tokens a warm ivory and clay reading in
  both themes, sets names and figures in a serif, separates with hairlines
  and outlined chips, and lets each cover's record slide out of its sleeve
  (a photo of the real disc where there is one; where no disc was pressed,
  the cover printed on a disc with a clear hub). The header sets the
  figures cut from the acoustic cover trilogy's covers in a small landscape,
  which in one column sits between the title and the lead:
  each album's grooved disc sinks behind two far ridges like a setting sun,
  and the figures stand and sit on curved grassy ground with flowers in the
  three albums' colours, the land fading out at its edges. The scene has two
  sides, album order on side A and two leading on side B; a small two-sided
  record at its top right turns over to flip them while one and two sink and rise in
  their new places and the ground and flowers morph between the sides'
  curves, and this device remembers the side. It falls back to
  the trilogy's sleeves when a cutout is missing. A cover shelf of Kano's own releases by year opens
  a sheet for each release, one size for every release and edition. Its
  left pane holds the sleeve over a blurred wash of the cover, the label, one
  row of figures, and a card for each edition with that edition's cover and
  format, the chosen one drawn in ink. Choosing another edition or cover
  keeps the cover and its wash until the next has loaded, then fades it in
  over them; a cover still loading shows a skeleton with a sweeping sheen.
  The tracklist and details fade in place, and a picker under the sleeve
  that only some editions have folds open or shut, so the cards below glide
  instead of jumping (all at once under reduced motion). The tracklist has a round photo beside
  each disc that has one; with photos of two or more discs, round photos
  under the sleeve choose the disc that turns in the header, which slides
  back into the sleeve to change (in place under reduced motion) and is
  marked in ink in the tracklist. The details are the chosen edition's facts and
  bonuses, or for a release sold in one version its type, date, label and
  catalog number, then the official streaming and download listings as
  outlined chips with the store's name (the page listing every store drawn
  in ink first, high resolution as a small mark, each extra item such as a
  TV-size single named above its own chips). On the widest screens the tracklist and the details take
  columns of their own; a wide screen puts the details below the tracklist
  in one right pane, and narrower screens stack everything in one scrolling
  column. The panes'
  scrollbars are thin and show while the pointer is over them, and each
  scrolling pane reserves its gutter, so switching editions never shifts its
  content sideways. The section tabs carry their counts and
  no separate figures or notes; a tab chosen by pointer keeps no focus
  outline, while keyboard focus draws one the tab bar leaves room for. A
  closed sheet returns focus only to the control that opened it, and the
  section panels take no focus themselves, so a tap or a drag on their empty
  space draws no focus ring. The official discography is a small pill after
  the years in the eyebrow. On phones the English title fits one
  line, the filter chips scroll sideways in one row, and a cover's record
  rises only while a finger is on it. The works she
  is featured on are a list; her uploads are a grid of 16:9 covers (on phones
  a list of rows, thumbnail beside words), with the site marks as small icons
  at the end of the date line; at the end of the title a disc opens a menu
  of the albums including the recording and, for an upload of many songs,
  a list mark opens its numbered songs; and a
  printed sleeve in one of the palette's tones where no thumbnail is saved.
  Uploads after the collection date sit above the toolbar like a year of
  the list, the heading in its side column, each card with an outlined mark
  under its title for found automatically or added by hand (in clay); two
  rows of the grid, or four of the phone list, show until a pill under them
  opens the rest.
  The order is a single icon that ends the toolbar's row; search and
  narrowing toggles join that row on a wide screen and take a second row
  otherwise, and search shows focus by thickening its clay underline rather
  than boxing the field. Rows rise
  in as they scroll into view where the browser ties animation to scrolling,
  by movement alone: a fade tied to scrolling stalls frames while covers
  decode. Motion stops under reduced-motion settings. Codes get interface copy;
  titles and credits keep their wording and language.
- /lineblog is the static LINE BLOG archive. It reproduces the archived blog's
  own layout (its header art, white menu bar with its Home icon, article column beside a
  profile sidebar, its font stack and accent colour) inside the shared
  header and footer, and on phones the blog's smartphone template (white
  boxes on a grey page); its surfaces and text still come from the shared
  tokens, so both themes apply.
- /about is a static project, attribution, and code-license page.
- /admin is the hidden operator console. Its styles use adm- prefixed selectors.
  Do not add it to public navigation. Its copy is labels, states, counts and
  short hints for a format or an otherwise hidden action; how a feature works
  belongs in the docs, so panels, page headers and switches take no
  description. Lazy page loads show a localized status
  and offer a full reload on failure, using the shared theme tokens.

Keep loading, empty, stale, error, and unavailable-media states explicit. Never
replace an unready local media URL with a remote CDN URL. Preserve event date
precision and show Asia/Tokyo times; do not invent a start time for date-only
records. Distinguish automatic, manually confirmed, suspected cancellation, and
operator-confirmed cancellation states.

## Interaction Review

Check desktop and mobile layouts on the affected pages. Preserve semantic
buttons, labels, keyboard navigation, visible focus, and dialog Escape/focus
behavior. Open images through the shared image viewer and give each opener the
`media-opener` class, so every page shows the same cursor and closes the same
way. The page always reserves the vertical scrollbar's gutter, so a page
growing past the viewport or a dialog or menu locking scroll never changes its
width; scroll locks hide overflow and add no compensating margin or padding.
Use status color to communicate state, with text or an icon available
as another cue.

## Related Docs

- [User guide](../user/index.md)
- [Frontend ownership](../architecture/frontend.md)
- [History archive maintenance](history-archive.md)
- [Testing](testing.md)
