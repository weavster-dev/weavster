package config

import (
	"context"
	"errors"
	"testing"
)

type failingConfigSource struct {
	err error
}

func (s failingConfigSource) List() (map[string][]byte, error) {
	return nil, s.err
}

type trackedListStore struct {
	*MemStore
	err   error
	calls int
}

func (s *trackedListStore) List(context.Context) (map[string][]byte, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return map[string][]byte{}, nil
}

func TestDetectDriftReturnsDependencyErrors(t *testing.T) {
	sourceErr := errors.New("source list failed")
	liveErr := errors.New("live list failed")

	tests := []struct {
		name          string
		source        ConfigSource
		storeErr      error
		wantErr       error
		wantStoreCall int
	}{
		{
			name:          "source listing",
			source:        failingConfigSource{err: sourceErr},
			wantErr:       sourceErr,
			wantStoreCall: 0,
		},
		{
			name:          "live listing",
			source:        NewMemSource(map[string][]byte{"flow/a": []byte("source")}),
			storeErr:      liveErr,
			wantErr:       liveErr,
			wantStoreCall: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &trackedListStore{MemStore: NewMemStore(), err: tc.storeErr}

			report, err := DetectDrift(tc.source, store)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("DetectDrift() error = %v, want %v", err, tc.wantErr)
			}
			if store.calls != tc.wantStoreCall {
				t.Errorf("live List calls = %d, want %d", store.calls, tc.wantStoreCall)
			}
			if len(report.OutOfBand) != 0 || len(report.Diverged) != 0 || len(report.Missing) != 0 {
				t.Errorf("DetectDrift() report = %+v, want zero report", report)
			}
		})
	}
}
