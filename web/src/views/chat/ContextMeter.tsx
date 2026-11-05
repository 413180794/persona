// The session header's model name and thin context bar (#2430): `opus 5.5 ▬▬ 61%`, the tooltip 「还剩 61% 上下文」
// (「已用 39%」 with 设置 › 外观 › 上下文显示为已用). Always shown in a wide header, like Moshi's session header (the user asked to follow
// Moshi); 「Agent 行显示详情」 only gates the sidebar rows' second line. Where the header is narrower than 800px (a phone, a side panel
// open beside the chat) the header hides it so the title fits (#3532, #3670). Where the header is 1000px or wider the number
// says what it is (「上下文剩 61%」, #3668); between 800 and 1000px it stays the bare share so the title keeps its room. The tooltip
// adds what happens when the window fills and, once close, what to do about it. A tablet has no hover, so a click (a tap)
// opens the same words as a popover, with the token count (#4054).
import { useEffect } from "react";
import { contextHeaderLabel, contextHint, contextLabel, contextNotes, contextShown, contextTier, contextTooltip, contextUsage, modelShort, useContextAsUsed, type ContextTier } from "../../lib/context-usage";
import { cx, EASE, Menu, useMenu } from "../../ui/atoms";
import type { Session } from "../../lib/types";

const TIER_BAR: Record<ContextTier, string> = { ok: "bg-accent", warn: "bg-orange", danger: "bg-red" };
const TIER_TEXT: Record<ContextTier, string> = { ok: "", warn: "font-medium text-orange", danger: "font-medium text-red" };

export function ContextMeter({ session }: { session: Session }) {
  const asUsed = useContextAsUsed();
  const info = useMenu();
  const model = modelShort(session.model);
  const usage = contextUsage(session);
  const known = usage !== null;
  // the window went unknown (a /clear) while it was open: close it, or it would come back by itself on the next reply
  useEffect(() => {
    if (!known) info.close();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only a change of `known` closes it
  }, [known]);
  if (!model && !usage) return null;
  const tier = usage ? contextTier(usage) : "ok";
  const tip = usage ? contextTooltip(usage, asUsed) : undefined;
  const title = [[model, tip].filter(Boolean).join(" · "), usage ? contextHint(usage) : ""].filter(Boolean).join("\n");
  const look = "flex shrink-0 items-center gap-1.5 whitespace-nowrap text-[11.5px] text-ink-3";
  if (!usage) {
    return (
      <div className={look} title={title}>
        <span className="max-w-28 truncate">{model}</span>
      </div>
    );
  }
  return (
    <>
      <button
        type="button"
        onClick={info.open}
        aria-haspopup="dialog"
        aria-expanded={Boolean(info.anchor)}
        // the visible words are in the name (「上下文剩 61%」, or the bare 「61%」 in a narrower header) and the explanation
        // is the description, open or not; the tooltip goes while the popover shows the same words
        aria-label={[model, contextHeaderLabel(usage, asUsed)].filter(Boolean).join(" · ")}
        aria-description={contextHint(usage)}
        title={info.anchor ? undefined : title}
        className={cx(look, "touch-hit -mx-1.5 rounded-chip px-1.5 transition-colors", info.anchor && "bg-hover")}
      >
        {model && <span className="max-w-28 truncate">{model}</span>}
        <span
          className="block h-1 w-10 overflow-hidden rounded-full bg-hover-2"
          role="progressbar"
          aria-label={tip}
          aria-valuenow={contextShown(usage, asUsed)}
          aria-valuemin={0}
          aria-valuemax={100}
        >
          <span className={cx("block h-full rounded-full transition-[width] duration-300", TIER_BAR[tier])} style={{ width: `${contextShown(usage, asUsed)}%`, transitionTimingFunction: EASE }} />
        </span>
        <span className={cx("tabular-nums @min-[1000px]:hidden", TIER_TEXT[tier])}>{contextLabel(usage, asUsed)}</span>
        <span className={cx("hidden tabular-nums @min-[1000px]:inline", TIER_TEXT[tier])}>{contextHeaderLabel(usage, asUsed)}</span>
      </button>
      {info.anchor && <Menu anchor={info.anchor} onClose={info.close} align="right" width={300} items={contextNotes(usage, asUsed)} />}
    </>
  );
}
