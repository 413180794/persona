Search the knowledge base (~/.jarvis/data): summaries and metadata of chats, people, projects, docs, meetings, events, project timelines, the notes directories the user added (hits with an absolute path) and, with messages=true, the full message archive (newest days first). All words must match.

```json
{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {
      "type": "string"
    },
    "messages": {
      "type": "boolean",
      "description": "also search the message archive (default true)"
    },
    "limit": {
      "type": "integer",
      "description": "default 40"
    }
  }
}
```
