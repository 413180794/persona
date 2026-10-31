Read one message in full: headers, plain text, HTML and the attachment list. Does not mark the message as read.

```json
{
  "type": "object",
  "properties": {
    "account_id": {
      "type": "string"
    },
    "uid": {
      "type": "integer"
    },
    "mailbox": {
      "type": "string",
      "description": "Mailbox name, default INBOX"
    }
  },
  "required": ["account_id", "uid"],
  "additionalProperties": false
}
```
