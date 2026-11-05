// The session header's model name and thin context bar (#2430): `opus 5.5 ▬▬ 61%`, the tooltip 「还剩 61% 上下文」
// (「已用 39%」 with 设置 › 外观 › 上下文显示为已用). Always shown in a wide header, like Moshi's session header (the user asked to follow
// Moshi); 「Agent 行显示详情」 only gates the sidebar rows' second line. Where the header is narrower than 800px (a phone, a side panel
// open beside the chat) the header hides it so the title fits (#3532, #3670).
import { contextLabel, contextShown, contextTier, contextTooltip, contextUsage, modelShort, useContextAsUsed, type ContextTier } from "../../lib/context-usage";
import { cx, EASE } from "../../ui/atoms";
import type { Session } from "../../lib/types";

const TIER_BAR: Record<ContextTier, string> = { ok: "bg-accent", warn: "bg-orange", danger: "bg-red" };
const TIER_TEXT: Record<ContextTier, string> = { ok: "", warn: "font-medium text-orange", danger: "font-medium text-red" };

export function ContextMeter({ session }: { session: Session }) {
  const asUsed = useContextAsUsed();
  const model = modelShort(session.model);
  const usage = contextUsage(session);
  if (!model && !usage) return null;
  const tier = usage ? contextTier(usage) : "ok";
  const tip = usage ? contextTooltip(usage, asUsed) : undefined;
  return (
    <div className="flex shrink-0 items-center gap-1.5 whitespace-nowrap text-[11.5px] text-ink-3" title={[model, tip].filter(Boolean).join(" · ")}>
      {model && <span className="max-w-28 truncate">{model}</span>}
      {usage && (
        <>
          <div
            className="h-1 w-10 overflow-hidden rounded-full bg-hover-2"
            role="progressbar"
            aria-label={tip}
            aria-valuenow={contextShown(usage, asUsed)}
            aria-valuemin={0}
            aria-valuemax={100}
          >
            <div className={cx("h-full rounded-full transition-[width] duration-300", TIER_BAR[tier])} style={{ width: `${contextShown(usage, asUsed)}%`, transitionTimingFunction: EASE }} />
          </div>
          <span className={cx("tabular-nums", TIER_TEXT[tier])}>{contextLabel(usage, asUsed)}</span>
        </>
      )}
    </div>
  );
}
