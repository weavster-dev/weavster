package migrate

import "context"

// DryRun returns the dry-run report — counts plus the list of constructs not
// auto-translated — before any write (gap #1).
func DryRun(data []byte, opts Options) (*Report, error) {
	return Run(context.Background(), data, opts, nil, true)
}
