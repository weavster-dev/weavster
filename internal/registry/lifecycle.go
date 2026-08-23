package registry

// State is a module lifecycle state (gap #2):
// draft -> promoted -> active -> superseded -> retired.
type State string

const (
	StateDraft      State = "draft"
	StatePromoted   State = "promoted"
	StateActive     State = "active"
	StateSuperseded State = "superseded"
	StateRetired    State = "retired"
)
