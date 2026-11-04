// What the chat hands down to its rows without threading props through every step: the background tasks and their
// calls (to badge a call that went to the background and name a notice's call), the subagents (their cards), the
// items that came in live (their words fade in), and the key the open / closed state is kept under.
import { createContext, useContext } from "react";
import { emptyTaskIndex, type TaskIndex } from "../../lib/tasks";
import type { Subagent } from "../../lib/types";

export interface ChatContextValue {
  tasks: TaskIndex;
  /** The session's subagents, by the tool_use_id of the call that launched them. */
  subagents: Subagent[];
  /** Items pushed over the WebSocket after the chat subscribed (not the first page, older pages or an unfolded
   *  range): their words fade in once (Markdown's `fade`). */
  fresh?: ReadonlySet<string>;
  /** Prefixes the keys of what the reader opened (expanded.ts): one chat's groups do not open another's. */
  scope: string;
}

export const ChatContext = createContext<ChatContextValue>({ tasks: emptyTaskIndex(), subagents: [], scope: "" });

export function useChatContext() {
  return useContext(ChatContext);
}
