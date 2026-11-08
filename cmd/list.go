package cmd

import (
	"fmt"

	"dedup/pkg/cache"
	"dedup/pkg/config"
)

// runList 列出数据库中保存的重复文件。
func runList(cfg *config.Config, store *cache.CacheManager) error {
	groups, err := store.LoadDuplicateGroups()
	if err != nil {
		return fmt.Errorf("读取扫描结果失败：%v", err)
	}

	if !cfg.Quiet {
		if scannedAt, ok, err := store.LastScanTime(); err == nil && ok {
			fmt.Printf("[INFO] 上次扫描：%s\n", scannedAt.Local().Format("2006-01-02 15:04:05"))
		}
	}

	if len(groups) == 0 {
		fmt.Println("[INFO] 没有重复文件")
		return nil
	}

	totalFiles := 0
	for i, group := range groups {
		totalFiles += len(group.Files)
		fmt.Printf("\n[%d] %s  %d 个文件  hash=%s\n",
			i+1, humanSize(group.Size), len(group.Files), shortHash(group.Hash))
		for _, file := range group.Files {
			fmt.Printf("    %s\n", file)
		}
	}

	fmt.Printf("\n共 %d 组，%d 个文件\n", len(groups), totalFiles)
	return nil
}
