import { expect, test } from "bun:test";
import { connect } from "../sdk/ts/src/index.ts";
import { program } from "./app.ts";

// The hub's end of the protocol: send a request, wait for its answer.
function hub() {
  const answers = new Map<string, (f: any) => void>();
  const conn = connect(program(), (l) => {
    const f = JSON.parse(l);
    if (f.id && !f.method) answers.get(f.id)?.(f);
  });
  let n = 0;
  return (method: string, params: unknown = {}) =>
    new Promise<any>((resolve) => {
      const id = `h${++n}`;
      answers.set(id, resolve);
      conn.line(JSON.stringify({ id, method, params }));
    });
}

test("search and open", async () => {
  const call = hub();
  await call("initialize", { protocol: 1, hub: { build: 0 }, extension: { id: "{{id}}", version: "0.1.0" }, config: { greeting: "早" }, granted: {} });
  const ctx = { contribution: "{{id}}", placement: "nav_page", size: "full", ui_version: 3 };
  let { result } = await call("ui.action", { ...ctx, action: "search", values: { q: "条目 2" } });
  expect(result.view.sections[0].items[0].text).toBe("早");
  expect(result.view.search.value).toBe("条目 2");
  // Items 2 and 20; a row opens its detail on top of the list.
  const rows = result.view.sections[1].items;
  expect(rows).toHaveLength(2);
  ({ result } = await call("ui.render", { ...ctx, state: rows[1].action.push.state }));
  expect(result.title).toBe("示例条目 20");
  // An app before UI version 3 taps the row: the SDK draws the detail in place of the list.
  const old = { ...ctx, ui_version: 2 };
  ({ result } = await call("ui.render", old));
  ({ result } = await call("ui.action", { ...old, action: result.sections[1].items[0].action.id }));
  expect(result.view.title).toBe("示例条目 1");
});
