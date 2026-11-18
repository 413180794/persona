// Real optimized Persona Web with synthetic HTTP/WebSocket data; loopback only, no user's hub or external requests.
// node web/e2e/parked-wait-info.mjs <loopback-web-url> --shots <directory> [--chromium <executable>] [--baseline] [--headed]
import assert from "node:assert/strict";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const args = process.argv.slice(2);
const flag = (n) => { const i = args.indexOf(n); return i < 0 ? undefined : args[i + 1]; };
const base = args[0]?.replace(/\/$/, "");
const shots = flag("--shots");
assert(base && shots, "usage: node web/e2e/parked-wait-info.mjs <loopback-web-url> --shots <directory> [--chromium <executable>] [--baseline] [--headed]");
const origin = new URL(base);
const baseline = args.includes("--baseline");
assert(origin.protocol === "http:" && ["127.0.0.1", "localhost", "[::1]"].includes(origin.hostname), "only a loopback web server is allowed");
assert(!origin.username && !origin.password && !origin.search && !origin.hash);
mkdirSync(shots, { recursive: true });
const timeout = setTimeout(() => { console.error("TIMEOUT parked-wait-info browser checks (180s)"); process.exit(124); }, 180_000);
const now = Date.now();
const iso = (ms) => new Date(now + ms).toISOString();
const since = iso(-3 * 86400_000);
const contact = { channel: "Email", url: "https://example.test/thread/7", sender: "Riley", sent_at: iso(-28 * 3600_000), request: "Please confirm the revised date", status: "sent" };
const firstContact = { ...contact, url: "https://example.test/thread/6", sent_at: iso(-100 * 3600_000) };
const pr = (n, review = "required") => ({ url: `https://github.com/example/demo/pull/${n}`, state: "open", draft: false, review, checks: "pending", conflicts: false, checked_at: iso(-60_000) });
const evidence = (kind, target, wake, more = {}) => ({ kind, target, wake, recorded_by: "Synthetic supervisor", ...more });
const sid = "demo-main";
const agent = (session_id) => ({ machine_id: "demo", session_id, linked_at: since });
function dateTask(id, title, sessionIds, minutes, reason, record) {
  return { id, title, created_at: since, updated_at: iso(-60_000), agents: sessionIds.map(agent),
    supervision: { supervisor: sid, state: "parked",
      parked: { reason, until: iso(minutes * 60_000), waiting_on: "date", count: 1, evidence: record },
      reviews: [{ at: iso(-2 * 3600_000), verdict: "parked", parks: 1, evidence: record }] } };
}
const mixed = dateTask("recheck-build", "Validate release build", ["build-unread", "build-running"], 25,
  "Recheck the running build", evidence("date", "Release build", "Read the build result", { url: "https://example.test/builds/7" }));
const background = dateTask("recheck-artifacts", "Prepare release artifacts", ["artifact-worker"], 80,
  "Recheck the background jobs", evidence("date", "Artifact jobs", "Read the artifact report"));
const deploy = dateTask("wait-deploy", "Deploy the validated release", ["deploy-worker"], 24 * 60 + 145,
  "Deployment waits for the build session to finish", evidence("date", "Release build session", "Deploy after build-running exits"));
