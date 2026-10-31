/**
 * The blog's own header and profile art, recovered from Wayback Machine
 * captures of lineblog.me/kano. The files are archive media like the posts'
 * pictures, packaged into data/lineblog/ and listed in the media catalog. Each
 * design applies from the Japan date of the first capture that shows it; a
 * page takes the design of the newest post it shows.
 */
export type LineblogArt = {
  /** Archive media name: the first 12 hex digits of its SHA-256. */
  media: string
  /** The address the blog loaded it from. */
  source: string
  /** The Wayback capture the bytes come from. */
  capture: string
}

export type LineblogDesign = {
  /** Japan date (inclusive) from which the blog used this design. */
  since: string
  /** The blog's link and menu colour for this design. */
  accent: string
  /** The profile text beside it, kept in its original Japanese. */
  profileText: string
  /** The profile text's Simplified Chinese translation. */
  profileTextZh: string
  /** The profile text's English translation. */
  profileTextEn: string
  /** The official site the menu bar linked as 鹿乃オフィシャルサイト. */
  officialSite: string
  header: LineblogArt
  profile: LineblogArt
}

const OBS = "https://obs.line-scdn.net/"

const SITE_WHV = "https://whv-amusic.com/kano/"
// First shown by the 2018-05-18 index capture; the 2018-04-23 one still
// linked SITE_WHV.
const SITE_AMEBA = "https://kano-official.amebaownd.com/"

const header2015: LineblogArt = {
  media: "941e5735900c.jpg",
  source: "http://line.blogimg.jp/kano/design/pc_design/header_bg.jpg",
  capture: "20160310193256",
}

const header2016: LineblogArt = {
  media: "0da83289f328.jpg",
  source: "http://line.blogimg.jp/kano/design/pc_design/header_bg.jpg?v=0318",
  capture: "20160529120551",
}

const header2017: LineblogArt = {
  media: "03adb75fc762.jpg",
  source: `${OBS}0hi8kV9zRRNhdWLxlxNztJQBdyMHgvTCwfPFc4ITBqKjk6TzIEKFUjISxxdHAkayggHTUAEBhbaV05dnADMSIkCSgmaEQldxo7GUE_bXMqYCV4GHVEbE15eSF_bCd9WnBGaBxxc3Et`,
  capture: "20180810022832",
}

const header2018: LineblogArt = {
  media: "85edc40caea4.jpg",
  source: `${OBS}0h1GjK3ywCbmhXMkP59NYRPxtvaAcuUXRgPUp4WiJnZBtkW31uPgBiEihibwZmeHc6LBxydig7VyIGZ1duNgB1VilmVCc6fXp8YjZxSRIuMFx_ASA2aVcjCnI6Ygp8AS03LlUpDHFnNAh4`,
  capture: "20190327134738",
}

const profile2015: LineblogArt = {
  media: "a46359dc7aa9.jpg",
  source: "http://line.blogimg.jp/kano/design/pc_design/profile_photo.jpg",
  capture: "20160311231111",
}

// The 400px copy of the art the blog first served at 240px from
// line.blogimg.jp (profile_photo.jpg?v=0318).
const profile2016: LineblogArt = {
  media: "8d127714bf83.jpg",
  source: `${OBS}0m0e742a26ef54370037322142617e657d337b626c204b294b107c3d3c282f3e306e3b643035303008792f32353f226a603d/large`,
  capture: "20170319002612",
}

const profile2017: LineblogArt = {
  media: "5b5533c91f9f.jpg",
  source: `${OBS}0hCa2oQ7WxHE4PFjLBOVpjGUFLGiF2dQZGZW4Sa3RBGiN2OBpbbScLdX4KGC59eEdTWzgPVE51QD9JZC5ZYyYodX9XJDtVRDsaVwcvQVYRBmIiI1MYNHBWLSMRQnZwLlIfN3AWKS0VR3khJwg/large`,
  capture: "20170517052416",
}

