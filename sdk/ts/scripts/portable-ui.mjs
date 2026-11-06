import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";

// The SDK under test is actual TypeScript-emitted ESM, with no Bun loader or globals.
const ui = await import(pathToFileURL(process.argv[2]));
assert.equal(typeof globalThis.Bun, "undefined");
const { View, Section, Text, Button, createElement: h, callback, mount, useState, useEffect, useFetch, useInterval } = ui;
let cleanup = 0, stopped = 0, activeSignal;
const timers = { setInterval: fn => fn, clearInterval: () => { stopped++; } };
const settle = async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); };
function Page() {
  const [n, set] = useState(0);
  const result = useFetch("https://example.com/blocked");
  useEffect(signal => { activeSignal = signal; return () => { cleanup++; }; }, []);
  useInterval(() => {}, 100);
  return h(View, { key: "root", children: h(Section, { key: "main", children: [
    h(Text, { key: "value", text: String(n) }),
    h(Text, { key: "network", text: result.status }),
    h(Button, { key: "press", title: "fixture", action: callback(() => set(n + 1)) }),
  ] }) });
}
const session = mount(h(Page, { key: "page" }), { authorize: () => true, timers });
await settle();
assert.equal(session.current().view.sections[0].items[1].text, "error");
const snapshot = session.current(), id = snapshot.view.sections[0].items[2].action.id;
const event = { generation: snapshot.generation, version: snapshot.version, eventId: "one", key: "press", actionId: id, values: {} };
const accepted = session.event(event); await accepted; await settle();
assert.equal(session.current().view.sections[0].items[0].text, "1");
assert.equal(session.current().view.sections[0].items[2].action.id, id);
assert.equal(session.event(event), accepted);
session.suspend(); assert.equal(activeSignal.aborted, true); assert.equal(cleanup, 1); assert.equal(stopped, 1);
session.resume(); session.dispose(); assert.equal(cleanup, 2); assert.equal(stopped, 2);
console.log("PASS: TypeScript-emitted ESM hooks, fail-closed fetch, callbacks, replay and cleanup in Node without Bun globals");
