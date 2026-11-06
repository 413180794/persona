// @vitest-environment jsdom
// Path presence across a hub outage and over a long tab life: paths that got no answer are asked again when the hub
// says hello, the remembered answers stay bounded (expired first, then the oldest), and a reset drops late answers.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.fn();
vi.mock("./api", () => ({ api: (...a: unknown[]) => api(...a), enc: encodeURIComponent }));

// The store's hello listeners, called as the socket would on a reconnect.
const hello = vi.hoisted(() => new Set<(m: Record<string, unknown>) => void>());
vi.mock("./store", () => ({
  onHubEvent: (type: string, l: (m: Record<string, unknown>) => void) => {
    if (type === "hello") hello.add(l);
    return () => hello.delete(l);
  },
}));

import { askPaths, pathStat, resetPathPresence, usePathLinks } from "./path-presence";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const statsOf = (url: string, kind: (p: string) => string) => {
  const paths = new URL(url, "http://x").searchParams.get("paths")!.split("\n");
  return { stats: paths.map((p) => ({ path: p, kind: kind(p) })) };
};
const answered = () => act(() => vi.advanceTimersByTimeAsync(100)); // past the batch wait, and the answer

function Links({ paths }: { paths: string[] }) {
  const { key } = usePathLinks("mac", "/repo", paths);
  return <p>{key}</p>;
}

let root: Root | undefined;
let host: HTMLDivElement;

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "Date"] });
  resetPathPresence();
  api.mockReset();
  host = document.createElement("div");
});

afterEach(() => {
  act(() => root?.unmount());
  root = undefined;
  resetPathPresence();
  vi.useRealTimers();
});

describe("path presence", () => {
  it("asks again, within the minute, the paths that got no answer once the hub says hello", async () => {
    api.mockRejectedValueOnce(new Error("hub unreachable"));
    root = createRoot(host);
    act(() => root!.render(<Links paths={["src/a.ts"]} />));
    await answered();
    expect(api).toHaveBeenCalledTimes(1);
    expect(host.textContent).toBe(""); // no answer: plain text

    api.mockImplementation(async (url: string) => statsOf(url, () => "file"));
    await act(() => vi.advanceTimersByTimeAsync(5_000)); // a quiet conversation: nothing else changes
    expect(api).toHaveBeenCalledTimes(1);

    expect(hello.size).toBe(1);
    act(() => hello.forEach((l) => l({ type: "hello" })));
    await answered();
    expect(api).toHaveBeenCalledTimes(2);
    expect(host.textContent).toBe("src/a.ts");
  });

  it("keeps answers that came on a hello", async () => {
    api.mockImplementation(async (url: string) => statsOf(url, () => "file"));
    askPaths("mac", "/repo", ["src/a.ts"]);
    await answered();
    hello.forEach((l) => l({ type: "hello" }));
    askPaths("mac", "/repo", ["src/a.ts"]);
    await answered();
    expect(api).toHaveBeenCalledTimes(1);
    expect(pathStat("mac", "/repo", "src/a.ts")?.kind).toBe("file");
  });

  it("stays bounded: expired answers go first, then the oldest", async () => {
    api.mockImplementation(async (url: string) => statsOf(url, (p) => (p === "gone.md" ? "missing" : "file")));
    askPaths("mac", "/repo", ["kept.md"]);
    await answered();
    askPaths("mac", "/repo", ["gone.md"]); // newer, but a missing path is remembered for 2 minutes only
    await answered();
    await vi.advanceTimersByTimeAsync(3 * 60e3);

    askPaths("mac", "/repo", Array.from({ length: 4999 }, (_, i) => `f${i}`)); // 5001 in all
    await answered();
    expect(pathStat("mac", "/repo", "gone.md")).toBeUndefined();
    expect(pathStat("mac", "/repo", "kept.md")?.kind).toBe("file");
    expect(pathStat("mac", "/repo", "f4998")?.kind).toBe("file");

    askPaths("mac", "/repo", ["one-more.md"]); // 5001 again, none expired: the oldest goes
    await answered();
    expect(pathStat("mac", "/repo", "kept.md")).toBeUndefined();
    expect(pathStat("mac", "/repo", "f0")?.kind).toBe("file");
    expect(pathStat("mac", "/repo", "one-more.md")?.kind).toBe("file");
  });

  it("drops an answer still in flight when reset", async () => {
    let answer: (v: unknown) => void = () => {};
    api.mockImplementationOnce(() => new Promise((r) => (answer = r)));
    askPaths("mac", "/repo", ["src/a.ts"]);
    await vi.advanceTimersByTimeAsync(100);
    expect(api).toHaveBeenCalledTimes(1);

    resetPathPresence();
    answer({ stats: [{ path: "src/a.ts", kind: "file" }] });
    await vi.advanceTimersByTimeAsync(0);
    expect(pathStat("mac", "/repo", "src/a.ts")).toBeUndefined();

    api.mockImplementation(async (url: string) => statsOf(url, () => "dir"));
    askPaths("mac", "/repo", ["src/a.ts"]); // asked anew, not taken for one in flight
    await answered();
    expect(api).toHaveBeenCalledTimes(2);
    expect(pathStat("mac", "/repo", "src/a.ts")?.kind).toBe("dir");
  });
});
