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

// runDelete interactively selects and permanently deletes duplicate files.
func runDelete(cfg *config.Config, store *cache.CacheManager) error {
	groups, err := store.LoadDuplicateGroups()
	if err != nil {
		return fmt.Errorf("failed to load scan results: %v", err)
	}
	if len(groups) == 0 {
		fmt.Println("[INFO] No duplicate files to clean")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println()
		for i, group := range groups {
			fmt.Printf("[%d] %s  %d files  hash=%s\n",
				i+1, humanSize(group.Size), len(group.Files), shortHash(group.Hash))
		}
		fmt.Print("\nEnter the group number to process (q to quit): ")

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
			fmt.Println("[WARN] Invalid group number")
			continue
		}
		group := &groups[groupIndex-1]

		fmt.Printf("\nGroup [%d] %s, %d files:\n", groupIndex, humanSize(group.Size), len(group.Files))
		for i, file := range group.Files {
			suffix := ""
			if _, statErr := os.Stat(file); os.IsNotExist(statErr) {
				suffix = "  [missing]"
			}
			fmt.Printf("  %d) %s%s\n", i+1, file, suffix)
		}

		fmt.Print("Enter file numbers to permanently delete (comma-separated, Enter to cancel): ")
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
			fmt.Println("[WARN] At least one file must remain in each group; please choose again")
			continue
		}

		fmt.Print("Permanently delete the selected files? Type yes to confirm: ")
		confirm, err := readLine(reader)
		if err != nil {
			return nil
		}
		if strings.TrimSpace(confirm) != "yes" {
			fmt.Println("[INFO] Cancelled")
			continue
		}

		remaining, deleted := deleteSelected(group.Files, indices)
		if len(deleted) > 0 {
			if err := store.DeleteHashCache(deleted); err != nil {
				fmt.Printf("[WARN] Failed to clean hash cache: %v\n", err)
			}
		}

		group.Files = remaining
		if err := store.UpdateDuplicateGroup(*group); err != nil {
			return fmt.Errorf("failed to update scan results: %v", err)
		}

		fmt.Printf("[INFO] Deleted %d file(s)\n", len(deleted))
		if len(remaining) < 2 {
			groups = append(groups[:groupIndex-1], groups[groupIndex:]...)
		}
	}
}

// parseIndices parses a 1-based list of numbers, removes duplicates, and validates the range.
func parseIndices(input string, length int) ([]int, error) {
	fields := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})

	seen := make(map[int]struct{}, len(fields))
	indices := make([]int, 0, len(fields))
	for _, field := range fields {
		value, err := strconv.Atoi(field)
		if err != nil {
			return nil, fmt.Errorf("invalid number: %s", field)
		}
		if value < 1 || value > length {
			return nil, fmt.Errorf("number out of range: %d", value)
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

// deleteSelected deletes the selected files and returns the remaining and deleted lists.
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
				// The file is already gone, treat it as cleaned up.
				deleted = append(deleted, file)
				continue
			}
			fmt.Printf("[ERROR] Failed to delete %s: %v\n", file, err)
			remaining = append(remaining, file)
			continue
		}

		deleted = append(deleted, file)
		fmt.Printf("[DELETE] %s\n", file)
	}

	return remaining, deleted
}

// readLine reads one line of input and returns the content read on EOF.
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
