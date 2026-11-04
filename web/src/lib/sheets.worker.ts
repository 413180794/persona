// Parses a table file off the main thread (a big xlsx takes seconds): delimited text or a workbook's bytes in,
// sheets of display text out (lib/sheets.ts).
import * as XLSX from "xlsx";
import { delimitedSheet, workbookSheets, type Sheet } from "./sheets";

export type SheetRequest = { name: string; text: string } | { name: string; bytes: ArrayBuffer };
export type SheetReply = { sheets: Sheet[] } | { error: string };

self.onmessage = (e: MessageEvent<SheetRequest>) => {
  const req = e.data;
  let reply: SheetReply;
  try {
    if ("text" in req) reply = { sheets: [delimitedSheet(req.name, req.text)] };
    else {
      // dense: rows as arrays (far less memory than a key per cell)
      const wb = XLSX.read(new Uint8Array(req.bytes), { type: "array", dense: true, cellDates: true, cellStyles: true }); // cellStyles: column widths
      reply = { sheets: workbookSheets(XLSX, wb) };
    }
  } catch (err) {
    reply = { error: err instanceof Error ? err.message : String(err) };
  }
  (self as unknown as Worker).postMessage(reply);
};
