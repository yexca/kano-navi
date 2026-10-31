package config

import (
	"testing"
	"time"
)

func TestParseDefaultsAndOverrides(t *testing.T) {
	c, err := Parse([]string{"-listen", "127.0.0.1:8765", "-database", "snapshot.sqlite", "-read-timeout", "2s"})
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if c.ListenAddress != "127.0.0.1:8765" || c.DatabasePath != "snapshot.sqlite" || c.ReadTimeout != 2*time.Second {
		t.Fatalf("unexpected config: %+v", c)
	}
}

func TestParseRejectsInvalidTimeout(t *testing.T) {
	if _, err := Parse([]string{"-read-timeout", "0"}); err == nil {
		t.Fatal("expected invalid timeout error")
	}
}
