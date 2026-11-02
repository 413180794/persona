import { uuid } from "./format";

export const MAX_IMAGE = 8 * 1024 * 1024;
export const MAX_FILE = 200 * 1024 * 1024;

export interface LocalAttachment {
  id: string;
  name: string;
  mime: string;
  file: File;
  url?: string;
}

export function pickAttachment(file: File): LocalAttachment {
  const image = file.type.startsWith("image/");
  if (file.size > (image ? MAX_IMAGE : MAX_FILE)) {
    throw new Error(`${file.name} 太大了（${image ? "图片最多 8 MB" : "文件最多 200 MB"}）`);
  }
  return { id: uuid(), name: file.name || "image.png", mime: file.type || "application/octet-stream", file, url: image ? URL.createObjectURL(file) : undefined };
}

export async function imagePayload(file: File): Promise<{ mime: string; data: string }> {
  const bytes = new Uint8Array(await file.arrayBuffer());
  let bin = "";
  for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return { mime: file.type || "image/png", data: btoa(bin) };
}

export function quotePath(path: string) {
  return /[\s'"\\$`]/.test(path) ? `'${path.replace(/'/g, `'\\''`)}'` : path;
}

export function releaseAttachments(attachments: { url?: string }[]) {
  attachments.forEach((a) => { if (a.url) URL.revokeObjectURL(a.url); });
}
