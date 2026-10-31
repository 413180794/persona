Read the recent conversation of one agent (from agents_list): the user's messages, what the agent said, tool calls in short form. `turns` = how many recent exchanges (default 2, max 10).

```json
{
  "type": "object",
  "required": ["machine", "session_id"],
  "properties": {
    "machine": {
      "type": "string"
    },
    "session_id": {
      "type": "string",
      "description": "full id or its first 8 characters"
    },
    "turns": {
      "type": "integer"
    }
  }
}
```
