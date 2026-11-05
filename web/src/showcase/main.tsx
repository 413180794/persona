// Dev only (showcase.html, not built): every page of the 组件展示 extension drawn by the real ExtensionUI from its
// snapshots, for screenshots next to the other apps' (#1852). The hub is faked: drawing the extension's page "showcase"
// with state <page> answers that page's view; a press answers the same view again (nothing is stored).
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "../styles.css";
import { ExtensionUI } from "../views/settings/ExtensionView";
import { SHOWCASE, showcaseKind } from "./snapshots";
import { Toasts } from "../ui/atoms";

const realFetch = window.fetch.bind(window);
window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
  const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
  if (url.includes("/-/ui/")) {
    const body = JSON.parse(String(init?.body ?? "{}")) as { state?: string };
    const view = SHOWCASE[body.state ?? ""] ?? SHOWCASE.home;
    return new Response(JSON.stringify({ view: { ...view, state: body.state } }), { status: 200, headers: { "Content-Type": "application/json" } });
  }
  return realFetch(input, init);
};

const q = new URLSearchParams(location.search);
const only = q.get("only");
const width = Number(q.get("width")) || (only ? 390 : 440);
const some = q.get("pages")?.split(",").filter((n) => n in SHOWCASE);
const names = only ? [only] : some?.length ? some : Object.keys(SHOWCASE);

function Page({ name }: { name: string }) {
  const kind = showcaseKind(name);
  const ui = kind === "card" ? "showcase-card" : kind === "widget" ? "showcase-widget" : "showcase";
  return (
    <section data-page={name} className="flex flex-col gap-2">
      {!only && <p className="px-1 font-mono text-[11px] text-ink-3">{name}</p>}
      <div className={kind === "page" ? "" : "rounded-card bg-surface py-2 shadow-card"}>
        <ExtensionUI extension="showcase" ui={ui} state={name} compact={kind !== "page"} history={false} title="组件展示" />
      </div>
    </section>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <main className="min-h-dvh bg-page p-4 text-ink" style={{ width }}>
      <div className="flex flex-col gap-8">
        {names.map((n) => (
          <Page key={n} name={n} />
        ))}
      </div>
      <Toasts />
    </main>
  </StrictMode>,
);
