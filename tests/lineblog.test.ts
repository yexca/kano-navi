import assert from "node:assert/strict"
import fs from "node:fs/promises"
import os from "node:os"
import path from "node:path"
import test, { type TestContext } from "node:test"
import zlib from "node:zlib"

import {
  cleanUrl,
  packageLineblog,
  postIndex,
  referencedMedia,
  sha256ForContent,
  videoSources,
  withRecoveredImages,
} from "../scripts/package-lineblog.ts"
import {
  packageLineblogComments,
  parseCapture,
  pickComments,
} from "../scripts/package-lineblog-comments.ts"
import {
  addPostFile,
  isEmoji,
  lineblogView,
  loadedPost,
  media,
  neighbours,
  pagerPages,
  postDate,
  posts,
} from "../src/lineblog/content.ts"
import {
  chosenDesign,
  designFor,
  designMedia,
  designOptions,
  designs,
  homeIcon,
} from "../src/lineblog/designs.ts"
import { recoveredImages, tweetPictures } from "../src/lineblog/recovered.ts"
import catalogFile from "../src/lineblog/media-catalog.json" with { type: "json" }
import type {
  LineblogComment,
  LineblogMedia,
  LineblogPost,
  LineblogPostFile,
  LineblogSummary,
} from "../src/lineblog/types.ts"

const catalog = new Map(
  (catalogFile as LineblogMedia[]).map((item) => [item.name, item]),
)
const postsDir = new URL("../src/lineblog/posts/", import.meta.url)
const postFiles = await Promise.all(
  posts.map(
    async (summary) =>
      JSON.parse(
        await fs.readFile(new URL(`${summary.id}.json`, postsDir), "utf8"),
      ) as LineblogPostFile,
  ),
)
const fullPosts = postFiles.map((file) => file.post)

const PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+j1WQAAAAASUVORK5CYII=",
  "base64",
)

/** A one-member ustar archive, the format of the archive's split videos. */
function tarball(name: string, content: Buffer) {
  const header = Buffer.alloc(512)
  header.write(name, 0, "utf8")
  header.write("0000644\0", 100, "ascii")
  header.write("0000000\0", 108, "ascii")
  header.write("0000000\0", 116, "ascii")
  header.write(
    `${content.length.toString(8).padStart(11, "0")}\0`,
    124,
    "ascii",
  )
  header.write("00000000000\0", 136, "ascii")
  header.write("        ", 148, "ascii")
  header.write("0", 156, "ascii")
  header.write("ustar\0" + "00", 257, "ascii")
  const sum = header.reduce((total, byte) => total + byte, 0)
  header.write(`${sum.toString(8).padStart(6, "0")}\0 `, 148, "ascii")
  const padding = Buffer.alloc((512 - (content.length % 512)) % 512)
  return zlib.gzipSync(
    Buffer.concat([header, content, padding, Buffer.alloc(1024)]),
  )
}

