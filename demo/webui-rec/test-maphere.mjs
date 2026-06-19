// Headless end-to-end test of the WebUI "Put these sessions under the current
// folder" (--map-cwd-here) checkbox. Safe: uses a FAKE Codex home in a temp dir,
// never the real ~/.codex. Exits non-zero on any failed assertion.
import { chromium } from "playwright";
import { spawn, execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

const EXE = process.env.CCT_EXE;            // path to freshly built cct.exe
const PORT = 7811;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const ok = (c, m) => { if (!c) { console.error("FAIL: " + m); process.exitCode = 1; throw new Error(m); } console.log("PASS: " + m); };

const root = mkdtempSync(path.join(tmpdir(), "cct-maphere-"));
const HOME_SRC = path.join(root, "src-home");  // bundle is built from here
const HOME_DST = path.join(root, "dst-home");  // empty; the server imports into this
const HERE = path.join(root, "current-project"); // the server's launch dir
const BUNDLE = path.join(root, "b.codexbundle");
const day = path.join(HOME_SRC, "sessions", "2026", "06", "13");
mkdirSync(day, { recursive: true });
mkdirSync(HOME_DST, { recursive: true });
mkdirSync(HERE, { recursive: true });

// One session recorded under a laptop path that does NOT exist on this machine.
const id = "abcd1234-1111-2222-3333-444455556666";
const line1 = JSON.stringify({ type: "session_meta", payload: { id, cwd: "/old/laptop/proj", source: "cli" } });
const line2 = JSON.stringify({ type: "event_msg", payload: { type: "user_message", message: "hello from the laptop" } });
writeFileSync(path.join(day, `rollout-2026-06-13T18-22-01-${id}.jsonl`), line1 + "\n" + line2 + "\n");

// Export a single-project bundle from the source home.
execFileSync(EXE, ["export", "--all", "--codex-home", HOME_SRC, "-o", BUNDLE]);

function startApp() {
  return new Promise((resolve, reject) => {
    // Launch the server FROM the HERE dir so os.Getwd() (the "current folder") is predictable.
    const child = spawn(EXE, ["app", "--no-browser", "--port", String(PORT)], {
      cwd: HERE, env: { ...process.env, CODEX_HOME: HOME_DST },
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
    setTimeout(() => reject(new Error("timeout waiting for app URL")), 8000);
  });
}

const { child, url } = await startApp();
const browser = await chromium.launch();
try {
  const page = await browser.newPage();
  await page.goto(url);

  // Go to the Import view, enter the bundle, run the dry-run preview.
  await page.click('button.nav[data-view="import"]');
  await page.fill("#import-path", BUNDLE);
  await page.click("#import-preview");
  await page.waitForSelector("#import-options:not(.hidden)", { timeout: 5000 });

  // 1) The "map here" checkbox row appears and is labelled with the launch dir.
  const rowVisible = await page.isVisible("#import-map-here-row");
  ok(rowVisible, "map-here checkbox row is visible for a single-project bundle");
  const label = await page.textContent("#import-map-here-label");
  ok(label.includes(HERE), `label names the launch dir (${HERE}); got: ${label}`);

  // 2) Ticking it greys out / disables the explicit map rows + add button.
  await page.check("#import-map-here");
  const addDisabled = await page.isDisabled("#import-add-map");
  ok(addDisabled, "explicit '+ Redirect a folder' button disabled while map-here is checked");

  // 3) The request body sent on "Import for real" carries map_cwd_here:true.
  const reqBodyP = page.waitForRequest((r) =>
    r.url().endsWith("/api/import") && r.method() === "POST" && (() => {
      try { return JSON.parse(r.postData() || "{}").map_cwd_here === true; } catch { return false; }
    })());
  await page.click("#import-run");
  await reqBodyP;
  ok(true, "POST /api/import body included map_cwd_here:true");

  // 4) The import really lands one session and reports the remap.
  await page.waitForSelector("#import-out .card", { timeout: 5000 });
  const outText = await page.textContent("#import-out");
  ok(/1 new/.test(outText) && /remapped/.test(outText),
    `import imported 1 new + remapped; got: ${outText.trim().slice(0, 160)}`);

  console.log("\nALL WEBUI MAP-HERE CHECKS PASSED");
} finally {
  await browser.close();
  child.kill();
}
