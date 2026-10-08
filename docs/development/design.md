# Design and Interaction

The public board should be easy to scan and keep source provenance visible.
Use the shared semantic tokens in src/index.css and the existing shadcn/ui-style
primitives in src/components/ui/. Preserve light/dark themes and Japanese,
English, and Chinese interface copy. Do not introduce a separate token system
for a single page.

## Page Boundaries

- / is the public snapshot board. Refresh reads the local API and preserves known
  state when a request fails.
- /history is static, sourced content with local fixed images and original links.
- /about is a static project, attribution, and code-license page.
- /admin is the hidden operator console. Its styles use adm- prefixed selectors
  because all pages share a bundle. Do not add it to public navigation.

Keep loading, empty, stale, error, and unavailable-media states explicit. Never
replace an unready local media URL with a remote CDN URL. Preserve event date
precision and show Asia/Tokyo times; do not invent a start time for date-only
records. Distinguish automatic, manually confirmed, suspected cancellation, and
operator-confirmed cancellation states.

## Interaction Review

Check desktop and mobile layouts on the affected pages. Preserve semantic
buttons, labels, keyboard navigation, visible focus, and dialog Escape/focus
behavior. Use status color to communicate state, with text or an icon available
as another cue. Keep implementation details out of visitor-facing copy.

## Related Docs

- [Product overview](../overview.md)
- [Frontend ownership](../architecture/frontend.md)
- [History provenance](../product/history.md)
- [Testing](testing.md)
