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
				!c.Flows.DeployOnStartup || c.Stats != (Stats{SampleIntervalMs: 60000, RetentionHours: 24}) || c.Prune != (Prune{IntervalMinutes: 60}) ||
				c.Processing != (Processing{MaxConcurrent: 32, WaitMs: 5000}) {
				t.Errorf("defaults not applied: %+v", c)
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
		{name: "dsn from a secret", yaml: "store: {dialect: postgres, dsnEnv: WEAVSTER_STORE_DSN}\nsecrets: {dir: /etc/weavster/secrets}\n", check: func(t *testing.T, c Config) {
			if c.Store.DSNEnv != "WEAVSTER_STORE_DSN" || c.Secrets.Dir != "/etc/weavster/secrets" {
				t.Errorf("unexpected config: %+v", c)
			}
		}},
		{name: "unknown key", yaml: "store: {dialekt: postgres}\n", wantErr: "field dialekt not found"},
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
		{name: "postgres without dsn", yaml: "store: {dialect: postgres}\n", wantErr: "needs store.dsn or store.dsnEnv"},
		{name: "postgres with both", yaml: "store: {dialect: postgres, dsn: \"postgres://h/db\", dsnEnv: WEAVSTER_STORE_DSN}\n", wantErr: "needs store.dsn or store.dsnEnv"},
		{name: "bad dsnEnv", yaml: "store: {dialect: postgres, dsnEnv: ../dsn}\n", wantErr: "store.dsnEnv must be a secret name"},
		{name: "relative secrets dir", yaml: "secrets: {dir: secrets}\n", wantErr: "secrets.dir must be an absolute path"},
		{name: "sqlite removed", yaml: "store: {dialect: sqlite, dsn: /var/lib/weavster/weavster.db}\n", wantErr: "sqlite is no longer supported: use postgres"},
		{name: "prune negative age", yaml: "prune: {maxAgeHours: -1}\n", wantErr: "prune.maxAgeHours must be 0-876000"},
		{name: "prune negative count", yaml: "prune: {maxMessages: -1}\n", wantErr: "prune.maxMessages >= 0"},
		{name: "prune audit age negative", yaml: "prune: {auditMaxAgeDays: -1}\n", wantErr: "prune.auditMaxAgeDays must be 0-36500"},
		{name: "prune event age negative", yaml: "prune: {eventMaxAgeDays: -1}\n", wantErr: "prune.eventMaxAgeDays must be 0-36500"},
		{name: "prune without a store", yaml: "store: {dialect: disabled}\nprune: {auditMaxAgeDays: 30}\n", wantErr: "prune needs a message store"},
		{name: "prune interval zero", yaml: "prune: {intervalMinutes: 0}\n", wantErr: "prune.intervalMinutes 1-10080"},
		{name: "prune interval too long", yaml: "prune: {intervalMinutes: 10081}\n", wantErr: "prune.intervalMinutes 1-10080"},
		{name: "context path without slash", yaml: "listen: {contextPath: weavster}\n", wantErr: "listen.contextPath must look like /weavster"},
		{name: "context path trailing slash", yaml: "listen: {contextPath: /weavster/}\n", wantErr: "listen.contextPath must look like /weavster"},
		{name: "context path dot-dot", yaml: "listen: {contextPath: /a/../b}\n", wantErr: "listen.contextPath must look like /weavster"},
		{name: "context path control character", yaml: "listen: {contextPath: \"/a\\tb\"}\n", wantErr: "listen.contextPath must look like /weavster"},
		{name: "context path with dots inside a segment", yaml: "listen: {contextPath: /api/..well-known}\n", check: func(t *testing.T, c Config) {
			if c.Listen.ContextPath != "/api/..well-known" {
				t.Errorf("contextPath = %q", c.Listen.ContextPath)
			}
		}},
		{name: "context path single quote", yaml: "listen: {contextPath: \"/weav'ster\"}\n", wantErr: "listen.contextPath must look like /weavster"},
		{name: "context path dot segment", yaml: "listen: {contextPath: /a/./b}\n", wantErr: "listen.contextPath must look like /weavster"},
		{name: "context path dot-dot segment at the end", yaml: "listen: {contextPath: /a/..}\n", wantErr: "listen.contextPath must look like /weavster"},
		{name: "context path query", yaml: "listen: {contextPath: \"/a?b\"}\n", wantErr: "listen.contextPath must look like /weavster"},
		{name: "paths removed", yaml: "paths: {dataDir: /var/lib/weavster}\n", wantErr: "field paths not found"},
		{name: "zero pool", yaml: "store: {maxConnections: 0}\n", wantErr: "store.maxConnections must be >= 1"},
		{name: "negative retry", yaml: "store: {maxRetry: -1}\n", wantErr: "must be >= 0"},
		{name: "zero shutdown timeout", yaml: "listen: {shutdownTimeoutMs: 0}\n", wantErr: "listen.shutdownTimeoutMs"},
		{name: "huge shutdown timeout", yaml: "listen: {shutdownTimeoutMs: 600001}\n", wantErr: "listen.shutdownTimeoutMs"},
		{name: "processing defaults and values", yaml: "processing: {maxConcurrent: 4, waitMs: 0}\n", check: func(t *testing.T, c Config) {
			if c.Processing != (Processing{MaxConcurrent: 4, WaitMs: 0}) {
				t.Errorf("processing = %+v", c.Processing)
			}
		}},
		{name: "zero concurrency", yaml: "processing: {maxConcurrent: 0}\n", wantErr: "processing.maxConcurrent must be between 1 and 10000"},
		{name: "negative wait", yaml: "processing: {waitMs: -1}\n", wantErr: "processing.waitMs must be between 0 and 60000"},
		{name: "negative shutdown timeout", yaml: "listen: {shutdownTimeoutMs: -5}\n", wantErr: "listen.shutdownTimeoutMs"},
		{name: "zero delivery attempts", yaml: "delivery: {maxAttempts: 0}\n", wantErr: "delivery.maxAttempts"},
		{name: "huge retry interval", yaml: "delivery: {retryIntervalMs: 9223372036854775807}\n", wantErr: "delivery.retryIntervalMs must be between"},
		{name: "fast stats sampling", yaml: "stats: {sampleIntervalMs: 99}\n", wantErr: "stats.sampleIntervalMs must be between"},
		{name: "zero stats retention", yaml: "stats: {retentionHours: 0}\n", wantErr: "stats.retentionHours must be between"},
		{name: "too many stats samples", yaml: "stats: {sampleIntervalMs: 1000, retentionHours: 28}\n", wantErr: "more than 100000 samples"},
		{name: "one stats sample too many", yaml: "stats: {sampleIntervalMs: 864, retentionHours: 24}\n" /* 100000 intervals + the first sample */, wantErr: "more than 100000 samples"},
		{name: "stats just under the sample limit", yaml: "stats: {sampleIntervalMs: 865, retentionHours: 24}\n", check: func(t *testing.T, c Config) {
			if c.Stats.SampleIntervalMs != 865 { // 99884 intervals + the first sample
				t.Errorf("stats = %+v", c.Stats)
			}
		}},
		{name: "stats at the sample limit", yaml: "stats: {sampleIntervalMs: 1000, retentionHours: 27}\n", check: func(t *testing.T, c Config) {
			if c.Stats.RetentionHours != 27 {
				t.Errorf("stats = %+v", c.Stats)
			}
		}},
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
