package serverconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
		check   func(t *testing.T, c Config)
	}{
		{name: "empty file keeps defaults", yaml: "", check: func(t *testing.T, c Config) {
			if c.Listen.Address != "127.0.0.1:8080" || c.Store.Dialect != DialectMemory || !c.Listen.RequireMarkerHeader ||
				c.Listen.ShutdownTimeoutMs != 10000 || c.Delivery != (Delivery{MaxAttempts: 5, BackoffBaseMs: 1000, RetryIntervalMs: 1000}) ||
				!c.Flows.DeployOnStartup {
				t.Errorf("defaults not applied: %+v", c)
			}
		}},
		{name: "sqlite dsn defaults under dataDir", yaml: "store: {dialect: sqlite}\npaths: {dataDir: /var/lib/weavster}\n", check: func(t *testing.T, c Config) {
			if c.StoreDSN() != filepath.Join("/var/lib/weavster", "weavster.db") {
				t.Errorf("dsn = %q", c.StoreDSN())
			}
		}},
		{name: "explicit values override defaults", yaml: `
listen: {address: "", tlsAddress: "0.0.0.0:8443", requireMarkerHeader: false}
tls: {certFile: c.pem, keyFile: k.pem, minVersion: "1.3"}
store: {dialect: postgres, dsn: "postgres://db/weavster", maxRetry: 0, retryWaitMs: 50}
auth:
  passwordPolicy: {minLength: 12, minSpecial: -1}
  lockout: {retryLimit: 0, lockoutPeriodSeconds: 0}
`, check: func(t *testing.T, c Config) {
			if c.Listen.Address != "" || c.Listen.RequireMarkerHeader || c.TLS.MinVersion != "1.3" ||
				c.Store.MaxRetry != 0 || c.Auth.PasswordPolicy.MinLength != 12 || c.Auth.PasswordPolicy.MinSpecial != -1 ||
				c.Auth.PasswordPolicy.MinUpper != 1 {
				t.Errorf("unexpected config: %+v", c)
			}
		}},
		{name: "unknown key", yaml: "store: {dialekt: sqlite}\n", wantErr: "field dialekt not found"},
		{name: "second document", yaml: "store: {dialect: memory}\n---\nstore: {dialect: sqlite}\n", wantErr: "exactly one YAML document"},
		{name: "bad listen address", yaml: "listen: {address: \"8080\"}\n", wantErr: "listen.address must be host:port"},
		{name: "bad tls address", yaml: "listen: {tlsAddress: \"localhost\"}\n", wantErr: "listen.tlsAddress must be host:port"},
		{name: "ephemeral port", yaml: "listen: {address: \":0\"}\n", wantErr: "listen.address needs a port from 1 to 65535"},
		{name: "unknown port name", yaml: "listen: {address: \":nosuchservice\"}\n", wantErr: "listen.address needs a port"},
		{name: "all classes forbidden", yaml: "auth: {passwordPolicy: {minUpper: -1, minLower: -1, minNumeric: -1, minSpecial: -1}}\n", wantErr: "forbids every character class"},
		{name: "malformed yaml", yaml: "listen: [\n", wantErr: "config:"},
		{name: "no listener", yaml: "listen: {address: \"\"}\n", wantErr: "listen.address or listen.tlsAddress is required"},
		{name: "tls without cert", yaml: "listen: {tlsAddress: \":8443\"}\n", wantErr: "requires tls.certFile and tls.keyFile"},
		{name: "bad tls version", yaml: "tls: {minVersion: \"1.0\"}\n", wantErr: "tls.minVersion must be 1.2 or 1.3"},
		{name: "bad dialect", yaml: "store: {dialect: mysql}\n", wantErr: "store.dialect must be"},
		{name: "postgres without dsn", yaml: "store: {dialect: postgres}\n", wantErr: "store.dsn is required"},
		{name: "sqlite without dsn or dataDir", yaml: "store: {dialect: sqlite}\n", wantErr: "store.dsn or paths.dataDir"},
		{name: "zero pool", yaml: "store: {maxConnections: 0}\n", wantErr: "store.maxConnections must be >= 1"},
		{name: "negative retry", yaml: "store: {maxRetry: -1}\n", wantErr: "must be >= 0"},
		{name: "zero shutdown timeout", yaml: "listen: {shutdownTimeoutMs: 0}\n", wantErr: "listen.shutdownTimeoutMs"},
		{name: "huge shutdown timeout", yaml: "listen: {shutdownTimeoutMs: 600001}\n", wantErr: "listen.shutdownTimeoutMs"},
		{name: "negative shutdown timeout", yaml: "listen: {shutdownTimeoutMs: -5}\n", wantErr: "listen.shutdownTimeoutMs"},
		{name: "zero delivery attempts", yaml: "delivery: {maxAttempts: 0}\n", wantErr: "delivery.maxAttempts"},
		{name: "huge retry interval", yaml: "delivery: {retryIntervalMs: 9223372036854775807}\n", wantErr: "delivery.retryIntervalMs must be between"},
		{name: "zero backoff", yaml: "delivery: {backoffBaseMs: 0}\n", wantErr: "delivery.backoffBaseMs must be between"},
		{name: "negative lockout", yaml: "auth: {lockout: {retryLimit: -2}}\n", wantErr: "auth.lockout values must be >= 0"},
		{name: "class count below -1", yaml: "auth: {passwordPolicy: {minUpper: -2}}\n", wantErr: "must be >= -1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "weavster.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			c, err := Load(path)
			if err == nil {
				err = c.Validate()
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, c)
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.yaml")); err == nil || !strings.HasPrefix(err.Error(), "config: ") {
		t.Fatalf("err = %v, want config: prefix", err)
	}
}
