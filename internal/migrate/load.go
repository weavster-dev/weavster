package migrate

import (
	"context"
	"fmt"

	"github.com/weavster-dev/weavster/internal/config"
)

// Load seeds the target store with the transformed config after validating it
// against the JSON Schemas (gap #1).
func Load(ctx context.Context, store config.Store, cfg *config.Config) error {
	if store == nil {
		return fmt.Errorf("migrate: nil store")
	}
	data, err := cfg.Marshal()
	if err != nil {
		return err
	}
	if err := config.Validate(data); err != nil {
		return fmt.Errorf("migrate: validation: %w", err)
	}
	for key, artifact := range cfg.Artifacts() {
		if err := store.Put(ctx, key, artifact); err != nil {
			return err
		}
	}
	return nil
}
