// Files a message carries as <jarvis-file> tags (docs/protocol.md「发送视频」; iOS draws the same cards), and files an
// answer names by path that exist on the machine: videos and audio play in place, other files open in the file viewer.
import { useMemo, useState } from "react";
import { usePathLinks } from "../../lib/path-presence";
import { bytes } from "../../lib/format";
import { openPanel } from "../../lib/panel";
import { formatDuration, mediaKindOf, replyPaths, type AttachedFile } from "../../lib/turns";
import { Icon } from "../../ui/icons";
import { MediaPlayer } from "../../ui/Media";

export function FileCards({ files, mid, align = "end" }: { files: AttachedFile[]; mid: string; align?: "start" | "end" }) {
  if (!files.length) return null;
  return (
    <div className={align === "end" ? "flex max-w-full flex-col items-end gap-1.5" : "flex max-w-full flex-col items-start gap-1.5"}>
      {files.map((f) => (
        <FileCard key={f.path} f={f} mid={mid} />
      ))}
    </div>
  );
}

function FileCard({ f, mid }: { f: AttachedFile; mid: string }) {
  // the length the file really has wins over the one the message says (it may be rounded, or wrong)
  const [real, setReal] = useState<number | undefined>(undefined);
  const duration = real ?? f.duration;
  const meta = [f.size ? bytes(f.size) : "", duration ? formatDuration(duration) : ""].filter(Boolean).join(" · ");
  if (f.kind === "video" || f.kind === "audio")
    return (
      <figure className="m-0 flex w-[min(360px,100%)] flex-col gap-1">
        <MediaPlayer mid={mid} path={f.path} kind={f.kind} className={f.kind === "video" ? "max-h-[420px] w-full" : undefined} compact onDuration={setReal} />
        <figcaption className="flex min-w-0 items-center gap-1.5 px-1 text-[11.5px] text-ink-3">
          <span className="min-w-0 truncate">{f.name}</span>
          {meta && <span className="shrink-0 tabular-nums">{meta}</span>}
        </figcaption>
      </figure>
    );
  return (
    <button
      type="button"
      onClick={() => openPanel({ kind: "file", mid, path: f.path })}
      className="flex max-w-[360px] min-w-0 items-center gap-2 rounded-[12px] bg-inset px-3 py-2 text-left shadow-hairline hover:bg-hover"
      title={f.path}
    >
      <Icon name={f.kind === "image" ? "image" : "read"} size={16} className="shrink-0 text-ink-3" />
      <span className="min-w-0 truncate text-[13px] text-ink">{f.name}</span>
      {meta && <span className="shrink-0 text-[11.5px] text-ink-3 tabular-nums">{meta}</span>}
    </button>
  );
}

/** Files an answer names by absolute path that are there on the machine, as cards under it (iOS links the same). */
export function ReplyFiles({ text, mid, cwd }: { text: string; mid: string; cwd?: string }) {
  const paths = useMemo(() => replyPaths(text), [text]);
  const { stat, key } = usePathLinks(mid, cwd, paths);
  const files = useMemo<AttachedFile[]>(
    () =>
      paths.flatMap((p) => {
        const s = stat(p);
        return s?.kind === "file" ? [{ kind: mediaKindOf(p), path: p, name: p.split("/").pop() || p, size: s.size }] : [];
      }),
    [paths, key], // eslint-disable-line react-hooks/exhaustive-deps
  );
  return files.length ? (
    <div className="mt-2">
      <FileCards files={files} mid={mid} align="start" />
    </div>
  ) : null;
}
