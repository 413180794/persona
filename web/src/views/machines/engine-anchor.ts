// The machine page header's engine pill (#2842) opens Engine below: EngineSection renders <section id="engine">.
import { prefersReducedMotion } from "../../ui/stream/motion";

export const ENGINE_SECTION_ID = "engine";

/** Scroll to Engine (no animation when the system asks for less motion) and move focus to the chosen engine there, else
 * to the section, so the keyboard goes on from it rather than from the header. */
export function openEngineSection() {
  const section = document.getElementById(ENGINE_SECTION_ID);
  if (!section) return;
  section.scrollIntoView({ behavior: prefersReducedMotion() ? "auto" : "smooth", block: "start" });
  (section.querySelector<HTMLElement>('[role="radio"][aria-checked="true"]:not(:disabled)') ?? section).focus({ preventScroll: true });
}
