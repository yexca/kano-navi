package main

import (
	"fmt"
	"io"

	"github.com/yexca/kano-navi/backend/internal/adminauth"
)

// runTOTPSecret prints a new one-time code secret as an environment line and
// its otpauth:// address as a comment, for a password manager or
// authenticator app to import. It reads no environment and stores nothing.
func runTOTPSecret(args []string, out io.Writer) int {
	if len(args) != 0 {
		return fail("totp-secret takes no arguments", 2)
	}
	secret, err := adminauth.GenerateTOTPSecret(nil)
	if err != nil {
		return fail("secret could not be generated", 1)
	}
	fmt.Fprintf(out, "ADMIN_TOTP_SECRET=%s\n# %s\n", secret, adminauth.TOTPURI(secret))
	return 0
}
