Send a message to another Jarvis conversation (not this one). If it is running, the message waits until its current turn ends. The message arrives marked as coming from this conversation, so the other side can answer with conversation_send. Conversations passing messages back and forth are limited (a few hops from the user's message, a few messages per pair in ten minutes): don't reply just to acknowledge.

```json
{
  "type": "object",
  "required": ["id", "text"],
  "properties": {
    "id": {
      "type": "string",
      "description": "full id or its first 8 characters"
    },
    "text": {
      "type": "string"
    }
  }
}
```
