Start a new Persona conversation with an opening message and return its id at once (it does not wait for the answer). For work that deserves its own thread, when the user asks for one. The message arrives marked as coming from this conversation; it is named after its first turn like any new conversation.

```json
{
  "type": "object",
  "required": ["text"],
  "properties": {
    "text": {
      "type": "string",
      "description": "the opening message: everything the new conversation needs, it does not see this one"
    }
  }
}
```
