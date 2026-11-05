List knowledge-base entities of one kind ranked by importance (P0 first), relevance to the user, recency: projects (short = a feature / launch, long = a team mission), chats, people, docs, meetings, events, recordings (transcripts of the user's phone / watch recordings, kept a few days). Optional tag filter.

```json
{
  "type": "object",
  "required": ["kind"],
  "properties": {
    "kind": {
      "type": "string",
      "enum": ["projects", "chats", "people", "docs", "meetings", "events", "recordings"]
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
