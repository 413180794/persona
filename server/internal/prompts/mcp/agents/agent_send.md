Send a message to an agent as the user would type it (answers its question, tells it to continue, gives new instructions). Queued if the agent is working. Only send what the user asked for. An agent that is not running any more (a finished task's agent is closed after a few idle minutes) is resumed first, in the same session with the same machine, directory and launcher, so you can keep working with it.

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
