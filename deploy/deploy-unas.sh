#!/bin/bash
set -euo pipefail

HOST="${1:-unas}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/dist/dedup-linux-amd64"
STAGE="/tmp/dedup-deploy"

mkdir -p "$ROOT/dist"

echo "==> 构建 linux/amd64 静态二进制"
(
	cd "$ROOT"
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -trimpath -ldflags="-s -w" -o "$BIN" .
)

echo "==> 上传到临时目录"
ssh "$HOST" "rm -rf $STAGE && mkdir -p $STAGE"
scp "$BIN" "$HOST:$STAGE/dedup"
scp "$ROOT/deploy/dedup.yaml" "$HOST:$STAGE/dedup.yaml"
scp "$ROOT/deploy/dedup.service" "$HOST:$STAGE/dedup.service"
scp "$ROOT/deploy/dedup.timer" "$HOST:$STAGE/dedup.timer"

echo "==> 安装到系统目录并启用 timer"
ssh "$HOST" "
	set -e
	doas install -d /etc/dedup
	doas install -m 0755 $STAGE/dedup /usr/local/bin/dedup
	doas install -m 0644 $STAGE/dedup.yaml /etc/dedup/dedup.yaml
	doas install -m 0644 $STAGE/dedup.service /etc/systemd/system/dedup.service
	doas install -m 0644 $STAGE/dedup.timer /etc/systemd/system/dedup.timer
	doas systemctl daemon-reload
	doas systemctl enable --now dedup.timer
	rm -rf $STAGE
	doas systemctl list-timers dedup.timer --no-pager
"

echo "==> 清理旧用户级部署"
ssh "$HOST" '
	systemctl --user disable --now dedup.timer >/dev/null 2>&1 || true
	rm -f ~/.config/systemd/user/dedup.service ~/.config/systemd/user/dedup.timer
	rm -f ~/.local/bin/dedup ~/.config/dedup/dedup.yaml
	rm -rf ~/.local/share/dedup
	rmdir ~/.config/dedup 2>/dev/null || true
	systemctl --user daemon-reload >/dev/null 2>&1 || true
	loginctl disable-linger svmgr >/dev/null 2>&1 || true
'

echo "==> 部署完成"
