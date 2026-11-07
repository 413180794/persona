// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.mock("../assistant/TaskPanel", () => ({ AgentPanel: () => null, TaskPanel: () => null }));
vi.mock("../chat/SubagentPanel", () => ({ SubagentPanel: () => null }));
vi.mock("../../lib/use-fs-live", () => ({ useFsLive: () => {} }));
import type { GitStatus } from "../../lib/files";
import { getState } from "../../lib/store";
import { t } from "../../i18n";
import { GitPanel } from "./GitPanel";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// The Git panel's writes (#6121): each click sends what it names, the ones that leave the computer ask first, a
// refusal says what happened and lists its files.
type Pending = { path: string; body: unknown; answer: (value: unknown, status?: number) => void };
let pending: Pending[];
let host: HTMLDivElement;
let root: Root;
const features = getState().features;
const setFeatures = (list: string[]) => Object.assign(getState() as unknown as Record<string, unknown>, { features: list });

const status = (over: Partial<GitStatus> = {}): GitStatus => ({
  is_repo: true,
  root: "/projects/demo",
  branch: "fix/login",
  upstream: "origin/fix/login",
  ahead: 0,
  behind: 0,
  head: { hash: "a1", short: "a1", subject: "Start", author: "Author" },
  commits: [{ hash: "a1", short: "a1", subject: "Start", author: "Author" }],
  files: [
    { path: "src/login.ts", index: ".", worktree: "M", added: 3, deleted: 1 },
    { path: "src/new.ts", index: "?", worktree: "?", added: 0, deleted: 0 },
  ],
  ...over,
});
const button = (name: string) => [...document.querySelectorAll("button")].find((b) => b.textContent?.trim() === name || b.getAttribute("aria-label") === name);
const last = () => pending.at(-1)!;

function mount(writes: boolean) {
  setFeatures(writes ? [...features, "git_write"] : features.filter((f) => f !== "git_write"));
  act(() => root.render(<GitPanel mid="computer" sid="session" cwd="/projects/demo" onClose={() => {}} />));
}

beforeEach(() => {
  pending = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(
      (input: RequestInfo | URL, init?: RequestInit) =>
        new Promise<Response>((resolve) =>
          pending.push({
            path: String(input),
            body: init?.body ? JSON.parse(String(init.body)) : undefined,
            answer: (value, code = 200) => resolve(new Response(JSON.stringify(value), { status: code, headers: { "Content-Type": "application/json" } })),
          }),
        ),
    ),
  );
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  setFeatures(features);
  vi.unstubAllGlobals();
});

describe("GitPanel writes", () => {
  it("shows nothing to write on a hub without them", async () => {
    mount(false);
    await act(async () => last().answer(status()));
    expect(document.querySelector("[role=checkbox]")).toBeNull();
    expect(button(t("session-gitwrite.commit"))).toBeUndefined();
  });

  it("stages a file, then commits what is staged", async () => {
    mount(true);
    await act(async () => last().answer(status()));
    const commit = button(t("session-gitwrite.commit"))!;
    expect(commit.disabled).toBe(true);
    act(() => button(t("session-gitwrite.stage_file", { name: "src/login.ts" }))!.click());
    expect(last().path).toContain("/machines/computer/git/stage");
    expect(last().body).toEqual({ cwd: "/projects/demo", paths: ["src/login.ts"], unstage: false });
    const staged = status({ files: [{ path: "src/login.ts", index: "M", worktree: ".", added: 3, deleted: 1 }, status().files[1]!] });
    await act(async () => last().answer({ status: staged }));
    expect(document.querySelector("[role=checkbox]")!.getAttribute("aria-checked")).toBe("true");
    const box = document.querySelector("textarea")!;
    act(() => {
      const set = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!;
      set.call(box, "Handle the empty password");
      box.dispatchEvent(new Event("input", { bubbles: true }));
    });
    expect(button(t("session-gitwrite.commit"))!.disabled).toBe(false);
    act(() => button(t("session-gitwrite.commit"))!.click());
    expect(last().path).toContain("/git/commit");
    expect(last().body).toEqual({ cwd: "/projects/demo", message: "Handle the empty password" });
    const after = status({ ahead: 1, files: [status().files[1]!], head: { hash: "b2", short: "b2", subject: "Handle the empty password", author: "Author" } });
    await act(async () => last().answer({ status: after, commit: after.head }));
    expect(box.value).toBe("");
    expect(button(t("session-gitwrite.push"))).toBeDefined();
  });

  it("asks before pushing, and a new branch's push sets its upstream", async () => {
    mount(true);
    await act(async () => last().answer(status({ upstream: undefined })));
    const asked = pending.length;
    act(() => button(t("session-gitwrite.push"))!.click());
    expect(pending.length).toBe(asked);
    expect(document.body.textContent).toContain(t("session-gitwrite.push_new", { branch: "fix/login" }));
    const confirm = [...document.querySelectorAll("[role=dialog] button")].find((b) => b.textContent?.trim() === t("session-gitwrite.push")) as HTMLButtonElement;
    act(() => confirm.click());
    expect(last().path).toContain("/git/push");
    expect(last().body).toEqual({ cwd: "/projects/demo", set_upstream: true });
  });

  it("says a refusal with git's words and the files it lists", async () => {
    mount(true);
    await act(async () => last().answer(status()));
    act(() => button(t("session-gitwrite.stage_file", { name: "src/new.ts" }))!.click());
    await act(async () => last().answer({ error: "git add: fatal: Unable to create index.lock", code: "git_conflict", files: ["src/new.ts"] }, 409));
    const dialog = document.querySelector("[role=dialog]")!;
    expect(dialog.textContent).toContain(t("session-gitwrite.refused_git_conflict"));
    expect(dialog.textContent).toContain("src/new.ts");
    expect(dialog.textContent).toContain("index.lock");
  });
});
