// What the chat's message parts need from the chat around them: where quotes, edits and edited images go (the
// composer's draft key), whether it takes them now, and the conversation's images for the viewer to page through.
import { createContext, useContext } from "react";
import type { RewindRestore } from "../../lib/rewind";
import type { Item } from "../../lib/types";

export interface ChatActions {
  draftKey: string;
  /** The conversation title for message context cards, separate from the reply's speaker. */
  title: string;
  /** Agent chats can hand messages to Persona, including when this chat is read-only. */
  isAgent: boolean;
  /** The composer can send here: "Quote" / "Use as context" / "Edit" / "Edit image" are offered only then. */
  canCompose: boolean;
  /** Who the replies are from, for "Quote"'s "Quote Persona · 16:45" (the assistant's name, or the agent). */
  speaker: string;
  /** Every image of the loaded conversation, oldest first. A function: read when the viewer opens, not on every push. */
  images: () => Item[];
  /** "Branch to new chat" from a reply (its item id): set only when the session can be forked. */
  fork?: (itemId: string) => void;
  /** "Rewind to here" (#2436) on a message of yours: set for a session in a terminal that takes messages. `block` is why it is
   *  off for now ("" = on: Claude only, working…); `run` goes back to before the message (its UserMessage.id) and puts it
   *  in the composer, rejecting with the reason in the message. */
  rewind?: { block: string; run: (messageId: string, text: string, restore: RewindRestore) => Promise<void> };
}

export const ChatActionsContext = createContext<ChatActions | null>(null);

export function useChatActions(): ChatActions | null {
  return useContext(ChatActionsContext);
}
