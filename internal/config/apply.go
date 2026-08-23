package config

import (
	"context"
)

// Apply mutates live state to match the plan and desired artifacts, and
// records the applied plan in the audit log (gap #6).
func Apply(ctx context.Context, s Store, plan Plan, artifacts map[string][]byte, audit AuditSink) error {
	for _, k := range plan.Added {
		if err := s.Put(ctx, k, artifacts[k]); err != nil {
			return err
		}
	}
	for _, k := range plan.Updated {
		if err := s.Put(ctx, k, artifacts[k]); err != nil {
			return err
		}
	}
	for _, k := range plan.Removed {
		if err := s.Delete(ctx, k); err != nil {
			return err
		}
	}
	if audit != nil {
		return audit.Record(ctx, "config.apply", map[string]any{"plan": plan})
	}
	return nil
}
