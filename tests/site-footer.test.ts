import assert from "node:assert/strict"
import path from "node:path"
import test from "node:test"
import { act, createElement } from "react"
import { createServer } from "vite"
import { click, render, type Rendered } from "./dom.ts"

test("the footer's Notes opens the site's notice in a dialog, and it links About and the top", async () => {
  const vite = await createServer({
    configFile: false,
    envDir: false,
    server: { middlewareMode: true, watch: null, ws: false },
    optimizeDeps: { noDiscovery: true, include: [] },
    resolve: { alias: { "@": path.resolve("src") } },
  })
  const previousAct = (globalThis as any).IS_REACT_ACT_ENVIRONMENT
  ;(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true
  let root: Rendered | undefined
  try {
    const [{ SiteFooter }, { AppSettingsProvider }] = await Promise.all([
      vite.ssrLoadModule("/src/site/site-shell.tsx"),
      vite.ssrLoadModule("/src/app-settings.tsx"),
    ])
    await act(async () => {
      root = render(
        createElement(AppSettingsProvider, null, createElement(SiteFooter)),
      )
    })
    const footer = root!.container.querySelector("footer.site-footer")!
    const page = root!.container.ownerDocument

    // The unaffiliated line stays in the footer; the notice waits closed.
    assert.match(footer.textContent || "", /Not affiliated/)
    assert.equal(page.querySelector("dialog.site-notice-dialog"), null)
    const notes = footer.querySelector("button.site-footer-notes")!
    assert.equal(notes.textContent, "Notes")
    assert.equal(notes.getAttribute("aria-haspopup"), "dialog")

    await click(notes)
    const dialog = page.querySelector("dialog.site-notice-dialog")!
    assert.ok(dialog)
    const title = page.getElementById(dialog.getAttribute("aria-labelledby")!)
    assert.equal(title?.textContent, "Notes")
    // One notice for every page: affiliation, freshness, rights and license.
    const text = dialog.textContent || ""
    assert.match(text, /no affiliation/)
    assert.match(text, /not a live feed/)
    assert.match(text, /rights holders/)
    assert.match(text, /AGPL-3\.0/)

    await click(dialog.querySelector("button.site-notice-close"))
    assert.equal(page.querySelector("dialog.site-notice-dialog"), null)

    // The header holds the pages; the footer links only About and the top.
    assert.deepEqual(
      [...footer.querySelectorAll(".site-footer-links a")].map((link) =>
        link.getAttribute("href"),
      ),
      ["/about", "#top"],
    )
  } finally {
    await act(async () => root?.unmount())
    ;(globalThis as any).IS_REACT_ACT_ENVIRONMENT = previousAct
    await vite.close()
  }
})
