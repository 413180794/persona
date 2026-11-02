import { afterEach, describe, expect, it, vi } from "vitest";
import { addLinkMachine, addServiceMachine, adminApi, AdminError, errorText, handleLoginRequired, linkPair, repairConnector, setCenter, setCsrf, setTransport } from "./api";

type Call = { url: string; init: RequestInit };

function stub(status: number, body: unknown, headers: Record<string, string> = {}) {
  const calls: Call[] = [];
  setTransport(async (url, init) => {
    calls.push({ url, init });
    return new Response(typeof body === "string" ? body : JSON.stringify(body), { status, headers });
  });
  return calls;
}

afterEach(() => {
  setCsrf("");
  handleLoginRequired(() => {});
});

describe("adminApi", () => {
  it("reads without a body or CSRF header", async () => {
    setCsrf("tok");
    const calls = stub(200, { ok: true });
    expect(await adminApi("/overview")).toEqual({ ok: true });
    expect(calls[0].url).toBe("/api/v1/admin/overview");
    expect(calls[0].init.method).toBe("GET");
    expect(calls[0].init.body).toBeUndefined();
    expect(calls[0].init.credentials).toBe("same-origin");
    expect((calls[0].init.headers as Record<string, string>)["X-Jarvis-CSRF"]).toBeUndefined();
  });

  it("sends JSON and the CSRF token on writes, {} when there is nothing to send", async () => {
    setCsrf("tok");
    const calls = stub(200, {});
    await adminApi("/logout", "POST");
    await adminApi("/hub", "PUT", { name: "x" });
    const h0 = calls[0].init.headers as Record<string, string>;
    expect(h0["Content-Type"]).toBe("application/json");
    expect(h0["X-Jarvis-CSRF"]).toBe("tok");
    expect(calls[0].init.body).toBe("{}");
    expect(calls[1].init.body).toBe('{"name":"x"}');
  });

  it("drops the CSRF token and calls back on login_required", async () => {
    setCsrf("tok");
    const gone = vi.fn();
    handleLoginRequired(gone);
    stub(401, { error: "login required", code: "login_required" });
    await expect(adminApi("/overview")).rejects.toBeInstanceOf(AdminError);
    expect(gone).toHaveBeenCalledOnce();
    const calls = stub(200, {});
    await adminApi("/logout", "POST");
    expect((calls[0].init.headers as Record<string, string>)["X-Jarvis-CSRF"]).toBeUndefined();
  });

  it("does not treat a wrong password as a lost session", async () => {
    const gone = vi.fn();
    handleLoginRequired(gone);
    stub(401, { error: "bad password", code: "bad_password" });
    const err = (await adminApi("/login", "POST", { password: "x" }).catch((e) => e)) as AdminError;
    expect(err.code).toBe("bad_password");
    expect(errorText(err)).toBe("密码不对");
    expect(gone).not.toHaveBeenCalled();
  });

  it("reports how long to wait when rate limited", async () => {
    stub(429, { error: "too many", code: "rate_limited", retry_after: 30 });
    expect(errorText(await adminApi("/login", "POST").catch((e) => e))).toBe("试错太多次了，30 秒后再试");
    stub(429, { error: "too many", code: "rate_limited" }, { "Retry-After": "12" });
    expect(errorText(await adminApi("/login", "POST").catch((e) => e))).toBe("试错太多次了，12 秒后再试");
  });

  it("explains the setup code errors", async () => {
    stub(401, { error: "bad setup code", code: "bad_setup_code" });
    expect(errorText(await adminApi("/setup", "POST").catch((e) => e))).toContain("jarvisd hub admin setup-code");
    stub(403, { error: "setup code required", code: "setup_code_required" });
    expect(errorText(await adminApi("/setup", "POST").catch((e) => e))).toBe("先输入设置码");
  });

  it("keeps a non-JSON error readable", async () => {
    stub(502, "Bad Gateway");
    const err = (await adminApi("/overview").catch((e) => e)) as AdminError;
    expect(err.status).toBe(502);
    expect(errorText(err)).toBe("Bad Gateway");
  });
});

describe("the center and connectors", () => {
  const body = (c: Call) => JSON.parse(String(c.init.body));

  it("sets a center, with confirm only when asked to", async () => {
    setCsrf("tok");
    const calls = stub(200, { role: "center" });
    await setCenter("hub/1");
    await setCenter("hub/1", true);
    expect(calls[0].url).toBe("/api/v1/admin/peers/hub%2F1/center");
    expect(calls[0].init.method).toBe("POST");
    expect(body(calls[0])).toEqual({ confirm: false });
    expect(body(calls[1])).toEqual({ confirm: true });
  });

  it("adds a computer either way", async () => {
    const calls = stub(200, { machine: {}, restart: { required: false, reasons: [] } });
    await addLinkMachine({ id: "air", name: "Air", icon: "laptop" });
    await addServiceMachine({ id: "example-machine", name: "开发机", icon: "server", pair_link: "jarvis://connector?fp=f&code=c", proxy: "socks5://192.0.2.5:1080" });
    expect(body(calls[0])).toEqual({ kind: "link", id: "air", name: "Air", icon: "laptop" });
    expect(body(calls[1])).toEqual({ kind: "service", id: "example-machine", name: "开发机", icon: "server", pair_link: "jarvis://connector?fp=f&code=c", proxy: "socks5://192.0.2.5:1080" });
    await linkPair("air");
    expect(calls[2].url).toBe("/api/v1/admin/machines/air/link-pair");
    await repairConnector("example-machine", "jarvis://connector?fp=f&code=c", "");
    expect(calls[3].url).toBe("/api/v1/admin/machines/example-machine/connector");
    expect(body(calls[3])).toEqual({ pair_link: "jarvis://connector?fp=f&code=c" });
  });

  it("shows the hub's own words for a failed pairing and a member's refusal", async () => {
    stub(502, { error: "连不上那台电脑的连接器：192.0.2.30:18663：connection refused", code: "pair_failed" });
    expect(errorText(await addServiceMachine({ id: "d", name: "d", icon: "server", pair_link: "x" }).catch((e) => e))).toBe("连不上那台电脑的连接器：192.0.2.30:18663：connection refused");
    stub(409, { error: "这台 hub 是组里的成员，设备由中心 hub「书房」管理：到它的管理界面去", code: "member_hub" });
    expect(errorText(await adminApi("/devices").catch((e) => e))).toContain("中心 hub「书房」");
    stub(400, { error: "no ssh", code: "ssh_unsupported" });
    expect(errorText(await addLinkMachine({ id: "a", name: "a", icon: "laptop" }).catch((e) => e))).toContain("电脑来连");
  });
});
