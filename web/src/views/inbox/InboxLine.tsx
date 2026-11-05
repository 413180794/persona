// 线索 (#2128, docs/inbox.md「线索」): under an item's summary, what it grew out of (its parent's chain, up to a todo or
// an agent session) and what grew out of it (the items right under it). Each row opens where it lives. Nothing is
// drawn when the item has neither.
import { useEffect, useState } from "react";
import { fetchInboxTree, parentRoute, type InboxItem, type InboxParent, type InboxTree } from "../../lib/inbox";
import { navigate, type Route } from "../../lib/router";
import { cx } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

export type LineRow = { key: string; label: string; title: string; route?: Route; up: boolean };

const kindLabel: Record<InboxParent["kind"], string> = { inbox: "事件", todo: "待办", session: "Agent 会话" };

/** The rows of a line: the ancestors farthest first (the chain reads top down), then the children oldest first. */
export function lineRows(tree: InboxTree): LineRow[] {
  const ups = [...tree.ancestors].reverse().map((a): LineRow => ({
    key: `up:${a.kind}:${a.id}`,
    label: kindLabel[a.kind],
    title: a.item?.title || (a.kind === "inbox" ? "已不在收件箱里" : a.id),
    route: a.kind === "inbox" && !a.item ? undefined : parentRoute(a),
    up: true,
  }));
  const downs = tree.children.map((c): LineRow => ({ key: `down:${c.id}`, label: "下一步", title: c.title, route: { name: "inbox", id: c.id }, up: false }));
  return [...ups, ...downs];
}

export function LineList({ rows }: { rows: LineRow[] }) {
  if (!rows.length) return null;
  return (
    <section className="mt-3" data-inbox-line>
      <p className="mb-1 text-[11.5px] font-medium text-ink-3">线索</p>
      <ul className="flex flex-col gap-0.5">
        {rows.map((r) => (
          <li key={r.key}>
            <button
              type="button"
              disabled={!r.route}
              onClick={() => r.route && navigate(r.route)}
              className={cx(
                "flex w-full min-w-0 items-center gap-2 rounded-md px-1.5 py-1 text-left text-[12.5px] whitespace-nowrap",
                r.route ? "text-ink hover:bg-field" : "cursor-default text-ink-3",
              )}
            >
              <Icon name={r.up ? "up" : "chevron"} size={12} />
              <span className="shrink-0 text-ink-3">{r.label}</span>
              <span className="truncate">{r.title}</span>
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}

export function InboxLine({ item }: { item: InboxItem }) {
  const [rows, setRows] = useState<LineRow[]>([]);
  const parentKey = item.parent ? `${item.parent.kind}:${item.parent.id}` : "";
  useEffect(() => {
    let live = true;
    setRows([]);
    fetchInboxTree("inbox", item.id)
      .then((t) => live && setRows(lineRows(t)))
      .catch(() => {}); // an older hub without the route: no line
    return () => {
      live = false;
    };
  }, [item.id, parentKey]);
  return <LineList rows={rows} />;
}
