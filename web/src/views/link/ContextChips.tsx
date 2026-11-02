// Contexts waiting in a composer (picked in the link panel): one chip each above the input — what it is, the passage
// (or the page's title for a whole document), × to drop it. They go with the next message as <jarvis-context>.
import { removeContext, useContexts, WHOLE } from "../../lib/links";
import { EASE } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { QuoteIcon } from "./QuoteIcon";

export function ContextChips({ draftKey }: { draftKey: string }) {
  const refs = useContexts(draftKey);
  if (!refs.length) return null;
  return (
    <div className="flex flex-wrap gap-1.5 px-0.5 pt-0.5" aria-label="随消息发送的上下文">
      {refs.map((r) => {
        const whole = r.text === WHOLE;
        const label = whole ? r.title || r.url : r.text.replace(/\s+/g, " ");
        return (
          <span
            key={r.id}
            title={`${r.kind === "edit" ? "要改的内容" : "上下文"} · ${r.title}\n${whole ? r.url : r.text}`}
            className="flex h-7 max-w-[280px] min-w-0 items-center gap-1.5 rounded-full bg-accent-tint pr-1 pl-2.5 text-accent-ink"
            style={{ animation: `pop-in 180ms ${EASE} both` }}
          >
            {r.kind === "edit" ? <Icon name="spark" size={11} /> : <QuoteIcon size={12} />}
            <span className="shrink-0 text-[11px] font-semibold">{r.kind === "edit" ? "要改" : "上下文"}</span>
            <span className="min-w-0 truncate text-[12px]">{whole ? label : `「${label}」`}</span>
            <button
              type="button"
              aria-label="移除这个上下文"
              title="移除"
              onClick={() => removeContext(draftKey, r.id)}
              className="flex size-5 shrink-0 items-center justify-center rounded-full transition-colors hover:bg-accent-tint"
            >
              <Icon name="x" size={11} stroke={2.4} />
            </button>
          </span>
        );
      })}
    </div>
  );
}
