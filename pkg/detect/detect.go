package detect

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"dedup/pkg/cache"
	"dedup/pkg/hash"
	"dedup/pkg/scanner"
)

// Stats 描述一次重复检测的统计信息。
type Stats struct {
	TotalFiles      int
	CandidateFiles  int
	CachedHashes    int
	ComputedHashes  int
	DuplicateGroups int
	DuplicateFiles  int
}

// FindDuplicates 在一组文件里查找内容完全相同的重复组。
func FindDuplicates(files []scanner.FileInfo, hasher hash.Hasher, workers int, quiet bool, cacheMgr *cache.CacheManager) ([]cache.DuplicateGroup, Stats, []error) {
	stats := Stats{TotalFiles: len(files)}
	if len(files) == 0 {
		return nil, stats, nil
	}

	sizeByPath := make(map[string]int64, len(files))
	sizeGroups := make(map[int64][]scanner.FileInfo)
	for _, file := range files {
		sizeByPath[file.Path] = file.Size
		sizeGroups[file.Size] = append(sizeGroups[file.Size], file)
	}

	hashByPath := make(map[string]string)
	var toHash []scanner.FileInfo

	for _, group := range sizeGroups {
		if len(group) < 2 {
			continue
		}
		stats.CandidateFiles += len(group)
		for _, file := range group {
			if hashValue, found := cacheMgr.GetCachedHash(file.Path, hasher.Name()); found {
				hashByPath[file.Path] = hashValue
				stats.CachedHashes++
			} else {
				toHash = append(toHash, file)
			}
		}
	}
	stats.ComputedHashes = len(toHash)

	newHashes, errors := computeHashes(toHash, hasher, workers, quiet, cacheMgr)
	for path, hashValue := range newHashes {
		hashByPath[path] = hashValue
	}

	type hashBucket struct {
		size  int64
		files []string
	}

	buckets := make(map[string]*hashBucket)
	for path, hashValue := range hashByPath {
		bucket := buckets[hashValue]
		if bucket == nil {
			bucket = &hashBucket{size: sizeByPath[path]}
			buckets[hashValue] = bucket
		}
		bucket.files = append(bucket.files, path)
	}

	groups := make([]cache.DuplicateGroup, 0, len(buckets))
	for hashValue, bucket := range buckets {
		if len(bucket.files) < 2 {
			continue
		}
		sort.Strings(bucket.files)
		groups = append(groups, cache.DuplicateGroup{
			Hash:      hashValue,
			Algorithm: hasher.Name(),
			Size:      bucket.size,
			Files:     bucket.files,
		})
		stats.DuplicateGroups++
		stats.DuplicateFiles += len(bucket.files)
	}

	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Size != groups[j].Size {
			return groups[i].Size > groups[j].Size
		}
		return groups[i].Hash < groups[j].Hash
	})

	return groups, stats, errors
}

type hashJob struct {
	path string
	size int64
}

type hashResult struct {
	path     string
	hash     string
	err      error
	size     int64
	modified time.Time
}

// computeHashes 并发计算哈希，并把结果异步写入缓存。
func computeHashes(files []scanner.FileInfo, hasher hash.Hasher, workers int, quiet bool, cacheMgr *cache.CacheManager) (map[string]string, []error) {
	hashMap := make(map[string]string)
	if len(files) == 0 {
		return hashMap, nil
	}

	if workers <= 0 {
		workers = 1
	}
	if workers > len(files) {
		workers = len(files)
	}

	const maxBuffered = 100
	jobs := make(chan hashJob, maxBuffered)
	results := make(chan hashResult, maxBuffered)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				f, err := os.Open(job.path)
				if err != nil {
					results <- hashResult{path: job.path, err: fmt.Errorf("无法打开文件：%v", err)}
					continue
				}

				var modifiedTime time.Time
				if fileInfo, statErr := f.Stat(); statErr == nil {
					modifiedTime = fileInfo.ModTime()
				}

				hashValue, err := hasher.Sum(f)
				f.Close()
				if err != nil {
					results <- hashResult{path: job.path, err: fmt.Errorf("哈希计算失败：%v", err)}
					continue
				}

				results <- hashResult{
					path:     job.path,
					hash:     hashValue,
					size:     job.size,
					modified: modifiedTime,
				}
			}
		}()
	}

	go func() {
		for _, file := range files {
			jobs <- hashJob{path: file.Path, size: file.Size}
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var errors []error
	for result := range results {
		if result.err != nil {
			errors = append(errors, result.err)
			if !quiet {
				fmt.Printf("[ERROR] %s: %v\n", result.path, result.err)
			}
			continue
		}

		hashMap[result.path] = result.hash
		if result.size > 0 && !result.modified.IsZero() {
			cacheMgr.CacheHash(result.path, result.hash, hasher.Name(), result.size, result.modified)
		}
	}

	return hashMap, errors
}
