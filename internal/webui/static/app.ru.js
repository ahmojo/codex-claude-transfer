"use strict";

// The token only arrives via the launch URL's query string; we forward it on
// every API call so other local processes / web pages cannot drive the server.
const TOKEN = new URLSearchParams(location.search).get("token") || "";
const langLink = document.getElementById("lang-link");
if (langLink) langLink.href = "/?lang=en&token=" + encodeURIComponent(TOKEN);

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
    const err = new Error(data.error || ("Ошибка запроса (" + res.status + ")"));
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
function setBusy(node, msg) { node.innerHTML = '<div class="spinner">' + esc(msg || "Выполняется…") + "</div>"; }
// Approximate human-readable byte size for one-line summaries.
function humanBytes(n) {
  n = Number(n) || 0;
  if (n < 1024) return n + " B";
  const u = ["KB", "MB", "GB", "TB"];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
  return n.toFixed(1) + " " + u[i];
}
function ruCount(n, one, few, many) {
  const v = Math.abs(Number(n)) % 100;
  const d = v % 10;
  return n + " " + (v >= 11 && v <= 14 ? many : d === 1 ? one : d >= 2 && d <= 4 ? few : many);
}
const BACKEND_RU = [
  [
    "no sessions have a cwd matching ",
    "Нет чатов с указанным путём cwd: "
  ],
  [
    " compressed session(s) skipped: cwd is unknown for .jsonl.zst in v0.1 (use --all to include them)",
    " сжатых чатов пропущено: для .jsonl.zst неизвестен cwd (используйте --all, чтобы включить их)"
  ],
  [
    " is not a git repository; no git metadata was recorded",
    " не является git-репозиторием; данные git не добавлены"
  ],
  [
    "git: commit ",
    "git: коммит "
  ],
  [
    " is not on any remote; push it first or the other machine cannot fetch it",
    " отсутствует в удалённом репозитории; сначала отправьте его, иначе другой компьютер не сможет его получить"
  ],
  [
    "compressed session not transformed (zstd not installed); copied as-is",
    "сжатый чат не преобразован (нет утилиты zstd); скопирован без изменений"
  ],
  [
    "archived session skipped (enable archived import explicitly to include it)",
    "архивный чат пропущен (явно включите импорт архивных чатов)"
  ],
  [
    "unexpected non-session file; skipped",
    "неожиданный файл, не относящийся к чату; пропущен"
  ],
  [
    "recorded cwd did not match the mapping; not rewritten",
    "сохранённый cwd не совпал с правилом замены; путь не изменён"
  ],
  [
    "compressed session cannot be cwd-mapped without the 'zstd' tool; copied byte-for-byte",
    "путь cwd сжатого чата нельзя заменить без утилиты zstd; файл скопирован без изменений"
  ],
  [
    "target exists with different content; the local file will be backed up and replaced",
    "целевой файл уже есть и отличается; местная копия будет сохранена в резерве и заменена"
  ],
  [
    "target exists with different content; skipped (conflict). Use --replace-with-backup to overwrite while keeping a backup of the local file",
    "целевой файл уже есть и отличается; пропущен из-за конфликта. Для замены с резервной копией используйте соответствующий параметр импорта"
  ],
  [
    "no --project given: whether imported sessions show in a project's view depends on ",
    "Путь проекта не указан: видимость импортированных чатов в проекте зависит от того, как "
  ],
  [
    "'s cwd filtering; if the project path differs from the source device they may be hidden from that project view",
    " фильтрует cwd; если путь проекта отличается от исходного компьютера, чаты могут не появиться в списке проекта"
  ],
  [
    "backed up local file to ",
    "резервная копия местного файла сохранена в "
  ],
  [
    "no sessionId to reassign; cannot import as a copy; skipped (conflict)",
    "нет sessionId для замены; импорт как копии невозможен, чат пропущен из-за конфликта"
  ],
  [
    "no session_meta id to reassign; cannot import as a copy; skipped (conflict)",
    "нет ID в session_meta для замены; импорт как копии невозможен, чат пропущен из-за конфликта"
  ],
  [
    "target exists with different content; importing as a new session (id ",
    "целевой файл отличается; импортируется новый чат (ID "
  ],
  [
    "compressed session cannot be imported as a copy in v0.1; skipped (conflict)",
    "сжатый чат нельзя импортировать как копию в этой версии; пропущен из-за конфликта"
  ],
  [
    " compressed session(s) skipped by --match because searchable text was unavailable (compressed .jsonl.zst)",
    " сжатых чатов пропущено фильтром --match: текст .jsonl.zst недоступен для поиска"
  ],
  [
    "project memory in the bundle was not imported (pass --with-memory to write it)",
    "память проекта из архива не импортирована (для записи нужен --with-memory)"
  ],
  [
    "memory file is not described in the manifest; skipped",
    "файл памяти не указан в манифесте; пропущен"
  ],
  [
    " exists and is not a regular file; left untouched",
    " существует, но не является обычным файлом; не изменён"
  ],
  [
    "; memory is never overwritten",
    "; память проекта никогда не перезаписывается"
  ],
  [
    "cannot read the memory directory for ",
    "не удалось прочитать папку памяти проекта для "
  ],
  [
    " is not a directory; its memory was not exported",
    " не является папкой; память проекта не экспортирована"
  ],
  [
    " is not a regular file; skipped",
    " не является обычным файлом; пропущен"
  ],
  [
    "compressed session differs and cannot be merged without the 'zstd' tool; skipped (conflict)",
    "сжатый чат отличается; без утилиты zstd его нельзя объединить, поэтому он пропущен из-за конфликта"
  ],
  [
    "not translated (",
    "не преобразован ("
  ],
  [
    "Codex does not support state-only list verification; used its scan-and-repair thread/list fallback",
    "Codex не поддерживает проверку списка без изменения состояния; использован запасной способ сканирования и восстановления"
  ],
  [
    "transcript contains sub-agent sidechain lines; resume behavior for sidechains is not yet verified",
    "в переписке есть строки дочернего агента; продолжение таких чатов ещё не проверено"
  ],
  [
    "no sessionId/cwd found; file may be incomplete or a newer Claude Code format",
    "не найдены sessionId/cwd; файл может быть неполным или создан более новой версией Claude Code"
  ],
  [
    "compressed (.jsonl.zst) rollout detected; metadata not parsed",
    "обнаружен сжатый чат (.jsonl.zst); метаданные не прочитаны"
  ],
  [
    "no SessionMeta found; file may be incomplete or a newer format",
    "SessionMeta не найден; файл может быть неполным или нового формата"
  ],
  [
    "cannot access ",
    "нет доступа к "
  ],
  [
    "walk error under ",
    "ошибка обхода папки "
  ],
  [
    "cannot stat ",
    "не удалось получить сведения о "
  ],
  [
    "cannot read ",
    "не удалось прочитать "
  ],
  [
    "read error: ",
    "ошибка чтения: "
  ],
  [
    "no sessions selected for export",
    "Для экспорта не выбраны чаты"
  ],
  [
    "no sessions matched ",
    "Нет чатов по запросу "
  ],
  [
    "empty thread id",
    "Не указан ID чата"
  ],
  [
    "no session matches thread id ",
    "Чат с указанным ID не найден: "
  ],
  [
    " is ambiguous: it matches ",
    " неоднозначен: подходит к "
  ],
  [
    " sessions (use a longer prefix)",
    " чатам (введите больше символов ID)"
  ],
  [
    "create bundle directory ",
    "не удалось создать папку для архива "
  ],
  [
    "create temp bundle: ",
    "не удалось создать временный архив: "
  ],
  [
    "collect project memory: ",
    "не удалось собрать память проекта: "
  ],
  [
    "marshal manifest: ",
    "не удалось записать манифест: "
  ],
  [
    "marshal checksums: ",
    "не удалось записать контрольные суммы: "
  ],
  [
    "finalize zip: ",
    "не удалось завершить ZIP-архив: "
  ],
  [
    "sync bundle: ",
    "не удалось записать архив на диск: "
  ],
  [
    "close bundle: ",
    "не удалось закрыть архив: "
  ],
  [
    "finalize bundle: ",
    "не удалось завершить архив: "
  ],
  [
    "open bundle: ",
    "не удалось открыть архив: "
  ],
  [
    "mapped ",
    "для изменённого пути "
  ],
  [
    " failed validation: ",
    " проверка не пройдена: "
  ],
  [
    "back up ",
    "не удалось создать резервную копию "
  ],
  [
    " before replacing: ",
    " перед заменой: "
  ],
  [
    " before updating: ",
    " перед обновлением: "
  ],
  [
    "assign new session id for ",
    "не удалось назначить новый ID чату "
  ],
  [
    "could not find a free destination to import a copy of ",
    "не удалось найти свободный путь для импорта копии "
  ],
  [
    "checksum mismatch for ",
    "контрольная сумма не совпадает для "
  ],
  [
    "bundle is corrupt or tampered",
    "архив повреждён или изменён"
  ],
  [
    "manifest missing format_version",
    "в манифесте отсутствует format_version"
  ],
  [
    "unrecognized bundle format_version ",
    "неизвестная версия формата архива "
  ],
  [
    "unsupported bundle format_version ",
    "неподдерживаемая версия формата архива "
  ],
  [
    "bundle already contains ",
    "архив уже содержит чаты программы "
  ],
  [
    " sessions; use a plain import (no --to) instead",
    "; используйте обычный импорт без --to"
  ],
  [
    "no session in the bundle matched the given filters",
    "В архиве нет чатов, подходящих под заданные фильтры"
  ],
  [
    "no session in the bundle matches --session ",
    "В архиве нет чата для --session "
  ]
];
BACKEND_RU.push(...[
  [
    "this bundle is encrypted; provide an age identity (key) file to decrypt it in the browser. Passphrase-encrypted bundles must be imported from the terminal.",
    "Архив зашифрован. Укажите файл закрытого ключа age. Архив с паролем можно импортировать только через терминал."
  ],
  [
    "age is not installed or not on PATH; install age to read encrypted bundles",
    "Утилита age не установлена или не найдена в PATH; она нужна для чтения зашифрованных архивов"
  ],
  [
    "cannot create temp dir: ",
    "Не удалось создать временную папку: "
  ],
  [
    "decrypt failed: ",
    "Ошибка расшифровки: "
  ],
  [
    "scan failed: ",
    "Ошибка сканирования: "
  ],
  [
    "bad request: ",
    "Некорректный запрос: "
  ],
  [
    "choose a project folder, or export everything",
    "Выберите папку проекта или экспортируйте все чаты"
  ],
  [
    "invalid project path: ",
    "Некорректный путь к проекту: "
  ],
  [
    "enter a session id (or a unique prefix) to export one session",
    "Введите ID чата (или его уникальное начало) для экспорта одного чата"
  ],
  [
    "age is not installed or not on PATH; install age to encrypt bundles",
    "Утилита age не установлена или не найдена в PATH; она нужна для шифрования архивов"
  ],
  [
    "git push needs a single project, not 'everything' or a single session",
    "Для git push нужно выбрать один проект, а не все чаты или один чат"
  ],
  [
    "git is not installed; cannot push",
    "Утилита git не установлена; отправить код не удалось"
  ],
  [
    " is not a git repository",
    " не является git-репозиторием"
  ],
  [
    "git push failed: ",
    "Ошибка git push: "
  ],
  [
    "This bundle would contain a likely secret. Turn on \"Replace secrets with placeholders\", ",
    "В архиве может оказаться секрет. Включите «Заменить найденные секреты заглушками» "
  ],
  [
    "or confirm \"Export anyway\".",
    "или подтвердите «Экспортировать всё равно»."
  ],
  [
    "encrypt failed: ",
    "Ошибка шифрования: "
  ],
  [
    "choose a .codexbundle file",
    "Выберите файл .codexbundle"
  ],
  [
    "choose either replace-with-backup or import-as-copy, not both",
    "Выберите замену с резервной копией либо импорт как новой копии, но не оба варианта"
  ],
  [
    "reconcile cannot be combined with a dry run",
    "Проверку обнаружения нельзя совмещать с предпросмотром"
  ],
  [
    "reconcile is only available for native Codex imports",
    "Проверка обнаружения доступна только при обычном импорте в Codex"
  ],
  [
    "choose either map-to-current-folder or explicit cwd mappings, not both",
    "Выберите текущую папку либо укажите замену путей cwd, но не оба варианта"
  ],
  [
    "cannot determine the current directory: ",
    "Не удалось определить текущую папку: "
  ],
  [
    "reconcile applies only to Codex bundles",
    "Проверка обнаружения применима только к архивам Codex"
  ],
  [
    "the bundle records no git remote URL to clone",
    "В архиве нет адреса удалённого git-репозитория для скачивания"
  ],
  [
    "clone failed: ",
    "Ошибка клонирования: "
  ],
  [
    "could not determine an exact thread ID for %d affected rollout(s)",
    "Не удалось определить точный ID для изменённых чатов (%d)"
  ],
  [
    "translate target: ",
    "Целевая программа преобразования: "
  ],
  [
    "enter something to search for",
    "Введите поисковый запрос"
  ],
  [
    "choose a session to resume",
    "Выберите чат для продолжения"
  ],
  [
    "no session matches ",
    "Чат не найден по запросу "
  ],
  [
    "that prefix matches several sessions; use a longer id",
    "Это начало ID совпадает с несколькими чатами; введите больше символов"
  ],
  [
    "missing session id",
    "Не указан ID чата"
  ],
  [
    "cannot save: ",
    "Не удалось сохранить: "
  ]
]);
function ruBackendMessage(message) {
  let result = String(message == null ? "" : message);
  for (const [source, target] of BACKEND_RU) result = result.split(source).join(target);
  return result;
}
function setError(node, e) { node.innerHTML = '<div class="error">' + esc(ruBackendMessage(e.message || e)) + "</div>"; }


