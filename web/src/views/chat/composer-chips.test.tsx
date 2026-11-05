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

  // #4142: at 360 px the launcher chip at 5rem still left 手动确认 / 思考 高 a letter each; on a phone it is an icon
  it("at phone width a chip with a phone icon shows the icon, keeps its label for screen readers, and frees the row", () => {
    const [, icon, label, chevron] = tags(<Chip label="claude · opus" phoneIcon="agent" title="launcher" onClick={() => {}} />);
    expect(icon.tag).toBe("svg");
    expect(icon.cls).toEqual(expect.arrayContaining(["shrink-0", "sm:hidden"]));
    expect(label.text).toBe("claude · opus");
    expect(label.cls).toEqual(expect.arrayContaining(["truncate", "max-sm:sr-only"]));
    expect(chevron.tag).toBe("svg");
    const [, plainLabel] = tags(<Chip label="手动确认" title="mode" onClick={() => {}} />);
    expect(plainLabel.cls).not.toContain("max-sm:sr-only");
    expect(tags(<ModeChips session={session} assistant />)[0].cls).toContain("max-sm:max-w-[calc(100%-3rem)]");
    // the footer's ⓘ button (a read-only or conflicting source, #3533) takes room too
    expect(tags(<ModeChips session={session} assistant withInfo />)[0].cls).toContain("max-sm:max-w-[calc(100%-5rem)]");
  });
});
