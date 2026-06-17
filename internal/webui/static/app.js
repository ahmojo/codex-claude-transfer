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
    let h = '<p class="muted">' + d.count + " session(s)</p>";
    d.sessions.forEach(s => {
      h += '<div class="card"><div class="row"><span class="grow"><strong>' +
        esc(s.preview || "(no preview)") + "</strong></span>" +
        (s.compressed ? '<span class="pill info">zst</span>' : "") +
        (s.archived ? '<span class="pill info">archived</span>' : "") + "</div>" +
        '<div class="row muted"><span class="grow mono">' + esc(s.cwd || "(no cwd)") +
        "</span><span>" + esc(s.updated_at) + "</span></div></div>";
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
      project: el("export-project").value,
      session: el("export-session").value.trim(),
      since: el("export-since").value.trim(),
      output: el("export-output").value.trim(),
      include_archived: el("export-archived").checked,
      with_git: el("export-withgit").checked,
      git_push: el("export-gitpush").checked,
      encrypt_to: splitList(el("export-encrypt-to").value),
      recipients_file: el("export-recipients-file").value.trim(),
    });
    let h = '<div class="card"><div class="success">Exported ' + d.included + " session(s)." +
      (d.encrypted ? " Encrypted." : "") + "</div>" +
      '<div class="row"><strong>Bundle</strong><span class="grow mono">' + esc(d.bundle) + "</span></div>";
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
    const d = await api("/api/inspect", { path: el("inspect-path").value.trim(), identity: el("inspect-identity").value.trim() });
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
    path: el("import-path").value.trim(),
    identity: el("import-identity").value.trim(),
    translate_to: el("import-translate").value,
    dry_run: dryRun,
    merge: conflict === "merge",
    replace_with_backup: conflict === "replace",
    import_as_copy: conflict === "copy",
    project: el("import-project").value.trim(),
    sessions: splitList(el("import-sessions").value),
    clone_dir: el("import-clone").value.trim(),
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
