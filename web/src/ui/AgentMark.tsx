// The mark of the agent that runs in a row, a header, a pane or a launcher (#3391): drawn as the owner publishes it, like
// BrandMark (CLAUDE.md: brand marks are not redrawn), in a fixed box so a column of rows lines up, centred, at the share
// of the box its shape takes. Next to the state dot, never in its place. The one-color marks are templates drawn in
// the text color, so they read in light and dark and in a selected button; Claude keeps its coral (Anthropic's mark is
// coral on neutral). An agent with no mark of its own (TRAEX, a runtime, whatever else herdr reports) draws its first
// letter on a plain tile of the same box.
//
// Where each mark comes from (also in THIRD_PARTY_NOTICES.md). Simple Icons (SI) releases the files under CC0; the
// marks stay their owners' trademarks and are shown only to say which agent a row is.
//   claude    SI 16.36.0 `claude.svg` (source claude.ai), coral #D97757 as in apple/…/Brands/brand-claude
//   codex     OpenAI Blossom, github.com/openai/openai-cookbook `chatgpt-blossom-black.svg`, as apple/…/Brands/brand-openai
//   opencode  SI `opencode.svg`; source github.com/anomalyco/opencode packages/identity/mark.svg (the project is MIT)
//   cursor    SI `cursor.svg`; source cursor.com/brand
//   gemini    SI `googlegemini.svg`; source gemini.google.com (no brand kit found)
//   pi        SI `pi.svg`; source pi.dev/favicon.svg (no licence text found)
// viewBoxes of opencode and cursor are cropped to the glyph (CLAUDE.md: assets carry no padding); nothing else is changed.
import { agentId, agentLetter, agentName, isMarked, sessionAgent, type MarkedAgent } from "../lib/agent-kind";
import type { Session } from "../lib/types";
import { cx } from "./atoms";
import claudeMark from "./brands/claude.svg";
import codexMark from "./brands/codex.svg";
import cursorMark from "./brands/cursor.svg";
import geminiMark from "./brands/gemini.svg";
import opencodeMark from "./brands/opencode.svg";
import piMark from "./brands/pi.svg";

// `fit`: the share of the box the mark is drawn at. Round and full-bleed shapes fill it; a square or a tall one is
// pulled in so it does not look bigger than its neighbours (iOS: `Theme.Brand.optical`).
const MARKS: Record<MarkedAgent, { src: string; template?: boolean; fit: number }> = {
  claude: { src: claudeMark, fit: 1 },
  codex: { src: codexMark, template: true, fit: 1 },
  opencode: { src: opencodeMark, template: true, fit: 0.88 },
  pi: { src: piMark, template: true, fit: 0.88 },
  gemini: { src: geminiMark, template: true, fit: 1 },
  cursor: { src: cursorMark, template: true, fit: 0.92 },
};

/**
 * `agent` is what the node reports ("" is Claude Code); `label` the runtime's own name for an agent the app does not
 * know. `size` is the box. The mark says what it is (role=img, "Claude Code"); `decorative` for a place that names the
 * agent in text right beside it.
 */
export function AgentMark({ agent, label, size = 14, decorative, className }: { agent?: string; label?: string; size?: number; decorative?: boolean; className?: string }) {
  const id = agentId(agent);
  const name = agentName(agent, label);
  const a11y = decorative ? { "aria-hidden": true as const } : { role: "img" as const, "aria-label": name };
  const box = cx("inline-flex shrink-0 items-center justify-center", className ?? "text-ink-2");
  if (!isMarked(id)) {
    return (
      <span {...a11y} data-agent-mark="letter" className={box} style={{ width: size, height: size }}>
        <span
          aria-hidden
          className="flex items-center justify-center bg-inset font-semibold leading-none"
          style={{ width: Math.round(size * 0.92), height: Math.round(size * 0.92), borderRadius: Math.max(3, Math.round(size * 0.28)), fontSize: Math.max(8, Math.round(size * 0.6)) }}
        >
          {agentLetter(name)}
        </span>
      </span>
    );
  }
  const { src, template, fit } = MARKS[id];
  const px = Math.round(size * fit);
  const mask = `url("${src}") center / contain no-repeat`;
  return (
    <span {...a11y} data-agent-mark={id} className={box} style={{ width: size, height: size }}>
      {template ? (
        <span aria-hidden className="block bg-current" style={{ width: px, height: px, mask, WebkitMask: mask }} />
      ) : (
        <img src={src} alt="" aria-hidden width={px} height={px} style={{ width: px, height: px }} />
      )}
    </span>
  );
}

/** The mark of a session's agent. */
export function SessionMark({ s, ...rest }: { s: Pick<Session, "agent" | "agent_label" | "source_kind"> } & Omit<Parameters<typeof AgentMark>[0], "agent" | "label">) {
  return <AgentMark {...sessionAgent(s)} {...rest} />;
}