const DOCTOR_RU = [
  [
    "%d transcript(s) are already past Claude Code's cleanup window (%s) and can be deleted at any time; archive them with `cct export --all --tool claude -o claude-archive.codexbundle`",
    "У файлов переписки (%d) уже истёк срок хранения Claude Code (%s). Они могут быть удалены в любой момент; сохраните архив командой `cct export --all --tool claude -o claude-archive.codexbundle`"
  ],
  [
    "%d transcript(s) fall out of Claude Code's cleanup window (%s) within %d days, the first on %s; archive them with `cct export --all --tool claude -o claude-archive.codexbundle`",
    "У файлов переписки (%d) истечёт срок хранения Claude Code (%s) в ближайшие %d дней, первый срок — %s. Сохраните архив командой `cct export --all --tool claude -o claude-archive.codexbundle`"
  ],
  [
    "%d session file(s) have a modification time ahead of their content (imported by an older cct); run `cct repair-times` so Codex stops re-parsing them on every open",
    "У файлов чатов (%d) время изменения новее содержимого (импортированы старой версией cct). Выполните `cct repair-times`, чтобы Codex не перечитывал их при каждом открытии"
  ],
  [
    "%d transcript file(s) have a modification time ahead of their content (imported by an older cct); run `cct repair-times --tool claude` to fix the open-lag",
    "У файлов переписки (%d) время изменения новее содержимого. Выполните `cct repair-times --tool claude`, чтобы ускорить открытие"
  ],
  [
    "Claude Code deletes transcripts older than %d days (its default; settings.json sets no cleanupPeriodDays) — nothing is due in the next %d days",
    "Claude Code по умолчанию удаляет переписки старше %d дней (cleanupPeriodDays не задан в settings.json); в ближайшие %d дней удалений не ожидается"
  ],
  [
    "%d compressed (.jsonl.zst) rollout files detected (export/list recover their metadata when the 'zstd' tool is installed)",
    "Найдено сжатых файлов чатов (.jsonl.zst): %d. Для чтения их метаданных нужна утилита zstd"
  ],
  [
    "bundle encryption (--encrypt-to/--passphrase) and decrypting .age bundles",
    "шифрование архива и чтение файлов .age"
  ],
  [
    "%d session(s) have cwd paths that do not exist on this device",
    "У чатов (%d) указаны пути cwd, которых нет на этом компьютере"
  ],
  [
    "Optional tool '%s' not found — %s unavailable until installed",
    "Дополнительная утилита «%s» не найдена; пока недоступно: %s"
  ],
  [
    "~/.claude.json and the Claude cloud are never modified",
    "Файл ~/.claude.json и облако Claude не изменяются"
  ],
  [
    "No transcript is due for Claude Code's cleanup (%s)",
    "Срок хранения переписок Claude Code ещё не истёк (%s)"
  ],
  [
    "reading metadata of compressed .jsonl.zst sessions",
    "чтение метаданных сжатых чатов .jsonl.zst"
  ],
  [
    "cannot read Claude Code's cleanup setting: %v",
    "Не удалось прочитать настройку очистки Claude Code: %v"
  ],
  [
    "No archived sessions folder (this is normal)",
    "Папки архивных чатов нет (это нормально)"
  ],
  [
    "%d transcript file(s) could not be parsed",
    "Не удалось прочитать файлов переписки: %d"
  ],
  [
    "cleanupPeriodDays=%d from settings.json",
    "cleanupPeriodDays=%d из settings.json"
  ],
  [
    "%d rollout file(s) could not be parsed",
    "Не удалось прочитать файлов чатов: %d"
  ],
  [
    "Optional tool '%s' found (enables %s)",
    "Дополнительная утилита «%s» найдена (доступно: %s)"
  ],
  [
    "Archived sessions folder found: %s",
    "Папка архивных чатов найдена: %s"
  ],
  [
    "export --with-git, import --clone",
    "экспорт --with-git, импорт --clone"
  ],
  [
    "Claude Code home not found: %s",
    "Папка данных Claude Code не найдена: %s"
  ],
  [
    "Sessions folder not found: %s",
    "Папка чатов не найдена: %s"
  ],
  [
    "Projects folder not found: %s",
    "Папка проектов не найдена: %s"
  ],
  [
    "%d transcript files detected",
    "Найдено файлов переписки: %d"
  ],
  [
    "SQLite will not be modified",
    "База SQLite не будет изменена"
  ],
  [
    "Claude Code home found: %s",
    "Папка данных Claude Code найдена: %s"
  ],
  [
    "Sessions folder found: %s",
    "Папка чатов найдена: %s"
  ],
  [
    "%d rollout files detected",
    "Найдено файлов чатов: %d"
  ],
  [
    "Projects folder found: %s",
    "Папка проектов найдена: %s"
  ],
  [
    "Codex home not found: %s",
    "Папка данных Codex не найдена: %s"
  ],
  [
    "its default of %d days",
    "стандартный срок — %d дней"
  ],
  [
    "Codex home found: %s",
    "Папка данных Codex найдена: %s"
  ],
  [
    "%d valid sessions",
    "Корректных чатов: %d"
  ]
];
function doctorPattern(template) {
  let out = "^";
  for (let i = 0; i < template.length; i++) {
    if (template[i] === "%" && /[dsv]/.test(template[i + 1] || "")) {
      out += template[i + 1] === "d" ? "(\\d+)" : "(.+?)";
      i++;
    } else {
      out += template[i].replace(/[-/\\^$*+?.()|[\]{}]/g, "\\$&");
    }
  }
  return new RegExp(out + "$");
}
function localizeDoctorMessage(message) {
  let result = String(message == null ? "" : message);
  for (const [english, russian] of DOCTOR_RU) {
    if (!/%[dsv]/.test(english)) continue;
    const match = result.match(doctorPattern(english));
    if (!match) continue;
    let arg = 0;
    result = russian.replace(/%[dsv]/g, () => match[++arg] || "");
    break;
  }
  for (const [english, russian] of DOCTOR_RU) {
    if (!/%[dsv]/.test(english)) result = result.split(english).join(russian);
  }
  result = result.replace(/its default of (\d+) days/g, (_, days) => "стандартный срок — " + days + " дней");
  result = result.replace(/cleanupPeriodDays=(\d+) from settings\.json/g, (_, days) => "cleanupPeriodDays=" + days + " из settings.json");
  return ruBackendMessage(result);
}

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
  setBusy(out, "Проверка…");
  try {
    const d = await api(withTool("/api/doctor"));
    let h = '<div class="card">';
    d.checks.forEach(c => {
      h += '<div class="row"><span class="pill ' + esc(c.status) + '">' + esc(({ok: "ГОТОВО", warn: "ВНИМАНИЕ", info: "СВЕДЕНИЯ"})[c.status] || c.status) +
        '</span><span class="grow">' + esc(localizeDoctorMessage(c.message)) + "</span></div>";
    });
    h += "</div>";
    h += '<div class="card"><div class="row"><strong>' + esc(toolLabel()) + ' — папка данных</strong><span class="grow mono">' +
      esc(d.codex_home) + "</span></div></div>";
    out.innerHTML = h;
  } catch (e) { setError(out, e); }
}
el("doctor-refresh").addEventListener("click", runDoctor);

