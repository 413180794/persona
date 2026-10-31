The user's Feishu (Lark) chats from their own account: unread ones by default, or the ones that @mention them, or the latest. Each line has the chat id (for lark_messages; the lark-connector skill calls it feedId), name, unread count, @me, muted, last message preview and time.

```json
{
  "type": "object",
  "properties": {
    "filter": {
      "type": "string",
      "enum": ["unread", "at_me", "recent"],
      "description": "default unread"
    },
    "limit": {
      "type": "integer",
      "description": "default 30"
    }
  }
}
```
