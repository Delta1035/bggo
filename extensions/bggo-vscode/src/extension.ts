import * as vscode from "vscode";
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
    vscode.commands.registerCommand("bggo.repairPermissions", () => repairWorkbenchPermissions()),
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

  await vscode.workspace.fs.createDirectory(context.globalStorageUri);
  await vscode.workspace.fs.writeFile(
    scriptUri,
    Buffer.from(wallpaperScript(imageUri, opacity, blur, position), "utf8"),
  );
  await addManagedImport(scriptUri, styleUri);
  await reloadCustomCSS(notify);
}

async function disableWallpaper(context: vscode.ExtensionContext): Promise<void> {
  const styleUri = vscode.Uri.joinPath(context.globalStorageUri, managedStyleFile);
  const scriptUri = vscode.Uri.joinPath(context.globalStorageUri, managedScriptFile);
  const settings = vscode.workspace.getConfiguration();
  const imports = globalImports(settings);
  const managed = managedImportURIs(scriptUri, styleUri);
  const next = imports.filter((item) => !managed.includes(item));
  await settings.update(cssImportsKey, next, vscode.ConfigurationTarget.Global);
  await reloadCustomCSS(true);
}

async function addManagedImport(scriptUri: vscode.Uri, legacyStyleUri: vscode.Uri): Promise<void> {
  const settings = vscode.workspace.getConfiguration();
  const imports = globalImports(settings);
  const managed = managedImportURIs(scriptUri, legacyStyleUri);
  const fileImport = vscode.Uri.file(scriptUri.fsPath).toString();
  const next = [...imports.filter((item) => !managed.includes(item)), fileImport];
  if (next.length !== imports.length || next.some((item, index) => item !== imports[index])) {
    await settings.update(cssImportsKey, next, vscode.ConfigurationTarget.Global);
  }
}

function managedImportURIs(...uris: vscode.Uri[]): string[] {
  // Custom CSS and JS Loader understands file: URLs, not VS Code's vscode-userdata: scheme.
  return uris.flatMap((uri) => [vscode.Uri.file(uri.fsPath).toString(), uri.toString()]);
}

function workbenchImageURI(path: string): string {
  // VS Code's sandbox blocks file: images; vscode-file: is its allowed local-resource scheme.
  return vscode.Uri.file(path).toString().replace("file://", "vscode-file://vscode-app");
}

async function reloadCustomCSS(notify: boolean): Promise<void> {
  if (!(await prepareWorkbenchPermissions())) {
    return;
  }
  try {
    await vscode.commands.executeCommand("extension.updateCustomCSS");
  } catch (error) {
    if (!isWorkbenchPermissionError(error)) {
      const detail = error instanceof Error ? error.message : String(error);
      void vscode.window.showErrorMessage(`Custom CSS and JS Loader 未能更新样式：${detail}`);
      return;
    }
    if (!(await repairWorkbenchPermissions(true))) {
      return;
    }
    try {
      await vscode.commands.executeCommand("extension.updateCustomCSS");
    } catch (retryError) {
      const detail = retryError instanceof Error ? retryError.message : String(retryError);
      void vscode.window.showErrorMessage(`权限修复后仍无法更新壁纸样式：${detail}`);
      return;
    }
  }
  if (notify) {
    const action = await vscode.window.showInformationMessage(
      "BGGo 壁纸样式已更新，重新加载窗口后生效。",
      "重新加载",
    );
    if (action === "重新加载") {
      await vscode.commands.executeCommand("workbench.action.reloadWindow");
    }
  }
}

function wallpaperScript(
  imageUri: string,
  opacity: number,
  blur: number,
  position: string,
): string {
  const style = {
    position: "fixed",
    inset: `-${blur * 2}px`,
    zIndex: "1001",
    pointerEvents: "none",
    backgroundImage: `url(${JSON.stringify(imageUri)})`,
    backgroundPosition: position,
    backgroundRepeat: "no-repeat",
    backgroundSize: "cover",
    opacity: String(opacity),
    filter: `blur(${blur}px)`,
  };
  return `/* Generated by BGGo Wallpaper. */
(() => {
  const render = () => {
    const id = "bggo-wallpaper-layer";
    const layer = document.getElementById(id) || document.body.appendChild(document.createElement("div"));
    layer.id = id;
    Object.assign(layer.style, ${JSON.stringify(style)});
  };
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", render, { once: true });
  } else {
    render();
  }
})();
`;
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

