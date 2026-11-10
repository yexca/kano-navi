// Package works holds the index of the Works page's uploads that the server
// joins with stored videos: each upload's ID and its videos by platform, in
// the order covers are tried. scripts/package-works.ts writes videos.json
// from the maintainer's collection; the list ships with each release and the
// server never changes it.
package works

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"time"
)

// Platforms of an upload's videos.
const (
	YouTube  = "youtube"
	Niconico = "niconico"
	Bilibili = "bilibili"
)

// Video is one of an upload's videos.
type Video struct {
	Platform string `json:"platform"`
	ID       string `json:"id"`
	// Members marks a YouTube video only channel members can watch.
	Members bool `json:"members,omitempty"`
}

// Upload is one recording and its videos, in cover order: YouTube, YouTube
// members-only, niconico, then Bilibili.
type Upload struct {
	ID     string  `json:"id"`
	Date   string  `json:"date"`
	Videos []Video `json:"videos"`
}

// Index is the packaged upload list.
type Index struct {
	// UpdatedAt is the Japan date the list was checked through.
	UpdatedAt string   `json:"updatedAt"`
	Uploads   []Upload `json:"uploads"`
}

//go:embed videos.json
var packaged []byte

var (
	idPatterns = map[string]*regexp.Regexp{
		YouTube:  regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`),
		Niconico: regexp.MustCompile(`^(?:sm|nm|so)[0-9]{1,10}$`),
		Bilibili: regexp.MustCompile(`^BV1[1-9A-HJ-NP-Za-km-z]{9}$`),
	}
	uploadID = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}-[a-f0-9]{6}(?:-[0-9]+)?$`)
	isoDate  = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
)

// validDate reports whether value is a calendar date in YYYY-MM-DD form.
func validDate(value string) bool {
	if !isoDate.MatchString(value) {
		return false
	}
	_, err := time.Parse(time.DateOnly, value)
	return err == nil
}

// ValidVideo reports whether id has its platform's shape.
func ValidVideo(platform, id string) bool {
	pattern, ok := idPatterns[platform]
	return ok && pattern.MatchString(id)
}

// Parse reads an index, keeping only well-formed uploads and videos.
func Parse(content []byte) Index {
	var raw Index
	if json.Unmarshal(content, &raw) != nil {
		return Index{Uploads: []Upload{}}
	}
	out := Index{Uploads: []Upload{}}
	if validDate(raw.UpdatedAt) {
		out.UpdatedAt = raw.UpdatedAt
	}
	seen := map[string]bool{}
	for _, upload := range raw.Uploads {
		if !uploadID.MatchString(upload.ID) || !validDate(upload.Date) || seen[upload.ID] {
			continue
		}
		seen[upload.ID] = true
		videos := []Video{}
		for _, video := range upload.Videos {
			if ValidVideo(video.Platform, video.ID) {
				video.Members = video.Members && video.Platform == YouTube
				videos = append(videos, video)
			}
		}
		out.Uploads = append(out.Uploads, Upload{ID: upload.ID, Date: upload.Date, Videos: videos})
	}
	return out
}

var packagedIndex = Parse(packaged)

// Packaged is the index this build ships with.
func Packaged() Index { return packagedIndex }
