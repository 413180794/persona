// @vitest-environment jsdom
// The built-in faces loop as Web Animations (face-motion.ts), not CSS animations, whose every cycle sent React's root
// listener an animationiteration event and cost the page a recalc and a layout: the loops start with their state, keep
// their phase across renders, restart with a new state, and none runs under reduced motion.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PersonaState } from "../../lib/persona";
import { FACE_LOOPS } from "./face-motion";
import { MochiFace } from "./MochiFace";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const media = { reduce: false, listeners: new Set<() => void>() };
type Started = { el: Element; keyframes: Keyframe[]; options: KeyframeAnimationOptions; cancel: ReturnType<typeof vi.fn> };
let started: Started[] = [];
let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  media.reduce = false;
  media.listeners.clear();
  started = [];
  (globalThis as Record<string, unknown>).matchMedia = () => ({
    get matches() {
      return media.reduce;
    },
    addEventListener: (_: string, f: () => void) => media.listeners.add(f),
    removeEventListener: (_: string, f: () => void) => media.listeners.delete(f),
  });
  Element.prototype.animate = function (this: Element, keyframes: Keyframe[], options: KeyframeAnimationOptions) {
    const a = { el: this, keyframes, options, cancel: vi.fn() };
    started.push(a);
    return a as unknown as Animation;
  } as Element["animate"];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  delete (Element.prototype as Partial<Element>).animate;
});

const mochi = (state: PersonaState, size = 40) => act(() => root.render(<MochiFace size={size} state={state} />));
const running = () => started.filter((a) => !a.cancel.mock.calls.length);
const loopOf = (a: Started) => [...a.el.classList].find((c) => c in FACE_LOOPS);

describe("the faces' loops", () => {
  it("start with the state, as the CSS ones did: timing, delays, looping", () => {
    mochi("working");
    expect(running().map(loopOf)).toEqual(["mochi-spin", "mochi-pulse", "mochi-pulse", "mochi-pulse"]);
    expect(running().map((a) => a.options.delay)).toEqual([0, 0, 250, 500]);
    for (const a of running()) expect(a.options.iterations).toBe(Infinity);
    expect(running()[0].options.duration).toBe(1600);
    expect(running()[1].options.duration).toBe(1200);
  });

  it("keep their phase across renders and restart with a new state", () => {
    mochi("needs_you");
    expect(running().map(loopOf).sort()).toEqual(["mochi-pulse", "mochi-rock"]);
    const first = running();
    mochi("needs_you");
    expect(running()).toEqual(first);
    mochi("idle");
    expect(first.every((a) => a.cancel.mock.calls.length === 1)).toBe(true);
    expect(running().map(loopOf)).toEqual(["mochi-blink"]);
  });

  it("the compact face has no motion lines, so working pulses none of them", () => {
    mochi("working", 24);
    expect(running().map(loopOf)).toEqual(["mochi-spin"]);
  });

  it("none runs under reduced motion, and they stop when it is asked for", () => {
    media.reduce = true;
    mochi("working");
    expect(started).toEqual([]);
    act(() => root.unmount());
    root = createRoot(host);
    media.reduce = false;
    mochi("working");
    expect(running().length).toBe(4);
    media.reduce = true;
    act(() => media.listeners.forEach((f) => f()));
    expect(running()).toEqual([]);
  });

  it("the stylesheet runs no CSS animation on them any more", () => {
    const css = readFileSync(join(__dirname, "..", "..", "styles.css"), "utf8");
    expect(css).not.toMatch(/@keyframes mochi-/);
    for (const name of Object.keys(FACE_LOOPS)) {
      for (const m of css.matchAll(new RegExp(`\\.${name}\\b[^{]*\\{([^}]*)\\}`, "g"))) expect(m[1]).not.toMatch(/animation/);
    }
  });
});
