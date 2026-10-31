package llmadmin

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yexca/kano-navi/backend/internal/secretstore"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

func TestMigrateProviderKeySealsVerifiesAndProtectsStoredKey(t *testing.T) {
	store, err := sqlite.OpenStandalone(filepath.Join(t.TempDir(), "database", "kano.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	migrate := func(key string, force bool) error {
		return MigrateProviderKey(ctx, store, LegacyProviderID, key, testSecretsKey, force)
	}
	if err = migrate(testAPIKey, false); !errors.Is(err, ErrMigrationProviderMissing) {
		t.Fatalf("missing provider err=%v", err)
	}
	if _, err = store.DB().Exec(`INSERT INTO llm_providers(id,name,base_url,model,created_at,updated_at) VALUES(?,'Synthetic',?,'synthetic-model','2026-03-01T00:00:00.000Z','2026-03-01T00:00:00.000Z')`, LegacyProviderID, testBaseURL); err != nil {
		t.Fatal(err)
	}
	if err = MigrateProviderKey(ctx, store, LegacyProviderID, testAPIKey, "", false); !errors.Is(err, secretstore.ErrKeyRequired) {
		t.Fatalf("missing secrets key err=%v", err)
	}
	if err = migrate(testAPIKey, false); err != nil {
		t.Fatal(err)
	}
	var ciphertext string
	if err = store.DB().QueryRow(`SELECT api_key_ciphertext FROM llm_providers WHERE id=?`, LegacyProviderID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ciphertext, testAPIKey) || !strings.HasPrefix(ciphertext, "v1:") {
		t.Fatal("the stored value is not sealed")
	}
	if stored, _ := ProviderAPIKey(ctx, store.DB(), LegacyProviderID, testSecretsKey); stored != testAPIKey {
		t.Fatal("the stored key does not decrypt to the migrated value")
	}
	// An existing key is replaced only on request.
	if err = migrate("synthetic-replacement-credential", false); !errors.Is(err, ErrMigrationKeyExists) {
		t.Fatalf("existing key err=%v", err)
	}
	if stored, _ := ProviderAPIKey(ctx, store.DB(), LegacyProviderID, testSecretsKey); stored != testAPIKey {
		t.Fatal("a refused migration changed the stored key")
	}
	if err = migrate("synthetic-replacement-credential", true); err != nil {
		t.Fatal(err)
	}
	if stored, _ := ProviderAPIKey(ctx, store.DB(), LegacyProviderID, testSecretsKey); stored != "synthetic-replacement-credential" {
		t.Fatal("--force did not replace the stored key")
	}
}