async function fixture(t: TestContext) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "kano-lineblog-test-"))
  t.after(async () => {
    assert.equal(path.dirname(root), path.resolve(os.tmpdir()))
    assert.ok(path.basename(root).startsWith("kano-lineblog-test-"))
    await fs.rm(root, { recursive: true, force: true })
  })
  const archive = path.join(root, "archive")
  for (const folder of [
    "BLOG",
    "BLOG_zh-cn",
    "BLOG_en",
    "media",
    "media-parts",
  ])
    await fs.mkdir(path.join(archive, folder), { recursive: true })

  const image = Buffer.concat([PNG, Buffer.from("synthetic image")])
  const imageName = `${sha256ForContent(image).slice(0, 12)}.png`
  await fs.writeFile(path.join(archive, "media", imageName), image)
  // Header art no post references.
  const art = Buffer.concat([PNG, Buffer.from("synthetic header")])
  const artName = `${sha256ForContent(art).slice(0, 12)}.png`
  await fs.writeFile(path.join(archive, "media", artName), art)

  const watch = "https://www.youtube.com/watch?v=AAAAAAAAAAA"
  const videoName = `${sha256ForContent(Buffer.from(watch)).slice(0, 12)}.mp4`
  const video = Buffer.concat([
    Buffer.from("\0\0\0\x18ftypmp42"),
    Buffer.from("synthetic video ".repeat(64)),
  ])
  const packed = tarball(videoName, video)
  const half = Math.floor(packed.length / 2)
  await fs.writeFile(
    path.join(archive, "media-parts", `${videoName}.tar.gz.part-001`),
    packed.subarray(0, half),
  )
  await fs.writeFile(
    path.join(archive, "media-parts", `${videoName}.tar.gz.part-002`),
    packed.subarray(half),
  )

  const header = (title: string) => [
    title,
    "",
    "- Published: 2016-04-15T21:57:00+0900",
    "- Original URL: http://lineblog.me/kano/archives/100.html",
    "- Wayback snapshot: 20160418055640",
    "- Source HTML: ../data/pages/synthetic.html",
    "",
  ]
  const body = [
    "こんばんは、鹿乃です。",
    "",
    "一行目",
    "http://example.invalid/page",
    "",
    `![image](../media/${imageName})`,
    "",
    "[Image source](https://images.example.invalid/a.png)",
    "",
    '<iframe width="480" height="360" src="https://www.youtube.com/embed/AAAAAAAAAAA?rel=0" title="x" allowfullscreen></iframe>',
    "",
    `<video controls preload="metadata" src="../media/${videoName}"></video>`,
    "",
    "> [!video] YouTube",
    `> [![YouTube 视频封面](../media/${imageName})](${watch})`,
    ">",
    `> [播放原视频](${watch}) · [查看网页存档](https://web.archive.org/web/2016/${watch})`,
    "",
    "[CD](http://www.amazon.co.jp/x/dp/B000000000%3FSubscriptionId%3Dsynthetic)",
  ]
  const translated = [
    "こんばんは、鹿乃です。",
    "",
    "晚上好，我是鹿乃。",
    "",
    "一行目",
    "第一行",
    "http://example.invalid/page",
    ...body.slice(4),
  ]
  await fs.writeFile(
    path.join(archive, "BLOG", "20160415_0123456789ABCDEF_v01.md"),
    [...header("# 動画"), ...body].join("\r\n"),
  )
  await fs.writeFile(
    path.join(archive, "BLOG_zh-cn", "20160415_0123456789ABCDEF_v01.md"),
    [...header("# 動画\n# 视频"), ...translated].join("\n"),
  )
  // The English file translates only the greeting.
  await fs.writeFile(
    path.join(archive, "BLOG_en", "20160415_0123456789ABCDEF_v01.md"),
    [
      ...header("# 動画\n# Video"),
      "こんばんは、鹿乃です。",
      "",
      "Good evening, this is Kano.",
      "",
      ...body.slice(2),
    ].join("\n"),
  )
  return { root, archive, image, imageName, art, artName, video, videoName }
}

