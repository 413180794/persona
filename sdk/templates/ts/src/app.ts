import { decodeState, encodeState, open, type Component, type Program, type UIRequest, type View } from "../sdk/ts/src/index.ts";
import manifest from "../persona-extension.json";

interface Item {
  id: string;
  title: string;
  detail: string;
}

/** The home page's state: what is searched for, which item is open (a page pushed on top of the list). */
interface PageState {
  q: string;
  open: string;
}

export function program(): Program {
  let greeting = "你好";
  let apiKey = "";

  // The sample data: your service's goes here (read with apiKey).
  const items = (): Item[] =>
    Array.from({ length: 20 }, (_, i) => ({ id: `${i + 1}`, title: `示例条目 ${i + 1}`, detail: `第 ${i + 1} 条的详情。` }));

  function detail(r: UIRequest, s: PageState): View {
    const it = items().find((x) => x.id === s.open);
    if (!it) return { title: "不见了", sections: [{ items: [{ type: "empty", title: "这一条不见了" }] }] };
    const body: Component[] = [{ type: "markdown", text: `**${it.title}**\n\n${it.detail}` }];
    // Drawn in place of the list in an app before UI version 3: no back button of the app's there.
    if (r.ui_version < 3) body.push({ type: "button", title: "返回", action: { id: "back" } });
    return { title: it.title, state: encodeState(s), sections: [{ items: body }] };
  }

  function home(r: UIRequest): View {
    const s = decodeState<PageState>(r, { q: "", open: "" });
    if (s.open) return detail(r, s);
    const rows: Component[] = items()
      .filter((it) => !s.q || it.title.includes(s.q))
      .map((it) => ({ type: "row", title: it.title, icon: "doc", action: open(`open:${it.id}`, "", { q: s.q, open: it.id }, it.title) }));
    if (rows.length === 0) rows.push({ type: "empty", title: `没有找到「${s.q}」`, icon: "search" });
    const top: Component[] = [{ type: "text", text: greeting }];
    const v: View = {
      title: manifest.name,
      state: encodeState(s),
      sections: [{ items: top }, { title: "条目", footer: `API Key：${apiKey ? "已设置" : "未设置"}`, items: rows }],
    };
    if (r.ui_version >= 3) v.search = { placeholder: "条目名", value: s.q, action: { id: "search" } };
    else {
      // An app before UI version 3 has no search box: a field and a button.
      top.push(
        { type: "field", name: "q", label: "搜索", kind: "text", value: s.q, placeholder: "条目名" },
        { type: "button", title: "搜索", action: { id: "search", submit: true } },
      );
    }
    return v;
  }

  return {
    id: manifest.id,
    version: manifest.version,
    manifest,
    init(init) {
      const c = (init.config ?? {}) as { greeting?: string };
      if (c.greeting) greeting = c.greeting;
      apiKey = init.secrets?.api_key ?? "";
    },
    // Contribution ids are the manifest's: unique among every extension's on the hub.
    ui: {
      "{{id}}": {
        render: home,
        action(r) {
          const s = decodeState<PageState>(r, { q: "", open: "" });
          if (r.action === "search") Object.assign(s, { q: (r.values?.q ?? "").trim(), open: "" });
          else if (r.action === "back") s.open = "";
          r.state = encodeState(s); // nothing returned: the page draws again with the new state
        },
      },
      "{{id}}-today": {
        render: () => ({
          sections: [{ items: [{ type: "stat_grid", stats: [{ label: "条目", value: `${items().length}` }, { label: "问候", value: greeting }] }] }],
        }),
      },
    },
  };
}
