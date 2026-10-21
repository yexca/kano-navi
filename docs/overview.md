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

Every public page shares one shell: a sticky header with the site name, page
navigation (Now, Journey, About), a live Japan-time clock, and language and
theme controls, plus a common footer. On the board the header also carries a
snapshot health dot.

1. Now: a ticket for what airs next (artwork, Japan date and time, the
   visitor's local time, and an hours/minutes countdown, or an on-air state
   while a stream is live), falling back to the latest video. A schedule entry
   without artwork gets a compact date ticket instead, with its automatic or
   confirmed provenance and a link named for what it opens (the stream or its
   announcement), and the latest video follows it as a small card. Beside it, the
   profile card: banner, avatar, name, catchphrase, main links, counts, and
   the snapshot state with a reload button.
2. Weekly schedule: a Monday-first program guide with seven day columns on
   wide screens and a day list on narrow ones. Each entry shows its Japan
   time (or "time TBA" for date-only records), type, status, and automatic or
   confirmed provenance. Week navigation, a shortcut to the nearest week with
   events, more-event pagination, and the week's schedule image from X. A
   Week/Month switch widens it to a month grid (compact entries, dots on
   phones); picking a date opens that week.
3. YouTube: the manually selected Featured video with its focus copy, then
   reservations (soonest first) and recent uploads.
4. Posts on X: roughly three days of posts from both accounts in a masonry
   wall with an account filter, an image viewer, and engagement counts. When
   the window is empty the most recent stored post is still shown.
5. Links: the resource directory, a milestone summary linking to the full
   journey page, and common hashtags.

A separate `/history` page lists Kano's public milestones from the first 2010
covers to joining Milpro SONA in 2026. It is static, curated content in
`src/history/milestones.ts`: every entry keeps its original date precision
(year, month, day, or range), names its activity identity (singer, Hanayori
Joshiryo, Kano Mahoro, MKLNtic, Milpro SONA), and links the strongest public
source. Visitors can filter by identity and jump to a year from a year rail
(a sticky side rail on wide screens, a sticky strip on phones). A visual archive
adds 59 bundled local images with image-type filtering, full-image dialogs,
dimensions, date basis, recorded credits, and original-site links. Official
stream frames explicitly identify the third-party archive and frame timestamp.
Images load from local `/assets/history/` URLs; visits do not fetch external
sources. See [History and visual archive](product/history.md) for the packaging workflow.

A separate `/about` page introduces Kano Navi, credits yexca and the three
development models (GPT-5.6-Sol, Claude Opus 5.5, GPT-6.1-Sol), lists the actual
technology stack, and explains the fan-made status and GNU AGPL v3 code license.
It is static, supports English, Japanese, and Simplified Chinese with the shared
theme controls, and is linked from the board and history-page footers. It makes
no API or external-source requests. Third-party artwork retains its original
rights outside the code license.

The visual language borrows from the artist's public motifs: strawberry red
and cream gingham from the banner, the outfit's sky blue as a secondary hue,
rounded cards, and a rounded Japanese display face where the system has one.
Light and dark themes share the same tokens in `src/index.css`.

A separate, unlinked `/admin` route presents a maintainer console organized
around operator tasks. Its overview shows what waits for a decision, the active
run with per-step progress, source health and recent runs. A review queue
collects schedule images, suspected cancellations and failed post extractions;
the schedule page curates entries with durable locks; runs and workflows start
syncs or save step lists on a server-side timer; sources run recent or
historical fetch windows and YouTube push; content covers posts, the featured
video and profile media; and models & detection holds the extraction stages,
one ordered model priority queue and a provider catalog in the style of desktop
LLM clients (API host and key, fetched model list, capability tags). The public calendar labels
automatic extraction and manual confirmation so visitors can distinguish their
provenance. A `/mcp` integration endpoint
offers sanitized read tools without a key and narrowly scoped, bearer-protected
automation; confirmation and other human-only actions remain in `/admin`.

## Current Status and Known Limits

- The page and API run locally. Static profile, milestones, resource links, and
  history images ship with the code; source snapshots and LLM providers start empty.
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
