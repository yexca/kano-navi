import assert from "node:assert/strict"
import test from "node:test"
import vm from "node:vm"
import { createTranslator } from "../src/i18n.ts"
import { dashboardMessages } from "../src/dashboard/dashboard-i18n.ts"
import { pageFiles, pagePreload } from "../scripts/vite-page-preload.ts"

const chunk = (
  imports: string[],
  facadeModuleId: string | null = null,
  css: string[] = [],
) => ({
  type: "chunk" as const,
  imports,
  facadeModuleId,
  viteMetadata: { importedCss: new Set(css) },
})

// The entry shares one chunk with the board; the board also shares the site
// shell with other pages.
const bundle = {
  "assets/index.js": chunk(["assets/react.js"], "/app/index.html", [
    "assets/index.css",
  ]),
  "assets/react.js": chunk([]),
  "assets/board.js": chunk(
    ["assets/react.js", "assets/shell.js"],
    "C:\\app\\src\\dashboard\\dashboard-app.tsx",
    ["assets/board.css"],
  ),
  "assets/shell.js": chunk(["assets/react.js"], null, ["assets/shell.css"]),
  "assets/board-copy.js": chunk([], "/app/src/dashboard/dashboard-i18n.ts"),
  "assets/history.js": chunk(
    ["assets/shell.js"],
    "/app/src/history/history-app.tsx",
  ),
  "assets/logo.svg": { type: "asset" as const },
}
const board = [
  "src/dashboard/dashboard-app.tsx",
  "src/dashboard/dashboard-i18n.ts",
]

test("a page preloads only what the entry does not already load", () => {
  assert.deepEqual(pageFiles(bundle, "assets/index.js", board), {
    scripts: ["assets/board.js", "assets/shell.js", "assets/board-copy.js"],
    styles: ["assets/board.css", "assets/shell.css"],
  })
  assert.throws(
    () => pageFiles(bundle, "assets/index.js", ["src/missing.tsx"]),
    /src\/missing\.tsx/,
  )
})

function preloadsAt(html: string, pathname: string) {
  const links: { rel: string; href: string; as?: string }[] = []
  const document = {
    createElement: () => ({}),
    head: { appendChild: (link: (typeof links)[number]) => links.push(link) },
  }
  const script = /<script>([\s\S]*?)<\/script>/.exec(html)?.[1] ?? ""
  vm.runInNewContext(script, { document, location: { pathname } })
  return links.map(({ rel, href, as }) =>
    [rel, as, href].filter(Boolean).join(" "),
  )
}

test("index.html asks for the board's files only on the board", () => {
  const plugin = pagePreload({ "/": board })
  ;(plugin.configResolved as (config: { base: string }) => void)({
    base: "/",
  })
  const { handler } = plugin.transformIndexHtml as {
    handler: (html: string, context: object) => string
  }
  const html = handler(
    '<head><script type="module" src="/assets/index.js"></script></head>',
    { bundle, chunk: { fileName: "assets/index.js" } },
  )
  assert.ok(html.indexOf("<script>") < html.indexOf('<script type="module"'))
  const boardFiles = [
    "modulepreload /assets/board.js",
    "modulepreload /assets/shell.js",
    "modulepreload /assets/board-copy.js",
    "preload style /assets/board.css",
    "preload style /assets/shell.css",
  ]
  for (const path of ["/", "/index.html", "//"])
    assert.deepEqual(preloadsAt(html, path), boardFiles, path)
  for (const path of ["/history", "/lineblog/", "/about", "/admin", "/x"])
    assert.deepEqual(preloadsAt(html, path), [], path)
})

test("the board's copy lives with the board, with English for every key", () => {
  const base = createTranslator("en")
  const page = createTranslator("en", dashboardMessages)
  for (const key of Object.keys(dashboardMessages.en)) {
    assert.equal(base(key), key, `${key} is also in the base dictionary`)
    assert.notEqual(page(key), key, key)
  }
  for (const locale of ["ja", "zh-CN"] as const)
    for (const key of Object.keys(dashboardMessages[locale]))
      assert.ok(Object.hasOwn(dashboardMessages.en, key), `${locale} ${key}`)
})
