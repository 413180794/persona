// Run against an owned loopback Web build with an unreachable hub proxy.
import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { chromium } from "playwright-core";

const [address, artifacts, ...args] = process.argv.slice(2);
assert(address && artifacts, "Supply a loopback web origin and an artifact directory");
const base = new URL(address);
assert(base.protocol === "http:" && base.hostname === "127.0.0.1" && base.pathname === "/" && !base.username && !base.password && !base.search && !base.hash);
const executablePath = args.includes("--chromium") ? args[args.indexOf("--chromium") + 1] : undefined;
const output = resolve(artifacts);
await mkdir(output, { recursive: true });
const strings = JSON.parse(await readFile(new URL("../src/i18n/locales/en/workspaceboard.json", import.meta.url), "utf8"));
const states = JSON.parse(await readFile(new URL("../src/i18n/locales/en/lib-state-labels.json", import.meta.url), "utf8"));
const at = new Date().toISOString();
const earlier = new Date(Date.parse(at) - 3_600_000).toISOString();
const machine = { id: "fixture", name: "Fixture Linux", online: true, os: "linux" };
const session = (id, title, state, cwd = `/work/${id}`, updated_at = at) => ({
  machine_id: machine.id, session_id: id, title, state, status: state === "unread" ? "done" : state, cwd, updated_at,
});
const seen = session("seen", "Seen standalone session", "idle");
const workspaceSeen = session("workspace-seen", "Seen workspace session", "idle", "/work/seen-workspace");
const workspaceUnread = session("workspace-unread", "Unread workspace session", "unread", "/work/unread-workspace", earlier);
const workspaceNewer = session("workspace-newer", "Newer seen workspace session", "idle", "/work/unread-workspace");
const solo = session("running", "Running solo session", "working");
const unread = session("unread", "Unread standalone session", "unread");
const multiSeen = session("multi-unread", "Unread linked session", "unread");
const multiWorking = session("multi-working", "Running linked session", "working");
const sessions = [seen, workspaceSeen, workspaceUnread, workspaceNewer, solo, unread, multiSeen, multiWorking];
const bind = ({ machine_id, session_id }) => ({ machine_id, session_id, linked_at: earlier });
const todo = (id, title, fields = {}) => ({ id, title, created_at: earlier, updated_at: at, ...fields });
const ask = { verdict: "Please verify this fixture", asked_at: at };
const verify = todo("verify", "Owner verification requested", { ask });
const autoClosed = todo("auto-closed", "Automatically closed, awaiting verification", { ask, done_at: at, done_by: "auto" });
const accepted = todo("accepted", "Accepted closed work", {
  done_at: earlier, done_by: "review", seen_at: earlier,
  supervision: { supervisor: "fixture-main", state: "accepted", reviews: [{ at: earlier, verdict: "accepted" }] },
});
const running = todo("running", "Running to-do", { agents: [bind(solo)] });
const multi = todo("multi", "Multi-session to-do", { agents: [bind(multiSeen), bind(multiWorking)] });
const unreadTodo = todo("unread", "To-do with an ordinary unread session", { agents: [bind(unread)] });
const planned = todo("planned", "Planned to-do");
const todos = [verify, autoClosed, accepted, running, multi, unreadTodo, planned];
const card = (id, title) => ({
  id, title, machine_id: machine.id, cwd: `/work/${id}`, workspace_id: id, source: "folder", column_id: "todo", pinned: false, archived: false,
});
const board = {
  revision: 1, can_write: true, columns: [{ id: "todo", title: "To do" }],
  cards: [card("seen-workspace", "Seen workspace"), card("unread-workspace", "Unread workspace")],
};
const sessionID = ({ machine_id, session_id }) => `session:${machine_id}/${session_id}`;
const todoID = ({ id }) => `todo:${id}`;
const lanes = [
  ["needs_you", states.needs_you, []],
  ["working", states.working, [sessionID(solo), sessionID(multiWorking), todoID(running), todoID(multi)]],
  ["review", strings.auto_lane_review, [todoID(verify), todoID(autoClosed)]],
  ["unread", states.unread, [sessionID(unread), sessionID(multiSeen), todoID(unreadTodo), "workspace:unread-workspace"]],
  ["planned", strings.auto_lane_planned, [todoID(planned)]],
  ["idle", strings.auto_lane_idle, [sessionID(seen), "workspace:seen-workspace"]],
  ["done", states.done, [todoID(accepted)]],
];
const responses = {
  "/workspace-board": board,
  "/workspace-board/settings": { enabled: true, can_write: true },
  "/workspaces": { workspaces: [], errors: [] },
  "/machines": { machines: [machine] },
  "/sessions": { sessions },
  "/agents/sessions": { sessions },
  "/todos": { todos },
  "/interactions": { interactions: [] },
  "/assistant": { machine_id: machine.id, conversations: [], tasks: [], memory: [] },
  "/managers": { managers: [{ id: "assistant", name: "Fixture assistant", builtin: true, online: true }], default: "assistant" },
};
const result = { status: "running", origin: base.origin, fixture: { todos, sessions, workspaces: board.cards }, runs: [] };
const timeout = setTimeout(() => process.exit(124), 180_000);
const browser = await chromium.launch({ executablePath, headless: true });
result.chromium = browser.version();

