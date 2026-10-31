// Package secretstore encrypts provider credentials for storage in SQLite.
// The format is shared with server/secret-store.ts so either backend can read
// rows written by the other: v1:<nonce>:<tag>:<ciphertext>, each part unpadded
// base64url, AES-256-GCM with a SHA-256 derived key.
package secretstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const version = "v1"

var (
	ErrKeyRequired = errors.New("LLM_SECRETS_KEY is required to store a secret")
	ErrKeyMissing  = errors.New("LLM_SECRETS_KEY is required to read a secret")
	ErrFormat      = errors.New("stored LLM secret has an invalid format")
	ErrDecrypt     = errors.New("stored LLM secret could not be decrypted")
)

func gcm(secret string) (cipher.AEAD, bool) {
	if secret == "" {
		return nil, false
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, false
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, false
	}
	return aead, true
}

// Encrypt returns "" for an empty value, which callers store as NULL.
func Encrypt(value, secret string) (string, error) {
	if value == "" {
		return "", nil
	}
	aead, ok := gcm(secret)
	if !ok {
		return "", ErrKeyRequired
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", ErrKeyRequired
	}
	sealed := aead.Seal(nil, nonce, []byte(value), nil)
	split := len(sealed) - aead.Overhead()
	encode := base64.RawURLEncoding.EncodeToString
	return strings.Join([]string{version, encode(nonce), encode(sealed[split:]), encode(sealed[:split])}, ":"), nil
}

// Decrypt returns "" for an empty stored value.
func Decrypt(value, secret string) (string, error) {
	if value == "" {
		return "", nil
	}
	aead, ok := gcm(secret)
	if !ok {
		return "", ErrKeyMissing
	}
	parts := strings.Split(value, ":")
	if len(parts) < 4 || parts[0] != version || parts[1] == "" || parts[2] == "" || parts[3] == "" {
		return "", ErrFormat
	}
	decode := base64.RawURLEncoding.DecodeString
	nonce, err1 := decode(parts[1])
	tag, err2 := decode(parts[2])
	ciphertext, err3 := decode(parts[3])
	if err1 != nil || err2 != nil || err3 != nil || len(nonce) != aead.NonceSize() || len(tag) != aead.Overhead() {
		return "", ErrDecrypt
	}
	plaintext, err := aead.Open(nil, nonce, append(ciphertext, tag...), nil)
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plaintext), nil
}
