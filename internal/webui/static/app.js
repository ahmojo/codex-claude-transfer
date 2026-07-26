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
  if (!res.ok) {
    const err = new Error(data.error || ("request failed (" + res.status + ")"));
    err.data = data; // structured fields (e.g. the secret-gate flags) for callers
    throw err;
  }
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

function exportBody(allowSecrets) {
  return {
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
    redact: el("export-redact").checked,
    allow_secrets: !!allowSecrets,
    encrypt_to: splitList(el("export-encrypt-to").value),
    recipients_file: cleanPath(el("export-recipients-file").value),
  };
}

async function runExport(allowSecrets) {
  const out = el("export-out");
  setBusy(out, "Exporting…");
  try {
    const d = await api("/api/export", exportBody(allowSecrets));
    let h = '<div class="card"><div class="success">Exported ' + d.included + " session(s)." +
      (d.encrypted ? " Encrypted." : "") + "</div>" +
      '<div class="row"><strong>Bundle</strong><span class="grow mono">' + esc(d.bundle) + "</span></div>";
    if (d.secrets_redacted) {
      h += '<div class="row">Secrets redacted<span class="grow"></span><strong>' + d.secrets_redacted + "</strong></div>";
    }
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
  } catch (e) {
    // The pre-egress secret gate returns a structured 422; offer redact/allow.
    const data = e.data || {};
    if (data.secrets_blocked) {
      out.innerHTML = '<div class="card"><div class="error">' + esc(e.message) + "</div>" +
        '<div class="row warn">Found ' + (data.secret_count || 0) + " likely secret(s) in " +
        (data.sessions_with_secrets || 0) + " session(s).</div>" +
        '<div class="row"><button class="primary" id="export-redact-go">Replace secrets &amp; export</button>' +
        '<button class="ghost danger" id="export-anyway">Export anyway</button></div></div>';
      el("export-redact-go").addEventListener("click", () => { el("export-redact").checked = true; runExport(false); });
      el("export-anyway").addEventListener("click", () => runExport(true));
      return;
    }
    setError(out, e);
  }
}
el("export-run").addEventListener("click", () => runExport(false));

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
  const mapHere = el("import-map-here").checked;
  const maps = [];
  // "Map to current folder" (--map-cwd-here) is mutually exclusive with explicit
  // mappings, so the explicit rows are ignored when it is ticked.
  if (!mapHere) {
    el("import-maps").querySelectorAll(".maprow").forEach(r => {
      const i = r.querySelectorAll("input");
      if (i[0].value.trim() && i[1].value.trim()) maps.push({ old: i[0].value.trim(), new: i[1].value.trim() });
    });
  }
  return {
    path: cleanPath(el("import-path").value),
    identity: cleanPath(el("import-identity").value),
    translate_to: el("import-translate").value,
    dry_run: dryRun,
    merge: conflict === "merge",
    reconcile: !dryRun && !el("import-translate").value && el("import-reconcile").checked,
    replace_with_backup: conflict === "replace",
    import_as_copy: conflict === "copy",
    project: cleanPath(el("import-project").value),
    sessions: splitList(el("import-sessions").value),
    clone_dir: cleanPath(el("import-clone").value),
    map_cwd: maps,
    map_cwd_here: mapHere,
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
    configureMapHere(d);
    configureReconcile(d);
  } catch (e) { setError(out, e); lastPreview = null; }
});

// configureMapHere shows the "put these under the current folder" shortcut
// (--map-cwd-here) only for a single-project bundle, labelled with the actual
// launch directory so it is unambiguous which folder "here" is.
function configureMapHere(d) {
  const row = el("import-map-here-row"), hint = el("import-map-here-hint");
  const box = el("import-map-here");
  const single = !d.translated && Array.isArray(d.projects) && d.projects.length === 1 && d.here_dir;
  if (!single) {
    row.style.display = "none"; hint.style.display = "none";
    box.checked = false; setMapsDisabled(false);
    return;
  }
  el("import-map-here-label").textContent = "Put these sessions under the current folder (" + d.here_dir + ")";
  row.style.display = "flex"; hint.style.display = "block";
  setMapsDisabled(box.checked);
}

// The "map here" shortcut and explicit folder redirects are mutually exclusive,
// so ticking one greys out the other.
function setMapsDisabled(disabled) {
  el("import-maps").style.opacity = disabled ? "0.4" : "";
  el("import-maps").querySelectorAll("input").forEach(i => { i.disabled = disabled; });
  el("import-add-map").disabled = disabled;
}
el("import-map-here").addEventListener("change", e => setMapsDisabled(e.target.checked));

