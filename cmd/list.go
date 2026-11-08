package cmd

import (
	"fmt"

	"dedup/pkg/cache"
	"dedup/pkg/config"
)

// runList prints duplicate files stored in the database.
func runList(cfg *config.Config, store *cache.CacheManager) error {
	groups, err := store.LoadDuplicateGroups()
	if err != nil {
		return fmt.Errorf("failed to load scan results: %v", err)
	}

	if !cfg.Quiet {
		if scannedAt, ok, err := store.LastScanTime(); err == nil && ok {
			fmt.Printf("[INFO] Last scan: %s\n", scannedAt.Local().Format("2006-01-02 15:04:05"))
		}
	}

	if len(groups) == 0 {
		fmt.Println("[INFO] No duplicate files found")
		return nil
	}

	totalFiles := 0
	for i, group := range groups {
		totalFiles += len(group.Files)
		fmt.Printf("\n[%d] %s  %d files  hash=%s\n",
			i+1, humanSize(group.Size), len(group.Files), shortHash(group.Hash))
		for _, file := range group.Files {
			fmt.Printf("    %s\n", file)
		}
	}

	fmt.Printf("\nTotal: %d groups, %d files\n", len(groups), totalFiles)
	return nil
}
