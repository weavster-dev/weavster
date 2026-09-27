package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/weavster-dev/weavster/internal/flowlife"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/observability"
)

// File source timing (#107 D-56).
const (
	// sourceTick is how often the poller checks which flows are due.
	sourceTick = 100 * time.Millisecond
	// fileSettle is how long a file must be unchanged before it is read,
	// so a file still being written is not taken half-way.
	fileSettle = time.Second
	// defaultPollInterval applies when a source sets no pollIntervalMs.
	defaultPollInterval = time.Second
)

// fileStamp identifies a version of a file (a rejected file is skipped
// until it changes).
type fileStamp struct {
	size int64
	mod  time.Time
}

// fileSources polls the directory of every started flow that has a file
// source and sends each file through the flow like a message sent with the
// API (#107 D-56). It runs in one goroutine, so its maps need no lock.
type fileSources struct {
	flows   flowAdapter
	ingest  ingestAdapter
	events  *observability.EventLog
	logger  *slog.Logger
	last    map[string]time.Time // flow id -> last poll
	skip    map[string]fileStamp // path -> rejected version left in place
	lastErr map[string]string    // flow id -> last directory error logged
}

func newFileSources(flows flowAdapter, ingest ingestAdapter, logger *slog.Logger) *fileSources {
	return &fileSources{flows: flows, ingest: ingest, events: flows.events, logger: logger,
		last: map[string]time.Time{}, skip: map[string]fileStamp{}, lastErr: map[string]string{}}
}

// loop polls until ctx is cancelled; a file in progress is finished first.
func (s *fileSources) loop(ctx context.Context) {
	ticker := time.NewTicker(sourceTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.pass(ctx, now)
		}
	}
}

// pass polls every started flow with a file source whose interval has
// passed.
func (s *fileSources) pass(ctx context.Context, now time.Time) {
	flows, err := s.flows.List(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Warn("file sources: listing flows failed", "error", err)
		}
		return
	}
	for _, f := range flows {
		src := f.Source
		if src == nil || src.Type != "file" || flowlife.Normalize(f.Status) != flowlife.Started {
			delete(s.last, f.ID)
			continue
		}
		interval := defaultPollInterval
		if src.PollIntervalMs > 0 {
			interval = time.Duration(src.PollIntervalMs) * time.Millisecond
		}
		if now.Sub(s.last[f.ID]) < interval {
			continue
		}
		s.last[f.ID] = now
		s.poll(ctx, f, now)
		if ctx.Err() != nil {
			return
		}
	}
}

// poll reads the files of one flow's directory, oldest name first.
func (s *fileSources) poll(ctx context.Context, f gateway.Flow, now time.Time) {
	src := f.Source
	pattern := src.Pattern
	if pattern == "" {
		pattern = "*"
	}
	entries, err := os.ReadDir(src.Dir)
	if err != nil {
		if msg := err.Error(); s.lastErr[f.ID] != msg { // once per distinct error
			s.lastErr[f.ID] = msg
			s.logger.Warn("file source: cannot read the directory", "flow", f.ID, "error", err)
		}
		return
	}
	delete(s.lastErr, f.ID)
	for _, e := range entries { // ReadDir sorts by name and does not follow symlinks
		if ctx.Err() != nil {
			return
		}
		if !e.Type().IsRegular() {
			continue // directories, symlinks, devices
		}
		if ok, _ := filepath.Match(pattern, e.Name()); !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed meanwhile
		}
		stamp := fileStamp{size: info.Size(), mod: info.ModTime()}
		if now.Sub(stamp.mod) < fileSettle {
			continue // possibly still being written
		}
		path := filepath.Join(src.Dir, e.Name())
		if s.skip[path] == stamp {
			continue // rejected, unchanged since
		}
		delete(s.skip, path)
		if !s.readFile(ctx, f, e.Name(), path, stamp) {
			return
		}
	}
}

// readFile sends one file through the flow and then removes it (moves it
// to moveTo when set). It returns false when the flow stopped accepting
// messages or the store failed, so the rest waits for the next poll.
func (s *fileSources) readFile(ctx context.Context, f gateway.Flow, name, path string, stamp fileStamp) bool {
	if stamp.size > gateway.MaxMessageBytes {
		s.reject(f, name, path, stamp, fmt.Sprintf("larger than %d MiB", gateway.MaxMessageBytes>>20))
		return true
	}
	body, err := os.ReadFile(path)
	if err != nil {
		s.logger.Warn("file source: cannot read a file", "flow", f.ID, "file", name, "error", err)
		return true
	}
	res, err := s.ingest.ingest(ctx, f.ID, body, map[string]string{"source.file": name})
	switch {
	case errors.Is(err, gateway.ErrFlowNotRunning), errors.Is(err, gateway.ErrFlowNotFound):
		return false
	case errors.Is(err, gateway.ErrInvalidMessage):
		s.reject(f, name, path, stamp, err.Error())
		return true
	case err != nil:
		s.logger.Warn("file source: processing failed; the file is kept", "flow", f.ID, "file", name, "error", err)
		return false
	}
	// The message is stored: from here the file is done. If removing it
	// fails, it is read again (at-least-once).
	if f.Source.MoveTo == "" {
		err = os.Remove(path)
	} else {
		err = moveFile(path, f.Source.MoveTo, name, res.ID)
	}
	if err != nil {
		s.logger.Error("file source: the file was processed but could not be removed; it will be read again", "flow", f.ID, "file", name, "message", res.ID, "error", err)
	}
	return true
}

// reject moves a file the flow refuses to moveTo/rejected, or leaves it
// and skips it until it changes, and records a source.file.rejected event.
func (s *fileSources) reject(f gateway.Flow, name, path string, stamp fileStamp, reason string) {
	moved := false
	if f.Source.MoveTo != "" {
		if err := moveFile(path, filepath.Join(f.Source.MoveTo, "rejected"), name, ""); err == nil {
			moved = true
		} else {
			s.logger.Warn("file source: cannot move a rejected file", "flow", f.ID, "file", name, "error", err)
		}
	}
	if !moved {
		s.skip[path] = stamp
	}
	if s.events != nil {
		s.events.Add("source.file.rejected", "", f.ID, map[string]string{"file": name, "reason": reason})
	}
}

// moveFile moves path into dir (created if missing) under name, or, when a
// file of that name is already there, under name.<suffix>. It copies when
// the directories are on different filesystems.
func moveFile(path, dir, name, suffix string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	dest := filepath.Join(dir, name)
	if _, err := os.Lstat(dest); err == nil {
		if suffix == "" {
			suffix = time.Now().UTC().Format("20060102T150405.000000000")
		}
		dest += "." + suffix
	}
	if err := os.Rename(path, dest); err == nil {
		return nil
	}
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dest)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dest)
		return err
	}
	return os.Remove(path)
}
