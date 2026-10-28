package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultListenAddress = ":7658"
	DefaultDatabasePath  = "data/database/kano.sqlite"
	// StandalonePort is the product port when this process owns the site.
	StandalonePort         = 7657
	DefaultStaticDirectory = "dist"
)

// Config contains only process-local settings for the read-only Go service.
// The database path is deliberately a command-line option rather than a new
// shared production environment variable.
type Config struct {
	ListenAddress        string
	DatabasePath         string
	ReadHeaderTimeout    time.Duration
	ReadTimeout          time.Duration
	WriteTimeout         time.Duration
	IdleTimeout          time.Duration
	ShutdownTimeout      time.Duration
	EnableAdminWrites    bool
	EnableXSync          bool
	EnableYoutubeSync    bool
	EnableMediaDownloads bool
	EnableScheduler      bool
	Initialize           bool
	// Standalone makes this process the single owner of the site: writable
	// snapshot, full operator console, workflows, MCP and the built frontend.
	Standalone      bool
	StaticDirectory string
	// PublicOrigin is the site's public origin from PUBLIC_ORIGIN. It only
	// makes the share tags in index.html absolute; empty leaves them relative.
	PublicOrigin string
	// HealthCheck probes a running standalone process and exits; the scratch
	// image has no shell or HTTP client for a container health check.
	HealthCheck bool
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

func standalonePort(raw string) int {
	port, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || port < 1 || port > 65535 {
		return StandalonePort
	}
	return port
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
	flags.BoolVar(&c.EnableYoutubeSync, "enable-youtube-sync", false, "enable development-only YouTube recent source writes")
	flags.BoolVar(&c.EnableMediaDownloads, "enable-media-downloads", false, "enable development-only bounded media downloads")
	flags.BoolVar(&c.EnableScheduler, "enable-scheduler", false, "enable development-only Go source schedule timers")
	flags.BoolVar(&c.Initialize, "initialize", false, "initialize and seed a development snapshot with Go")
	flags.BoolVar(&c.Standalone, "standalone", false, "serve the whole site from this process (API, admin, workflows, MCP, frontend)")
	flags.StringVar(&c.StaticDirectory, "static", DefaultStaticDirectory, "built frontend directory served in standalone mode")
	flags.BoolVar(&c.HealthCheck, "healthcheck", false, "probe the local standalone service and exit")
	if err := flags.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return Config{}, fmt.Errorf("unexpected positional arguments")
	}
	if c.Standalone || c.HealthCheck {
		explicit := map[string]bool{}
		flags.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
		if !explicit["listen"] {
			c.ListenAddress = ":" + strconv.Itoa(standalonePort(os.Getenv("PORT")))
		}
		// Operator requests may wait on a provider test, a profile download
		// or a 15 MiB upload, which the read-only mode's bounds would cut.
		if !explicit["read-timeout"] {
			c.ReadTimeout = 60 * time.Second
		}
		if !explicit["write-timeout"] {
			c.WriteTimeout = 150 * time.Second
		}
	}
	origin, err := PublicOrigin(os.Getenv("PUBLIC_ORIGIN"))
	if err != nil {
		return Config{}, err
	}
	c.PublicOrigin = origin
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// PublicOrigin normalizes PUBLIC_ORIGIN to scheme://host[:port] with a
// lowercase host. An empty value is allowed; anything that is not an HTTP(S)
// origin whose host is a DNS name or IP address fails startup, so the value
// can never carry markup into the pages and feeds that print it.
func PublicOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	invalid := errors.New("PUBLIC_ORIGIN must be an http or https origin")
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.HasSuffix(parsed.Host, ":") {
		return "", invalid
	}
	host := strings.ToLower(parsed.Hostname())
	if ip := net.ParseIP(host); ip != nil {
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
	} else if !dnsName(host) {
		return "", invalid
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 || strconv.Itoa(number) != port {
			return "", invalid
		}
		host += ":" + port
	}
	return parsed.Scheme + "://" + host, nil
}

// dnsName reports whether host is a hostname of letters, digits and inner
// hyphens in dot-separated labels of at most 63 characters.
func dnsName(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}
