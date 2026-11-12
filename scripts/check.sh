#!/usr/bin/env bash
# BGGo 本地全流程 CI 预检脚本
set -e

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "========================================================"
echo "🔍 开始 BGGo 本地预检 (模拟 CI 流程)..."
echo "========================================================"

# 1. 版本号一致性检查
echo "▶ [1/6] 检查项目各组件版本号一致性..."
python3 - <<'PY'
import json, re, sys
from pathlib import Path

mygo_ver = json.loads(Path("mygo.json").read_text(encoding="utf-8"))["version"]
vscode_ver = json.loads(Path("extensions/bggo-vscode/package.json").read_text(encoding="utf-8"))["version"]
main_go = Path("main.go").read_text(encoding="utf-8")
m = re.search(r'const AppVersion = "v?([0-9]+\.[0-9]+\.[0-9]+)"', main_go)
main_ver = m.group(1) if m else None

print(f"   mygo.json:                        {mygo_ver}")
print(f"   extensions/bggo-vscode/package:   {vscode_ver}")
print(f"   main.go (AppVersion):             {main_ver}")

if not (mygo_ver == vscode_ver == main_ver):
    print("❌ 错误: 组件版本号不一致！请运行 python3 scripts/bump-version.py <版本号> 统一对齐。", file=sys.stderr)
    sys.exit(1)
print("   ✓ 版本号一致。")
PY

# 2. Go 代码格式检查
echo "▶ [2/6] 检查 Go 代码格式 (gofmt)..."
files=$(git ls-files '*.go' | xargs gofmt -l)
if [ -n "$files" ]; then
    echo "⚠️ 以下文件未对齐 gofmt 规范："
    printf '%s\n' "$files"
    if [ "$1" = "--fix" ] || [ "$1" = "-f" ]; then
        echo "正在自动格式化..."
        echo "$files" | xargs gofmt -w
        echo "   ✓ 已自动完成格式化。"
    else
        echo "❌ 请执行 'gofmt -w <文件>' 或运行 './scripts/check.sh --fix' 修复格式。"
        exit 1
    fi
else
    echo "   ✓ Go 代码格式规范合格。"
fi

# 3. Go 依赖完整性与 vet 检查
echo "▶ [3/6] 执行 go mod verify 与 go vet..."
go mod verify
go vet ./...
if go tool mygo vet >/dev/null 2>&1; then
    go tool mygo vet
    echo "   ✓ mygo vet 通过。"
fi
echo "   ✓ go mod 与 vet 检查通过。"

# 4. Go 单元测试 (含竞态检测)
echo "▶ [4/6] 运行单元测试 (go test -race ./...)..."
go test -race ./...
echo "   ✓ 所有 Go 单元测试通过。"

# 5. VS Code 扩展检查与类型编译
echo "▶ [5/6] 检查 VS Code 扩展 (TypeScript 检查)..."
(
    cd extensions/bggo-vscode
    pnpm run check
    echo "   ✓ VS Code 扩展类型检查通过。"
)

# 6. VS Code 扩展打包与审计
echo "▶ [6/6] 试打包 VS Code 扩展并执行审计..."
(
    cd extensions/bggo-vscode
    pnpm run package
    echo "   ✓ VS Code 扩展打包成功。"
    
    # 审计执行，兼容国内镜像源无 bulk audit 接口的情况
    set +e
    audit_output=$(pnpm audit --prod 2>&1)
    audit_code=$?
    set -e
    if [ $audit_code -ne 0 ]; then
        if echo "$audit_output" | grep -q "ERR_PNPM_AUDIT_ENDPOINT_NOT_EXISTS"; then
            echo "   ⚠️ 当前使用的 npm 镜像源不支持安全审计端点，本地跳过 (CI 将在官方源运行并验证)。"
        else
            echo "$audit_output"
            exit $audit_code
        fi
    else
        echo "   ✓ 安全依赖审计通过。"
    fi
)

echo "========================================================"
echo "🎉 恭喜！本地预检全部通过，可以安全提交或发版！"
echo "========================================================"
