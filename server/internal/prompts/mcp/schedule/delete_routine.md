Delete a scheduled task (routine or follow-up) by id (from list_followups). Confirm with the user first unless they asked for it.

```json
{
  "type": "object",
  "required": ["id"],
  "properties": {
    "id": {
      "type": "string"
    }
  }
}
```