const rechecks = [mixed, background];
const tasks = [
  { id: "wait-person", title: "Confirm release date", created_at: since, updated_at: iso(-60_000), agents: [{ machine_id: "demo", session_id: "wait-person-agent", linked_at: since }], supervision: { supervisor: "demo-main", state: "parked", parked: { reason: "Waiting for the design team", until: iso(-3600_000), waiting_on: "someone_else", count: 2, evidence: evidence("person_reply", "Design team", "They confirm a date", { contact }) }, reviews: [
    { at: iso(-99 * 3600_000), verdict: "parked", parks: 1, evidence: evidence("person_reply", "Design team", "They confirm a date", { contact: firstContact }) },
    { at: iso(-2 * 3600_000), verdict: "parked", parks: 2, evidence: evidence("person_reply", "Design team", "They confirm a date", { contact }) },
  ] } },
  { id: "wait-review", title: "Merge schedule update", created_at: since, updated_at: iso(-60_000), agents: [], pr_url: pr(73).url, pr_status: pr(73), supervision: { supervisor: "demo-main", state: "parked", parked: { reason: "Waiting for review", until: iso(2 * 86400_000), waiting_on: "someone_else", count: 1, evidence: evidence("merge_review", "Review team", "Approval and merge", { url: pr(73).url, ready: true, contact: { ...contact, url: "https://example.test/review/73" } }) }, reviews: [{ at: iso(-27 * 3600_000), verdict: "parked", parks: 1, evidence: evidence("merge_review", "Review team", "Approval and merge", { url: pr(73).url, ready: true, contact: { ...contact, url: "https://example.test/review/73" } }) }] } },
  { id: "wait-legacy", title: "Check vendor delivery", created_at: since, updated_at: iso(-60_000), agents: [], supervision: { supervisor: "demo-main", state: "parked", parked: { reason: "Waiting on vendor", until: iso(86400_000), waiting_on: "external_event", count: 1 }, reviews: [] } },
  { id: "wait-unrecorded-review", title: "Merge earlier copy update", created_at: since, updated_at: iso(-60_000), agents: [], pr_url: pr(74).url, pr_status: pr(74) },
  { id: "wait-active", title: "Draft rollout notes", created_at: since, updated_at: iso(-60_000), agents: [{ machine_id: "demo", session_id: "active-agent", linked_at: since }], supervision: { supervisor: "demo-main", state: "pending_review" } },
  ...rechecks, deploy,
].map(task => ({ ...task, origin: sid })).reverse();
const waitingOrder = ["wait-person", "wait-review", deploy.id, "wait-legacy", "wait-unrecorded-review"];
const workingCount = rechecks.length + 1; // also the existing rollout draft
const runtime = {
  "wait-person-agent": { state: "unread" },
  "active-agent": { state: "working" },
  "build-unread": { state: "unread" },
  "build-running": { state: "working" },
  "artifact-worker": { state: "idle", background_tasks_count: 2 },
  "deploy-worker": { state: "idle", work: { phase: "blocked", on: "agent",
    agent: { machine_id: "demo", session_id: "build-running" }, line: deploy.supervision.parked.reason } },
};
const agentSessions = tasks.flatMap(({ id, title, agents }) => agents.map(binding => {
  const live = runtime[binding.session_id];
  return { ...binding, ...live, status: live.state === "working" ? "working" : "idle", todo: { id, title },
    title: binding.session_id, updated_at: since, state_since: since };
}));
const reads = [], writes = [], forbidden = [], cases = [];
const api = async (route) => {
  const req = route.request(), path = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
  if (req.method() !== "GET") {
    writes.push({ method: req.method(), path });
    // The pre-existing Today-card refresh is mocked too; no model or real hub is contacted.
    if (!/\/(?:read|seen)$/.test(path) && path !== "/assistant/conversations/demo-main/today") {
      forbidden.push({ method: req.method(), path });
      await route.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ error: "unexpected write in isolated fixture" }) }); return;
    }
    await route.fulfill({ contentType: "application/json", body: JSON.stringify({ ok: true }) }); return;
  }
  reads.push(path);
  const session = { machine_id: "demo", session_id: sid, title: "Example conversation", assistant: true, state: "idle", status: "idle", updated_at: since };
  let body = {};
  if (path === "/machines") body = { machines: [{ id: "demo", name: "Example computer", online: true, os: "linux" }] };
  else if (path === "/sessions") body = { sessions: [session, ...agentSessions] };
  else if (path === "/interactions") body = { interactions: [] };
  else if (path === "/assistant") body = { machine_id: "demo", main: sid, conversations: [session], tasks: [], memory: [] };
  else if (path === "/todos") body = { todos: tasks };
  else if (path === "/assistant/repositories") body = { manager: req.headers()["x-persona-manager"] || "assistant", catalog_revision: 0, repositories: [] };
  else if (/^\/todos\/[^/]+\/repositories$/.test(path)) body = { schema_version: 1, manager: req.headers()["x-persona-manager"] || "assistant", todo_id: path.split("/")[2], revision: 0, repositories: [] };
  else if (path === `/sessions/demo/${sid}`) body = { session };
  else if (path.endsWith("/feed")) body = { items: [] };
  else if (path.endsWith("/items")) body = { items: [], has_more: false };
  else if (path === "/extensions/ui") body = { slots: [] };
  else if (path === "/extensions") body = { extensions: [] };
  else if (path === "/managers") body = { managers: [] };
  await route.fulfill({ contentType: "application/json", body: JSON.stringify(body) });
};
async function checkLocalTime(row, task) {
  await row.waitFor();
  const [text, localTime] = await row.evaluate((element, until) => {
    const at = new Date(until);
    const dateStyle = at.toDateString() === new Date().toDateString() ? undefined : "medium";
    const time = new Intl.DateTimeFormat(navigator.language, { dateStyle, timeStyle: "short" }).format(at);
    return [element.innerText.replace(/\s+/g, " "), time.replace(/\s+/g, " ")];
  }, task.supervision.parked.until);
  assert(text.includes(localTime), `${task.title}: expected local time "${localTime}" in "${text}"`);
  return text;
}
const browserPromise = chromium.launch({ executablePath: flag("--chromium") || "/usr/bin/chromium", headless: !args.includes("--headed") });
let browser;
try {
  browser = await browserPromise;
  const modes = [
    ...["light", "dark"].flatMap(theme => ["standard", "large"].flatMap(size => ["desktop", "phone"].map(view => ({ theme, size, view, lang: "en" })))),
    { theme: "light", size: "standard", view: "phone", lang: "zh-Hans" },
    { theme: "dark", size: "large", view: "phone", lang: "ja" },
  ];
  for (const [i, mode] of modes.entries()) {
    if (baseline && (mode.lang !== "en" || mode.size !== "standard")) continue;
    const started = performance.now();
    const context = await browser.newContext({ viewport: mode.view === "desktop" ? { width: 1280, height: 900 } : { width: 390, height: 844 },
      hasTouch: mode.view === "phone", isMobile: mode.view === "phone", colorScheme: mode.theme, reducedMotion: "reduce", serviceWorkers: "block",
      locale: mode.lang, timezoneId: mode.lang === "zh-Hans" ? "Asia/Shanghai" : "America/Los_Angeles" });
    await context.addInitScript(({ theme, lang, size }) => {
      localStorage.setItem("persona.token", "synthetic-demo-token"); localStorage.setItem("persona.language", lang);
      localStorage.setItem("persona.theme", theme); localStorage.setItem("persona.text-size", size);
      localStorage.setItem("persona.todos.view", "list"); localStorage.setItem("persona.todos.grouping", "status");
      // The isolated test clipboard never reads or overwrites the desktop user's clipboard.
      window.__testClipboard = "";
      Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async text => { window.__testClipboard = text; } } });
    }, { theme: mode.theme, lang: mode.lang, size: mode.size });
    await context.route("**/*", async route => {
      const url = new URL(route.request().url());
      if (url.origin !== origin.origin) { await route.abort(); return; }
      if (url.pathname.startsWith("/api/v1/")) { await api(route); return; }
      await route.continue();
    });
    await context.routeWebSocket("**/api/v1/ws**", ws => {
      ws.send(JSON.stringify({ type: "hello", server_version: "synthetic", hub_name: "Example hub", features: [] }));
      ws.onMessage(raw => { const m = JSON.parse(String(raw)); if (m.type === "app_ping") ws.send(JSON.stringify({ type: "pong" })); });
    });
    const page = await context.newPage(), errors = [];
    page.on("pageerror", e => errors.push(String(e))); page.setDefaultTimeout(10_000);
    const stem = `wait-info-${i + 1}-${mode.view}-${mode.theme}-${mode.size}-${mode.lang}`;
    const shot = async (step) => {
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth), false, `${step}: no horizontal overflow`);
      await page.screenshot({ path: join(shots, `${stem}-${step}.png`) });
    };
    const words = {
      en: { copy: "Copy draft", cancel: "Cancel", all: "All", nextCheck: "Next check" },
      "zh-Hans": { copy: "复制话术", cancel: "取消", all: "全部", nextCheck: "下次复查" },
      ja: { copy: "下書きをコピー", cancel: "キャンセル", all: "すべて", nextCheck: "次回の確認" },
    }[mode.lang];
    const schedules = {};
    const checkWorking = async (rows) => {
      for (const task of rechecks) {
        const row = rows.filter({ has: page.getByText(task.title, { exact: true }) });
        schedules[task.id] = await checkLocalTime(row.getByText(words.nextCheck), task);
      }
      const draft = rows.filter({ has: page.getByText("Draft rollout notes", { exact: true }) });
      assert(!(await draft.innerText()).includes(words.nextCheck), "ordinary running work has no scheduled check");
      assert.equal(await draft.locator("[data-task-wait-info]").count(), 0);
    };
    const checkParked = async (scope, capture = false) => {
      const date = scope.locator(`[data-task-wait-info="${deploy.id}"]`);
      const disclosure = date.locator("summary");
      await disclosure.click();
      const text = await checkLocalTime(date, deploy);
      const { reason, evidence } = deploy.supervision.parked;
      assert.equal(await date.getAttribute("data-wait-kind"), "date");
      assert(text.includes(reason) && text.includes(evidence.target) && text.includes(evidence.wake), `deployment wait: ${text}`);
      assert.equal(await date.locator("[data-wait-contact-missing]").count(), 0);
      assert.deepEqual(await scope.locator("[data-task-wait-info]").evaluateAll(rows => rows.map(row => row.dataset.taskWaitInfo)), waitingOrder);
      if (capture) {
        schedules[deploy.id] = text;
        await date.scrollIntoViewIfNeeded();
        await shot("date");
      }
      await disclosure.click();
    };
    try {
      await page.goto(`${base}/`);
      await page.evaluate(() => document.fonts.ready);
      const running = page.locator('[data-tray-entry="running"]'); await running.waitFor();
      const parked = page.locator('[data-tray-entry="parked"]');
      if (!baseline) {
        await running.getByText(String(workingCount), { exact: true }).waitFor();
        await parked.getByText(String(waitingOrder.length), { exact: true }).waitFor();
      }
      const grouped = await parked.count() > 0;
      // Let startup effects finish their synthetic responses before measuring the local-only tray interaction.
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      const preReadCount = reads.length;
      const feedback = await page.evaluate(async (key) => {
        const start = performance.now();
        document.querySelector(`[data-tray-entry="${key}"]`).click();
        await new Promise(requestAnimationFrame);
        return { start, ms: performance.now() - start };
      }, grouped ? "parked" : "running");
      if (baseline) {
        await page.locator(grouped ? "[data-parked-task-row]" : "[data-task-slot]").first().waitFor();
        const readyMs = await page.evaluate(start => performance.now() - start, feedback.start);
        // Grouped baselines can already show wait evidence from before timed rechecks.
        if (!grouped) assert.equal(await page.locator("[data-task-wait-info]").count(), 0);
        assert.equal(reads.length, preReadCount, `opening reads: ${JSON.stringify(reads.slice(preReadCount))}`);
        await shot("before");
        assert.deepEqual(errors, []);
        cases.push({ ...mode, baseline, feedbackMs: feedback.ms, readyMs, extraMetadataGets: 0, screenshots: 1 });
        continue;
      }
      const person = page.locator('[data-task-wait-info="wait-person"]'); await person.waitFor();
      assert.deepEqual(await page.locator("[data-parked-task-row]").evaluateAll(rows => rows.map(row => row.dataset.parkedTaskRow)), waitingOrder);
      const readyMs = await page.evaluate(start => performance.now() - start, feedback.start);
      assert(feedback.ms < 1000 && readyMs < 1000, "local feedback and information-ready are measured separately, both under 1s");
      assert.equal(await person.getAttribute("data-wait-kind"), "person_reply");
      assert((await person.locator("[data-wait-contact]").innerText()).includes("Riley"));
      assert.equal(await person.locator("[data-wait-age]").getAttribute("title"), firstContact.sent_at, "a later request does not reset the first wait");
      assert.equal(await person.getAttribute("data-wait-overdue"), "true");
      assert.equal(await person.locator("[data-wait-contact]").getAttribute("href"), contact.url);
      assert((await person.locator("[data-wait-wake]").innerText()).includes("They confirm a date"));
      assert.equal(reads.length, preReadCount, `opening wait metadata reads: ${JSON.stringify(reads.slice(preReadCount))}`);
      const extraMetadataGets = reads.length - preReadCount;
      const tray = page.locator(".assistant-action-tray");
      await checkParked(tray, true);
      await person.locator("summary").click();
      await person.locator("[data-wait-request]").waitFor({ state: "visible" });
      assert((await person.locator("[data-wait-reason]").innerText()).includes("design team"));
      if (mode.view === "phone") {
        for (const control of ["[data-wait-contact]", "summary", '[data-testid="wait-reminder"]']) {
          const box = await person.locator(control).boundingBox();
          assert(box && box.width >= 44 && box.height >= 44, `touch control ${control} has a 44px target`);
        }
      }
      await person.scrollIntoViewIfNeeded();
      await shot("progress");
      const writesBeforeDraft = writes.length;
      await person.locator('[data-testid="wait-reminder"]').click();
      const dialog = page.getByRole("dialog");
      await dialog.locator("textarea").fill("Edited synthetic reminder draft");
      await shot("draft");
      await dialog.getByRole("button", { name: words.cancel, exact: true }).click();
      assert.equal(writes.length, writesBeforeDraft, "cancelling a draft does not write or send");
      await person.locator('[data-testid="wait-reminder"]').click();
      await dialog.locator("textarea").fill("Edited synthetic reminder draft");
      await dialog.getByRole("button", { name: words.copy, exact: true }).click();
      assert.equal(await page.evaluate(() => window.__testClipboard), "Edited synthetic reminder draft");
      assert.equal(writes.length, writesBeforeDraft, "copying the draft does not write task state or send a message");
      const draftWrites = writes.length - writesBeforeDraft;
      await dialog.waitFor({ state: "hidden" });
      const review = page.locator('[data-task-wait-info="wait-review"]'); await review.waitFor();
      assert.equal(await review.getAttribute("data-wait-kind"), "merge_review");
      assert.equal(await review.locator('[data-pr-waiting-url="https://github.com/example/demo/pull/73"]').count(), 1);
      assert.equal(await review.locator('[data-wait-source][href="https://github.com/example/demo/pull/73"]').count(), 1);
      assert.equal(await review.locator("[data-wait-contact]").count(), 1);
      const unknown = page.locator('[data-task-wait-info="wait-unrecorded-review"]');
      assert.equal(await unknown.locator("[data-wait-contact-missing]").count(), 1);
      assert.equal(await unknown.locator("[data-wait-age]").getAttribute("title"), null, "poll time is not a wait start");
      await review.scrollIntoViewIfNeeded();
      await shot("pr");
      await running.click();
      const runningRows = tray.locator("[data-task-slot]");
      await checkWorking(runningRows);
      assert.equal(await runningRows.count(), workingCount);
      await shot("running");
      await tray.getByRole("button", { name: words.all, exact: true }).click();
      const board = page.locator('[data-testid="task-board"]');
      const boardWorking = board.locator('[data-board-section="working"]'), boardParked = board.locator('[data-board-section="parked"]');
      await checkWorking(boardWorking.getByRole("button"));
      await checkParked(boardParked);
      assert((await boardWorking.getByRole("heading").innerText()).endsWith(` · ${workingCount}`));
      assert((await boardParked.getByRole("heading").innerText()).endsWith(` · ${waitingOrder.length}`));
      await shot("all");
      await page.goto(`${base}/todos`);
      const listWorking = page.locator('[data-testid="todo-section"][data-section="working"]');
      const listParked = page.locator('[data-testid="todo-section"][data-section="parked"]');
      await checkWorking(listWorking.locator('[data-todo][role="button"]'));
      await checkParked(listParked.locator("[data-todo]"));
      assert.equal(await listWorking.getAttribute("data-todos"), String(workingCount));
      assert.equal(await listParked.getAttribute("data-todos"), String(waitingOrder.length));
      assert.equal(await listParked.locator('[data-task-wait-info="wait-legacy"] [data-wait-contact-missing]').count(), 1);
      await shot("todos");
      await page.locator('[data-todo="wait-person"] [data-wait-target]').click();
      const details = page.locator('[data-testid="review-checklist"] [data-task-wait-info="wait-person"]');
      await details.locator("[data-wait-request]").waitFor({ state: "visible" });
      assert.equal(await details.locator("[data-wait-contact]").getAttribute("href"), contact.url);
      assert.equal(await details.locator("[data-wait-age]").getAttribute("title"), firstContact.sent_at);
      await details.scrollIntoViewIfNeeded();
      await shot("details");
      await page.goto(`${base}/todos?id=${mixed.id}`);
      const checklist = page.locator('[data-testid="review-checklist"]');
      await checkLocalTime(checklist.getByText(words.nextCheck), mixed);
      const activeInfo = checklist.locator(`[data-task-wait-info="${mixed.id}"]`);
      assert.equal(await activeInfo.locator("[data-wait-reason]").innerText(), mixed.supervision.parked.reason);
      assert.equal(await activeInfo.locator("[data-wait-source]").getAttribute("href"), mixed.supervision.parked.evidence.url);
      assert.equal(await activeInfo.locator("[data-wait-contact-missing], [data-wait-age]").count(), 0);
      assert.deepEqual(errors, []);
      cases.push({ ...mode, grouped, progress: true, prTray: true, running: workingCount, all: waitingOrder.length, todos: waitingOrder.length, schedules, screenshots: 8, feedbackMs: feedback.ms,
        readyMs, extraMetadataGets, draftWrites, elapsedMs: Math.round(performance.now() - started) });
    } catch (error) {
      await page.screenshot({ path: join(shots, `${stem}-failed.png`) }).catch(() => {});
      writeFileSync(join(shots, `${stem}-failed.json`), JSON.stringify({ reads, writes, forbidden, errors }, null, 2) + "\n");
      throw error;
    } finally { await context.close(); }
  }
  const summary = { baseline, cases: cases.length, results: cases, screenshots: cases.reduce((n, c) => n + c.screenshots, 0), apiReads: reads.length, writes };
  assert.equal(cases.length, baseline ? 4 : 10);
  assert.deepEqual(forbidden, [], "metadata view must not write task state or send messages");
  writeFileSync(join(shots, "readings.json"), JSON.stringify(summary, null, 2) + "\n");
  console.log(`PASS wait-info browser: ${cases.length} presentation cases; ${summary.screenshots} screenshots; API reads=${reads.length}; task writes=0`);
} catch (error) {
  console.error("FAIL wait-info browser", error); process.exitCode = 1;
} finally { clearTimeout(timeout); await browser?.close(); }
