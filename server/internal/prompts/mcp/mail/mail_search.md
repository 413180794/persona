Search mail. Returns message summaries (uid, subject, from, date, snippet). Use account_id from mail_accounts; omit it to search every account's inbox at once. For Gmail the query uses Gmail search syntax (from:, subject:, has:attachment, newer_than:7d); other providers treat it as a full-text search.

```json
{
  "type": "object",
  "properties": {
    "account_id": {
      "type": "string",
      "description": "Account id from mail_accounts; omit to search all inboxes"
    },
    "mailbox": {
      "type": "string",
      "description": "Mailbox name, default INBOX"
    },
    "query": {
      "type": "string",
      "description": "Search text or Gmail query"
    },
    "limit": {
      "type": "integer",
      "description": "Max results, default 20, max 200"
    },
    "before_uid": {
      "type": "integer",
      "description": "Return messages with UID lower than this, for paging"
    }
  },
  "additionalProperties": false
}
```