test("LINE BLOG packaging parses posts, aligns translations and restores split videos", async (t) => {
  const { root, archive, image, imageName, art, artName, video, videoName } =
    await fixture(t)
  const contentDir = path.join(root, "content")
  const mediaDir = path.join(root, "data", "lineblog")
  const result = await packageLineblog(archive, {
    contentDir,
    mediaDir,
    designArt: [artName, imageName],
  })
  assert.deepEqual(result, {
    posts: 1,
    media: 3,
    bytes: image.length + art.length + video.length,
  })

  const index = JSON.parse(
    await fs.readFile(path.join(contentDir, "post-index.json"), "utf8"),
  ) as LineblogSummary[]
  assert.deepEqual(index, [
    {
      id: "100",
      published: "2016-04-15T12:57:00.000Z",
      title: "動画",
      titleZh: "视频",
      titleEn: "Video",
      thumbnail: imageName,
    },
  ])
  assert.deepEqual(await fs.readdir(path.join(contentDir, "posts")), [
    "100.json",
  ])
  const { post, media: postMedia } = JSON.parse(
    await fs.readFile(path.join(contentDir, "posts", "100.json"), "utf8"),
  ) as LineblogPostFile
  // A post file carries its own media, not the design art.
  assert.deepEqual(
    postMedia.map((item) => item.name),
    [imageName, videoName].sort(),
  )
  assert.equal(post.id, "100")
  assert.equal(post.published, "2016-04-15T12:57:00.000Z")
  assert.equal(post.titleZh, "视频")
  assert.equal(post.titleEn, "Video")
  assert.equal(post.url, "https://lineblog.me/kano/archives/100.html")
  const [greeting, lines, picture, youtube, product] = post.blocks
  assert.deepEqual(greeting, {
    type: "p",
    content: [{ type: "text", text: "こんばんは、鹿乃です。" }],
    zh: [[{ type: "text", text: "晚上好，我是鹿乃。" }]],
    en: [[{ type: "text", text: "Good evening, this is Kano." }]],
  })
  // A translation line inside a paragraph follows that paragraph; each
  // language aligns on its own.
  assert.equal(lines.type === "p" && lines.content[1].type, "link")
  assert.deepEqual(lines.type === "p" && lines.zh, [
    [{ type: "text", text: "第一行" }],
  ])
  assert.equal(lines.type === "p" && lines.en, undefined)
  assert.deepEqual(picture, {
    type: "p",
    content: [
      {
        type: "image",
        alt: "image",
        media: imageName,
        source: "https://images.example.invalid/a.png",
      },
    ],
  })
  assert.deepEqual(youtube, {
    type: "youtube",
    videoId: "AAAAAAAAAAA",
    watch: "https://www.youtube.com/watch?v=AAAAAAAAAAA",
    video: videoName,
    cover: imageName,
    archive:
      "https://web.archive.org/web/2016/https://www.youtube.com/watch?v=AAAAAAAAAAA",
  })
  assert.equal(
    product.type === "p" &&
      product.content[0].type === "link" &&
      product.content[0].href,
    "http://www.amazon.co.jp/dp/B000000000",
  )

  assert.deepEqual(await fs.readFile(path.join(mediaDir, imageName)), image)
  assert.deepEqual(await fs.readFile(path.join(mediaDir, videoName)), video)
  assert.deepEqual(await fs.readFile(path.join(mediaDir, artName)), art)
  assert.deepEqual(
    (await fs.readdir(mediaDir)).sort(),
    [imageName, artName, videoName].sort(),
  )
  const written = JSON.parse(
    await fs.readFile(path.join(contentDir, "media-catalog.json"), "utf8"),
  ) as LineblogMedia[]
  const entry = {
    name: imageName,
    mimeType: "image/png",
    byteSize: image.length,
    sha256: sha256ForContent(image),
    width: 1,
    height: 1,
  }
  assert.deepEqual(
    written.find((item) => item.name === imageName),
    entry,
  )
  assert.deepEqual(
    postMedia.find((item) => item.name === imageName),
    entry,
  )
  assert.deepEqual(
    written.map((item) => item.name),
    [imageName, artName, videoName].sort(),
  )
})

