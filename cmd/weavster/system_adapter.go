package main

import (
	"crypto/tls"
	"fmt"
	"runtime"
	"time"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// license is the edition statement shown by /system and /system/about.
const license = "MVP (no entitlement gating)"

// systemAdapter serves gateway.SystemReporter from the running process and
// its configuration.
type systemAdapter struct {
	cfg     serverconfig.Config
	started time.Time
}

func (s systemAdapter) Status() gateway.SystemStatus {
	now := time.Now()
	return gateway.SystemStatus{
		ID: "weavster", Status: "running", Version: version, BuildDate: buildDate,
		Time: now.Format(time.RFC3339), Timezone: time.Local.String(), UptimeSeconds: int64(now.Sub(s.started).Seconds()),
		Runtime: runtime.Version(), Charsets: []string{"UTF-8", "ISO-8859-1"}, TLS: s.tlsStatus(), License: license,
	}
}

// tlsStatus reports the HTTPS listener as the server configures it: the
// protocol floor and the cipher suites Go will offer with it.
func (s systemAdapter) tlsStatus() gateway.TLSStatus {
	st := gateway.TLSStatus{Protocols: []string{}, Ciphers: []string{}}
	if s.cfg.Listen.TLSAddress == "" {
		return st
	}
	st.Enabled, st.Address, st.MinVersion = true, s.cfg.Listen.TLSAddress, s.cfg.TLS.MinVersion
	if s.cfg.TLS.MinVersion != "1.3" {
		st.Protocols = append(st.Protocols, "TLS 1.2")
		for _, id := range gateway.DefaultTLSOptions().CipherSuites {
			st.Ciphers = append(st.Ciphers, tls.CipherSuiteName(id))
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

func (s systemAdapter) PasswordRequirements() gateway.PasswordRequirements {
	p := s.cfg.Auth.PasswordPolicy
	req := gateway.PasswordRequirements{MinLength: p.MinLength, MinUpper: p.MinUpper, MinLower: p.MinLower,
		MinNumeric: p.MinNumeric, MinSpecial: p.MinSpecial, Rules: []string{}}
	if p.MinLength > 0 {
		req.Rules = append(req.Rules, fmt.Sprintf("at least %d characters", p.MinLength))
	}
	for _, c := range []struct {
		n    int
		name string
	}{{p.MinUpper, "uppercase letter"}, {p.MinLower, "lowercase letter"}, {p.MinNumeric, "digit"}, {p.MinSpecial, "special character"}} {
		switch {
		case c.n == -1:
			req.Rules = append(req.Rules, "no "+c.name+"s")
		case c.n == 1:
			req.Rules = append(req.Rules, "at least 1 "+c.name)
		case c.n > 1:
			req.Rules = append(req.Rules, fmt.Sprintf("at least %d %ss", c.n, c.name))
		}
	}
	return req
}

func (s systemAdapter) Resources() gateway.SystemResources {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return gateway.SystemResources{CPUs: runtime.NumCPU(), Goroutines: runtime.NumGoroutine(),
		MemoryAllocBytes: m.Alloc, MemorySysBytes: m.Sys, UptimeSeconds: int64(time.Since(s.started).Seconds())}
}
