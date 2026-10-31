Send the user a push notification right now (works when the app is closed). Tapping it opens this conversation. Use for results of scheduled runs and anything time-sensitive; don't notify for ordinary replies (they get one automatically).

```json
{
  "type": "object",
  "required": ["title", "body"],
  "properties": {
    "title": {
      "type": "string"
    },
    "body": {
      "type": "string"
    }
  }
}
```
