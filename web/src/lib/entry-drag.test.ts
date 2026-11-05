import { describe, expect, it } from "vitest";
import { composerEntryDrop, dragHasEntry, dragIsDroppable, droppedEntry, ENTRY_DRAG_TYPE, entryPath, entryPayload, parseEntry, terminalEntryDrop } from "./entry-drag";

const file = (path: string, mid = "m1") => ({ mid, path, dir: false });
const dir = (path: string, mid = "m1") => ({ mid, path, dir: true });

describe("payload", () => {
  it("round-trips and refuses anything else", () => {
    const e = dir("/home/me/proj/src");
    expect(parseEntry(entryPayload(e))).toEqual(e);
    expect(parseEntry("")).toBeNull();
    expect(parseEntry("not json")).toBeNull();
    expect(parseEntry("[1]")).toBeNull();
    expect(parseEntry(JSON.stringify({ mid: "m1" }))).toBeNull();
    expect(parseEntry(JSON.stringify({ mid: "", path: "/a" }))).toBeNull();
    expect(parseEntry(JSON.stringify({ mid: "m1", path: "/a", dir: "yes" }))).toEqual({ mid: "m1", path: "/a", dir: false });
  });

  it("is recognised by its type while dragging and read on the drop", () => {
    expect(dragHasEntry({ types: ["text/plain", ENTRY_DRAG_TYPE] })).toBe(true);
    expect(dragHasEntry({ types: ["Files"] })).toBe(false);
    expect(dragHasEntry(null)).toBe(false);
    expect(dragIsDroppable({ types: ["Files"] })).toBe(true);
    expect(dragIsDroppable({ types: [ENTRY_DRAG_TYPE] })).toBe(true);
    expect(dragIsDroppable({ types: ["text/plain"] })).toBe(false);
    const data = { getData: (t: string) => (t === ENTRY_DRAG_TYPE ? entryPayload(file("/a/b")) : "") };
    expect(droppedEntry(data)).toEqual(file("/a/b"));
    expect(droppedEntry({ getData: () => "" })).toBeNull();
    expect(droppedEntry(null)).toBeNull();
  });
});

describe("entryPath", () => {
  it("is relative inside the folder", () => {
    expect(entryPath(file("/home/me/proj/src/a.ts"), "/home/me/proj")).toBe("src/a.ts");
    expect(entryPath(file("/home/me/proj/a.ts"), "/home/me/proj/")).toBe("a.ts");
  });

  it("ends a folder in a slash, once, whatever the input had", () => {
    expect(entryPath(dir("/home/me/proj/src"), "/home/me/proj")).toBe("src/");
    expect(entryPath(dir("/home/me/proj/src/"), "/home/me/proj")).toBe("src/");
    expect(entryPath(dir("/home/me/proj/src//"), "/home/me/proj")).toBe("src/");
  });

  it("names the folder itself as .", () => {
    expect(entryPath(dir("/home/me/proj"), "/home/me/proj")).toBe("./");
    expect(entryPath(dir("/home/me/proj/"), "/home/me/proj")).toBe("./");
  });

  it("is absolute outside the folder or without one", () => {
    expect(entryPath(file("/home/me/other/a.ts"), "/home/me/proj")).toBe("/home/me/other/a.ts");
    expect(entryPath(file("/home/me/project/a.ts"), "/home/me/proj")).toBe("/home/me/project/a.ts"); // a sibling with the same prefix
    expect(entryPath(file("/home/me/a.ts"), "/home/me/proj")).toBe("/home/me/a.ts"); // above it
    expect(entryPath(dir("/home/me/other/"), "/home/me/proj")).toBe("/home/me/other/");
    expect(entryPath(file("/home/me/proj/a.ts"), "")).toBe("/home/me/proj/a.ts");
  });

  it("copes with the root", () => {
    expect(entryPath(dir("/"), "/home/me")).toBe("/");
    expect(entryPath(file("/etc/hosts"), "/")).toBe("etc/hosts");
  });
});

describe("terminalEntryDrop", () => {
  const driving = { mid: "m1", cwd: "/home/me/proj", driving: true };

  it("types @path and a space", () => {
    expect(terminalEntryDrop(file("/home/me/proj/src/a.ts"), driving)).toEqual({ action: "paste", text: "@src/a.ts " });
    expect(terminalEntryDrop(dir("/home/me/proj/src"), driving)).toEqual({ action: "paste", text: "@src/ " });
  });

  it("escapes the path as a shell word", () => {
    expect(terminalEntryDrop(file("/home/me/proj/my notes (1).md"), driving)).toEqual({ action: "paste", text: "@my\\ notes\\ \\(1\\).md " });
    expect(terminalEntryDrop(file("/home/me/proj/a&b;c$d'e.txt"), driving)).toEqual({ action: "paste", text: "@a\\&b\\;c\\$d\\'e.txt " });
    expect(terminalEntryDrop(file("/home/me/proj/文档/说明.md"), driving)).toEqual({ action: "paste", text: "@文档/说明.md " });
  });

  it("uses the absolute path outside the folder", () => {
    expect(terminalEntryDrop(file("/tmp/log file.txt"), driving)).toEqual({ action: "paste", text: "@/tmp/log\\ file.txt " });
  });

  it("does nothing but say so when not driving", () => {
    expect(terminalEntryDrop(file("/home/me/proj/a.ts"), { ...driving, driving: false })).toEqual({ action: "refuse", toast: "接管终端后才能粘贴文件" });
  });

  it("does nothing but say so for a file of another machine", () => {
    const r = terminalEntryDrop(file("/home/me/proj/a.ts", "m2"), driving);
    expect(r.action).toBe("refuse");
  });
});

describe("composerEntryDrop", () => {
  it("types @path relative to the session's folder, unescaped as the @ menu does", () => {
    expect(composerEntryDrop(file("/home/me/proj/a b.ts"), { mid: "m1", cwd: "/home/me/proj" })).toEqual({ action: "paste", text: "@a b.ts " });
    expect(composerEntryDrop(dir("/home/me/proj/src"), { mid: "m1", cwd: "/home/me/proj" })).toEqual({ action: "paste", text: "@src/ " });
    expect(composerEntryDrop(file("/tmp/x.txt"), { mid: "m1", cwd: "/home/me/proj" })).toEqual({ action: "paste", text: "@/tmp/x.txt " });
    expect(composerEntryDrop(file("/tmp/x.txt"), { mid: "m1" })).toEqual({ action: "paste", text: "@/tmp/x.txt " });
  });

  it("refuses another machine's file", () => {
    expect(composerEntryDrop(file("/a", "m2"), { mid: "m1", cwd: "/" }).action).toBe("refuse");
    expect(composerEntryDrop(file("/a"), {}).action).toBe("refuse");
  });
});
