import { beforeEach, describe, expect, it, vi } from "vitest";

const calls = vi.hoisted(() => ({ list: [] as { path: string; token?: string }[], good: "" }));
vi.mock("./api", () => ({
  api: vi.fn(async (path: string, init?: { token?: string }) => {
    calls.list.push({ path, token: init?.token });
    if (init?.token !== calls.good) throw new Error("invalid token");
    return path === "/auth/tokens" ? { token: "jd_browser", id: "dev1" } : { machines: [] };
  }),
  setToken: vi.fn(),
}));

import { setToken } from "./api";
import { pair, pairToken, takeToken } from "./auth";

describe("pairToken", () => {
  it("keeps bare tokens as they are", () => {
    expect(pairToken("  master-secret \n")).toBe("master-secret");
    expect(pairToken("jp_cafe00a1")).toBe("jp_cafe00a1");
    expect(pairToken("jd_cafe00a1")).toBe("jd_cafe00a1");
  });
  it("takes the token from the app's pairing link, percent-decoded", () => {
    expect(pairToken("jarvis://pair?token=jp_example%2B%26&url=http%3A%2F%2Fhub.example%3A18660")).toBe("jp_example+&");
    expect(pairToken("persona://pair?url=http%3A%2F%2Fhub.example%3A18660&token=a%26b%3D%2B+%3F")).toBe("a&b=+ ?");
  });
  it("takes it from ?token= or #token= in an ordinary link", () => {
    expect(pairToken("http://hub.example:18660/?token=t0k&tab=x")).toBe("t0k");
    expect(pairToken("https://hub.example/j#a=1&token=t0k")).toBe("t0k");
  });
  it("passes anything else through, so the hub turns it down", () => {
    expect(pairToken("jarvis://pair?url=http%3A%2F%2Fhub.example")).toBe("jarvis://pair?url=http%3A%2F%2Fhub.example");
    expect(pairToken("not a token")).toBe("not a token");
    expect(pairToken("http://[bad")).toBe("http://[bad");
  });
});

describe("pair", () => {
  beforeEach(() => {
    calls.list = [];
    vi.mocked(setToken).mockClear();
    vi.stubGlobal("localStorage", { setItem: vi.fn() });
    vi.stubGlobal("navigator", { userAgent: "" });
  });
  it("checks and trades the token from a pasted pairing link", async () => {
    calls.good = "jp_example";
    await pair("jarvis://pair?url=http%3A%2F%2Fhub.example%3A18660&token=jp_example");
    expect(calls.list.map((c) => [c.path, c.token])).toEqual([
      ["/machines", "jp_example"],
      ["/auth/tokens", "jp_example"],
    ]);
    expect(setToken).toHaveBeenCalledWith("jd_browser");
  });
  it("keeps a device token without trading it", async () => {
    calls.good = "jd_device";
    await pair("jd_device");
    expect(calls.list.map((c) => c.path)).toEqual(["/machines"]);
    expect(setToken).toHaveBeenCalledWith("jd_device");
  });
  it("throws what the hub said for a link without a token", async () => {
    calls.good = "jp_example";
    await expect(pair("jarvis://pair?url=http%3A%2F%2Fhub.example")).rejects.toThrow("invalid token");
    expect(setToken).not.toHaveBeenCalled();
  });
});

describe("takeToken", () => {
  it("takes the token from the query and keeps the rest of the link", () => {
    expect(takeToken("http://hub.example:18660/s/m1/abc?token=t0k&tab=x#top")).toEqual({ token: "t0k", rest: "/s/m1/abc?tab=x#top" });
    expect(takeToken("http://hub.example:18660/?token=t0k")).toEqual({ token: "t0k", rest: "/" });
  });
  it("takes it from the fragment too", () => {
    expect(takeToken("http://hub.example:18660/j#token=t0k")).toEqual({ token: "t0k", rest: "/j" });
    expect(takeToken("http://hub.example:18660/j#a=1&token=t0k")).toEqual({ token: "t0k", rest: "/j#a=1" });
  });
  it("leaves links without one alone, and strips an empty one", () => {
    expect(takeToken("http://hub.example:18660/settings?tab=models")).toBeNull();
    expect(takeToken("http://hub.example:18660/?token=")).toEqual({ token: "", rest: "/" });
  });
});
