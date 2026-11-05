// @vitest-environment jsdom
// UI version 4's code hosting components drawn from the 组件展示 pages (code-review, code-browse, code-overview), and
// what pressing them sends: the action with the values it carries and the component's own.
import { act as flush } from "react";
import { createRoot } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SHOWCASE } from "../../showcase/snapshots";
import type { UIAction } from "../../lib/extension-ui";
import { INERT, ViewSections } from "./ViewBody";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.IS_REACT_ACT_ENVIRONMENT = true;
  g.matchMedia = (q: string) => ({ matches: q.includes("min-width"), addEventListener() {}, removeEventListener() {} });
});

let host: HTMLDivElement | null = null;
afterEach(() => {
  host?.remove();
  host = null;
});

function mount(name: string, onAct: (a: UIAction, v?: Record<string, string>) => void) {
  host = document.createElement("div");
  document.body.append(host);
  const root = createRoot(host);
  flush(() => root.render(<ViewSections view={SHOWCASE[name]} {...INERT} onAct={onAct} />));
  return host;
}

const click = (el: Element | null | undefined) => {
  if (!el) throw new Error("not found");
  flush(() => (el as HTMLElement).click());
};
const byText = (root: Element, sel: string, text: string) => [...root.querySelectorAll(sel)].find((e) => e.textContent?.includes(text));
const type = (el: HTMLTextAreaElement, text: string) =>
  flush(() => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(el, text);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  });

describe("代码托管组件", () => {
  it.each(["code-review", "code-browse", "code-overview"])("draws %s", (name) => {
    const html = renderToStaticMarkup(<ViewSections view={SHOWCASE[name]} {...INERT} />);
    expect(html).not.toContain("undefined");
    if (name === "code-review") {
      expect(html).toContain("src/upload/client.go");
      expect(html).toContain("Should this back off");
      expect(html).toContain("time.Sleep");
    }
    if (name === "code-browse") expect(html).toContain("Retry calls f until");
    if (name === "code-overview") {
      expect(html).toContain("<table");
      expect(html).toContain("请求修改");
    }
  });

  it("opens a file from the tree with its path", () => {
    const sent: [string, Record<string, string> | undefined][] = [];
    const root = mount("code-review", (a, v) => sent.push([a.id, v]));
    click(byText(root, "[role=tree] button", "client.go"));
    expect(sent).toEqual([["open_file", { path: "src/upload/client.go" }]]);
  });

  it("asks for a directory still to load", () => {
    const sent: [string, Record<string, string> | undefined][] = [];
    const root = mount("code-browse", (a, v) => sent.push([a.id, v]));
    click(byText(root, "[role=tree] button", "docs"));
    expect(sent).toEqual([["open_dir", { path: "docs" }]]);
    click(byText(root, "nav button", "src"));
    expect(sent[1][0]).toBe("open_dir");
  });

  it("comments on a line, replies to a thread, shows hidden lines, loads a diff", () => {
    const sent: [string, Record<string, string> | undefined][] = [];
    const root = mount("code-review", (a, v) => sent.push([a.id, v]));
    click(root.querySelector('[aria-label="评论第 22 行"], [aria-label="Comment on line 22"]'));
    type(root.querySelector("textarea")!, "nit");
    click([...root.querySelectorAll("button")].find((b) => /^(评论|Comment)$/.test(b.textContent ?? "")));
    expect(sent.at(-1)?.[0]).toBe("inline_comment");
    expect(sent.at(-1)?.[1]).toMatchObject({ side: "new", body: "nit" });

    click([...root.querySelectorAll("button")].find((b) => /^(回复|Reply)…$/.test(b.textContent ?? "")));
    type(root.querySelector("textarea")!, "ok");
    click([...root.querySelectorAll("button")].filter((b) => /^(评论|Comment)$/.test(b.textContent ?? "")).at(-1));
    expect(sent.at(-1)).toEqual(["reply", { thread: "t1", body: "ok" }]);

    click(root.querySelector('[aria-label="展开上面的行"], [aria-label="Show lines above"]'));
    expect(sent.at(-1)?.[0]).toBe("expand");
    expect(sent.at(-1)?.[1]).toMatchObject({ path: "src/upload/client.go", direction: "up" });

    click(byText(root, "button", "加载改动") ?? byText(root, "button", "Load diff"));
    expect(sent.at(-1)).toEqual(["load_diff", { path: "docs/upload.md" }]);
  });

  it("sends what is typed with the composer's button", () => {
    const sent: [string, Record<string, string> | undefined][] = [];
    const root = mount("code-overview", (a, v) => sent.push([a.id, v]));
    type(root.querySelector("textarea")!, "LGTM");
    click(byText(root, "button", "批准"));
    expect(sent).toEqual([["approve", { body: "LGTM" }]]);
  });
});
