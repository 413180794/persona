Read a Feishu chat (id from lark_chats or lark_search): the latest messages as text, oldest first. Pass `before` (a message position from an earlier page) to read further back. Reading does not mark anything as read.

```json
{
  "type": "object",
  "required": ["chat_id"],
  "properties": {
    "chat_id": {
      "type": "string"
    },
    "limit": {
      "type": "integer",
      "description": "default 40, max 100"
    },
    "before": {
      "type": "string"
    }
  }
}
```
