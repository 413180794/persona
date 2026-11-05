// The chips under the composer on a phone (#3575): at 390 px the main conversation's row (+, @, 手动确认, 思考 高,
// claude · opus) is wider than the box, and the model chip ran under the send button with its chevron past the box's
// edge. The chips give way instead: a chip may shrink (its label truncates, the chevron stays), and a Persona
// conversation's mode chips leave the launcher chip 5rem of the row, so it truncates first and they only past that. A unit test has no
// layout, so this holds the classes that make the row fit; the 390 px measurement is in the PR.
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { Session } from "../../lib/types";
import { Chip, ModeChips } from "./SessionDialogs";

// the class lists of the markup's elements, in document order
const tags = (node: React.ReactElement) =>
  [...renderToStaticMarkup(node).matchAll(/<(\w+)[^>]*?class="([^"]*)"[^>]*>([^<]*)/g)].map(([, tag, cls, text]) => ({ tag, cls: cls.split(/\s+/), text }));

const session = { machine_id: "m", session_id: "s", permission_mode: "default", effort: "high" } as unknown as Session;

describe("composer chips on a narrow row", () => {
  it("a chip can shrink below its label, truncating the label and keeping the chevron", () => {
    const [button, label, chevron] = tags(<Chip label="claude · opus" title="launcher" onClick={() => {}} />);
    expect(button.tag).toBe("button");
    expect(button.cls).toEqual(expect.arrayContaining(["min-w-0", "max-w-full"]));
    expect(label.text).toBe("claude · opus");
    expect(label.cls).toEqual(expect.arrayContaining(["min-w-0", "truncate"]));
    expect(chevron.tag).toBe("svg");
    expect(chevron.cls).toContain("shrink-0");
  });

  it("a Persona conversation's mode chips leave the launcher chip room; an agent's keep their width", () => {
    const wrapper = (assistant: boolean) => tags(<ModeChips session={session} assistant={assistant} />)[0].cls;
    expect(wrapper(true)).toEqual(expect.arrayContaining(["min-w-0", "max-w-[calc(100%-5.5rem)]"]));
    expect(wrapper(false)).toContain("shrink-0");
    expect(wrapper(false)).not.toContain("min-w-0");
  });
});
