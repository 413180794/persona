// Two versions of a changed file side by side in the Git panel (docs/web.md「对比」, #1437; iOS / Mac: #1299): pictures
// next to each other or under a slider, documents (PDF / docx / pptx / xlsx) as a diff of their text by page, slide or
// sheet. A version comes from git/blob (written into the node's cache) or the working tree.
import { useEffect, useState } from "react";
import { api, enc, errorText, fileDownloadURL, isAbort } from "../../../lib/api";
import { diffLines, docxUnits, flatten, hunks, pairRows, pdfUnits, pptxUnits, ROW_SEP, shownText, workbookUnits, type Hunk, type Unit } from "../../../lib/compare";
import { extOf } from "../../../lib/files";
import { openPanel } from "../../../lib/panel";
import { Button, cx, Loading } from "../../../ui/atoms";
import { Empty, Segmented } from "./parts";

const IMAGE_EXTS = ["png", "jpg", "jpeg", "gif", "webp", "bmp", "avif", "svg", "ico", "heic"];
const DOC_EXTS = ["pdf", "docx", "pptx", "xlsx", "xlsm", "xls", "ods"];

/** Whether the Git panel compares this file itself (instead of a text diff). */
export function compareKind(path: string): "image" | "doc" | null {
  const ext = extOf(path);
  return IMAGE_EXTS.includes(ext) ? "image" : DOC_EXTS.includes(ext) ? "doc" : null;
}

/** HEAD, INDEX, a commit or "<commit>^"; WORKTREE is the file as it is on disk. */
export type Rev = string;

interface Side {
  label: string;
  path?: string; // on the machine; missing: the file does not exist in that version
}

async function resolve(mid: string, cwd: string, root: string, path: string, rev: Rev, signal: AbortSignal): Promise<Side> {
  if (rev === "WORKTREE") return { label: "工作区", path: `${root.replace(/\/$/, "")}/${path}` };
  const b = await api<{ path?: string; missing?: boolean }>(`/machines/${enc(mid)}/git/blob?cwd=${enc(cwd)}&path=${enc(path)}&rev=${enc(rev)}`, { signal });
  const label = rev === "HEAD" ? "HEAD" : rev === "INDEX" ? "暂存区" : rev.endsWith("^") ? `${rev.slice(0, 7)} 之前` : rev.slice(0, 7);
  return { label, path: b.missing ? undefined : b.path };
}

export function CompareView({ mid, cwd, root, path, before, after, deleted }: { mid: string; cwd: string; root: string; path: string; before: Rev; after: Rev; deleted?: boolean }) {
  const [sides, setSides] = useState<[Side, Side] | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    const ac = new AbortController();
    setSides(null);
    setError("");
    Promise.all([resolve(mid, cwd, root, path, before, ac.signal), deleted && after === "WORKTREE" ? Promise.resolve({ label: "工作区" }) : resolve(mid, cwd, root, path, after, ac.signal)])
      .then((s) => !ac.signal.aborted && setSides(s))
      .catch((e) => !isAbort(e) && setError(errorText(e)));
    return () => ac.abort();
  }, [mid, cwd, root, path, before, after, deleted]);
  if (error) return <Empty icon="info" title="取不到这两个版本" detail={error} />;
  if (!sides)
    return (
      <div className="flex flex-1 items-center justify-center">
        <Loading label="正在取两个版本" />
      </div>
    );
  return compareKind(path) === "image" ? <ImageCompare mid={mid} sides={sides} /> : <DocCompare mid={mid} sides={sides} ext={extOf(path)} />;
}

function OpenButtons({ mid, sides }: { mid: string; sides: [Side, Side] }) {
  return (
    <div className="flex shrink-0 gap-1.5">
      {sides.map((s, i) =>
        s.path ? (
          <Button key={i} tone="quiet" onClick={() => openPanel({ kind: "file", mid, path: s.path! })}>
            打开{i === 0 ? "旧版" : "新版"}
          </Button>
        ) : null,
      )}
    </div>
  );
}

// ── pictures ──────────────────────────────────────────────

