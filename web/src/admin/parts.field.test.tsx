// @vitest-environment jsdom
// The admin Field: only the label text names the input; the hint or the error is its description, and an error marks it
// invalid, so a screen reader reads "Address, edit text, invalid entry, <error>" instead of one long run-on name.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { Field } from "./parts";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const render = (node: React.ReactNode) => act(() => root.render(node));
const control = () => host.querySelector<HTMLInputElement | HTMLTextAreaElement>("input, textarea")!;
// what aria-describedby points at, joined the way a screen reader reads it
const description = (el: Element) =>
  (el.getAttribute("aria-describedby") ?? "")
    .split(/\s+/)
    .filter(Boolean)
    .map((id) => document.getElementById(id)?.textContent ?? "")
    .join(" ");

describe("Field", () => {
  it("names the input by its label only and describes it with the hint", () => {
    render(<Field label="Address" hint="Where the hub listens" value="" onChange={() => {}} />);
    const input = control();
    expect(input.labels).toHaveLength(1);
    const label = input.labels![0];
    expect(label.textContent).toBe("Address");
    expect(label.contains(input)).toBe(false);
    expect(label.textContent).not.toContain("Where the hub listens");
    expect(description(input)).toBe("Where the hub listens");
    expect(input.getAttribute("aria-invalid")).toBeNull();
  });

  it("marks the input invalid and describes it with the error instead of the hint", () => {
    render(<Field label="Address" hint="Where the hub listens" value="nope" onChange={() => {}} error="Not a CIDR" />);
    const input = control();
    expect(input.labels![0].textContent).toBe("Address");
    expect(input.getAttribute("aria-invalid")).toBe("true");
    expect(description(input)).toBe("Not a CIDR");
    expect(host.textContent).not.toContain("Where the hub listens");
  });

  it("drops the description and the invalid mark once the error is fixed and there is no hint", () => {
    render(<Field label="Password" type="password" value="x" onChange={() => {}} error="Too short" />);
    expect(control().getAttribute("aria-invalid")).toBe("true");
    render(<Field label="Password" type="password" value="long enough" onChange={() => {}} />);
    const input = control();
    expect(input.getAttribute("aria-invalid")).toBeNull();
    expect(input.hasAttribute("aria-describedby")).toBe(false);
    expect(input.labels![0].textContent).toBe("Password");
  });

  it("does the same for a multiline field", () => {
    render(<Field label="Advertise" hint="One per line" value="" onChange={() => {}} multiline error="Bad address" />);
    const area = control();
    expect(area.tagName).toBe("TEXTAREA");
    expect(area.labels![0].textContent).toBe("Advertise");
    expect(area.getAttribute("aria-invalid")).toBe("true");
    expect(description(area)).toBe("Bad address");
  });

  it("gives two fields on one page their own label and description", () => {
    render(
      <>
        <Field label="New password" value="" onChange={() => {}} hint="At least 8 characters" />
        <Field label="Again" value="" onChange={() => {}} error="Does not match" />
      </>,
    );
    const [first, second] = host.querySelectorAll("input");
    expect(first.id).not.toBe(second.id);
    expect(first.labels![0].textContent).toBe("New password");
    expect(second.labels![0].textContent).toBe("Again");
    expect(description(first)).toBe("At least 8 characters");
    expect(description(second)).toBe("Does not match");
  });
});
