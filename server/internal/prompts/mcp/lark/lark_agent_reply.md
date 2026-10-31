Answer the Feishu /agent request this conversation was started for: the hub posts `text` (Markdown) as the user's reply to the request, in its thread if it was asked in one, with a footer saying an agent wrote it. Call it once, at the end, with the answer for the people in that chat.

```json
{
  "type": "object",
  "required": ["text"],
  "properties": {
    "text": {
      "type": "string"
    }
  }
}
```
