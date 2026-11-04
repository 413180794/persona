// An inbox item's body drawn by the plugin whose connector brought it in (docs/plugins.md「收件箱条目」, placement
// inbox_item): its View in place of the item's own body, under the detail's own header (title, 已处理, the summary,
// AI 草拟回复 / 交给 AI), which stays the web's. The hub is told which item (context.item) and looks it up itself;
// the plugin draws it again when the item changes. When it cannot draw it — a device that may only read, the item
// gone, the connector not the plugin's now (403 / 404) — the item's own body is drawn as if there were no plugin; when
// the plugin fails, the own body too, with a line saying so and a retry. A press that opens the item's source
// (persona://inbox/<item>/source, 在会话里看) shows the own body — the chat at the item's first message — with a way
// back to the plugin's page, which stays as it was.
import { useEffect, useMemo, useState } from "react";
import { ApiError, errorText } from "../../lib/api";
import type { InboxItem } from "../../lib/inbox";
import type { UIPlace } from "../../lib/plugin-ui";
import { Button, cx } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { PluginUI } from "../settings/PluginView";

/** What became of the plugin's draw: "" drawn (or drawing); quiet: the own body, said nothing; else why it failed. */
type Fallback = "" | { quiet: true } | { error: string };

/** How a failed first draw falls back: refused or nothing to draw (403 / 404) quietly, anything else (the plugin failed) with why. */
export function fallbackOf(e: unknown): Exclude<Fallback, ""> {
  if (e instanceof ApiError && (e.status === 403 || e.status === 404)) return { quiet: true };
  return { error: errorText(e) || "出错了" };
}

/** `onOwn`: whether the item's own body is drawn now (the header's Pin button works on the chat it draws). */
export function InboxPluginBody({ item, place, own, onOwn }: { item: InboxItem; place: UIPlace; own: () => React.ReactNode; onOwn?: (shown: boolean) => void }) {
  const [fallback, setFallback] = useState<Fallback>("");
  const [source, setSource] = useState(false); // the plugin opened the item's own source page
  const [n, setN] = useState(0);
  const context = useMemo(() => ({ item: item.id }), [item.id]);
  const ownShown = !!fallback || source;
  useEffect(() => onOwn?.(ownShown), [ownShown, onOwn]);
  if (fallback && "quiet" in fallback) return <>{own()}</>;
  if (fallback) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <div role="status" className="flex shrink-0 items-center gap-2 border-b border-line bg-inset px-4 py-1.5 sm:px-6">
          <Icon name="plugin" size={12} className="shrink-0 text-ink-3" />
          <p className="min-w-0 flex-1 truncate text-[12px] text-ink-3" title={fallback.error}>
            {place.plugin_name || place.title}的详情没画出来：{fallback.error}
          </p>
          <Button
            tone="quiet"
            icon="retry"
            onClick={() => {
              setFallback("");
              setN((x) => x + 1);
            }}
          >
            重试
          </Button>
        </div>
        {own()}
      </div>
    );
  }
  return (
    <>
      {source && (
        <div className="flex min-h-0 flex-1 flex-col" data-inbox-source>
          <div className="flex shrink-0 items-center border-b border-line bg-inset px-2 py-1 sm:px-4">
            <Button tone="quiet" icon="chevronLeft" onClick={() => setSource(false)}>
              回到{place.title}
            </Button>
          </div>
          {own()}
        </div>
      )}
      <PluginPage hidden={source} place={place}>
        <PluginUI
          key={n}
          plugin={place.plugin}
          ui={place.id}
          context={context}
          bare
          title={place.title}
          revision={item.version}
          onUnsupported={() => setFallback({ quiet: true })}
          onMissing={() => setFallback({ quiet: true })}
          onFailed={(e) => setFallback(fallbackOf(e))}
          onOpenSource={() => setSource(true)}
        />
      </PluginPage>
    </>
  );
}

/** The plugin's page in its own scroller; kept (hidden) while the item's source is open, to come back to as it was. */
function PluginPage({ hidden, place, children }: { hidden: boolean; place: UIPlace; children: React.ReactNode }) {
  return (
    <div className={cx("min-h-0 flex-1 overflow-y-auto", hidden && "hidden")} data-plugin-inbox={`${place.plugin}/${place.id}`}>
      <div className="mx-auto max-w-[760px] px-4 py-5 sm:px-6">{children}</div>
    </div>
  );
}
