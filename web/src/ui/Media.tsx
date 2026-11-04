// Video and audio from a machine (docs/web.md「媒体」, #1422): played straight from file/download, which the browser
// reads by Range, so seeking never waits for the whole file. A codec this browser lacks (HEVC outside Safari, most
// .mkv) says so and offers the download instead of a black box.
import { useState } from "react";
import { fileDownloadURL } from "../lib/api";
import { cx } from "./atoms";
import { Icon } from "./icons";

export type MediaKind = "video" | "audio";

export function MediaPlayer({
  mid,
  path,
  cwd,
  kind,
  className,
  compact,
  onDuration,
}: {
  mid: string;
  path: string;
  cwd?: string;
  kind: MediaKind;
  className?: string;
  compact?: boolean;
  /** The file's real length once the browser has read its header (seconds). */
  onDuration?: (seconds: number) => void;
}) {
  const [failed, setFailed] = useState(false);
  const src = fileDownloadURL(mid, path, cwd, { inline: true });
  const reportDuration = (d: number) => {
    if (Number.isFinite(d) && d > 0) onDuration?.(d);
  };
  if (failed)
    return (
      <div className={cx("flex flex-col items-center justify-center gap-2 rounded-[10px] bg-inset px-4 py-6 text-center", className)}>
        <Icon name="info" size={18} className="text-ink-3" />
        <div className="text-[13px] text-ink">这个浏览器放不了这个{kind === "video" ? "视频" : "音频"}</div>
        <div className="max-w-72 text-[12px] leading-relaxed text-ink-3">{kind === "video" ? "可能是 HEVC 或 MKV 这类编码，Safari 或下载到本机再看。" : "这种编码浏览器不支持，下载到本机再听。"}</div>
        <a href={fileDownloadURL(mid, path, cwd)} download className="mt-1 inline-flex h-7 items-center gap-1 rounded-[8px] bg-surface px-2.5 text-[12px] font-medium whitespace-nowrap text-ink shadow-btn hover:bg-hover">
          <Icon name="arrowDown" size={13} />
          下载
        </a>
      </div>
    );
  return kind === "video" ? (
    <video src={src} controls preload="metadata" playsInline onError={() => setFailed(true)} onLoadedMetadata={(e) => reportDuration(e.currentTarget.duration)} className={cx("block rounded-[10px] bg-black", className)} />
  ) : (
    <audio src={src} controls preload="metadata" onError={() => setFailed(true)} onLoadedMetadata={(e) => reportDuration(e.currentTarget.duration)} className={cx(compact ? "h-9 w-full" : "w-full max-w-[560px]", className)} />
  );
}
