package flowlife

import (
	"errors"
	"testing"
)

func TestNext(t *testing.T) {
	states := []string{Undeployed, Deployed, Started, Paused, Halted, Stopped}
	want := map[string]map[string]string{ // action → from → to ("" = rejected)
		Deploy:   {Undeployed: Deployed},
		Start:    {Deployed: Started, Stopped: Started},
		Pause:    {Started: Paused},
		Halt:     {Started: Halted},
		Resume:   {Paused: Started, Halted: Started},
		Stop:     {Started: Stopped, Paused: Stopped, Halted: Stopped},
		Undeploy: {Deployed: Undeployed, Started: Undeployed, Paused: Undeployed, Halted: Undeployed, Stopped: Undeployed},
	}
	for action, to := range want {
		for _, from := range states {
			t.Run(action+" from "+from, func(t *testing.T) {
				got, err := Next(from, action)
				if exp := to[from]; exp == "" {
					if err == nil {
						t.Errorf("allowed, got %s; want rejected", got)
					}
				} else if err != nil || got != exp {
					t.Errorf("got %q, %v; want %s", got, err, exp)
				}
			})
		}
	}
	if _, err := Next(Started, "explode"); !errors.Is(err, ErrUnknownAction) {
		t.Errorf("unknown action: err = %v", err)
	}
	if got, err := Next("", Deploy); err != nil || got != Deployed {
		t.Errorf("legacy empty state = %q, %v", got, err)
	}
	if _, err := Next("", Start); err == nil || err.Error() != "cannot start a flow that is undeployed" {
		t.Errorf("error = %v", err)
	}
	if !AcceptsMessages(Started) || AcceptsMessages(Deployed) || AcceptsMessages("weird") {
		t.Error("AcceptsMessages wrong")
	}
}
