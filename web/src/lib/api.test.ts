import { afterEach, describe, expect, it, vi } from "vitest";
import { initI18n, t } from "../i18n";
import { api, ApiError, errorText } from "./api";
import { isOfflineError } from "./feedback";
import { sessionLoadError } from "./session-link";

afterEach(() => vi.unstubAllGlobals());

describe("readable hub errors", () => {
  it.each([
    new ApiError(404, "not found", {}),
    new ApiError(404, "entry missing", {}),
    new ApiError(400, "entry missing", { code: "not_found" }),
    new ApiError(400, "not_found", {}),
    new ApiError(400, "Not Found\n", {}),
  ])("explains missing content or an unsupported endpoint: %s", (error) => {
    expect(errorText(error)).toBe(t("lib-api.not_found", { detail: `HTTP ${error.status}: ${error.message}` }));
    expect(errorText(error)).toContain(error.message);
  });

  it.each([500, 501, 505, 599])("explains HTTP %s without discarding the server's reason", (status) => {
    expect(errorText(new ApiError(status, "storage failed", {}))).toBe(t("lib-api.server_error", { detail: `HTTP ${status}: storage failed` }));
  });

  it.each([502, 503, 504])("explains HTTP %s and still lets offline feedback fold it", (status) => {
    const text = errorText(new ApiError(status, "upstream unavailable", {}));
    expect(text).toBe(t("lib-api.hub_unavailable", { detail: `HTTP ${status}: upstream unavailable` }));
    expect(isOfflineError(text)).toBe(true);
  });

  it.each(["", "HTTP 500"])("keeps a bare status only once: %s", (message) => {
    expect(errorText(new ApiError(500, message, {}, false))).toBe(t("lib-api.server_error", { detail: "HTTP 500" }));
  });

  it("keeps authentication and other client errors intact", () => {
    for (const [status, message] of [[401, "invalid token"], [403, "forbidden"], [409, "already exists"], [429, "slow down"], [400, "file not found in upload"]] as const) {
      expect(errorText(new ApiError(status, message, {}))).toBe(message);
    }
  });

  it("preserves non-API errors and the existing network explanation", () => {
    expect(errorText(new TypeError("Failed to fetch"))).toBe(t("lib-api.cant_reach_hub"));
    expect(errorText(new Error("invalid token"))).toBe("invalid token");
    expect(errorText("not found")).toBe("not found");
    expect(errorText(null)).toBe("null");
  });

  it("leaves the original error available to callers and session-specific explanations", () => {
    const message = "transcript for session deadbeef not found";
    const error = new ApiError(404, message, { code: "not_found" }, true, "request-1");
    expect(sessionLoadError(errorText(error))).toBe("找不到这个会话：可能已经删除，或链接里的 id 不完整");
    expect(error.message).toBe(message);
    expect(error.code).toBe("not_found");
    expect(error.requestId).toBe("request-1");
  });

  it.each([
    [404, '{"error":"not found"}', "HTTP 404: not found"],
    [500, "", "HTTP 500"],
    [502, "Bad Gateway", "HTTP 502: Bad Gateway"],
  ] as const)("formats a failed HTTP %s response through the REST client", async (status, body, detail) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body, { status })));
    const error = await api("/missing", { token: "test-token" }).catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect(errorText(error)).toContain(detail);
    expect(errorText(error)).not.toBe(detail);
  });
});

describe("error language", { timeout: 30_000 }, () => {
  afterEach(() => initI18n("zh-Hans"), 30_000);

  it.each([
    ["zh-Hans", "找不到", "服务暂时出错", "hub 暂时无法响应"],
    ["en", "Content not found", "The service ran into a problem", "The hub can't respond right now"],
    ["ja", "内容が見つからない", "サービスで問題が発生しました", "ハブが応答できません"],
  ] as const)("uses the current %s catalog and retains diagnostics", async (lang, missing, server, gateway) => {
    await initI18n(lang);
    for (const [status, explanation] of [[404, missing], [500, server], [503, gateway]] as const) {
      const text = errorText(new ApiError(status, "original error", {}));
      expect(text).toContain(explanation);
      expect(text).toContain(`HTTP ${status}: original error`);
    }
  });
});
