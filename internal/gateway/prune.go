package gateway

import (
	"errors"
	"net/http"
	"time"
)

// Prune errors (409).
var (
	ErrPruneRunning    = errors.New("a prune pass is already running")
	ErrPruneNotRunning = errors.New("no prune pass is running")
	ErrPruneOff        = errors.New("pruning is not configured: set prune.maxAgeHours, prune.maxMessages, prune.auditMaxAgeDays, or prune.eventMaxAgeDays")
	// ErrPruneUnavailable: the server is starting or stopping (503).
	ErrPruneUnavailable = errors.New("pruning is unavailable while the server starts or stops")
)

// Pruner removes old messages on a schedule and on demand (spec §2.6.23).
type Pruner interface {
	Status() PruneStatus
	// Start runs a pass now: ErrPruneRunning, ErrPruneOff.
	Start() error
	// Stop ends the running pass: ErrPruneNotRunning.
	Stop() error
}

// PruneStatus is the configured limits and the passes.
type PruneStatus struct {
	MaxAgeHours     int        `json:"maxAgeHours"`
	MaxMessages     int        `json:"maxMessages"`
	AuditMaxAgeDays int        `json:"auditMaxAgeDays"`
	EventMaxAgeDays int        `json:"eventMaxAgeDays"`
	IntervalMinutes int        `json:"intervalMinutes"`
	Running         bool       `json:"running"`
	NextRun         *time.Time `json:"nextRunAt,omitempty"`
	LastRun         *PruneRun  `json:"lastRun,omitempty"`
}

// PruneRun is one pass: FinishedAt is nil while it runs.
type PruneRun struct {
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Removed    int        `json:"removed"`
	Busy       int        `json:"busy"`
	// AuditRemoved counts audit entries removed (prune.auditMaxAgeDays).
	AuditRemoved int `json:"auditRemoved"`
	// EventsRemoved counts stored events removed (prune.eventMaxAgeDays).
	EventsRemoved int    `json:"eventsRemoved"`
	Stopped       bool   `json:"stopped"`
	Error         string `json:"error,omitempty"`
}

func (s *Server) handlePruneStatus(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.Pruner == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "messages unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.cfg.Pruner.Status())
}

// handlePrune starts (202) or stops (200) a pass; the reply is the status.
func (s *Server) handlePrune(start bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if s.cfg.Pruner == nil {
			writeStatusError(w, http.StatusServiceUnavailable, "messages unavailable")
			return
		}
		op, code := s.cfg.Pruner.Stop, http.StatusOK
		if start {
			op, code = s.cfg.Pruner.Start, http.StatusAccepted
		}
		if err := op(); errors.Is(err, ErrPruneUnavailable) {
			writeStatusError(w, http.StatusServiceUnavailable, err.Error())
			return
		} else if err != nil {
			writeStatusError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, code, s.cfg.Pruner.Status())
	}
}