// Native Codex imports can opt into immediate discovery. Cross-agent handoffs
// and Claude-native bundles keep the control hidden because their discovery
// mechanisms are different.
function configureReconcile(d) {
  CCTReconcileState.configureReconcile(d, el);
}
CCTReconcileState.bindTranslationChange(el("import-translate"), () => lastPreview, el);

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
    if (d.reconcile) {
      if (d.reconcile.error) {
        h += '<div class="row warn">The rollout import is complete, but Codex discovery could not be verified: ' + esc(d.reconcile.error) + "</div>";
        (d.reconcile.warnings || []).forEach(w => { h += '<div class="row warn">' + esc(w) + "</div>"; });
        const fallbackCommands = d.reconcile.fallback_commands || [];
        h += '<div class="preview-tip">Restart the Codex App.</div>';
        if (fallbackCommands.length) {
          h += '<div class="preview-tip">To force Codex to read a specific imported thread now, run:</div>';
          fallbackCommands.forEach(cmd => {
            h += '<div class="row mono cmd">' + esc(cmd) + "</div>";
          });
        }
      } else if (d.reconcile.requested === 0) {
        h += '<div class="row success">No changed Codex threads required discovery reconciliation.</div>';
      } else {
        let detail = "Codex discovery verified " + d.reconcile.verified + "/" + d.reconcile.requested + " thread(s)";
        if (d.reconcile.verification_method) detail += " via " + d.reconcile.verification_method;
        if (d.reconcile.codex_version) detail += " (app-server " + d.reconcile.codex_version + ")";
        h += '<div class="row success">' + esc(detail) + ".</div>";
        if (d.reconcile.read_for_repair) {
          h += '<div class="row success">Codex natively repaired discovery metadata for ' + d.reconcile.read_for_repair + " thread(s).</div>";
        }
        (d.reconcile.warnings || []).forEach(w => { h += '<div class="row warn">' + esc(w) + "</div>"; });
        h += '<div class="preview-tip">Verified through Codex itself; cct did not write SQLite or session_index.jsonl.</div>';
      }
    } else if (d.translated) {
      h += '<div class="preview-tip">Relaunch the target agent so it picks up the imported sessions.</div>';
    } else {
      h += '<div class="preview-tip">Restart Codex (or run Codex again) so it discovers the imported sessions.</div>';
    }
    h += "</div>";
    out.innerHTML = h;
  } catch (e) { setError(out, e); }
});

// ---- search (+ resume / tag / name on each result) ----
async function runSearch() {
  const out = el("search-out");
  const q = el("search-query").value.trim();
  if (!q) { out.innerHTML = '<div class="card muted">Type something to search for.</div>'; return; }
  setBusy(out, "Searching…");
  try {
    const d = await api(withTool("/api/search"), {
      query: q, regex: el("search-regex").checked, case_sensitive: el("search-case").checked,
    });
    if (!d.count) { out.innerHTML = '<div class="card muted">No ' + esc(toolLabel()) + " sessions matched " + esc(q) + ".</div>"; return; }
    out.innerHTML = '<p class="muted">' + d.count + " match(es)</p>" + d.matches.map(searchCard).join("");
    d.matches.forEach(wireSearchCard);
  } catch (e) { setError(out, e); }
}

function searchCard(m) {
  const id = esc(m.thread_id);
  const tags = (m.tags || []).map(t => '<span class="pill info">' + esc(t) + "</span>").join("");
  return '<div class="card" data-id="' + id + '">' +
    '<div class="row"><strong class="grow">' + esc(m.name || m.preview || "(no preview)") + "</strong>" +
    '<span class="muted">' + esc(m.updated_at) + " · " + (m.hits || 0) + " hit(s)</span></div>" +
    (m.cwd ? '<div class="row mono muted">' + esc(m.cwd) + "</div>" : "") +
    (m.snippet ? '<div class="row snippet">… ' + esc(m.snippet) + "</div>" : "") +
    '<div class="row"><span class="muted mono">' + id + "</span></div>" +
    '<div class="row tagrow">' + tags + "</div>" +
    '<div class="row actions">' +
    '<button class="ghost act-resume">Resume…</button>' +
    '<input class="act-tag" type="text" placeholder="add a tag" />' +
    '<button class="ghost act-tag-add">Tag</button>' +
    '<input class="act-name" type="text" placeholder="set a name" />' +
    '<button class="ghost act-name-set">Name</button>' +
    '</div><div class="act-out"></div></div>';
}

