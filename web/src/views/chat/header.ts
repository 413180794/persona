// The page header's side slots around a switch in its middle (the shell's 助手 | Agents, narrow screens): one fixed
// width each, the left's buttons at its start, the right's at its end, so two pages with the same switch have their
// buttons in the same places (iOS's new shell). 80px fits the widest right side a page with the switch has (the main
// conversation's ⋯; it has no 同空间 or activity chips). Kept apart from ChatView so another page reads it cheaply.

/** A slot's box: `HEADER_SLOT` plus `HEADER_SLOT_START` or `HEADER_SLOT_END`. */
export const HEADER_SLOT = "flex shrink-0 items-center gap-2 max-lg:w-20";
export const HEADER_SLOT_START = "justify-start";
export const HEADER_SLOT_END = "justify-end";
