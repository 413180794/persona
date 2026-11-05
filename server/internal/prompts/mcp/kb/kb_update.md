Write to the knowledge base: create or change a project (name, project_kind short|long, status, goal, my_role, aliases, link chats / people, code dirs as "<machine>:<path>" so Agents there belong to it, append a timeline `log` line, replace its summary), or tag / annotate a chat, person, doc, meeting, event, recording (a recording's content is the phone's: only its tags, pinned and projects; importance P0–P3, relevance 0–100, tags, note, link projects). Tags set here are kept over the automatic ones. Use it whenever a conversation settles something about a project or person. Two projects that are the same work under two names: `merge_into`.

```json
{
  "type": "object",
  "required": ["kind", "id"],
  "properties": {
    "kind": {
      "type": "string",
      "enum": ["projects", "chats", "people", "docs", "meetings", "events", "recordings"]
    },
    "id": {
      "type": "string",
      "description": "id, or a project's name (created when new)"
    },
    "merge_into": {
      "type": "string",
      "description": "projects only: fold this project into that one (slug or name) when they are the same work; nothing else is changed in the same call"
    },
    "name": {
      "type": "string"
    },
    "project_kind": {
      "type": "string",
      "enum": ["short", "long"]
    },
    "status": {
      "type": "string",
      "enum": ["active", "paused", "done"]
    },
    "goal": {
      "type": "string"
    },
    "my_role": {
      "type": "string"
    },
    "note": {
      "type": "string"
    },
    "importance": {
      "type": "string",
      "enum": ["P0", "P1", "P2", "P3"]
    },
    "relevance": {
      "type": "integer"
    },
    "tags": {
      "type": "array",
      "items": {
        "type": "string"
      }
    },
    "summary": {
      "type": "string",
      "description": "Markdown, replaces summary.md"
    },
    "log": {
      "type": "string",
      "description": "one line appended to a project's timeline"
    },
    "aliases": {
      "type": "array",
      "items": {
        "type": "string"
      }
    },
    "chats": {
      "type": "array",
      "items": {
        "type": "string"
      }
    },
    "people": {
      "type": "array",
      "items": {
        "type": "string"
      }
    },
    "dirs": {
      "type": "array",
      "items": {
        "type": "string"
      }
    },
    "projects": {
      "type": "array",
      "items": {
        "type": "string"
      }
    }
  }
}
```
