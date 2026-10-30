/**
 * Pictures recovered after the recovery archive was made. Packaging stores
 * each file as archive media: in place of an image the archive kept only as a
 * remote address, or after the paragraph of a tweet the blog embedded.
 */
export type RecoveredFile = {
  /** Archive media name: the first 12 hex digits of its SHA-256. */
  media: string
  /** The address the bytes come from. */
  source: string
  /** The Wayback capture of `source` the bytes come from, if archived. */
  capture?: string
}

export type RecoveredImage = {
  /** The image address as the packaged post records it. */
  address: string
  /** The files shown in its place, in order. */
  files: RecoveredFile[]
  /**
   * The original was never archived; the files are the same official
   * pictures as published elsewhere, so the page labels them as stand-ins.
   */
  standIn?: boolean
}

export type TweetPictures = {
  /** The tweet's picture link (pic.twitter.com) as the post records it. */
  link: string
  /** The tweet's pictures, in reading order. */
  files: RecoveredFile[]
}

const AMAZON = "http://ecx.images-amazon.com/images/I/"
const OBS = "https://obs.line-scdn.net/"
const LISANI = "https://www.lisani.jp/admin/wp-content/uploads/2017/11/"

/**
 * Store thumbnails from Wayback captures of the same address. Amazon renders
 * a thumbnail anew for each request, so the bytes may differ from the file
 * the blog showed, while the picture is the same.
 */
function amazon(id: string, media: string, capture: string): RecoveredImage {
  const address = `${AMAZON}${id}._SL160_.jpg`
  return { address, files: [{ media, source: address, capture }] }
}

/** The 2017-11-18 article's pictures, from the same day's Lis-Ani! report. */
function lisani(token: string, files: [string, string][]): RecoveredImage {
  return {
    address: `${OBS}${token}/small`,
    files: files.map(([media, name]) => ({
      media,
      source: `${LISANI}1711201336-${name}.jpg`,
    })),
    standIn: true,
  }
}

function tweet(link: string, ids: [string, string][]): TweetPictures {
  return {
    link: `https://t.co/${link}`,
    files: ids.map(([media, id]) => ({
      media,
      source: `https://pbs.twimg.com/media/${id}?format=jpg&name=orig`,
    })),
  }
}

export const recoveredImages: RecoveredImage[] = [
  amazon("51%2B-mGeP2tL", "c65cd1a2039d.jpg", "20160402022349"),
  amazon("51%2BFYT7rw2L", "10e900ec0417.jpg", "20161112135530"),
  amazon("51051T2YVuL", "f26e5e6b6e1b.jpg", "20220307162227"),
  amazon("517s3E3VFrL", "2dbbd15e05ab.jpg", "20160404072932"),
  amazon("51BCcChjU0L", "9afa5f8030f9.jpg", "20160404063514"),
  amazon("51IE5YReY7L", "69410f59b8a7.jpg", "20151126110402"),
  amazon("51LtNSpoYsL", "9fe21ccbaaaf.jpg", "20170517021451"),
  amazon("51QWD808nHL", "7127b72a8789.jpg", "20160614002359"),
  amazon("51UhYwIQebL", "53de3a027a56.jpg", "20160203054229"),
  amazon("51c%2B7U1ICBL", "93d752986683.jpg", "20160923150325"),
  amazon("51eVw0AhK-L", "48b0ee01baff.jpg", "20160511112252"),
  amazon("51prpDIVQqL", "1b026b4054fb.jpg", "20170517022244"),
  // IMG_1162, the new artist photo.
  lisani(
    "0h2eBf2BnGbUp_H0EiBt0SHT1CayUGfHdCFWd6cApJZ2QKc3pKF2V4fAVBLzoKUCNUAA1VWglBYSQlWmpUFxl0dC1DSHsmc1F1FHF7MFobM3taKCkeQH0iJAhINnpUK28dRXxxJAkWNw",
    [["dde1ca53f49d.jpg", "mk-001"]],
  ),
  // IMG_1163, the store bonus pictures.
  lisani(
    "0hVv9wdfx8CVkEDiUxfcx2DkZTDzZ9bRNRbnYeY3FYA3dxYh5ZbHQcb35QSyx_eAxRbikwQVNTLgBtVRduUB0yZ0FlDRUqfC5wbCsvIyEKV2ghOU0NO2xGN3QPUmsrOAsOPm0VN3IHUw",
    [
      ["6b38731a2918.jpg", "mk-009"],
      ["4cf00a3f367d.jpg", "mk-007"],
      ["8b2fe3d7c458.jpg", "mk-008"],
      ["bb08070cf397.jpg", "mk-006"],
    ],
  ),
  // IMG_1164, the acrylic keyholder.
  lisani(
    "0hpeUnkTWBL0poFwMiEdVQHSpKKSURdDVCAm84cB1BJWQdezhKAG06fBJJbQEVRwNKLTcVXhthChIZYCxFDiMneTVtC3wtIi1FFAk5ME0TcXtNIGseV3VgJB9EeXtNIi0dUnQzJB4edQ",
    [["8d9147ab5a24.jpg", "mk-005"]],
  ),
]

/** The @wb_anime tweets the 2017-11-01 article embedded. */
export const tweetPictures: TweetPictures[] = [
  tweet("CZX1ny96rc", [
    ["98c63725ccfc.jpg", "DNI_ONlV4AAeC4Y"],
    ["8212d7597e7e.jpg", "DNI_ONnVQAUWbI2"],
    ["486885b2f016.jpg", "DNI_ONnU8AEY5no"],
    ["a0c08255224a.jpg", "DNI_ONkVwAA9-Nq"],
  ]),
  tweet("DcktPjXQ7V", [["299f3f50ff26.jpg", "DNfpNJ_VQAEQbAj"]]),
]
