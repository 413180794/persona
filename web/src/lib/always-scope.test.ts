import { describe, expect, it } from "vitest";
import { alwaysScope } from "./always-scope";
import { t } from "../i18n";

describe("always permission scope", () => {
  it("says Bash allows every command, including old cards", () => {
    expect(alwaysScope({ tool_name: "Bash", always_keys: ["tool:Bash"] })).toBe(t("chat-interactions.always_scope_commands"));
    expect(alwaysScope({ tool_name: "Bash" })).toBe(t("chat-interactions.always_scope_commands"));
  });
  it("promises a directory only for the directory rules the node supplied", () => {
    expect(alwaysScope({ tool_name: "Write", always_keys: ["tool:Edit(//home/me/notes/**)"] })).toBe(t("chat-interactions.always_scope_directory"));
    expect(alwaysScope({ tool_name: "Read", always_keys: ["tool:Read"] })).toBe(t("chat-interactions.always_scope_tool", { tool: "Read" }));
    expect(alwaysScope({ tool_name: "Read" })).toBe(t("chat-interactions.always_scope_tool", { tool: "Read" }));
  });
  it("preserves runtime scope and does not promise unsupported always grants", () => {
    expect(alwaysScope({ agent: "codex", tool_name: "Bash", always_label: "npm test" })).toBe(t("chat-interactions.always_scope_named", { scope: "npm test" }));
    expect(alwaysScope({ agent: "codex", tool_name: "Bash" })).toBe("");
    expect(alwaysScope({ tool_name: "Bash", always_keys: ["lark:POST /messages"] })).toBe(t("chat-interactions.always_scope_saved"));
  });
});
