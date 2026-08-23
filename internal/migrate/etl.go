// Package migrate implements the legacy import ETL (extract, transform, load,
// dry-run) for the legacy XML/archive export format (gap #1).
package migrate

import (
	"context"
	"encoding/xml"
	"fmt"

	"github.com/weavster-dev/weavster/internal/config"
)

// Options configures the legacy import ETL.
type Options struct {
	// WithContent imports message history content (default: metadata only).
	WithContent bool
	// MappingVersion selects the legacy->YAML mapper version.
	MappingVersion string
}

// Report is the result of a legacy import run (gap #1).
type Report struct {
	Flows          int
	Snippets       int
	Scripts        int
	Users          int
	ConfigMapEntry int
	Messages       int
	ReviewRequired []string // constructs not auto-translated (flagged for review)
	Config         *config.Config
}

// Run executes the three-phase ETL: extract -> transform -> load (gap #1).
// When dryRun is true, nothing is written and only the report is returned.
func Run(ctx context.Context, data []byte, opts Options, store config.Store, dryRun bool) (*Report, error) {
	legacy, err := Extract(data)
	if err != nil {
		return nil, fmt.Errorf("migrate: extract: %w", err)
	}

	version := opts.MappingVersion
	if version == "" {
		version = MappingVersion
	}
	cfg, review, err := Transform(legacy, version)
	if err != nil {
		return nil, fmt.Errorf("migrate: transform: %w", err)
	}

	rep := &Report{
		Flows:          len(legacy.Flows),
		Snippets:       len(legacy.Snippets),
		Scripts:        len(legacy.Scripts),
		Users:          len(legacy.Users),
		ConfigMapEntry: len(legacy.ConfigMap),
		Messages:       len(legacy.Messages),
		ReviewRequired: review,
		Config:         cfg,
	}

	if dryRun {
		return rep, nil
	}
	if err := Load(ctx, store, cfg); err != nil {
		return nil, fmt.Errorf("migrate: load: %w", err)
	}
	return rep, nil
}

// Extract parses the legacy XML export format (gap #1).
func Extract(data []byte) (*LegacyExport, error) {
	var le LegacyExport
	if err := xml.Unmarshal(data, &le); err != nil {
		return nil, err
	}
	return &le, nil
}
