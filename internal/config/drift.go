package config

import (
	"bytes"
	"context"
	"sort"
)

// DriftReport reports live state that diverges from the Git source of truth
// (gap #6).
type DriftReport struct {
	// OutOfBand lists live keys with no source-of-truth counterpart.
	OutOfBand []string `json:"outOfBand"`
	// Diverged lists keys whose live content differs from source.
	Diverged []string `json:"diverged"`
	// Missing lists source keys absent from live state.
	Missing []string `json:"missing"`
}

// DetectDrift compares live state against the source of truth and returns an
// out-of-band changes report (gap #6).
func DetectDrift(src ConfigSource, live Store) (DriftReport, error) {
	source, err := src.List()
	if err != nil {
		return DriftReport{}, err
	}
	liveArtifacts, err := live.List(context.Background())
	if err != nil {
		return DriftReport{}, err
	}

	var r DriftReport
	for k, sv := range source {
		lv, ok := liveArtifacts[k]
		if !ok {
			r.Missing = append(r.Missing, k)
			continue
		}
		if !bytes.Equal(sv, lv) {
			r.Diverged = append(r.Diverged, k)
		}
	}
	for k := range liveArtifacts {
		if _, ok := source[k]; !ok {
			r.OutOfBand = append(r.OutOfBand, k)
		}
	}
	sort.Strings(r.OutOfBand)
	sort.Strings(r.Diverged)
	sort.Strings(r.Missing)
	return r, nil
}
