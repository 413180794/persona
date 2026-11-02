// Full-screen viewer for a conversation's images (iOS ImageViewer.swift): ← / → (or a swipe) through all of them,
// n / m on top, double-click for the real size; 编辑 (box + pen, then into the composer for the next message),
// 拷贝 where the browser allows it, 下载, 在新标签页打开. Esc or a click beside the picture closes it.
import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { imageURL } from "../../lib/api";
import { sendDraft, type DraftImage } from "../../lib/drafts";
import { fitRect } from "../../lib/image-edit";
import type { Item } from "../../lib/types";
import { cx, EASE, toast } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { useChatActions } from "./chat-actions";
import { ImageEditor } from "./ImageEditor";

export interface ViewerImage {
  id: string;
  src: string;
  mime?: string;
}

function viewerImage(item: Item, mid: string, sid: string): ViewerImage {
  const src = item.image?.data ? `data:${item.image.mime};base64,${item.image.data}` : imageURL(mid, sid, item.id);
  return { id: item.id, src, mime: item.image?.mime };
}

/** The viewer for a picture in the chat: the whole conversation's images when it is among them, else just this one. */
export function ChatImageViewer({ item, mid, sid, onClose }: { item: Item; mid: string; sid: string; onClose: () => void }) {
  const chat = useChatActions();
  const [{ images, index }] = useState(() => {
    const all = (chat?.images() ?? []).filter((i) => i.kind === "image");
    const at = all.findIndex((i) => i.id === item.id);
    return at >= 0 ? { images: all.map((i) => viewerImage(i, mid, sid)), index: at } : { images: [viewerImage(item, mid, sid)], index: 0 };
  });
  const attach = chat?.canCompose
    ? (img: DraftImage) => {
        if (!sendDraft(chat.draftKey, { type: "image", image: img })) {
          toast("没有能放图片的输入框", "error");
          return false;
        }
        toast("已加到输入框，补一句话就能发", "ok");
        return true;
      }
    : undefined;
  return <ImageViewer images={images} start={index} onClose={onClose} attach={attach} />;
}

const canCopyImage = () => typeof window !== "undefined" && window.isSecureContext && "ClipboardItem" in window && Boolean(navigator.clipboard?.write);

