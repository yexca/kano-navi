package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	DefaultListenAddress = ":7658"
	DefaultDatabasePath  = "data/database/kano.sqlite"
)

// Config contains only process-local settings for the read-only Go service.
// The database path is deliberately a command-line option rather than a new
// shared production environment variable.
type Config struct {
	ListenAddress     string
	DatabasePath      string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	EnableAdminWrites bool
	EnableXSync       bool
}

func Default() Config {
	return Config{
		ListenAddress:     DefaultListenAddress,
		DatabasePath:      DefaultDatabasePath,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   10 * time.Second,
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.ListenAddress) == "" {
		return errors.New("listen address is required")
	}
	if strings.TrimSpace(c.DatabasePath) == "" {
		return errors.New("database path is required")
	}
	if c.ReadHeaderTimeout <= 0 || c.ReadTimeout <= 0 || c.WriteTimeout <= 0 || c.IdleTimeout <= 0 || c.ShutdownTimeout <= 0 {
		return errors.New("timeouts must be positive")
	}
	return nil
}

func Parse(args []string) (Config, error) {
	c := Default()
	flags := flag.NewFlagSet("kano-go", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&c.ListenAddress, "listen", c.ListenAddress, "HTTP listen address")
	flags.StringVar(&c.DatabasePath, "database", c.DatabasePath, "path to the existing SQLite snapshot")
	flags.DurationVar(&c.ReadHeaderTimeout, "read-header-timeout", c.ReadHeaderTimeout, "maximum time to read request headers")
	flags.DurationVar(&c.ReadTimeout, "read-timeout", c.ReadTimeout, "maximum request read time")
	flags.DurationVar(&c.WriteTimeout, "write-timeout", c.WriteTimeout, "maximum response write time")
	flags.DurationVar(&c.IdleTimeout, "idle-timeout", c.IdleTimeout, "maximum keep-alive idle time")
	flags.DurationVar(&c.ShutdownTimeout, "shutdown-timeout", c.ShutdownTimeout, "graceful shutdown deadline")
	flags.BoolVar(&c.EnableAdminWrites, "enable-admin-writes", false, "enable development-only admin event writes")
	flags.BoolVar(&c.EnableXSync, "enable-x-sync", false, "enable development-only X recent source writes")
	if err := flags.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return Config{}, fmt.Errorf("unexpected positional arguments")
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}