function ImageCompare({ mid, sides }: { mid: string; sides: [Side, Side] }) {
  const [mode, setMode] = useState<"side" | "slide">("side");
  const [at, setAt] = useState(50);
  const src = (s: Side) => (s.path ? fileDownloadURL(mid, s.path, "", { inline: true }) : "");
  const both = sides.every((s) => s.path);
  const checker = { backgroundImage: "conic-gradient(var(--field) 25%, var(--inset) 0 50%, var(--field) 0 75%, var(--inset) 0)", backgroundSize: "20px 20px" };
  return (
    <>
      <div className="flex min-h-10 shrink-0 items-center gap-2 border-b border-line px-3 py-1.5">
        {both && <Segmented label="对比方式" value={mode} onChange={setMode} options={[["side", "并排"], ["slide", "滑动"]]} />}
        <span className="min-w-0 flex-1 truncate text-[11.5px] text-ink-3">
          {sides[0].label} → {sides[1].label}
        </span>
        <OpenButtons mid={mid} sides={sides} />
      </div>
      {mode === "side" || !both ? (
        <div className="grid min-h-0 flex-1 grid-cols-2 gap-px bg-line">
          {sides.map((s, i) => (
            <div key={i} className="flex min-h-0 flex-col bg-surface">
              <div className={cx("shrink-0 px-3 py-1.5 text-[11.5px] font-medium", i === 0 ? "text-red" : "text-green")}>
                {i === 0 ? "旧版" : "新版"} · {s.label}
              </div>
              <div className="flex min-h-0 flex-1 items-center justify-center overflow-auto p-4" style={checker}>
                {s.path ? <img src={src(s)} alt="" className="max-h-full max-w-full shadow-hairline" /> : <span className="text-[12.5px] text-ink-3">这个版本里没有这个文件</span>}
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="relative flex min-h-0 flex-1 items-center justify-center overflow-hidden p-4" style={checker}>
            <div className="relative max-h-full max-w-full">
              <img src={src(sides[1])} alt="新版" className="block max-h-[70vh] max-w-full" />
              <img src={src(sides[0])} alt="旧版" className="absolute inset-0 block h-full w-full object-contain" style={{ clipPath: `inset(0 ${100 - at}% 0 0)` }} />
              <div className="pointer-events-none absolute top-0 bottom-0 w-[2px] bg-accent" style={{ left: `${at}%` }} />
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-3 border-t border-line px-4 py-2 text-[11.5px] whitespace-nowrap">
            <span className="text-red">旧版</span>
            <input type="range" min={0} max={100} value={at} onChange={(e) => setAt(Number(e.target.value))} aria-label="旧版和新版的分界" className="flex-1 accent-[var(--accent)]" />
            <span className="text-green">新版</span>
          </div>
        </div>
      )}
    </>
  );
}

// ── documents ─────────────────────────────────────────────

async function unitsOf(mid: string, side: Side, ext: string, signal: AbortSignal): Promise<Unit[]> {
  if (!side.path) return [];
  const res = await fetch(fileDownloadURL(mid, side.path, ""), { signal });
  if (!res.ok) throw new Error(`读不到 ${side.label} 的版本（HTTP ${res.status}）`);
  const data = await res.arrayBuffer();
  if (ext === "pdf") return pdfUnits(data);
  const parse = (s: string) => new DOMParser().parseFromString(s, "application/xml");
  if (ext === "docx" || ext === "pptx") {
    const { default: JSZip } = await import("jszip");
    const zip = await JSZip.loadAsync(data);
    return ext === "docx" ? docxUnits(zip, parse) : pptxUnits(zip, parse);
  }
  const XLSX = await import("xlsx");
  return workbookUnits(XLSX, XLSX.read(new Uint8Array(data), { type: "array" }));
}

function DocCompare({ mid, sides, ext }: { mid: string; sides: [Side, Side]; ext: string }) {
  const [state, setState] = useState<{ hunks?: Hunk[]; added?: number; removed?: number; error?: string; tooBig?: boolean }>({});
  useEffect(() => {
    const ac = new AbortController();
    setState({});
    Promise.all(sides.map((s) => unitsOf(mid, s, ext, ac.signal)))
      .then(([a, b]) => {
        if (ac.signal.aborted) return;
        const ops = diffLines(flatten(a), flatten(b));
        if (!ops) return setState({ tooBig: true });
        setState({ hunks: hunks(ops), added: ops.filter((o) => o.kind === "add").length, removed: ops.filter((o) => o.kind === "del").length });
      })
      .catch((e) => !isAbort(e) && setState({ error: errorText(e) }));
    return () => ac.abort();
  }, [mid, sides, ext]);
  const bar = (
    <div className="flex min-h-10 shrink-0 items-center gap-2 border-b border-line px-3 py-1.5 text-[11.5px] whitespace-nowrap text-ink-3 tabular-nums">
      <span className="min-w-0 flex-1 truncate">
        {sides[0].label} → {sides[1].label}
        {state.hunks && (
          <>
            {" · "}
            <span className="text-green">+{state.added}</span> <span className="text-red">−{state.removed}</span> 行 · {state.hunks.length} 处改动（按文字比较）
          </>
        )}
      </span>
      <OpenButtons mid={mid} sides={sides} />
    </div>
  );
  if (state.error)
    return (
      <>
        {bar}
        <Empty icon="info" title="比较不了" detail={state.error} />
      </>
    );
  if (state.tooBig)
    return (
      <>
        {bar}
        <Empty icon="info" title="改动太多，没法逐行比较" detail="分别打开旧版和新版看。" />
      </>
    );
  if (!state.hunks)
    return (
      <>
        {bar}
        <div className="flex flex-1 items-center justify-center">
          <Loading label="正在读取两个版本的文字" />
        </div>
      </>
    );
  return (
    <>
      {bar}
      {state.hunks.length === 0 ? (
        <Empty icon="check" title="文字没有变化" detail="改的可能是格式、图片或排版：分别打开旧版和新版看。" />
      ) : (
        <div className="min-h-0 flex-1 overflow-y-auto py-2">
          {state.hunks.map((h, i) => (
            <section key={i} className="mb-2">
              {h.skippedBefore > 0 && <div className="px-4 py-1 text-[11.5px] text-ink-3">… 未改动 {h.skippedBefore} 行</div>}
              <div className="sticky top-0 z-10 bg-surface/95 px-4 py-1 text-[12px] font-semibold text-ink-2 backdrop-blur">{h.unit}</div>
              {pairRows(h.ops).map((o, j) =>
                o.kind === "row" ? (
                  <div key={j} className="flex gap-2 bg-orange-tint/60 px-4 py-[3px] text-[13px] leading-[1.55] [overflow-wrap:anywhere]">
                    <span className="w-3 shrink-0 font-mono text-orange select-none">~</span>
                    <span className="min-w-0 text-ink-2">
                      {o.cells.map((c, k) => (
                        <span key={k}>
                          {k > 0 && <span className="text-ink-3">{ROW_SEP}</span>}
                          {c.old === c.new ? (
                            c.new
                          ) : (
                            <span className="text-ink">
                              <del className="text-red decoration-red/50">{c.old}</del>
                              <span className="px-0.5 text-ink-3">→</span>
                              <ins className="text-green no-underline">{c.new}</ins>
                            </span>
                          )}
                        </span>
                      ))}
                    </span>
                  </div>
                ) : (
                  <div
                    key={j}
                    className={cx(
                      "flex gap-2 px-4 py-[3px] text-[13px] leading-[1.55] whitespace-pre-wrap [overflow-wrap:anywhere]",
                      o.kind === "add" ? "bg-green-tint text-ink" : o.kind === "del" ? "bg-red-tint text-ink line-through decoration-red/40" : "text-ink-2",
                    )}
                  >
                    <span className={cx("w-3 shrink-0 font-mono select-none", o.kind === "add" ? "text-green" : o.kind === "del" ? "text-red" : "text-ink-3")}>{o.kind === "add" ? "+" : o.kind === "del" ? "−" : ""}</span>
                    <span className="min-w-0">{shownText(o.text)}</span>
                  </div>
                ),
              )}
            </section>
          ))}
        </div>
      )}
    </>
  );
}
