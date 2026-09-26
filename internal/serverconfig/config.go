// Package serverconfig loads and validates the `weavster server`
// configuration file (spec §4.1–§4.3).
package serverconfig

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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
	Listen Listen `yaml:"listen"`
	TLS    TLS    `yaml:"tls"`
	Store  Store  `yaml:"store"`
	Paths  Paths  `yaml:"paths"`
	Auth   Auth   `yaml:"auth"`
}

// Listen configures the cleartext and TLS listeners.
type Listen struct {
	Address             string `yaml:"address"`
	TLSAddress          string `yaml:"tlsAddress"`
	RequireMarkerHeader bool   `yaml:"requireMarkerHeader"`
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
		Listen: Listen{Address: "127.0.0.1:8080", RequireMarkerHeader: true},
		TLS:    TLS{MinVersion: "1.2"},
		Store:  Store{Dialect: DialectMemory, MaxConnections: 10, MaxRetry: 3, RetryWaitMs: 1000},
		Paths:  Paths{DataDir: "data"},
		Auth: Auth{
			PasswordPolicy: PasswordPolicy{MinLength: 8, MinUpper: 1, MinLower: 1, MinNumeric: 1},
			Lockout:        Lockout{RetryLimit: 5, LockoutPeriodSeconds: 300},
		},
	}
}

// Load reads path over the defaults, rejecting unknown keys, and validates
// the result.
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
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Validate reports the first invalid value or combination.
func (c *Config) Validate() error {
	if c.Listen.Address == "" && c.Listen.TLSAddress == "" {
		return errors.New("config: listen.address or listen.tlsAddress is required")
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
		if c.Store.DSN == "" {
			if c.Paths.DataDir == "" {
				return errors.New("config: store.dsn or paths.dataDir is required for the sqlite dialect")
			}
			c.Store.DSN = filepath.Join(c.Paths.DataDir, "weavster.db")
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
	p := c.Auth.PasswordPolicy
	for _, v := range []int{p.MinLength, c.Auth.Lockout.RetryLimit, c.Auth.Lockout.LockoutPeriodSeconds} {
		if v < 0 {
			return errors.New("config: auth.passwordPolicy.minLength and auth.lockout values must be >= 0")
		}
	}
	for _, v := range []int{p.MinUpper, p.MinLower, p.MinNumeric, p.MinSpecial} {
		if v < -1 {
			return errors.New("config: auth.passwordPolicy character-class counts must be >= -1 (-1 forbids the class)")
		}
	}
	return nil
}
