package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/flowlife"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/observability"
	"github.com/weavster-dev/weavster/internal/serverconfig"
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
		roots := make([]string, 0, len(all))
		for id := range all {
			roots = append(roots, id)
		}
		err := checkDependencies(all, roots)
		if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("%s: %v, want %q", tt.name, err, tt.want)
		}
	}
}

func TestCheckDependenciesScopedToRoots(t *testing.T) {
	all := map[string]gateway.Flow{
		"bad": {ID: "bad", DependsOn: []string{"gone"}}, // pre-existing dangling edge
		"a":   {ID: "a", DependsOn: []string{"b"}},
		"b":   {ID: "b"},
	}
	if err := checkDependencies(all, []string{"a"}); err != nil {
		t.Errorf("an unrelated bad flow blocked the check: %v", err)
	}
	if got := dependencyOrder(all, []string{"a", "b"}); strings.Join(got, ",") != "b,a" {
		t.Errorf("dependencyOrder = %v, want b before a", got)
	}
}

type createFailRepo struct {
	*state.MemStore
	failID string
}

func (r createFailRepo) CreateFlow(ctx context.Context, f state.FlowDefinition) error {
	if f.ID == r.failID {
		return errors.New("database is locked")
	}
	return r.MemStore.CreateFlow(ctx, f)
}

// TestImportPartialFailure: writes happen dependencies-first, and a failure
// reports what was already written.
func TestImportPartialFailure(t *testing.T) {
	ctx := context.Background()
	flows := flowAdapter{store: createFailRepo{MemStore: state.NewMemStore(), failID: "top"}, locks: newFlowLocks(), defs: &sync.Mutex{}}
	res, err := flows.Import(ctx, []gateway.Flow{{ID: "top", DependsOn: []string{"base"}}, {ID: "base"}}, false)
	if !errors.Is(err, gateway.ErrImportIncomplete) || strings.Join(res.Created, ",") != "base" {
		t.Errorf("Import = %+v, %v; want base written first, then the failure", res, err)
	}
}

// TestDeleteFailsClosed: a flow cannot be deleted while another flow is
// unreadable (its dependencies are unknown), but the unreadable flow itself
// can always be deleted.
func TestDeleteFailsClosed(t *testing.T) {
	ctx := context.Background()
	mem := state.NewMemStore()
	_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: "ok", Document: []byte(`{"id":"ok"}`)})
	_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: "corrupt", Document: []byte(`{`)})
	flows := flowAdapter{store: mem, locks: newFlowLocks(), defs: &sync.Mutex{}}
	if err := flows.Delete(ctx, "ok"); err == nil || !strings.Contains(err.Error(), "corrupt is unreadable") {
		t.Errorf("delete with an unreadable neighbour = %v, want refusal", err)
	}
	if err := flows.Delete(ctx, "corrupt"); err != nil {
		t.Errorf("deleting the unreadable flow itself: %v", err)
	}
	if err := flows.Delete(ctx, "ok"); err != nil {
		t.Errorf("delete after cleanup: %v", err)
	}
	if err := (flowAdapter{store: listFailRepo{mem}}).Delete(ctx, "x"); err == nil {
		t.Error("delete with a failing store list: want error")
	}
}

// TestImportDependencyErrorsBeatConflicts: a bundle that both collides and
// has a bad dependency is rejected as invalid (400), not as a conflict.
func TestImportDependencyErrorsBeatConflicts(t *testing.T) {
	ctx := context.Background()
	mem := state.NewMemStore()
	_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: "a", Document: []byte(`{"id":"a"}`)})
	flows := flowAdapter{store: mem, locks: newFlowLocks(), defs: &sync.Mutex{}}
	_, err := flows.Import(ctx, []gateway.Flow{{ID: "a", DependsOn: []string{"missing"}}}, false)
	if !errors.Is(err, gateway.ErrInvalidFlow) {
		t.Errorf("err = %v, want ErrInvalidFlow", err)
	}
}

