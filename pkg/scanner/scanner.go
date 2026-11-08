package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// FileInfo 文件信息结构
type FileInfo struct {
	Path string // 文件完整路径
	Size int64  // 文件大小
}

// Scanner 文件扫描器
type Scanner struct {
	workers  int        // 并发工作协程数
	quiet    bool       // 静默模式
	excludes []string   // 排除规则（glob）
	files    []FileInfo // 扫描结果
	mu       sync.Mutex // 保护 files 切片的互斥锁
}

// dirQueue 无界目录队列，使用条件变量协调待处理目录数
type dirQueue struct {
	mu      sync.Mutex
	cond    *sync.Cond
	items   []string
	pending int // 已入队但尚未处理完成的目录数
}

func newDirQueue() *dirQueue {
	q := &dirQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// push 将一个目录加入队列
func (q *dirQueue) push(dir string) {
	q.mu.Lock()
	q.items = append(q.items, dir)
	q.pending++
	q.mu.Unlock()
	q.cond.Signal()
}

// pop 取出一个目录；队列为空且无待处理目录时返回 false
func (q *dirQueue) pop() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for len(q.items) == 0 {
		if q.pending == 0 {
			return "", false
		}
		q.cond.Wait()
	}

	dir := q.items[0]
	q.items = q.items[1:]
	return dir, true
}

// done 标记一个目录已处理完成
func (q *dirQueue) done() {
	q.mu.Lock()
	q.pending--
	if q.pending == 0 {
		q.cond.Broadcast()
	}
	q.mu.Unlock()
}

// NewScanner 创建新的扫描器
func NewScanner(workers int, quiet bool, excludes []string) *Scanner {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	return &Scanner{
		workers:  workers,
		quiet:    quiet,
		excludes: excludes,
		files:    make([]FileInfo, 0),
	}
}

// Scan 并发递归扫描目录
func (s *Scanner) Scan(root string) ([]FileInfo, error) {
	// 清理路径
	root = filepath.Clean(root)

	// 检查路径是否存在
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, fmt.Errorf("path does not exist: %s", root)
	}

	// 检查是否为目录
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("cannot access path %s: %v", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", root)
	}

	if !s.quiet {
		fmt.Printf("[INFO] Scanning: %s (workers=%d)\n", root, s.workers)
	}

	// 使用并发目录遍历
	s.files = make([]FileInfo, 0)
	err = s.scanDirConcurrent(root)
	if err != nil {
		return nil, fmt.Errorf("error while scanning: %v", err)
	}

	if !s.quiet {
		fmt.Printf("[INFO] Scan complete: %d files\n", len(s.files))
	}

	return s.files, nil
}

// scanDirConcurrent 并发扫描目录
func (s *Scanner) scanDirConcurrent(root string) error {
	queue := newDirQueue()
	var wg sync.WaitGroup

	// 启动工作协程处理目录
	for i := 0; i < s.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				dir, ok := queue.pop()
				if !ok {
					return
				}
				s.processDir(dir, queue.push)
				queue.done()
			}
		}()
	}

	// 发送根目录
	queue.push(root)
	wg.Wait()

	return nil
}

// processDir 处理单个目录
func (s *Scanner) processDir(dir string, push func(string)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !s.quiet {
			fmt.Printf("[WARN] Cannot read directory %s: %v\n", dir, err)
		}
		return nil // 继续扫描其他目录
	}

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())

		// 命中排除规则的目录/文件直接跳过
		if s.isExcluded(path) {
			continue
		}

		if entry.IsDir() {
			// 将子目录加入队列进行并发处理
			push(path)
			continue
		}

		// 处理文件
		fileInfo, err := entry.Info()
		if err != nil {
			if !s.quiet {
				fmt.Printf("[WARN] Cannot stat %s: %v\n", path, err)
			}
			continue
		}

		// 跳过符号链接
		if fileInfo.Mode()&os.ModeSymlink != 0 {
			if !s.quiet {
				fmt.Printf("[INFO] Skipping symlink: %s\n", path)
			}
			continue
		}

		// 跳过空文件
		if fileInfo.Size() == 0 {
			if !s.quiet {
				fmt.Printf("[INFO] Skipping empty file: %s\n", path)
			}
			continue
		}

		// 添加文件信息
		s.mu.Lock()
		s.files = append(s.files, FileInfo{
			Path: path,
			Size: fileInfo.Size(),
		})
		s.mu.Unlock()
	}

	return nil
}

// isExcluded 判断路径是否命中排除规则（同时匹配完整路径和文件名）。
func (s *Scanner) isExcluded(path string) bool {
	if len(s.excludes) == 0 {
		return false
	}
	base := filepath.Base(path)
	for _, pattern := range s.excludes {
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}
	}
	return false
}

// GetFiles 获取扫描结果
func (s *Scanner) GetFiles() []FileInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 返回副本以避免外部修改
	result := make([]FileInfo, len(s.files))
	copy(result, s.files)
	return result
}
