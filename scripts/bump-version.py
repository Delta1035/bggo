#!/usr/bin/env python3
"""
统一升级 BGGo 全项目组件版本号。
同步更新：
  - mygo.json: "version": "X.Y.Z"
  - extensions/bggo-vscode/package.json: "version": "X.Y.Z"
  - main.go: const AppVersion = "vX.Y.Z"
"""
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

def parse_version(raw: str) -> str:
    raw = raw.strip()
    match = re.fullmatch(r'v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)', raw)
    if not match:
        raise SystemExit(f"错误: 无效的版本格式 '{raw}'。必须为标准 semver，例如 0.2.8 或 v0.2.8")
    return f"{match[1]}.{match[2]}.{match[3]}"

def update_mygo_json(version: str) -> None:
    path = ROOT / "mygo.json"
    data = json.loads(path.read_text(encoding="utf-8"))
    old = data.get("version", "unknown")
    data["version"] = version
    path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"  ✓ mygo.json: {old} -> {version}")

def update_vscode_json(version: str) -> None:
    path = ROOT / "extensions" / "bggo-vscode" / "package.json"
    data = json.loads(path.read_text(encoding="utf-8"))
    old = data.get("version", "unknown")
    data["version"] = version
    path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"  ✓ extensions/bggo-vscode/package.json: {old} -> {version}")

def update_main_go(version: str) -> None:
    path = ROOT / "main.go"
    content = path.read_text(encoding="utf-8")
    pattern = r'const AppVersion = "v[0-9]+\.[0-9]+\.[0-9]+"'
    replacement = f'const AppVersion = "v{version}"'
    if not re.search(pattern, content):
        raise SystemExit(f"错误: 未在 {path} 中找到匹配 'const AppVersion = \"vX.Y.Z\"' 的代码")
    new_content = re.sub(pattern, replacement, content, count=1)
    path.write_text(new_content, encoding="utf-8")
    print(f"  ✓ main.go: AppVersion -> v{version}")

def main() -> None:
    if len(sys.argv) < 2:
        print("用法: python3 scripts/bump-version.py <新版本号>")
        print("示例: python3 scripts/bump-version.py 0.2.8 或 v0.2.8")
        sys.exit(1)

    version = parse_version(sys.argv[1])
    print(f"正在将全项目组件版本号统一更新为 v{version} ({version})...")
    update_mygo_json(version)
    update_vscode_json(version)
    update_main_go(version)
    print("\n版本更新完成！请核对更改后执行提交与打标签：")
    print(f'  git commit -m "chore(release): 升级版本至 v{version}"')
    print(f'  git tag v{version}')

if __name__ == "__main__":
    main()
