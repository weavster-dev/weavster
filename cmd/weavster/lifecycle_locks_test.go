package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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

func TestFlowLocksDoNotAccumulate(t *testing.T) {
	l := newFlowLocks()
	for i := 0; i < 100; i++ {
		l.ProcessFlow(fmt.Sprintf("unknown-%d", i))()
		l.exclusive(fmt.Sprintf("x-%d", i), i%2 == 0)()
	}
	if len(l.flows) != 0 {
		t.Errorf("%d lock entries left after release", len(l.flows))
	}
	held := l.ProcessFlow("f")
	if len(l.flows) != 1 {
		t.Error("held lock entry missing")
	}
	held()
}

func TestRedeployAllSkipsFlowsUndeployedMeanwhile(t *testing.T) {
	ctx := context.Background()
	mem := state.NewMemStore()
	for _, doc := range []string{`{"id":"a","status":"started"}`, `{"id":"b","status":"started"}`} {
		var f struct{ ID string }
		_ = json.Unmarshal([]byte(doc), &f)
		_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: f.ID, Document: []byte(doc)})
	}
	flows := flowAdapter{store: mem, locks: newFlowLocks()}
	// b is undeployed between RedeployAll's List and its per-flow lock: hold
	// b's lock so RedeployAll must wait, then undeploy b and release.
	release := flows.locks.exclusive("b", false)
	result := make(chan []gateway.Flow, 1)
	go func() {
		out, _ := flows.RedeployAll(ctx)
		result <- out
	}()
	time.Sleep(50 * time.Millisecond)
	f, _ := flows.Get(ctx, "b")
	f.Status = "undeployed"
	doc, _ := json.Marshal(f)
	_ = mem.UpdateFlow(ctx, state.FlowDefinition{ID: "b", Document: doc})
	release()
	out := <-result
	if len(out) != 1 || out[0].ID != "a" {
		t.Errorf("redeployed %+v; want only a", out)
	}
	if got, _ := flows.Get(ctx, "b"); got.Status != "undeployed" {
		t.Errorf("b resurrected as %s", got.Status)
	}
}

// TestDeployEnabledSkipsBadFlows: a flow that cannot be read or written is
// logged and skipped; other enabled flows still start.
func TestDeployEnabledSkipsBadFlows(t *testing.T) {
	ctx := context.Background()
	mem := state.NewMemStore()
	for id, doc := range map[string]string{
		"good":  `{"id":"good","enabled":true}`,
		"fails": `{"id":"fails","enabled":true}`,
		"off":   `{"id":"off","enabled":false}`,
		"bad":   `{`, // unreadable: logged and skipped
	} {
		_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: id, Document: []byte(doc)})
	}
	logs := &syncBuffer{}
	flows := flowAdapter{store: failingUpdateRepo{MemStore: mem, failID: "fails"}, locks: newFlowLocks()}
	flows.DeployEnabled(ctx, slog.New(slog.NewTextHandler(logs, nil)))
	for id, want := range map[string]string{"good": "started", "fails": "undeployed", "off": "undeployed"} {
		if f, _ := flows.Get(ctx, id); f.Status != want {
			t.Errorf("%s = %s, want %s", id, f.Status, want)
		}
	}
	if !strings.Contains(logs.String(), "auto-deploy failed") || !strings.Contains(logs.String(), "flow=fails") || !strings.Contains(logs.String(), "flow=bad") {
		t.Errorf("failure not logged: %s", logs.String())
	}

	// A store that cannot list flows: logged, nothing started.
	logs = &syncBuffer{}
	flowAdapter{store: listFailRepo{mem}}.DeployEnabled(ctx, slog.New(slog.NewTextHandler(logs, nil)))
	if !strings.Contains(logs.String(), "cannot list flows") {
		t.Errorf("list failure not logged: %s", logs.String())
	}
}

type listFailRepo struct{ *state.MemStore }

func (listFailRepo) ListFlows(context.Context) ([]state.FlowDefinition, error) {
	return nil, errors.New("database is locked")
}

func TestCheckDependencies(t *testing.T) {
	flow := func(id string, deps ...string) gateway.Flow { return gateway.Flow{ID: id, DependsOn: deps} }
	tests := []struct {
		name  string
		flows []gateway.Flow
		want  string
	}{
		{"none", []gateway.Flow{flow("a"), flow("b")}, ""},
		{"chain", []gateway.Flow{flow("a", "b"), flow("b", "c"), flow("c")}, ""},
		{"diamond", []gateway.Flow{flow("a", "b", "c"), flow("b", "d"), flow("c", "d"), flow("d")}, ""},
		{"unknown", []gateway.Flow{flow("a", "zz")}, "unknown flow zz"},
		{"self", []gateway.Flow{flow("a", "a")}, "itself"},
		{"cycle", []gateway.Flow{flow("a", "b"), flow("b", "c"), flow("c", "a")}, "dependency cycle"},
	}
	for _, tt := range tests {
		all := map[string]gateway.Flow{}
		for _, f := range tt.flows {
			all[f.ID] = f
		}
		err := checkDependencies(all)
		if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("%s: %v, want %q", tt.name, err, tt.want)
		}
	}
}
