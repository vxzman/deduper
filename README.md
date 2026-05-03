# dedup-cli

[![Go Version](https://img.shields.io/badge/Go-1.21+-blue.svg)](https://golang.org/)
[![License](https://img.shields.io/badge/License-BSD--3--Clause-green.svg)](LICENSE)
![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20Linux%20%7C%20macOS%20%7C%20FreeBSD%20%7C%20OpenBSD%20%7C%20NetBSD-blue)

高性能跨平台文件去重工具，采用并发扫描、智能缓存与流式哈希计算，快速识别并处理重复文件。

## 特性

- **并发目录扫描** — 基于 `errgroup` 的多协程遍历，大幅提升碎片化目录扫描速度
- **智能预过滤** — 按文件大小分组 + Partial Hash 快速筛选，避免不必要的全量哈希计算
- **流式哈希计算** — 1MB 缓冲区流式读取，大文件不 OOM，支持 BLAKE3/SHA256/SHA3
- **异步缓存系统** — BoltDB 持久化 + 批量异步写入，重复扫描秒级响应
- **内存池优化** — `sync.Pool` 复用缓冲区，降低 GC 压力
- **安全重命名** — `dep_` 前缀重命名，冲突自动编号，原子操作
- **跨平台支持** — Windows/Linux/macOS/FreeBSD/OpenBSD/NetBSD，amd64/arm64 架构

## 安装

### 从源码构建

```bash
git clone https://github.com/vxzman/deduper.git
cd deduper
go mod tidy

# Linux/macOS
./build.sh

# Windows PowerShell
.\build.ps1

# 或手动构建（当前平台）
go build -trimpath -ldflags="-s -w" -o dedup main.go
```

### 二进制文件

构建完成后，可执行文件位于 `dist/` 目录：

| 系统 | 文件名 |
|------|--------|
| Linux | `dedup-linux-amd64` / `dedup-linux-arm64` |
| macOS | `dedup-darwin-amd64` / `dedup-darwin-arm64` |
| Windows | `dedup-windows-amd64.exe` / `dedup-windows-arm64.exe` |

## 快速开始

```bash
# 查看帮助
./dedup --help

# 单文件夹去重
./dedup --dedup ~/Downloads

# 双文件夹比较
./dedup --compare ./backup/old ./backup/new

# 预览模式（不实际修改）
./dedup --dedup ./photos --dry-run

# 指定哈希算法和并发数
./dedup --dedup ./videos --hash blake3 --workers 8
```

## 命令参考

### 基本用法

```
dedup 文件去重工具

用法:
  dedup --dedup <folder> [选项]     单文件夹去重
  dedup --compare <folder1> <folder2> [选项]  双文件夹比较
  dedup --clear-cache               清除缓存
  dedup --help                      显示帮助

选项:
  --dedup         单文件夹去重模式
  --compare       双文件夹比较模式
  --hash          哈希算法 (blake3|sha256|sha3) [默认：blake3]
  --workers       并发数 (默认：CPU 核心数)
  --dry-run       预览模式，仅显示操作
  --quiet         静默模式，仅输出错误
  --cache         缓存选项:
                  default - 使用默认缓存
                  none - 禁用缓存
                  <路径> - 自定义缓存文件
  --clear-cache   清除所有缓存
  --help          显示帮助
```

### 使用示例

**单文件夹去重**

```bash
# 基础用法
./dedup --dedup ./Downloads

# 使用 SHA256 算法
./dedup --dedup ./photos --hash sha256

# 禁用缓存（确保最新结果）
./dedup --dedup ./documents --cache none

# 静默运行（适合脚本）
./dedup --dedup ./temp --quiet
```

**双文件夹比较**

```bash
# 比较两个目录
./dedup --compare ./source ./backup

# 预览重复文件
./dedup --compare ./old ./new --dry-run
```

**缓存管理**

```bash
# 清除缓存
./dedup --clear-cache

# 使用自定义缓存文件
./dedup --dedup ./large_folder --cache /tmp/dedup_cache.db
```

## 工作原理

### 处理流程

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  并发扫描   │ ──► │  按大小分组  │ ──► │ Partial Hash│
│  目录文件   │     │  快速过滤   │     │  二次筛选   │
└─────────────┘     └─────────────┘     └─────────────┘
                                              │
                                              ▼
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  重命名处理  │ ◄── │  识别重复   │ ◄── │  全量哈希   │
│  dep_前缀   │     │  哈希比对   │     │  精确匹配   │
└─────────────┘     └─────────────┘     └─────────────┘
```

### 哈希算法对比

| 算法 | 速度 | 安全性 | 推荐场景 |
|------|------|--------|----------|
| BLAKE3 | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | 日常使用（默认） |
| SHA256 | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | 高安全需求 |
| SHA3 | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ | 最高安全标准 |

### 重命名规则

```
原始文件：report.pdf
首次重命名：dep_report.pdf
冲突处理：dep_report(1).pdf → dep_report(2).pdf → ...
```

## 性能优化

### 并发数调优

```bash
# 小文件密集型（照片/文档）
./dedup --dedup ./photos --workers 16

# 大文件密集型（视频/镜像）
./dedup --dedup ./videos --workers 4

# 混合类型（默认 CPU 核心数）
./dedup --dedup ./downloads
```

### 缓存策略

| 场景 | 推荐配置 |
|------|----------|
| 首次扫描 | `--cache default` |
| 确保最新 | `--cache none` |
| 频繁扫描同目录 | `--cache default` |
| 临时任务 | `--cache /tmp/dedup.db` |

### 基准测试

测试环境：Intel i7-12700K (12 核), 32GB RAM, NVMe SSD

| 文件类型 | 数量 | 总大小 | 耗时 | 内存 |
|----------|------|--------|------|------|
| 照片 (JPG) | 10,000 | 25GB | ~2.5 分钟 | <50MB |
| 文档 (PDF) | 5,000 | 15GB | ~1.5 分钟 | <40MB |
| 视频 (MP4) | 500 | 50GB | ~8 分钟 | <70MB |

## 故障排除

**路径不存在**
```bash
# 使用绝对路径
./dedup --dedup $(pwd)/relative/path
```

**权限不足**
```bash
# 检查权限或使用 sudo
sudo ./dedup --dedup /restricted/path
```

**内存不足**
```bash
# 减少并发数
./dedup --dedup ./large_folder --workers 2
```

**哈希计算失败**
```bash
# 文件可能被占用，稍后重试
# 或检查磁盘空间
df -h
```

## 开发

```bash
# 安装依赖
go mod tidy

# 运行测试
go test ./...

# 代码检查
go vet ./...

# 构建（带优化）
go build -trimpath -ldflags="-s -w" -o dedup main.go
```

## 许可证

BSD 3-Clause License - 详见 [LICENSE](LICENSE) 文件

## 致谢

- [BLAKE3](https://github.com/BLAKE3-team/BLAKE3) - 高性能哈希算法
- [bbolt](https://github.com/etcd-io/bbolt) - 嵌入式 KV 存储
- [golang.org/x/sync](https://pkg.go.dev/golang.org/x/sync) - 并发原语
