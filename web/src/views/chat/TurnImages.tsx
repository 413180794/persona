// The pictures an agent looked at in a turn, under its answer (#1540; iOS / Mac #1543 draw the same): one picture is a
// card like a file card (its thumbnail, name, where it came from); several are a strip that scrolls sideways and fades
// at its edge. A tap opens the gallery of the turn's pictures. Tiles keep their keys and the strip its height, so a
// picture arriving while the agent works appends without moving anything above it.
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { fileDownloadURL, fileThumbURL } from "../../lib/api";
import { openPanel } from "../../lib/panel";
import type { TurnImage } from "../../lib/turn-images";
import { copyText, cx, IconButton } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { imageSrc } from "./Steps";

const TILE_H = 60;
const ONE_H = 88; // a lone picture: a little larger

/** The picture's own bytes (for the gallery) and a small one for the strip. */
function sources(img: TurnImage, mid: string, sid: string, cwd?: string): { full: string; thumb: string } {
  if (img.item) {
    const src = imageSrc(img.item, mid, sid);
    return { full: src, thumb: src };
  }
  return { full: fileDownloadURL(mid, img.path!, cwd, { inline: true }), thumb: fileThumbURL(mid, img.path!, cwd, 256) };
}

const SOURCE: Record<TurnImage["source"], string> = { read: "读取的图片", screenshot: "截图", browser: "网页截图", mcp: "MCP 工具", tool: "工具返回" };

function meta(img: TurnImage): string {
  // a screenshot's label already says what it is (截图 · MacBook): only the size then
  const what = img.label.startsWith(SOURCE[img.source]) ? "" : SOURCE[img.source];
  return [what, img.width && img.height ? `${img.width} × ${img.height}` : ""].filter(Boolean).join(" · ");
}

/** The turn's pictures, between its work row and its answer (#1540). */
export function TurnImages({ images, mid, sid, cwd }: { images: TurnImage[]; mid: string; sid: string; cwd?: string }) {
  const [open, setOpen] = useState<number | null>(null);
  if (!images.length) return null;
  return (
    <div className="flex flex-col gap-1.5" data-turn-images="">
      <Strip images={images} mid={mid} sid={sid} cwd={cwd} onOpen={setOpen} />
      {open !== null && <Gallery images={images} start={open} mid={mid} sid={sid} cwd={cwd} onClose={() => setOpen(null)} />}
    </div>
  );
}

/** The pictures as a row of thumbnails that scrolls sideways, fading at the edge that has more; one picture is just a
 * larger thumbnail. No words: names and sizes are on hover and in the gallery. */