const profile2018: LineblogArt = {
  media: "a9d41bb6d892.jpg",
  source: `${OBS}0hoPj1MdsEMH9XSBzsXiBPKBkVNhAuKyp3PTA-WiwfNhIuZjZqNXknRCZUNB8lJmsoG2gJfnA-DygkBnQhNS97TxMSHgsjcHZdMXV2YXZAPVN6fHQtYyt_G3JIbkcocHAuYyY6GHtKb0wpeyA/large`,
  capture: "20180810010211",
}

const profile2019: LineblogArt = {
  media: "e4293469ed7c.jpg",
  source: `${OBS}0hbRqIEVCQPX1MMBc5cH9CKgJtOxI1Uyd1JkgzWDdnOxA1HjtoLgEqRj0sOR0-XmZMJCggSShQBSgdBQ5rdVIBaDFxZTAFcglpIgQrTz9FCFFhBH8veVZzGGgxY0UzU3widFQ3GmE0NEloAXM/large`,
  capture: "20190330222146",
}

/**
 * The platform's menu-bar Home icon (30x66, the house centred), the same in
 * every design; the menu showed it in place of the word Home.
 */
export const homeIcon: LineblogArt = {
  media: "960bf01a008b.png",
  source: "https://parts.lineblog.me/img/usr/lineblog/common/gnav_home.png",
  capture: "20200101031454",
}

const PROFILE_LEAD =
  "内なる強さが秘められた優しい歌声\n動画共有サイトを中心に活動している人気の女性ボーカリスト。"
const PROFILE_TWO_SINGLES = `${PROFILE_LEAD}1stSG「Stellar-rium」はTVアニメ「放課後のプレアデス」OP曲、2ndSG「ディアブレイブ」は「ヘヴィーオブジェクト」ED曲。`
const PROFILE_FOUR_SINGLES = `${PROFILE_LEAD}1stSG「Stellar-rium」はTVアニメ「放課後のプレアデス」OP、2ndSG「ディアブレイブ」は「ヘヴィーオブジェクト」ED、3rdSG「nameless」は「天鏡のアルデラミン」ED、4thSG「day by day」は「ソード・オラトリア」ED。`
const PROFILE_LEAD_ZH =
  "蕴藏着内在力量的温柔歌声\n以视频分享网站为中心活动的人气女歌手。"
const PROFILE_TWO_SINGLES_ZH = `${PROFILE_LEAD_ZH}1st单曲《Stellar-rium》为TV动画《放学后的昴宿星》OP曲，2nd单曲《ディアブレイブ》为《重装武器》ED曲。`
const PROFILE_FOUR_SINGLES_ZH = `${PROFILE_LEAD_ZH}1st单曲《Stellar-rium》为TV动画《放学后的昴宿星》OP，2nd单曲《ディアブレイブ》为《重装武器》ED，3rd单曲《nameless》为《天镜的极北之星》ED，4th单曲《day by day》为《剑姬神圣谭》ED。`
const PROFILE_LEAD_EN =
  "A gentle singing voice with a hidden inner strength\nA popular female vocalist active mainly on video-sharing sites."
const PROFILE_TWO_SINGLES_EN = `${PROFILE_LEAD_EN} Her 1st single "Stellar-rium" is the opening theme of the TV anime "Wish Upon the Pleiades", and her 2nd single "Dear Brave" the ending theme of "Heavy Object".`
const PROFILE_FOUR_SINGLES_EN = `${PROFILE_LEAD_EN} Her 1st single "Stellar-rium" is the opening of the TV anime "Wish Upon the Pleiades", her 2nd single "Dear Brave" the ending of "Heavy Object", her 3rd single "nameless" the ending of "Alderamin on the Sky", and her 4th single "day by day" the ending of "Sword Oratoria".`

const twoSingles = {
  profileText: PROFILE_TWO_SINGLES,
  profileTextZh: PROFILE_TWO_SINGLES_ZH,
  profileTextEn: PROFILE_TWO_SINGLES_EN,
}
const fourSingles = {
  profileText: PROFILE_FOUR_SINGLES,
  profileTextZh: PROFILE_FOUR_SINGLES_ZH,
  profileTextEn: PROFILE_FOUR_SINGLES_EN,
}

