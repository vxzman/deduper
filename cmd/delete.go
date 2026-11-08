package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"dedup/pkg/cache"
	"dedup/pkg/config"
)

// runDelete 交互式选择并永久删除重复文件。
func runDelete(cfg *config.Config, store *cache.CacheManager) error {
	groups, err := store.LoadDuplicateGroups()
	if err != nil {
		return fmt.Errorf("读取扫描结果失败：%v", err)
	}
	if len(groups) == 0 {
		fmt.Println("[INFO] 没有可清理的重复文件")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println()
		for i, group := range groups {
			fmt.Printf("[%d] %s  %d 个文件  hash=%s\n",
				i+1, humanSize(group.Size), len(group.Files), shortHash(group.Hash))
		}
		fmt.Print("\n输入要处理的组编号，q 退出：")

		line, err := readLine(reader)
		if err != nil {
			return nil
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.EqualFold(line, "q") || strings.EqualFold(line, "quit") {
			return nil
		}

		groupIndex, err := strconv.Atoi(line)
		if err != nil || groupIndex < 1 || groupIndex > len(groups) {
			fmt.Println("[WARN] 无效的组编号")
			continue
		}
		group := &groups[groupIndex-1]

		fmt.Printf("\n组 [%d] %s，%d 个文件：\n", groupIndex, humanSize(group.Size), len(group.Files))
		for i, file := range group.Files {
			suffix := ""
			if _, statErr := os.Stat(file); os.IsNotExist(statErr) {
				suffix = "  [文件不存在]"
			}
			fmt.Printf("  %d) %s%s\n", i+1, file, suffix)
		}

		fmt.Print("输入要永久删除的文件编号（逗号分隔，回车取消）：")
		line, err = readLine(reader)
		if err != nil {
			return nil
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		indices, err := parseIndices(line, len(group.Files))
		if err != nil {
			fmt.Printf("[WARN] %v\n", err)
			continue
		}
		if len(indices) == 0 {
			continue
		}
		if len(indices) >= len(group.Files) {
			fmt.Println("[WARN] 每组至少需要保留一个文件，请重新选择")
			continue
		}

		fmt.Print("确认永久删除以上文件？输入 yes 确认：")
		confirm, err := readLine(reader)
		if err != nil {
			return nil
		}
		if strings.TrimSpace(confirm) != "yes" {
			fmt.Println("[INFO] 已取消")
			continue
		}

		remaining, deleted := deleteSelected(group.Files, indices)
		if len(deleted) > 0 {
			if err := store.DeleteHashCache(deleted); err != nil {
				fmt.Printf("[WARN] 清理哈希缓存失败：%v\n", err)
			}
		}

		group.Files = remaining
		if err := store.UpdateDuplicateGroup(*group); err != nil {
			return fmt.Errorf("更新扫描结果失败：%v", err)
		}

		fmt.Printf("[INFO] 已删除 %d 个文件\n", len(deleted))
		if len(remaining) < 2 {
			groups = append(groups[:groupIndex-1], groups[groupIndex:]...)
		}
	}
}

// parseIndices 解析 1-based 编号列表，去重并校验范围。
func parseIndices(input string, length int) ([]int, error) {
	fields := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})

	seen := make(map[int]struct{}, len(fields))
	indices := make([]int, 0, len(fields))
	for _, field := range fields {
		value, err := strconv.Atoi(field)
		if err != nil {
			return nil, fmt.Errorf("无效的编号：%s", field)
		}
		if value < 1 || value > length {
			return nil, fmt.Errorf("编号超出范围：%d", value)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		indices = append(indices, value)
	}

	sort.Ints(indices)
	return indices, nil
}

// deleteSelected 删除指定编号的文件，返回保留与已删除的文件列表。
func deleteSelected(files []string, indices []int) (remaining []string, deleted []string) {
	selected := make(map[int]struct{}, len(indices))
	for _, index := range indices {
		selected[index] = struct{}{}
	}

	for i, file := range files {
		if _, ok := selected[i+1]; !ok {
			remaining = append(remaining, file)
			continue
		}

		if err := os.Remove(file); err != nil {
			if os.IsNotExist(err) {
				// 文件已不存在，视为已清理
				deleted = append(deleted, file)
				continue
			}
			fmt.Printf("[ERROR] 删除失败 %s：%v\n", file, err)
			remaining = append(remaining, file)
			continue
		}

		deleted = append(deleted, file)
		fmt.Printf("[DELETE] %s\n", file)
	}

	return remaining, deleted
}

// readLine 读取一行输入，EOF 时返回已读取内容。
func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		if err == io.EOF && len(line) > 0 {
			return line, nil
		}
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
