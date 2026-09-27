// Package serverconfig loads and validates the `weavster server`
// configuration file (spec §4.1–§4.3).
package serverconfig

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Store dialects accepted by store.dialect (spec §4.2, §11).
const (
	DialectMemory   = "memory"
	DialectSQLite   = "sqlite"
	DialectPostgres = "postgres"
	DialectDisabled = "disabled"
)

// Config is the server configuration file.
type Config struct {
	Listen   Listen   `yaml:"listen"`
	TLS      TLS      `yaml:"tls"`
	Store    Store    `yaml:"store"`
	Paths    Paths    `yaml:"paths"`
	Auth     Auth     `yaml:"auth"`
	Delivery Delivery `yaml:"delivery"`
	Flows    Flows    `yaml:"flows"`
	Stats    Stats    `yaml:"stats"`
}

// Stats configures time-series statistics (spec §2.11.37): every flow's
// lifetime counters are sampled every SampleIntervalMs and kept for
// RetentionHours.
type Stats struct {
	SampleIntervalMs int `yaml:"sampleIntervalMs"`
	RetentionHours   int `yaml:"retentionHours"`
}

// MaxStatsSamples bounds the samples kept per flow.
const MaxStatsSamples = 100000

// Flows configures flow handling at startup (spec §4.5).
type Flows struct {
	// DeployOnStartup deploys and starts every enabled, undeployed flow when
	// the server starts.
	DeployOnStartup bool `yaml:"deployOnStartup"`
}

// Delivery configures delivery retries (spec §2.5).
type Delivery struct {
	MaxAttempts     int `yaml:"maxAttempts"`
	BackoffBaseMs   int `yaml:"backoffBaseMs"`
	RetryIntervalMs int `yaml:"retryIntervalMs"`
}

// Listen configures the cleartext and TLS listeners.
type Listen struct {
	Address             string `yaml:"address"`
	TLSAddress          string `yaml:"tlsAddress"`
	RequireMarkerHeader bool   `yaml:"requireMarkerHeader"`
	// ShutdownTimeoutMs bounds how long a stop signal waits for in-flight
	// requests.
	ShutdownTimeoutMs int `yaml:"shutdownTimeoutMs"`
}

// TLS configures the HTTPS listener's certificate and protocol floor.
type TLS struct {
	CertFile   string `yaml:"certFile"`
	KeyFile    string `yaml:"keyFile"`
	MinVersion string `yaml:"minVersion"`
}

// Store selects and connects the message store.
type Store struct {
	Dialect        string `yaml:"dialect"`
	DSN            string `yaml:"dsn"`
	MaxConnections int    `yaml:"maxConnections"`
	MaxRetry       int    `yaml:"maxRetry"`
	RetryWaitMs    int    `yaml:"retryWaitMs"`
}

// Paths configures on-disk locations.
type Paths struct {
	DataDir string `yaml:"dataDir"`
}

// Auth configures the local password and lockout policy (spec §4.4).
type Auth struct {
	PasswordPolicy PasswordPolicy `yaml:"passwordPolicy"`
	Lockout        Lockout        `yaml:"lockout"`
}

// PasswordPolicy sets required character-class counts.
type PasswordPolicy struct {
	MinLength  int `yaml:"minLength"`
	MinUpper   int `yaml:"minUpper"`
	MinLower   int `yaml:"minLower"`
	MinNumeric int `yaml:"minNumeric"`
	MinSpecial int `yaml:"minSpecial"`
}

// Lockout sets the failed-login threshold and lockout duration.
type Lockout struct {
	RetryLimit           int `yaml:"retryLimit"`
	LockoutPeriodSeconds int `yaml:"lockoutPeriodSeconds"`
}

// Default returns the configuration used when no file is given.
func Default() Config {
	return Config{
		Listen: Listen{Address: "127.0.0.1:8080", RequireMarkerHeader: true, ShutdownTimeoutMs: 10000},
		TLS:    TLS{MinVersion: "1.2"},
		Store:  Store{Dialect: DialectMemory, MaxConnections: 10, MaxRetry: 3, RetryWaitMs: 1000},
		Paths:  Paths{},
		Auth: Auth{
			PasswordPolicy: PasswordPolicy{MinLength: 8, MinUpper: 1, MinLower: 1, MinNumeric: 1},
			Lockout:        Lockout{RetryLimit: 5, LockoutPeriodSeconds: 300},
		},
		Delivery: Delivery{MaxAttempts: 5, BackoffBaseMs: 1000, RetryIntervalMs: 1000},
		Flows:    Flows{DeployOnStartup: true},
		Stats:    Stats{SampleIntervalMs: 60000, RetentionHours: 24},
	}
}

