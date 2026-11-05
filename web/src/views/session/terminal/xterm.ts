// xterm and its fit and clipboard addons, loaded only when a terminal opens (they are most of a terminal's weight).
import "@xterm/xterm/css/xterm.css";
import "./terminal.css";

export { Terminal } from "@xterm/xterm";
export { FitAddon } from "@xterm/addon-fit";
export { ClipboardAddon } from "@xterm/addon-clipboard";