// TestDeployDependencyErrors: a missing dependency is a 409-class
// ErrDependency naming it; a store failure part-way leaves earlier
// dependencies deployed and returns the error.
func TestDeployDependencyErrors(t *testing.T) {
	ctx := context.Background()
	mem := state.NewMemStore()
	for id, doc := range map[string]string{
		"orphan": `{"id":"orphan","status":"undeployed","dependsOn":["gone"]}`,
		"top":    `{"id":"top","status":"undeployed","dependsOn":["mid"]}`,
		"mid":    `{"id":"mid","status":"undeployed","dependsOn":["base"]}`,
		"base":   `{"id":"base","status":"undeployed"}`,
	} {
		_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: id, Document: []byte(doc)})
	}
	flows := flowAdapter{store: failingUpdateRepo{MemStore: mem, failID: "mid"}, locks: newFlowLocks()}
	if _, err := flows.Transition(ctx, "orphan", "deploy"); !errors.Is(err, gateway.ErrDependency) || !strings.Contains(err.Error(), "missing flow gone") {
		t.Errorf("missing dependency: %v", err)
	}
	if _, err := flows.Transition(ctx, "top", "deploy"); err == nil {
		t.Fatal("deploy with a failing dependency write: want error")
	}
	for id, want := range map[string]string{"base": "deployed", "mid": "undeployed", "top": "undeployed"} {
		if f, _ := flows.Get(ctx, id); f.Status != want {
			t.Errorf("%s = %s, want %s", id, f.Status, want)
		}
	}
}

// TestLifecycleEventsAndAutoDeployDependencies: every status change logs
// flow.<status>; startup auto-deploy deploys dependencies like a manual deploy.
func TestLifecycleEventsAndAutoDeployDependencies(t *testing.T) {
	ctx := context.Background()
	mem := state.NewMemStore()
	_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: "base", Document: []byte(`{"id":"base","status":"undeployed"}`)})
	_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: "top", Document: []byte(`{"id":"top","status":"undeployed","enabled":true,"dependsOn":["base"]}`)})
	events := observability.NewEventLog()
	flows := flowAdapter{store: mem, locks: newFlowLocks(), events: events}
	flows.DeployEnabled(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for id, want := range map[string]string{"top": "started", "base": "deployed"} {
		if f, _ := flows.Get(ctx, id); f.Status != want {
			t.Errorf("%s = %s, want %s", id, f.Status, want)
		}
	}
	if _, err := flows.Transition(ctx, "top", "pause"); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range events.Search(observability.EventFilter{}) {
		got = append(got, e.Flow+":"+e.Type)
	}
	if strings.Join(got, ",") != "base:flow.deployed,top:flow.started,top:flow.paused" {
		t.Errorf("events = %v", got)
	}
}

// TestConcurrentDeploysDoNotDeadlock: two flows whose dependencies are listed
// in opposite orders deploy concurrently without deadlocking.
func TestConcurrentDeploysDoNotDeadlock(t *testing.T) {
	for round := 0; round < 50; round++ {
		ctx := context.Background()
		mem := state.NewMemStore()
		for id, doc := range map[string]string{
			"a": `{"id":"a","status":"undeployed","dependsOn":["b","c"]}`,
			"d": `{"id":"d","status":"undeployed","dependsOn":["c","b"]}`,
			"b": `{"id":"b","status":"undeployed"}`,
			"c": `{"id":"c","status":"undeployed"}`,
		} {
			_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: id, Document: []byte(doc)})
		}
		flows := flowAdapter{store: mem, locks: newFlowLocks()}
		done := make(chan error, 2)
		for _, id := range []string{"a", "d"} {
			go func(id string) { _, err := flows.Transition(ctx, id, "deploy"); done <- err }(id)
		}
		for i := 0; i < 2; i++ {
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("round %d: concurrent deploys deadlocked", round)
			}
		}
	}
}

