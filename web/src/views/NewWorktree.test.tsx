import { afterEach, describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";

// the modules read the colour scheme and this browser's settings when they load
vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
  const kept = new Map<string, string>();
  g.localStorage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) };
});

vi.mock("react-dom", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-dom")>()),
  createPortal: (children: React.ReactNode) => children,
}));
vi.mock("../lib/router", () => ({ href: () => "/", navigate: vi.fn() }));

import { NewWorktree, NewWorktreeHost } from "./NewWorktree";

afterEach(() => vi.unstubAllGlobals());

describe("新建 worktree 的对话框", () => {
  it("shows nothing until a workspace's menu asks for it", () => {
    expect(renderToStaticMarkup(<NewWorktreeHost />)).toBe("");
  });

  it("reads the repository first, with the create button waiting", () => {
    vi.stubGlobal("document", { body: {} }); // the portal's target (createPortal is mocked)
    const html = renderToStaticMarkup(<NewWorktree request={{ target: { mid: "m1", workspaceId: "w1", host: "herdr" }, changed: () => {} }} onClose={() => {}} />);
    expect(html).toContain("新建 worktree");
    expect(html).toContain("正在读取仓库…");
    expect(html).toContain("创建并打开");
    expect(html).toMatch(/<button[^>]*disabled=""[^>]*>[^]*创建并打开/);
    expect(html).not.toContain("检出路径");
  });
});
