import type { Plugin, Rolldown } from "vite"

type Bundle = Record<
  string,
  | {
      type: "chunk"
      imports: readonly string[]
      facadeModuleId: string | null
      viteMetadata?: { importedCss: ReadonlySet<string> }
    }
  | { type: "asset" }
>

/**
 * The files a lazily loaded page needs beyond what the entry already loads:
 * the chunks of the page's modules, their static imports, and their CSS.
 */
export function pageFiles(
  bundle: Bundle,
  entry: string,
  modules: readonly string[],
): { scripts: string[]; styles: string[] } {
  const closure = (starts: string[]) => {
    const files = new Set<string>()
    const visit = (name: string) => {
      const chunk = bundle[name]
      if (files.has(name) || chunk?.type !== "chunk") return
      files.add(name)
      for (const imported of chunk.imports) visit(imported)
    }
    for (const start of starts) visit(start)
    return files
  }
  const page = modules.map((module) => {
    const found = Object.entries(bundle).find(
      ([, chunk]) =>
        chunk.type === "chunk" &&
        chunk.facadeModuleId?.replaceAll("\\", "/").endsWith(`/${module}`),
    )
    if (!found) throw new Error(`No build chunk for ${module}`)
    return found[0]
  })
  const loaded = closure([entry])
  const css = (files: Set<string>) =>
    new Set(
      [...files].flatMap((name) => {
        const chunk = bundle[name]
        return chunk?.type === "chunk"
          ? [...(chunk.viteMetadata?.importedCss ?? [])]
          : []
      }),
    )
  const loadedCss = css(loaded)
  const scripts = [...closure(page)].filter((name) => !loaded.has(name))
  const styles = [...css(new Set(scripts))].filter(
    (name) => !loadedCss.has(name),
  )
  return { scripts, styles }
}

/**
 * The server answers every page with the same index.html, so a page's code
 * cannot be listed there for all visitors. Instead a small script at the top
 * of index.html asks for the page's chunks before the entry runs, but only
 * when the address is that page. Keep the path rule in step with pagePath in
 * src/lib/page-path.ts.
 */
export function pagePreload(pages: Record<string, readonly string[]>): Plugin {
  let base = "/"
  return {
    name: "kano-page-preload",
    apply: "build",
    configResolved(config) {
      base = config.base
    },
    transformIndexHtml: {
      order: "post",
      handler(html, context) {
        if (!context.bundle || !context.chunk) return html
        const bundle = context.bundle as Rolldown.OutputBundle & Bundle
        const files: Record<string, { scripts: string[]; styles: string[] }> =
          {}
        for (const [path, modules] of Object.entries(pages)) {
          const found = pageFiles(bundle, context.chunk.fileName, modules)
          files[path] = {
            scripts: found.scripts.map((name) => base + name),
            styles: found.styles.map((name) => base + name),
          }
        }
        // A classic script after a stylesheet waits for it, so this one goes
        // before the entry's script and stylesheets.
        const script = `<script>
      (function () {
        var page = location.pathname.replace(/\\/+$/, "") || "/"
        var files = ${JSON.stringify(files)}[page === "/index.html" ? "/" : page]
        function add(rel, href, as) {
          var link = document.createElement("link")
          link.rel = rel
          link.href = href
          link.crossOrigin = ""
          if (as) link.as = as
          document.head.appendChild(link)
        }
        if (!files) return
        files.scripts.forEach(function (href) { add("modulepreload", href) })
        files.styles.forEach(function (href) { add("preload", href, "style") })
      })()
    </script>
    `
        const at = html.indexOf('<script type="module"')
        if (at < 0) throw new Error("index.html has no module script")
        return html.slice(0, at) + script + html.slice(at)
      },
    },
  }
}
