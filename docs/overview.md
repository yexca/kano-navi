# Project Overview

## What This Is

This is an unofficial status board for Kano Mahoro fans. It is intended to make
the current situation easy to scan at a glance: recent X posts, a future-first
activity calendar, the latest focus item, YouTube updates, a timeline, and public
resource links. It is not a publishing platform and does not replace X,
YouTube, or any other original source.

## Goals

- Aggregate public updates into a stable, scannable page.
- Store external data as a local SQLite snapshot instead of fetching on every page view.
- Keep changing platform media in an ignored, traceable cache instead of the Git
  repository or a browser-side CDN request.
- Distinguish completed, upcoming, tentative, and temporarily unavailable information.
- Give new Agents enough documentation and scripts to understand, verify, and extend the project.
- Keep every resource traceable to its original public page without exposing credentials or the maintainer's environment.

## Non-goals

- Do not become a full X/YouTube client, comment system, or account-login surface.
- Do not promise push-level real-time updates; freshness depends on how often the synchronization script runs.
- Do not bypass login walls, CAPTCHAs, access restrictions, or platform API permissions in the browser.
- Do not present fan-maintained material as an official announcement; the original platform page is always authoritative.

## Page Information Architecture

1. Sticky header: avatar, name, section navigation, snapshot time and health,
   language and theme controls.
2. Now: the gingham banner, identity (avatar, name, catchphrase, main links),
   a spotlight card for the next stream or event with a Japan-time countdown
   (falling back to the latest video), and a strip of counts plus a reload
   button.
3. Weekly schedule: a Monday-first week strip with per-day event dots, a week
   agenda that can be narrowed to one day, a shortcut to the nearest week with
   events, and the week's schedule image from X.
4. Posts on X: roughly three days of posts from both accounts with an account
   filter, image lightbox, and engagement counts. When the window is empty the
   most recent stored post is still shown.
5. YouTube: the manually selected Featured video with its focus copy, plus
   recent uploads and reservations.
6. Archive: a milestone timeline, the resource directory, and common hashtags,
   with a link to the full journey page.

A separate `/history` page lists Kano's public milestones from the first 2010
covers to joining Milpro SONA in 2026. It is static, curated content in
`src/history/milestones.js`: every entry keeps its original date precision
(year, month, day, or range), names its activity identity (singer, Hanayori
Joshiryo, Kano Mahoro, MKLNtic, Milpro SONA), and links the strongest public
source. Entries backed only by a third-party stream archive show no link.
Visitors can filter by identity. The page makes no API or external request.

The visual language borrows from the artist's public motifs: strawberry red
and cream gingham from the banner, the outfit's sky blue as a secondary hue,
rounded cards, and a rounded Japanese display face where the system has one.
Light and dark themes share the same tokens in `src/index.css`.

A separate, unlinked `/admin` route presents a maintainer console that opens on
one-click workflows: an operator chooses which modules to refresh (X, YouTube,
media cache, AI schedule scan), runs them immediately with live per-module
progress, or saves the selection with a server-side timer. It also offers
paginated schedule curation, detection rules with an ordered model list per
route, a central LLM provider page in the style of desktop LLM clients (API
host and key, fetched model list, capability tags), Featured-video selection,
and profile media. The public calendar labels
automatic extraction and manual confirmation so visitors can distinguish their
provenance. A `/mcp` integration endpoint
offers sanitized read tools without a key and narrowly scoped, bearer-protected
automation; confirmation and other human-only actions remain in `/admin`.

## Current Status and Known Limits

- The page and API run locally, and an initial dataset ships with the code.
- X profile structure, the public status endpoint, and YouTube page structure may change. The synchronization script records warnings and retains the old snapshot; a failed fetch must not be interpreted as no update.
- Activity times are displayed in the Japan time zone. Schedule-board candidates
  and heuristic single-message candidates can be classified and parsed through
  compatible LLM providers with cached image input. `uncertain` results are
  retained as extraction metadata without creating events; low-confidence or
  important entries can still be manually confirmed and locked.
- The deployment is a single local SQLite instance with one password-protected
  maintainer surface, in-memory sessions, and no multi-user permission model.
  MCP control uses a separate environment token and does not inherit the admin
  session.
- Media registration and bounded download are implemented. Retry scheduling,
  image dimension probing, and garbage collection remain follow-up work;
  unready media is intentionally shown as unavailable.

## Maintainer Decision Rule

When a new request conflicts with “easy to understand at a glance,” “traceable
data,” or “old data survives a failure,” preserve those three properties first.
Before adding a source, describe its fetch boundary, cached fields, failure
behavior, and public links.
