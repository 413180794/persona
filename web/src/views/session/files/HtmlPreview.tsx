// An HTML file shown as a page (docs/web.md「HTML 预览」, #1413): in a sandboxed frame with scripts and the network
// off; each can be turned on for this file while it is open. lib/html-preview.ts has the policy.
import { useEffect, useMemo, useState } from "react";
import { fileDownloadURL } from "../../../lib/api";
import { inlineSiblings } from "../../../lib/html-inline";
import { previewDocument, relativeRefs, sandboxFor } from "../../../lib/html-preview";
import { cx, IconButton, Loading } from "../../../ui/atoms";

export function HtmlPreview({ html, title, mid, path }: { html: string; title: string; mid: string; path: string }) {
  const [scripts, setScripts] = useState(false);
  const [network, setNetwork] = useState(false);
  const [reloads, setReloads] = useState(0);
  const policy = { scripts, network };
  const local = useMemo(() => relativeRefs(html), [html]);
  // files beside the page, read from the machine and put into it: the frame has no address to load them from
  const [inlined, setInlined] = useState<{ html: string; loaded: number; missing: number } | null>(null);
  useEffect(() => {
    let live = true;
    setInlined(null);
    if (!local) {
      setInlined({ html, loaded: 0, missing: 0 });
      return;
    }
    void inlineSiblings(html, path, async (p) => {
      const res = await fetch(fileDownloadURL(mid, p, ""));
      return res.ok ? res.blob() : null;
    })
      .catch(() => ({ html, loaded: 0, missing: local }))
      .then((r) => live && setInlined(r));
    return () => {
      live = false;
    };
  }, [html, path, mid, local, reloads]);
  const srcDoc = useMemo(() => (inlined ? previewDocument(inlined.html, { scripts, network }) : ""), [inlined, scripts, network]);
  const state = scripts && network ? "脚本和联网已打开" : scripts ? "已打开脚本，联网仍禁用" : network ? "已打开联网，脚本仍禁用" : "已禁用脚本和联网";
  const chip = (on: boolean) => cx("h-6 shrink-0 rounded-[6px] px-2 text-[12px] font-medium whitespace-nowrap transition-colors", on ? "bg-orange-tint text-orange" : "text-ink-3 hover:bg-hover hover:text-ink");
  return (
    <>
      <div className="flex min-h-9 shrink-0 items-center gap-2 border-b border-line px-3 py-1 text-[11.5px] whitespace-nowrap text-ink-3">
        <span className="min-w-0 flex-1 truncate" title={local ? "页面引用的同目录文件（图片、样式、字体、脚本）经 hub 读出来放进页面；隔离框本身仍不联网" : undefined}>
          {state}
          {inlined && inlined.loaded > 0 && ` · 从那台电脑读入了 ${inlined.loaded} 个同目录的文件`}
          {inlined && inlined.missing > 0 && ` · ${inlined.missing} 个读不到`}
        </span>
        <button type="button" aria-pressed={scripts} onClick={() => setScripts(!scripts)} className={chip(scripts)}>
          脚本
        </button>
        <button type="button" aria-pressed={network} onClick={() => setNetwork(!network)} className={chip(network)}>
          联网
        </button>
        <IconButton icon="retry" label="重新加载" onClick={() => setReloads((n) => n + 1)} />
      </div>
      {!inlined ? (
        <div className="flex flex-1 items-center justify-center bg-inset">
          <Loading label={`正在读入同目录的文件 · ${local} 个`} />
        </div>
      ) : (
      <iframe
        key={`${reloads}:${scripts}:${network}`}
        title={title}
        // never allow-same-origin: the page cannot reach this one, its storage or its token
        sandbox={sandboxFor(policy)}
        srcDoc={srcDoc}
        className="min-h-0 w-full flex-1 border-0 bg-white"
      />
      )}
    </>
  );
}