// TestAutoDeployOrderIndependent: an enabled flow that is also an enabled
// flow's dependency is started even when the dependent is processed first.
func TestAutoDeployOrderIndependent(t *testing.T) {
	ctx := context.Background()
	mem := state.NewMemStore()
	_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: "a-top", Document: []byte(`{"id":"a-top","status":"undeployed","enabled":true,"dependsOn":["b-base"]}`)})
	_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: "b-base", Document: []byte(`{"id":"b-base","status":"undeployed","enabled":true}`)})
	flows := flowAdapter{store: mem, locks: newFlowLocks()}
	flows.DeployEnabled(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, id := range []string{"a-top", "b-base"} {
		if f, _ := flows.Get(ctx, id); f.Status != "started" {
			t.Errorf("%s = %s, want started", id, f.Status)
		}
	}
}

func TestRuntimeHelpersAndSetDestinationRunning(t *testing.T) {
	current := gateway.Flow{StoppedDestinations: []string{"a", "b"}}
	next := gateway.Flow{Destinations: []gateway.FlowDestination{{Name: "b"}, {Name: "c"}}}
	current.Status = "started"
	if got := withRuntime(current, next); strings.Join(got.StoppedDestinations, ",") != "b" || got.Status != "started" {
		t.Errorf("withRuntime = %+v, want status started and stopped [b] (a was removed)", got)
	}
	if got := withoutRuntime(current, "undeployed"); got.Status != "undeployed" || got.StoppedDestinations != nil {
		t.Errorf("withoutRuntime = %+v", got)
	}

	ctx := context.Background()
	mem := state.NewMemStore()
	_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: "f", Document: []byte(`{"id":"f","status":"started","destinations":[{"name":"d","type":"file","dir":"/x"}]}`)})
	events := observability.NewEventLog()
	flows := flowAdapter{store: mem, locks: newFlowLocks(), events: events}
	for _, running := range []bool{false, false, true} { // stopping twice is a no-op
		if _, err := flows.SetDestinationRunning(ctx, "f", "d", running); err != nil {
			t.Fatal(err)
		}
	}
	if got := events.Count(observability.EventFilter{}); got != 2 {
		t.Errorf("%d events, want stopped then started", got)
	}
	if _, err := flows.SetDestinationRunning(ctx, "nope", "d", true); !errors.Is(err, gateway.ErrFlowNotFound) {
		t.Errorf("unknown flow: %v", err)
	}
}

// TestDestinationStopWaitsForInFlight: stop waits for the flow's in-flight
// messages; start does not.
func TestDestinationStopWaitsForInFlight(t *testing.T) {
	ctx := context.Background()
	mem := state.NewMemStore()
	_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: "f", Document: []byte(`{"id":"f","status":"started","destinations":[{"name":"d","type":"file","dir":"/x"}]}`)})
	flows := flowAdapter{store: mem, locks: newFlowLocks()}
	done := flows.locks.ProcessFlow("f")
	stopped := make(chan struct{})
	go func() { _, _ = flows.SetDestinationRunning(ctx, "f", "d", false); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("stop returned while a message was in flight")
	case <-time.After(50 * time.Millisecond):
	}
	done()
	<-stopped
	hold := flows.locks.ProcessFlow("f")
	defer hold()
	started := make(chan struct{})
	go func() { _, _ = flows.SetDestinationRunning(ctx, "f", "d", true); close(started) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("start waited for in-flight messages")
	}
}

