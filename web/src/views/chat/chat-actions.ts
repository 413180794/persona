// What the chat's message parts need from the chat around them: where quotes, edits and edited images go (the
// composer's draft key), whether it takes them now, and the conversation's images for the viewer to page through.
import { createContext, useContext } from "react";
import type { Item } from "../../lib/types";

export interface ChatActions {
  draftKey: string;
  /** The composer can send here (a read-only source cannot): 引用 / 编辑 / 编辑图片 are offered only then. */
  canCompose: boolean;
  /** Who the replies are from, for 引用's 「引用 Persona · 16:45」 (the assistant's name, or the agent). */
  speaker: string;
  /** Every image of the loaded conversation, oldest first. A function: read when the viewer opens, not on every push. */
  images: () => Item[];
  /** 分支到新聊天 from a reply (its item id): set only when the session can be forked. */
  fork?: (itemId: string) => void;
}

export const ChatActionsContext = createContext<ChatActions | null>(null);

export function useChatActions(): ChatActions | null {
  return useContext(ChatActionsContext);
}
