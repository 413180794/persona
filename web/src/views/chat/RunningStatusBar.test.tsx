import { afterEach, describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { tokenCount, RunningStatusView, statusText } from "./RunningStatusBar";

afterEach(() => vi.useRealTimers());

const render = (props: Parameters<typeof RunningStatusView>[0]) => renderToStaticMarkup(<RunningStatusView {...props} />);

describe("tokenCount", () => {
  it("shows thousands with one decimal", () => {
    expect(tokenCount(950)).toBe("950");
    expect(tokenCount(1200)).toBe("1.2k");
    expect(tokenCount(22_800)).toBe("22.8k");
    expect(tokenCount(412.6)).toBe("413");
  });
});

describe("statusText", () => {
  it("says the verb, 思考中 while thinking, 运行中 without a line", () => {
    expect(statusText({ verb: "Frosting" })).toBe("Frosting…");
    expect(statusText({ verb: "Frosting", thinking: true })).toBe("思考中");
    expect(statusText(null)).toBe("运行中");
    expect(statusText({})).toBe("运行中");
  });
});

describe("RunningStatusView", () => {
  it("shows the verb, the line's time and its tokens", () => {
    const html = render({ live: { verb: "Frosting", elapsed_s: 975, tokens_down: 22_800 }, working: true });
    expect(html).toContain("Frosting…");
    expect(html).toContain("16 分 15 秒");
    expect(html).toContain("↓ 22.8k tokens");
    expect(html).toContain("text-status-accent");
    expect(html).toContain('data-running-status="on"');
  });

  it("says 思考中 while thinking", () => {
    expect(render({ live: { verb: "Frosting", thinking: true, elapsed_s: 3 }, working: true })).toContain("思考中");
  });

  it("without a line: 运行中, counting from when the turn started, no tokens", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-05T10:01:05Z"));
    const html = render({ live: null, working: true, startedAt: "2026-11-05T10:00:00Z" });
    expect(html).toContain("运行中");
    expect(html).toContain("1 分 5 秒");
    expect(html).not.toContain("tokens");
  });

  it("is not there while nothing works", () => {
    expect(render({ live: null, working: false })).toBe("");
  });
});