// Load reads path over the defaults and rejects unknown keys. Callers run
// Validate after applying any overrides.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("config: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return cfg, fmt.Errorf("config: %s: %w", path, err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return cfg, fmt.Errorf("config: %s: must contain exactly one YAML document", path)
	}
	return cfg, nil
}

// StoreDSN returns store.dsn, or for the sqlite dialect without one,
// <paths.dataDir>/weavster.db.
func (c Config) StoreDSN() string {
	if c.Store.Dialect == DialectSQLite && c.Store.DSN == "" && c.Paths.DataDir != "" {
		return filepath.Join(c.Paths.DataDir, "weavster.db")
	}
	return c.Store.DSN
}

// Validate reports the first invalid value or combination.
func (c Config) Validate() error {
	if c.Listen.Address == "" && c.Listen.TLSAddress == "" {
		return errors.New("config: listen.address or listen.tlsAddress is required")
	}
	for key, addr := range map[string]string{"listen.address": c.Listen.Address, "listen.tlsAddress": c.Listen.TLSAddress} {
		if addr == "" {
			continue
		}
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("config: %s must be host:port, got %q", key, addr)
		}
		// A fixed port, so the reported listeners (ports-in-use) are exact.
		if p, err := net.LookupPort("tcp", port); err != nil || p == 0 {
			return fmt.Errorf("config: %s needs a port from 1 to 65535 or a service name, got %q", key, addr)
		}
	}
	if c.Listen.TLSAddress != "" && (c.TLS.CertFile == "" || c.TLS.KeyFile == "") {
		return errors.New("config: listen.tlsAddress requires tls.certFile and tls.keyFile")
	}
	if c.TLS.MinVersion != "1.2" && c.TLS.MinVersion != "1.3" {
		return fmt.Errorf("config: tls.minVersion must be 1.2 or 1.3, got %q", c.TLS.MinVersion)
	}
	switch c.Store.Dialect {
	case DialectMemory, DialectDisabled:
	case DialectSQLite:
		if c.StoreDSN() == "" {
			return errors.New("config: store.dsn or paths.dataDir is required for the sqlite dialect")
		}
	case DialectPostgres:
		if c.Store.DSN == "" {
			return errors.New("config: store.dsn is required for the postgres dialect")
		}
	default:
		return fmt.Errorf("config: store.dialect must be memory, sqlite, postgres, or disabled, got %q", c.Store.Dialect)
	}
	if c.Store.MaxConnections < 1 {
		return errors.New("config: store.maxConnections must be >= 1")
	}
	if c.Store.MaxRetry < 0 || c.Store.RetryWaitMs < 0 {
		return errors.New("config: store.maxRetry and store.retryWaitMs must be >= 0")
	}
	if c.Listen.ShutdownTimeoutMs < 1 || c.Listen.ShutdownTimeoutMs > 600000 {
		return errors.New("config: listen.shutdownTimeoutMs must be between 1 and 600000 (ten minutes)")
	}
	if d := c.Delivery; d.MaxAttempts < 1 || d.MaxAttempts > 1000 {
		return errors.New("config: delivery.maxAttempts must be between 1 and 1000")
	}
	for key, v := range map[string]int{"delivery.backoffBaseMs": c.Delivery.BackoffBaseMs, "delivery.retryIntervalMs": c.Delivery.RetryIntervalMs} {
		if v < 1 || v > 3600000 {
			return fmt.Errorf("config: %s must be between 1 and 3600000 (one hour)", key)
		}
	}
	if st := c.Stats; st.SampleIntervalMs < 100 || st.SampleIntervalMs > 3600000 {
		return errors.New("config: stats.sampleIntervalMs must be between 100 and 3600000 (one hour)")
	}
	if st := c.Stats; st.RetentionHours < 1 || st.RetentionHours > 8760 {
		return errors.New("config: stats.retentionHours must be between 1 and 8760 (one year)")
	} else if int64(st.RetentionHours)*3600000/int64(st.SampleIntervalMs) > MaxStatsSamples {
		return fmt.Errorf("config: stats.retentionHours / stats.sampleIntervalMs keeps more than %d samples per flow", MaxStatsSamples)
	}
	p := c.Auth.PasswordPolicy
	for _, v := range []int{p.MinLength, c.Auth.Lockout.RetryLimit, c.Auth.Lockout.LockoutPeriodSeconds} {
		if v < 0 {
			return errors.New("config: auth.passwordPolicy.minLength and auth.lockout values must be >= 0")
		}
	}
	forbidden := 0
	for _, v := range []int{p.MinUpper, p.MinLower, p.MinNumeric, p.MinSpecial} {
		if v < -1 {
			return errors.New("config: auth.passwordPolicy character-class counts must be >= -1 (-1 forbids the class)")
		}
		if v == -1 {
			forbidden++
		}
	}
	if forbidden == 4 {
		return errors.New("config: auth.passwordPolicy forbids every character class, so no password can satisfy it")
	}
	return nil
}
