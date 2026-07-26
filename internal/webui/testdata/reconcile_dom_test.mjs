import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";

const source = readFileSync(
  new URL("../static/reconcile_state.js", import.meta.url),
  "utf8",
);
const context = {};
vm.createContext(context);
vm.runInContext(source, context);

function fixture() {
  const elements = {
    "import-reconcile-row": { style: { display: "none" } },
    "import-reconcile-hint": { style: { display: "none" } },
    "import-reconcile": { checked: false },
  };

  class FakeSelect extends EventTarget {
    value = "";
  }

  return {
    elements,
    lookup: (id) => elements[id],
    select: new FakeSelect(),
  };
}

test("native preview restores reconcile controls after translation is cleared", () => {
  const { elements, lookup, select } = fixture();
  const preview = { translated: false, tool: "codex" };
  const state = context.CCTReconcileState;

  state.configureReconcile(preview, lookup);
  state.bindTranslationChange(select, () => preview, lookup);

  assert.equal(elements["import-reconcile-row"].style.display, "flex");
  assert.equal(elements["import-reconcile-hint"].style.display, "block");

  select.value = "claude";
  select.dispatchEvent(new Event("change"));
  assert.equal(elements["import-reconcile-row"].style.display, "none");
  assert.equal(elements["import-reconcile-hint"].style.display, "none");

  select.value = "";
  select.dispatchEvent(new Event("change"));
  assert.equal(elements["import-reconcile-row"].style.display, "flex");
  assert.equal(elements["import-reconcile-hint"].style.display, "block");
});
