package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
)

func testPNGBytes(marker byte) ([]byte, string) {
	picture := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	picture.SetNRGBA(0, 0, color.NRGBA{R: marker, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		panic(err)
	}
	content := encoded.Bytes()
	sum := sha256.Sum256(content)
	return content, hex.EncodeToString(sum[:])
}

func TestAccountAvatarsLifecycleAndMediaDownload(t *testing.T) {
	ctx := context.Background()
	dataRoot := filepath.Join(t.TempDir(), "data")
	store, err := OpenInitialized(filepath.Join(dataRoot, "database", "snapshot.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Record first discovered URL (pending until media step downloads it).
	urlA := "https://pbs.twimg.com/profile_images/1/first_400x400.jpg"
	state, candA, err := store.RecordAccountAvatarURL(ctx, "x", "@Kano_2525", AccountAvatarOriginSync, urlA, false)
	if err != nil {
		t.Fatalf("RecordAccountAvatarURL A: %v", err)
	}
	if state.AccountHandle != "Kano_2525" || state.Mode != AccountAvatarModeAuto || candA.Status != "pending" {
		t.Fatalf("unexpected initial state=%+v cand=%+v", state, candA)
	}

	// Before download completes, public map omits unset account.
	pub, err := store.AccountAvatars(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := pub["x:kano_2525"]; exists {
		t.Fatalf("expected unset account omitted from public map, got %+v", pub)
	}

	pending, err := store.PendingMedia(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].SourceURL != urlA {
		t.Fatalf("pending = %+v", pending)
	}

	pngA, hashA := testPNGBytes('A')
	rev0, _ := store.GetDashboardRevision(ctx)
	if err = store.SaveMediaDownload(ctx, pending[0], pngA, "image/png", `"a"`, "", "ready", ""); err != nil {
		t.Fatalf("SaveMediaDownload A: %v", err)
	}
	rev1, _ := store.GetDashboardRevision(ctx)
	if rev1 <= rev0 {
		t.Fatalf("expected revision bump on auto activation, rev0=%d rev1=%d", rev0, rev1)
	}

	pub, err = store.AccountAvatars(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantURLA := "/media/account-avatar/" + candA.ID + "?v=" + hashA
	if got := pub["x:kano_2525"]; got == nil || *got != wantURLA {
		t.Fatalf("public avatar A = %v, want %q", got, wantURLA)
	}

	// Verify OpenAccountAvatarMedia streams exact bytes and checks version.
	stream, err := store.OpenAccountAvatarMedia(ctx, candA.ID, hashA)
	if err != nil {
		t.Fatalf("OpenAccountAvatarMedia: %v", err)
	}
	gotBytes, _ := io.ReadAll(stream.File)
	stream.File.Close()
	if string(gotBytes) != string(pngA) {
		t.Fatalf("stream bytes mismatch")
	}
	wrongHash := hashA[:63] + "0"
	if hashA[63] == '0' {
		wrongHash = hashA[:63] + "1"
	}
	if _, err = store.OpenAccountAvatarMedia(ctx, candA.ID, wrongHash); err != ErrMediaVersionNotFound {
		t.Fatalf("expected ErrMediaVersionNotFound on wrong hash, got %v", err)
	}

	// Lock manual on candidate A, then sync + download candidate B; A must stay active.
	locked, err := store.SelectAccountAvatarCandidate(ctx, "x", "kano_2525", candA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if locked.Mode != AccountAvatarModeManual || locked.ActiveCandidateID != candA.ID {
		t.Fatalf("locked = %+v", locked)
	}

	urlB := "https://pbs.twimg.com/profile_images/2/second_400x400.jpg"
	_, candB, err := store.RecordAccountAvatarURL(ctx, "x", "kano_2525", AccountAvatarOriginSync, urlB, false)
	if err != nil {
		t.Fatal(err)
	}
	pending, err = store.PendingMedia(ctx, 10)
	if err != nil || len(pending) != 1 || pending[0].SourceURL != urlB {
		t.Fatalf("pending B = %+v, err=%v", pending, err)
	}
	pngB, hashB := testPNGBytes('B')
	if err = store.SaveMediaDownload(ctx, pending[0], pngB, "image/png", `"b"`, "", "ready", ""); err != nil {
		t.Fatal(err)
	}

	pub, _ = store.AccountAvatars(ctx)
	if got := pub["x:kano_2525"]; got == nil || *got != wantURLA {
		t.Fatalf("manual lock broken, got %v", got)
	}

	// Hidden mode emits explicit nil entry in public map.
	hiddenState, err := store.SetAccountAvatarMode(ctx, "x", "kano_2525", AccountAvatarModeHidden)
	if err != nil {
		t.Fatal(err)
	}
	if hiddenState.Mode != AccountAvatarModeHidden {
		t.Fatalf("mode = %s", hiddenState.Mode)
	}
	pub, _ = store.AccountAvatars(ctx)
	if v, ok := pub["x:kano_2525"]; !ok || v != nil {
		t.Fatalf("expected explicit nil entry when hidden, ok=%v v=%v", ok, v)
	}

	// Switching back to auto promotes newest ready candidate (candB).
	autoState, err := store.SetAccountAvatarMode(ctx, "x", "kano_2525", AccountAvatarModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if autoState.ActiveCandidateID != candB.ID {
		t.Fatalf("expected candB=%s promoted on auto, got %s", candB.ID, autoState.ActiveCandidateID)
	}
	wantURLB := "/media/account-avatar/" + candB.ID + "?v=" + hashB
	pub, _ = store.AccountAvatars(ctx)
	if got := pub["x:kano_2525"]; got == nil || *got != wantURLB {
		t.Fatalf("public avatar B = %v, want %q", got, wantURLB)
	}

	// Upload candidate saves under data/avatar/... and activates in manual mode.
	pngU, hashU := testPNGBytes('U')
	relUpload := "avatar/sha256/" + hashU + ".png"
	absUpload := filepath.Join(dataRoot, filepath.FromSlash(relUpload))
	if err = os.MkdirAll(filepath.Dir(absUpload), 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(absUpload, pngU, 0o600); err != nil {
		t.Fatal(err)
	}
	upState, candU, err := store.SaveUploadedAccountAvatar(ctx, "x", "_Kanotic", relUpload, "image/png", int64(len(pngU)), hashU, 400, 400, true)
	if err != nil {
		t.Fatalf("SaveUploadedAccountAvatar: %v", err)
	}
	if upState.Mode != AccountAvatarModeManual || upState.ActiveCandidateID != candU.ID {
		t.Fatalf("upload state = %+v", upState)
	}
	streamU, err := store.OpenAccountAvatarMedia(ctx, candU.ID, hashU)
	if err != nil {
		t.Fatalf("OpenAccountAvatarMedia upload: %v", err)
	}
	streamU.File.Close()

	// Deleting active candidate in auto mode falls back to candA.
	afterDelete, err := store.DeleteAccountAvatarCandidate(ctx, "x", "kano_2525", candB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterDelete.ActiveCandidateID != candA.ID {
		t.Fatalf("expected fallback to candA=%s after deleting candB, got %s", candA.ID, afterDelete.ActiveCandidateID)
	}
}

func TestXSyncAndRelatedRegisterAccountAvatars(t *testing.T) {
	ctx := context.Background()
	dataRoot := filepath.Join(t.TempDir(), "data")
	store, err := OpenInitialized(filepath.Join(dataRoot, "database", "snapshot.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ownURL := "https://pbs.twimg.com/profile_images/10/own_400x400.jpg"
	relURL := "https://pbs.twimg.com/profile_images/20/related_400x400.jpg"
	res := xsource.Result{
		Account: "kano_2525",
		Posts: []xsource.Post{{
			ID:               "1900000000000000910",
			AccountHandle:    "kano_2525",
			Type:             "daily",
			Label:            xsource.LabelDaily,
			Text:             "hello",
			PublishedAt:      "2026-10-20T00:00:00Z",
			URL:              "https://x.com/kano_2525/status/1900000000000000910",
			AuthorAvatarURL:  ownURL,
			AuthorScreenName: "kano_2525",
		}},
		AccountAvatars: map[string]string{
			"kano_2525": ownURL,
			"partner_x": relURL,
		},
	}
	if err = store.WriteXRecent(ctx, "kano_2525", res, true); err != nil {
		t.Fatalf("WriteXRecent: %v", err)
	}
	if _, err = store.WriteXRelated(ctx, "partner_x", xsource.Result{
		Account:        "partner_x",
		AccountAvatars: map[string]string{"partner_x": relURL},
	}, true); err != nil {
		t.Fatalf("WriteXRelated: %v", err)
	}

	items, err := store.ListAccountAvatars(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 synced account avatar rows, got %d (%+v)", len(items), items)
	}
	pending, err := store.PendingMedia(ctx, 10)
	if err != nil || len(pending) != 2 {
		t.Fatalf("expected 2 pending media downloads for avatars, got %+v, err=%v", pending, err)
	}
}

func TestAccountAvatarAutoRevertsToEarlierReadyCandidate(t *testing.T) {
	ctx := context.Background()
	dataRoot := filepath.Join(t.TempDir(), "data")
	store, err := OpenInitialized(filepath.Join(dataRoot, "database", "snapshot.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	urlA := "https://pbs.twimg.com/profile_images/100/avatar_a_400x400.png"
	urlB := "https://pbs.twimg.com/profile_images/200/avatar_b_400x400.png"
	pngA, _ := testPNGBytes('A')
	pngB, _ := testPNGBytes('B')

	// Sync A and finish downloading A -> active candidate is A.
	stateA, candA, err := store.RecordAccountAvatarURL(ctx, "x", "kano_2525", AccountAvatarOriginSync, urlA, false)
	if err != nil {
		t.Fatalf("record A: %v", err)
	}
	pending, err := store.PendingMedia(ctx, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending A = %+v err=%v", pending, err)
	}
	if err = store.SaveMediaDownload(ctx, pending[0], pngA, "image/png", `"a"`, "", "ready", ""); err != nil {
		t.Fatalf("save A: %v", err)
	}

	// Sync B and finish downloading B -> auto switches active candidate to B.
	_, candB, err := store.RecordAccountAvatarURL(ctx, "x", "kano_2525", AccountAvatarOriginSync, urlB, false)
	if err != nil {
		t.Fatalf("record B: %v", err)
	}
	pending, err = store.PendingMedia(ctx, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending B = %+v err=%v", pending, err)
	}
	if err = store.SaveMediaDownload(ctx, pending[0], pngB, "image/png", `"b"`, "", "ready", ""); err != nil {
		t.Fatalf("save B: %v", err)
	}

	gotB, err := store.GetAccountAvatar(ctx, "x", "kano_2525")
	if err != nil || gotB.ActiveCandidateID != candB.ID || stateA.Mode != AccountAvatarModeAuto {
		t.Fatalf("expected active candidate B (%s), got %+v err=%v", candB.ID, gotB, err)
	}

	// Re-observe already-ready A via X sync (A -> B -> A): auto mode must revert back to A.
	revBefore, _ := store.GetDashboardRevision(ctx)
	if err = store.WriteXRecent(ctx, "kano_2525", xsource.Result{
		Account:        "kano_2525",
		AccountAvatars: map[string]string{"kano_2525": urlA},
	}, true); err != nil {
		t.Fatalf("WriteXRecent A revisit: %v", err)
	}
	gotA, err := store.GetAccountAvatar(ctx, "x", "kano_2525")
	if err != nil || gotA.ActiveCandidateID != candA.ID {
		t.Fatalf("expected auto mode to revert active candidate back to A (%s), got %+v err=%v", candA.ID, gotA, err)
	}
	revAfter, _ := store.GetDashboardRevision(ctx)
	if revAfter <= revBefore {
		t.Fatalf("expected revision bump reverting A->B->A, before=%d after=%d", revBefore, revAfter)
	}
}
