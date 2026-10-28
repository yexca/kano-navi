package llmadmin

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/secretstore"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// LegacyProviderID is the provider whose key `kano-go migrate-llm` seals from
// OPENAI_API_KEY.
const LegacyProviderID = "openai-default"

var (
	ErrMigrationProviderMissing = errors.New("create the openai-default provider in /admin before migrating its key")
	ErrMigrationKeyExists       = errors.New("openai-default already has a stored key; use --force to replace it")
	ErrMigrationUnverified      = errors.New("migrated key verification failed")
)

// MigrateProviderKey seals an environment-supplied key as the stored
// credential of an existing provider and verifies that it decrypts again. The
// change commits only after verification. plaintext never enters an error.
func MigrateProviderKey(ctx context.Context, store *sqlite.Store, providerID, plaintext, secretsKey string, force bool) error {
	if !store.Writable() {
		return sqlite.ErrReadOnly
	}
	ciphertext, err := secretstore.Encrypt(plaintext, secretsKey)
	if err != nil || ciphertext == "" {
		return secretstore.ErrKeyRequired
	}
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT api_key_ciphertext FROM llm_providers WHERE id = ?`, providerID).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMigrationProviderMissing
	}
	if err != nil {
		return errors.New("llm provider credential lookup failed")
	}
	if existing.String != "" && !force {
		return ErrMigrationKeyExists
	}
	if _, err = tx.ExecContext(ctx, `UPDATE llm_providers SET api_key_ciphertext = ?, updated_at = ? WHERE id = ?`, ciphertext, sqlite.ISOTime(time.Now()), providerID); err != nil {
		return errors.New("llm provider credential could not be stored")
	}
	stored, err := ProviderAPIKey(ctx, tx, providerID, secretsKey)
	if err != nil || stored != plaintext {
		return ErrMigrationUnverified
	}
	return tx.Commit()
}
