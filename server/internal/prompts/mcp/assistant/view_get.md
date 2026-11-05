Read a persistent live card created by the calling assistant conversation. Returns its id, conversation, version, full View, timestamps and actions (the user's selected reply and submitted values, with pending/sent/failed delivery status). Use it before updating a card after resuming work; an id from another conversation is forbidden. Updating the card never clears a committed answer.

```json
{
  "type": "object",
  "required": ["id"],
  "properties": {
    "id": {"type": "string", "description": "The exact live card id returned by view_show."}
  }
}
```
