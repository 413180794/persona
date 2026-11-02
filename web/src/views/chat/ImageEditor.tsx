// A small markup editor (iOS ImageEditor.swift): a box or a freehand pen in a few colors, undo, clear. The marks
// are kept in image pixels and burned into the bitmap at its own resolution on 完成.
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { imagePayload, MAX_IMAGE } from "../../lib/attachments";
import type { DraftImage } from "../../lib/drafts";
import { boxRect, burnWidth, fitRect, keepMark, toImage, toScreen, type Mark, type MarkTool, type Rect } from "../../lib/image-edit";
import { cx, Spinner, toast } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";

const COLORS = ["red", "yellow", "green", "blue", "white", "black"] as const;
const COLOR_NAME: Record<(typeof COLORS)[number], string> = { red: "红", yellow: "黄", green: "绿", blue: "蓝", white: "白", black: "黑" };

function markColor(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(`--mark-${name}`).trim() || name;
}

function drawMark(ctx: CanvasRenderingContext2D, m: Mark, map: (p: { x: number; y: number }) => { x: number; y: number }) {
  const pts = m.points.map(map);
  if (pts.length < 2) return;
  ctx.strokeStyle = m.color;
  ctx.beginPath();
  if (m.tool === "box") {
    const r = boxRect(pts[0], pts[pts.length - 1]);
    ctx.rect(r.x, r.y, r.width, r.height);
  } else {
    ctx.moveTo(pts[0].x, pts[0].y);
    for (const p of pts.slice(1)) ctx.lineTo(p.x, p.y);
  }
  ctx.stroke();
}

