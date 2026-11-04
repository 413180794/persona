// An agent's words are Markdown. Places that show them as they are (a one-line preview, a two-line clamp) take the
// text without its markup: no #, list markers, quote marks, ** / __ / ~~ / `, a link as its text. Marks a clip left
// unpaired (the session list's last_text is cut at 200 characters) are dropped too. Code keeps its words.

const CODE_SPAN = /(`+)([\s\S]*?[^`])\1(?!`)/g;
const HELD = /(\d+)/g;

function plainLine(line: string): string {
  const held: string[] = [];
  const hold = (s: string) => `${held.push(s) - 1}`;
  return line
    .replace(CODE_SPAN, (_, __, code: string) => hold(code.trim()))
    .replace(/<(https?:\/\/[^\s<>]+)>/g, (_, url: string) => hold(url))
    .replace(/!?\[([^\]\n]*)\]\([^)\n]*\)/g, "$1")
    .replace(/\[([^\]\n]+)\]\[[^\]\n]*\]/g, "$1")
    .replace(/(\*\*|__)(?=\S)([^\n]*?\S)\1/g, "$2")
    .replace(/~~(?=\S)([^\n]*?\S)~~/g, "$1")
    .replace(/(^|[^\w*])\*(?=\S)([^*\n]*?\S)\*(?![\w*])/g, "$1$2")
    .replace(/(^|[^\w])_(?=\S)([^_\n]*?\S)_(?!\w)/g, "$1$2")
    .replace(/\*{2,}|_{2,}|~{2,}|`+/g, "")
    .replace(/<\/?[a-zA-Z][^<>]*>/g, "")
    .replace(/\\([!-/:-@[-`{-~])/g, "$1")
    .replace(HELD, (_, n: string) => held[+n]);
}

/** md as one line of plain text. */
export function plainMarkdown(md: string | undefined): string {
  const lines: string[] = [];
  for (let line of (md ?? "").split("\n")) {
    if (/^\s*(?:`{3,}|~{3,})/.test(line)) continue; // a fence: its code stays
    if (/^\s*(?:[-*_]\s*){3,}$/.test(line)) continue; // a rule
    if (/^\s*\|?\s*:?-+:?\s*(?:\|\s*:?-+:?\s*)*\|?\s*$/.test(line) && line.includes("-") && line.includes("|")) continue; // a table's delimiter row
    line = line
      .replace(/^(?:\s*>)+\s?/, "")
      .replace(/^\s*#{1,6}\s+/, "")
      .replace(/\s+#+\s*$/, "")
      .replace(/^\s*(?:[-*+]|\d{1,9}[.)])\s+(?:\[[ xX]\]\s+)?/, "");
    if (/^\s*\|/.test(line)) line = line.replace(/^\s*\|/, "").replace(/\|\s*$/, "").replace(/\s*\|\s*/g, " · ");
    line = plainLine(line).replace(/\s+/g, " ").trim();
    if (line) lines.push(line);
  }
  return lines.join(" ");
}
