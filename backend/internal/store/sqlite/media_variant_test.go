package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/mediavariant"
)

func syntheticImage(t *testing.T, mime string, width, height int, alpha bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			a := uint8(0xff)
			if alpha && x < width/2 {
				a = 0x40
			}
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 7), G: uint8(y * 13), B: uint8((x ^ y) * 3), A: a})
		}
	}
	var out bytes.Buffer
	var err error
	switch mime {
	case "image/jpeg":
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 100})
	case "image/png":
		err = png.Encode(&out, img)
	case "image/gif":
		err = gif.Encode(&out, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func readyAsset(t *testing.T, s *Store, sourceURL string, content []byte, mime string) string {
	t.Helper()
	id, _, _ := mediaIdentity(sourceURL)
	if _, err := s.db.Exec(`INSERT INTO media_assets(id,source,source_url,status,created_at,updated_at) VALUES(?,'x',?,'pending','','')`, id, sourceURL); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMediaDownload(context.Background(), PendingMedia{ID: id, Source: "x", SourceURL: sourceURL}, content, mime, "", "", "ready", ""); err != nil {
		t.Fatal(err)
	}
	return id
}

func openVariantStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStandalone(filepath.Join(t.TempDir(), "database", "snapshot.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSaveMediaDownloadProbesDimensions(t *testing.T) {
	s := openVariantStore(t)
	id := readyAsset(t, s, "https://pbs.twimg.com/media/synthetic-dims.png", syntheticImage(t, "image/png", 30, 20, false), "image/png")
	var width, height sql.NullInt64
	if err := s.db.QueryRow(`SELECT width,height FROM media_assets WHERE id=?`, id).Scan(&width, &height); err != nil || width.Int64 != 30 || height.Int64 != 20 {
		t.Fatalf("dimensions not probed: %v %v %v", width, height, err)
	}
	// An unreadable header is stored as 0×0 and reads as unknown.
	bad := readyAsset(t, s, "https://pbs.twimg.com/media/synthetic-bad.png", []byte("\x89PNG\r\n\x1a\nsynthetic"), "image/png")
	if err := s.db.QueryRow(`SELECT width,height FROM media_assets WHERE id=?`, bad).Scan(&width, &height); err != nil || width.Int64 != 0 || height.Int64 != 0 {
		t.Fatalf("unreadable header stored %v %v %v", width, height, err)
	}
	reference, err := s.resolveMedia(context.Background(), strPtr("https://pbs.twimg.com/media/synthetic-bad.png"))
	if err != nil || reference.Width != nil || reference.Height != nil {
		t.Fatalf("0×0 exposed as a dimension: %#v %v", reference, err)
	}
}

func TestMeasureMediaDimensionsBackfillsOlderFiles(t *testing.T) {
	s := openVariantStore(t)
	ctx := context.Background()
	id := readyAsset(t, s, "https://pbs.twimg.com/media/synthetic-old.jpg", syntheticImage(t, "image/jpeg", 40, 10, false), "image/jpeg")
	if _, err := s.db.Exec(`UPDATE media_assets SET width=NULL,height=NULL WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	before, _ := s.GetDashboardRevision(ctx)
	measured, err := s.MeasureMediaDimensions(ctx, 10)
	if err != nil || measured != 1 {
		t.Fatalf("measured %d %v", measured, err)
	}
	var width, height int64
	if err = s.db.QueryRow(`SELECT width,height FROM media_assets WHERE id=?`, id).Scan(&width, &height); err != nil || width != 40 || height != 10 {
		t.Fatalf("backfill wrote %d×%d %v", width, height, err)
	}
	if after, _ := s.GetDashboardRevision(ctx); after == before {
		t.Fatal("backfill did not advance the revision")
	}
	if measured, err = s.MeasureMediaDimensions(ctx, 10); err != nil || measured != 0 {
		t.Fatalf("second backfill measured %d %v", measured, err)
	}
}

func TestReadMediaVariantResizesAndCaches(t *testing.T) {
	s := openVariantStore(t)
	ctx := context.Background()
	original := syntheticImage(t, "image/jpeg", 1000, 500, false)
	id := readyAsset(t, s, "https://pbs.twimg.com/media/synthetic-wide.jpg", original, "image/jpeg")
	file, err := s.ReadMediaVariant(ctx, id, "", 320)
	if err != nil || file.MIMEType != "image/jpeg" || file.Size >= int64(len(original)) {
		t.Fatalf("variant not made: %v %#v", err, file)
	}
	img, _, err := image.DecodeConfig(bytes.NewReader(file.Content))
	if err != nil || img.Width != 320 || img.Height != 160 {
		t.Fatalf("variant size %dx%d %v", img.Width, img.Height, err)
	}
	matches, _ := filepath.Glob(filepath.Join(s.dataRoot, "cache", "variants", "*", "*-320.jpg"))
	if len(matches) != 1 {
		t.Fatalf("variant not cached: %v", matches)
	}
	again, err := s.ReadMediaVariant(ctx, id, "", 320)
	if err != nil || !bytes.Equal(again.Content, file.Content) || again.SHA256 != file.SHA256 {
		t.Fatalf("cached variant differs: %v", err)
	}
	// The original stays untouched and is still served without a width.
	whole, err := s.ReadMedia(ctx, id, "")
	if err != nil || !bytes.Equal(whole.Content, original) {
		t.Fatalf("original changed: %v", err)
	}
	if _, err = s.ReadMediaVariant(ctx, id, "", 333); err != ErrMediaNotFound {
		t.Fatalf("unlisted width accepted: %v", err)
	}
	if _, err = s.ReadMediaVariant(ctx, id, "0000000000000000000000000000000000000000000000000000000000000000", 320); err != ErrMediaVersionNotFound {
		t.Fatalf("unknown version accepted: %v", err)
	}
}

func TestReadMediaVariantKeepsOriginalWhenItCannotShrink(t *testing.T) {
	s := openVariantStore(t)
	ctx := context.Background()
	cases := map[string][]byte{
		"https://pbs.twimg.com/media/synthetic-small.jpg": syntheticImage(t, "image/jpeg", 200, 100, false),
		"https://pbs.twimg.com/media/synthetic-anim.gif":  syntheticImage(t, "image/gif", 800, 400, false),
	}
	for source, content := range cases {
		mime := "image/jpeg"
		if filepath.Ext(source) == ".gif" {
			mime = "image/gif"
		}
		id := readyAsset(t, s, source, content, mime)
		file, err := s.ReadMediaVariant(ctx, id, "", 320)
		if err != nil || !bytes.Equal(file.Content, content) || file.MIMEType != mime {
			t.Fatalf("%s: original not served: %v", source, err)
		}
	}
	markers, _ := filepath.Glob(filepath.Join(s.dataRoot, "cache", "variants", "*", "*-320.orig"))
	if len(markers) != 2 {
		t.Fatalf("keep-original markers %v", markers)
	}
}

func TestReadMediaVariantKeepsTransparency(t *testing.T) {
	s := openVariantStore(t)
	original := syntheticImage(t, "image/png", 1400, 700, true)
	id := readyAsset(t, s, "https://pbs.twimg.com/media/synthetic-alpha.png", original, "image/png")
	file, err := s.ReadMediaVariant(context.Background(), id, "", 640)
	if err != nil || file.MIMEType != "image/png" {
		t.Fatalf("transparent variant: %v %#v", err, file.MIMEType)
	}
}

func TestReadMediaVariantServesOriginalWhenCacheIsReadOnly(t *testing.T) {
	s := openVariantStore(t)
	original := syntheticImage(t, "image/jpeg", 1000, 500, false)
	id := readyAsset(t, s, "https://pbs.twimg.com/media/synthetic-ro.jpg", original, "image/jpeg")
	// A file where the variant directory should be makes every write fail.
	if err := os.MkdirAll(filepath.Join(s.dataRoot, "cache"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dataRoot, "cache", "variants"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := s.ReadMediaVariant(context.Background(), id, "", 320)
	if err != nil || file.Size >= int64(len(original)) {
		t.Fatalf("unstored variant not served: %v", err)
	}
}

func TestReadMediaVariantMakesAColdCopyOnce(t *testing.T) {
	s := openVariantStore(t)
	original := syntheticImage(t, "image/jpeg", 1000, 500, false)
	id := readyAsset(t, s, "https://pbs.twimg.com/media/synthetic-cold.jpg", original, "image/jpeg")
	var resizes atomic.Int32
	started, proceed := make(chan struct{}, 8), make(chan struct{})
	t.Cleanup(func() { resizeVariant = mediavariant.Resize })
	resizeVariant = func(content []byte, width int) ([]byte, string, error) {
		resizes.Add(1)
		started <- struct{}{}
		<-proceed
		return mediavariant.Resize(content, width)
	}
	const requests = 4
	results := make(chan *MediaFile, requests)
	var wg sync.WaitGroup
	for range requests {
		wg.Go(func() {
			file, err := s.ReadMediaVariant(context.Background(), id, "", 320)
			if err != nil {
				t.Error(err)
			}
			results <- file
		})
	}
	<-started
	// A request that gives up while the copy is being made gets the original.
	cancelled, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if file, err := s.ReadMediaVariant(cancelled, id, "", 320); err != nil || !bytes.Equal(file.Content, original) {
		t.Fatalf("cancelled waiter: %v", err)
	}
	// Let the other requests queue behind the first maker before it finishes.
	time.Sleep(50 * time.Millisecond)
	close(proceed)
	wg.Wait()
	close(results)
	if got := resizes.Load(); got != 1 {
		t.Fatalf("resized %d times for one copy", got)
	}
	var first *MediaFile
	for file := range results {
		if file == nil || file.Size >= int64(len(original)) || (first != nil && file.SHA256 != first.SHA256) {
			t.Fatalf("request got %#v", file)
		}
		first = file
	}
}

// failingStat is an open copy whose metadata cannot be read.
type failingStat struct{ *bytes.Reader }

func (failingStat) Stat() (fs.FileInfo, error) { return nil, errors.New("synthetic stat failure") }

func TestReadVariantFileSkipsACopyItCannotExamine(t *testing.T) {
	content := syntheticImage(t, "image/jpeg", 8, 8, false)
	if out := readVariantFile(failingStat{bytes.NewReader(content)}, "image/jpeg"); out != nil {
		t.Fatalf("unexaminable copy served: %+v", out)
	}
	path := filepath.Join(t.TempDir(), "copy.jpg")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if out := readVariantFile(file, "image/jpeg"); out == nil || !bytes.Equal(out.Content, content) || out.MIMEType != "image/jpeg" {
		t.Fatalf("readable copy not served: %+v", out)
	}
}

func TestReadVariantCachesOnlyBytesMatchingTheHash(t *testing.T) {
	s := openVariantStore(t)
	ctx := context.Background()
	content := syntheticImage(t, "image/jpeg", 1000, 500, false)
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	load := func() (*MediaFile, error) {
		return &MediaFile{Content: content, MIMEType: "image/jpeg", SHA256: hash, Size: int64(len(content))}, nil
	}
	if _, err := s.ReadVariant(ctx, "not-a-hash", 320, load); err != ErrMediaNotFound {
		t.Fatalf("unsafe hash accepted: %v", err)
	}
	if _, err := s.ReadVariant(ctx, hash, 333, load); err != ErrMediaNotFound {
		t.Fatalf("unlisted width accepted: %v", err)
	}
	// Bytes that do not match the claimed hash are served whole and never
	// cached under it.
	other := strings.Repeat("0", 64)
	file, err := s.ReadVariant(ctx, other, 320, load)
	if err != nil || !bytes.Equal(file.Content, content) {
		t.Fatalf("mismatched bytes: %v", err)
	}
	if entries, _ := filepath.Glob(filepath.Join(s.dataRoot, "cache", "variants", "00", "*")); len(entries) != 0 {
		t.Fatalf("mismatched bytes cached: %v", entries)
	}
	file, err = s.ReadVariant(ctx, hash, 320, load)
	if err != nil || file.Size >= int64(len(content)) {
		t.Fatalf("matching bytes not resized: %v", err)
	}
	// A cached copy is found without loading the original.
	file, err = s.ReadVariant(ctx, hash, 320, func() (*MediaFile, error) { return nil, errors.New("synthetic load failure") })
	if err != nil || file.Size >= int64(len(content)) {
		t.Fatalf("cached copy needed the original: %v", err)
	}
}
