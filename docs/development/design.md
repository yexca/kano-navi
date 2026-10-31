# Design and Interaction

The public board should be easy to scan and keep source provenance visible.
Use the shared semantic tokens in src/index.css, the public shell and controls
in src/site/, and the existing shadcn/ui-style primitives in
src/components/ui/. Public pages use cream paper with hairline rules rather
than boxing everything, a rounded display face for names and titles, tabular
numerals for times, and strawberry gingham as the signature edge. Admin keeps
its own adm- styles on the same tokens. Preserve light/dark themes and the
English (base), Japanese, and Simplified Chinese interface copy. Do not
introduce a separate token system for a single page.

## Page Boundaries

- / is the public snapshot board. Refresh reads the local API and preserves known
  state when a request fails.
- /history is static, sourced content with local fixed images and original links.
- /lineblog is the static LINE BLOG archive. It reproduces the archived blog's
  own layout (its header art, white menu bar with its Home icon, article column beside a
  profile sidebar, its font stack and accent colour) inside the shared
  header and footer; its surfaces and text still come from the shared tokens,
  so both themes apply.
- /about is a static project, attribution, and code-license page.
- /admin is the hidden operator console. Its styles use adm- prefixed selectors.
  Do not add it to public navigation. Lazy page loads show a localized status
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
way. Use status color to communicate state, with text or an icon available
as another cue. Keep implementation details out of visitor-facing copy.

## Related Docs

- [User guide](../user/index.md)
- [Frontend ownership](../architecture/frontend.md)
- [History archive maintenance](history-archive.md)
- [Testing](testing.md)
