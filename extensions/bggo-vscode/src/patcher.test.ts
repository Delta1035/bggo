import assert from "node:assert/strict";
import { test } from "node:test";
import {
  BGGO_END_TAG,
  BGGO_START_TAG,
  DEFAULT_CSP,
  cleanPatches,
  ensureCSP,
  patchHTMLContent,
  stylesheet,
  unpatchHTMLContent,
} from "./patcher";

const mockOfficialHTML = `<!-- Copyright (C) Microsoft Corporation. All rights reserved. -->
<!DOCTYPE html>
<html>
	<head>
		<meta charset="utf-8" />
		<meta
			http-equiv="Content-Security-Policy"
			content="
				default-src
					'none'
				;
				img-src
					'self'
					data:
					blob:
					vscode-remote-resource:
					vscode-managed-remote-resource:
					https:
				;
				media-src
					'self'
				;
				frame-src
					'self'
					vscode-webview:
				;
				script-src
					'self'
					'unsafe-eval'
					blob:
				;
				style-src
					'self'
					'unsafe-inline'
				;
		"/>

		<!-- Workbench CSS -->
		<link rel="stylesheet" href="../../../workbench/workbench.desktop.main.css">
	</head>

	<body aria-label="">
	</body>

	<script src="./workbench.js" type="module"></script>
</html>`;

test("stylesheet generates correct pure CSS targeting body::after", () => {
  const css = stylesheet("vscode-file://vscode-app/home/ubuntu/pic.jpg", 0.2, 5, "top");
  assert.ok(css.includes("body::after"), "CSS should target body::after");
  assert.ok(css.includes('url("vscode-file://vscode-app/home/ubuntu/pic.jpg")'), "CSS should contain image URL");
  assert.ok(css.includes("opacity: 0.2"), "CSS should contain opacity");
  assert.ok(css.includes("filter: blur(5px)"), "CSS should contain blur filter");
  assert.ok(css.includes("background-position: top"), "CSS should contain position");
  assert.ok(css.includes("pointer-events: none"), "CSS should have pointer-events none");
  assert.ok(!css.includes("<script>"), "CSS must not contain script tags");
});

test("patchHTMLContent preserves official CSP while injecting CSS into head", () => {
  const css = stylesheet("vscode-file://vscode-app/home/ubuntu/pic.jpg", 0.16, 0, "center");
  const patched = patchHTMLContent(mockOfficialHTML, css);

  // 验证 CSP 完整保留
  assert.ok(patched.includes("http-equiv=\"Content-Security-Policy\""), "CSP must be preserved");
  assert.ok(patched.includes("vscode-webview:"), "frame-src vscode-webview: must be preserved");

  // 验证注入标签
  assert.ok(patched.includes(BGGO_START_TAG), "Should contain start tag");
  assert.ok(patched.includes(BGGO_END_TAG), "Should contain end tag");
  assert.ok(patched.includes('<style id="bggo-wallpaper-style">'), "Should contain style tag");
  assert.ok(patched.indexOf(BGGO_START_TAG) < patched.indexOf("</head>"), "Style must be in head before </head>");
});

test("patchHTMLContent automatically restores CSP if CSP was previously deleted by other tools", () => {
  const ruinedHTML = `<!DOCTYPE html><html><head><meta charset="utf-8" /></head><body></body></html>`;
  const css = stylesheet("vscode-file://vscode-app/test.png", 0.16, 0, "center");
  const patched = patchHTMLContent(ruinedHTML, css);

  assert.ok(patched.includes("http-equiv=\"Content-Security-Policy\""), "CSP must be restored");
  assert.ok(patched.includes("vscode-webview:"), "Restored CSP must support vscode-webview");
  assert.ok(patched.includes(BGGO_START_TAG));
});

test("cleanPatches removes prior BGGo patches and legacy custom-css artifacts", () => {
  const dirtyHTML = `<html><head>
<!-- !! VSCODE-CUSTOM-CSS-SESSION-ID 12345 !! -->
<!-- !! VSCODE-CUSTOM-CSS-START !! -->
<script>/* old script */</script>
<!-- !! VSCODE-CUSTOM-CSS-END !! -->
<!-- !! BGGO-WALLPAPER-START !! -->
<style id="bggo-wallpaper-style">old style</style>
<!-- !! BGGO-WALLPAPER-END !! -->
</head><body>
<script>/* eslint-env browser */ __CUSTOM_CSS_JS_INDICATOR_CLS </script>
</body></html>`;

  const cleaned = cleanPatches(dirtyHTML);
  assert.ok(!cleaned.includes("BGGO-WALLPAPER-START"));
  assert.ok(!cleaned.includes("VSCODE-CUSTOM-CSS-START"));
  assert.ok(!cleaned.includes("__CUSTOM_CSS_JS_INDICATOR_CLS"));
  assert.ok(!cleaned.includes("12345"));
});

test("unpatchHTMLContent cleans up injected styles and ensures CSP remains", () => {
  const css = stylesheet("vscode-file://vscode-app/test.png", 0.16, 0, "center");
  const patched = patchHTMLContent(mockOfficialHTML, css);
  const unpatched = unpatchHTMLContent(patched);

  assert.ok(!unpatched.includes(BGGO_START_TAG), "Start tag should be removed");
  assert.ok(!unpatched.includes(BGGO_END_TAG), "End tag should be removed");
  assert.ok(!unpatched.includes("bggo-wallpaper-style"), "Style tag should be removed");
  assert.ok(unpatched.includes("http-equiv=\"Content-Security-Policy\""), "CSP should still be intact");
});

test("locateWorkbench locates real workbench when appRoot exists", async () => {
  const { locateWorkbench } = await import("./patcher");
  const target = await locateWorkbench("/usr/share/code/resources/app");
  if (target) {
    assert.ok(target.html.endsWith("workbench.html"), "target html must end with workbench.html");
    assert.ok(target.directory.length > 0, "target directory must not be empty");
  }
});
