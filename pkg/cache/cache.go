package cache

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

const (
	bucketMetadata = "metadata"
	bucketIndices  = "indices"
	bucketEntries  = "entries"
	bucketResults  = "results"
)

// CacheEntry 缓存条目
type CacheEntry struct {
	FilePath  string `json:"path"`
	Hash      string `json:"hash"`
	Algorithm string `json:"algorithm"`
	FileSize  int64  `json:"size"`
	Modified  int64  `json:"mtime"`  // Unix timestamp
	CachedAt  int64  `json:"cached"` // Unix timestamp
}

// DuplicateGroup 一组内容完全相同的文件。
type DuplicateGroup struct {
	Hash      string   `json:"hash"`
	Algorithm string   `json:"algorithm"`
	Size      int64    `json:"size"`
	Files     []string `json:"files"`
}

// ID 返回重复组的稳定存储键。
func (g DuplicateGroup) ID() string {
	return g.Algorithm + ":" + g.Hash
}

// CacheManager 缓存管理器
type CacheManager struct {
	db            *bolt.DB
	cachePath     string
	Enabled       bool
	Quiet         bool
	mutex         sync.RWMutex
	writeChan     chan *CacheEntry
	writeDone     chan struct{}
	writeTicker   *time.Ticker
	writeMu       sync.Mutex
	pending       []*CacheEntry
	batchSize     int
	flushInterval time.Duration
}

// NewCacheManager 创建缓存管理器
func NewCacheManager(cacheOption string, quiet bool) (*CacheManager, error) {
	if cacheOption == "none" {
		return NewDisabledCacheManager(quiet), nil
	}

	var cachePath string
	if cacheOption == "default" {
		cachePath = getDefaultCachePath()
	} else {
		cachePath = cacheOption
	}

	// 确保缓存目录存在
	cacheDir := filepath.Dir(cachePath)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %v", err)
	}

	// 打开或创建数据库
	db, err := bolt.Open(cachePath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	// 初始化 buckets
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range []string{bucketMetadata, bucketIndices, bucketEntries, bucketResults} {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return fmt.Errorf("failed to initialize database buckets: %v", err)
			}
		}
		return nil
	})

	if err != nil {
		db.Close()
		return nil, err
	}

	cm := &CacheManager{
		db:            db,
		cachePath:     cachePath,
		Enabled:       true,
		Quiet:         quiet,
		writeChan:     make(chan *CacheEntry, 1000),
		writeDone:     make(chan struct{}),
		pending:       make([]*CacheEntry, 0, 100),
		batchSize:     100,
		flushInterval: 500 * time.Millisecond,
	}

	// 启动异步写入协程
	go cm.writeWorker()

	if !quiet {
		fmt.Printf("[INFO] Database: %s\n", cachePath)
	}

	return cm, nil
}

// NewDisabledCacheManager 创建禁用的缓存管理器
func NewDisabledCacheManager(quiet bool) *CacheManager {
	return &CacheManager{
		Enabled: false,
		Quiet:   quiet,
	}
}

// writeWorker 异步写入工作协程
func (cm *CacheManager) writeWorker() {
	cm.writeTicker = time.NewTicker(cm.flushInterval)
	defer cm.writeTicker.Stop()

	for {
		select {
		case entry, ok := <-cm.writeChan:
			if !ok {
				// channel 已关闭，刷新剩余数据
				cm.flushPending()
				cm.writeDone <- struct{}{}
				return
			}
			cm.writeMu.Lock()
			cm.pending = append(cm.pending, entry)
			if len(cm.pending) >= cm.batchSize {
				cm.flushPending()
			}
			cm.writeMu.Unlock()

		case <-cm.writeTicker.C:
			cm.writeMu.Lock()
			if len(cm.pending) > 0 {
				cm.flushPending()
			}
			cm.writeMu.Unlock()
		}
	}
}

// flushPending 刷新待写入的数据
func (cm *CacheManager) flushPending() error {
	if len(cm.pending) == 0 {
		return nil
	}

	err := cm.db.Update(func(tx *bolt.Tx) error {
		indices := tx.Bucket([]byte("indices"))
		entries := tx.Bucket([]byte("entries"))

		if indices == nil || entries == nil {
			return nil
		}

		for _, entry := range cm.pending {
			// 每个条目使用独立的编码器，避免因复用编码器而丢失 gob 类型信息
			var buffer bytes.Buffer
			if err := gob.NewEncoder(&buffer).Encode(entry); err != nil {
				continue
			}
			entryBytes := buffer.Bytes()
			indices.Put([]byte(entry.FilePath), entryBytes)
			entries.Put([]byte(entry.Hash), entryBytes)
		}

		return nil
	})

	cm.pending = cm.pending[:0]
	return err
}