export function ImageEditor({ src, doneTitle = "完成", onDone, onCancel }: { src: string; doneTitle?: string; onDone: (img: DraftImage) => void; onCancel: () => void }) {
  const [img, setImg] = useState<HTMLImageElement | null>(null);
  const [tool, setTool] = useState<MarkTool>("box");
  const [color, setColor] = useState<(typeof COLORS)[number]>("red");
  const [marks, setMarks] = useState<Mark[]>([]);
  const [current, setCurrent] = useState<Mark | null>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });
  const [busy, setBusy] = useState(false);
  const area = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    let live = true;
    const el = new Image();
    el.src = src;
    el.decode().then(
      () => live && setImg(el),
      () => {
        if (!live) return;
        toast("图片读不出来，没法编辑", "error");
        onCancel();
      },
    );
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [src]);

  useLayoutEffect(() => {
    const el = area.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setSize({ width: el.clientWidth, height: el.clientHeight }));
    ro.observe(el);
    setSize({ width: el.clientWidth, height: el.clientHeight });
    return () => ro.disconnect();
  }, []);

  const natural = img ? { width: img.naturalWidth, height: img.naturalHeight } : { width: 0, height: 0 };
  const fit: Rect = fitRect(natural, size);

  // the picture and the marks, at the screen's pixel density
  useEffect(() => {
    const c = canvas.current;
    if (!c || !img || !size.width) return;
    const dpr = devicePixelRatio || 1;
    c.width = Math.round(size.width * dpr);
    c.height = Math.round(size.height * dpr);
    const ctx = c.getContext("2d")!;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, size.width, size.height);
    ctx.drawImage(img, fit.x, fit.y, fit.width, fit.height);
    ctx.lineWidth = 3.5;
    ctx.lineCap = "round";
    ctx.lineJoin = "round";
    for (const m of current ? [...marks, current] : marks) drawMark(ctx, m, (p) => toScreen(p, fit, natural));
  });

  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        onCancel();
      } else if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "z") {
        e.preventDefault();
        e.stopPropagation();
        setMarks((ms) => ms.slice(0, -1));
      }
    };
    addEventListener("keydown", key, true);
    return () => removeEventListener("keydown", key, true);
  }, [onCancel]);

  const at = (e: React.PointerEvent) => {
    const r = canvas.current!.getBoundingClientRect();
    return toImage({ x: e.clientX - r.left, y: e.clientY - r.top }, fit, natural);
  };

  const finish = async () => {
    if (!img) return;
    if (!marks.length) {
      // nothing drawn: the picture as it is
      setBusy(true);
      try {
        const blob = await (await fetch(src)).blob();
        onDone({ name: "image.png", ...(await imagePayload(new File([blob], "image", { type: blob.type || "image/png" }))) });
      } finally {
        setBusy(false);
      }
      return;
    }
    setBusy(true);
    try {
      const out = document.createElement("canvas");
      out.width = natural.width;
      out.height = natural.height;
      const ctx = out.getContext("2d")!;
      ctx.drawImage(img, 0, 0);
      ctx.lineWidth = burnWidth(natural);
      ctx.lineCap = "round";
      ctx.lineJoin = "round";
      for (const m of marks) drawMark(ctx, m, (p) => p);
      const blob = (await encode(out, "image/png")) ?? null;
      // a big screenshot as PNG can pass the 8 MB a message takes: JPEG then
      const fitting = blob && blob.size <= MAX_IMAGE ? blob : await encode(out, "image/jpeg", 0.9);
      if (!fitting || fitting.size > MAX_IMAGE) {
        toast("编辑后的图片太大了（最多 8 MB）", "error");
        return;
      }
      const name = fitting.type === "image/jpeg" ? "image.jpg" : "image.png";
      onDone({ name, ...(await imagePayload(new File([fitting], name, { type: fitting.type }))) });
    } catch {
      toast("没存成：浏览器读不了这张图的像素", "error");
    } finally {
      setBusy(false);
    }
  };

  return createPortal(
    <div role="dialog" aria-modal="true" aria-label="编辑图片" className="fixed inset-0 z-[60] flex flex-col bg-black text-white" style={{ animation: "fade-in 150ms ease-out both" }}>
      <div className="flex h-14 shrink-0 items-center gap-2 px-3">
        <button type="button" onClick={onCancel} className="h-9 rounded-full px-3 text-[13.5px] whitespace-nowrap text-white/85 transition-colors hover:bg-white/15 hover:text-white">
          取消
        </button>
        <span className="flex-1 truncate text-center text-[14px] font-semibold">编辑图片</span>
        <button
          type="button"
          onClick={() => void finish()}
          disabled={!img || busy}
          className="flex h-9 items-center gap-1.5 rounded-full bg-white px-4 text-[13.5px] font-semibold whitespace-nowrap text-black transition-[opacity,transform] active:scale-[0.97] disabled:opacity-50"
        >
          {busy && <Spinner />}
          {doneTitle}
        </button>
      </div>
      <div ref={area} className="relative min-h-0 flex-1">
        <canvas
          ref={canvas}
          className="absolute inset-0 size-full cursor-crosshair"
          style={{ touchAction: "none" }}
          onPointerDown={(e) => {
            if (!img || e.button !== 0) return;
            e.currentTarget.setPointerCapture(e.pointerId);
            setCurrent({ tool, color: markColor(color), points: [at(e)] });
          }}
          onPointerMove={(e) => {
            if (!current) return;
            const p = at(e);
            setCurrent((c) => (c ? { ...c, points: c.tool === "box" ? [c.points[0], p] : [...c.points, p] } : c));
          }}
          onPointerUp={() => {
            if (current && keepMark(current)) setMarks((ms) => [...ms, current]);
            setCurrent(null);
          }}
          onPointerCancel={() => setCurrent(null)}
        />
        {!img && (
          <div className="absolute inset-0 flex items-center justify-center">
            <Spinner />
          </div>
        )}
      </div>
      <div className="flex shrink-0 flex-col gap-3 px-4 pt-3 pb-[max(12px,env(safe-area-inset-bottom))]">
        <div className="flex items-center justify-center gap-3">
          {COLORS.map((c) => (
            <button
              key={c}
              type="button"
              aria-label={`${COLOR_NAME[c]}色`}
              aria-pressed={color === c}
              onClick={() => setColor(c)}
              className={cx("size-7 rounded-full transition-[box-shadow,transform] active:scale-90", color === c ? "ring-3 ring-white" : "ring-1 ring-white/40")}
              style={{ background: `var(--mark-${c})` }}
            />
          ))}
        </div>
        <div className="flex items-center gap-2">
          <ToolButton icon="square" active={tool === "box"} onClick={() => setTool("box")}>
            框选
          </ToolButton>
          <ToolButton icon="write" active={tool === "pen"} onClick={() => setTool("pen")}>
            画笔
          </ToolButton>
          <div className="flex-1" />
          <IconAction icon="undo" label="撤销（⌘Z）" disabled={!marks.length} onClick={() => setMarks((ms) => ms.slice(0, -1))} />
          <IconAction icon="trash" label="清除" disabled={!marks.length} onClick={() => setMarks([])} />
        </div>
      </div>
    </div>,
    document.body,
  );
}

function encode(c: HTMLCanvasElement, type: string, quality?: number): Promise<Blob | null> {
  return new Promise((ok) => c.toBlob(ok, type, quality));
}

function ToolButton({ icon, active, onClick, children }: { icon: IconName; active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cx(
        "flex h-9 items-center gap-1.5 rounded-full px-4 text-[13px] font-medium whitespace-nowrap transition-[background-color,color]",
        active ? "bg-white text-black" : "bg-white/15 text-white hover:bg-white/25",
      )}
    >
      <Icon name={icon} size={14} />
      {children}
    </button>
  );
}

function IconAction({ icon, label, disabled, onClick }: { icon: IconName; label: string; disabled: boolean; onClick: () => void }) {
  return (
    <button type="button" aria-label={label} title={label} disabled={disabled} onClick={onClick} className="flex size-9 items-center justify-center rounded-full bg-white/15 transition-colors hover:bg-white/25 disabled:opacity-40">
      <Icon name={icon} size={15} />
    </button>
  );
}
