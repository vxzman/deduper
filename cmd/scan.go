package cmd

import (
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"dedup/pkg/cache"
	"dedup/pkg/config"
	"dedup/pkg/detect"
	"dedup/pkg/hash"
	"dedup/pkg/scanner"
)

// runScan scans all configured roots and stores duplicate groups in the database.
func runScan(cfg *config.Config, store *cache.CacheManager) error {
	workers := cfg.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	hasher, err := hash.NewHasher(cfg.Hash)
	if err != nil {
		return fmt.Errorf("failed to initialize hash algorithm: %v", err)
	}

	if !cfg.Quiet {
		fmt.Printf("[INFO] Scanning %d root(s) with %s (workers=%d)\n", len(cfg.Roots), cfg.Hash, workers)
	}

	absDB, err := filepath.Abs(cfg.Database)
	if err != nil {
		absDB = cfg.Database
	}

	seen := make(map[string]struct{})
	var files []scanner.FileInfo
	scannedRoots := 0

	for _, root := range cfg.Roots {
		fileScanner := scanner.NewScanner(workers, cfg.Quiet, cfg.Exclude)
		scanned, err := fileScanner.Scan(root)
		if err != nil {
			// Continue with other roots when one is unavailable (e.g. an offline NAS mount).
			fmt.Printf("[WARN] Skipping root %s: %v\n", root, err)
			continue
		}
		scannedRoots++

		for _, file := range scanned {
			abs, absErr := filepath.Abs(file.Path)
			if absErr != nil {
				abs = file.Path
			}
			if abs == absDB || file.Size < cfg.MinSize {
				continue
			}
			if _, exists := seen[abs]; exists {
				continue
			}
			seen[abs] = struct{}{}
			files = append(files, scanner.FileInfo{Path: abs, Size: file.Size})
		}
	}

	if scannedRoots == 0 {
		return fmt.Errorf("no roots scanned successfully; keeping previous results")
	}

	groups, stats, hashErrors := detect.FindDuplicates(files, hasher, workers, cfg.Quiet, store)
	for _, hashErr := range hashErrors {
		fmt.Printf("[WARN] %v\n", hashErr)
	}

	scannedAt := time.Now()
	if err := store.SaveDuplicateGroups(groups, scannedAt); err != nil {
		return fmt.Errorf("failed to save scan results: %v", err)
	}

	if !cfg.Quiet {
		fmt.Printf("[INFO] Files: %d, candidates: %d, cache hits: %d, computed: %d\n",
			stats.TotalFiles, stats.CandidateFiles, stats.CachedHashes, stats.ComputedHashes)
		fmt.Printf("[INFO] Found %d duplicate group(s), %d file(s)\n", stats.DuplicateGroups, stats.DuplicateFiles)
		if len(groups) > 0 {
			fmt.Println("[INFO] Run `dedup delete` to clean duplicates interactively")
		}
	}

	return nil
}
