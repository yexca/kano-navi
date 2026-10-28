package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheFileExistsKeepsMediaInsideTheDataRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "data")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{filepath.Join(root, "x", "sha256", "ab"), outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("synthetic"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "x", "sha256", "ab", "inside.jpg"))
	write(filepath.Join(outside, "secret.jpg"))
	cases := map[string]bool{
		"x/sha256/ab/inside.jpg":             true,
		"x/sha256/ab/missing.jpg":            false,
		"x/sha256/ab":                        false,
		"../outside/secret.jpg":              false,
		"x/../../outside/secret.jpg":         false,
		filepath.Join(outside, "secret.jpg"): false,
	}
	// Links are optional on the host: Windows needs a privilege for them.
	if os.Symlink(outside, filepath.Join(root, "x", "escape")) == nil {
		cases["x/escape/secret.jpg"] = false
	}
	if os.Symlink(filepath.Join(root, "x", "sha256", "ab", "inside.jpg"), filepath.Join(root, "x", "inner-link.jpg")) == nil {
		cases["x/inner-link.jpg"] = true
	}
	if os.Symlink(filepath.Join(outside, "secret.jpg"), filepath.Join(root, "x", "outer-link.jpg")) == nil {
		cases["x/outer-link.jpg"] = false
	}
	store := &Store{dataRoot: root}
	shared := withReadMemo(context.Background())
	for name, want := range cases {
		for label, ctx := range map[string]context.Context{"plain": context.Background(), "memo": withReadMemo(context.Background()), "shared": shared} {
			if got := store.cacheFileExists(ctx, name); got != want {
				t.Errorf("%s %q: got %v, want %v", label, name, got, want)
			}
		}
	}
	// A directory checked once stays answered from the read's memo.
	for name, want := range cases {
		if got := store.cacheFileExists(shared, name); got != want {
			t.Errorf("repeat %q: got %v, want %v", name, got, want)
		}
	}
}
