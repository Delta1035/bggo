import * as vscode from "vscode";
import {
  applyWorkbenchPatch,
  removeWorkbenchPatch,
  stylesheet,
} from "./patcher";
import {
  isWorkbenchPermissionError,
  prepareWorkbenchPermissions,
  repairWorkbenchPermissions,
} from "./permissions";

const cssImportsKey = "vscode_custom_css.imports";
const managedStyleFile = "bggo-wallpaper.css";
const managedScriptFile = "bggo-wallpaper.js";

export function activate(context: vscode.ExtensionContext): void {
  context.subscriptions.push(
    vscode.commands.registerCommand("bggo.selectWallpaper", () => selectWallpaper(context)),
    vscode.commands.registerCommand("bggo.refreshWallpaper", () => refreshWallpaper(context, true)),
    vscode.commands.registerCommand("bggo.disableWallpaper", () => disableWallpaper(context)),
    vscode.commands.registerCommand("bggo.repairPermissions", () => repairWorkbenchPermissions(true)),
    vscode.workspace.onDidChangeConfiguration((event) => {
      if (event.affectsConfiguration("bggo.wallpaper.path")) {
        void refreshWallpaper(context, true);
      } else if (
        event.affectsConfiguration("bggo.wallpaper.opacity") ||
        event.affectsConfiguration("bggo.wallpaper.blur") ||
        event.affectsConfiguration("bggo.wallpaper.position")
      ) {
        void refreshWallpaper(context, false);
      }
    }),
  );

  const initialPath = vscode.workspace.getConfiguration("bggo.wallpaper").get<string>("path", "").trim();
  if (initialPath) {
    void refreshWallpaper(context, false);
  }
}

async function selectWallpaper(context: vscode.ExtensionContext): Promise<void> {
  const selected = await vscode.window.showOpenDialog({
    title: "选择 VS Code 壁纸",
    canSelectMany: false,
    canSelectFiles: true,
    canSelectFolders: false,
    filters: { 图片: ["jpg", "jpeg", "png", "webp", "bmp", "gif"] },
  });
  if (!selected?.[0]) {
    return;
  }

  await vscode.workspace
    .getConfiguration("bggo.wallpaper")
    .update("path", selected[0].fsPath, vscode.ConfigurationTarget.Global);
  await refreshWallpaper(context, true);
}

async function refreshWallpaper(context: vscode.ExtensionContext, notify: boolean): Promise<void> {
  const config = vscode.workspace.getConfiguration("bggo.wallpaper");
  const path = config.get<string>("path", "").trim();
  if (!path) {
    if (notify) {
      void vscode.window.showInformationMessage("请先运行“BGGo: 选择 VS Code 壁纸”。");
    }
    return;
  }

  const imageUri = workbenchImageURI(path);
  const opacity = clamp(config.get<number>("opacity", 0.16), 0.03, 1);
  const blur = clamp(config.get<number>("blur", 0), 0, 30);
  const position = safePosition(config.get<string>("position", "center"));

  const styleUri = vscode.Uri.joinPath(context.globalStorageUri, managedStyleFile);
  const scriptUri = vscode.Uri.joinPath(context.globalStorageUri, managedScriptFile);

  const css = stylesheet(imageUri, opacity, blur, position);

  await vscode.workspace.fs.createDirectory(context.globalStorageUri);
  await vscode.workspace.fs.writeFile(styleUri, Buffer.from(css, "utf8"));

  // 清理 settings.json 中遗留的 Custom CSS 导入项，避免第三方插件破坏 CSP
  await removeLegacyImports(scriptUri, styleUri);

  await reloadWallpaper(css, notify);
}

async function disableWallpaper(context: vscode.ExtensionContext): Promise<void> {
  const styleUri = vscode.Uri.joinPath(context.globalStorageUri, managedStyleFile);
  const scriptUri = vscode.Uri.joinPath(context.globalStorageUri, managedScriptFile);

  await removeLegacyImports(scriptUri, styleUri);

  if (!(await prepareWorkbenchPermissions())) {
    return;
  }
  try {
    await removeWorkbenchPatch();
  } catch (error) {
    if (isWorkbenchPermissionError(error) && (await repairWorkbenchPermissions(true))) {
      await removeWorkbenchPatch();
    }
  }

  const action = await vscode.window.showInformationMessage(
    "BGGo 壁纸已关闭，重新加载窗口后生效。",
    "重新加载",
  );
  if (action === "重新加载") {
    await vscode.commands.executeCommand("workbench.action.reloadWindow");
  }
}

async function removeLegacyImports(scriptUri: vscode.Uri, legacyStyleUri: vscode.Uri): Promise<void> {
  const settings = vscode.workspace.getConfiguration();
  const imports = globalImports(settings);
  if (!imports || imports.length === 0) {
    return;
  }
  const managed = managedImportURIs(scriptUri, legacyStyleUri);
  const next = imports.filter((item) => !managed.includes(item));
  if (next.length !== imports.length) {
    await settings.update(cssImportsKey, next, vscode.ConfigurationTarget.Global);
  }
}

function managedImportURIs(...uris: vscode.Uri[]): string[] {
  return uris.flatMap((uri) => [vscode.Uri.file(uri.fsPath).toString(), uri.toString()]);
}

function workbenchImageURI(path: string): string {
  return vscode.Uri.file(path).toString().replace("file://", "vscode-file://vscode-app");
}

async function reloadWallpaper(css: string, notify: boolean): Promise<void> {
  if (!(await prepareWorkbenchPermissions())) {
    return;
  }
  try {
    await applyWorkbenchPatch(css);
  } catch (error) {
    if (!isWorkbenchPermissionError(error)) {
      const detail = error instanceof Error ? error.message : String(error);
      void vscode.window.showErrorMessage(`未能更新壁纸样式：${detail}`);
      return;
    }
    if (!(await repairWorkbenchPermissions(true))) {
      return;
    }
    try {
      await applyWorkbenchPatch(css);
    } catch (retryError) {
      const detail = retryError instanceof Error ? retryError.message : String(retryError);
      void vscode.window.showErrorMessage(`权限修复后仍无法更新壁纸样式：${detail}`);
      return;
    }
  }

  if (notify) {
    const action = await vscode.window.showInformationMessage(
      "BGGo 壁纸样式已更新（原生安全策略已完整保留），重新加载窗口后生效。",
      "重新加载",
    );
    if (action === "重新加载") {
      await vscode.commands.executeCommand("workbench.action.reloadWindow");
    }
  }
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function safePosition(value: string): string {
  const allowed = new Set(["center", "top", "bottom", "left", "right"]);
  return allowed.has(value) ? value : "center";
}

function globalImports(settings: vscode.WorkspaceConfiguration): string[] {
  return settings.inspect<string[]>(cssImportsKey)?.globalValue ?? [];
}

export function deactivate(): void {}
