import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { ConnectorPrompt } from "../../lib/connectors";
import { SignInStep } from "./HubConnector";

// 设置 › 连接器's login card (#1693): a QR code step is drawn and a step the page can't draw says so, never an empty
// form whose 下一步 stays grey forever.
const draw = (prompt: ConnectorPrompt, isChannel = false) =>
  renderToStaticMarkup(
    <SignInStep
      name="微信"
      prompt={prompt}
      values={{}}
      busy={false}
      isChannel={isChannel}
      onChange={() => {}}
      onSubmit={() => {}}
      onRestart={() => {}}
      onCancel={() => {}}
    />,
  );

describe("a sign-in step", () => {
  it("draws the hub's QR code with 保存二维码, and no 下一步", () => {
    const html = draw({ step: "qr", kind: "qr", url: "https://example.com/q", interval: 2, hint: "用微信扫一扫", qr: { size: 3, rows: ["101", "010", "101"] } });
    expect(html).toContain('role="img"');
    expect(html).toContain('aria-label="登录用的二维码"');
    expect(html).toContain("M2 2h1v1h-1z"); // the top-left module, past the quiet zone
    expect(html).toContain("保存二维码");
    expect(html).toContain("等你在微信里扫码确认");
    expect(html).toContain("用微信扫一扫");
    expect(html).not.toContain("下一步");
  });

  it("says a QR step without its modules or a link can't be drawn, and what to do instead", () => {
    const html = draw({ step: "qr", kind: "qr", hint: "用微信扫一扫" }, true);
    expect(html).not.toContain('role="img"'); // no QR code
    expect(html).toContain("这一步当前页面画不了");
    expect(html).toContain("设置 › 扩展 › 渠道 › 微信");
    expect(html).toContain("重新开始");
    expect(html).not.toContain("下一步");
  });

  it("offers the link a QR step encodes when only its modules are missing", () => {
    const html = draw({ step: "qr", kind: "qr", url: "https://example.com/q" });
    expect(html).toContain("https://example.com/q");
    expect(html).toContain("复制链接");
    expect(html).not.toContain("下一步");
  });

  it("says a kind it doesn't know can't be drawn, without pointing at 渠道 for a connector that isn't one", () => {
    const html = draw({ step: "auth", kind: "open_url", url: "https://example.com/auth" });
    expect(html).toContain("这一步当前页面画不了");
    expect(html).not.toContain("渠道");
    expect(html).not.toContain("下一步");
  });

  it("still draws a form with its fields and 下一步", () => {
    const html = draw({ step: "credentials", fields: [{ name: "phone", label: "手机号" }] });
    expect(html).toContain("手机号");
    expect(html).toContain("下一步");
    expect(html).not.toContain("画不了");
  });
});
