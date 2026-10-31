Start a new Claude Code agent in herdr on one of the user's computers, in a project directory, with a first prompt. Use for real development work instead of doing it here. Returns the new session id.

```json
{
  "type": "object",
  "required": ["machine", "cwd", "prompt"],
  "properties": {
    "machine": {
      "type": "string"
    },
    "cwd": {
      "type": "string",
      "description": "absolute path or ~/…"
    },
    "prompt": {
      "type": "string"
    },
    "launcher": {
      "type": "string",
      "description": "shell command that starts claude (default: the machine's default)"
    }
  }
}
```
