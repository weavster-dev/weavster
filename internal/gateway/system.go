package gateway

import (
	"crypto/rand"
	"fmt"
	"net/http"
)

// SystemStatus is the server's status, computed per request (spec §5
// system info, §2.11.38).
type SystemStatus struct {
	ID            string    `json:"id"`
	Status        string    `json:"status"`
	Version       string    `json:"version"`
	BuildDate     string    `json:"buildDate"`
	Time          string    `json:"time"`
	Timezone      string    `json:"timezone"`
	UptimeSeconds int64     `json:"uptimeSeconds"`
	Runtime       string    `json:"runtime"`
	Charsets      []string  `json:"charsets"`
	TLS           TLSStatus `json:"tls"`
	License       string    `json:"license"`
}

// TLSStatus reports the HTTPS listener as configured: without it, enabled
// is false and the lists are empty.
type TLSStatus struct {
	Enabled    bool     `json:"enabled"`
	Address    string   `json:"address,omitempty"`
	MinVersion string   `json:"minVersion,omitempty"`
	Protocols  []string `json:"protocols"`
	Ciphers    []string `json:"ciphers"`
}

// SystemAbout names the product and its build.
type SystemAbout struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	BuildDate string `json:"buildDate"`
	Runtime   string `json:"runtime"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	License   string `json:"license"`
}

// PasswordRequirements are the password policy's rules: a count of 0 means
// no requirement and -1 forbids the class; Rules says the same in words.
type PasswordRequirements struct {
	MinLength  int      `json:"minLength"`
	MinUpper   int      `json:"minUpper"`
	MinLower   int      `json:"minLower"`
	MinNumeric int      `json:"minNumeric"`
	MinSpecial int      `json:"minSpecial"`
	Rules      []string `json:"rules"`
}

// SystemResources is the server process's current use of the host.
type SystemResources struct {
	CPUs             int    `json:"cpus"`
	Goroutines       int    `json:"goroutines"`
	MemoryAllocBytes uint64 `json:"memoryAllocBytes"`
	MemorySysBytes   uint64 `json:"memorySysBytes"`
	UptimeSeconds    int64  `json:"uptimeSeconds"`
}

// SystemReporter reports on the running server.
type SystemReporter interface {
	Status() SystemStatus
	About() SystemAbout
	PasswordRequirements() PasswordRequirements
	Resources() SystemResources
}

// systemRoute serves one SystemReporter view; without a reporter, 503.
func (s *Server) systemRoute(view func(SystemReporter) any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if s.cfg.System == nil {
			writeStatusError(w, http.StatusServiceUnavailable, "system information unavailable")
			return
		}
		writeJSON(w, http.StatusOK, view(s.cfg.System))
	}
}

// handleGUID returns a new random (version 4) UUID.
func (s *Server) handleGUID(w http.ResponseWriter, _ *http.Request) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		writeBackendError(w, err)
		return
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	writeJSON(w, http.StatusOK, map[string]string{"guid": fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])})
}
