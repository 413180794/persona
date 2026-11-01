Add a mail account to Jarvis for the user (Jarvis then reads it in the inbox and you can use mail_* on it). The user approves it on their phone first, then the hub connects with the settings and saves the account only when IMAP and SMTP both work; the password is stored encrypted on the hub. Most providers need only email and password: QQ, 163, Outlook, iCloud and others fill their servers by themselves, and provider "feishu" uses Feishu mail's fixed servers. Many providers want an app password / 授权码 rather than the login password: tell the user so when it fails to log in. Gmail cannot be added here (Google sign-in happens in the app): the tool says so. Never repeat the password back in your replies.

```json
{
  "type": "object",
  "properties": {
    "email": {
      "type": "string"
    },
    "password": {
      "type": "string",
      "description": "the password or app password / 授权码"
    },
    "provider": {
      "type": "string",
      "enum": ["imap", "feishu", "gmail"],
      "description": "default imap; feishu for Feishu / Lark mail"
    },
    "name": {
      "type": "string",
      "description": "how the account is shown; default the email"
    },
    "username": {
      "type": "string",
      "description": "login name when it is not the email"
    },
    "imap_host": {
      "type": "string"
    },
    "imap_port": {
      "type": "integer"
    },
    "smtp_host": {
      "type": "string"
    },
    "smtp_port": {
      "type": "integer"
    },
    "smtp_security": {
      "type": "string",
      "enum": ["tls", "starttls"]
    }
  },
  "required": ["email"],
  "additionalProperties": false
}
```
