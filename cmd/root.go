package cmd

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"dedup/pkg/cache"
	"dedup/pkg/config"
)

var (
	configPath = flag.String("config", "dedup.yaml", "path to the configuration file")
	helpFlag   = flag.Bool("help", false, "show help")
)

// Execute is the program entry point; it returns the process exit code.
func Execute() {
	os.Exit(execute())
}

func execute() int {
	// The Go flag package stops parsing at the first non-flag argument,
	// so reorder arguments first to allow `dedup scan --config x.yaml`.
	flag.CommandLine.Parse(reorderArgs(os.Args[1:]))
	args := flag.Args()

	if *helpFlag || len(args) == 0 {
		printHelp()
		if *helpFlag || len(os.Args) == 1 {
			return 0
		}
		return 1
	}

	path := *configPath
	if env := os.Getenv("DEDUP_CONFIG"); env != "" && path == "dedup.yaml" {
		path = env
	}

	cfg, err := config.Load(path)
	if err != nil {
		fmt.Printf("[ERROR] %v\n", err)
		return 1
	}

	store, err := cache.NewCacheManager(cfg.Database, cfg.Quiet)
	if err != nil {
		fmt.Printf("[ERROR] Failed to open database: %v\n", err)
		return 1
	}
	defer store.Close()

	switch args[0] {
	case "scan":
		if err := runScan(cfg, store); err != nil {
			fmt.Printf("[ERROR] %v\n", err)
			return 1
		}
	case "list":
		if err := runList(cfg, store); err != nil {
			fmt.Printf("[ERROR] %v\n", err)
			return 1
		}
	case "delete":
		if err := runDelete(cfg, store); err != nil {
			fmt.Printf("[ERROR] %v\n", err)
			return 1
		}
	case "clear":
		if err := runClear(cfg, store); err != nil {
			fmt.Printf("[ERROR] %v\n", err)
			return 1
		}
	default:
		fmt.Printf("[ERROR] Unknown command: %s\n\n", args[0])
		printHelp()
		return 1
	}

	return 0
}

// reorderArgs moves flags before positional arguments so that flags placed
// after positional arguments are still parsed.
func reorderArgs(args []string) []string {
	valueFlags := map[string]bool{
		"config": true,
	}

	var flags []string
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}

		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}

		flags = append(flags, arg)

		name := strings.TrimLeft(arg, "-")
		if strings.ContainsRune(name, '=') {
			continue
		}

		if valueFlags[name] && i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}

	return append(flags, positional...)
}

func printHelp() {
	fmt.Println(`dedup - declarative duplicate file finder

Usage:
  dedup scan   [--config <file>]  Scan configured roots and save duplicate results
  dedup list   [--config <file>]  List saved duplicate files
  dedup delete [--config <file>]  Interactively delete duplicate files permanently
  dedup clear  [--config <file>]  Clear the hash cache and scan results
  dedup --help                    Show this help

Options:
  --config <file>   Path to the configuration file (default: dedup.yaml, or DEDUP_CONFIG)
  --help            Show this help

Example configuration (YAML):
  database: /var/lib/dedup/dedup.db
  hash: blake3
  workers: 4
  min_size: 1
  roots:
    - /volume1/photos
    - /volume1/backup
  exclude:
    - "*.tmp"
    - "@eaDir"

Periodic scans can be scheduled with cron or a systemd timer:
  dedup scan --config /etc/dedup/dedup.yaml`)
}
