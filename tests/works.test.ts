import assert from "node:assert/strict"
import crypto from "node:crypto"
import fs from "node:fs/promises"
import os from "node:os"
import path from "node:path"
import test, { type TestContext } from "node:test"

import {
  creditOf,
  kanoCredits,
  packageWorks,
  parseCsv,
  participationForms,
  participationRoles,
  editionDiscs,
  editionTracks,
  linkUploads,
  personalFormat,
  printedDiscOf,
  publicPrice,
  publicSource,
  certainText,
  publicText,
  publicTrackNote,
  recordId,
  storeGroups,
  storeUrl,
  uploadVideoRefs,
  worksVideoIndex,
} from "../scripts/package-works.ts"
import { mediaSrcSet } from "../src/lib/media.ts"
import {
  byDate,
  discsOf,
  firstEdition,
  groupByYear,
  matches,
  uploadLinks,
} from "../src/works/content.ts"
import {
  foldedCount,
  japanDay,
  recentLinks,
  recentUploads,
  type WorksRecentUpload,
  type WorksUploadsOverlay,
} from "../src/works/use-works-uploads.ts"
import type {
  ParticipationRelease,
  PersonalEdition,
  PersonalRelease,
  Upload,
} from "../src/works/types.ts"

const PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+j1WQAAAAASUVORK5CYII=",
  "base64",
)
const OTHER_PNG = Buffer.concat([PNG, Buffer.from("tail")])
const THUMB_PNG = Buffer.concat([PNG, Buffer.from("thumb")])

const mediaName = (body: Buffer) =>
  `${crypto.createHash("sha256").update(body).digest("hex").slice(0, 12)}.png`

