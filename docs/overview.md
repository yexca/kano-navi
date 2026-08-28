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

1. Identity header: banner, avatar, name, bio, and the latest snapshot time.
2. Recent activity: roughly three days of X posts with all, announcement, and daily filters.
3. Schedule: a navigable calendar and future-first event list containing X schedules and YouTube reservations.
4. Latest focus: one highlight traceable to a video or official reference.
5. Channels and archive: recent YouTube uploads, a timeline, and a resource directory.

## Current Status and Known Limits

- The page and API run locally, and an initial dataset ships with the code.
- X profile structure, the public status endpoint, and YouTube page structure may change. The synchronization script records warnings and retains the old snapshot; a failed fetch must not be interpreted as no update.
- Activity times are displayed in the Japan time zone. Text inside source images still requires manual review.
- The deployment is currently a single local SQLite instance with no background queue or multi-user editing interface.
- Media registration exists, but the downloader, retry queue, image probing, and
  garbage collection are still follow-up work; unready media is intentionally
  shown as unavailable.

## Maintainer Decision Rule

When a new request conflicts with “easy to understand at a glance,” “traceable
data,” or “old data survives a failure,” preserve those three properties first.
Before adding a source, describe its fetch boundary, cached fields, failure
behavior, and public links.