// GetCachedHash 获取缓存的哈希值
func (cm *CacheManager) GetCachedHash(filepath string, algorithm string) (string, bool) {
	if !cm.Enabled {
		return "", false
	}

	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	var hash string
	var found bool

	err := cm.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("indices"))
		if bucket == nil {
			return nil
		}

		value := bucket.Get([]byte(filepath))
		if value != nil {
			var entry CacheEntry
			buffer := bytes.NewBuffer(value)
			decoder := gob.NewDecoder(buffer)
			if decoder.Decode(&entry) == nil {
				// 算法不一致时视为未命中，避免不同算法的哈希混用
				if entry.Algorithm != algorithm {
					return nil
				}
				// 验证文件是否仍然有效
				if cm.isFileStillValid(filepath, &entry) {
					hash = entry.Hash
					found = true
				}
			}
		}
		return nil
	})

	if err != nil && !cm.Quiet {
		fmt.Printf("[WARN] Failed to read cache: %v\n", err)
	}

	return hash, found
}

// CacheHash 缓存哈希值 (异步写入)
func (cm *CacheManager) CacheHash(filepath, hash, algorithm string, fileSize int64, modifiedTime time.Time) error {
	if !cm.Enabled {
		return nil
	}

	entry := &CacheEntry{
		FilePath:  filepath,
		Hash:      hash,
		Algorithm: algorithm,
		FileSize:  fileSize,
		Modified:  modifiedTime.Unix(),
		CachedAt:  time.Now().Unix(),
	}

	// 非阻塞发送到写入队列
	select {
	case cm.writeChan <- entry:
		return nil
	default:
		// 如果队列已满，直接写入 (阻塞)
		return cm.writeEntry(entry)
	}
}

// writeEntry 直接写入单个条目 (同步)
func (cm *CacheManager) writeEntry(entry *CacheEntry) error {
	var buffer bytes.Buffer
	encoder := gob.NewEncoder(&buffer)
	err := encoder.Encode(entry)
	if err != nil {
		return err
	}
	entryBytes := buffer.Bytes()

	return cm.db.Update(func(tx *bolt.Tx) error {
		indices := tx.Bucket([]byte("indices"))
		entries := tx.Bucket([]byte("entries"))

		if indices != nil {
			indices.Put([]byte(entry.FilePath), entryBytes)
		}
		if entries != nil {
			entries.Put([]byte(entry.Hash), entryBytes)
		}
		return nil
	})
}

// isFileStillValid 检查文件是否仍然有效
func (cm *CacheManager) isFileStillValid(filepath string, entry *CacheEntry) bool {
	fileInfo, err := os.Stat(filepath)
	if err != nil {
		return false
	}

	return fileInfo.Size() == entry.FileSize &&
		fileInfo.ModTime().Unix() == entry.Modified
}

// ClearCache 清除所有缓存
func (cm *CacheManager) ClearCache() error {
	if !cm.Enabled {
		return nil
	}

	// 先刷新待写入的数据
	cm.writeMu.Lock()
	cm.flushPending()
	cm.writeMu.Unlock()

	return cm.db.Update(func(tx *bolt.Tx) error {
		// 删除所有 buckets 并重新创建
		for _, name := range []string{bucketMetadata, bucketIndices, bucketEntries, bucketResults} {
			if err := tx.DeleteBucket([]byte(name)); err != nil && err != bolt.ErrBucketNotFound {
				return err
			}
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	})
}

// SaveDuplicateGroups 覆盖保存本次扫描得到的重复组结果。
func (cm *CacheManager) SaveDuplicateGroups(groups []DuplicateGroup, scannedAt time.Time) error {
	if !cm.Enabled {
		return fmt.Errorf("storage is disabled")
	}

	// 先把异步哈希缓存落盘，避免与下面的事务交错影响可读性
	if err := cm.Flush(); err != nil {
		return err
	}

	return cm.db.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket([]byte(bucketResults)); err != nil && err != bolt.ErrBucketNotFound {
			return err
		}
		results, err := tx.CreateBucketIfNotExists([]byte(bucketResults))
		if err != nil {
			return err
		}

		for _, group := range groups {
			var buffer bytes.Buffer
			if err := gob.NewEncoder(&buffer).Encode(group); err != nil {
				return err
			}
			if err := results.Put([]byte(group.ID()), buffer.Bytes()); err != nil {
				return err
			}
		}

		meta, err := tx.CreateBucketIfNotExists([]byte(bucketMetadata))
		if err != nil {
			return err
		}
		return meta.Put([]byte("last_scan"), []byte(scannedAt.Format(time.RFC3339)))
	})
}

