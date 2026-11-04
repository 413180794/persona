Read a Feishu chat (id from lark_chats or lark_search): the latest messages as text, oldest first. Pass `before` (a message position from an earlier page) to read further back. In a topic group (话题群) each line is one topic's first message with its id and reply count; pass `thread` (that id) to read the topic's replies. Reading does not mark anything as read.

```json
{
  "type": "object",
  "required": ["chat_id"],
  "properties": {
    "chat_id": {
      "type": "string"
    },
    "thread": {
      "type": "string",
      "description": "a topic's message id (from a topic group's page): read the replies in that topic instead of the chat"
    },
    "limit": {
      "type": "integer",
      "description": "default 40, max 100"
    },
    "before": {
      "type": "string"
    },
    "account": {
      "type": "integer",
      "description": "the Feishu account (lark-connector account number) the chat is in, when an inbox item names one; omit for the default account"
    }
  }
}
```