test("LINE BLOG packaging removes the files of posts it no longer holds", async (t) => {
  const { root, archive, artName } = await fixture(t)
  const contentDir = path.join(root, "content")
  await fs.mkdir(path.join(contentDir, "posts"), { recursive: true })
  await fs.writeFile(path.join(contentDir, "posts", "99.json"), "{}\n")
  await fs.mkdir(path.join(contentDir, "comments"), { recursive: true })
  await fs.writeFile(path.join(contentDir, "comments", "100.json"), "[]\n")
  await packageLineblog(archive, {
    contentDir,
    mediaDir: path.join(root, "data", "lineblog"),
    designArt: [artName],
  })
  assert.deepEqual(await fs.readdir(path.join(contentDir, "posts")), [
    "100.json",
  ])
  // Comments come from page captures, which packaging leaves alone.
  assert.equal(
    await fs.readFile(path.join(contentDir, "comments", "100.json"), "utf8"),
    "[]\n",
  )
})

test("LINE BLOG packaging writes nothing when media do not match their names", async (t) => {
  const { root, archive, imageName } = await fixture(t)
  await fs.writeFile(
    path.join(archive, "media", imageName),
    Buffer.concat([PNG, Buffer.from("altered")]),
  )
  const contentDir = path.join(root, "content")
  const mediaDir = path.join(root, "data", "lineblog")
  await assert.rejects(
    packageLineblog(archive, { contentDir, mediaDir, designArt: [] }),
    /identity mismatch/u,
  )
  assert.deepEqual(await fs.readdir(mediaDir), [])
  await assert.rejects(fs.stat(contentDir))
})

test("archive links lose affiliate and API query parts", () => {
  assert.equal(
    cleanUrl(
      "http://www.amazon.co.jp/name/dp/B01CSANBYQ%3FSubscriptionId%3Dsynthetic%26tag%3Dx",
    ),
    "http://www.amazon.co.jp/dp/B01CSANBYQ",
  )
  assert.equal(
    cleanUrl("https://twitter.com/x/status/1?ref_src=twsrc%5Etfw"),
    "https://twitter.com/x/status/1",
  )
})

test("the post index and post files agree with each other and the catalog", async () => {
  assert.deepEqual(
    (await fs.readdir(postsDir)).sort(),
    posts.map((post) => `${post.id}.json`).sort(),
  )
  assert.deepEqual(postIndex(fullPosts, [...catalog.values()]), posts)
  for (const file of postFiles)
    assert.deepEqual(
      file.media,
      referencedMedia([file.post]).map((name) => catalog.get(name)),
      file.post.id,
    )
})

test("a loaded post file brings its media entries", () => {
  const file = postFiles.find((item) =>
    item.media.some((entry) => entry.width !== undefined && entry.width <= 80),
  )!
  const emoji = file.media.find((entry) => entry.width! <= 80)!
  assert.equal(loadedPost(file.post.id), undefined)
  assert.equal(isEmoji(emoji.name), false)
  addPostFile(file)
  assert.equal(loadedPost(file.post.id), file.post)
  for (const entry of file.media) assert.equal(media.get(entry.name), entry)
  assert.equal(isEmoji(emoji.name), true)
  const [newest, second] = posts
  assert.deepEqual(neighbours(second.id), {
    newer: newest,
    older: posts[2],
  })
})

test("the packaged archive references only catalogued media with verified names", () => {
  assert.equal(new Set(posts.map((post) => post.id)).size, posts.length)
  for (let index = 1; index < posts.length; index += 1)
    assert.ok(posts[index - 1].published >= posts[index].published)
  const sources = videoSources(fullPosts)
  for (const name of [...referencedMedia(fullPosts), ...designMedia()]) {
    const item = catalog.get(name)
    assert.ok(item, name)
    const stem = name.slice(0, 12)
    if (item.mimeType === "video/mp4") {
      assert.ok(
        sha256ForContent(Buffer.from(sources.get(name) ?? "")).startsWith(stem),
        name,
      )
    } else {
      assert.ok(item.sha256.startsWith(stem), name)
      assert.ok(item.width && item.height, name)
    }
  }
  const text = JSON.stringify(fullPosts)
  // A link never holds another link; nested anchors are invalid HTML.
  assert.doesNotMatch(
    text,
    /"type":"link","href":"[^"]*","content":\[[^\]]*"type":"link"/u,
  )
  assert.doesNotMatch(text, /SubscriptionId|\.\.\/media\/|data\/pages/u)
  // Hard line breaks lose their backslash.
  assert.doesNotMatch(text, /\\\\\\n/u)
})

