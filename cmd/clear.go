package cmd

import (
	"fmt"

	"dedup/pkg/cache"
	"dedup/pkg/config"
)

// runClear clears the hash cache and scan results.
func runClear(cfg *config.Config, store *cache.CacheManager) error {
	if err := store.ClearCache(); err != nil {
		return fmt.Errorf("failed to clear: %v", err)
	}
	if !cfg.Quiet {
		fmt.Println("[INFO] Hash cache and scan results cleared")
	}
	return nil
}
