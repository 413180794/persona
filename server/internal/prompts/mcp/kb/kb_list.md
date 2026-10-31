List knowledge-base entities of one kind ranked by importance (P0 first), relevance to the user, recency: projects (short = a feature / launch, long = a team mission), chats, people, docs, meetings, events. Optional tag filter.

```json
{
  "type": "object",
  "required": ["kind"],
  "properties": {
    "kind": {
      "type": "string",
      "enum": ["projects", "chats", "people", "docs", "meetings", "events"]
    },
    "tag": {
      "type": "string"
    },
    "limit": {
      "type": "integer",
      "description": "default 30"
    }
  }
}
```