// TestUpdateManyErrors: document problems are reported before anything is
// written; a store failure part-way reports what was written, dependencies
// first.
func TestUpdateManyErrors(t *testing.T) {
	ctx := context.Background()
	newFlows := func(failID string) flowAdapter {
		mem := state.NewMemStore()
		for _, id := range []string{"base", "top"} {
			_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: id, Document: []byte(`{"id":"` + id + `","status":"started"}`)})
		}
		return flowAdapter{store: failingUpdateRepo{MemStore: mem, failID: failID}, locks: newFlowLocks(), defs: &sync.Mutex{}}
	}
	bad := gateway.Flow{ID: "base", Destinations: []gateway.FlowDestination{{Name: "x", Type: "ftp"}}}
	tests := []struct {
		name    string
		failID  string
		changes []gateway.FlowChange
		want    error
		written string
	}{
		{"duplicate", "", []gateway.FlowChange{{Flow: gateway.Flow{ID: "base"}}, {Flow: gateway.Flow{ID: "base"}}}, gateway.ErrInvalidFlow, ""},
		{"invalid definition", "", []gateway.FlowChange{{Flow: bad}}, gateway.ErrInvalidFlow, ""},
		{"unknown flow", "", []gateway.FlowChange{{Flow: gateway.Flow{ID: "base"}}, {Flow: gateway.Flow{ID: "nope"}}}, gateway.ErrFlowNotFound, ""},
		{"bad dependency", "", []gateway.FlowChange{{Flow: gateway.Flow{ID: "top", DependsOn: []string{"gone"}}}}, gateway.ErrInvalidFlow, ""},
		{"incomplete", "top", []gateway.FlowChange{{Flow: gateway.Flow{ID: "top", DependsOn: []string{"base"}}}, {Flow: gateway.Flow{ID: "base"}}}, gateway.ErrUpdateIncomplete, "base"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flows := newFlows(tt.failID)
			written, err := flows.UpdateMany(ctx, tt.changes)
			if !errors.Is(err, tt.want) || strings.Join(written, ",") != tt.written {
				t.Errorf("UpdateMany = %v, %v; want %v with %q written", written, err, tt.want, tt.written)
			}
			if f, _ := flows.Get(ctx, "base"); f.Status != "started" {
				t.Errorf("base status = %q; an update must keep it", f.Status)
			}
		})
	}
	// An unrelated unreadable flow does not block an update without
	// dependencies; with dependencies every flow is read.
	mem := state.NewMemStore()
	_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: "a", Document: []byte(`{"id":"a"}`)})
	_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: "corrupt", Document: []byte(`{`)})
	flows := flowAdapter{store: mem, locks: newFlowLocks(), defs: &sync.Mutex{}}
	if _, err := flows.UpdateMany(ctx, []gateway.FlowChange{{Flow: gateway.Flow{ID: "a", Name: "A"}}}); err != nil {
		t.Errorf("UpdateMany next to an unreadable flow: %v", err)
	}
	if _, err := flows.UpdateMany(ctx, []gateway.FlowChange{{Flow: gateway.Flow{ID: "a", DependsOn: []string{"corrupt"}}}}); err == nil {
		t.Error("UpdateMany with dependencies and an unreadable flow: want error")
	}
	if _, err := flows.UpdateMany(ctx, []gateway.FlowChange{{Flow: gateway.Flow{ID: "corrupt"}}}); err == nil {
		t.Error("UpdateMany reading an unreadable flow: want error")
	}
}

