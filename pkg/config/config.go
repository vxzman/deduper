package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config describes a declarative dedup configuration.
type Config struct {
	// Database is the bbolt path used for both the hash cache and scan results.
	Database string `yaml:"database"`
	// Hash is the hash algorithm: blake3 | sha256 | sha3.
	Hash string `yaml:"hash"`
	// Workers is the concurrency level; 0 means the number of CPU cores.
	Workers int `yaml:"workers"`
	// MinSize is the minimum file size in bytes to consider.
	MinSize int64 `yaml:"min_size"`
	// Roots is the list of directories to scan; duplicates are matched across all roots.
	Roots []string `yaml:"roots"`
	// Exclude is a list of glob patterns matched against full paths and file names.
	Exclude []string `yaml:"exclude"`
	// Quiet disables normal log output.
	Quiet bool `yaml:"quiet"`

	// Path records the config file path for diagnostics.
	Path string `yaml:"-"`
}

var validHashes = map[string]bool{
	"blake3": true,
	"sha256": true,
	"sha3":   true,
}

// Load reads and validates the configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
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
		return nil, fmt.Errorf("database must be configured in declarative mode and cannot be none")
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

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if len(c.Roots) == 0 {
		return fmt.Errorf("at least one root directory is required")
	}
	if !validHashes[c.Hash] {
		return fmt.Errorf("unsupported hash algorithm: %s (choose blake3|sha256|sha3)", c.Hash)
	}
	if c.Workers < 0 {
		return fmt.Errorf("workers cannot be negative")
	}
	if c.MinSize < 0 {
		return fmt.Errorf("min_size cannot be negative")
	}
	if c.Database == "" {
		return fmt.Errorf("database cannot be empty")
	}
	return nil
}

// resolvePath expands ~ and resolves relative paths against configDir.
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
