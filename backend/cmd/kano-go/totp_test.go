package main

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/yexca/kano-navi/backend/internal/adminauth"
)

func TestTOTPSecretCommandPrintsASecretAndItsAddress(t *testing.T) {
	var out bytes.Buffer
	if code := runTOTPSecret(nil, &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	match := regexp.MustCompile(`^ADMIN_TOTP_SECRET=([A-Z2-7]{32})\n# (otpauth://totp/\S+)\n$`).FindStringSubmatch(out.String())
	if match == nil {
		t.Fatalf("output=%q", out.String())
	}
	if secret, err := adminauth.ParseTOTPSecret(match[1]); err != nil || len(secret) != 20 {
		t.Fatalf("secret does not parse: %v", err)
	}
	if match[2] != adminauth.TOTPURI(match[1]) {
		t.Fatalf("address=%q", match[2])
	}
	if code := runTOTPSecret([]string{"-x"}, &out); code != 2 {
		t.Fatalf("argument exit %d", code)
	}
}