func TestListeners(t *testing.T) {
	tests := []struct {
		name   string
		listen serverconfig.Listen
		want   []gateway.PortInUse
	}{
		{"none", serverconfig.Listen{}, nil},
		{"service name", serverconfig.Listen{TLSAddress: ":https"}, []gateway.PortInUse{{Address: ":https", Port: 443, UsedBy: "api-tls"}}},
		{"both", serverconfig.Listen{Address: ":8080", TLSAddress: "127.0.0.1:8443"},
			[]gateway.PortInUse{{Address: ":8080", Port: 8080, UsedBy: "api"}, {Address: "127.0.0.1:8443", Port: 8443, UsedBy: "api-tls"}}},
	}
	for _, tt := range tests {
		if got := listeners(tt.listen); !slices.Equal(got, tt.want) {
			t.Errorf("%s: listeners = %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestRetriedCounters(t *testing.T) {
	tests := []struct {
		status state.Status
		want   func(observability.FlowStats) int64
	}{
		{state.StatusSent, func(s observability.FlowStats) int64 { return s.Sent }},
		{state.StatusDeadLettered, func(s observability.FlowStats) int64 { return s.Errored }},
		{state.StatusFiltered, func(s observability.FlowStats) int64 { return s.Filtered }},
	}
	for _, tt := range tests {
		stats := observability.NewStatsRegistry()
		processingObserver{stats, observability.NewEventLog()}.Retried(state.Message{ID: "m", FlowID: "f", Status: tt.status}, nil)
		if got := tt.want(stats.Snapshot("f", false)); got != 1 {
			t.Errorf("%s: counter = %d, want 1", tt.status, got)
		}
	}
}

// TestTransitionAll: deploy-all reports a disabled dependency deployed by
// its dependent as changed; a store failure part-way reports what changed;
// an unknown action and a failing list are errors.
func TestTransitionAll(t *testing.T) {
	ctx := context.Background()
	docs := map[string]string{
		"a":   `{"id":"a","enabled":true,"dependsOn":["dep"]}`,
		"dep": `{"id":"dep"}`,
		"off": `{"id":"off"}`,
		"b":   `{"id":"b","status":"deployed"}`,
		"c":   `{"id":"c","status":"deployed"}`,
	}
	tests := []struct {
		name, action, failID string
		listFails            bool
		wantErr              error
		changed, skipped     string
	}{
		{"deploy with a disabled dependency", "deploy", "", false, nil, "dep,a", "off=disabled"},
		{"store failure part-way", "start", "b", false, gateway.ErrTransitionIncomplete, "", "b=failed,c=not attempted,off=not attempted"},
		{"unknown action", "explode", "", false, gateway.ErrUnknownAction, "", ""},
		{"failing list", "start", "", true, errors.New("any"), "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := state.NewMemStore()
			for id, doc := range docs {
				_ = mem.CreateFlow(ctx, state.FlowDefinition{ID: id, Document: []byte(doc)})
			}
			var repo flowRepository = failingUpdateRepo{MemStore: mem, failID: tt.failID}
			if tt.listFails {
				repo = listFailRepo{mem}
			}
			flows := flowAdapter{store: repo, locks: newFlowLocks(), defs: &sync.Mutex{}}
			res, err := flows.TransitionAll(ctx, tt.action)
			var skipped []string
			for _, sk := range res.Skipped {
				if sk.Reason == "disabled" || sk.Reason == "failed" || sk.Reason == "not attempted" {
					skipped = append(skipped, sk.ID+"="+sk.Reason)
				}
			}
			switch {
			case tt.wantErr == nil && err != nil, tt.wantErr != nil && err == nil,
				tt.wantErr != nil && !tt.listFails && !errors.Is(err, tt.wantErr):
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got := strings.Join(res.Changed, ","); got != tt.changed {
				t.Errorf("changed = %q, want %q", got, tt.changed)
			}
			if got := strings.Join(skipped, ","); got != tt.skipped {
				t.Errorf("skipped (disabled, failed, not attempted) = %q, want %q", got, tt.skipped)
			}
		})
	}
}

// TestAllFlowActionsMatchLifecycle keeps the all-flows routes in step with
// the lifecycle actions.
func TestAllFlowActionsMatchLifecycle(t *testing.T) {
	want := []string{flowlife.Deploy, flowlife.Undeploy, flowlife.Start, flowlife.Stop, flowlife.Pause, flowlife.Halt, flowlife.Resume}
	if !slices.Equal(gateway.AllFlowActions, want) {
		t.Errorf("gateway.AllFlowActions = %v, want %v", gateway.AllFlowActions, want)
	}
}
