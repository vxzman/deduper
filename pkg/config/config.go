package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 声明式去重配置。
type Config struct {
	// Database 是 bbolt 数据库路径，同时保存哈希缓存与扫描结果。
	Database string `yaml:"database"`
	// Hash 是哈希算法：blake3 | sha256 | sha3。
	Hash string `yaml:"hash"`
	// Workers 是并发数，0 表示使用 CPU 核心数。
	Workers int `yaml:"workers"`
	// MinSize 是参与去重的最小文件大小（字节），小于该值的文件会被忽略。
	MinSize int64 `yaml:"min_size"`
	// Roots 是要扫描的目录列表，重复文件会在所有目录之间统一比对。
	Roots []string `yaml:"roots"`
	// Exclude 是排除规则（glob），同时匹配完整路径与文件名。
	Exclude []string `yaml:"exclude"`
	// Quiet 关闭普通日志输出。
	Quiet bool `yaml:"quiet"`

	// Path 记录配置文件路径，便于诊断。
	Path string `yaml:"-"`
}

var validHashes = map[string]bool{
	"blake3": true,
	"sha256": true,
	"sha3":   true,
}

// Load 读取并校验配置文件。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败：%v", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败：%v", err)
	}

	absConfig, err := filepath.Abs(path)
	if err != nil {
		absConfig = path
	}
	cfg.Path = absConfig
	configDir := filepath.Dir(absConfig)

	if cfg.Hash == "" {
		cfg.Hash = "blake3"
	}
	if cfg.Database == "" {
		cfg.Database = "dedup.db"
	}
	if cfg.Database == "none" {
		return nil, fmt.Errorf("声明式模式必须配置 database，不能为 none")
	}

	cfg.Database = resolvePath(cfg.Database, configDir)
	for i := range cfg.Roots {
		cfg.Roots[i] = resolvePath(cfg.Roots[i], configDir)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate 校验配置合法性。
func (c *Config) Validate() error {
	if len(c.Roots) == 0 {
		return fmt.Errorf("配置中至少需要一个 roots 目录")
	}
	if !validHashes[c.Hash] {
		return fmt.Errorf("不支持的哈希算法：%s（可选 blake3|sha256|sha3）", c.Hash)
	}
	if c.Workers < 0 {
		return fmt.Errorf("workers 不能为负数")
	}
	if c.MinSize < 0 {
		return fmt.Errorf("min_size 不能为负数")
	}
	if c.Database == "" {
		return fmt.Errorf("database 不能为空")
	}
	return nil
}

// resolvePath 展开 ~ 并把相对路径解析为相对于 configDir 的绝对路径。
func resolvePath(path, configDir string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}

	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				path = home
			} else {
				path = filepath.Join(home, path[2:])
			}
		}
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(configDir, path)
	}
	return filepath.Clean(path)
}
