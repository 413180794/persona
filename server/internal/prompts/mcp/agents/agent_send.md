Send a message to an agent as the user would type it (answers its question, tells it to continue, gives new instructions). Queued if the agent is working. Only send what the user asked for.

```json
{
  "type": "object",
  "required": ["machine", "session_id", "text"],
  "properties": {
    "machine": {
      "type": "string"
    },
    "session_id": {
      "type": "string"
    },
    "text": {
      "type": "string"
    }
  }
}
```
