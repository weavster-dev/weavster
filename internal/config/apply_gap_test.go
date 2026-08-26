package config

import (
	"context"
	"errors"
	"testing"
)

// errStore is a Store whose Put/Delete always fail, used to exercise Apply's
// error-propagation branches (existing TestApply only covers the success path).
type errStore struct {
	MemStore
	failPut    bool
	failDelete bool
}

func newErrStore() *errStore {
	return &errStore{MemStore: *NewMemStore()}
}

func (e *errStore) Put(ctx context.Context, key string, value []byte) error {
	if e.failPut {
		return errors.New("put failed")
	}
	return e.MemStore.Put(ctx, key, value)
}

func (e *errStore) Delete(ctx context.Context, key string) error {
	if e.failDelete {
		return errors.New("delete failed")
	}
	return e.MemStore.Delete(ctx, key)
}

func TestApplyPutErrorOnAdded(t *testing.T) {
	store := newErrStore()
	store.failPut = true
	plan := Plan{Added: []string{"flow/a"}}
	err := Apply(context.Background(), store, plan, map[string][]byte{"flow/a": []byte("x")}, nil)
	if err == nil {
		t.Fatal("expected error from failed Put on Added")
	}
}

func TestApplyPutErrorOnUpdated(t *testing.T) {
	store := newErrStore()
	store.failPut = true
	plan := Plan{Updated: []string{"flow/a"}}
	err := Apply(context.Background(), store, plan, map[string][]byte{"flow/a": []byte("x")}, nil)
	if err == nil {
		t.Fatal("expected error from failed Put on Updated")
	}
}

func TestApplyDeleteErrorOnRemoved(t *testing.T) {
	store := newErrStore()
	store.failDelete = true
	plan := Plan{Removed: []string{"flow/a"}}
	err := Apply(context.Background(), store, plan, nil, nil)
	if err == nil {
		t.Fatal("expected error from failed Delete on Removed")
	}
}

// TestApplyNilAuditNoRecord asserts Apply succeeds and simply skips recording
// when no AuditSink is supplied, without calling Record on a nil interface.
func TestApplyNilAuditNoRecord(t *testing.T) {
	store := NewMemStore()
	plan := Plan{Added: []string{"flow/a"}}
	if err := Apply(context.Background(), store, plan, map[string][]byte{"flow/a": []byte("x")}, nil); err != nil {
		t.Fatalf("apply with nil audit: %v", err)
	}
}
