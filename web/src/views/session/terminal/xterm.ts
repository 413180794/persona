// xterm and its fit, clipboard, link and search addons, loaded only when a terminal opens (they are most of a terminal's weight).
import "@xterm/xterm/css/xterm.css";
import "./terminal.css";

export { Terminal } from "@xterm/xterm";
export { FitAddon } from "@xterm/addon-fit";
export { ClipboardAddon } from "@xterm/addon-clipboard";
export { WebLinksAddon } from "@xterm/addon-web-links";
export { SearchAddon } from "@xterm/addon-search";
export { matchHerdrWidths } from "./widths";
