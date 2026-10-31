Set up a recurring routine: every day (or on some weekdays) at a time, Jarvis starts a new conversation with `prompt`, you do the work, and the user gets a push with your answer. Use for "每天 9 点给我早报", "每周五下午起草周报". For one-off reminders use schedule_followup. Pass `id` (from list_followups) to change an existing routine: only the fields you give change; `enabled: false` pauses it. Times are in the user's time zone.

```json
{
  "type": "object",
  "properties": {
    "id": {
      "type": "string",
      "description": "to change an existing routine"
    },
    "title": {
      "type": "string",
      "description": "short name shown in the app, e.g. 早报"
    },
    "prompt": {
      "type": "string",
      "description": "what to do each time, self-contained"
    },
    "time": {
      "type": "string",
      "description": "HH:MM, 24-hour"
    },
    "weekdays": {
      "type": "array",
      "items": {
        "type": "integer",
        "minimum": 1,
        "maximum": 7
      },
      "description": "1 = Monday … 7 = Sunday; omit or empty = every day"
    },
    "enabled": {
      "type": "boolean"
    }
  }
}
```
