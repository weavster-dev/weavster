package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/state"
)

// TestLifecycleLocking: pause waits for the flow's in-flight message, halt
// does not, and neither waits for another flow's message.
func TestLifecycleLocking(t *testing.T) {
	ctx := context.Background()
	flows := flowAdapter{store: state.NewMemStore(), locks: newFlowLocks()}
	for _, id := range []string{"a", "b"} {
		if _, err := flows.Create(ctx, gateway.Flow{ID: id}); err != nil {
			t.Fatal(err)
		}
		for _, action := range []string{"deploy", "start"} {
			if _, err := flows.Transition(ctx, id, action); err != nil {
				t.Fatal(err)
			}
		}
	}
	done := flows.locks.ProcessFlow("a") // a message of flow a is in flight

	within := func(name string, fn func() error, wantBlocked bool) {
		t.Helper()
		ch := make(chan error, 1)
		go func() { ch <- fn() }()
		select {
		case err := <-ch:
			if wantBlocked {
				t.Errorf("%s did not wait for the in-flight message", name)
			} else if err != nil {
				t.Errorf("%s: %v", name, err)
			}
		case <-time.After(100 * time.Millisecond):
			if !wantBlocked {
				t.Errorf("%s waited for another flow's message", name)
			}
			done()
			if err := <-ch; err != nil {
				t.Errorf("%s: %v", name, err)
			}
			done = flows.locks.ProcessFlow("a")
		}
	}
	within("pause b", func() error { _, err := flows.Transition(ctx, "b", "pause"); return err }, false)
	within("halt a", func() error { _, err := flows.Transition(ctx, "a", "halt"); return err }, false)
	within("resume a", func() error { _, err := flows.Transition(ctx, "a", "resume"); return err }, true)
	done()

	if _, err := flows.Transition(ctx, "a", "explode"); !errors.Is(err, gateway.ErrUnknownAction) {
		t.Errorf("unknown action: %v", err)
	}
}

type failingUpdateRepo struct {
	*state.MemStore
	failID string
}

func (r failingUpdateRepo) UpdateFlow(ctx context.Context, f state.FlowDefinition) error {
	if f.ID == r.failID {
		return errors.New("database is locked")
	}
	return r.MemStore.UpdateFlow(ctx, f)
}

func TestRedeployAllPartialAndLegacyStatus(t *testing.T) {
	ctx := context.Background()
	mem := state.NewMemStore()
	// Flows stored before the lifecycle existed: free-form statuses.
	for id, doc := range map[string]string{
		"a": `{"id":"a","status":"started"}`, "b": `{"id":"b","status":"started"}`,
		"c": `{"id":"c","status":"idle"}`, "d": `{"id":"d"}`,
	} {
		if err := mem.CreateFlow(ctx, state.FlowDefinition{ID: id, Document: []byte(doc)}); err != nil {
			t.Fatal(err)
		}
	}
	flows := flowAdapter{store: failingUpdateRepo{MemStore: mem, failID: "b"}, locks: newFlowLocks()}
	for id, want := range map[string]string{"c": "undeployed", "d": "undeployed", "a": "started"} {
		if f, _ := flows.Get(ctx, id); f.Status != want {
			t.Errorf("legacy %s reads as %q, want %s", id, f.Status, want)
		}
	}
	done, err := flows.RedeployAll(ctx)
	if err == nil || len(done) != 1 || done[0].ID != "a" || done[0].Status != "deployed" {
		t.Errorf("RedeployAll = %+v, %v; want a redeployed, then the error for b", done, err)
	}
}
