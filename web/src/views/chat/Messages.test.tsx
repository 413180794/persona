import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { UserMessage } from "../../lib/turns";
import { PeerMessage, keepLineBreaks } from "./Messages";

describe("PeerMessage", () => {
  it("draws the sender and its words as Markdown", () => {
    const text = "**web branch rebased onto master**\n\nChecks pass.\n\n- **Why:** master changed the mock file";
    const msg = { id: "p1", text, images: [], contexts: [], from: "worker-1" } as unknown as UserMessage;
    const html = renderToStaticMarkup(<PeerMessage msg={msg} />);
    expect(html).toContain("来自 worker-1");
    expect(html).toContain("<strong>web branch rebased onto master</strong>");
    expect(html).toMatch(/<li[^>]*>.*<strong>Why:<\/strong>/s);
    expect(html).not.toContain("**");
  });

  it("keeps a single line break as a break", () => {
    expect(keepLineBreaks("Report is in /tmp/r.md\nAll checks pass.\n\n- a\n- b")).toBe("Report is in /tmp/r.md  \nAll checks pass.\n\n- a  \n- b");
    const msg = { id: "p2", text: "Report is in /tmp/r.md\nAll checks pass.", images: [], contexts: [], from: "docs" } as unknown as UserMessage;
    expect(renderToStaticMarkup(<PeerMessage msg={msg} />)).toMatch(/r\.md<br\/?>/);
  });
});