// ---- sessions ----
let cachedProjects = [];
async function runSessions() {
  const out = el("sessions-out");
  setBusy(out, "Сканирование…");
  try {
    const d = await api(withTool("/api/sessions"));
    cachedProjects = d.projects || [];
    if (!d.count) { out.innerHTML = '<div class="card muted">Чаты ' + esc(toolLabel()) + ' не найдены.</div>'; return; }

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

    let h = '<p class="muted">' + ruCount(d.count, "чат", "чата", "чатов") + "</p>";
    sorted.forEach(g => {
      const label = g.cwd || "(проект не указан)";
      h += '<div class="card"><div class="row"><strong class="grow mono">' + esc(label) +
        '</strong><span class="muted">' + ruCount(g.sessions.length, "чат", "чата", "чатов") + "</span></div>";
      g.sessions.forEach(s => {
        h += '<div class="row"><span class="grow">' + esc(s.preview || "(нет описания)") + "</span>" +
          (s.compressed ? '<span class="pill info">zst</span>' : "") +
          (s.archived ? '<span class="pill info">в архиве</span>' : "") +
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
    sel.innerHTML = '<option value="">(сначала откройте раздел «Чаты», чтобы загрузить список папок)</option>';
    return;
  }
  cachedProjects.forEach(p => {
    const o = document.createElement("option");
    o.value = p.path;
    o.textContent = p.path + "  (" + ruCount(p.count, "чат", "чата", "чатов") + ")";
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
  setBusy(out, "Экспорт…");
  try {
    const d = await api("/api/export", exportBody(allowSecrets));
    let h = '<div class="card"><div class="success">Экспортировано ' + ruCount(d.included, "чат", "чата", "чатов") + "." +
      (d.encrypted ? " Архив зашифрован." : "") + "</div>" +
      '<div class="row"><strong>Архив</strong><span class="grow mono">' + esc(d.bundle) + "</span></div>";
    if (d.secrets_redacted) {
      h += '<div class="row">Секретов замаскировано<span class="grow"></span><strong>' + d.secrets_redacted + "</strong></div>";
    }
    if (d.images_stripped) {
      h += '<div class="row">Изображений удалено<span class="grow"></span><strong>' + d.images_stripped +
        " (сэкономлено около " + humanBytes(d.bytes_saved) + ")</strong></div>";
      h += '<div class="row warn">Архив без изображений нельзя объединять с исходным — импортируйте его отдельно, ' +
        "без добавочной синхронизации (его содержимое отличается от исходного архива).</div>";
    }
    if (d.pushed_remote) {
      h += '<div class="row success">Ветка ' + esc(d.pushed_branch) + " отправлена в ваш git-репозиторий " +
        esc(d.pushed_remote) + " (только код, без чатов).</div>";
    }
    h += "</div>";
    if (d.warnings && d.warnings.length) {
      h += '<div class="card">' + d.warnings.map(w => '<div class="row warn">' + esc(ruBackendMessage(w)) + "</div>").join("") + "</div>";
    }
    out.innerHTML = h;
  } catch (e) {
    // The pre-egress secret gate returns a structured 422; offer redact/allow.
    const data = e.data || {};
    if (data.secrets_blocked) {
      out.innerHTML = '<div class="card"><div class="error">' + esc(ruBackendMessage(e.message)) + "</div>" +
        '<div class="row warn">Найдено ' + ruCount(data.secret_count || 0, "возможный секрет", "возможных секрета", "возможных секретов") + " в " +
        ruCount(data.sessions_with_secrets || 0, "чате", "чатах", "чатах") + ".</div>" +
        '<div class="row"><button class="primary" id="export-redact-go">Замаскировать секреты и экспортировать</button>' +
        '<button class="ghost danger" id="export-anyway">Экспортировать всё равно</button></div></div>';
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
  let h = '<div class="card"><strong>Папки проектов (сохранённый cwd)</strong>';
  projects.forEach(p => {
    h += '<div class="row"><span class="pill ' + (p.exists_local ? "ok" : "missing") + '">' +
      (p.exists_local ? "на месте" : "отсутствует") + '</span><span class="grow mono">' + esc(p.path) +
      "</span><span class='muted'>" + p.count + "</span></div>";
  });
  return h + "</div>";
}
el("inspect-run").addEventListener("click", async () => {
  const out = el("inspect-out");
  setBusy(out, "Чтение…");
  try {
    const d = await api("/api/inspect", { path: cleanPath(el("inspect-path").value), identity: cleanPath(el("inspect-identity").value) });
    let h = '<div class="card"><div class="row"><strong>Чаты</strong><span class="grow">' + d.sessions + "</span></div>" +
      '<div class="row"><strong>Формат</strong><span class="grow mono">' + esc(d.format) + "</span></div>" +
      (d.created ? '<div class="row"><strong>Создан</strong><span class="grow">' + esc(d.created) +
        (d.device ? " на устройстве " + esc(d.device) : "") + "</span></div>" : "") + "</div>";
    h += projectsCard(d.projects);
    if (d.git && d.git.remote_url) {
      h += '<div class="card"><div class="row"><strong>Удалённый git-репозиторий</strong><span class="grow mono">' +
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
  setBusy(out, "Чтение архива…");
  try {
    // Preview never clones or writes; force dry-run and drop the clone target.
    const body = importBody(true);
    body.clone_dir = "";
    const d = await api("/api/import", body);
    lastPreview = d;
    let h = '<div class="card"><strong>Предпросмотр</strong>';
    if (d.translated) {
      h += row("Преобразование для другой программы", d.source_tool + " → " + d.target_tool) +
        row("Чатов будет записано", d.written) +
        row("Уже преобразовано", d.skipped_identical) +
        row("Пропущено", d.skipped || 0) + "</div>";
    } else {
      h += row("Новых чатов", d.imported) +
        row("Уже есть на компьютере", d.skipped_identical) +
        row("Отличаются от местной копии", d.conflicts) + "</div>";
    }
    if (d.warnings && d.warnings.length) {
      h += '<div class="card">' + d.warnings.slice(0, 12).map(w => '<div class="row muted">' + esc(ruBackendMessage(w)) + "</div>").join("") + "</div>";
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
  el("import-map-here-label").textContent = "Поместить эти чаты в текущую папку (" + d.here_dir + ")";
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

// Native Codex imports can opt into immediate discovery. Преобразование для другой программыs
// and Claude-native bundles keep the control hidden because their discovery
// mechanisms are different.
function configureReconcile(d) {
  CCTReconcileState.configureReconcile(d, el);
}
CCTReconcileState.bindTranslationChange(el("import-translate"), () => lastPreview, el);

el("import-add-map").addEventListener("click", () => {
  const r = document.createElement("div");
  r.className = "maprow";
  r.innerHTML = '<input type="text" placeholder="старый cwd (из архива)" /><input type="text" placeholder="новая локальная папка" />';
  el("import-maps").appendChild(r);
});

el("import-run").addEventListener("click", async () => {
  const out = el("import-out");
  setBusy(out, "Импорт…");
  try {
    const d = await api("/api/import", importBody(false));
    let summary;
    if (d.translated) {
      summary = "Преобразовано " + d.source_tool + " → " + d.target_tool + ": записано " + ruCount(d.written, "чат", "чата", "чатов");
    } else {
      const parts = ["Новых чатов: " + d.imported];
      if (d.updated) parts.push("Обновлено чатов: " + d.updated + " (добавлено строк: " + d.lines_added + ")");
      if (d.already_ahead) parts.push("Уже актуальных: " + d.already_ahead);
      if (d.remapped) parts.push("С заменённым путём: " + d.remapped);
      if (d.replaced) parts.push("Заменено чатов: " + d.replaced);
      if (d.imported_copies) parts.push("Импортировано копий: " + d.imported_copies);
      summary = "Импорт завершён: " + parts.join(", ");
    }
    let h = '<div class="card"><div class="success">' + esc(summary) + ".</div>";
    if (d.cloned) h += '<div class="row success">Код проекта скачан в ' + esc(d.cloned) + " (только код).</div>";
    if (d.clone_error) h += '<div class="row warn">Клонирование: ' + esc(ruBackendMessage(d.clone_error)) + "</div>";
    if (d.cwd_mismatch) h += '<div class="row warn">Путь cwd отличается от выбранного проекта у ' + ruCount(d.cwd_mismatch, "чата", "чатов", "чатов") + ".</div>";
    if (d.reconcile) {
      if (d.reconcile.error) {
        h += '<div class="row warn">Файлы чатов импортированы, но не удалось подтвердить, что Codex их обнаружил: ' + esc(ruBackendMessage(d.reconcile.error)) + "</div>";
        (d.reconcile.warnings || []).forEach(w => { h += '<div class="row warn">' + esc(ruBackendMessage(w)) + "</div>"; });
        const fallbackCommands = d.reconcile.fallback_commands || [];
        h += '<div class="preview-tip">Перезапустите приложение Codex.</div>';
        if (fallbackCommands.length) {
          h += '<div class="preview-tip">Чтобы Codex сейчас прочитал конкретный импортированный чат, выполните:</div>';
          fallbackCommands.forEach(cmd => {
            h += '<div class="row mono cmd">' + esc(cmd) + "</div>";
          });
        }
      } else if (d.reconcile.requested === 0) {
        h += '<div class="row success">Изменённых чатов Codex, требующих повторного обнаружения, нет.</div>';
      } else {
        let detail = "Codex подтвердил обнаружение " + d.reconcile.verified + "/" + ruCount(d.reconcile.requested, "чат", "чата", "чатов");
        if (d.reconcile.verification_method) detail += "; способ проверки: " + d.reconcile.verification_method;
        if (d.reconcile.codex_version) detail += " (сервер приложений " + d.reconcile.codex_version + ")";
        h += '<div class="row success">' + esc(detail) + ".</div>";
        if (d.reconcile.read_for_repair) {
          h += '<div class="row success">Codex обновил свои данные об обнаружении для ' + ruCount(d.reconcile.read_for_repair, "чата", "чатов", "чатов") + ".</div>";
        }
        (d.reconcile.warnings || []).forEach(w => { h += '<div class="row warn">' + esc(ruBackendMessage(w)) + "</div>"; });
        h += '<div class="preview-tip">Проверено самим Codex; cct не изменял SQLite и session_index.jsonl.</div>';
      }
    } else if (d.translated) {
      h += '<div class="preview-tip">Перезапустите целевую программу, чтобы она обнаружила импортированные чаты.</div>';
    } else {
      h += '<div class="preview-tip">Перезапустите Codex, чтобы он обнаружил импортированные чаты.</div>';
    }
    h += "</div>";
    out.innerHTML = h;
  } catch (e) { setError(out, e); }
});

// ---- search (+ resume / tag / name on each result) ----
async function runSearch() {
  const out = el("search-out");
  const q = el("search-query").value.trim();
  if (!q) { out.innerHTML = '<div class="card muted">Введите поисковый запрос.</div>'; return; }
  setBusy(out, "Поиск…");
  try {
    const d = await api(withTool("/api/search"), {
      query: q, regex: el("search-regex").checked, case_sensitive: el("search-case").checked,
    });
    if (!d.count) { out.innerHTML = '<div class="card muted">В чатах ' + esc(toolLabel()) + " нет совпадений по запросу " + esc(q) + ".</div>"; return; }
    out.innerHTML = '<p class="muted">' + ruCount(d.count, "совпадение", "совпадения", "совпадений") + "</p>" + d.matches.map(searchCard).join("");
    d.matches.forEach(wireSearchCard);
  } catch (e) { setError(out, e); }
}

function searchCard(m) {
  const id = esc(m.thread_id);
  const tags = (m.tags || []).map(t => '<span class="pill info">' + esc(t) + "</span>").join("");
  return '<div class="card" data-id="' + id + '">' +
    '<div class="row"><strong class="grow">' + esc(m.name || m.preview || "(нет описания)") + "</strong>" +
    '<span class="muted">' + esc(m.updated_at) + " · " + ruCount(m.hits || 0, "совпадение", "совпадения", "совпадений") + "</span></div>" +
    (m.cwd ? '<div class="row mono muted">' + esc(m.cwd) + "</div>" : "") +
    (m.snippet ? '<div class="row snippet">… ' + esc(m.snippet) + "</div>" : "") +
    '<div class="row"><span class="muted mono">' + id + "</span></div>" +
    '<div class="row tagrow">' + tags + "</div>" +
    '<div class="row actions">' +
    '<button class="ghost act-resume">Продолжить…</button>' +
    '<input class="act-tag" type="text" placeholder="добавить метку" />' +
    '<button class="ghost act-tag-add">Метка</button>' +
    '<input class="act-name" type="text" placeholder="задать имя" />' +
    '<button class="ghost act-name-set">Имя</button>' +
    '</div><div class="act-out"></div></div>';
}

function wireSearchCard(m) {
  const card = document.querySelector('.card[data-id="' + cssEsc(m.thread_id) + '"]');
  if (!card) return;
  const actOut = card.querySelector(".act-out");
  card.querySelector(".act-resume").addEventListener("click", async () => {
    try {
      const d = await api(withTool("/api/resume"), { session: m.thread_id });
      actOut.innerHTML = '<div class="row">Чтобы продолжить этот чат, выполните:</div>' +
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
      actOut.innerHTML = '<div class="row success">Имя сохранено.</div>';
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
  setBusy(out, "Подсчёт…");
  try {
    const d = await api(withTool("/api/stats"));
    if (!d.total) { out.innerHTML = '<div class="card muted">Чаты ' + esc(toolLabel()) + " не найдены.</div>"; return; }
    let h = '<div class="card"><div class="row"><strong class="grow">Чаты</strong><strong>' + d.total + "</strong></div>";
    if (d.compressed) h += row("Сжатые", d.compressed);
    if (d.archived) h += row("Архивные", d.archived);
    h += row("Размер на диске", humanBytes(d.total_bytes));
    if (d.first_day) h += row("Период активности", d.first_day + " → " + d.last_day);
    h += "</div>";
    if (d.projects && d.projects.length) {
      h += '<div class="card"><strong>Проекты с наибольшим числом чатов</strong>';
      d.projects.slice(0, 10).forEach(p => {
        h += '<div class="row"><span class="grow mono">' + esc(p.path) + '</span><strong>' + p.count + "</strong></div>";
      });
      if (d.no_cwd) h += '<div class="row"><span class="grow muted">(проект не записан)</span><strong>' + d.no_cwd + "</strong></div>";
      h += "</div>";
    }
    if (d.days && d.days.length) {
      const recent = d.days.slice(-14);
      const peak = Math.max.apply(null, recent.map(x => x.count));
      h += '<div class="card"><strong>Недавняя активность</strong><div class="spark">';
      recent.forEach(x => {
        const pct = peak ? Math.max(8, Math.round((x.count / peak) * 100)) : 8;
        h += '<span class="bar" style="height:' + pct + '%" title="' + esc(x.day) + ": " + x.count + '"></span>';
      });
      h += "</div><div class='row muted'>" + esc(recent[0].day) + " … " + esc(recent[recent.length - 1].day) + " (максимум " + peak + " в день)</div></div>";
    }
    out.innerHTML = h;
  } catch (e) { setError(out, e); }
}
el("stats-refresh").addEventListener("click", runStats);

// ---- scan (secrets) ----
async function runScan() {
  const out = el("scan-out");
  setBusy(out, "Сканирование…");
  try {
    const d = await api(withTool("/api/scan"));
    if (!d.secret_count) { out.innerHTML = '<div class="card success">В чатах ' + esc(toolLabel()) + " возможные секреты не найдены.</div>"; return; }
    let h = '<div class="card warn">Найдено ' + ruCount(d.secret_count, "возможный секрет", "возможных секрета", "возможных секретов") + " в " + ruCount(d.session_count, "чате", "чатах", "чатах") + ". Проверьте результаты: поиск приблизительный.</div>";
    d.sessions.forEach(s => {
      h += '<div class="card"><div class="row"><strong class="grow">' + esc(s.preview || "(нет описания)") + "</strong></div>" +
        (s.cwd ? '<div class="row mono muted">' + esc(s.cwd) + "</div>" : "") +
        s.findings.map(f => '<div class="row"><span class="pill missing">' + esc(f.type) + '</span><span class="grow mono">' + esc(f.masked) + "</span></div>").join("") +
        "</div>";
    });
    h += '<div class="card muted">Для передачи без секретов включите при экспорте «Заменить найденные секреты заглушками».</div>';
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