function wireSearchCard(m) {
  const card = document.querySelector('.card[data-id="' + cssEsc(m.thread_id) + '"]');
  if (!card) return;
  const actOut = card.querySelector(".act-out");
  card.querySelector(".act-resume").addEventListener("click", async () => {
    try {
      const d = await api(withTool("/api/resume"), { session: m.thread_id });
      actOut.innerHTML = '<div class="row">To continue this session, run:</div>' +
        '<div class="row mono cmd">' + esc(d.command) + "</div>";
    } catch (e) { setError(actOut, e); }
  });
  card.querySelector(".act-tag-add").addEventListener("click", async () => {
    const inp = card.querySelector(".act-tag"); const t = inp.value.trim();
    if (!t) return;
    try {
      const d = await api("/api/tags", { session: m.thread_id, add_tags: [t] });
      inp.value = ""; card.querySelector(".tagrow").innerHTML = (d.tags || []).map(x => '<span class="pill info">' + esc(x) + "</span>").join("");
    } catch (e) { setError(actOut, e); }
  });
  card.querySelector(".act-name-set").addEventListener("click", async () => {
    const inp = card.querySelector(".act-name"); const n = inp.value.trim();
    try {
      await api("/api/tags", { session: m.thread_id, set_name: n });
      actOut.innerHTML = '<div class="row success">Saved name.</div>';
    } catch (e) { setError(actOut, e); }
  });
}
// Escape a value for use inside a CSS attribute selector.
function cssEsc(s) { return String(s || "").replace(/["\\]/g, "\\$&"); }
el("search-run").addEventListener("click", runSearch);
el("search-query").addEventListener("keydown", e => { if (e.key === "Enter") runSearch(); });

// ---- stats ----
async function runStats() {
  const out = el("stats-out");
  setBusy(out, "Computing…");
  try {
    const d = await api(withTool("/api/stats"));
    if (!d.total) { out.innerHTML = '<div class="card muted">No ' + esc(toolLabel()) + " sessions found.</div>"; return; }
    let h = '<div class="card"><div class="row"><strong class="grow">Sessions</strong><strong>' + d.total + "</strong></div>";
    if (d.compressed) h += row("Compressed", d.compressed);
    if (d.archived) h += row("Archived", d.archived);
    h += row("On disk", humanBytes(d.total_bytes));
    if (d.first_day) h += row("Activity", d.first_day + " → " + d.last_day);
    h += "</div>";
    if (d.projects && d.projects.length) {
      h += '<div class="card"><strong>Busiest projects</strong>';
      d.projects.slice(0, 10).forEach(p => {
        h += '<div class="row"><span class="grow mono">' + esc(p.path) + '</span><strong>' + p.count + "</strong></div>";
      });
      if (d.no_cwd) h += '<div class="row"><span class="grow muted">(no recorded project)</span><strong>' + d.no_cwd + "</strong></div>";
      h += "</div>";
    }
    if (d.days && d.days.length) {
      const recent = d.days.slice(-14);
      const peak = Math.max.apply(null, recent.map(x => x.count));
      h += '<div class="card"><strong>Recent activity</strong><div class="spark">';
      recent.forEach(x => {
        const pct = peak ? Math.max(8, Math.round((x.count / peak) * 100)) : 8;
        h += '<span class="bar" style="height:' + pct + '%" title="' + esc(x.day) + ": " + x.count + '"></span>';
      });
      h += "</div><div class='row muted'>" + esc(recent[0].day) + " … " + esc(recent[recent.length - 1].day) + " (peak " + peak + "/day)</div></div>";
    }
    out.innerHTML = h;
  } catch (e) { setError(out, e); }
}
el("stats-refresh").addEventListener("click", runStats);

// ---- scan (secrets) ----
async function runScan() {
  const out = el("scan-out");
  setBusy(out, "Scanning…");
  try {
    const d = await api(withTool("/api/scan"));
    if (!d.secret_count) { out.innerHTML = '<div class="card success">No likely secrets found in your ' + esc(toolLabel()) + " sessions.</div>"; return; }
    let h = '<div class="card warn">Found ' + d.secret_count + " possible secret(s) across " + d.session_count + " session(s). Heuristic — review before trusting.</div>";
    d.sessions.forEach(s => {
      h += '<div class="card"><div class="row"><strong class="grow">' + esc(s.preview || "(no preview)") + "</strong></div>" +
        (s.cwd ? '<div class="row mono muted">' + esc(s.cwd) + "</div>" : "") +
        s.findings.map(f => '<div class="row"><span class="pill missing">' + esc(f.type) + '</span><span class="grow mono">' + esc(f.masked) + "</span></div>").join("") +
        "</div>";
    });
    h += '<div class="card muted">To share without these, export with "Replace detected secrets with placeholders".</div>';
    out.innerHTML = h;
  } catch (e) { setError(out, e); }
}
el("scan-run").addEventListener("click", runScan);

// Switching the tool re-checks health and clears any stale session/project list.
el("tool-select").addEventListener("change", () => {
  cachedProjects = [];
  el("sessions-out").innerHTML = "";
  el("search-out").innerHTML = "";
  el("stats-out").innerHTML = "";
  el("scan-out").innerHTML = "";
  refreshExportProjects();
  runDoctor();
});

// initial load
syncExportMode();
runDoctor();
