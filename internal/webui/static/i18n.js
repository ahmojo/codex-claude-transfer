"use strict";

// UI localization. English is the source text and the fallback: text without a
// translation stays English, so new UI never breaks another language. The
// server marks the page with <html lang>, and a dictionary file
// (i18n.<lang>.js) registers two tables for that language:
//
//   text      English text -> translation. Keys are page text as written in
//             index.html (whitespace collapsed) and the strings app.js passes
//             to t(), where {name} stands for a value. A translation may be a
//             function of those values, for plural forms.
//   messages  [English, translation] templates for server messages, with %s
//             and %d where the server inserts a value. A template must match
//             the whole message. Inserted values are kept as they are, and are
//             only translated themselves when one matches a template in full.
(function (global) {
  const doc = global.document;
  const lang = ((doc && doc.documentElement.lang) || "en").toLowerCase();
  const text = {};
  const messages = [];

  function register(code, dict) {
    if (code !== lang) return;
    Object.assign(text, dict.text || {});
    for (const [english, translated] of dict.messages || []) {
      messages.push({ pattern: templatePattern(english), translated });
    }
  }

  function fill(s, vars) {
    return s.replace(/\{(\w+)\}/g, (m, name) => (vars && name in vars ? String(vars[name]) : m));
  }

  function t(key, vars) {
    const tr = Object.prototype.hasOwnProperty.call(text, key) ? text[key] : undefined;
    if (typeof tr === "function") return tr(vars || {});
    return fill(typeof tr === "string" ? tr : key, vars);
  }

  function templatePattern(template) {
    const source = template.split(/(%[sd])/).map(part => {
      if (part === "%d") return "(-?\\d+)";
      if (part === "%s") return "(.+?)";
      return part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    }).join("");
    return new RegExp("^" + source + "$", "s");
  }

  function msg(message) {
    const s = String(message == null ? "" : message);
    for (const { pattern, translated } of messages) {
      const m = pattern.exec(s);
      if (!m) continue;
      let i = 0;
      return translated.replace(/%[sd]/g, () => {
        const value = m[++i];
        return value === s ? value : msg(value);
      });
    }
    return s;
  }

  // translatePage localizes the static page once, before app.js renders
  // anything, so user data never passes through it.
  function translatePage(root) {
    if (!Object.keys(text).length) return;
    const walker = doc.createTreeWalker(root, 1 | 4); // elements and text
    for (let n = walker.currentNode; n; n = walker.nextNode()) {
      if (n.nodeType === 3) {
        const key = n.nodeValue.replace(/\s+/g, " ").trim();
        if (key && typeof text[key] === "string") {
          // A translation that starts with punctuation attaches to the word before.
          const lead = /^[,.;:!?)]/.test(text[key]) ? "" : n.nodeValue.match(/^\s*/)[0];
          n.nodeValue = lead + text[key] + n.nodeValue.match(/\s*$/)[0];
        }
        continue;
      }
      for (const attr of ["placeholder", "title", "aria-label"]) {
        const key = (n.getAttribute(attr) || "").replace(/\s+/g, " ").trim();
        if (key && typeof text[key] === "string") n.setAttribute(attr, text[key]);
      }
    }
  }

  global.I18N = { lang, register, t, msg, translatePage };
})(globalThis);
