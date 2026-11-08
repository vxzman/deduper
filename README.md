# dedup-cli

[![Go Version](https://img.shields.io/badge/Go-1.21+-blue.svg)](https://golang.org/)
[![License](https://img.shields.io/badge/License-BSD--3--Clause-green.svg)](LICENSE)
![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows%20%7C%20BSD-blue)

面向 Linux NAS 的声明式文件去重工具。通过 YAML 配置要扫描的目录，定期扫描后把重复文件结果保存在 bbolt 数据库里；需要清理时运行交互式命令手动选择要永久删除的文件。

## 特性

- **声明式配置** — 所有行为由 YAML 配置描述，适合 cron / systemd timer 定时执行
- **多目录统一比对** — 配置多个 `roots`，跨目录识别重复文件
- **增量扫描** — bbolt 持久化哈希缓存，文件未变化时直接命中
- **节省内存** — 并发扫描、按大小预过滤、Partial Hash 预筛、流式哈希与缓冲池
- **手动清理** — `delete` 交互式菜单选择文件，确认后永久删除，避免误删
- **缓存与结果分离语义清晰** — 哈希缓存和扫描结果存放在同一个数据库文件中

## 安装

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

## 配置

创建 `dedup.yaml`（相对路径会相对于配置文件所在目录解析）：

```yaml
# bbolt 数据库，同时保存哈希缓存与扫描结果
database: /var/lib/dedup/dedup.db

# 哈希算法：blake3 | sha256 | sha3
hash: blake3

# 并发数，0 表示使用 CPU 核心数
workers: 4

# 小于该字节数的文件不参与去重（默认 1，空文件会被跳过）
min_size: 1

# 要扫描的目录，重复文件会在所有目录之间统一比对
roots:
  - /volume1/photos
  - /volume1/backup

# 可选排除规则（glob），同时匹配完整路径与文件名
exclude:
  - "*.tmp"
  - "#recycle"
  - "@eaDir"
  - "Thumbs.db"
```

也可用环境变量指定配置路径：

```bash
export DEDUP_CONFIG=/etc/dedup/dedup.yaml
```

## 命令

```bash
# 扫描配置中的目录并把重复结果写入数据库
dedup scan --config /etc/dedup/dedup.yaml

# 查看上次扫描时间与所有重复组
dedup list --config /etc/dedup/dedup.yaml

# 交互式永久删除重复文件
dedup delete --config /etc/dedup/dedup.yaml

# 清除哈希缓存与扫描结果
dedup clear --config /etc/dedup/dedup.yaml

# 查看帮助
dedup --help
```

`--config` 默认值为 `dedup.yaml`，也可以放在子命令后面：

```bash
dedup scan --config ./dedup.yaml
```

### 交互式删除

`delete` 会先列出所有重复组，选择组编号后再选择要删除的文件编号：

```
[1] 1.2 MiB  3 个文件  hash=977e1f52fda8

输入要处理的组编号，q 退出：1

组 [1] 1.2 MiB，3 个文件：
  1) /volume1/photos/a.jpg
  2) /volume1/backup/a.jpg
  3) /volume1/photos/copy/a.jpg
输入要永久删除的文件编号（逗号分隔，回车取消）：2,3
确认永久删除以上文件？输入 yes 确认：yes
```

安全约束：

- 每组至少保留一个文件，不能一次删除整组
- 必须输入 `yes` 才会执行删除
- 删除是永久的，不进入回收站
- 已不存在的文件会被视为已清理

## 定期扫描

由系统定时器调用 `scan` 即可，工具本身不常驻后台。

**cron**

```cron
# 每天 03:30 扫描
30 3 * * * /usr/local/bin/dedup scan --config /etc/dedup/dedup.yaml >> /var/log/dedup.log 2>&1
```

**systemd timer**

`/etc/systemd/system/dedup.service`：

```ini
[Unit]
Description=dedup scan
After=local-fs.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/dedup scan --config /etc/dedup/dedup.yaml
```

`/etc/systemd/system/dedup.timer`：

```ini
[Unit]
Description=Run dedup scan daily

[Timer]
OnCalendar=daily
Persistent=true

[Install]
WantedBy=timers.target
```

```bash
systemctl enable --now dedup.timer
```

## 工作原理

```
读取 YAML 配置
      │
      ▼
并发扫描 roots（应用 exclude）
      │
      ▼
按文件大小分组 ──► 查询哈希缓存 ──► 未命中则流式计算哈希
      │
      ▼
按哈希归组，得到重复组
      │
      ▼
写入 bbolt（results + 元数据）
      │
      ├── dedup list    查看结果
      ├── dedup delete  交互式永久删除
      └── dedup clear   清空缓存与结果
```

### 哈希算法对比

| 算法 | 速度 | 安全性 | 推荐场景 |
|------|------|--------|----------|
| BLAKE3 | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | 日常使用（默认） |
| SHA256 | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | 高安全需求 |
| SHA3 | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ | 最高安全标准 |

缓存条目会记录哈希算法，切换 `hash` 后不会误用旧算法的结果。

## 开发

```bash
go mod tidy
go build ./...
go vet ./...
go test ./...
```

## 许可证

BSD 3-Clause License - 详见 [LICENSE](LICENSE)

## 致谢

- [BLAKE3](https://github.com/BLAKE3-team/BLAKE3)
- [bbolt](https://github.com/etcd-io/bbolt)
- [yaml.v3](https://gopkg.in/yaml.v3)