// LoadDuplicateGroups 读取已保存的重复组，按文件大小降序、哈希升序排序。
func (cm *CacheManager) LoadDuplicateGroups() ([]DuplicateGroup, error) {
	if !cm.Enabled {
		return nil, fmt.Errorf("storage is disabled")
	}

	var groups []DuplicateGroup
	err := cm.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketResults))
		if bucket == nil {
			return nil
		}
		return bucket.ForEach(func(_, value []byte) error {
			if value == nil {
				return nil
			}
			var group DuplicateGroup
			if err := gob.NewDecoder(bytes.NewReader(value)).Decode(&group); err != nil {
				return fmt.Errorf("failed to decode duplicate group: %v", err)
			}
			groups = append(groups, group)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Size != groups[j].Size {
			return groups[i].Size > groups[j].Size
		}
		return groups[i].Hash < groups[j].Hash
	})

	return groups, nil
}

// UpdateDuplicateGroup 更新单个重复组；文件数少于 2 时删除该组。
func (cm *CacheManager) UpdateDuplicateGroup(group DuplicateGroup) error {
	if !cm.Enabled {
		return fmt.Errorf("storage is disabled")
	}

	return cm.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketResults))
		if bucket == nil {
			return nil
		}
		if len(group.Files) < 2 {
			return bucket.Delete([]byte(group.ID()))
		}

		var buffer bytes.Buffer
		if err := gob.NewEncoder(&buffer).Encode(group); err != nil {
			return err
		}
		return bucket.Put([]byte(group.ID()), buffer.Bytes())
	})
}

// DeleteHashCache 删除指定路径的哈希缓存索引。
func (cm *CacheManager) DeleteHashCache(paths []string) error {
	if !cm.Enabled {
		return nil
	}

	return cm.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketIndices))
		if bucket == nil {
			return nil
		}
		for _, path := range paths {
			if err := bucket.Delete([]byte(path)); err != nil {
				return err
			}
		}
		return nil
	})
}

// LastScanTime 返回上次扫描时间。
func (cm *CacheManager) LastScanTime() (time.Time, bool, error) {
	if !cm.Enabled {
		return time.Time{}, false, nil
	}

	var (
		scanTime time.Time
		found    bool
	)
	err := cm.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketMetadata))
		if bucket == nil {
			return nil
		}
		value := bucket.Get([]byte("last_scan"))
		if value == nil {
			return nil
		}
		parsed, err := time.Parse(time.RFC3339, string(value))
		if err != nil {
			return nil
		}
		scanTime, found = parsed, true
		return nil
	})
	return scanTime, found, err
}

// Close 关闭缓存 (确保所有数据写入)
func (cm *CacheManager) Close() error {
	if !cm.Enabled || cm.db == nil {
		return nil
	}

	// 关闭写入 channel，等待所有数据写入
	close(cm.writeChan)
	<-cm.writeDone

	return cm.db.Close()
}

// Flush 强制刷新缓存
func (cm *CacheManager) Flush() error {
	if !cm.Enabled {
		return nil
	}

	cm.writeMu.Lock()
	defer cm.writeMu.Unlock()
	return cm.flushPending()
}

// getDefaultCachePath 获取默认缓存路径
func getDefaultCachePath() string {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		return filepath.Join(appData, "dedup", "cache.db")
	case "darwin": // macOS
		home := os.Getenv("HOME")
		if home == "" {
			home = "/tmp"
		}
		return filepath.Join(home, "Library", "Application Support", "dedup", "cache.db")
	case "linux", "freebsd", "openbsd", "netbsd", "dragonfly":
		home := os.Getenv("HOME")
		if home == "" {
			home = "/tmp"
		}
		return filepath.Join(home, ".cache", "dedup", "cache.db")
	default:
		// 未知操作系统，使用/tmp 作为后备
		home := os.Getenv("HOME")
		if home == "" {
			home = "/tmp"
		}
		return filepath.Join(home, ".cache", "dedup", "cache.db")
	}
}
