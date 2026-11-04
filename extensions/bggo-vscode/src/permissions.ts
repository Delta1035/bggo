import { execFile } from "node:child_process";
import { constants } from "node:fs";
import { access, realpath, stat } from "node:fs/promises";
import { userInfo } from "node:os";
import * as path from "node:path";
import * as vscode from "vscode";

type WorkbenchTarget = {
  directory: string;
  html: string;
};

const authorizeAction = "一键授权并继续";
const copyAction = "复制手动命令";

export async function prepareWorkbenchPermissions(): Promise<boolean> {
  if (process.platform !== "linux") {
    return true;
  }
  const target = await locateWorkbench();
  if (!target || (await canModify(target))) {
    return true;
  }
  return requestPermission(target);
}

export async function repairWorkbenchPermissions(force = false): Promise<boolean> {
  if (process.platform !== "linux") {
    void vscode.window.showInformationMessage("当前系统不需要 Linux 工作台权限修复。");
    return false;
  }
  const target = await locateWorkbench();
  if (!target) {
    void vscode.window.showErrorMessage("BGGo 无法定位当前 VS Code 的工作台目录。");
    return false;
  }
  if (!force && (await canModify(target))) {
    void vscode.window.showInformationMessage("VS Code 壁纸目录权限正常，无需修复。");
    return true;
  }
  return requestPermission(target);
}

export function isWorkbenchPermissionError(error: unknown): boolean {
  if (typeof error === "object" && error !== null && "code" in error) {
    const code = String((error as { code?: unknown }).code);
    if (code === "EACCES" || code === "EPERM") {
      return true;
    }
  }
  const message = error instanceof Error ? error.message : String(error);
  return /EACCES|EPERM|permission denied/i.test(message);
}

async function locateWorkbench(): Promise<WorkbenchTarget | undefined> {
  const appRoot = await realpath(vscode.env.appRoot);
  const base = path.join(appRoot, "out", "vs", "code");
  const directories = [
    path.join(base, "electron-browser", "workbench"),
    path.join(base, "electron-browser"),
    path.join(base, "electron-sandbox", "workbench"),
    path.join(base, "electron-sandbox"),
  ];
  const htmlNames = [
    "workbench-dev.html",
    "workbench.esm.html",
    "workbench.html",
    "workbench-apc-extension.html",
  ];

  for (const directory of directories) {
    for (const htmlName of htmlNames) {
      const html = path.join(directory, htmlName);
      try {
        if (!(await stat(html)).isFile()) {
          continue;
        }
        const realDirectory = await realpath(directory);
        const realHTML = await realpath(html);
        assertInside(appRoot, realDirectory);
        assertInside(realDirectory, realHTML);
        return {
          directory: realDirectory,
          html: realHTML,
        };
      } catch {
        // Try the next path used by supported VS Code versions and forks.
      }
    }
  }
  return undefined;
}

function assertInside(parent: string, child: string): void {
  const relative = path.relative(parent, child);
  if (relative === "") {
    return;
  }
  if (relative === ".." || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
    throw new Error("工作台路径不在 VS Code 安装目录内");
  }
}

async function canModify(target: WorkbenchTarget): Promise<boolean> {
  try {
    await Promise.all([
      access(target.directory, constants.R_OK | constants.W_OK | constants.X_OK),
      access(target.html, constants.R_OK | constants.W_OK),
    ]);
    return true;
  } catch {
    return false;
  }
}

async function requestPermission(target: WorkbenchTarget): Promise<boolean> {
  const choice = await vscode.window.showWarningMessage(
    "启用 VS Code 壁纸需要一次系统授权",
    {
      modal: true,
      detail:
        "BGGo 只会为当前用户开放 VS Code 工作台目录和入口文件，不会修改其他系统目录，也不会保存密码。",
    },
    authorizeAction,
    copyAction,
  );
  if (choice === copyAction) {
    await copyManualCommand(target);
    return false;
  }
  if (choice !== authorizeAction) {
    return false;
  }

  const pkexec = await findExecutable(["/usr/bin/pkexec", "/bin/pkexec"]);
  const setfacl = await findExecutable(["/usr/bin/setfacl", "/bin/setfacl"]);
  if (!pkexec || !setfacl) {
    await copyManualCommand(target);
    void vscode.window.showErrorMessage(
      "系统缺少 pkexec 或 setfacl，手动修复命令已复制到剪贴板。Debian/Ubuntu 可先安装 acl 软件包。",
    );
    return false;
  }

  const uid = userInfo().uid;
  try {
    // The fixed file is processed before its parent becomes writable, in one authorization.
    await run(pkexec, [setfacl, "-m", `u:${uid}:rwx`, target.html, target.directory]);
  } catch (error) {
    const detail = error instanceof Error ? error.message : String(error);
    void vscode.window.showErrorMessage(`系统授权已取消或执行失败：${detail}`);
    return false;
  }

  const refreshed = await locateWorkbench();
  if (!refreshed || !(await canModify(refreshed))) {
    void vscode.window.showErrorMessage("授权命令已完成，但工作台目录仍不可写。请使用手动修复命令。");
    return false;
  }
  void vscode.window.showInformationMessage("BGGo 权限修复完成，正在继续应用壁纸。");
  return true;
}

async function copyManualCommand(target: WorkbenchTarget): Promise<void> {
  const uid = userInfo().uid;
  const command = `sudo setfacl -m u:${uid}:rwx ${shellQuote(target.html)} ${shellQuote(target.directory)}`;
  await vscode.env.clipboard.writeText(command);
  void vscode.window.showInformationMessage("BGGo 权限修复命令已复制到剪贴板。");
}

async function findExecutable(candidates: string[]): Promise<string | undefined> {
  for (const candidate of candidates) {
    try {
      await access(candidate, constants.X_OK);
      return candidate;
    } catch {
      // Keep looking in the small list of fixed system paths.
    }
  }
  return undefined;
}

function run(executable: string, args: string[]): Promise<void> {
  return new Promise((resolve, reject) => {
    execFile(executable, args, { timeout: 120_000, windowsHide: true }, (error, _stdout, stderr) => {
      if (!error) {
        resolve();
        return;
      }
      const detail = stderr.trim();
      reject(new Error(detail ? `${error.message}: ${detail}` : error.message));
    });
  });
}

function shellQuote(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}
