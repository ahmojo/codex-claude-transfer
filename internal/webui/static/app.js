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
el("export-mode").addEventListener("change", () => {
  el("export-project-row").classList.toggle("hidden", el("export-mode").value === "all");
});
document.querySelector('[data-view="export"]').addEventListener("click", refreshExportProjects);

el("export-run").addEventListener("click", async () => {
  const out = el("export-out");
  setBusy(out, "Exporting…");
  try {
    const d = await api("/api/export", {
      mode: el("export-mode").value,
      tool: currentTool(),
      project: el("export-project").value,
      output: el("export-output").value.trim(),
      include_archived: el("export-archived").checked,
      with_git: el("export-withgit").checked,
      git_push: el("export-gitpush").checked,
    });
    let h = '<div class="card"><div class="success">Exported ' + d.included + " session(s).</div>" +
      '<div class="row"><strong>Bundle</strong><span class="grow mono">' + esc(d.bundle) + "</span></div></div>";
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
    const d = await api("/api/inspect", { path: el("inspect-path").value.trim() });
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
let lastPreview = null;
el("import-preview").addEventListener("click", async () => {
  const out = el("import-preview-out");
  el("import-options").classList.add("hidden");
  setBusy(out, "Reading bundle…");
  try {
    const d = await api("/api/import", { path: el("import-path").value.trim(), dry_run: true });
    lastPreview = d;
    let h = '<div class="card"><strong>Preview</strong>' +
      '<div class="row"><span class="grow">New sessions to add</span><strong>' + d.imported + "</strong></div>" +
      '<div class="row"><span class="grow">Already here</span><strong>' + d.skipped_identical + "</strong></div>" +
      '<div class="row"><span class="grow">Differ from a local copy</span><strong>' + d.conflicts + "</strong></div></div>";
    if (d.warnings && d.warnings.length) {
      h += '<div class="card">' + d.warnings.slice(0, 12).map(w => '<div class="row muted">' + esc(w) + "</div>").join("") + "</div>";
    }
    out.innerHTML = h;
    el("import-options").classList.remove("hidden");
    document.querySelectorAll('input[name="conflict"]').forEach(r => {
      r.parentElement.style.display = d.conflicts > 0 ? "flex" : "none";
    });
  } catch (e) { setError(out, e); lastPreview = null; }
});

el("import-add-map").addEventListener("click", () => {
  const row = document.createElement("div");
  row.className = "maprow";
  row.innerHTML = '<input type="text" placeholder="old cwd (from the bundle)" /><input type="text" placeholder="new local folder" />';
  el("import-maps").appendChild(row);
});

el("import-run").addEventListener("click", async () => {
  const out = el("import-out");
  const conflict = (document.querySelector('input[name="conflict"]:checked') || {}).value;
  const maps = [];
  el("import-maps").querySelectorAll(".maprow").forEach(r => {
    const i = r.querySelectorAll("input");
    if (i[0].value.trim() && i[1].value.trim()) maps.push({ old: i[0].value.trim(), new: i[1].value.trim() });
  });
  setBusy(out, "Importing…");
  try {
    const d = await api("/api/import", {
      path: el("import-path").value.trim(),
      dry_run: false,
      replace_with_backup: conflict === "replace",
      import_as_copy: conflict === "copy",
      map_cwd: maps,
    });
    let summary = "Imported " + d.imported + " new";
    if (d.remapped) summary += ", " + d.remapped + " remapped";
    if (d.replaced) summary += ", " + d.replaced + " replaced";
    if (d.imported_copies) summary += ", " + d.imported_copies + " as copies";
    out.innerHTML = '<div class="card"><div class="success">' + esc(summary) + ".</div>" +
      '<div class="preview-tip">Run your agent again (restart Codex, or relaunch Claude Code) so it picks up the imported sessions.</div></div>";
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
runDoctor();