function Strip({ images, mid, sid, cwd, onOpen }: { images: TurnImage[]; mid: string; sid: string; cwd?: string; onOpen: (i: number) => void }) {
  const row = useRef<HTMLDivElement>(null);
  const [edges, setEdges] = useState({ left: false, right: false });
  const measure = useCallback(() => {
    const el = row.current;
    if (!el) return;
    const left = el.scrollLeft > 2;
    const right = el.scrollLeft + el.clientWidth < el.scrollWidth - 2;
    setEdges((e) => (e.left === left && e.right === right ? e : { left, right }));
  }, []);
  useLayoutEffect(measure, [images.length, measure]);
  useEffect(() => {
    const el = row.current;
    if (!el) return;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [measure]);
  const mask = edges.left && edges.right ? "linear-gradient(90deg,transparent,#000 28px,#000 calc(100% - 44px),transparent)" : edges.right ? "linear-gradient(90deg,#000 calc(100% - 44px),transparent)" : edges.left ? "linear-gradient(90deg,transparent,#000 28px)" : undefined;
  return (
    <>
      <div
        ref={row}
        onScroll={measure}
        className="flex gap-2 overflow-x-auto overscroll-x-contain pb-0.5 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
        style={{ height: (images.length === 1 ? ONE_H : TILE_H) + 2, maskImage: mask, WebkitMaskImage: mask }}
      >
        {images.map((img, i) => (
          <Tile key={img.key} img={img} mid={mid} sid={sid} cwd={cwd} h={images.length === 1 ? ONE_H : TILE_H} onOpen={() => onOpen(i)} />
        ))}
      </div>
    </>
  );
}

function Tile({ img, mid, sid, cwd, h, onOpen }: { img: TurnImage; mid: string; sid: string; cwd?: string; h: number; onOpen: () => void }) {
  const { thumb } = sources(img, mid, sid, cwd);
  const [failed, setFailed] = useState(false);
  // the picture's shape, kept between square and a wide screenshot, so a row of mixed pictures stays even
  const ratio = img.width && img.height ? img.width / img.height : 1;
  const w = Math.round(h * Math.max(h === ONE_H ? 0.75 : 1, Math.min(1.78, ratio)));
  return (
    <button
      type="button"
      onClick={onOpen}
      title={[img.label, meta(img)].filter(Boolean).join(" · ")}
      aria-label={img.label}
      className="group relative shrink-0 overflow-hidden rounded-[10px] bg-field shadow-hairline outline-none focus-visible:ring-2 focus-visible:ring-accent"
      style={{ width: w, height: h, animation: "fade-in 220ms ease-out both" }}
    >
      {failed ? (
        // no thumbnail (the file is gone, or the machine cannot draw it): a quiet glyph; the gallery still tries the file
        <span className="flex size-full items-center justify-center text-ink-3">
          <Icon name="image" size={18} />
        </span>
      ) : (
        <img src={thumb} alt="" loading="lazy" decoding="async" onError={() => setFailed(true)} className="size-full object-cover transition-transform duration-200 group-hover:scale-[1.05]" />
      )}
      <span className="pointer-events-none absolute inset-x-0 bottom-0 truncate bg-gradient-to-t from-black/70 to-transparent px-1.5 pt-3 pb-1 text-left text-[10.5px] font-medium text-white opacity-0 transition-opacity duration-150 group-hover:opacity-100 group-focus-visible:opacity-100">
        {img.label}
      </span>
    </button>
  );
}

/** The turn's pictures full size: ← / → or the thumbnails to move, Esc to close; download, copy the path, open the file. */
function Gallery({ images, start, mid, sid, cwd, onClose }: { images: TurnImage[]; start: number; mid: string; sid: string; cwd?: string; onClose: () => void }) {
  const [at, setAt] = useState(start);
  const img = images[Math.min(at, images.length - 1)];
  const { full } = sources(img, mid, sid, cwd);
  const go = useCallback((d: number) => setAt((i) => (i + d + images.length) % images.length), [images.length]);
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
      else if (e.key === "ArrowLeft") go(-1);
      else if (e.key === "ArrowRight") go(1);
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [go, onClose]);
  const download = () => {
    const a = document.createElement("a");
    a.href = img.path ? fileDownloadURL(mid, img.path, cwd) : full;
    a.download = img.path?.split("/").pop() || `${img.label}.png`;
    document.body.appendChild(a);
    a.click();
    a.remove();
  };
  return createPortal(
    <div role="dialog" aria-label="这一轮看过的图片" className="fixed inset-0 z-50 flex flex-col bg-black/85 text-white backdrop-blur-sm" style={{ animation: "fade-in 160ms ease-out both" }} onClick={onClose}>
      <div className="flex h-12 shrink-0 items-center gap-3 px-4" onClick={(e) => e.stopPropagation()}>
        <span className="min-w-0 flex-1 truncate text-[13px]">
          <span className="font-medium">{img.label}</span>
          <span className="ml-2 text-white/60">{meta(img)}</span>
        </span>
        <span className="shrink-0 text-[12px] text-white/60 tabular-nums">
          {at + 1} / {images.length}
        </span>
        <div className="flex shrink-0 items-center gap-1 [&_button]:text-white/85 [&_button:hover]:bg-white/10">
          <IconButton icon="arrowDown" label="下载" onClick={download} />
          {img.path && <IconButton icon="copy" label="拷贝路径" onClick={() => copyText(img.path!)} />}
          {img.path && (
            <IconButton
              icon="folder"
              label="在文件页打开"
              onClick={() => {
                onClose();
                openPanel({ kind: "file", mid, path: img.path!, cwd });
              }}
            />
          )}
          <IconButton icon="x" label="关闭" onClick={onClose} />
        </div>
      </div>
      <div className="relative flex min-h-0 flex-1 items-center justify-center px-14">
        <img key={img.key} src={full} alt={img.label} className="max-h-full max-w-full rounded-[6px] object-contain shadow-2xl" style={{ animation: "fade-in 160ms ease-out both" }} onClick={(e) => e.stopPropagation()} />
        {images.length > 1 && (
          <>
            <button type="button" aria-label="上一张" onClick={(e) => (e.stopPropagation(), go(-1))} className="absolute left-3 flex size-10 items-center justify-center rounded-full bg-white/10 hover:bg-white/20">
              <Icon name="chevronLeft" size={18} />
            </button>
            <button type="button" aria-label="下一张" onClick={(e) => (e.stopPropagation(), go(1))} className="absolute right-3 flex size-10 items-center justify-center rounded-full bg-white/10 hover:bg-white/20">
              <Icon name="chevronRight" size={18} />
            </button>
          </>
        )}
      </div>
      {images.length > 1 && (
        <div className="flex shrink-0 justify-center gap-2 overflow-x-auto px-4 py-3" onClick={(e) => e.stopPropagation()}>
          {images.map((m, i) => (
            <button
              key={m.key}
              type="button"
              aria-label={m.label}
              onClick={() => setAt(i)}
              className={cx("size-12 shrink-0 overflow-hidden rounded-[8px] transition-[opacity,box-shadow]", i === at ? "opacity-100 ring-2 ring-white" : "opacity-55 hover:opacity-90")}
            >
              <img src={sources(m, mid, sid, cwd).thumb} alt="" loading="lazy" className="size-full object-cover" />
            </button>
          ))}
        </div>
      )}
    </div>,
    document.body,
  );
}
