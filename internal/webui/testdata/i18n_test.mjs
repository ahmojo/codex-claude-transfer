import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";

const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

// load runs i18n.js and the Russian dictionary for a page marked <html lang>.
function load(lang) {
  const context = { document: { documentElement: { lang } } };
  vm.createContext(context);
  vm.runInContext(read("../static/i18n.js"), context);
  vm.runInContext(read("../static/i18n.ru.js"), context);
  return context.I18N;
}

// dictionary returns what i18n.ru.js registers.
function dictionary() {
  let registered;
  const context = { I18N: { register: (code, dict) => { registered = dict; } } };
  vm.createContext(context);
  vm.runInContext(read("../static/i18n.ru.js"), context);
  return registered;
}

test("server messages keep inserted values unchanged", () => {
  const ru = load("ru");
  const path = "C:\\work\\mapped back up\\cannot read.jsonl";
  assert.equal(
    ru.msg(path + ": recorded cwd did not match the mapping; not rewritten"),
    path + ": сохранённый cwd не совпал с правилом замены; путь не изменён",
  );
  // No template matches the whole message, so nothing inside it changes.
  assert.equal(ru.msg("mapped " + path + " failed"), "mapped " + path + " failed");
});

test("inserted values are translated only when they match in full", () => {
  const ru = load("ru");
  assert.equal(
    ru.msg("open bundle: manifest missing format_version"),
    "не удалось открыть архив: в манифесте отсутствует format_version",
  );
  assert.equal(
    ru.msg("No transcript is due for Claude Code's cleanup (its default of 30 days)"),
    "Срок хранения переписок Claude Code ещё не истёк (стандартный срок — 30 дней)",
  );
  assert.equal(ru.msg("open bundle: zip: not a valid zip file"), "не удалось открыть архив: zip: not a valid zip file");
});

test("untranslated text falls back to English", () => {
  const ru = load("ru");
  assert.equal(ru.t("Brand new {thing}", { thing: "feature" }), "Brand new feature");
});

test("Russian plural forms", () => {
  const ru = load("ru");
  for (const [n, want] of [[1, "1 чат"], [2, "2 чата"], [5, "5 чатов"], [11, "11 чатов"], [21, "21 чат"], [22, "22 чата"]]) {
    assert.equal(ru.t("{n} session(s)", { n }), want);
  }
});

test("the English page is left as it is", () => {
  const en = load("en");
  assert.equal(en.t("{n} session(s)", { n: 2 }), "2 session(s)");
  const message = "open bundle: manifest missing format_version";
  assert.equal(en.msg(message), message);
});

// jsStrings returns every string literal in a JavaScript source.
function jsStrings(source) {
  const out = new Set();
  for (const m of source.matchAll(/"((?:[^"\\\n]|\\.)*)"|'((?:[^'\\\n]|\\.)*)'/g)) {
    const body = m[1] !== undefined ? m[1] : m[2].replace(/\\'/g, "'").replace(/"/g, '\\"');
    out.add(JSON.parse('"' + body + '"'));
  }
  return out;
}

// htmlText returns the text runs and translatable attributes of index.html.
function htmlText(source) {
  const decode = (s) => s.replace(/&mdash;/g, "—").replace(/&quot;/g, '"').replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">").replace(/&nbsp;/g, " ").replace(/&amp;/g, "&");
  const norm = (s) => decode(s).replace(/\s+/g, " ").trim();
  const out = new Set();
  for (const m of source.matchAll(/\b(?:placeholder|title|aria-label)="([^"]*)"/g)) out.add(norm(m[1]));
  const body = source.replace(/<script[\s\S]*?<\/script>/g, "").replace(/<style[\s\S]*?<\/style>/g, "");
  for (const part of body.split(/<[^>]*>/)) {
    const text = norm(part);
    if (text) out.add(text);
  }
  return out;
}

test("every translated text is still used by the page or app.js", () => {
  const used = new Set([...jsStrings(read("../static/app.js")), ...htmlText(read("../static/index.html"))]);
  const unused = Object.keys(dictionary().text).filter((key) => !used.has(key));
  assert.deepEqual(unused, [], "remove or update these stale keys");
});

function unquoteGo(body) {
  return body.replace(/\\(x[0-9a-fA-F]{2}|u[0-9a-fA-F]{4}|U[0-9a-fA-F]{8}|.)/g, (_, e) => {
    if (e === "n") return "\n";
    if (e === "t") return "\t";
    if (e === "r") return "\r";
    if (e.length > 1) return String.fromCodePoint(parseInt(e.slice(1), 16));
    return e;
  });
}

// goStrings returns the string literals of the non-test Go sources, with
// adjacent literals joined by + merged into one.
function goStrings(dir, out = []) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = new URL(entry.name + (entry.isDirectory() ? "/" : ""), dir);
    if (entry.isDirectory()) goStrings(path, out);
    else if (entry.name.endsWith(".go") && !entry.name.endsWith("_test.go")) {
      const source = readFileSync(path, "utf8")
        .replace(/'(?:\\.|[^'\\\n])'/g, "0") // rune literals such as '"'
        .replace(/"\s*\+\s*"/g, "");
      for (const m of source.matchAll(/"((?:[^"\\\n]|\\.)*)"/g)) out.push(unquoteGo(m[1]));
    }
  }
  return out;
}

test("every message template matches text the server sends", () => {
  const corpus = goStrings(new URL("../../", import.meta.url)).join("\u0000");
  const missing = [];
  for (const [english] of dictionary().messages) {
    for (const part of english.split(/%[sd]/)) {
      if (part.trim().length >= 3 && !corpus.includes(part)) missing.push([english, part]);
    }
  }
  assert.deepEqual(missing, [], "these templates no longer match a server message");
});
