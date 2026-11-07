// Files pasted into or dropped on a terminal (docs/integrations/terminal.md "Pasting images / files", item 26): uploaded to the computer the
// pane is on, their escaped paths typed in as one paste (Claude Code and Codex take a path to an image as the image), and
// a Files panel entry dropped on it typed as `@path `. Both surfaces use this: the single pane's feed (feed.ts) and the
// full herdr surface (full.ts). What differs is only how a paste reaches the program and what the status line says.
import { dragHasEntry, dragIsDroppable, droppedEntry, terminalEntryDrop } from "../../../lib/entry-drag";
import { pastedPaths, pasteVerdict, refuseUpload, transferFiles, uploadName, uploadingText } from "../../../lib/terminal-clipboard";
import { toast } from "../../../ui/atoms";
import { uploadFailure, uploadToMachine } from "./clipboard";

/** The terminal a transfer is for. */
export interface TransferHost {
  /** The computer the files go to. */
  readonly mid: string;
  /** The folder a Files panel entry is named relative to; "" names it by its absolute path. */
  readonly cwd: string;
  /** The keys go in ("Take control" active, or the herdr client live). */
  readonly driving: boolean;
  /** The surface is gone (another session opened, the panel closed). */
  readonly disposed: boolean;
  /** The upload status line; null when nothing is uploading. */
  setUploading(text: string | null): void;
  /** A drag is over the terminal. */
  setDropping(on: boolean): void;
  /** Types text into the program as a paste (bracketed when it asked for that) and gives the terminal the keyboard. */
  paste(text: string): void;
}

/** What is said when the terminal is not in a state to be pasted into (the surfaces word "driving" differently). */
export interface TransferWords {
  notDriving: string;
  changed: string;
  released: string;
}

export class FileTransfer {
  private uploads: string[] = [];
  private dragDepth = 0;

  constructor(
    private readonly host: TransferHost,
    private readonly words: TransferWords,
  ) {}

  /**
   * Sends the files to the computer the pane is on, then types their escaped paths into it as one paste. Only while the
   * terminal is driven — before, and again when the uploads end (taken over, "View only", disconnected or another terminal
   * meanwhile: nothing is pasted).
   */
  async uploadFiles(files: File[]) {
    const host = this.host;
    if (!host.driving) return void toast(this.words.notDriving);
    const sending = files.filter((f) => {
      const why = refuseUpload(f);
      if (why) toast(why, "error");
      return !why;
    });
    if (!sending.length) return;
    const names = sending.map(uploadName);
    this.uploads.push(...names);
    host.setUploading(uploadingText(this.uploads));
    const results = await Promise.allSettled(sending.map((f) => uploadToMachine(host.mid, f)));
    for (const n of names) this.uploads.splice(this.uploads.indexOf(n), 1);
    host.setUploading(this.uploads.length ? uploadingText(this.uploads) : null);
    const paths: string[] = [];
    results.forEach((r, i) => {
      if (r.status === "fulfilled") paths.push(r.value);
      else if (!host.disposed) toast(uploadFailure(names[i], r.reason), "error");
    });
    if (!paths.length) return;
    const verdict = pasteVerdict({ disposed: host.disposed, driving: host.driving });
    if (verdict === "changed") return void toast(this.words.changed, "error");
    if (verdict === "watching") return void toast(this.words.released, "error");
    host.paste(pastedPaths(paths));
  }

  dragEnter = (e: DragEvent) => {
    if (!dragIsDroppable(e.dataTransfer)) return;
    this.dragDepth++;
    if (this.host.driving) this.host.setDropping(true);
  };

  dragOver = (e: DragEvent) => {
    if (!dragIsDroppable(e.dataTransfer)) return;
    e.preventDefault(); // without it the browser does not offer the drop (and opens the file instead)
    // a Files panel entry is accepted while watching too: the drop says why nothing was typed
    if (e.dataTransfer) e.dataTransfer.dropEffect = this.host.driving || dragHasEntry(e.dataTransfer) ? "copy" : "none";
  };

  dragLeave = (e: DragEvent) => {
    if (!dragIsDroppable(e.dataTransfer)) return;
    this.dragDepth = Math.max(0, this.dragDepth - 1);
    if (!this.dragDepth) this.host.setDropping(false);
  };

  drop = (e: DragEvent) => {
    const files = transferFiles(e.dataTransfer);
    this.dragDepth = 0;
    this.host.setDropping(false);
    if (this.dropEntry(e)) return;
    if (!files.length) return;
    e.preventDefault();
    e.stopPropagation();
    void this.uploadFiles(files);
  };

  /** A Files panel entry dropped on the terminal: its `@path ` is typed in, nothing is uploaded. */
  private dropEntry(e: DragEvent): boolean {
    const entry = droppedEntry(e.dataTransfer);
    if (!entry) return false;
    e.preventDefault();
    e.stopPropagation();
    const { mid, cwd, driving } = this.host;
    const drop = terminalEntryDrop(entry, { mid, cwd, driving });
    if (drop.action === "refuse") toast(driving ? drop.toast : this.words.notDriving);
    else this.host.paste(drop.text);
    return true;
  }
}
