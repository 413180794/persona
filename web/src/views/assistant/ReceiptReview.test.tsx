import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it } from "vitest";
import { currentLanguage, initI18n } from "../../i18n";
import type { FeedItem } from "../../lib/feed";
import { getState } from "../../lib/store";
import type { Todo } from "../../lib/todos";
import { FeedLineView } from "./Tasks";

const now = "2026-11-07T09:00:00Z";
const todo: Todo = { id: "todo_review", title: "Check the connection", origin: "c1", created_at: now, updated_at: now, agents: [] };
const receipt: FeedItem = {
  id: "receipt_review", conversation: "c1", at: now, kind: "receipt", ref: todo.id,
  body: { title: todo.title, state: "done", summary: "The connection check passed." },
};
const before = { ...getState() };
const language = currentLanguage();
const seed = (todos: Todo[]) => Object.assign(getState(), { todos });
const render = (body: Record<string, unknown> = {}) => renderToStaticMarkup(<FeedLineView item={{ ...receipt, body: { ...receipt.body, ...body } }} />);

afterEach(async () => {
  Object.assign(getState(), before);
  await initI18n(language);
});

describe("a receipt awaiting Persona's review", () => {
  it("shows the review hint without a count when there are no criteria", () => {
    seed([todo]);
    const html = render({ review: { state: "awaiting", criteria: 0 } });
    expect(html).toContain("等 Persona 验收");
    expect(html).not.toContain("条验收标准");
    expect(html).toContain("Check the connection 停下了，还没完成");
    expect(html).not.toContain("bg-green-tint");
  });

  it("shows the number of acceptance criteria", () => {
    seed([todo]);
    expect(render({ review: { state: "awaiting", criteria: 3 } })).toContain("等 Persona 验收 · 3 条验收标准");
  });

  it("keeps legacy and ordinary receipts unchanged without an awaiting review", () => {
    seed([todo]);
    const legacy = render();
    expect(legacy).not.toContain("等 Persona 验收");
    for (const review of [undefined, null, {}, { state: "accepted", criteria: 3 }, { state: "unknown", criteria: 3 }]) {
      expect(render({ review })).toBe(legacy);
    }
  });

  it("hides the historical review hint when the todo is completed or no longer available", () => {
    for (const todos of [[{ ...todo, done_at: now }], []]) {
      seed(todos);
      const html = render({ review: { state: "awaiting", criteria: 3 } });
      expect(html).toBe(render());
      expect(html).toContain("Check the connection 完成");
      expect(html).toContain("bg-green-tint");
    }
  });

  it.each(["failed", "interrupted"])("does not change a %s receipt", (state) => {
    seed([todo]);
    expect(render({ state, review: { state: "awaiting", criteria: 3 } })).toBe(render({ state }));
  });

  it.each([
    ["en", 0, "Waiting for Persona&#x27;s review"],
    ["en", 1, "Waiting for Persona&#x27;s review · 1 acceptance criterion"],
    ["en", 3, "Waiting for Persona&#x27;s review · 3 acceptance criteria"],
    ["ja", 0, "Personaの検収待ち"],
    ["ja", 3, "Personaの検収待ち · 検収基準3件"],
  ] as const)("localizes %s review copy with %i criteria", async (lang, criteria, expected) => {
    await initI18n(lang);
    seed([todo]);
    expect(render({ review: { state: "awaiting", criteria } })).toContain(expected);
  });
});
