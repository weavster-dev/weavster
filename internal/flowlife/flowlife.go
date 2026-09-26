// Package flowlife defines the flow lifecycle state machine (spec §6.1,
// #107 D-29).
package flowlife

import (
	"errors"
	"fmt"
)

// ErrUnknownAction is returned by Next for a name that is not a lifecycle
// action.
var ErrUnknownAction = errors.New("unknown lifecycle action")

// Flow lifecycle states.
const (
	Undeployed = "undeployed"
	Deployed   = "deployed"
	Started    = "started"
	Paused     = "paused"
	Halted     = "halted"
	Stopped    = "stopped"
)

// Lifecycle actions.
const (
	Deploy   = "deploy"
	Undeploy = "undeploy"
	Start    = "start"
	Stop     = "stop"
	Pause    = "pause"
	Halt     = "halt"
	Resume   = "resume"
)

// transitions maps action → allowed source states → target state.
var transitions = map[string]struct {
	from []string
	to   string
}{
	Deploy:   {[]string{Undeployed}, Deployed},
	Start:    {[]string{Deployed, Stopped}, Started},
	Pause:    {[]string{Started}, Paused},
	Halt:     {[]string{Started}, Halted},
	Resume:   {[]string{Paused, Halted}, Started},
	Stop:     {[]string{Started, Paused, Halted}, Stopped},
	Undeploy: {[]string{Deployed, Started, Paused, Halted, Stopped}, Undeployed},
}

// Normalize maps a stored state to a known one; empty or unknown legacy
// states are undeployed.
func Normalize(state string) string {
	switch state {
	case Deployed, Started, Paused, Halted, Stopped:
		return state
	}
	return Undeployed
}

// Next returns the state after applying action to state, or an error
// describing why the transition is not allowed.
func Next(state, action string) (string, error) {
	t, ok := transitions[action]
	if !ok {
		return "", fmt.Errorf("%w %q", ErrUnknownAction, action)
	}
	state = Normalize(state)
	for _, from := range t.from {
		if from == state {
			return t.to, nil
		}
	}
	return "", fmt.Errorf("cannot %s a flow that is %s", action, state)
}

// AcceptsMessages reports whether a flow in state processes messages.
func AcceptsMessages(state string) bool { return Normalize(state) == Started }
