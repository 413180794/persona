import { describe, expect, it } from "vitest";
import { installState } from "./pwa";

const ua = {
  iphone: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1",
  ipad: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15",
  safari: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15",
  chrome: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
  firefox: "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:132.0) Gecko/20100101 Firefox/132.0",
};
const env = (over: Partial<Parameters<typeof installState>[0]>) => installState({ standalone: false, installed: false, prompt: false, secure: true, ua: ua.chrome, touchPoints: 0, ...over });

describe("installState", () => {
  it("is installed when running standalone or just installed", () => {
    expect(env({ standalone: true, prompt: true })).toBe("installed");
    expect(env({ installed: true })).toBe("installed");
  });

  it("offers the browser's prompt when there is one", () => {
    expect(env({ prompt: true })).toBe("prompt");
  });

  it("points iPhone and iPad (desktop-site UA) to Safari's share menu, even over http", () => {
    expect(env({ ua: ua.iphone, secure: false })).toBe("ios");
    expect(env({ ua: ua.ipad, touchPoints: 5 })).toBe("ios");
  });

  it("points Safari on a Mac to 添加到程序坞", () => {
    expect(env({ ua: ua.safari })).toBe("safari");
  });

  it("says why other browsers can't install over plain http, else points to their menu", () => {
    expect(env({ secure: false })).toBe("insecure");
    expect(env({})).toBe("menu");
    expect(env({ ua: ua.firefox })).toBe("menu");
  });
});
