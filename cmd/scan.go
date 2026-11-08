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

// runScan 扫描配置中的所有目录，并把重复组结果写入数据库。
func runScan(cfg *config.Config, store *cache.CacheManager) error {
	workers := cfg.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	hasher, err := hash.NewHasher(cfg.Hash)
	if err != nil {
		return fmt.Errorf("哈希算法初始化失败：%v", err)
	}

	if !cfg.Quiet {
		fmt.Printf("[INFO] 开始扫描 %d 个目录，算法 %s，并发 %d\n", len(cfg.Roots), cfg.Hash, workers)
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
			// 单个目录不可用时继续扫描其余目录（例如 NAS 挂载点暂时离线）
			fmt.Printf("[WARN] 跳过目录 %s：%v\n", root, err)
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
		return fmt.Errorf("没有任何目录扫描成功，保留上次扫描结果")
	}

	groups, stats, hashErrors := detect.FindDuplicates(files, hasher, workers, cfg.Quiet, store)
	for _, hashErr := range hashErrors {
		fmt.Printf("[WARN] %v\n", hashErr)
	}

	scannedAt := time.Now()
	if err := store.SaveDuplicateGroups(groups, scannedAt); err != nil {
		return fmt.Errorf("保存扫描结果失败：%v", err)
	}

	if !cfg.Quiet {
		fmt.Printf("[INFO] 扫描文件 %d 个，候选文件 %d 个，缓存命中 %d 个，新计算 %d 个\n",
			stats.TotalFiles, stats.CandidateFiles, stats.CachedHashes, stats.ComputedHashes)
		fmt.Printf("[INFO] 发现 %d 组重复，共 %d 个文件\n", stats.DuplicateGroups, stats.DuplicateFiles)
		if len(groups) > 0 {
			fmt.Println("[INFO] 可运行 `dedup delete` 交互式清理重复文件")
		}
	}

	return nil
}
