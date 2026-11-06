// Documents an answer names (iOS DocumentCards / FileThumbnails.isDocument): Word, Excel, PowerPoint, iWork, RTF and
// PDF, the files that have a first page to show. The chat draws them as cards with that page (views/chat/FileCards.tsx).
import { t } from "../i18n";

const KINDS: Record<string, string> = {
  docx: "word",
  doc: "word",
  xlsx: "excel",
  xls: "excel",
  pptx: "powerpoint",
  ppt: "powerpoint",
  pages: "pages",
  numbers: "numbers",
  key: "keynote",
  rtf: "rtf",
  pdf: "pdf",
};

function ext(path: string): string {
  const name = path.split("/").pop() ?? "";
  const dot = name.lastIndexOf(".");
  return dot > 0 ? name.slice(dot + 1).toLowerCase() : "";
}

/** A page of a document (not a picture, a video or code): it gets a card with its first page. */
export function isDocument(path: string): boolean {
  return ext(path) in KINDS;
}

/** What a document is, as the Files app says it (Word document, PDF document…). */
export function documentKind(path: string): string {
  const k = KINDS[ext(path)];
  return t(k ? `lib-documents.${k}` : "lib-documents.other");
}

/** At most this many document cards under one answer (iOS takes 6 too). */
export const MAX_DOCUMENT_CARDS = 6;

/**
 * Below this mean lightness a first page is a dark page, not the document: Quick Look on a Mac in dark mode draws RTF
 * and text documents white on black. The card shows the file's glyph instead (QuickLookFile.swift `lightness`, 0.35).
 */
export const DARK_PAGE = 0.35;

/** The mean lightness (0 black … 1 white) of RGBA pixels, each laid over white first as iOS does. */
export function lightness(rgba: ArrayLike<number>): number {
  let sum = 0;
  const n = Math.floor(rgba.length / 4);
  if (!n) return 1;
  for (let i = 0; i < n; i++) {
    const a = rgba[i * 4 + 3] / 255;
    const gray = 0.299 * rgba[i * 4] + 0.587 * rgba[i * 4 + 1] + 0.114 * rgba[i * 4 + 2];
    sum += gray * a + 255 * (1 - a);
  }
  return sum / (n * 255);
}

/** A loaded first page that is a dark page (see DARK_PAGE); false when it cannot be read back (no canvas). */
export function isDarkPage(img: HTMLImageElement): boolean {
  const side = 8;
  try {
    const canvas = document.createElement("canvas");
    canvas.width = side;
    canvas.height = side;
    const ctx = canvas.getContext("2d");
    if (!ctx) return false;
    ctx.fillStyle = "#fff";
    ctx.fillRect(0, 0, side, side);
    ctx.drawImage(img, 0, 0, side, side);
    return lightness(ctx.getImageData(0, 0, side, side).data) < DARK_PAGE;
  } catch {
    return false; // a tainted canvas or no 2D context: keep the picture
  }
}
