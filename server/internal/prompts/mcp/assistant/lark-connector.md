Run lark-connector's `ol` helper (the lark-connector skill): `args` are the words after `ol`, e.g. ["GET", "/me"], ["GET", "/conversations?scope=group&limit=100"], ["POST", "/conversations/FEED_ID/messages", "{\"text\":\"hi\"}"]. Reads run at once; writes other people see (send, reply, react, documents, calendar, approvals…) are confirmed by the user on the phone first, and a refusal comes back as the result. Returns what ol prints.

```json
{
  "type": "object",
  "required": ["args"],
  "properties": {
    "args": {
      "type": "array",
      "items": {
        "type": "string"
      },
      "description": "The ol arguments: METHOD PATH [JSON body] and its flags (--url-query, -o FILE, --upload FILE…), one per item"
    }
  }
}
```