const csv = (rows: string[][]) =>
  `﻿${rows
    .map((row) =>
      row
        .map((cell) =>
          /[",\n]/u.test(cell) ? `"${cell.replaceAll('"', '""')}"` : cell,
        )
        .join(","),
    )
    .join("\r\n")}\r\n`

async function collection(t: TestContext) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "kano-works-test-"))
  t.after(async () => {
    await fs.rm(root, { recursive: true, force: true })
  })
  const library = path.join(root, "kano_official")
  await fs.mkdir(path.join(library, "images", "covers"), { recursive: true })
  await fs.mkdir(path.join(library, "参与专辑", "img"), { recursive: true })
  await fs.mkdir(path.join(library, "投稿作品"), { recursive: true })
  await fs.writeFile(path.join(library, "images", "covers", "a.png"), PNG)
  await fs.writeFile(path.join(library, "参与专辑", "img", "b.png"), OTHER_PNG)
  await fs.mkdir(path.join(library, "投稿作品", "thumbnails"), {
    recursive: true,
  })
  await fs.writeFile(
    path.join(library, "投稿作品", "thumbnails", "bbbbbbbbbbb.png"),
    THUMB_PNG,
  )
  await fs.writeFile(
    path.join(library, "manifest.json"),
    JSON.stringify({
      images: [
        {
          file: "images/covers/a.png",
          title: "Synthetic album cover",
          title_zh: "合成专辑：初回限定盘封面（NAS）",
          source_page: "https://example.com/shop/item/1",
          evidence_class: "release_artwork_third_party_file",
        },
        {
          file: "images/covers/c.gif",
          title: "Synthetic bonus disc",
          title_zh: "合成专辑：特典盘面",
          source_page: "https://example.com/news/1",
          evidence_class: "publisher_or_commissioned_creator",
        },
      ],
    }),
  )
  await fs.writeFile(
    path.join(library, "discography.csv"),
    csv([
      [
        "date",
        "title",
        "category",
        "kind",
        "kind_note",
        "group",
        "notes",
        "label",
        "catalog",
        "cover_files",
      ],
      [
        "2019-08-12",
        "合成专辑",
        "独立作品",
        "单曲",
        "1st シングル",
        "",
        "据使用者的 NAS 收藏补入",
        "Example Records",
        "EX-001 (VocaDB)",
        "images/covers/a.png",
      ],
      [
        "2011-12-31",
        "Unit Disc",
        "组合作品",
        "同人",
        "同人专辑（C81）",
        "Example Unit",
        "C81",
        "Example Unit",
        "",
        "",
      ],
    ]),
  )
  await fs.writeFile(
    path.join(library, "discography_tracks.csv"),
    csv([
      [
        "date",
        "release",
        "edition",
        "disc",
        "disc_label",
        "no",
        "track",
        "credit",
        "note",
        "kano",
      ],
      [
        "2019-08-12",
        "合成专辑",
        "",
        "1",
        "",
        "2",
        "Second",
        "",
        "カバー? (原曲不明)",
        "",
      ],
      [
        "2019-08-12",
        "合成专辑",
        "",
        "1",
        "",
        "1",
        "First",
        "作詞:鹿乃",
        "TVアニメ「Example」OP",
        "",
      ],
      [
        "2019-08-12",
        "合成专辑",
        "初回限定盤",
        "2",
        "DVD",
        "1",
        "Music Video",
        "",
        "",
        "",
      ],
    ]),
  )
  await fs.writeFile(
    path.join(library, "discography_editions.csv"),
    csv([
      [
        "date",
        "release",
        "edition",
        "catalog",
        "jan",
        "price",
        "format",
        "packaging",
        "cover_files",
        "bonus",
        "bonus_images",
        "notes",
        "source",
      ],
      [
        "2019-08-12",
        "合成专辑",
        "初回限定盤",
        "EX-001",
        "4900000000001",
        "2,000 日元",
        "CD+DVD",
        "デジパック仕様（销售页 3000 像素）",
        "images/covers/a.png",
        "封入特典：ステッカー（据官方公告）；无其他封入特典",
        "images/covers/c.gif",
        "2019-09-01 发售",
        "https://example.com/shop/item/1",
      ],
      [
        "2019-08-12",
        "合成专辑",
        "通常盤（发售中止）",
        "EX-002（MusicBrainz，存疑）",
        "",
        "",
        "CD",
        "",
        "",
        "",
        "",
        "原定 2019-09-01 发售",
        "",
      ],
    ]),
  )
  await fs.writeFile(
    path.join(library, "discography_store_bonuses.csv"),
    csv([
      [
        "date",
        "release",
        "store",
        "item",
        "applies_to",
        "image_files",
        "source",
      ],
      [
        "2019-08-12",
        "合成专辑",
        "Example Shop",
        "缶バッジ（骏河屋记 X-1）",
        "初回限定盤",
        "images/covers/a.png",
        "",
      ],
      [
        "2019-08-12",
        "合成专辑",
        "全国対象店",
        "B2 ポスター（官网正文写 A3）",
        "",
        "",
        "",
      ],
    ]),
  )
  await fs.writeFile(
    path.join(library, "参与专辑", "participation.csv"),
    csv([
      [
        "date",
        "title",
        "kano_tracks",
        "member_of",
        "form",
        "label",
        "event",
        "catalog",
        "notes",
        "cover_file",
        "source_url",
        "cover_source",
      ],
      [
        "2011-08-13",
        "Synthetic Compilation",
        "演唱、作词：3. Song",
        "",
        "配信单曲 / 印刷 CD-R",
        "Example Circle",
        "C80 (day 2)",
        "EX-9（MusicBrainz，存疑）",
        "与 Other Album 3 为同曲的不同音源",
        "img/b.png",
        "https://example.org/db/album/1",
        "Example DB https://example.org/db/cover/1",
      ],
    ]),
  )
  await fs.writeFile(
    path.join(library, "参与专辑", "participation_tracks.csv"),
    csv([
      ["date", "release", "disc", "no", "track", "credit", "note", "kano"],
      [
        "2011-08-13",
        "Synthetic Compilation",
        "1",
        "3",
        "Song",
        "circle feat. 鹿乃 (鹿乃: Lyricist)",
        "与 Other Album 3 为同曲的不同音源",
        "1",
      ],
      [
        "2011-08-13",
        "Synthetic Compilation",
        "1",
        "1",
        "Opening",
        "circle",
        "",
        "0",
      ],
    ]),
  )
  await fs.writeFile(
    path.join(library, "投稿作品", "works.csv"),
    csv([
      [
        "date",
        "song",
        "original_artist",
        "kind",
        "with",
        "niconico",
        "youtube",
        "youtube_members",
        "bilibili",
        "bilibili_title",
        "album",
        "notes",
        "tracks",
      ],
      [
        "2013-05-15",
        "Synthetic Song",
        "",
        "翻唱",
        "",
        "sm10000001",
        "aaaaaaaaaaa;bbbbbbbbbbb",
        "",
        "BV1aaaaaaaaa",
        "",
        "合成专辑（2019）、Other（2020）",
        "bilibili 2020-01-07 补投（推定为同一录音）",
        "",
      ],
      [
        "2022-04-05",
        "Medley",
        "",
        "合集",
        "",
        "",
        "",
        "ccccccccccc",
        "",
        "",
        "",
        "YouTube 会员限定",
        "First；Second",
      ],
    ]),
  )
  return { root, library }
}

test("CSV rows keep quoted commas, quotes and line breaks", () => {
  const rows = parseCsv('﻿a,b\r\n"x, y","say ""hi""\nagain"\r\n\r\nplain,\n')
  assert.deepEqual(rows, [
    { a: "x, y", b: 'say "hi"\nagain' },
    { a: "plain", b: "" },
  ])
  assert.throws(() => parseCsv("a,b\nonly\n"), /fields/u)
})

test("collection words map to codes, and unknown ones stop packaging", () => {
  assert.deepEqual(participationRoles("演唱、作词、声演：1. Song"), [
    "vocal",
    "lyrics",
    "voice",
  ])
  assert.deepEqual(participationRoles("配音：drama"), ["voice"])
  assert.throws(() => participationRoles("监修：1. Song"), /role/u)
  assert.deepEqual(participationForms("特典 · 广播 CD"), ["bonus", "drama"])
  assert.deepEqual(participationForms("众筹回报 · 非卖品"), ["notForSale"])
  assert.deepEqual(participationForms(""), [])
  assert.throws(() => participationForms("黑胶"), /form/u)
  assert.deepEqual(kanoCredits("nana feat. ミク (鹿乃: Lyricist, Other)"), [
    "lyrics",
    "other",
  ])
  assert.deepEqual(kanoCredits("Vo: 鹿乃"), [])
  assert.throws(() => kanoCredits("x (鹿乃: Mixer)"), /credit/u)
})

test("personal kinds, prices and edition tracklists", () => {
  assert.equal(personalFormat("专辑"), "album")
  assert.equal(personalFormat("单曲"), "single")
  assert.equal(personalFormat("EP"), "ep")
  assert.equal(personalFormat("同人"), "doujin")
  assert.throws(() => personalFormat("黑胶"), /kind/u)

  assert.equal(publicPrice("2,000 日元"), "¥2,000")
  assert.equal(publicPrice("15 元"), "CN¥15")
  assert.equal(publicPrice("¥1,500+税"), "¥1,500+税")

  const track = (disc: number, no: number, title: string) => ({
    disc,
    no,
    title,
    credit: "",
    note: "",
  })
  const shared = [track(1, 1, "Song"), track(1, 2, "Song (instrumental)")]
  // An edition's own disc 1 replaces the shared one; other discs add to it.
  assert.deepEqual(
    editionTracks(shared, [track(1, 1, "Song -TV size-"), track(2, 1, "MV")]),
    [track(1, 1, "Song -TV size-"), track(2, 1, "MV")],
  )
  assert.deepEqual(editionTracks(shared, [track(2, 1, "MV")]), [
    ...shared,
    track(2, 1, "MV"),
  ])
})

test("shown fields drop the collection's checking asides", () => {
  assert.equal(publicText("封面（NAS）"), "封面")
  assert.equal(publicText("文字图（NAS，非盘面，来源未核实）"), "文字图")
  assert.equal(publicText("通常盘封面（销售页 3000 像素）"), "通常盘封面")
  assert.equal(
    publicText("アニメ盘封面（ヘヴィーオブジェクト）"),
    "アニメ盘封面（ヘヴィーオブジェクト）",
  )
  assert.equal(publicText("CD 商品图（らしんばん）"), "CD 商品图（らしんばん）")
  assert.equal(publicText("JTCD-0005 (Discogs: JTCD-0006)"), "JTCD-0005")
  assert.equal(
    publicText("Shinpei Nasuno (Rebrast)"),
    "Shinpei Nasuno (Rebrast)",
  )
  assert.throws(() => publicText("据整理者的收藏补入"), /Checking wording/u)
  assert.equal(certainText("C82 (unconfirmed)"), "")
  assert.equal(certainText("BBCL-0007（MusicBrainz，存疑）"), "")
  assert.equal(certainText("TECI-1609 (初回限定盤)"), "TECI-1609 (初回限定盤)")
  assert.equal(publicTrackNote("カバー? (原曲不明)"), "")
  assert.equal(publicTrackNote("live 版见 Example 13"), "")
  assert.equal(
    publicTrackNote("TVアニメ「Example」OP"),
    "TVアニメ「Example」OP",
  )
})

test("works packaging writes the tables and covers named by their bytes", async (t) => {
  const { root, library } = await collection(t)
  const contentDir = path.join(root, "content")
  const mediaDir = path.join(root, "data", "works")
  assert.deepEqual(
    await packageWorks(library, {
      contentDir,
      mediaDir,
      indexFile: path.join(root, "videos.json"),
    }),
    {
      personal: 2,
      participation: 1,
      uploads: 2,
      media: 2,
      bytes: PNG.length + OTHER_PNG.length,
      skipped: ["images/covers/c.gif"],
    },
  )
  const read = async (name: string) =>
    JSON.parse(await fs.readFile(path.join(contentDir, name), "utf8"))

  const personal = (await read("personal.json")) as PersonalRelease[]
  const cover = {
    media: mediaName(PNG),
    width: 1,
    height: 1,
    caption: "初回限定盘封面",
    source: "https://example.com/shop/item/1",
    thirdParty: true,
  }
  const shared = [
    { disc: 1, no: 2, title: "Second", credit: "", note: "" },
    {
      disc: 1,
      no: 1,
      title: "First",
      credit: "作詞:鹿乃",
      note: "TVアニメ「Example」OP",
    },
  ]
  assert.deepEqual(personal[0], {
    id: recordId("2019-08-12", "合成专辑"),
    date: "2019-08-12",
    title: "合成专辑",
    format: "single",
    digital: false,
    cancelled: false,
    unit: null,
    label: "Example Records",
    catalog: "EX-001",
    covers: [cover],
    disc: null,
    printedDisc: false,
    cutout: null,
    tracks: shared,
    discs: [],
    editions: [
      {
        name: "初回限定盤",
        date: "2019-09-01",
        cancelled: false,
        catalog: "EX-001",
        jan: "4900000000001",
        price: "¥2,000",
        format: "CD+DVD",
        packaging: "デジパック仕様",
        covers: [cover],
        bonuses: ["封入特典：ステッカー"],
        bonusImages: [],
        storeBonuses: [
          { store: "Example Shop", item: "缶バッジ", images: [cover] },
        ],
        stores: [],
        tracks: [
          ...shared,
          {
            disc: 2,
            discLabel: "DVD",
            no: 1,
            title: "Music Video",
            credit: "",
            note: "",
          },
        ],
        discs: [],
        printedDisc: false,
      },
      {
        name: "通常盤",
        date: null,
        cancelled: true,
        catalog: "",
        jan: "",
        price: "",
        format: "CD",
        packaging: "",
        covers: [],
        bonuses: [],
        bonusImages: [],
        storeBonuses: [],
        stores: [],
        tracks: shared,
        discs: [],
        // Withdrawn before sale, so no disc was pressed.
        printedDisc: true,
      },
    ],
    storeBonuses: [{ store: "全国対象店", item: "B2 ポスター", images: [] }],
    stores: [],
  })
  assert.equal(personal[1].unit, "Example Unit")
  assert.equal(personal[1].format, "doujin")
  assert.deepEqual(personal[1].covers, [])

  const [participation] = (await read(
    "participation.json",
  )) as ParticipationRelease[]
  assert.deepEqual(participation.roles, ["vocal", "lyrics"])
  assert.deepEqual(participation.forms, ["digital", "cdr"])
  assert.equal(participation.event, "C80")
  assert.equal(participation.catalog, "")
  assert.deepEqual(participation.cover, {
    media: mediaName(OTHER_PNG),
    width: 1,
    height: 1,
    source: "https://example.org/db/cover/1",
    sourceLabel: "Example DB",
    thirdParty: true,
  })
  assert.deepEqual(
    participation.tracks.map((track) => [
      track.no,
      track.credit,
      track.kano,
      track.kanoCredits,
    ]),
    [
      [3, "circle feat. 鹿乃", true, ["lyrics"]],
      [1, "circle", false, []],
    ],
  )

  const uploads = (await read("uploads.json")) as Upload[]
  assert.deepEqual(uploads[0].youtube, ["aaaaaaaaaaa", "bbbbbbbbbbb"])
  assert.deepEqual(uploads[0].albums, ["合成专辑（2019）", "Other（2020）"])
  assert.equal(uploads[0].kind, "cover")
  assert.deepEqual(uploads[1].tracks, ["First", "Second"])
  assert.deepEqual(uploads[1].youtubeMembers, ["ccccccccccc"])
  // Saved thumbnails are not packaged: the server downloads upload covers.
  assert.equal("cover" in uploads[0], false)

  // The collection's checking notes reach no published file.
  for (const name of ["personal.json", "participation.json", "uploads.json"]) {
    const text = await fs.readFile(path.join(contentDir, name), "utf8")
    assert.doesNotMatch(
      text,
      /"notes"|同曲|补投|NAS|据|骏河屋|官网|像素|存疑|发售中止/u,
      name,
    )
  }

  assert.deepEqual(await read("summary.json"), {
    personal: 2,
    participation: 1,
    uploads: 2,
    firstYear: 2011,
    lastYear: 2022,
    updatedAt: uploads
      .map((upload) => upload.date)
      .sort()
      .at(-1),
  })
  // The server's index lists each upload's videos in cover order.
  const index = JSON.parse(
    await fs.readFile(path.join(root, "videos.json"), "utf8"),
  ) as { updatedAt: string; uploads: { id: string; videos: unknown[] }[] }
  assert.equal(
    index.updatedAt,
    uploads
      .map((upload) => upload.date)
      .sort()
      .at(-1),
  )
  assert.deepEqual(
    index.uploads.map((upload) => upload.id),
    uploads.map((upload) => upload.id),
  )
  assert.deepEqual(index.uploads[1].videos, uploadVideoRefs(uploads[1]))
  assert.deepEqual(
    (await fs.readdir(mediaDir)).sort(),
    [mediaName(PNG), mediaName(OTHER_PNG)].sort(),
  )
  assert.deepEqual(await fs.readFile(path.join(mediaDir, mediaName(PNG))), PNG)
})

test("works packaging writes nothing when a cover lies outside the collection", async (t) => {
  const { root, library } = await collection(t)
  await fs.writeFile(path.join(root, "outside.png"), PNG)
  const table = path.join(library, "参与专辑", "participation.csv")
  await fs.writeFile(
    table,
    (await fs.readFile(table, "utf8")).replace(
      "img/b.png",
      "../../outside.png",
    ),
  )
  const contentDir = path.join(root, "content")
  const mediaDir = path.join(root, "data", "works")
  await assert.rejects(
    packageWorks(library, {
      contentDir,
      mediaDir,
      indexFile: path.join(root, "videos.json"),
    }),
    /outside the collection/u,
  )
  await assert.rejects(fs.readdir(mediaDir), { code: "ENOENT" })
  await assert.rejects(fs.readdir(contentDir), { code: "ENOENT" })
})

test("works packaging rejects a cover that is not an image", async (t) => {
  const { root, library } = await collection(t)
  await fs.writeFile(
    path.join(library, "images", "covers", "a.png"),
    "not an image",
  )
  await assert.rejects(
    packageWorks(library, {
      contentDir: path.join(root, "content"),
      mediaDir: path.join(root, "data", "works"),
      indexFile: path.join(root, "videos.json"),
    }),
    /not a JPEG or PNG/u,
  )
})

test("works packaging stops at an edition the release does not list", async (t) => {
  const { root, library } = await collection(t)
  const table = path.join(library, "discography_store_bonuses.csv")
  await fs.writeFile(
    table,
    (await fs.readFile(table, "utf8")).replace(",初回限定盤,", ",限定盤,"),
  )
  await assert.rejects(
    packageWorks(library, {
      contentDir: path.join(root, "content"),
      mediaDir: path.join(root, "data", "works"),
      indexFile: path.join(root, "videos.json"),
    }),
    /no edition named 限定盤/u,
  )
})

test("an edition shows its own discs and the shared ones it keeps", () => {
  const photo = (media: string) => ({
    media,
    width: 1,
    height: 1,
    thirdParty: true,
  })
  const track = (disc: number) => ({
    disc,
    no: 1,
    title: "",
    credit: "",
    note: "",
  })
  const shared = [
    { disc: 1, image: photo("aaaaaaaaaaaa.png") },
    { disc: 2, image: photo("bbbbbbbbbbbb.png") },
  ]
  const dvd = { disc: 3, image: photo("cccccccccccc.png") }
  // Its own DVD, the shared CD, and no photo for disc 2, which it replaces.
  assert.deepEqual(
    editionDiscs(
      shared,
      [dvd],
      [track(2), track(3)],
      [track(1), track(2), track(3)],
    ),
    [shared[0], dvd],
  )
  // A shared photo of a disc the edition does not have stays out.
  assert.deepEqual(editionDiscs(shared, [], [], [track(1)]), [shared[0]])
})

test("a release opens on the edition with the most disc photos", () => {
  const edition = (...media: string[]) => ({
    discs: media.map((name) => ({ image: { media: name } })),
  })
  // A download before its CD opens on the CD; CD+DVD before the CD alone.
  assert.equal(firstEdition([edition(), edition("cd")]), 1)
  assert.equal(
    firstEdition([edition(), edition("cd", "dvd"), edition("cd")]),
    1,
  )
  // Ties and releases without disc photos keep the first edition.
  assert.equal(firstEdition([edition("a", "b"), edition("c", "d")]), 0)
  assert.equal(firstEdition([edition(), edition()]), 0)
  assert.equal(firstEdition([]), 0)
  // A store set of the limited edition and a bonus disc bundles it; a
  // later edition with other discs still takes over.
  assert.equal(
    firstEdition([
      edition("cd", "dvd"),
      edition("cd"),
      edition("cd", "dvd", "bonus"),
    ]),
    0,
  )
  assert.equal(firstEdition([edition("cd"), edition("cd2", "dvd")]), 1)
})

test("a release without a pressed disc prints its cover on the record", () => {
  const row = (kindNote: string, printed = "") => ({
    title: "Synthetic",
    kind_note: kindNote,
    catalog: "",
    printed_disc: printed,
  })
  const edition = (format: string, printedDisc: boolean) =>
    ({ format, printedDisc }) as PersonalEdition
  // A download sold in one version, and editions all downloads or withdrawn.
  assert.equal(printedDiscOf(row("配信单曲"), []), true)
  assert.equal(
    printedDiscOf(row(""), [edition("CD", true), edition("CD", true)]),
    true,
  )
  // A download released beside a CD keeps the CD's record.
  assert.equal(
    printedDiscOf(row(""), [edition("配信", true), edition("CD", false)]),
    false,
  )
  assert.equal(printedDiscOf(row("同人专辑"), []), false)
  // The collection can ask for it, and an unknown value stops packaging.
  assert.equal(printedDiscOf(row("同人专辑", "封面"), []), true)
  assert.throws(() => printedDiscOf(row("同人专辑", "yes"), []), /printed_disc/)
})

test("a seller's listing shows only its marketplace's name", () => {
  const markets = new Map([["example.net", "Market"]])
  const listing = "https://example.net/item/1"
  assert.deepEqual(publicSource(listing, "", markets), {
    sourceLabel: "Market",
  })
  // A recorded site name wins; other pages keep their link.
  assert.deepEqual(publicSource(listing, "Shop", markets), {
    sourceLabel: "Shop",
  })
  assert.deepEqual(
    publicSource("https://example.com/item/1", "Shop", markets),
    {
      source: "https://example.com/item/1",
      sourceLabel: "Shop",
    },
  )
  assert.deepEqual(publicSource("", "", markets), {})
})

test("a file from a person's copy credits them and keeps its page private", async (t) => {
  const record = (role: string, name = "", url = "") => ({
    file: "images/covers/x.png",
    title: "",
    title_zh: "",
    source_page: "",
    evidence_class: "release_artwork_third_party_file",
    credit_role: role,
    credit_name: name,
    credit_url: url,
  })
  assert.equal(creditOf(record("")), null)
  assert.deepEqual(creditOf(record("fan")), { role: "fan" })
  assert.deepEqual(
    creditOf(record("scan", "collector", "https://example.org/collector")),
    { role: "scan", name: "collector", url: "https://example.org/collector" },
  )
  // A scan or photo names its person; a link needs a name and a web address.
  assert.throws(() => creditOf(record("scan")), /needs a name/)
  assert.throws(
    () => creditOf(record("fan", "", "https://example.org/")),
    /needs a name/,
  )
  assert.throws(
    () => creditOf(record("scan", "a", "ftp://example.org/")),
    /web source/,
  )
  assert.throws(() => creditOf(record("found", "a")), /Unknown credit role/)
  assert.throws(() => creditOf(record("", "a")), /needs a role/)

  const { root, library } = await collection(t)
  const contentDir = path.join(root, "content")
  const mediaDir = path.join(root, "data", "works")
  const manifestFile = path.join(library, "manifest.json")
  const manifest = JSON.parse(await fs.readFile(manifestFile, "utf8"))
  Object.assign(manifest.images[0], {
    credit_role: "photo",
    credit_name: "collector",
    credit_url: "https://example.org/collector",
  })
  await fs.writeFile(manifestFile, JSON.stringify(manifest))
  await packageWorks(library, {
    contentDir,
    mediaDir,
    indexFile: path.join(root, "videos.json"),
  })
  const [release] = JSON.parse(
    await fs.readFile(path.join(contentDir, "personal.json"), "utf8"),
  ) as PersonalRelease[]
  const [cover] = release.covers
  assert.deepEqual(cover.credit, {
    role: "photo",
    name: "collector",
    url: "https://example.org/collector",
  })
  assert.equal(cover.source, undefined)
  assert.equal(
    (
      await fs.readFile(path.join(contentDir, "personal.json"), "utf8")
    ).includes("example.com/shop"),
    false,
  )
})

test("store listings keep canonical addresses only", () => {
  assert.equal(
    storeUrl("spotify", "https://open.spotify.com/album/synthetic"),
    "https://open.spotify.com/album/synthetic",
  )
  assert.equal(
    storeUrl(
      "youtube_music",
      "https://music.youtube.com/playlist?list=OLAK5uy_synthetic",
    ),
    "https://music.youtube.com/playlist?list=OLAK5uy_synthetic",
  )
  for (const [platform, url] of [
    // A referral or a sharer's token, another platform's host, plain HTTP.
    ["spotify", "https://open.spotify.com/album/synthetic?si=token"],
    [
      "youtube_music",
      "https://music.youtube.com/playlist?list=a&feature=share",
    ],
    ["apple_music", "https://open.spotify.com/album/synthetic"],
    ["mora", "http://mora.jp/package/synthetic/"],
  ] as const)
    assert.throws(() => storeUrl(platform, url), /Not a canonical/u)
})

test("store listings group by item and fold high-resolution stores", () => {
  const row = (item: string, platform: string, url: string) => ({
    date: "2019-08-12",
    release: "合成专辑",
    edition: "",
    item,
    platform,
    url,
  })
  assert.deepEqual(
    storeGroups([
      row("TVsize", "spotify", "https://open.spotify.com/album/tv"),
      row("", "mora_hires", "https://mora.jp/package/full_hires/"),
      row("", "spotify", "https://open.spotify.com/album/full"),
      row("", "mora", "https://mora.jp/package/full/"),
      row("", "smart_link", "https://linkco.re/full"),
    ]),
    [
      {
        item: "",
        links: [
          {
            platform: "smart_link",
            url: "https://linkco.re/full",
            hiRes: false,
          },
          {
            platform: "spotify",
            url: "https://open.spotify.com/album/full",
            hiRes: false,
          },
          {
            platform: "mora",
            url: "https://mora.jp/package/full/",
            hiRes: true,
          },
        ],
      },
      {
        item: "TVsize",
        links: [
          {
            platform: "spotify",
            url: "https://open.spotify.com/album/tv",
            hiRes: false,
          },
        ],
      },
    ],
  )
  assert.throws(
    () =>
      storeGroups([row("", "unknown_store", "https://example.com/album/1")]),
    /Unknown store platform/u,
  )
})

test("an edition lists its own store items and the release's", async (t) => {
  const { root, library } = await collection(t)
  const contentDir = path.join(root, "content")
  const options = {
    contentDir,
    mediaDir: path.join(root, "data", "works"),
    indexFile: path.join(root, "videos.json"),
  }
  const storesFile = path.join(library, "discography_stores.csv")
  const table = (editionItem: string) =>
    csv([
      ["date", "release", "edition", "item", "platform", "url"],
      [
        "2019-08-12",
        "合成专辑",
        "",
        "",
        "apple_music",
        "https://music.apple.com/jp/album/1",
      ],
      [
        "2019-08-12",
        "合成专辑",
        "初回限定盤",
        editionItem,
        "spotify",
        "https://open.spotify.com/album/edition",
      ],
    ])
  await fs.writeFile(storesFile, table("TVsize"))
  await packageWorks(library, options)
  const [release] = JSON.parse(
    await fs.readFile(path.join(contentDir, "personal.json"), "utf8"),
  ) as PersonalRelease[]
  assert.deepEqual(
    release.stores.map((group) => group.item),
    [""],
  )
  assert.deepEqual(
    release.editions.map((edition) =>
      edition.stores.map((group) => [group.item, group.links[0].platform]),
    ),
    [[["TVsize", "spotify"]], []],
  )
  // Two unnamed items would show side by side under one edition.
  await fs.writeFile(storesFile, table(""))
  await assert.rejects(
    packageWorks(library, options),
    /needs a name for one of its store items/u,
  )
})

test("disc photos follow their discs and leave the covers", async (t) => {
  const { root, library } = await collection(t)
  const contentDir = path.join(root, "content")
  const mediaDir = path.join(root, "data", "works")
  const dvd = Buffer.concat([PNG, Buffer.from("dvd")])
  await fs.writeFile(path.join(library, "images", "covers", "d.png"), dvd)
  const manifestFile = path.join(library, "manifest.json")
  const manifest = JSON.parse(await fs.readFile(manifestFile, "utf8"))
  manifest.images.push({
    file: "images/covers/d.png",
    title: "Synthetic DVD",
    title_zh: "合成专辑：初回限定盘 DVD 盘面",
    source_page: "https://example.org/db/disc/1",
    evidence_class: "release_artwork_third_party_file",
  })
  await fs.writeFile(manifestFile, JSON.stringify(manifest))
  const editionsFile = path.join(library, "discography_editions.csv")
  await fs.writeFile(
    editionsFile,
    (await fs.readFile(editionsFile, "utf8")).replace(
      "images/covers/a.png",
      "images/covers/a.png;images/covers/d.png",
    ),
  )
  const discsFile = path.join(library, "discography_discs.csv")
  const table = (edition: string, disc: number) =>
    csv([
      ["date", "release", "edition", "disc", "image_file"],
      ["2019-08-12", "合成专辑", edition, String(disc), "images/covers/d.png"],
    ])
  await fs.writeFile(discsFile, table("初回限定盤", 2))
  await packageWorks(library, {
    contentDir,
    mediaDir,
    indexFile: path.join(root, "videos.json"),
  })
  const [release] = JSON.parse(
    await fs.readFile(path.join(contentDir, "personal.json"), "utf8"),
  ) as PersonalRelease[]
  const media = mediaName(dvd)
  assert.deepEqual(
    release.editions.map((edition) =>
      edition.discs.map((item) => [item.disc, item.image.media]),
    ),
    [[[2, media]], []],
  )
  assert.equal(
    release.editions[0].covers.some((cover) => cover.media === media),
    false,
  )
  // Only a disc 1 photo stands for the release on the shelf.
  assert.equal(release.disc, null)

  // A disc the edition does not have, or an edition the release lacks,
  // stops packaging.
  await fs.writeFile(discsFile, table("初回限定盤", 4))
  await assert.rejects(
    packageWorks(library, {
      contentDir,
      mediaDir,
      indexFile: path.join(root, "videos.json"),
    }),
    /初回限定盤 has no disc 4/u,
  )
  await fs.writeFile(discsFile, table("限定盤", 2))
  await assert.rejects(
    packageWorks(library, {
      contentDir,
      mediaDir,
      indexFile: path.join(root, "videos.json"),
    }),
    /has no edition named 限定盤/u,
  )
})

test("a cutout joins the release it names, and only a transparent PNG does", async (t) => {
  const { root, library } = await collection(t)
  const contentDir = path.join(root, "content")
  const mediaDir = path.join(root, "data", "works")
  const cutouts = path.join(library, "抠图")
  await fs.mkdir(cutouts)
  const figure = Buffer.concat([PNG, Buffer.from("figure")])
  await fs.writeFile(path.join(cutouts, "合成专辑.png"), figure)
  await packageWorks(library, {
    contentDir,
    mediaDir,
    indexFile: path.join(root, "videos.json"),
  })
  const personal = JSON.parse(
    await fs.readFile(path.join(contentDir, "personal.json"), "utf8"),
  ) as PersonalRelease[]
  assert.deepEqual(
    personal.map((release) => release.cutout),
    personal.map((release) =>
      release.title === "合成专辑"
        ? { media: mediaName(figure), width: 1, height: 1 }
        : null,
    ),
  )

  // A cutout without an alpha channel would print its ground on the page.
  const opaque = Buffer.from(figure)
  opaque[25] = 0
  await fs.writeFile(path.join(cutouts, "合成专辑.png"), opaque)
  await assert.rejects(
    packageWorks(library, {
      contentDir,
      mediaDir,
      indexFile: path.join(root, "videos.json"),
    }),
    /not a transparent PNG: 合成专辑\.png/u,
  )
  await fs.writeFile(path.join(cutouts, "合成专辑.png"), figure)
  await fs.writeFile(path.join(cutouts, "Unknown.png"), figure)
  await assert.rejects(
    packageWorks(library, {
      contentDir,
      mediaDir,
      indexFile: path.join(root, "videos.json"),
    }),
    /names no personal release: Unknown\.png/u,
  )
})

test("album tracks link the uploads of the same recording", () => {
  const track = (title: string) => ({
    disc: 1,
    no: 1,
    title,
    credit: "",
    note: "",
  })
  const release = {
    title: "Example Album",
    tracks: [track("Song A"), track("No Sweet N♡ Life")],
    editions: [],
  } as unknown as PersonalRelease
  const upload = (song: string, albums: string[], youtube: string) =>
    ({
      song,
      albums,
      niconico: null,
      youtube: [youtube],
      youtubeMembers: [],
      bilibili: null,
    }) as unknown as Upload
  linkUploads(
    [release],
    [
      upload("Song A", ["Example Album（2019）"], "aaaaaaaaaaa"),
      upload("No Sweet N❤︎Life", ["example album"], "bbbbbbbbbbb"),
      upload("Song A", ["Other Album（2020）"], "ccccccccccc"),
    ],
  )
  assert.deepEqual(release.tracks[0].listen?.youtube, ["aaaaaaaaaaa"])
  assert.deepEqual(release.tracks[1].listen?.youtube, ["bbbbbbbbbbb"])
})

test("works views sort, group and search records", () => {
  const items = [
    { date: "2019-01-02", disc: 2, no: 1 },
    { date: "2018-05-01", disc: 1, no: 2 },
    { date: "2019-03-04", disc: 1, no: 1 },
  ]
  assert.deepEqual(
    byDate(items, "newest").map((item) => item.date),
    ["2019-03-04", "2019-01-02", "2018-05-01"],
  )
  const ties = [
    { date: "2020-01-01", no: 1 },
    { date: "2020-01-01", no: 2 },
    { date: "2021-01-01", no: 3 },
  ]
  assert.deepEqual(
    byDate(ties, "newest").map((item) => item.no),
    [3, 1, 2],
  )
  assert.deepEqual(
    byDate(ties, "oldest").map((item) => item.no),
    [1, 2, 3],
  )
  assert.deepEqual(
    groupByYear(byDate(items, "oldest")).map((group) => [
      group.year,
      group.items.length,
    ]),
    [
      ["2018", 1],
      ["2019", 2],
    ],
  )
  assert.deepEqual(
    discsOf(items).map(({ disc, tracks }) => [
      disc,
      tracks.map((track) => track.no),
    ]),
    [
      [1, [1, 2]],
      [2, [1]],
    ],
  )
  assert.ok(matches("ＳＴＥＬＬＡ", "Stella-rium"))
  assert.ok(matches("  ", "anything"))
  assert.ok(!matches("nowhere", "rye", "HOPE"))
})

test("an upload links each platform it was posted to", () => {
  const upload = {
    niconico: "sm10000001",
    youtube: ["aaaaaaaaaaa"],
    youtubeMembers: ["ddddddddddd"],
    bilibili: "BV1aaaaaaaaa",
  } as Upload
  assert.deepEqual(uploadLinks(upload), [
    {
      platform: "niconico",
      id: "sm10000001",
      href: "https://www.nicovideo.jp/watch/sm10000001",
    },
    {
      platform: "youtube",
      id: "aaaaaaaaaaa",
      href: "https://www.youtube.com/watch?v=aaaaaaaaaaa",
    },
    {
      platform: "youtube",
      id: "ddddddddddd",
      href: "https://www.youtube.com/watch?v=ddddddddddd",
      members: true,
    },
    {
      platform: "bilibili",
      id: "BV1aaaaaaaaa",
      href: "https://www.bilibili.com/video/BV1aaaaaaaaa",
    },
  ])
  assert.deepEqual(
    uploadLinks({
      niconico: null,
      youtube: [],
      youtubeMembers: [],
      bilibili: null,
    } as unknown as Upload),
    [],
  )
})

test("works covers list the server's smaller copies", () => {
  const cover = "/media/works/0123456789ab.png"
  assert.equal(
    mediaSrcSet(cover, 700),
    [`${cover}?w=320 320w`, `${cover}?w=640 640w`, `${cover} 700w`].join(", "),
  )
  assert.equal(mediaSrcSet("/media/works/0123456789ab.gif", 700), undefined)
  assert.equal(mediaSrcSet("/media/works/../x.png", 700), undefined)
})

test("recent uploads keep well-formed items and link each site once", () => {
  const item: WorksRecentUpload = {
    id: "AAAAAAAAAAA",
    source: "youtube",
    title: "synthetic song",
    url: "https://www.youtube.com/watch?v=AAAAAAAAAAA",
    publishedAt: "2026-06-30T15:00:00Z",
    cover: null,
    origin: "auto",
    alternates: [
      {
        source: "bilibili",
        url: "https://www.bilibili.com/video/BV1synthetic",
      },
      {
        source: "bilibili",
        url: "https://www.bilibili.com/video/BV1synthetic",
      },
      { source: "niconico", url: "https://www.nicovideo.jp/watch/sm9" },
    ],
  }
  const overlay = {
    updatedAt: "2026-06-30",
    revision: 1,
    uploads: {},
    recent: [item, { ...item, id: "x", source: "niconico" }],
  } as unknown as WorksUploadsOverlay
  assert.deepEqual(
    recentUploads(overlay).map((upload) => upload.id),
    ["AAAAAAAAAAA"],
  )
  // An older server without the field lists none.
  assert.deepEqual(recentUploads({ ...overlay, recent: undefined }), [])
  assert.deepEqual(recentUploads(undefined), [])
  assert.deepEqual(recentLinks(item), [
    {
      platform: "youtube",
      href: "https://www.youtube.com/watch?v=AAAAAAAAAAA",
    },
    {
      platform: "bilibili",
      href: "https://www.bilibili.com/video/BV1synthetic",
    },
  ])
  // Dates are Japan dates: 15:00 UTC is the next day in Tokyo.
  assert.equal(japanDay(item.publishedAt), "2026-07-01")
  assert.equal(japanDay(null), "")
  assert.equal(japanDay("not a date"), "")
})

test("the newer uploads fold to two grid rows, or four list rows", () => {
  assert.equal(foldedCount(4), 8)
  assert.equal(foldedCount(3), 6)
  assert.equal(foldedCount(1), 4)
  assert.equal(foldedCount(0), 4)
})

test("the server's works index matches the packaged uploads", async () => {
  const read = async (relative: string) =>
    JSON.parse(
      await fs.readFile(new URL(`../${relative}`, import.meta.url), "utf8"),
    )
  const uploads = (await read("src/works/uploads.json")) as Upload[]
  const summary = (await read("src/works/summary.json")) as {
    updatedAt: string
  }
  assert.deepEqual(
    await read("backend/internal/works/videos.json"),
    worksVideoIndex(uploads, summary.updatedAt),
  )
  assert.ok(uploads.every((upload) => upload.date <= summary.updatedAt))
})

test("an upload's videos are tried YouTube first, then niconico and Bilibili", () => {
  assert.deepEqual(
    uploadVideoRefs({
      niconico: "sm9",
      youtube: ["aaaaaaaaaaa"],
      youtubeMembers: ["bbbbbbbbbbb"],
      bilibili: "BV1xx411c7mD",
    } as Upload),
    [
      { platform: "youtube", id: "aaaaaaaaaaa" },
      { platform: "youtube", id: "bbbbbbbbbbb", members: true },
      { platform: "niconico", id: "sm9" },
      { platform: "bilibili", id: "BV1xx411c7mD" },
    ],
  )
})

test("works packaging refuses a list date before its newest upload", async (t) => {
  const { root, library } = await collection(t)
  await assert.rejects(
    packageWorks(library, {
      contentDir: path.join(root, "content"),
      mediaDir: path.join(root, "data", "works"),
      indexFile: path.join(root, "videos.json"),
      through: "2000-01-01",
    }),
    /checked through 2000-01-01/u,
  )
  await assert.rejects(fs.readdir(path.join(root, "data", "works")), {
    code: "ENOENT",
  })
  await assert.rejects(fs.readdir(path.join(root, "content")), {
    code: "ENOENT",
  })
  await assert.rejects(fs.stat(path.join(root, "videos.json")), {
    code: "ENOENT",
  })
})

test("works packaging leaves out uploads marked as Shorts", async (t) => {
  const { root, library } = await collection(t)
  const table = path.join(library, "投稿作品", "works.csv")
  const header = [
    "date",
    "song",
    "original_artist",
    "kind",
    "with",
    "niconico",
    "youtube",
    "youtube_members",
    "bilibili",
    "bilibili_title",
    "album",
    "notes",
    "tracks",
    "format",
  ]
  const upload = (song: string, youtube: string, format: string) => [
    "2013-06-01",
    song,
    "",
    "翻唱",
    "",
    "",
    youtube,
    "",
    "",
    "",
    "",
    "",
    "",
    format,
  ]
  const options = {
    contentDir: path.join(root, "content"),
    mediaDir: path.join(root, "data", "works"),
    indexFile: path.join(root, "videos.json"),
  }
  await fs.writeFile(
    table,
    csv([
      header,
      upload("Synthetic Song", "aaaaaaaaaaa", ""),
      upload("Synthetic Short", "ddddddddddd", "Shorts"),
    ]),
  )
  await packageWorks(library, options)
  const uploads = JSON.parse(
    await fs.readFile(path.join(options.contentDir, "uploads.json"), "utf8"),
  ) as Upload[]
  assert.deepEqual(
    uploads.map((item) => item.song),
    ["Synthetic Song"],
  )
  await fs.writeFile(
    table,
    csv([header, upload("Synthetic Short", "ddddddddddd", "Reel")]),
  )
  await assert.rejects(
    packageWorks(library, options),
    /Unknown upload format: Reel/u,
  )
})

test("works packaging accepts calendar dates only", async (t) => {
  const { root, library } = await collection(t)
  const options = {
    contentDir: path.join(root, "content"),
    mediaDir: path.join(root, "data", "works"),
    indexFile: path.join(root, "videos.json"),
  }
  for (const through of ["2026-13-45", "2099-02-30", "2099-2-03"])
    await assert.rejects(
      packageWorks(library, { ...options, through }),
      /Not a Japan date/u,
    )
  await assert.rejects(fs.readdir(options.contentDir), { code: "ENOENT" })
  const table = path.join(library, "投稿作品", "works.csv")
  const rows = await fs.readFile(table, "utf8")
  const date = /\d{4}-\d{2}-\d{2}/u.exec(rows)![0]
  await fs.writeFile(table, rows.replace(date, `${date.slice(0, 4)}-02-30`))
  await assert.rejects(packageWorks(library, options), /Not a Japan date/u)
  await fs.writeFile(table, rows)
  await packageWorks(library, { ...options, through: "2096-02-29" })
})