export function ImageViewer({
  images,
  start,
  onClose,
  attach,
}: {
  images: ViewerImage[];
  start: number;
  onClose: () => void;
  /** Where an edited picture goes (the chat's composer); none hides 编辑. True when it was taken. */
  attach?: (img: DraftImage) => boolean;
}) {
  const [index, setIndex] = useState(() => Math.min(Math.max(start, 0), images.length - 1));
  const [zoomed, setZoomed] = useState(false);
  const [editing, setEditing] = useState(false);
  const [failed, setFailed] = useState<Record<string, boolean>>({});
  const box = useRef<HTMLDivElement>(null);
  const swipe = useRef<{ x: number; y: number } | null>(null);
  const cur = images[index];
  const many = images.length > 1;
  // fitted like the app's viewer (scaled to fit, small pictures too): the picture's own size and the room it has
  const area = useRef<HTMLDivElement>(null);
  const [natural, setNatural] = useState<{ width: number; height: number } | null>(null);
  const [room, setRoom] = useState<{ width: number; height: number } | null>(null);
  useEffect(() => setNatural(null), [cur.id]);
  useEffect(() => {
    const el = area.current;
    if (!el) return;
    const ro = new ResizeObserver(() => {
      const cs = getComputedStyle(el);
      setRoom({ width: el.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight), height: el.clientHeight - parseFloat(cs.paddingTop) - parseFloat(cs.paddingBottom) });
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  const fitted = !zoomed && natural && room ? fitRect(natural, room) : null;

  const go = (d: number) => {
    setZoomed(false);
    setIndex((i) => Math.min(Math.max(i + d, 0), images.length - 1));
  };

  useEffect(() => {
    if (editing) return;
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        onClose();
      } else if (e.key === "ArrowLeft") go(-1);
      else if (e.key === "ArrowRight") go(1);
    };
    // capture: before the chat's own Esc (interrupt) and any menu
    addEventListener("keydown", key, true);
    return () => removeEventListener("keydown", key, true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editing, images.length, onClose]);

  useEffect(() => {
    box.current?.focus();
  }, []);

  const copy = async () => {
    try {
      const blob = await (await fetch(cur.src)).blob();
      // the clipboard takes PNG everywhere; other types are converted first
      const png = blob.type === "image/png" ? blob : await toPNG(blob);
      await navigator.clipboard.write([new ClipboardItem({ "image/png": png })]);
      toast("已拷贝图片", "ok");
    } catch {
      toast("拷贝失败：浏览器不让写入图片", "error");
    }
  };

  const download = async () => {
    try {
      const blob = await (await fetch(cur.src)).blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `image-${index + 1}.${(blob.type.split("/")[1] || "png").replace("jpeg", "jpg")}`;
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch {
      toast("下载失败", "error");
    }
  };

  // the original in a new tab; browsers refuse to open a data: address at the top level, so that goes as a blob
  const openOriginal = async () => {
    if (!cur.src.startsWith("data:")) {
      window.open(cur.src, "_blank", "noopener");
      return;
    }
    const url = URL.createObjectURL(await (await fetch(cur.src)).blob());
    window.open(url, "_blank", "noopener");
    setTimeout(() => URL.revokeObjectURL(url), 60_000);
  };

  return createPortal(
    <div
      ref={box}
      role="dialog"
      aria-modal="true"
      aria-label={many ? `图片 ${index + 1} / ${images.length}` : "图片"}
      tabIndex={-1}
      className="fixed inset-0 z-50 flex flex-col bg-black/90 text-white outline-none backdrop-blur-sm"
      style={{ animation: "fade-in 150ms ease-out both" }}
    >
      <div className="flex h-14 shrink-0 items-center gap-2 px-3">
        <RoundButton icon="x" label="关闭（Esc）" onClick={onClose} />
        <div className="flex-1" />
        {many && (
          <span className="rounded-full bg-white/15 px-3 py-1 text-[12.5px] font-medium tabular-nums whitespace-nowrap" aria-live="polite">
            {index + 1} / {images.length}
          </span>
        )}
        <div className="flex-1" />
        <RoundButton icon="external" label="在新标签页打开" onClick={() => void openOriginal()} />
      </div>

      <div
        ref={area}
        className={cx("relative min-h-0 flex-1", zoomed ? "overflow-auto" : "flex items-center justify-center overflow-hidden px-4 sm:px-16")}
        onClick={(e) => e.target === e.currentTarget && onClose()}
        onPointerDown={(e) => {
          if (!zoomed && e.pointerType !== "mouse") swipe.current = { x: e.clientX, y: e.clientY };
        }}
        onPointerUp={(e) => {
          const s = swipe.current;
          swipe.current = null;
          if (!s) return;
          const dx = e.clientX - s.x,
            dy = e.clientY - s.y;
          if (Math.abs(dx) > 50 && Math.abs(dy) < 80) go(dx < 0 ? 1 : -1);
        }}
        onPointerCancel={() => (swipe.current = null)}
      >
        {failed[cur.id] ? (
          <span className="text-[13px] text-white/70">图片读不出来</span>
        ) : (
          <img
            key={cur.id}
            src={cur.src}
            alt=""
            draggable={false}
            onDoubleClick={() => setZoomed((z) => !z)}
            onLoad={(e) => setNatural({ width: e.currentTarget.naturalWidth, height: e.currentTarget.naturalHeight })}
            onError={() => setFailed((f) => ({ ...f, [cur.id]: true }))}
            title={zoomed ? "双击缩小" : "双击看原始大小"}
            className={cx("select-none", zoomed ? "max-w-none cursor-zoom-out" : "max-h-full max-w-full cursor-zoom-in rounded-[8px] object-contain shadow-overlay")}
            style={{ animation: `pop-in 220ms ${EASE} both`, touchAction: zoomed ? "auto" : "pan-y", ...(fitted && fitted.width > 0 ? { width: fitted.width, height: fitted.height } : {}) }}
          />
        )}
        {many && !zoomed && (
          <>
            <SideButton side="left" disabled={index === 0} onClick={() => go(-1)} />
            <SideButton side="right" disabled={index === images.length - 1} onClick={() => go(1)} />
          </>
        )}
      </div>

      <div className="flex shrink-0 items-center justify-center gap-2 px-4 pt-2 pb-[max(12px,env(safe-area-inset-bottom))]">
        {attach && (
          <BarButton icon="pencil" onClick={() => setEditing(true)} disabled={failed[cur.id]}>
            编辑
          </BarButton>
        )}
        {canCopyImage() && (
          <BarButton icon="copy" onClick={() => void copy()} disabled={failed[cur.id]}>
            拷贝
          </BarButton>
        )}
        <BarButton icon="download" onClick={() => void download()} disabled={failed[cur.id]}>
          下载
        </BarButton>
      </div>

      {editing && attach && (
        <ImageEditor
          src={cur.src}
          doneTitle="加到输入框"
          onCancel={() => setEditing(false)}
          onDone={(img) => {
            setEditing(false);
            if (attach(img)) onClose();
          }}
        />
      )}
    </div>,
    document.body,
  );
}

async function toPNG(blob: Blob): Promise<Blob> {
  const bmp = await createImageBitmap(blob);
  const c = document.createElement("canvas");
  c.width = bmp.width;
  c.height = bmp.height;
  c.getContext("2d")!.drawImage(bmp, 0, 0);
  return new Promise((ok, fail) => c.toBlob((b) => (b ? ok(b) : fail(new Error("png"))), "image/png"));
}

function RoundButton({ icon, label, onClick }: { icon: IconName; label: string; onClick: () => void }) {
  return (
    <button type="button" aria-label={label} title={label} onClick={onClick} className="flex size-9 items-center justify-center rounded-full bg-white/15 transition-colors hover:bg-white/25">
      <Icon name={icon} size={15} />
    </button>
  );
}

function SideButton({ side, disabled, onClick }: { side: "left" | "right"; disabled: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      aria-label={side === "left" ? "上一张（←）" : "下一张（→）"}
      title={side === "left" ? "上一张（←）" : "下一张（→）"}
      disabled={disabled}
      onClick={onClick}
      className={cx(
        "absolute top-1/2 hidden size-10 -translate-y-1/2 items-center justify-center rounded-full bg-white/15 transition-[background-color,opacity] hover:bg-white/25 disabled:pointer-events-none disabled:opacity-0 sm:flex",
        side === "left" ? "left-3" : "right-3",
      )}
    >
      <Icon name={side === "left" ? "chevronLeft" : "chevronRight"} size={18} stroke={2.2} />
    </button>
  );
}

function BarButton({ icon, onClick, disabled, children }: { icon: IconName; onClick: () => void; disabled?: boolean; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className="flex h-9 items-center gap-1.5 rounded-full bg-white/15 px-4 text-[13px] font-medium whitespace-nowrap transition-[background-color,transform] hover:bg-white/25 active:scale-[0.97] disabled:opacity-40"
    >
      <Icon name={icon} size={14} />
      {children}
    </button>
  );
}
