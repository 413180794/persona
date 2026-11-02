import { cx, EASE } from "./atoms";
import { Icon } from "./icons";

export function AttachmentTray({ attachments, onRemove, onOpen }: {
  attachments: { id: string; name: string; url?: string }[];
  onRemove?: (id: string) => void;
  /** Click a picture to open it (the composer marks it up before sending). */
  onOpen?: (id: string) => void;
}) {
  if (!attachments.length) return null;
  return (
    <div className="flex flex-wrap gap-1.5">
      {attachments.map((a) => (
        <span key={a.id} title={a.name} className={cx("group/att relative overflow-hidden rounded-[8px] bg-field shadow-hairline", a.url ? "size-14" : "flex h-14 max-w-full items-center gap-2 px-3 pr-7")} style={{ animation: `pop-in 200ms ${EASE} both` }}>
          {a.url ? (
            onOpen ? (
              <button type="button" aria-label={`编辑 ${a.name}`} title="点一下标注后再发" onClick={() => onOpen(a.id)} className="block size-full cursor-pointer">
                <img src={a.url} alt={a.name} className="size-full object-cover" />
              </button>
            ) : (
              <img src={a.url} alt={a.name} className="size-full object-cover" />
            )
          ) : <><Icon name="read" size={16} className="shrink-0 text-ink-3" /><span className="min-w-0 truncate text-[12px] text-ink-2">{a.name}</span></>}
          {onRemove && <button type="button" aria-label={`移除 ${a.name}`} onClick={() => onRemove(a.id)} className="absolute top-0.5 right-0.5 flex size-4.5 items-center justify-center rounded-full bg-black/60 text-white opacity-0 transition-opacity group-hover/att:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100">
            <Icon name="x" size={10} stroke={2.6} />
          </button>}
        </span>
      ))}
    </div>
  );
}
