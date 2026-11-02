// What the chat hands down to its rows without threading props through every step: the background tasks and their
// calls (to badge a call that went to the background and name a notice's call), and the subagents (to open one from
// the Agent call that launched it).
import { createContext, useContext } from "react";
import { emptyTaskIndex, type TaskIndex } from "../../lib/tasks";
import type { Subagent } from "../../lib/types";

export interface ChatContextValue {
  tasks: TaskIndex;
  /** The session's subagents, by the tool_use_id of the call that launched them. */
  subagents: Subagent[];
  /** Shows a subagent's conversation in the chat (read-only); undefined where there is no chat to switch. */
  openSubagent?: (a: Subagent) => void;
}

export const ChatContext = createContext<ChatContextValue>({ tasks: emptyTaskIndex(), subagents: [] });

export function useChatContext() {
  return useContext(ChatContext);
}