/*
 * Two profile photos were never archived: the one shown from 2018-01-13 to
 * the 2018-05-18 change, and the one shown with the 2018 header on
 * 2018-12-04. Those periods keep the photo before and after them.
 */
export const designs: LineblogDesign[] = [
  {
    since: "2015-01-01",
    accent: "#913143",
    ...twoSingles,
    officialSite: SITE_WHV,
    header: header2015,
    profile: profile2015,
  },
  // The blog's own version stamp on the new art (?v=0318); captures show it
  // between 2016-03-14 and 2016-03-29.
  {
    since: "2016-03-18",
    accent: "#f2746f",
    ...twoSingles,
    officialSite: SITE_WHV,
    header: header2016,
    profile: profile2016,
  },
  {
    since: "2017-03-13",
    accent: "#f2746f",
    ...fourSingles,
    officialSite: SITE_WHV,
    header: header2016,
    profile: profile2016,
  },
  {
    since: "2017-04-14",
    accent: "#f2746f",
    ...fourSingles,
    officialSite: SITE_WHV,
    header: header2016,
    profile: profile2017,
  },
  {
    since: "2017-04-29",
    accent: "#826856",
    ...fourSingles,
    officialSite: SITE_WHV,
    header: header2017,
    profile: profile2017,
  },
  {
    since: "2018-05-18",
    accent: "#826856",
    ...fourSingles,
    officialSite: SITE_AMEBA,
    header: header2017,
    profile: profile2018,
  },
  {
    since: "2018-07-21",
    accent: "#376c9c",
    ...fourSingles,
    officialSite: SITE_AMEBA,
    header: header2017,
    profile: profile2018,
  },
  {
    since: "2018-12-04",
    accent: "#376c9c",
    ...fourSingles,
    officialSite: SITE_AMEBA,
    header: header2018,
    profile: profile2019,
  },
]

export type LineblogDesignOption = {
  /** The choice's stored value: the Japan date its first design applies from. */
  id: string
  /** The latest design of the run, which a reader's choice applies. */
  design: LineblogDesign
  /** Japan date (exclusive) of the next choice, if any. */
  until?: string
}

/**
 * The looks a reader can pick in place of the automatic one. A run of
 * designs with the same header, profile photo and accent counts as one.
 */
export const designOptions = designs.reduce<LineblogDesignOption[]>(
  (options, design) => {
    const last = options.at(-1)
    if (
      last &&
      last.design.header === design.header &&
      last.design.profile === design.profile &&
      last.design.accent === design.accent
    ) {
      last.design = design
      return options
    }
    if (last) last.until = design.since
    options.push({ id: design.since, design })
    return options
  },
  [],
)

export type LineblogOfficialSite = {
  url: string
  /** Japan date (inclusive) from which the menu bar linked it. */
  since: string
  /** Japan date (exclusive) of the next site, if any. */
  until?: string
}

/** The official sites the menu bar linked, oldest first. */
export const officialSites = designs.reduce<LineblogOfficialSite[]>(
  (sites, design) => {
    const last = sites.at(-1)
    if (last?.url === design.officialSite) return sites
    if (last) last.until = design.since
    sites.push({ url: design.officialSite, since: design.since })
    return sites
  },
  [],
)

/** The design of a reader's choice, or null for the automatic one. */
export function chosenDesign(id: string | null) {
  return designOptions.find((option) => option.id === id)?.design ?? null
}

/** The blog's own art, for packaging alongside the posts' media. */
export function designMedia() {
  return [
    ...new Set([
      homeIcon.media,
      ...designs.flatMap((design) => [
        design.header.media,
        design.profile.media,
      ]),
    ]),
  ].sort()
}

/** The design the blog used on a Japan date (YYYY-MM-DD). */
export function designFor(japanDate: string | undefined) {
  if (!japanDate) return designs[designs.length - 1]
  let current = designs[0]
  for (const design of designs) if (design.since <= japanDate) current = design
  return current
}
