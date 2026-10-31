Search the user's Feishu: messages, docs, people, chats, calendar events, meeting minutes, tasks, mail.

```json
{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {
      "type": "string"
    },
    "kind": {
      "type": "string",
      "enum": ["messages", "docs", "people", "chats", "calendar", "minutes", "todos", "mail"],
      "description": "default messages"
    }
  }
}
```