test("the page query selects an article, a month or a page", () => {
  const [newest] = posts
  assert.equal(lineblogView("").shown[0], newest)
  assert.equal(lineblogView("?page=2").page, 2)
  assert.equal(lineblogView("?page=999").page, 1)
  assert.equal(lineblogView("?page=x").page, 1)
  const article = lineblogView(`?article=${newest.id}`)
  assert.equal(article.article, newest)
  assert.deepEqual(lineblogView("?article=0").shown, [])
  assert.equal(lineblogView("?article=0").missingArticle, true)
  const month = lineblogView("?month=2015-11")
  assert.ok(month.shown.length > 0)
  assert.ok(
    month.shown.every((post) => postDate(post.published).startsWith("2015/11")),
  )
  assert.deepEqual(pagerPages(1, 33), [1, 2, 3, 4, 5])
  assert.deepEqual(pagerPages(33, 33), [29, 30, 31, 32, 33])
  assert.deepEqual(pagerPages(2, 3), [1, 2, 3])
})

test("post times show in Japan time", () => {
  assert.equal(postDate("2016-07-21T13:40:00.000Z"), "2016/07/21 22:40")
})

test("the blog's design art is catalogued archive media with its provenance", () => {
  const names = designMedia()
  assert.equal(names.length, 10)
  assert.equal(catalog.get(homeIcon.media)?.mimeType, "image/png")
  assert.match(homeIcon.capture, /^\d{14}$/u)
  for (const design of designs)
    for (const art of [design.header, design.profile]) {
      assert.equal(catalog.get(art.media)?.mimeType, "image/jpeg", art.media)
      assert.match(art.capture, /^\d{14}$/u, art.media)
      assert.match(art.source, /^https?:\/\/[a-z.-]+\//u, art.media)
    }
  for (const design of designs) {
    assert.equal(catalog.get(design.header.media)?.width, 2600)
    assert.equal(catalog.get(design.header.media)?.height, 390)
  }
})

test("a page takes the blog's design of its newest article", () => {
  for (let index = 1; index < designs.length; index += 1)
    assert.ok(designs[index - 1].since < designs[index].since)
  assert.equal(designFor("2015-11-27").header.media, "941e5735900c.jpg")
  assert.equal(designFor("2016-03-18").header.media, "0da83289f328.jpg")
  assert.equal(designFor("2017-04-14").header.media, "0da83289f328.jpg")
  assert.equal(designFor("2017-04-14").profile.media, "5b5533c91f9f.jpg")
  assert.equal(designFor("2017-04-29").accent, "#826856")
  assert.equal(designFor("2018-07-21").accent, "#376c9c")
  assert.equal(
    designFor("2018-05-17").officialSite,
    "https://whv-amusic.com/kano/",
  )
  assert.equal(
    designFor("2018-05-18").officialSite,
    "https://kano-official.amebaownd.com/",
  )
  assert.equal(designFor("2018-12-03").header.media, "03adb75fc762.jpg")
  assert.equal(designFor("2019-03-20").profile.media, "e4293469ed7c.jpg")
  assert.equal(designFor(undefined), designs[designs.length - 1])
})

test("a reader picks among the blog's distinct looks or follows the articles", () => {
  // The 2017-03-13 design changed only the profile text, so it joins the
  // 2016 look, which then applies its newer text.
  assert.deepEqual(
    designOptions.map((option) => [option.id, option.until]),
    [
      ["2015-01-01", "2016-03-18"],
      ["2016-03-18", "2017-04-14"],
      ["2017-04-14", "2017-04-29"],
      ["2017-04-29", "2018-05-18"],
      ["2018-05-18", "2018-07-21"],
      ["2018-07-21", "2018-12-04"],
      ["2018-12-04", undefined],
    ],
  )
  assert.equal(chosenDesign("2016-03-18")?.since, "2017-03-13")
  assert.equal(chosenDesign("2018-12-04"), designs[designs.length - 1])
  assert.equal(chosenDesign(null), null)
  assert.equal(chosenDesign("2017-03-13"), null)
})

test("recovered pictures replace remote addresses and follow embedded tweets", () => {
  const address = "http://images.example.invalid/a.jpg"
  const [post] = withRecoveredImages(
    [
      {
        id: "1",
        published: "2016-01-01T00:00:00.000Z",
        title: "x",
        url: "https://lineblog.example.invalid/1",
        archive: "20160101000000",
        blocks: [
          {
            type: "p",
            content: [
              {
                type: "link",
                href: "https://shop.example.invalid/",
                content: [{ type: "image", alt: "image", remote: address }],
              },
              {
                type: "image",
                alt: "image",
                remote: "http://images.example.invalid/b.jpg",
              },
            ],
          },
          {
            type: "p",
            content: [
              { type: "text", text: "tweet " },
              {
                type: "link",
                href: "https://t.example.invalid/x",
                content: [{ type: "text", text: "pic" }],
              },
            ],
          },
        ],
      },
    ],
    [
      {
        address,
        files: [
          { media: "000000000001.jpg", source: "https://a.example.invalid/1" },
          { media: "000000000002.jpg", source: "https://a.example.invalid/2" },
        ],
        standIn: true,
      },
    ],
    [
      {
        link: "https://t.example.invalid/x",
        files: [
          { media: "000000000003.jpg", source: "https://p.example.invalid/3" },
        ],
      },
    ],
  )
  const [shop, tweet, pictures] = post.blocks
  assert.deepEqual(shop.type === "p" && shop.content, [
    {
      type: "link",
      href: "https://shop.example.invalid/",
      content: [1, 2].map((n) => ({
        type: "image",
        alt: "image",
        media: `00000000000${n}.jpg`,
        source: `https://a.example.invalid/${n}`,
        recovery: "stand-in",
      })),
    },
    {
      type: "image",
      alt: "image",
      remote: "http://images.example.invalid/b.jpg",
    },
  ])
  assert.equal(tweet.type, "p")
  assert.deepEqual(pictures, {
    type: "p",
    content: [
      {
        type: "image",
        alt: "image",
        media: "000000000003.jpg",
        source: "https://p.example.invalid/3",
        recovery: "tweet",
      },
    ],
  })
  assert.equal(post.blocks.length, 3)
})

test("every recovered picture is packaged with its source", () => {
  const images = fullPosts.flatMap((post) =>
    post.blocks.flatMap((block) =>
      block.type === "p"
        ? block.content.flatMap((inline) =>
            inline.type === "link" ? inline.content : [inline],
          )
        : [],
    ),
  )
  const files = [
    ...recoveredImages.flatMap((item) => item.files),
    ...tweetPictures.flatMap((item) => item.files),
  ]
  for (const file of files) {
    assert.match(file.media, /^[0-9a-f]{12}\.jpg$/u)
    if (file.capture) assert.match(file.capture, /^\d{14}$/u)
    assert.equal(catalog.get(file.media)?.mimeType, "image/jpeg", file.media)
    const image = images.find(
      (inline) => inline.type === "image" && inline.media === file.media,
    )
    assert.equal(image?.type === "image" && image.source, file.source)
  }
  for (const item of recoveredImages)
    assert.ok(
      !JSON.stringify(fullPosts).includes(`"remote":"${item.address}"`),
      item.address,
    )
})

// ---------------------------------------------------------------------------
// Archived comments
// ---------------------------------------------------------------------------

/** A synthetic article page in the blog's markup, with the given comments. */
function capturePage(article: string, comments: string[]) {
  return [
    "<!DOCTYPE html><html><head>",
    `<link rel="canonical" href="https://lineblog.me/kano/archives/${article}.html" />`,
    "</head><body>",
    '<ul class="article-pager"><li class="prev"><a rel="prev" href="https://lineblog.me/kano/archives/1.html" title="x">前の記事</a></li></ul>',
    '<div class="article-option" id="comments-list"><h3>コメント一覧</h3>',
    '<ol id="ld_blog_article_comment_entries">',
    ...comments,
    "</ol>",
    '<script type="text/html" id="ld_blog_article_comment_entries_tmpl">',
    '<li class="comment-set <%==entry.odd_even%>"><ul class="comment-info">',
    '<li class="comment-author"><%==entry.num%>.<a href="<%=entry.url%>" class="comment-url">x</a></li>',
    "</ul></li></script></div></body></html>",
  ].join("\n")
}

function commentSet(
  parity: string,
  author: string,
  date: string,
  body: string,
) {
  return [
    `\t\t\t\t<li class="comment-set ${parity}">`,
    '\t\t\t\t\t<ul class="comment-info">',
    `\t\t\t\t\t\t<li class="comment-author">${author}</li>`,
    `\t\t\t\t\t\t<li class="comment-date">${date}</li>`,
    `\t\t\t\t\t\t<li class="comment-body">${body}                        </li>`,
    "\t\t\t\t\t</ul>",
    "\t\t\t\t</li>",
  ].join("\r\n")
}

const syntheticComments = [
  commentSet(
    "odd",
    '6.<a href="https://commenter.example.invalid/blog/" class="comment-url" rel="nofollow">A&amp;B</a>',
    "2018/05/10 17:46",
    "一行目<br>\r\n<br>\r\n&lt;三行目&gt;  です",
  ),
  commentSet(
    "even",
    "4.\r\n\t\t\t\t\t\t\tNo Name",
    "2016/09/04 21:21",
    '\t\t\t\t\t\t\t<img src="http://parts.blog.livedoor.jp/img/star/4.gif" width="70" height="13" alt="4" class="commentEvaluation" />\t\t\t\t\t\t\t<br>\r\nおつかれさま<img src="http://parts.blog.livedoor.jp/img/emoji/3/ic_sun.png" width="15" height="15" />',
  ),
  commentSet(
    "odd",
    "5.名前",
    "2016/09/04 20:00",
    '<img src="https://parts.lineblog.me/img/face/2.gif" width="15" height="15" alt="2" class="commentEvaluation" />',
  ),
]

test("an archived article page yields its comments without commenter links", () => {
  const capture = parseCapture(capturePage("100", syntheticComments))
  assert.deepEqual(capture, {
    article: "100",
    comments: [
      {
        number: 6,
        author: "A&B",
        posted: "2018-05-10T08:46:00.000Z",
        body: "一行目\n\n<三行目> です",
      },
      {
        number: 5,
        author: "名前",
        posted: "2016-09-04T11:00:00.000Z",
        body: "",
        rating: { icon: "face", value: 2 },
      },
      {
        number: 4,
        author: "No Name",
        posted: "2016-09-04T12:21:00.000Z",
        body: "おつかれさま☀️",
        rating: { icon: "star", value: 4 },
      },
    ],
  })
  assert.ok(!JSON.stringify(capture).includes("example"))
  assert.equal(parseCapture("<html><body>月別アーカイブ</body></html>"), null)
  // A page whose comment list was empty, or not printed, has no comments.
  assert.deepEqual(parseCapture(capturePage("101", [])), {
    article: "101",
    comments: [],
  })
  assert.throws(
    () =>
      parseCapture(
        capturePage("102", [
          commentSet("odd", "1.x", "2018/05/10 17:46", "<b>bold</b>"),
        ]),
      ),
    /unexpected markup/u,
  )
})

test("each article keeps the capture listing the most comments", async (t) => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "kano-lineblog-test-"))
  t.after(async () => {
    assert.equal(path.dirname(root), path.resolve(os.tmpdir()))
    assert.ok(path.basename(root).startsWith("kano-lineblog-test-"))
    await fs.rm(root, { recursive: true, force: true })
  })
  const pages = path.join(root, "pages")
  const contentDir = path.join(root, "content")
  await fs.mkdir(pages)
  await fs.mkdir(path.join(contentDir, "comments"), { recursive: true })
  await fs.writeFile(
    path.join(contentDir, "post-index.json"),
    JSON.stringify([{ id: "100" }, { id: "200" }, { id: "300" }]),
  )
  await fs.writeFile(path.join(contentDir, "comments", "300.json"), "[]\n")
  // A file's name can claim an earlier capture than its content.
  await fs.writeFile(
    path.join(pages, "post_100_20160101000000.html"),
    capturePage("100", syntheticComments),
  )
  await fs.writeFile(
    path.join(pages, "post_100_20180101000000.html"),
    capturePage("100", syntheticComments.slice(0, 1)),
  )
  await fs.writeFile(
    path.join(pages, "post_200_20160101000000.html"),
    capturePage("200", []),
  )
  // Comments on an article the archive does not hold are not written.
  await fs.writeFile(
    path.join(pages, "post_999_20160101000000.html"),
    capturePage("999", syntheticComments),
  )
  await fs.writeFile(path.join(pages, "index_20160101000000.html"), "<html>")

  assert.deepEqual(await packageLineblogComments(pages, { contentDir }), {
    posts: 1,
    comments: 3,
  })
  assert.deepEqual(await fs.readdir(path.join(contentDir, "comments")), [
    "100.json",
  ])
  const written = JSON.parse(
    await fs.readFile(path.join(contentDir, "comments", "100.json"), "utf8"),
  ) as LineblogComment[]
  assert.deepEqual(
    written.map((comment) => comment.number),
    [6, 5, 4],
  )
  assert.deepEqual(
    pickComments([
      { article: "1", comments: written.slice(0, 1) },
      { article: "1", comments: written },
      { article: "2", comments: [] },
    ]),
    new Map([["1", written]]),
  )
})