try {
  for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
    const run = { viewport, lanes: [], details: [], writes: [], syntheticSeen: [], blocked: [], errors: [] };
    result.runs.push(run);
    const context = await browser.newContext({ viewport, reducedMotion: "reduce", serviceWorkers: "block" });
    await context.addInitScript(() => {
      localStorage.setItem("persona.token", "synthetic-workspace-review-fixture");
      localStorage.setItem("persona.language", "en");
      localStorage.setItem("persona.workspace-board.view", "auto");
      localStorage.setItem("agent.open", "page");
    });
    await context.route("**/*", (route) => {
      const request = route.request(), url = new URL(request.url());
      if (url.origin !== base.origin) {
        run.blocked.push(url.origin);
        return route.abort();
      }
      if (!url.pathname.startsWith("/api/")) return route.continue();
      const path = url.pathname.replace(/^\/api\/v1/, "");
      if (!["GET", "HEAD"].includes(request.method())) {
        run.writes.push({ method: request.method(), path });
        return route.fulfill({ status: 405, json: {} });
      }
      const selected = sessions.find((s) => path === `/sessions/${s.machine_id}/${s.session_id}`);
      const repositoryTodo = todos.find((t) => path === `/todos/${t.id}/repositories`);
      if (repositoryTodo) return route.fulfill({ json: { schema_version: 1, manager: "assistant", todo_id: repositoryTodo.id, revision: 0, repositories: [] } });
      return route.fulfill({ json: responses[path] ?? (selected ? { session: selected } : {}) });
    });
    await context.routeWebSocket("**/*", (socket) => {
      const url = new URL(socket.url());
      if (url.host !== base.host || url.pathname !== "/api/v1/ws") {
        // Vite's local development socket is unnecessary for this run.
        if (url.host !== base.host) run.blocked.push(url.host);
        return socket.close();
      }
      socket.send(JSON.stringify({ type: "hello", server_version: "synthetic-review", hub_name: "Fixture hub", protocol: 1, features: [] }));
      socket.onMessage((raw) => {
        const msg = JSON.parse(String(raw));
        if (msg.type === "app_ping") socket.send(JSON.stringify({ type: "pong", id: msg.id }));
        else if (msg.type === "seen") run.syntheticSeen.push(msg.session_id);
        else if (!["client_hello", "app_visible", "visibility", "activity", "subscribe", "unsubscribe"].includes(msg.type)) {
          run.errors.push(`Unexpected socket message: ${msg.type}`);
        }
      });
    });
    const page = await context.newPage();
    page.setDefaultTimeout(25_000);
    page.on("pageerror", (error) => run.errors.push(error.message));
    const picker = page.getByRole("combobox", { name: strings.pick_column, exact: true });
    const section = (label) => page.getByRole("region", { name: label, exact: true });
    const pick = async (key) => { if (await picker.isVisible()) await picker.selectOption(key); };
    const boardPage = async () => {
      await page.goto(`${base.origin}/workspace-board`);
      try {
        await page.locator(`[data-work-id="${todoID(verify)}"]`).waitFor({ state: "attached" });
      } catch (error) {
        run.errors.push((await page.locator("body").innerText()).slice(0, 1000));
        await capture("board-not-ready");
        throw error;
      }
    };
    const capture = (name) => page.screenshot({ path: join(output, `${viewport.width}-${name}.png`) });
    await boardPage();
    if (await picker.isVisible()) {
      assert.deepEqual(await picker.locator("option").allTextContents(), lanes.map(([, label, ids]) => `${label} · ${ids.length}`));
    }
    for (const [lane, label, expected] of lanes) {
      await pick(lane);
      const column = section(label);
      const count = Number(await column.getByRole("heading", { name: label, exact: true }).locator("xpath=following-sibling::*[1]").textContent());
      const ids = await column.locator("[data-work-id], [data-card-id]").evaluateAll((cards) =>
        cards.map((c) => c.dataset.workId ?? `workspace:${c.dataset.cardId}`));
      run.lanes.push({ lane, label, count, ids });
      assert.equal(count, expected.length, label);
      assert.deepEqual(ids.sort(), [...expected].sort(), label);
    }
    await pick("review");
    await section(strings.auto_lane_review).scrollIntoViewIfNeeded();
    await capture("board-review");

    async function openTodo(record, lane, expectedStatus) {
      await boardPage();
      await pick(lane);
      await page.locator(`[data-work-id="${todoID(record)}"]`).getByRole("button", { name: strings.work_open_todo }).click();
      const destination = `/todos?id=${encodeURIComponent(record.id)}`;
      await page.waitForURL(`${base.origin}${destination}`);
      const detail = page.getByTestId("todo-detail");
      const title = detail.getByRole("textbox").first();
      await title.waitFor();
      assert.equal(await title.inputValue(), record.title);
      const panel = detail.getByTestId("todo-ask-panel");
      assert.equal(await panel.count(), record.ask ? 1 : 0);
      if (record.ask) {
        assert.equal(await panel.getByText(ask.verdict, { exact: true }).count(), 1);
        assert.equal((await panel.getByRole("button").textContent()).trim(), "Accept");
      }
      const linked = (record.agents ?? []).map((a) => sessions.find((s) => s.session_id === a.session_id));
      const links = await detail.getByTestId("todo-detail-agent").evaluateAll((rows) => rows.map((row) => ({
        title: row.querySelector('[data-testid="todo-agent-title"]').textContent,
        state: row.querySelector(".status-mark").getAttribute("data-state"),
      })));
      assert.deepEqual(links, linked.map(({ title, state }) => ({ title, state })));
      const status = (await detail.getByTestId("todo-detail-status").textContent()).trim();
      const observation = { destination, title: record.title, status, links, ownerVerification: Boolean(record.ask) };
      run.details.push(observation);
      await capture(record.id);
      if (expectedStatus) assert.equal(status, expectedStatus, record.title);
      if (record === multi && viewport.width >= 1000) {
        observation.linkDestinations = [];
        for (const agent of linked) {
          await detail.getByTestId("todo-agent-title").filter({ hasText: agent.title }).click();
          const target = `/s/${agent.machine_id}/${agent.session_id}`;
          await page.waitForURL(`${base.origin}${target}`);
          await page.locator("[data-chat-title]").filter({ hasText: agent.title }).waitFor();
          observation.linkDestinations.push(target);
          await page.goBack();
          await detail.waitFor();
        }
      }
    }
    await openTodo(verify, "review");
    await openTodo(autoClosed, "review");
    await openTodo(accepted, "done", "Accepted");
    await openTodo(running, "working", states.working);
    await openTodo(unreadTodo, "unread", states.unread);
    await openTodo(multi, "working", states.working);

    if (viewport.width >= 1000) {
      async function openSession(cardLocator, record) {
        await cardLocator.getByRole("button", { name: strings.auto_open_session }).click();
        const destination = `/s/${record.machine_id}/${record.session_id}`;
        await page.waitForURL(`${base.origin}${destination}`);
        await page.locator("[data-chat-title]").filter({ hasText: record.title }).waitFor();
        run.details.push({ destination, title: record.title });
        await capture(record.session_id + "-session");
      }
      for (const record of [seen, unread, solo, multiSeen, multiWorking]) {
        await boardPage();
        await openSession(page.locator(`[data-work-id="${sessionID(record)}"]`), record);
      }
      for (const [id, record] of [["seen-workspace", workspaceSeen], ["unread-workspace", workspaceUnread]]) {
        await boardPage();
        await openSession(page.locator(`[data-card-id="${id}"]`).locator(".."), record);
      }
    }
    assert.deepEqual(run.writes, [], "No HTTP mutations");
    assert.deepEqual(run.errors, [], "No page errors or socket mutations");
    await context.close();
  }
  result.status = "passed";
} catch (error) {
  result.status = "failed";
  result.error = String(error);
  process.exitCode = 1;
} finally {
  clearTimeout(timeout);
  await browser.close();
  await writeFile(join(output, "results.json"), JSON.stringify(result, null, 2) + "\n");
  console.log(`Workspace board review: ${result.status}`, result.error ?? "");
}
