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
	configPath = flag.String("config", "dedup.yaml", "配置文件路径")
	helpFlag   = flag.Bool("help", false, "显示帮助")
)

// Execute 是程序入口，返回退出码。
func Execute() {
	os.Exit(execute())
}

func execute() int {
	// Go 标准库 flag 在遇到第一个非 flag 参数后会停止解析，
	// 这里先重排参数，保证 `dedup scan --config x.yaml` 也能生效。
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
		fmt.Printf("[ERROR] 打开数据库失败：%v\n", err)
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
		fmt.Printf("[ERROR] 未知命令：%s\n\n", args[0])
		printHelp()
		return 1
	}

	return 0
}

// reorderArgs 将 flag 与位置参数重新排列，确保位于位置参数之后的 flag 也能被解析。
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
	fmt.Println(`dedup 声明式文件去重工具

用法:
  dedup scan   [--config <file>]  扫描配置中的目录并保存重复文件结果
  dedup list   [--config <file>]  列出已保存的重复文件
  dedup delete [--config <file>]  交互式永久删除重复文件
  dedup clear  [--config <file>]  清除哈希缓存与扫描结果
  dedup --help                    显示帮助

选项:
  --config <file>   配置文件路径，默认 dedup.yaml，也可用 DEDUP_CONFIG 指定
  --help            显示帮助

配置文件示例 (YAML):
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

定期扫描可交由 cron 或 systemd timer 执行:
  dedup scan --config /etc/dedup/dedup.yaml`)
}
