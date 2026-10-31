import { describe, expect, it } from "vitest";
import { takeToken } from "./auth";

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
