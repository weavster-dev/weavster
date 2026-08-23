package executor

import "context"

// ResponseStage runs the response-transformer and response-selector stages,
// distinct from the initial transform (spec §2.2.7).
type ResponseStage struct {
	engine TransformEngine
}

// NewResponseStage wraps a TransformEngine for response processing.
func NewResponseStage(e TransformEngine) *ResponseStage {
	return &ResponseStage{engine: e}
}

// TransformResponse runs the response-transformer module on a response.
func (s *ResponseStage) TransformResponse(ctx context.Context, req Request) ([]byte, error) {
	return s.engine.Transform(ctx, req)
}

// SelectResponse returns the first candidate for which pred is true (the
// response-selector stage).
func SelectResponse(candidates [][]byte, pred func([]byte) bool) (int, []byte, bool) {
	for i, c := range candidates {
		if pred(c) {
			return i, c, true
		}
	}
	return -1, nil, false
}

// FanOut routes a message to every non-excluded destination (spec §2.3.8).
func FanOut(destinations []string, excluded map[string]bool, send func(dest string) error) []error {
	var errs []error
	for _, d := range destinations {
		if excluded[d] {
			continue
		}
		if err := send(d); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}
