"use strict";

(function (global) {
  function configureReconcile(preview, lookup) {
    const row = lookup("import-reconcile-row");
    const hint = lookup("import-reconcile-hint");
    const box = lookup("import-reconcile");
    const nativeCodex =
      !preview.translated &&
      String(preview.tool || "codex").toLowerCase() !== "claude";

    if (!nativeCodex) {
      row.style.display = "none";
      hint.style.display = "none";
      box.checked = false;
      return;
    }

    row.style.display = "flex";
    hint.style.display = "block";
  }

  function bindTranslationChange(select, getLastPreview, lookup) {
    select.addEventListener("change", (event) => {
      const preview = event.target.value
        ? { translated: true }
        : getLastPreview();
      configureReconcile(preview || { translated: true }, lookup);
    });
  }

  global.CCTReconcileState = {
    bindTranslationChange,
    configureReconcile,
  };
})(globalThis);
