#!/usr/bin/env bash
# 一键配置 BGGo 本地 Git Hooks
set -e

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "正在配置 Git hooks..."
git config core.hooksPath .githooks
chmod +x .githooks/*

# 同时为习惯原生 .git/hooks 的环境做兼容
if [ -d ".git/hooks" ]; then
    cp .githooks/pre-commit .git/hooks/pre-commit
    chmod +x .git/hooks/pre-commit
fi

echo "✅ Git hooks 配置成功！"
echo "  - 提交代码时会自动调用 gofmt 格式化暂存的 Go 文件"
echo "  - 提交版本更新时会自动校验 mygo.json、package.json 和 main.go 版本一致性"
