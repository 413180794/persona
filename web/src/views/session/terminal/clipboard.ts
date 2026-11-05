// The browser's side of the web terminal's clipboard (lib/terminal-clipboard.ts decides what to do and what to say).
// Pasting or dropping files: the request to the hub, which streams the file to the machine the pane is on and answers
// with its absolute path there; typing the paths into the pane is the feed's (feed.ts). Copying: the async clipboard
// where the page may use it, else a hidden textarea and execCommand("copy") (a page without HTTPS has no
// navigator.clipboard).
import { ApiError, errorText, isAbort } from "../../../lib/api";
import { sendFile } from "../../../lib/attachments";
import { MAX_TERMINAL_UPLOAD, uploadName, uploadTimeoutMs, type ClipboardEnv } from "../../../lib/terminal-clipboard";

/** Sends a file to the machine `mid` is (dest=terminal: kept in Persona's own directory there for a week); resolves to its path. */
export async function uploadToMachine(mid: string, file: File): Promise<string> {
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), uploadTimeoutMs(file.size));
  try {
    const path = await sendFile(mid, file, { name: uploadName(file), dest: "terminal", signal: ctl.signal });
    if (typeof path !== "string" || (!path.startsWith("/") && !/^[A-Za-z]:[\\/]/.test(path))) throw new Error("电脑没有返回文件路径");
    return path;
  } finally {
    clearTimeout(timer);
  }
}

/** Why an upload failed, in words for the toast. */
export function uploadFailure(name: string, e: unknown): string {
  if (isAbort(e)) return `上传「${name}」超时`;
  if (e instanceof ApiError && e.code === "too_large") return `「${name}」太大了（最多 ${MAX_TERMINAL_UPLOAD >> 20} MB）`;
  return `上传「${name}」失败：${errorText(e)}`;
}

/**
 * The files on the clipboard, where the browser lets a page look (the key bar's paste button; ⌘V / Ctrl+V come with
 * their files in the paste event). Empty when there are none, the browser cannot, or the user said no.
 */
export async function clipboardFiles(): Promise<File[]> {
  if (!navigator.clipboard?.read) return [];
  try {
    const out: File[] = [];
    for (const item of await navigator.clipboard.read()) {
      const type = item.types.find((t) => t.startsWith("image/"));
      if (!type) continue;
      const blob = await item.getType(type);
      out.push(new File([blob], "", { type }));
    }
    return out;
  } catch {
    return [];
  }
}

function legacyCopy(text: string): boolean {
  const active = document.activeElement as HTMLElement | null;
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.setAttribute("readonly", "");
  ta.setAttribute("aria-hidden", "true");
  ta.style.cssText = "position:fixed;top:0;left:0;width:1px;height:1px;opacity:0;pointer-events:none";
  document.body.appendChild(ta);
  try {
    ta.select();
    ta.setSelectionRange(0, text.length);
    return document.execCommand("copy");
  } finally {
    ta.remove();
    active?.focus?.(); // the terminal keeps the keyboard
  }
}

export function browserClipboard(): ClipboardEnv {
  const usable = window.isSecureContext && typeof navigator.clipboard?.writeText === "function";
  return { writeText: usable ? (text) => navigator.clipboard.writeText(text) : undefined, legacyCopy };
}
