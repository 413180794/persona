import type { CSSProperties } from "react";
import { stateColor } from "../lib/format";
import { FOLDED_ORDER, type FoldedCounts } from "../lib/folded-counts";
import { t } from "../i18n";
import { BUSY_MOTION } from "../lib/busy-motion";
import type { AgentState } from "../lib/types";
import { STATE_LABEL, stateCount, stateName } from "../lib/state-labels";

/** How a state is drawn (#3559, after Moshi). The shapes differ, so a state never rests on color alone:
 *  - arc: working, a short arc that turns;
 *  - alert: needs you, a glowing dot with a thin ring around it;
 *  - glow: done and not seen, a glowing dot;
 *  - dot: idle or unknown, a plain dot; seen and awaiting the user's reply (Awaiting Reply, #5096), a plain dot in the
 *    unread tint, between the glowing unread dot and the grey seen one;
 *  - ring: offline, a hollow ring. */
export type MarkShape = "arc" | "alert" | "glow" | "dot" | "ring";

const SHAPE: Record<AgentState, MarkShape> = { working: "arc", needs_you: "alert", unread: "glow", idle: "dot", unknown: "dot", offline: "ring" };

/** The shape and the color token a state is drawn with. */
export function markOf(state: AgentState): { shape: MarkShape; color: string } {
  return { shape: SHAPE[state], color: stateColor(state) };
}

/** The one place an agent's state is drawn: sidebar rows, the session header, the terminal tree, the palette, pane
 *  tabs, subagent cards. Sizes, glow and the arc's speed are tokens (styles.css, "status marks"); `sm` is for the
 *  dense rows. Reduced motion slows the arc (the busy-motion class, lib/busy-motion.ts), it does not stop it. */
export function StatusMark({ state, size = "md", label = true, awaiting = false }: { state: AgentState; size?: "sm" | "md"; label?: boolean; /** Seen and awaiting the user's reply (lib/agent-groups.ts awaitsReply); only drawn on an idle state. */ awaiting?: boolean }) {
  const reply = awaiting && state === "idle";
  const { shape, color } = reply ? { shape: "dot" as const, color: stateColor("unread") } : markOf(state);
  const style = { "--mark-color": color } as CSSProperties;
  const name = reply ? stateName("awaits_reply") : STATE_LABEL[state];
  const a11y = label ? ({ role: "img", "aria-label": name } as const) : ({ "aria-hidden": true } as const);
  if (shape === "arc") {
    // Moshi's loader: a circle drawn as 28 of 100 units, round caps, turning once in 0.9 s.
    return (
      <svg {...a11y} className={`status-mark ${BUSY_MOTION}`} data-shape={shape} data-state={state} data-size={size} style={style} viewBox="0 0 18 18">
        <circle cx="9" cy="9" r="7.5" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" pathLength={100} strokeDasharray="28 72" />
      </svg>
    );
  }
  return <span {...a11y} className="status-mark" data-shape={shape} data-state={reply ? "awaits_reply" : state} data-size={size} style={style} />;
}

/** What a folded group still shows of its agents: a mark and a monospace count for each of needs you, working and done,
 *  needs you first. Spoken as one phrase; the marks and numbers themselves are hidden from a screen reader. */
export function StateTally({ counts, className }: { counts: FoldedCounts; className?: string }) {
  const shown = FOLDED_ORDER.filter((k) => counts[k.key] > 0);
  if (!shown.length) return null;
  const label = t("sidebar.folded_states", { list: shown.map((k) => stateCount(k.key, counts[k.key])).join(t("sidebar.list_separator")) });
  return (
    <span title={label} className={className ? `state-tally ${className}` : "state-tally"} data-testid="state-tally">
      <span className="sr-only">{label}</span>
      {shown.map((k) => (
        <span key={k.key} aria-hidden data-state={k.state}>
          <StatusMark state={k.state} size="sm" label={false} />
          {counts[k.key]}
        </span>
      ))}
    </span>
  );
}
