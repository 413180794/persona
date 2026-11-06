// "View full text (12 KB)" under a text the hub cut for the page (Item.full_size; Android FullTextButton, iOS ResultBlock's
// cut): a click fetches the whole item (GET …/items/{id}/full) and it takes the cut one's place; "Loading…" meanwhile,
// "Failed to load, tap to retry" when it fails. The hub's own note at the end of the cut text goes: the button says it.
import { useState, type ReactNode } from "react";
import { fullItem } from "../../lib/api";
import { bytes } from "../../lib/format";
import { withoutCutMark } from "../../lib/tool-detail";
import type { Item } from "../../lib/types";
import { cx, Spinner } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { t } from "../../i18n";

/** The text to show for `item` (whole once loaded, else without the cut note) and the button that loads the rest. */
export function useFullText(item: Item | undefined, mid: string | undefined, sid: string | undefined): { text: string; more: ReactNode } {
  const [whole, setWhole] = useState<{ id: string; text: string } | null>(null);
  const [state, setState] = useState<"idle" | "loading" | "failed">("idle");
  const raw = item?.text ?? "";
  if (!item) return { text: "", more: null };
  if (whole?.id === item.id) return { text: whole.text, more: null };
  const size = item.full_size;
  if (!size || !mid || !sid) return { text: raw, more: null };
  const load = async () => {
    setState("loading");
    try {
      const it = await fullItem(mid, sid, item.id);
      setWhole({ id: item.id, text: it.text ?? "" });
      setState("idle");
    } catch {
      setState("failed");
    }
  };
  return { text: withoutCutMark(raw), more: <FullTextButton size={size} state={state} onClick={load} /> };
}

function FullTextButton({ size, state, onClick }: { size: number; state: "idle" | "loading" | "failed"; onClick: () => void }) {
  const loading = state === "loading";
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation();
        if (!loading) onClick();
      }}
      disabled={loading}
      aria-busy={loading || undefined}
      className={cx(
        "mt-1.5 flex h-7 w-fit items-center gap-1.5 rounded-control px-1 font-sans text-[12.5px] font-medium whitespace-nowrap transition-colors hover:bg-hover disabled:opacity-60",
        state === "failed" ? "text-red" : "text-accent-ink",
      )}
      data-full-text={state}
    >
      {loading ? <Spinner size={12} /> : <Icon name={state === "failed" ? "retry" : "external"} size={12} />}
      {loading ? t("chat-fulltext.loading") : state === "failed" ? t("chat-fulltext.failed") : t("chat-fulltext.show_all", { size: bytes(size) })}
    </button>
  );
}
