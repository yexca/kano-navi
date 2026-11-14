package adminauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"
)

// The one-time code is RFC 6238 TOTP with the parameters every authenticator
// app supports by default: HMAC-SHA1, six digits and a 30-second step. A code
// from the step before or after the server's is accepted for clock skew.
const (
	totpPeriod      = 30
	totpDigits      = 6
	totpSkew        = 1
	totpMinBytes    = 16
	totpSecretBytes = 20
	// TOTPIssuer names the console in an authenticator app.
	TOTPIssuer = "Kano Navi"
)

// ErrTOTPSecret reports a malformed ADMIN_TOTP_SECRET.
var ErrTOTPSecret = errors.New("ADMIN_TOTP_SECRET must be base32 of at least 16 bytes")

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// ParseTOTPSecret decodes a base32 secret as authenticator apps show it:
// letters in either case, spaces, hyphens and padding ignored. Only an empty
// or whitespace-only value means one-time codes are off; any other value
// that is not a secret, such as padding or separators alone, is malformed.
func ParseTOTPSecret(value string) ([]byte, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	cleaned := strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "=", "").Replace(trimmed))
	secret, err := totpEncoding.DecodeString(cleaned)
	if err != nil || len(secret) < totpMinBytes {
		return nil, ErrTOTPSecret
	}
	return secret, nil
}

// GenerateTOTPSecret returns a new 160-bit secret in unpadded base32, the
// size RFC 4226 recommends. A nil reader uses crypto/rand.
func GenerateTOTPSecret(random io.Reader) (string, error) {
	if random == nil {
		random = rand.Reader
	}
	secret := make([]byte, totpSecretBytes)
	if _, err := io.ReadFull(random, secret); err != nil {
		return "", err
	}
	return totpEncoding.EncodeToString(secret), nil
}

// TOTPURI is the otpauth:// address that password managers and authenticator
// apps import, for the encoded secret.
func TOTPURI(secret string) string {
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", TOTPIssuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", "6")
	query.Set("period", "30")
	// Spaces are written as %20, which every importer reads; some read a
	// query's "+" literally.
	return "otpauth://totp/" + url.PathEscape(TOTPIssuer+":admin") + "?" + strings.ReplaceAll(query.Encode(), "+", "%20")
}

// totpCode is the code of one time step (RFC 4226 dynamic truncation).
func totpCode(secret []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	code := make([]byte, totpDigits)
	for i := totpDigits - 1; i >= 0; i-- {
		code[i] = byte('0' + value%10)
		value /= 10
	}
	return string(code)
}

// totpStep is the time step a moment falls in.
func totpStep(now time.Time) int64 { return now.Unix() / totpPeriod }

// normalizeCode keeps a code of exactly six digits, allowing the space or
// hyphen some apps show in its middle; anything else is "".
func normalizeCode(code string) string {
	cleaned := strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(code))
	if len(cleaned) != totpDigits {
		return ""
	}
	for _, r := range cleaned {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return cleaned
}

// matchTOTP returns the step whose code equals code within the skew window,
// checking every step so the time taken does not depend on which matched.
func matchTOTP(secret []byte, code string, now time.Time) (int64, bool) {
	code = normalizeCode(code)
	current := totpStep(now)
	matched, ok := int64(0), false
	for step := current - totpSkew; step <= current+totpSkew; step++ {
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, step)), []byte(code)) == 1 && code != "" {
			matched, ok = step, true
		}
	}
	return matched, ok
}
