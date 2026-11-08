package cmd

import (
	"fmt"

	"dedup/pkg/cache"
	"dedup/pkg/config"
)

// runClear 清除哈希缓存与扫描结果。
func runClear(cfg *config.Config, store *cache.CacheManager) error {
	if err := store.ClearCache(); err != nil {
		return fmt.Errorf("清除失败：%v", err)
	}
	if !cfg.Quiet {
		fmt.Println("[INFO] 已清除哈希缓存与扫描结果")
	}
	return nil
}
