package main

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"fmt"
	"runtime"
	"runtime/metrics"
	"strings"
	"time"

	"github.com/weavster-dev/weavster/internal/auth"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// license is the edition statement shown by /system and /system/about.
const license = "MVP (no entitlement gating)"

// systemAdapter serves gateway.SystemReporter from the running process, its
// configuration, and the password policy it enforces.
type systemAdapter struct {
	cfg     serverconfig.Config
	policy  auth.PasswordPolicy
	started time.Time
	// certKey is the certificate's key type ("rsa", "ecdsa"), "" if unknown.
	certKey string
}

func newSystemAdapter(cfg serverconfig.Config, policy auth.PasswordPolicy) systemAdapter {
	s := systemAdapter{cfg: cfg, policy: policy, started: time.Now()}
	if cfg.Listen.TLSAddress != "" {
		if cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile); err == nil {
			switch cert.PrivateKey.(type) {
			case *rsa.PrivateKey:
				s.certKey = "rsa"
			case *ecdsa.PrivateKey:
				s.certKey = "ecdsa"
			}
		}
	}
	return s
}

func (s systemAdapter) Status() gateway.SystemStatus {
	now := time.Now()
	return gateway.SystemStatus{
		ID: "weavster", Status: "running", Version: version, BuildDate: buildDate,
		Time: now.Format(time.RFC3339), Timezone: now.Format("MST -07:00"), UptimeSeconds: int64(now.Sub(s.started).Seconds()),
		Runtime: runtime.Version(), Charsets: []string{"UTF-8", "ISO-8859-1"}, TLS: s.tlsStatus(), License: license,
	}
}

// tlsStatus reports the HTTPS listener from the options it is built with:
// the protocol floor, and the TLS 1.2 suites the certificate's key can use.
func (s systemAdapter) tlsStatus() gateway.TLSStatus {
	st := gateway.TLSStatus{Protocols: []string{}, Ciphers: []string{}}
	if s.cfg.Listen.TLSAddress == "" {
		return st
	}
	opts := tlsOptions(s.cfg)
	st.Enabled, st.Address, st.MinVersion = true, s.cfg.Listen.TLSAddress, s.cfg.TLS.MinVersion
	if opts.MinVersion <= tls.VersionTLS12 {
		st.Protocols = append(st.Protocols, "TLS 1.2")
		for _, id := range opts.CipherSuites {
			name := tls.CipherSuiteName(id)
			if s.certKey == "" || strings.Contains(name, "_"+strings.ToUpper(s.certKey)+"_") {
				st.Ciphers = append(st.Ciphers, name)
			}
		}
	}
	// TLS 1.3 suites are fixed by Go and always offered.
	st.Protocols = append(st.Protocols, "TLS 1.3")
	st.Ciphers = append(st.Ciphers, "TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384", "TLS_CHACHA20_POLY1305_SHA256")
	return st
}

func (s systemAdapter) About() gateway.SystemAbout {
	return gateway.SystemAbout{Name: "Weavster", Version: version, BuildDate: buildDate, Runtime: runtime.Version(),
		OS: runtime.GOOS, Arch: runtime.GOARCH, License: license}
}

// PasswordRequirements describes the policy the server enforces.
func (s systemAdapter) PasswordRequirements() gateway.PasswordRequirements {
	p := s.policy
	req := gateway.PasswordRequirements{MinLength: p.MinLength, MinUpper: p.MinUpper, MinLower: p.MinLower,
		MinNumeric: p.MinNumeric, MinSpecial: p.MinSpecial, Rules: []string{}}
	if p.MinLength > 0 {
		req.Rules = append(req.Rules, fmt.Sprintf("at least %d characters", p.MinLength))
	}
	for _, c := range []struct {
		n              int
		one, many, not string
	}{
		{p.MinUpper, "uppercase letter", "uppercase letters", "no uppercase letters"},
		{p.MinLower, "lowercase letter", "lowercase letters", "no lowercase letters"},
		{p.MinNumeric, "digit", "digits", "no digits"},
		{p.MinSpecial, "special character (anything but a letter or digit, spaces included)",
			"special characters (anything but letters and digits, spaces included)", "no special characters (only letters and digits)"},
	} {
		switch {
		case c.n == -1:
			req.Rules = append(req.Rules, c.not)
		case c.n == 1:
			req.Rules = append(req.Rules, "at least 1 "+c.one)
		case c.n > 1:
			req.Rules = append(req.Rules, fmt.Sprintf("at least %d %s", c.n, c.many))
		}
	}
	return req
}

// memoryMetrics are read without stopping the world (unlike ReadMemStats).
var memoryMetrics = []string{"/memory/classes/heap/objects:bytes", "/memory/classes/total:bytes"}

func (s systemAdapter) Resources() gateway.SystemResources {
	samples := make([]metrics.Sample, len(memoryMetrics))
	for i, name := range memoryMetrics {
		samples[i].Name = name
	}
	metrics.Read(samples)
	return gateway.SystemResources{CPUs: runtime.NumCPU(), Goroutines: runtime.NumGoroutine(),
		MemoryAllocBytes: samples[0].Value.Uint64(), MemorySysBytes: samples[1].Value.Uint64(),
		UptimeSeconds: int64(time.Since(s.started).Seconds())}
}
