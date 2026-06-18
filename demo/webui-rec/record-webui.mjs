// Record a walkthrough video of the cct desktop WebUI with Playwright.
//
// Safe: it launches `cct app` against a FAKE Codex home (CODEX_HOME), never the
// real ~/.codex. The only files it writes are a demo bundle in this folder's
// out/ dir and the recorded video.
//
//   node record-webui.mjs
//
// Env overrides:
//   CCT_EXE      path to cct.exe         (default: %USERPROFILE%\go\bin\cct.exe)
//   CCT_HOME     fake Codex home to show (default: the generated WebUI demo home)
import { chromium } from "playwright";
import { spawn } from "node:child_process";
import { mkdirSync, rmSync } from "node:fs";
import { homedir } from "node:os";
import path from "node:path";

const EXE = process.env.CCT_EXE || path.join(homedir(), "go", "bin", "cct.exe");
const HOME = process.env.CCT_HOME ||
  "C:\\Users\\faruk\\cct-webui-demo\\laptop\\codex-home";
const HERE = path.dirname(new URL(import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1"));
const OUT = path.join(HERE, "out");
const PORT = 7799;

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function startApp() {
  return new Promise((resolve, reject) => {
    const child = spawn(EXE, ["app", "--no-browser", "--port", String(PORT)], {
      env: { ...process.env, CODEX_HOME: HOME },
    });
    let buf = "";
    const onData = (d) => {
      buf += d.toString();
      const m = buf.match(/http:\/\/127\.0\.0\.1:\d+\/\?token=\S+/);
      if (m) resolve({ child, url: m[0] });
    };
    child.stdout.on("data", onData);
    child.stderr.on("data", onData);
    child.on("exit", (c) => reject(new Error("cct app exited early: " + c)));
    setTimeout(() => reject(new Error("timed out waiting for app URL")), 10000);
  });
}

// Type into a field slowly so it reads naturally on camera.
async function typeSlow(page, sel, text) {
  await page.click(sel);
  await page.fill(sel, "");
  await page.type(sel, text, { delay: 35 });
}

async function main() {
  rmSync(OUT, { recursive: true, force: true });
  mkdirSync(OUT, { recursive: true });

  const { child, url } = await startApp();
  console.log("app URL:", url);

  const browser = await chromium.launch();
  const context = await browser.newContext({
    viewport: { width: 1280, height: 820 },
    recordVideo: { dir: OUT, size: { width: 1280, height: 820 } },
    deviceScaleFactor: 2,
  });
  const page = await context.newPage();

  try {
    await page.goto(url, { waitUntil: "networkidle" });
    await sleep(1200);

    // --- Doctor ---------------------------------------------------------
    await page.click("#doctor-refresh");
    await sleep(2200);

    // --- Sessions (grouped) --------------------------------------------
    await page.click('.nav[data-view="sessions"]');
    await sleep(600);
    await page.click("#sessions-refresh");
    await sleep(2600);
    await page.mouse.wheel(0, 320);
    await sleep(1500);
    await page.mouse.wheel(0, -320);

    // --- Export (show the form + git clarity hints) --------------------
    await page.click('.nav[data-view="export"]');
    await sleep(800);
    await page.selectOption("#export-mode", "all");
    await sleep(900);
    await typeSlow(page, "#export-output", path.join(OUT, "demo.codexbundle"));
    await sleep(500);
    // reveal the git-action checkboxes + their explanatory hints
    await page.check("#export-withgit");
    await sleep(900);
    await page.mouse.wheel(0, 260);
    await sleep(1400);
    await page.click("#export-run");
    await sleep(2400);

    // --- Import (preview/dry-run + the clone hint) ---------------------
    await page.click('.nav[data-view="import"]');
    await sleep(800);
    await typeSlow(page, "#import-path", path.join(OUT, "demo.codexbundle"));
    await sleep(500);
    await page.click("#import-preview");
    await sleep(2400);
    await page.mouse.wheel(0, 360);
    await sleep(1800);
    await page.mouse.wheel(0, 360);
    await sleep(1800);
  } finally {
    await context.close(); // flushes the video
    await browser.close();
    child.kill();
  }
  console.log("video written under", OUT);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
