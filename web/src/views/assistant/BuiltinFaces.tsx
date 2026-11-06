// Visor (persona) and Circuit (persona-circuit), the built-in looks besides Mochi (docs/persona.md "Built-in avatars"): the same drawings as
// the apps' PersonaFace (Android PersonaFace.kt visor / circuit, iOS Shared/Design/PersonaFace.swift), in shares of a
// 100-unit side, painted in the page's accent (gray when offline). Each state reads from its first frame alone; the
// blink and the needs-you ring are the mochi-* loops (face-motion.ts) and stop with reduced motion.
import { useId } from "react";
import type { PersonaState } from "../../lib/persona";
import { cx } from "../../ui/atoms";
import { useFaceMotion } from "./face-motion";

const OFFLINE = "#9e9e9e";
const mix = (c: string, other: string, share: number) => `color-mix(in srgb, ${c} ${Math.round((1 - share) * 100)}%, ${other})`;

function NeedsYouRing() {
  return <circle cx="50" cy="50" r="47" fill="none" strokeWidth="5" className="mochi-pulse" style={{ stroke: "var(--mochi-needs-you)" }} />;
}

function Thought({ x, y, color }: { x: number; y: number; color: string }) {
  return (
    <g>
      <ellipse cx={x} cy={y} rx="14" ry="9" fill="white" fillOpacity="0.94" />
      {[-6, 0, 6].map((dx) => (
        <circle key={dx} cx={x + dx} cy={y} r="2" style={{ fill: color }} />
      ))}
    </g>
  );
}

/** Visor: an accent disc with a dark visor, two glowing eyes and a sparkle at the top right. */
export function VisorFace({ size, state = "idle", className }: { size: number; state?: PersonaState; className?: string }) {
  const id = useId().replace(/:/g, "");
  const offline = state === "offline";
  const accent = offline ? OFFLINE : "var(--accent)";
  const look = state === "thinking" ? [4, -3.5] : [0, 0];
  const eh = offline ? 3 : 13;
  const vw = 66, vh = 30, vx = (100 - vw) / 2, vy = (100 - vh) / 2 + 2;
  const eyeY = vy + vh / 2 + look[1], eyeX = 50 + look[0];
  const eye = offline ? "#808080" : mix(accent, "white", 0.55);
  const motion = useFaceMotion<SVGSVGElement>(state);
  return (
    <svg ref={motion} viewBox="0 0 100 100" width={size} height={size} aria-hidden className={cx("shrink-0 rounded-full", className)} style={{ width: size, height: size }}>
      <defs>
        <linearGradient id={`${id}d`} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" style={{ stopColor: mix(accent, "white", 0.28) }} />
          <stop offset="0.5" style={{ stopColor: accent }} />
          <stop offset="1" style={{ stopColor: mix(accent, "black", 0.18) }} />
        </linearGradient>
        <linearGradient id={`${id}v`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#292929" />
          <stop offset="1" stopColor="#0d0d0d" />
        </linearGradient>
        <filter id={`${id}g`} x="-50%" y="-50%" width="200%" height="200%">
          <feGaussianBlur stdDeviation={offline ? 0 : 2.5} />
        </filter>
      </defs>
      <circle cx="50" cy="50" r="50" fill={`url(#${id}d)`} />
      <rect x={vx} y={vy} width={vw} height={vh} rx={vh / 2} fill={`url(#${id}v)`} />
      <rect x={vx + 0.6} y={vy + 0.6} width={vw - 1.2} height={vh - 1.2} rx={(vh - 1.2) / 2} fill="none" stroke="white" strokeOpacity="0.14" strokeWidth="1.2" />
      <g className={state === "idle" ? "mochi-blink" : undefined}>
        {[eyeX - 7.5 - 8.5, eyeX + 7.5].map((x) => (
          <g key={x}>
            {!offline && <rect x={x} y={eyeY - eh / 2} width="8.5" height={eh} rx="4.25" style={{ fill: accent }} filter={`url(#${id}g)`} />}
            <rect x={x} y={eyeY - eh / 2} width="8.5" height={eh} rx="4.25" style={{ fill: eye }} />
          </g>
        ))}
      </g>
      <circle cx="50" cy="50" r="49.25" fill="none" stroke="white" strokeOpacity="0.18" strokeWidth="1.5" />
      {state === "thinking" ? (
        <Thought x={75} y={20} color={accent} />
      ) : (
        <path d="M75 12 L77 21 L86 23 L77 25 L75 34 L73 25 L64 23 L73 21 Z" fill="white" fillOpacity={offline ? 0.5 : 0.92} />
      )}
      {state === "needs_you" && <NeedsYouRing />}
    </svg>
  );
}

const ROWS = [31, 50, 69];
const PAD_X = 22;
const CORE = [64, 50] as const;
const TRACES = ROWS.map((row) => {
  const turn = CORE[0] - Math.abs(CORE[1] - row);
  return turn < CORE[0] ? [[PAD_X, row], [turn, row], CORE] : [[PAD_X, row], CORE];
});

/** Circuit: three traces running into a core with two dark eyes, on a dark disc of the accent. */
export function CircuitFace({ size, state = "idle", className }: { size: number; state?: PersonaState; className?: string }) {
  const id = useId().replace(/:/g, "");
  const offline = state === "offline";
  const accent = offline ? OFFLINE : "var(--accent)";
  const mark = mix(accent, "white", 0.15);
  const ground = mix(accent, "black", 0.82);
  const look = state === "working" ? [-2.5, 0] : state === "thinking" ? [2, -3.5] : [0, 0];
  const ew = 4.4, eh = offline ? 2.5 : 12.5, gap = 6.4;
  const c = [CORE[0] + look[0], CORE[1] + look[1]];
  const motion = useFaceMotion<SVGSVGElement>(state);
  return (
    <svg ref={motion} viewBox="0 0 100 100" width={size} height={size} aria-hidden className={cx("shrink-0 rounded-full", className)} style={{ width: size, height: size }}>
      <defs>
        <linearGradient id={`${id}d`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" style={{ stopColor: mix(accent, "black", 0.76) }} />
          <stop offset="1" style={{ stopColor: mix(accent, "black", 0.88) }} />
        </linearGradient>
      </defs>
      <circle cx="50" cy="50" r="50" fill={`url(#${id}d)`} />
      <path d={TRACES.map((pts) => "M" + pts.map((p) => p.join(" ")).join(" L")).join(" ")} fill="none" strokeWidth="5.6" strokeLinecap="round" strokeLinejoin="round" style={{ stroke: mark }} />
      {ROWS.map((row) => (
        <circle key={row} cx={PAD_X} cy={row} r="6.5" style={{ fill: mark }} />
      ))}
      <circle cx={CORE[0]} cy={CORE[1]} r="19" style={{ fill: mark }} />
      <g className={state === "idle" ? "mochi-blink" : undefined}>
        {[c[0] - gap / 2 - ew, c[0] + gap / 2].map((x) => (
          <rect key={x} x={x} y={c[1] - eh / 2} width={ew} height={eh} rx={ew / 2} style={{ fill: ground }} />
        ))}
      </g>
      {state === "working" && <circle cx={PAD_X + 14} cy={ROWS[1]} r="3.4" className="mochi-pulse" style={{ fill: mix(accent, "white", 0.75) }} />}
      <circle cx="50" cy="50" r="49" fill="none" strokeWidth="2" style={{ stroke: accent }} strokeOpacity="0.35" />
      {state === "thinking" && <Thought x={76} y={19} color={accent} />}
      {state === "needs_you" && <NeedsYouRing />}
    </svg>
  );
}
