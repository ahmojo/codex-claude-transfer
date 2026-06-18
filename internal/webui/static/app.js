"use strict";

// The token only arrives via the launch URL's query string; we forward it on
// every API call so other local processes / web pages cannot drive the server.
const TOKEN = new URLSearchParams(location.search).get("token") || "";

async function api(path, body) {
  const opts = {
    method: body ? "POST" : "GET",
    headers: { "X-Cct-Token": TOKEN },
  };
  if (body) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  let data = {};
  try { data = await res.json(); } catch (e) { /* non-JSON */ }
  if (!res.ok) throw new Error(data.error || ("request failed (" + res.status + ")"));
  return data;
}

function el(id) { return document.getElementById(id); }
// Strip surrounding double-quotes from a path pasted from Explorer or a terminal.
function cleanPath(s) {
  s = (s || "").trim();
  if (s.length >= 2 && s[0] === '"' && s[s.length - 1] === '"') s = s.slice(1, -1);
  return s;
}
// The selected agent (Codex or Claude Code). Sent to read endpoints via ?tool=
// and to export via the body; import always follows the bundle's own tool.
function currentTool() { return el("tool-select").value; }
function toolLabel() { return currentTool() === "claude" ? "Claude Code" : "Codex"; }
function withTool(path) { return path + (path.includes("?") ? "&" : "?") + "tool=" + encodeURIComponent(currentTool()); }
function esc(s) {
  return String(s == null ? "" : s).replace(/[&<>"]/g, c =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
}
function setBusy(node, msg) { node.innerHTML = '<div class="spinner">' + esc(msg || "Working…") + "</div>"; }
// Approximate human-readable byte size for one-line summaries.
function humanBytes(n) {
  n = Number(n) || 0;
  if (n < 1024) return n + " B";
  const u = ["KB", "MB", "GB", "TB"];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
  return n.toFixed(1) + " " + u[i];
}
function setError(node, e) { node.innerHTML = '<div class="error">' + esc(e.message || e) + "</div>"; }

// ---- navigation ----
document.querySelectorAll(".nav").forEach(btn => {
  btn.addEventListener("click", () => {
    document.querySelectorAll(".nav").forEach(b => b.classList.remove("active"));
    btn.classList.add("active");
    document.querySelectorAll(".view").forEach(v => v.classList.add("hidden"));
    el("view-" + btn.dataset.view).classList.remove("hidden");
  });
});

// ---- doctor ----
async function runDoctor() {
  const out = el("doctor-out");
  setBusy(out, "Checking…");
  try {
    const d = await api(withTool("/api/doctor"));
    let h = '<div class="card">';
    d.checks.forEach(c => {
      h += '<div class="row"><span class="pill ' + esc(c.status) + '">' + esc(c.status.toUpperCase()) +
        '</span><span class="grow">' + esc(c.message) + "</span></div>";
    });
    h += "</div>";
    h += '<div class="card"><div class="row"><strong>' + esc(toolLabel()) + ' home</strong><span class="grow mono">' +
      esc(d.codex_home) + "</span></div></div>";
    out.innerHTML = h;
  } catch (e) { setError(out, e); }
}
el("doctor-refresh").addEventListener("click", runDoctor);

// ---- sessions ----
let cachedProjects = [];
async function runSessions() {
  const out = el("sessions-out");
  setBusy(out, "Scanning…");
  try {
    const d = await api(withTool("/api/sessions"));
    cachedProjects = d.projects || [];
    if (!d.count) { out.innerHTML = '<div class="card muted">No ' + esc(toolLabel()) + ' sessions found.</div>'; return; }

    // Group by cwd, sort newest first within each group, groups sorted by newest.
    const groups = new Map();
    d.sessions.forEach(s => {
      const key = (s.cwd || "").toLowerCase().replace(/[\\/]+$/, "");
      if (!groups.has(key)) groups.set(key, { cwd: s.cwd || "", sessions: [], newest: "" });
      const g = groups.get(key);
      g.sessions.push(s);
      if (!g.newest || s.updated_at > g.newest) g.newest = s.updated_at;
    });
    // Sort sessions within each group newest first.
    groups.forEach(g => g.sessions.sort((a, b) => b.updated_at.localeCompare(a.updated_at)));
    // Sort groups by newest session descending.
    const sorted = [...groups.values()].sort((a, b) => b.newest.localeCompare(a.newest));

    let h = '<p class="muted">' + d.count + " session(s)</p>";
    sorted.forEach(g => {
      const label = g.cwd || "(no project)";
      h += '<div class="card"><div class="row"><strong class="grow mono">' + esc(label) +
        '</strong><span class="muted">' + g.sessions.length + " session" + (g.sessions.length === 1 ? "" : "s") + "</span></div>";
      g.sessions.forEach(s => {
        h += '<div class="row"><span class="grow">' + esc(s.preview || "(no preview)") + "</span>" +
          (s.compressed ? '<span class="pill info">zst</span>' : "") +
          (s.archived ? '<span class="pill info">archived</span>' : "") +
          '<span class="muted">' + esc(s.updated_at) + "</span></div>";
      });
      h += "</div>";
    });
    out.innerHTML = h;
  } catch (e) { setError(out, e); }
}
el("sessions-refresh").addEventListener("click", runSessions);

// ---- export ----
function refreshExportProjects() {
  const sel = el("export-project");
  sel.innerHTML = "";
  if (!cachedProjects.length) {
    sel.innerHTML = '<option value="">(scan Sessions first to list folders)</option>';
    return;
  }
  cachedProjects.forEach(p => {
    const o = document.createElement("option");
    o.value = p.path;
    o.textContent = p.path + "  (" + p.count + " session" + (p.count === 1 ? "" : "s") + ")";
    sel.appendChild(o);
  });
}
function syncExportMode() {
  const mode = el("export-mode").value;
  el("export-project-row").classList.toggle("hidden", mode !== "project");
  el("export-session-row").classList.toggle("hidden", mode !== "session");
}
el("export-mode").addEventListener("change", syncExportMode);
document.querySelector('[data-view="export"]').addEventListener("click", refreshExportProjects);

// Split a recipients textarea into a clean list (newline or comma separated).
function splitList(s) {
  return (s || "").split(/[\n,]+/).map(x => x.trim()).filter(Boolean);
}

el("export-run").addEventListener("click", async () => {
  const out = el("export-out");
  setBusy(out, "Exporting…");
  try {
    const d = await api("/api/export", {
      mode: el("export-mode").value,
      tool: currentTool(),
      project: cleanPath(el("export-project").value),
      session: el("export-session").value.trim(),
      since: el("export-since").value.trim(),
      output: cleanPath(el("export-output").value),
      include_archived: el("export-archived").checked,
      with_git: el("export-withgit").checked,
      git_push: el("export-gitpush").checked,
      strip_images: el("export-stripimages").checked,
      encrypt_to: splitList(el("export-encrypt-to").value),
      recipients_file: cleanPath(el("export-recipients-file").value),
    });
    let h = '<div class="card"><div class="success">Exported ' + d.included + " session(s)." +
      (d.encrypted ? " Encrypted." : "") + "</div>" +
      '<div class="row"><strong>Bundle</strong><span class="grow mono">' + esc(d.bundle) + "</span></div>";
    if (d.images_stripped) {
      h += '<div class="row">Images stripped<span class="grow"></span><strong>' + d.images_stripped +
        " (saved ~" + humanBytes(d.bytes_saved) + ")</strong></div>";
      h += '<div class="row warn">A stripped bundle isn\'t merge-friendly — import it fresh, ' +
        "not with incremental sync (it reads as diverged from an unstripped copy).</div>";
    }
    if (d.pushed_remote) {
      h += '<div class="row success">Pushed branch ' + esc(d.pushed_branch) + " to your git remote " +
        esc(d.pushed_remote) + " (code only — no sessions).</div>";
    }
    h += "</div>";
    if (d.warnings && d.warnings.length) {
      h += '<div class="card">' + d.warnings.map(w => '<div class="row warn">' + esc(w) + "</div>").join("") + "</div>";
    }
    out.innerHTML = h;
  } catch (e) { setError(out, e); }
});

// ---- inspect ----
function projectsCard(projects) {
  if (!projects || !projects.length) return "";
  let h = '<div class="card"><strong>Project folders (recorded cwd)</strong>';
  projects.forEach(p => {
    h += '<div class="row"><span class="pill ' + (p.exists_local ? "ok" : "missing") + '">' +
      (p.exists_local ? "here" : "missing") + '</span><span class="grow mono">' + esc(p.path) +
      "</span><span class='muted'>" + p.count + "</span></div>";
  });
  return h + "</div>";
}
el("inspect-run").addEventListener("click", async () => {
  const out = el("inspect-out");
  setBusy(out, "Reading…");
  try {
    const d = await api("/api/inspect", { path: cleanPath(el("inspect-path").value), identity: cleanPath(el("inspect-identity").value) });
    let h = '<div class="card"><div class="row"><strong>Sessions</strong><span class="grow">' + d.sessions + "</span></div>" +
      '<div class="row"><strong>Format</strong><span class="grow mono">' + esc(d.format) + "</span></div>" +
      (d.created ? '<div class="row"><strong>Created</strong><span class="grow">' + esc(d.created) +
        (d.device ? " by " + esc(d.device) : "") + "</span></div>" : "") + "</div>";
    h += projectsCard(d.projects);
    if (d.git && d.git.remote_url) {
      h += '<div class="card"><div class="row"><strong>git remote</strong><span class="grow mono">' +
        esc(d.git.remote_url) + "</span></div></div>";
    }
    out.innerHTML = h;
  } catch (e) { setError(out, e); }
});

// ---- import ----
// Build the import request body from the form. `dryRun` and the chosen conflict
// resolution are passed in so the same fields drive both preview and real run.
function importBody(dryRun) {
  const conflict = (document.querySelector('input[name="conflict"]:checked') || {}).value;
  const maps = [];
  el("import-maps").querySelectorAll(".maprow").forEach(r => {
    const i = r.querySelectorAll("input");
    if (i[0].value.trim() && i[1].value.trim()) maps.push({ old: i[0].value.trim(), new: i[1].value.trim() });
  });
  return {
    path: cleanPath(el("import-path").value),
    identity: cleanPath(el("import-identity").value),
    translate_to: el("import-translate").value,
    dry_run: dryRun,
    merge: conflict === "merge",
    replace_with_backup: conflict === "replace",
    import_as_copy: conflict === "copy",
    project: cleanPath(el("import-project").value),
    sessions: splitList(el("import-sessions").value),
    clone_dir: cleanPath(el("import-clone").value),
    map_cwd: maps,
  };
}

function row(label, value) {
  return '<div class="row"><span class="grow">' + esc(label) + "</span><strong>" + esc(value) + "</strong></div>";
}

let lastPreview = null;
el("import-preview").addEventListener("click", async () => {
  const out = el("import-preview-out");
  el("import-options").classList.add("hidden");
  setBusy(out, "Reading bundle…");
  try {
    // Preview never clones or writes; force dry-run and drop the clone target.
    const body = importBody(true);
    body.clone_dir = "";
    const d = await api("/api/import", body);
    lastPreview = d;
    let h = '<div class="card"><strong>Preview</strong>';
    if (d.translated) {
      h += row("Cross-agent handoff", d.source_tool + " → " + d.target_tool) +
        row("Sessions to write", d.written) +
        row("Already translated", d.skipped_identical) +
        row("Skipped", d.skipped || 0) + "</div>";
    } else {
      h += row("New sessions to add", d.imported) +
        row("Already here", d.skipped_identical) +
        row("Differ from a local copy", d.conflicts) + "</div>";
    }
    if (d.warnings && d.warnings.length) {
      h += '<div class="card">' + d.warnings.slice(0, 12).map(w => '<div class="row muted">' + esc(w) + "</div>").join("") + "</div>";
    }
    out.innerHTML = h;
    el("import-options").classList.remove("hidden");
    // Translate mode resolves nothing; conflict choices apply only to a normal
    // import that found differing local sessions.
    el("import-conflict").style.display = (!d.translated && d.conflicts > 0) ? "block" : "none";
  } catch (e) { setError(out, e); lastPreview = null; }
});

el("import-add-map").addEventListener("click", () => {
  const r = document.createElement("div");
  r.className = "maprow";
  r.innerHTML = '<input type="text" placeholder="old cwd (from the bundle)" /><input type="text" placeholder="new local folder" />';
  el("import-maps").appendChild(r);
});

el("import-run").addEventListener("click", async () => {
  const out = el("import-out");
  setBusy(out, "Importing…");
  try {
    const d = await api("/api/import", importBody(false));
    let summary;
    if (d.translated) {
      summary = "Translated " + d.source_tool + " → " + d.target_tool + ": wrote " + d.written + " session(s)";
    } else {
      const parts = [d.imported + " new"];
      if (d.updated) parts.push(d.updated + " updated (+" + d.lines_added + " lines)");
      if (d.already_ahead) parts.push(d.already_ahead + " already up to date");
      if (d.remapped) parts.push(d.remapped + " remapped");
      if (d.replaced) parts.push(d.replaced + " replaced");
      if (d.imported_copies) parts.push(d.imported_copies + " as copies");
      summary = "Imported " + parts.join(", ");
    }
    let h = '<div class="card"><div class="success">' + esc(summary) + ".</div>";
    if (d.cloned) h += '<div class="row success">Cloned the project code into ' + esc(d.cloned) + " (code only).</div>";
    if (d.clone_error) h += '<div class="row warn">Clone: ' + esc(d.clone_error) + "</div>";
    if (d.cwd_mismatch) h += '<div class="row warn">' + d.cwd_mismatch + " session(s) had a cwd different from the project you named.</div>";
    h += '<div class="preview-tip">Run your agent again (restart Codex, or relaunch Claude Code) so it picks up the imported sessions.</div></div>';
    out.innerHTML = h;
  } catch (e) { setError(out, e); }
});

// Switching the tool re-checks health and clears any stale session/project list.
el("tool-select").addEventListener("change", () => {
  cachedProjects = [];
  el("sessions-out").innerHTML = "";
  refreshExportProjects();
  runDoctor();
});

// initial load
syncExportMode();
runDoctor();