test("the archived comments belong to packaged posts and keep their shape", async () => {
  const commentsDir = new URL("../src/lineblog/comments/", import.meta.url)
  const ids = new Set(posts.map((post) => post.id))
  const names = await fs.readdir(commentsDir)
  assert.ok(names.length > 0)
  for (const name of names) {
    assert.match(name, /^\d+\.json$/u)
    assert.ok(ids.has(name.slice(0, -5)), name)
    const text = await fs.readFile(new URL(name, commentsDir), "utf8")
    // Commenters' blog addresses are never kept.
    assert.doesNotMatch(text, /https?:\/\/|lineblog\.me/u, name)
    const comments = JSON.parse(text) as LineblogComment[]
    assert.ok(comments.length > 0, name)
    comments.forEach((comment, index) => {
      assert.deepEqual(
        Object.keys(comment).filter(
          (key) =>
            !["number", "author", "posted", "body", "rating"].includes(key),
        ),
        [],
      )
      assert.ok(Number.isInteger(comment.number) && comment.number > 0)
      if (index) assert.ok(comment.number < comments[index - 1].number, name)
      assert.ok(comment.author && comment.author === comment.author.trim())
      assert.equal(new Date(comment.posted).toISOString(), comment.posted)
      assert.match(comment.posted, /:00\.000Z$/u)
      assert.doesNotMatch(comment.body, /^[ \n]|\s$/u)
      if (comment.rating) {
        assert.ok(["star", "face"].includes(comment.rating.icon))
        assert.ok([1, 2, 3, 4, 5].includes(comment.rating.value))
      }
    })
  }
})
