package migrate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/config"
)

// failingStore is a config.Store whose Put always fails, exercising the
// error-propagation branch of Load (the "load" phase of the legacy ETL).
type failingStore struct {
	config.Store
	putErr error
}

func (f *failingStore) Put(context.Context, string, []byte) error { return f.putErr }

func validConfig() *config.Config {
	return &config.Config{
		Version: "1",
		Flows: map[string]config.Flow{
			"admit": {
				Name:   "admit",
				Source: config.Source{Type: "file"},
				Destinations: []config.Destination{
					{Name: "his-mllp", Type: "tcp"},
				},
			},
		},
	}
}

// TestLoadNilStoreFails covers Load's nil-store guard, which must reject a
// missing destination store before any marshaling or validation work.
func TestLoadNilStoreFails(t *testing.T) {
	err := Load(context.Background(), nil, validConfig())
	if err == nil {
		t.Fatal("expected error loading into a nil store")
	}
	if !strings.Contains(err.Error(), "nil store") {
		t.Errorf("error = %q, want mention of nil store", err)
	}
}

// TestLoadPutErrorPropagates covers Load's per-artifact Put error path: a
// destination store that rejects writes must surface its error unchanged so
// callers can detect a partial/aborted import.
func TestLoadPutErrorPropagates(t *testing.T) {
	sentinel := errors.New("backend unavailable")
	store := &failingStore{Store: config.NewMemStore(), putErr: sentinel}

	err := Load(context.Background(), store, validConfig())
	if !errors.Is(err, sentinel) {
		t.Fatalf("Load error = %v, want wrapped sentinel %v", err, sentinel)
	}
}
