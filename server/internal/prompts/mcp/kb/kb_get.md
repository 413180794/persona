Read one entity: its metadata (links to people / chats / projects, tags), compressed context (summary.md), a project's timeline, a chat's messages of the last 7 days. id from kb_list / kb_search (a project also by name).

```json
{
  "type": "object",
  "required": ["kind", "id"],
  "properties": {
    "kind": {
      "type": "string",
      "enum": ["projects", "chats", "people", "docs", "meetings", "events"]
    },
    "id": {
      "type": "string"
    }
  }
}
```
